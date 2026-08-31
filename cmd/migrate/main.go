// Command migrate applies the SQL migrations in ./migrations to the payment
// database and exits.
//
// Run it from the repository root, because the migration source is resolved
// relative to the working directory:
//
//	go run ./cmd/migrate
package main

import (
	"log"

	"github.com/Kilat-Pet-Delivery/lib-common/database"
	"github.com/Kilat-Pet-Delivery/lib-common/logger"
	svcconfig "github.com/Kilat-Pet-Delivery/service-payment/internal/config"
	"go.uber.org/zap"
)

func main() {
	cfg, err := svcconfig.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	zapLogger, err := logger.NewNamed(cfg.AppEnv, "service-payment-migrate")
	if err != nil {
		log.Fatalf("failed to initialize logger: %v", err)
	}
	defer func() { _ = zapLogger.Sync() }()

	dbConfig := database.PostgresConfig{
		Host:     cfg.DBConfig.Host,
		Port:     cfg.DBConfig.Port,
		User:     cfg.DBConfig.User,
		Password: cfg.DBConfig.Password,
		DBName:   cfg.DBConfig.DBName,
		SSLMode:  cfg.DBConfig.SSLMode,
	}

	if err := database.RunMigrations(dbConfig.DatabaseURL(), "migrations", zapLogger); err != nil {
		zapLogger.Fatal("failed to run migrations", zap.Error(err))
	}
}
