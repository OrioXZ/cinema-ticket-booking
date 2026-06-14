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
	MongoURI          string
	MongoDatabase     string
	RedisURI          string
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
		MongoURI:          mongoURI,
		MongoDatabase:     mongoDatabase,
		RedisURI:          os.Getenv("REDIS_URI"),
		DependencyTimeout: defaultDependencyTimeout,
	}

	if cfg.RedisURI == "" {
		return Config{}, fmt.Errorf("REDIS_URI is required")
	}

	return cfg, nil
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
