package events

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

type BookingStateReader interface {
	IsBooked(context.Context, string, string) (bool, error)
}

type ExpirationPublisher interface {
	PublishExpiration(
		context.Context,
		string,
		string,
		int64,
		DomainEvent,
	) (bool, error)
}

type ExpirationProcessor struct {
	bookings  BookingStateReader
	publisher ExpirationPublisher
	logger    Logger
	now       func() time.Time
}

func NewExpirationProcessor(
	bookings BookingStateReader,
	publisher ExpirationPublisher,
	logger Logger,
) *ExpirationProcessor {
	return &ExpirationProcessor{
		bookings: bookings, publisher: publisher, logger: logger, now: time.Now,
	}
}

func (p *ExpirationProcessor) Process(ctx context.Context, markerKey string) {
	showtimeID, seatNo, generation, matched, valid := parseExpirationMarkerKey(markerKey)
	if !matched {
		return
	}
	if !valid {
		p.logger.Printf("ignored malformed expired seat-lock marker")
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

	event, err := New(
		SeatLockExpired,
		showtimeID,
		seatNo,
		"",
		"",
		"lock expired",
		p.now(),
		generation,
	)
	if err != nil {
		p.logger.Printf(
			"failed to create lock-expiration event for showtime %q seat %q",
			showtimeID,
			seatNo,
		)
		return
	}
	if _, err := p.publisher.PublishExpiration(
		ctx,
		showtimeID,
		seatNo,
		generation,
		event,
	); err != nil {
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

func parseExpirationMarkerKey(
	key string,
) (showtimeID string, seatNo string, generation int64, matched bool, valid bool) {
	if !strings.HasPrefix(key, "seat_lock_expiry:") {
		return "", "", 0, false, false
	}
	parts := strings.Split(key, ":")
	if len(parts) != 4 || parts[1] == "" || parts[2] == "" || parts[3] == "" {
		return "", "", 0, true, false
	}
	generation, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil || generation <= 0 {
		return "", "", 0, true, false
	}
	return parts[1], strings.ToUpper(parts[2]), generation, true, true
}
