package mongodb

import (
	"context"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Client struct {
	client *mongo.Client
}

func Connect(ctx context.Context, uri string) (*Client, error) {
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}

	wrapped := &Client{client: client}
	if err := wrapped.Ping(ctx); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, err
	}

	return wrapped, nil
}

func (c *Client) Ping(ctx context.Context) error {
	return c.client.Ping(ctx, nil)
}

func (c *Client) Disconnect(ctx context.Context) error {
	return c.client.Disconnect(ctx)
}
