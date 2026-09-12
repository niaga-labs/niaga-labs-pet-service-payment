package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Kilat-Pet-Delivery/lib-common/auth"
	"github.com/Kilat-Pet-Delivery/lib-common/database"
	"github.com/Kilat-Pet-Delivery/lib-common/health"
	"github.com/Kilat-Pet-Delivery/lib-common/kafka"
	"github.com/Kilat-Pet-Delivery/lib-common/logger"
	"github.com/Kilat-Pet-Delivery/lib-common/middleware"
	"github.com/Kilat-Pet-Delivery/service-payment/internal/adapter"
	"github.com/Kilat-Pet-Delivery/service-payment/internal/application"
	"github.com/Kilat-Pet-Delivery/service-payment/internal/config"
	paymentEvents "github.com/Kilat-Pet-Delivery/service-payment/internal/events"
	"github.com/Kilat-Pet-Delivery/service-payment/internal/handler"
	"github.com/Kilat-Pet-Delivery/service-payment/internal/rail"
	"github.com/Kilat-Pet-Delivery/service-payment/internal/repository"
	"github.com/Kilat-Pet-Delivery/service-payment/internal/saga"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load configuration: %v", err)
	}

	// Initialize logger
	zapLogger, err := logger.NewNamed(cfg.AppEnv, "service-payment")
	if err != nil {
		log.Fatalf("failed to initialize logger: %v", err)
	}
	defer zapLogger.Sync()

	zapLogger.Info("starting service-payment",
		zap.String("port", cfg.Port),
	)

	// Connect to database
	dbConfig := database.PostgresConfig{
		Host:     cfg.DBConfig.Host,
		Port:     cfg.DBConfig.Port,
		User:     cfg.DBConfig.User,
		Password: cfg.DBConfig.Password,
		DBName:   cfg.DBConfig.DBName,
		SSLMode:  cfg.DBConfig.SSLMode,
	}

	db, err := database.Connect(dbConfig, zapLogger)
	if err != nil {
		zapLogger.Fatal("failed to connect to database", zap.Error(err))
	}

	// Run database migrations.
	//
	// KPD-60: this used to AutoMigrate in development and run the SQL migrations
	// everywhere else. promos, promo_usages and subscriptions had no SQL
	// migration, so they existed only in development -- which would have put
	// KPD-39 (promo apply during checkout) on tables that do not exist off a
	// developer laptop. Now that 003 and 004 cover them, every model in this
	// service has a SQL migration, so there is one path for all environments and
	// the two can no longer drift apart. Development still gets its schema
	// automatically, because the server applies the migrations at startup.
	dbURL := dbConfig.DatabaseURL()
	if err := database.RunMigrations(dbURL, "migrations", zapLogger); err != nil {
		zapLogger.Fatal("failed to run migrations", zap.Error(err))
	}

	// Initialize JWT manager
	jwtManager := auth.NewJWTManager(
		cfg.JWTConfig.Secret,
		15*time.Minute,
		7*24*time.Hour,
	)

	// Initialize Kafka producer
	kafkaProducer := kafka.NewProducer(cfg.KafkaConfig.Brokers, zapLogger)
	defer kafkaProducer.Close()

	// Initialize Stripe adapter (mock for development)
	stripeAdapter := adapter.NewMockStripeAdapter(zapLogger)

	// Initialize repositories
	paymentRepo := repository.NewPaymentRepository(db)

	// Initialize saga service
	sagaService := saga.NewPaymentSagaService(paymentRepo, stripeAdapter, kafkaProducer, cfg.PlatformFeePercent, zapLogger)

	// Initialize application service
	paymentService := application.NewPaymentService(paymentRepo, sagaService, zapLogger)

	// Initialize Kafka consumer for booking events
	consumerGroupID := cfg.KafkaConfig.GroupPrefix + "payment-service"
	bookingConsumer := paymentEvents.NewBookingEventConsumer(
		cfg.KafkaConfig.Brokers,
		consumerGroupID,
		paymentService,
		zapLogger,
	)
	defer bookingConsumer.Close()

	// Start Kafka consumer in a goroutine
	consumerCtx, consumerCancel := context.WithCancel(context.Background())
	defer consumerCancel()

	go func() {
		zapLogger.Info("starting booking event consumer")
		if err := bookingConsumer.Start(consumerCtx); err != nil {
			if consumerCtx.Err() == nil {
				zapLogger.Error("booking event consumer failed", zap.Error(err))
			}
		}
	}()

	// Initialize promo service and handler
	promoRepo := repository.NewGormPromoRepository(db)
	promoService := application.NewPromoService(promoRepo, zapLogger)
	promoHandler := handler.NewPromoHandler(promoService)

	// Initialize subscription service and handler
	subRepo := repository.NewGormSubscriptionRepository(db)
	subService := application.NewSubscriptionService(subRepo, zapLogger)
	subHandler := handler.NewSubscriptionHandler(subService)

	// Initialize cash-out rail and handler
	simulatedRail := rail.NewSimulatedRail(cfg.CashOutRailDelay, zapLogger, rail.RealClock{})
	cashOutRepo := repository.NewGormCashOutRepository(db)
	// InMemoryDestinationOwnership seeds an empty map by default.
	// Replace when service-identity exposes payout destinations.
	destinationOwnership := adapter.NewInMemoryDestinationOwnership(nil)
	cashOutHandler := handler.NewCashOutHandler(cashOutRepo, destinationOwnership, simulatedRail, cfg.CashOutRailDelay, zapLogger)

	// Initialize HTTP handler
	paymentHandler := handler.NewPaymentHandler(paymentService)

	// Setup Gin router
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()

	// Apply global middleware
	router.Use(middleware.RecoveryMiddleware(zapLogger))
	router.Use(middleware.LoggerMiddleware(zapLogger))
	router.Use(middleware.CORSMiddleware())
	router.Use(middleware.RequestIDMiddleware())
	router.Use(middleware.SecurityHeadersMiddleware())

	// Register health check routes
	healthHandler := health.NewHandler(db, "service-payment")
	healthHandler.RegisterRoutes(router)

	// Register payment routes
	apiV1 := router.Group("/api/v1")
	paymentHandler.RegisterRoutes(apiV1, jwtManager)
	promoHandler.RegisterRoutes(apiV1, jwtManager)
	subHandler.RegisterRoutes(apiV1, jwtManager)
	cashOutHandler.RegisterRoutes(apiV1, jwtManager)

	// Register admin handler routes
	adminPaymentHandler := handler.NewAdminPaymentHandler(paymentService, promoService)
	adminPaymentHandler.RegisterRoutes(apiV1, jwtManager)

	// Create HTTP server
	srv := &http.Server{
		Addr:         cfg.Port,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in a goroutine
	go func() {
		zapLogger.Info("HTTP server starting", zap.String("addr", cfg.Port))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			zapLogger.Fatal("HTTP server failed", zap.Error(err))
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	zapLogger.Info("shutting down service-payment...")

	// Cancel Kafka consumer
	consumerCancel()

	// Shutdown HTTP server with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		zapLogger.Error("server forced to shutdown", zap.Error(err))
	}

	zapLogger.Info("service-payment stopped")
}
