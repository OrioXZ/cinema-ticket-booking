package events

import (
	"context"
	"errors"

	goredis "github.com/redis/go-redis/v9"
)

type Logger interface {
	Printf(format string, args ...any)
}

type RedisPublisher struct {
	client  goredis.UniversalClient
	channel string
}

func NewRedisPublisher(client goredis.UniversalClient, channel string) *RedisPublisher {
	return &RedisPublisher{client: client, channel: channel}
}

func (p *RedisPublisher) Publish(ctx context.Context, event DomainEvent) error {
	data, err := Marshal(event)
	if err != nil {
		return err
	}
	return p.client.Publish(ctx, p.channel, data).Err()
}

type Handler func(context.Context, DomainEvent) error

type RedisSubscriber struct {
	client  goredis.UniversalClient
	channel string
	handler Handler
	logger  Logger
}

func NewRedisSubscriber(
	client goredis.UniversalClient,
	channel string,
	handler Handler,
	logger Logger,
) *RedisSubscriber {
	return &RedisSubscriber{
		client: client, channel: channel, handler: handler, logger: logger,
	}
}

func (s *RedisSubscriber) Run(ctx context.Context, ready func()) error {
	subscription := s.client.Subscribe(ctx, s.channel)
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
			event, err := Unmarshal([]byte(message.Payload))
			if err != nil {
				s.logger.Printf("ignored malformed domain event on channel %q", s.channel)
				continue
			}
			if err := s.handler(ctx, event); err != nil {
				s.logger.Printf(
					"domain event consumer failed for type %q showtime %q seat %q",
					event.Type,
					event.ShowtimeID,
					event.SeatNo,
				)
			}
		}
	}
}
