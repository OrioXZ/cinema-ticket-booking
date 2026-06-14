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

	"github.com/OrioXZ/cinema-ticket-booking/backend/internal/audit"
	"github.com/OrioXZ/cinema-ticket-booking/backend/internal/booking"
	"github.com/OrioXZ/cinema-ticket-booking/backend/internal/config"
	"github.com/OrioXZ/cinema-ticket-booking/backend/internal/events"
	"github.com/OrioXZ/cinema-ticket-booking/backend/internal/health"
	"github.com/OrioXZ/cinema-ticket-booking/backend/internal/identity"
	"github.com/OrioXZ/cinema-ticket-booking/backend/internal/lifecycle"
	"github.com/OrioXZ/cinema-ticket-booking/backend/internal/platform/mongodb"
	redisclient "github.com/OrioXZ/cinema-ticket-booking/backend/internal/platform/redis"
	"github.com/OrioXZ/cinema-ticket-booking/backend/internal/realtime"
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

	database := mongoClient.Database(cfg.MongoDatabase)
	bookingRepository := booking.NewMongoRepository(database)
	auditRepository := audit.NewMongoRepository(database)
	initializeCtx, cancelInitialize := context.WithTimeout(context.Background(), 10*time.Second)
	if err := bookingRepository.Initialize(initializeCtx); err != nil {
		cancelInitialize()
		log.Fatalf("initialize booking persistence: %v", err)
	}
	if err := auditRepository.Initialize(initializeCtx); err != nil {
		cancelInitialize()
		log.Fatalf("initialize audit persistence: %v", err)
	}
	cancelInitialize()

	appCtx, cancelApp := context.WithCancel(context.Background())
	defer cancelApp()

	eventPublisher := events.NewRedisPublisher(redisClient.Raw(), cfg.EventChannel)
	lockRepository := booking.NewRedisLockRepository(redisClient.Raw())
	bookingService := booking.NewService(
		bookingRepository,
		bookingRepository,
		lockRepository,
		eventPublisher,
		log.Default(),
	)
	bookingHandler := booking.NewHandler(bookingService)
	hub := realtime.NewHub()
	websocketHandler := realtime.NewHandler(hub, cfg.WebSocketOrigins)

	auditConsumer := audit.NewConsumer(auditRepository)
	auditSubscriber := events.NewRedisSubscriber(
		redisClient.Raw(),
		cfg.EventChannel,
		auditConsumer.Handle,
		log.Default(),
	)
	realtimeConsumer := realtime.NewConsumer(hub)
	realtimeSubscriber := events.NewRedisSubscriber(
		redisClient.Raw(),
		cfg.EventChannel,
		realtimeConsumer.Handle,
		log.Default(),
	)
	expirationProcessor := events.NewExpirationProcessor(
		bookingRepository,
		events.NewRedisExpirationPublisher(redisClient.Raw()),
		cfg.EventChannel,
		log.Default(),
	)
	expirationListener := events.NewExpirationListener(
		redisClient.Raw(),
		redisClient.Raw().Options().DB,
		expirationProcessor,
	)

	workers, err := lifecycle.Start(appCtx, cfg.DependencyTimeout, []lifecycle.Worker{
		{Name: "audit subscriber", Run: auditSubscriber.Run},
		{Name: "realtime subscriber", Run: realtimeSubscriber.Run},
		{Name: "lock expiration listener", Run: expirationListener.Run},
	})
	if err != nil {
		log.Printf("start background workers: %v", err)
		hub.Shutdown()
		return
	}
	go func() {
		for workerError := range workers.Errors() {
			log.Printf("%s stopped with error", workerError.Name)
		}
	}()

	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery(), identity.DevelopmentMiddleware())

	healthHandler := health.NewHandler(mongoClient, redisClient, cfg.DependencyTimeout)
	router.GET("/health", healthHandler.Get)
	api := router.Group("/api")
	api.GET("/health", healthHandler.Get)
	bookingHandler.Register(api)
	router.GET("/ws/showtimes/:showtimeId", websocketHandler.Get)

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("backend listening on %s", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()

	shutdownSignal := make(chan os.Signal, 1)
	signal.Notify(shutdownSignal, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-shutdownSignal:
	case err := <-serverErrors:
		log.Printf("HTTP server stopped with error: %v", err)
	}

	cancelApp()
	workers.Stop()
	hub.Shutdown()
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown HTTP server: %v", err)
	}
	workers.Wait()
}
