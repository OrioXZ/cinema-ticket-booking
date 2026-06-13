package redis

import (
	"context"

	goredis "github.com/redis/go-redis/v9"
)

type Client struct {
	client *goredis.Client
}

func Connect(ctx context.Context, uri string) (*Client, error) {
	options, err := goredis.ParseURL(uri)
	if err != nil {
		return nil, err
	}

	client := goredis.NewClient(options)
	wrapped := &Client{client: client}
	if err := wrapped.Ping(ctx); err != nil {
		_ = client.Close()
		return nil, err
	}

	return wrapped, nil
}

func (c *Client) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

func (c *Client) Close() error {
	return c.client.Close()
}

func (c *Client) Raw() *goredis.Client {
	return c.client
}
