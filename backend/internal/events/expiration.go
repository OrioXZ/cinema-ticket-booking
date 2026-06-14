package events

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

type ExpirationListener struct {
	client    goredis.UniversalClient
	database  int
	publisher Publisher
	logger    Logger
	now       func() time.Time
}

func NewExpirationListener(
	client goredis.UniversalClient,
	database int,
	publisher Publisher,
	logger Logger,
) *ExpirationListener {
	return &ExpirationListener{
		client: client, database: database, publisher: publisher, logger: logger, now: time.Now,
	}
}

func (l *ExpirationListener) Run(ctx context.Context) error {
	channel := fmt.Sprintf("__keyevent@%d__:expired", l.database)
	subscription := l.client.Subscribe(ctx, channel)
	defer subscription.Close()
	if _, err := subscription.Receive(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
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
			showtimeID, seatNo, matched, valid := parseSeatLockKey(message.Payload)
			if !matched {
				continue
			}
			if !valid {
				l.logger.Printf("ignored malformed expired seat-lock key")
				continue
			}
			event, err := New(SeatLockExpired, showtimeID, seatNo, "", "", "lock expired", l.now())
			if err != nil {
				l.logger.Printf(
					"failed to create lock-expiration event for showtime %q seat %q",
					showtimeID,
					seatNo,
				)
				continue
			}
			if err := l.publisher.Publish(ctx, event); err != nil {
				l.logger.Printf(
					"failed to publish lock-expiration event for showtime %q seat %q",
					showtimeID,
					seatNo,
				)
			}
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
