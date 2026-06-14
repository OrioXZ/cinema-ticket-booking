package audit

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/OrioXZ/cinema-ticket-booking/backend/internal/events"
)

const collectionName = "audit_logs"

type Log struct {
	EventID     string      `bson:"event_id" json:"event_id"`
	EventType   events.Type `bson:"event_type" json:"event_type"`
	OccurredAt  time.Time   `bson:"occurred_at" json:"occurred_at"`
	ProcessedAt time.Time   `bson:"processed_at" json:"processed_at"`
	ShowtimeID  string      `bson:"showtime_id" json:"showtime_id"`
	SeatNo      string      `bson:"seat_no" json:"seat_no"`
	UserID      string      `bson:"user_id,omitempty" json:"user_id,omitempty"`
	BookingID   string      `bson:"booking_id,omitempty" json:"booking_id,omitempty"`
	Reason      string      `bson:"reason,omitempty" json:"reason,omitempty"`
}

type Repository interface {
	Insert(context.Context, Log) error
}

type MongoRepository struct {
	collection *mongo.Collection
}

func NewMongoRepository(database *mongo.Database) *MongoRepository {
	return &MongoRepository{collection: database.Collection(collectionName)}
}

func (r *MongoRepository) Initialize(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "event_id", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("unique_event_id"),
		},
		{
			Keys:    bson.D{{Key: "occurred_at", Value: -1}},
			Options: options.Index().SetName("recent_audit_events"),
		},
	})
	return err
}

func (r *MongoRepository) Insert(ctx context.Context, entry Log) error {
	_, err := r.collection.InsertOne(ctx, entry)
	if mongo.IsDuplicateKeyError(err) {
		return nil
	}
	return err
}

type Consumer struct {
	repository Repository
	now        func() time.Time
}

func NewConsumer(repository Repository) *Consumer {
	return &Consumer{repository: repository, now: time.Now}
}

func (c *Consumer) Handle(ctx context.Context, event events.DomainEvent) error {
	if !shouldAudit(event.Type) {
		return nil
	}
	return c.repository.Insert(ctx, FromEvent(event, c.now().UTC()))
}

func FromEvent(event events.DomainEvent, processedAt time.Time) Log {
	return Log{
		EventID:     event.ID,
		EventType:   event.Type,
		OccurredAt:  event.OccurredAt.UTC(),
		ProcessedAt: processedAt.UTC(),
		ShowtimeID:  event.ShowtimeID,
		SeatNo:      event.SeatNo,
		UserID:      event.UserID,
		BookingID:   event.BookingID,
		Reason:      event.Reason,
	}
}

func shouldAudit(eventType events.Type) bool {
	switch eventType {
	case events.BookingConfirmed,
		events.SeatReleased,
		events.SeatLockExpired,
		events.LockAcquisitionFailed:
		return true
	default:
		return false
	}
}
