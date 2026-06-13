package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/OrioXZ/cinema-ticket-booking/backend/internal/config"
	"github.com/OrioXZ/cinema-ticket-booking/backend/internal/health"
	"github.com/OrioXZ/cinema-ticket-booking/backend/internal/platform/mongodb"
	redisclient "github.com/OrioXZ/cinema-ticket-booking/backend/internal/platform/redis"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load configuration: %v", err)
	}

	mongoStartupCtx, cancelMongoStartup := context.WithTimeout(context.Background(), cfg.DependencyTimeout)
	mongoClient, err := mongodb.Connect(mongoStartupCtx, cfg.MongoURI)
	cancelMongoStartup()
	if err != nil {
		log.Fatalf("connect to MongoDB: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), cfg.DependencyTimeout)
		defer cancel()
		if err := mongoClient.Disconnect(ctx); err != nil {
			log.Printf("disconnect MongoDB: %v", err)
		}
	}()

	redisStartupCtx, cancelRedisStartup := context.WithTimeout(context.Background(), cfg.DependencyTimeout)
	redisClient, err := redisclient.Connect(redisStartupCtx, cfg.RedisURI)
	cancelRedisStartup()
	if err != nil {
		log.Fatalf("connect to Redis: %v", err)
	}
	defer func() {
		if err := redisClient.Close(); err != nil {
			log.Printf("close Redis: %v", err)
		}
	}()

	if cfg.AppEnv == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

	healthHandler := health.NewHandler(mongoClient, redisClient, cfg.DependencyTimeout)
	router.GET("/health", healthHandler.Get)

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("backend listening on %s", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("serve HTTP: %v", err)
		}
	}()

	shutdownSignal := make(chan os.Signal, 1)
	signal.Notify(shutdownSignal, syscall.SIGINT, syscall.SIGTERM)
	<-shutdownSignal

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown HTTP server: %v", err)
	}
}
