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
	t.Setenv("REDIS_URI", "redis://redis:6379/1")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.AppEnv != "test" || cfg.Port != "9090" {
		t.Fatalf("Load() returned unexpected config: %+v", cfg)
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
