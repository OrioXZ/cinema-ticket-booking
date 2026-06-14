package events

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

var publishExpirationIfUnlockedScript = goredis.NewScript(`
if redis.call("EXISTS", KEYS[1]) == 1 then
	return 0
end

redis.call("PUBLISH", ARGV[1], ARGV[2])
return 1
`)

type BookingStateReader interface {
	IsBooked(context.Context, string, string) (bool, error)
}

type ExpirationPublisher interface {
	PublishIfUnlocked(
		context.Context,
		string,
		string,
		DomainEvent,
	) (bool, error)
}

type RedisExpirationPublisher struct {
	client goredis.UniversalClient
}

func NewRedisExpirationPublisher(client goredis.UniversalClient) *RedisExpirationPublisher {
	return &RedisExpirationPublisher{client: client}
}

func (p *RedisExpirationPublisher) PublishIfUnlocked(
	ctx context.Context,
	lockKey string,
	channel string,
	event DomainEvent,
) (bool, error) {
	data, err := Marshal(event)
	if err != nil {
		return false, err
	}
	result, err := publishExpirationIfUnlockedScript.Run(
		ctx,
		p.client,
		[]string{lockKey},
		channel,
		string(data),
	).Int()
	if err != nil {
		return false, err
	}
	switch result {
	case 0:
		return false, nil
	case 1:
		return true, nil
	default:
		return false, fmt.Errorf("unexpected expiration publish result")
	}
}

type ExpirationProcessor struct {
	bookings  BookingStateReader
	publisher ExpirationPublisher
	channel   string
	logger    Logger
	now       func() time.Time
}

func NewExpirationProcessor(
	bookings BookingStateReader,
	publisher ExpirationPublisher,
	channel string,
	logger Logger,
) *ExpirationProcessor {
	return &ExpirationProcessor{
		bookings: bookings, publisher: publisher, channel: channel, logger: logger, now: time.Now,
	}
}

func (p *ExpirationProcessor) Process(ctx context.Context, lockKey string) {
	showtimeID, seatNo, matched, valid := parseSeatLockKey(lockKey)
	if !matched {
		return
	}
	if !valid {
		p.logger.Printf("ignored malformed expired seat-lock key")
		return
	}

	booked, err := p.bookings.IsBooked(ctx, showtimeID, seatNo)
	if err != nil {
		p.logger.Printf(
			"failed to check booking state for expired lock at showtime %q seat %q",
			showtimeID,
			seatNo,
		)
		return
	}
	if booked {
		return
	}

	event, err := New(SeatLockExpired, showtimeID, seatNo, "", "", "lock expired", p.now())
	if err != nil {
		p.logger.Printf(
			"failed to create lock-expiration event for showtime %q seat %q",
			showtimeID,
			seatNo,
		)
		return
	}
	if _, err := p.publisher.PublishIfUnlocked(ctx, lockKey, p.channel, event); err != nil {
		p.logger.Printf(
			"failed to atomically publish lock-expiration event for showtime %q seat %q",
			showtimeID,
			seatNo,
		)
	}
}

type ExpirationListener struct {
	client    goredis.UniversalClient
	database  int
	processor *ExpirationProcessor
}

func NewExpirationListener(
	client goredis.UniversalClient,
	database int,
	processor *ExpirationProcessor,
) *ExpirationListener {
	return &ExpirationListener{
		client: client, database: database, processor: processor,
	}
}

func (l *ExpirationListener) Run(ctx context.Context, ready func()) error {
	channel := fmt.Sprintf("__keyevent@%d__:expired", l.database)
	subscription := l.client.Subscribe(ctx, channel)
	defer subscription.Close()
	if _, err := subscription.Receive(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
	if ready != nil {
		ready()
	}

	messages := subscription.Channel()
	for {
		select {
		case <-ctx.Done():
			return nil
		case message, ok := <-messages:
			if !ok {
				return nil
			}
			l.processor.Process(ctx, message.Payload)
		}
	}
}

func parseSeatLockKey(key string) (showtimeID string, seatNo string, matched bool, valid bool) {
	if !strings.HasPrefix(key, "seat_lock:") {
		return "", "", false, false
	}
	parts := strings.Split(key, ":")
	if len(parts) != 3 || parts[1] == "" || parts[2] == "" {
		return "", "", true, false
	}
	return parts[1], strings.ToUpper(parts[2]), true, true
}
