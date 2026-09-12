//go:build integration

package main_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/niaga-labs/niaga-labs-pet-lib-common/kafka"
	"github.com/niaga-labs/niaga-labs-pet-service-payment/internal/adapter"
	"github.com/niaga-labs/niaga-labs-pet-service-payment/internal/application"
	paymentEvents "github.com/niaga-labs/niaga-labs-pet-service-payment/internal/events"
	"github.com/niaga-labs/niaga-labs-pet-service-payment/internal/repository"
	"github.com/niaga-labs/niaga-labs-pet-service-payment/internal/saga"
	"net"

	"github.com/google/uuid"
	kafkago "github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	kafkamodule "github.com/testcontainers/testcontainers-go/modules/kafka"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// testInfra holds shared test infrastructure.
type testInfra struct {
	DB           *gorm.DB
	KafkaBrokers []string
	Cleanup      func()
}

// paymentStack holds wired-up payment service components.
type paymentStack struct {
	Service         *application.PaymentService
	Consumer        *paymentEvents.BookingEventConsumer
	CleanupProducer func()
}

// setupContainers starts PostgreSQL and Kafka testcontainers and returns a connected GORM DB.
func setupContainers(t *testing.T) *testInfra {
	t.Helper()
	ctx := context.Background()

	// Start PostgreSQL (PostGIS) container with log-based wait strategy.
	pgReq := testcontainers.ContainerRequest{
		Image:        "postgis/postgis:16-3.4-alpine",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "test",
			"POSTGRES_PASSWORD": "test",
			"POSTGRES_DB":       "test_payment",
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).
			WithStartupTimeout(60 * time.Second),
	}
	pgContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: pgReq,
		Started:          true,
	})
	require.NoError(t, err, "failed to start PostgreSQL container")

	pgHost, err := pgContainer.Host(ctx)
	require.NoError(t, err)
	pgPort, err := pgContainer.MappedPort(ctx, "5432")
	require.NoError(t, err)

	dsn := fmt.Sprintf("host=%s port=%s user=test password=test dbname=test_payment sslmode=disable", pgHost, pgPort.Port())

	// Poll until GORM can actually connect and ping.
	var db *gorm.DB
	require.Eventually(t, func() bool {
		var err error
		db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
		if err != nil {
			return false
		}
		sqlDB, err := db.DB()
		if err != nil {
			return false
		}
		return sqlDB.Ping() == nil
	}, 30*time.Second, 1*time.Second, "PostgreSQL not ready for connections")

	// Enable uuid-ossp extension and auto-migrate.
	require.NoError(t, db.Exec(`CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`).Error)
	require.NoError(t, db.AutoMigrate(&repository.PaymentModel{}))

	// Start Kafka container using confluent-local (supports KRaft natively).
	kafkaContainer, err := kafkamodule.Run(ctx, "confluentinc/confluent-local:7.5.0")
	require.NoError(t, err, "failed to start Kafka container")

	kafkaBrokers, err := kafkaContainer.Brokers(ctx)
	require.NoError(t, err, "failed to get Kafka brokers")

	// Pre-create required topics.
	createTopics(t, kafkaBrokers, "booking.events", "payment.events")

	cleanup := func() {
		if err := kafkaContainer.Terminate(ctx); err != nil {
			t.Logf("failed to terminate Kafka container: %v", err)
		}
		if err := pgContainer.Terminate(ctx); err != nil {
			t.Logf("failed to terminate PostgreSQL container: %v", err)
		}
	}

	return &testInfra{
		DB:           db,
		KafkaBrokers: kafkaBrokers,
		Cleanup:      cleanup,
	}
}

// setupPaymentStack wires up the full payment service stack.
func setupPaymentStack(t *testing.T, db *gorm.DB, brokers []string) *paymentStack {
	t.Helper()
	logger, _ := zap.NewDevelopment()

	paymentRepo := repository.NewPaymentRepository(db)
	mockStripe := adapter.NewMockStripeAdapter(logger)
	producer := kafka.NewProducer(brokers, logger)
	sagaSvc := saga.NewPaymentSagaService(paymentRepo, mockStripe, producer, 15.0, logger)
	paymentSvc := application.NewPaymentService(paymentRepo, sagaSvc, logger)

	groupID := fmt.Sprintf("test-payment-%s", uuid.New().String()[:8])
	consumer := paymentEvents.NewBookingEventConsumer(brokers, groupID, paymentSvc, logger)

	return &paymentStack{
		Service:         paymentSvc,
		Consumer:        consumer,
		CleanupProducer: func() { _ = producer.Close() },
	}
}

// seedPaymentInHeldState inserts a payment in "held" state for testing.
func seedPaymentInHeldState(t *testing.T, db *gorm.DB, bookingID, ownerID uuid.UUID) uuid.UUID {
	t.Helper()
	paymentID := uuid.New()
	now := time.Now().UTC()
	model := repository.PaymentModel{
		ID:                paymentID,
		BookingID:         bookingID,
		OwnerID:           ownerID,
		EscrowStatus:      "held",
		AmountCents:       150000,
		PlatformFeeCents:  22500,
		RunnerPayoutCents: 127500,
		Currency:          "MYR",
		StripePaymentID:   fmt.Sprintf("pi_mock_%s", uuid.New().String()[:8]),
		EscrowHeldAt:      &now,
		Version:           2,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	require.NoError(t, db.Create(&model).Error, "failed to seed payment")
	return paymentID
}

// seedPaymentInPendingState inserts a payment in "pending" state for testing.
func seedPaymentInPendingState(t *testing.T, db *gorm.DB, bookingID, ownerID uuid.UUID) uuid.UUID {
	t.Helper()
	paymentID := uuid.New()
	now := time.Now().UTC()
	model := repository.PaymentModel{
		ID:                paymentID,
		BookingID:         bookingID,
		OwnerID:           ownerID,
		EscrowStatus:      "pending",
		AmountCents:       150000,
		PlatformFeeCents:  22500,
		RunnerPayoutCents: 127500,
		Currency:          "MYR",
		Version:           1,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	require.NoError(t, db.Create(&model).Error, "failed to seed payment")
	return paymentID
}

// publishTestEvent publishes a CloudEvent to Kafka.
func publishTestEvent(t *testing.T, brokers []string, topic, source, eventType string, data interface{}) {
	t.Helper()
	logger, _ := zap.NewDevelopment()
	producer := kafka.NewProducer(brokers, logger)
	defer func() { _ = producer.Close() }()

	ce, err := kafka.NewCloudEvent(source, eventType, data)
	require.NoError(t, err, "failed to create cloud event")

	err = producer.PublishEvent(context.Background(), topic, ce)
	require.NoError(t, err, "failed to publish event")
}

// waitForDBStatus polls the payments table until the escrow_status matches.
func waitForDBStatus(t *testing.T, db *gorm.DB, bookingID uuid.UUID, expectedStatus string, timeout time.Duration) repository.PaymentModel {
	t.Helper()
	var result repository.PaymentModel
	require.Eventually(t, func() bool {
		var model repository.PaymentModel
		err := db.Where("booking_id = ?", bookingID).First(&model).Error
		if err != nil {
			return false
		}
		if model.EscrowStatus == expectedStatus {
			result = model
			return true
		}
		return false
	}, timeout, 200*time.Millisecond, "payment did not transition to %s", expectedStatus)
	return result
}

// consumeOneEvent reads from a Kafka topic until it finds an event of the expected type.
func consumeOneEvent(t *testing.T, brokers []string, topic, expectedType string, timeout time.Duration) kafka.CloudEvent {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	groupID := fmt.Sprintf("test-assert-%s", uuid.New().String()[:8])
	reader := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:     brokers,
		GroupID:     groupID,
		Topic:       topic,
		MinBytes:    1,
		MaxBytes:    10e6,
		StartOffset: kafkago.FirstOffset,
	})
	defer func() { _ = reader.Close() }()

	for {
		msg, err := reader.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				t.Fatalf("timed out waiting for event type %q on topic %q", expectedType, topic)
			}
			continue
		}
		ce, err := kafka.ParseCloudEvent(msg.Value)
		if err != nil {
			continue
		}
		if ce.Type == expectedType {
			return ce
		}
	}
}

// createTopics pre-creates Kafka topics so producers don't fail with "Unknown Topic".
func createTopics(t *testing.T, brokers []string, topics ...string) {
	t.Helper()
	conn, err := kafkago.Dial("tcp", brokers[0])
	require.NoError(t, err, "failed to dial Kafka for topic creation")
	defer conn.Close()

	controller, err := conn.Controller()
	require.NoError(t, err, "failed to get Kafka controller")

	controllerConn, err := kafkago.Dial("tcp", net.JoinHostPort(controller.Host, fmt.Sprintf("%d", controller.Port)))
	require.NoError(t, err, "failed to connect to Kafka controller")
	defer controllerConn.Close()

	topicConfigs := make([]kafkago.TopicConfig, len(topics))
	for i, topic := range topics {
		topicConfigs[i] = kafkago.TopicConfig{
			Topic:             topic,
			NumPartitions:     1,
			ReplicationFactor: 1,
		}
	}
	err = controllerConn.CreateTopics(topicConfigs...)
	require.NoError(t, err, "failed to create Kafka topics")

	// Give Kafka a moment to propagate topic metadata.
	time.Sleep(1 * time.Second)
}
