package config

import "testing"

func TestLoadRequiresDependencyURIs(t *testing.T) {
	t.Setenv("MONGO_URI", "")
	t.Setenv("MONGO_HOST", "")
	t.Setenv("MONGO_DATABASE", "")
	t.Setenv("MONGO_USERNAME", "")
	t.Setenv("MONGO_PASSWORD", "")
	t.Setenv("REDIS_URI", "")

	if _, err := Load(); err == nil {
		t.Fatal("Load() expected an error when dependency URIs are missing")
	}
}

func TestLoadReadsEnvironment(t *testing.T) {
	t.Setenv("APP_ENV", "test")
	t.Setenv("BACKEND_PORT", "9090")
	t.Setenv("MONGO_URI", "mongodb://mongo/test")
	t.Setenv("MONGO_DATABASE", "test")
	t.Setenv("REDIS_URI", "redis://redis:6379/1")
	t.Setenv("EVENT_CHANNEL", "custom.events")
	t.Setenv("WEBSOCKET_ALLOWED_ORIGINS", "http://localhost:5173, http://localhost:4173")
	t.Setenv("AUTH_MODE", "development")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.AppEnv != "test" || cfg.Port != "9090" || cfg.MongoDatabase != "test" ||
		cfg.EventChannel != "custom.events" || len(cfg.WebSocketOrigins) != 2 ||
		cfg.AuthMode != "development" {
		t.Fatalf("Load() returned unexpected config: %+v", cfg)
	}
}

func TestLoadAuthenticationMode(t *testing.T) {
	setDependencies(t)

	t.Run("development default", func(t *testing.T) {
		t.Setenv("AUTH_MODE", "")
		cfg, err := Load()
		if err != nil || cfg.AuthMode != "development" {
			t.Fatalf("Load() = %+v, %v", cfg, err)
		}
	})

	t.Run("unsupported", func(t *testing.T) {
		t.Setenv("AUTH_MODE", "automatic")
		if _, err := Load(); err == nil {
			t.Fatal("Load() expected unsupported AUTH_MODE error")
		}
	})

	t.Run("firebase requires project", func(t *testing.T) {
		t.Setenv("AUTH_MODE", "firebase")
		t.Setenv("FIREBASE_PROJECT_ID", "")
		if _, err := Load(); err == nil {
			t.Fatal("Load() expected missing Firebase project error")
		}
	})

	t.Run("firebase", func(t *testing.T) {
		t.Setenv("AUTH_MODE", "firebase")
		t.Setenv("FIREBASE_PROJECT_ID", "cinema-project")
		cfg, err := Load()
		if err != nil || cfg.FirebaseProjectID != "cinema-project" {
			t.Fatalf("Load() = %+v, %v", cfg, err)
		}
	})
}

func TestLoadRequiresMongoDatabaseWithMongoURI(t *testing.T) {
	t.Setenv("MONGO_URI", "mongodb://mongo/other")
	t.Setenv("MONGO_DATABASE", "")
	t.Setenv("REDIS_URI", "redis://redis:6379/0")

	_, err := Load()
	if err == nil || err.Error() != "MONGO_DATABASE is required" {
		t.Fatalf("Load() error = %v, want MONGO_DATABASE is required", err)
	}
}

func TestLoadBuildsMongoURIFromEnvironment(t *testing.T) {
	t.Setenv("MONGO_URI", "")
	t.Setenv("MONGO_HOST", "mongodb:27017")
	t.Setenv("MONGO_DATABASE", "cinema")
	t.Setenv("MONGO_USERNAME", "cinema user")
	t.Setenv("MONGO_PASSWORD", "password/with:specials")
	t.Setenv("REDIS_URI", "redis://redis:6379/0")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	want := "mongodb://cinema%20user:password%2Fwith%3Aspecials@mongodb:27017/cinema?authSource=admin"
	if cfg.MongoURI != want {
		t.Fatalf("MongoURI = %q, want %q", cfg.MongoURI, want)
	}
}

func setDependencies(t *testing.T) {
	t.Helper()
	t.Setenv("MONGO_URI", "mongodb://mongo/test")
	t.Setenv("MONGO_DATABASE", "test")
	t.Setenv("REDIS_URI", "redis://redis:6379/0")
}
