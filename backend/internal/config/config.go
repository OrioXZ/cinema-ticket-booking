package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

const defaultDependencyTimeout = 3 * time.Second

type Config struct {
	AppEnv            string
	Port              string
	AuthMode          string
	FirebaseProjectID string
	MongoURI          string
	MongoDatabase     string
	RedisURI          string
	EventChannel      string
	WebSocketOrigins  []string
	DependencyTimeout time.Duration
}

func Load() (Config, error) {
	mongoDatabase := os.Getenv("MONGO_DATABASE")
	if mongoDatabase == "" {
		return Config{}, fmt.Errorf("MONGO_DATABASE is required")
	}

	mongoURI, err := loadMongoURI()
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		AppEnv:            valueOrDefault("APP_ENV", "development"),
		Port:              valueOrDefault("BACKEND_PORT", "8080"),
		AuthMode:          strings.ToLower(valueOrDefault("AUTH_MODE", "development")),
		FirebaseProjectID: strings.TrimSpace(os.Getenv("FIREBASE_PROJECT_ID")),
		MongoURI:          mongoURI,
		MongoDatabase:     mongoDatabase,
		RedisURI:          os.Getenv("REDIS_URI"),
		EventChannel:      valueOrDefault("EVENT_CHANNEL", "cinema.events"),
		WebSocketOrigins:  splitCSV(valueOrDefault("WEBSOCKET_ALLOWED_ORIGINS", "http://localhost:5173")),
		DependencyTimeout: defaultDependencyTimeout,
	}

	if cfg.RedisURI == "" {
		return Config{}, fmt.Errorf("REDIS_URI is required")
	}
	switch cfg.AuthMode {
	case "development":
	case "firebase":
		if cfg.FirebaseProjectID == "" {
			return Config{}, fmt.Errorf("FIREBASE_PROJECT_ID is required when AUTH_MODE=firebase")
		}
	default:
		return Config{}, fmt.Errorf("AUTH_MODE must be development or firebase")
	}

	return cfg, nil
}

func splitCSV(value string) []string {
	var values []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			values = append(values, item)
		}
	}
	return values
}

func loadMongoURI() (string, error) {
	if uri := os.Getenv("MONGO_URI"); uri != "" {
		return uri, nil
	}

	host := os.Getenv("MONGO_HOST")
	database := os.Getenv("MONGO_DATABASE")
	username := os.Getenv("MONGO_USERNAME")
	password := os.Getenv("MONGO_PASSWORD")
	if host == "" || database == "" || username == "" || password == "" {
		return "", fmt.Errorf(
			"MONGO_URI or MONGO_HOST, MONGO_DATABASE, MONGO_USERNAME, and MONGO_PASSWORD are required",
		)
	}

	uri := &url.URL{
		Scheme:   "mongodb",
		User:     url.UserPassword(username, password),
		Host:     host,
		Path:     "/" + strings.TrimPrefix(database, "/"),
		RawQuery: "authSource=admin",
	}
	return uri.String(), nil
}

func valueOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
