package booking

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	moviesCollection    = "movies"
	showtimesCollection = "showtimes"
	bookingsCollection  = "bookings"
)

type MongoRepository struct {
	database *mongo.Database
}

func NewMongoRepository(database *mongo.Database) *MongoRepository {
	return &MongoRepository{database: database}
}

func (r *MongoRepository) Initialize(ctx context.Context) error {
	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "showtime_id", Value: 1}, {Key: "seat_no", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("unique_showtime_seat"),
		},
		{
			Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "created_at", Value: -1}},
			Options: options.Index().SetName("bookings_by_user"),
		},
	}
	if _, err := r.database.Collection(bookingsCollection).Indexes().CreateMany(ctx, indexes); err != nil {
		return err
	}
	return r.seed(ctx)
}

func (r *MongoRepository) seed(ctx context.Context) error {
	createdAt := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	movie := Movie{
		ID:              "movie-1",
		Title:           "The Last Showing",
		DurationMinutes: 118,
		CreatedAt:       createdAt,
	}
	showtime := Showtime{
		ID:         "showtime-1",
		MovieID:    movie.ID,
		StartsAt:   time.Date(2026, time.December, 1, 19, 30, 0, 0, time.UTC),
		Auditorium: "Screen 1",
		Seats: []string{
			"A1", "A2", "A3", "A4", "A5", "A6", "A7", "A8", "A9", "A10",
			"B1", "B2", "B3", "B4", "B5", "B6", "B7", "B8", "B9", "B10",
		},
		CreatedAt: createdAt,
	}
	upsert := options.Replace().SetUpsert(true)
	if _, err := r.database.Collection(moviesCollection).ReplaceOne(ctx, bson.M{"_id": movie.ID}, movie, upsert); err != nil {
		return err
	}
	if _, err := r.database.Collection(showtimesCollection).ReplaceOne(ctx, bson.M{"_id": showtime.ID}, showtime, upsert); err != nil {
		return err
	}
	return nil
}

func (r *MongoRepository) ListShowtimes(ctx context.Context) ([]ShowtimeSummary, error) {
	cursor, err := r.database.Collection(showtimesCollection).Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "starts_at", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var showtimes []Showtime
	if err := cursor.All(ctx, &showtimes); err != nil {
		return nil, err
	}
	results := make([]ShowtimeSummary, 0, len(showtimes))
	for _, showtime := range showtimes {
		var movie Movie
		if err := r.database.Collection(moviesCollection).FindOne(ctx, bson.M{"_id": showtime.MovieID}).Decode(&movie); err != nil {
			return nil, err
		}
		results = append(results, ShowtimeSummary{Showtime: showtime, Movie: movie})
	}
	return results, nil
}

func (r *MongoRepository) GetShowtime(ctx context.Context, id string) (Showtime, error) {
	var showtime Showtime
	err := r.database.Collection(showtimesCollection).FindOne(ctx, bson.M{"_id": id}).Decode(&showtime)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Showtime{}, ErrNotFound
	}
	return showtime, err
}

func (r *MongoRepository) ListBookedSeats(ctx context.Context, showtimeID string) (map[string]struct{}, error) {
	cursor, err := r.database.Collection(bookingsCollection).Find(
		ctx,
		bson.M{"showtime_id": showtimeID, "status": BookingStatusConfirmed},
		options.Find().SetProjection(bson.M{"seat_no": 1}),
	)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	booked := make(map[string]struct{})
	for cursor.Next(ctx) {
		var value struct {
			SeatNo string `bson:"seat_no"`
		}
		if err := cursor.Decode(&value); err != nil {
			return nil, err
		}
		booked[value.SeatNo] = struct{}{}
	}
	return booked, cursor.Err()
}

func (r *MongoRepository) IsBooked(ctx context.Context, showtimeID, seatNo string) (bool, error) {
	err := r.database.Collection(bookingsCollection).FindOne(
		ctx,
		bson.M{"showtime_id": showtimeID, "seat_no": seatNo, "status": BookingStatusConfirmed},
		options.FindOne().SetProjection(bson.M{"_id": 1}),
	).Err()
	if errors.Is(err, mongo.ErrNoDocuments) {
		return false, nil
	}
	return err == nil, err
}

func (r *MongoRepository) Create(ctx context.Context, booking Booking) error {
	_, err := r.database.Collection(bookingsCollection).InsertOne(ctx, booking)
	if mongo.IsDuplicateKeyError(err) {
		return ErrDuplicateBooking
	}
	return err
}

func (r *MongoRepository) ListByUser(ctx context.Context, userID string) ([]Booking, error) {
	cursor, err := r.database.Collection(bookingsCollection).Find(
		ctx,
		bson.M{"user_id": userID},
		options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}),
	)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	bookings := make([]Booking, 0)
	if err := cursor.All(ctx, &bookings); err != nil {
		return nil, err
	}
	return bookings, nil
}

func (r *MongoRepository) ListConfirmed(ctx context.Context, userID string, limit int64) ([]Booking, error) {
	filter := bson.M{"status": BookingStatusConfirmed}
	if userID != "" {
		filter["user_id"] = userID
	}
	cursor, err := r.database.Collection(bookingsCollection).Find(
		ctx,
		filter,
		options.Find().
			SetSort(bson.D{{Key: "created_at", Value: -1}}).
			SetLimit(limit),
	)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	bookings := make([]Booking, 0)
	if err := cursor.All(ctx, &bookings); err != nil {
		return nil, err
	}
	return bookings, nil
}
