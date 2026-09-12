//go:build integration

// Package handler contains integration tests for the cash-out handler.
// These tests require a live PostgreSQL instance via testcontainers.
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/niaga-labs/niaga-labs-pet-lib-common/auth"
	"github.com/niaga-labs/niaga-labs-pet-lib-common/middleware"
	"github.com/niaga-labs/niaga-labs-pet-lib-proto/dto"
	"github.com/niaga-labs/niaga-labs-pet-service-payment/internal/adapter"
	"github.com/niaga-labs/niaga-labs-pet-service-payment/internal/rail"
	"github.com/niaga-labs/niaga-labs-pet-service-payment/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// setupIntegrationDB starts a PostgreSQL testcontainer and migrates required tables.
func setupIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	ctx := context.Background()

	pgReq := testcontainers.ContainerRequest{
		Image:        "postgis/postgis:16-3.4-alpine",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "test",
			"POSTGRES_PASSWORD": "test",
			"POSTGRES_DB":       "test_handler",
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
	t.Cleanup(func() { _ = pgContainer.Terminate(ctx) })

	pgHost, err := pgContainer.Host(ctx)
	require.NoError(t, err)
	pgPort, err := pgContainer.MappedPort(ctx, "5432")
	require.NoError(t, err)

	dsn := fmt.Sprintf("host=%s port=%s user=test password=test dbname=test_handler sslmode=disable", pgHost, pgPort.Port())

	var db *gorm.DB
	require.Eventually(t, func() bool {
		var connErr error
		db, connErr = gorm.Open(postgres.Open(dsn), &gorm.Config{})
		if connErr != nil {
			return false
		}
		sqlDB, pingErr := db.DB()
		if pingErr != nil {
			return false
		}
		return sqlDB.Ping() == nil
	}, 30*time.Second, 1*time.Second, "PostgreSQL not ready")

	require.NoError(t, db.Exec(`CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`).Error)
	require.NoError(t, db.AutoMigrate(&repository.PaymentModel{}, &repository.CashOutModel{}))

	return db
}

// seedReleasedPayment inserts a released payment so the runner has available balance.
func seedReleasedPayment(t *testing.T, db *gorm.DB, runnerID uuid.UUID, payoutCents int64) {
	t.Helper()
	now := time.Now().UTC()
	m := repository.PaymentModel{
		ID:                uuid.New(),
		BookingID:         uuid.New(),
		OwnerID:           uuid.New(),
		RunnerID:          &runnerID,
		EscrowStatus:      "released",
		AmountCents:       payoutCents + 1000,
		PlatformFeeCents:  1000,
		RunnerPayoutCents: payoutCents,
		Currency:          "MYR",
		Version:           2,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	require.NoError(t, db.Create(&m).Error)
}

// buildIntegrationRouter returns a Gin engine with auth context injected (no real JWT).
func buildIntegrationRouter(h *CashOutHandler, runnerID uuid.UUID) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.ContextKeyUserID, runnerID)
		c.Set(middleware.ContextKeyRole, auth.RoleRunner)
		c.Next()
	})
	apiV1 := r.Group("/api/v1")
	apiV1.POST("/payouts/cash-out", h.CashOut)
	return r
}

// TestCashOut_ValidRequest_Returns202 is an integration test that seeds a runner
// balance via released payments, hits the endpoint, and asserts:
// - HTTP 202 with valid body shape
// - A pending cash_out_requests row inserted in the DB.
func TestCashOut_ValidRequest_Returns202(t *testing.T) {
	db := setupIntegrationDB(t)
	logger, _ := zap.NewDevelopment()

	runnerID := uuid.New()
	destID := uuid.New()

	// Seed 500 MYR available (50000 cents).
	seedReleasedPayment(t, db, runnerID, 50000)

	cashOutRepo := repository.NewGormCashOutRepository(db)
	ownership := adapter.NewInMemoryDestinationOwnership(map[uuid.UUID]uuid.UUID{
		destID: runnerID,
	})
	simulatedRail := rail.NewSimulatedRail(200*time.Millisecond, logger, rail.RealClock{})
	h := NewCashOutHandler(cashOutRepo, ownership, simulatedRail, 200*time.Millisecond, logger)
	r := buildIntegrationRouter(h, runnerID)

	body, _ := json.Marshal(dto.CashOutRequest{
		AmountMyrCents: 1000,
		DestinationID:  destID.String(),
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/payouts/cash-out", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusAccepted, w.Code, "expected 202 Accepted, got: %d — body: %s", w.Code, w.Body.String())

	var resp dto.CashOutResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp.CashOutID)
	assert.Equal(t, 1, resp.EtaMinutes) // ceil(0.2/60) = 1

	// Verify DB row inserted.
	cashOutID, err := uuid.Parse(resp.CashOutID)
	require.NoError(t, err)
	var row repository.CashOutModel
	require.NoError(t, db.Where("id = ?", cashOutID).First(&row).Error)
	assert.Equal(t, runnerID, row.RunnerID)
	assert.Equal(t, int64(1000), row.AmountMyrCents)
	assert.Equal(t, int64(50), row.FeeMyrCents)
}

// TestCashOut_FlowsThroughToSimulatedRail_EventuallyCompletes verifies the
// end-to-end async rail flow: after the handler returns 202, the DB row
// eventually transitions to 'completed' with simulated_rail_id populated.
func TestCashOut_FlowsThroughToSimulatedRail_EventuallyCompletes(t *testing.T) {
	db := setupIntegrationDB(t)
	logger, _ := zap.NewDevelopment()

	runnerID := uuid.New()
	destID := uuid.New()

	// Seed 500 MYR available.
	seedReleasedPayment(t, db, runnerID, 50000)

	cashOutRepo := repository.NewGormCashOutRepository(db)
	ownership := adapter.NewInMemoryDestinationOwnership(map[uuid.UUID]uuid.UUID{
		destID: runnerID,
	})
	// Very short delay so the test completes quickly.
	railDelay := 200 * time.Millisecond
	simulatedRail := rail.NewSimulatedRail(railDelay, logger, rail.RealClock{})
	h := NewCashOutHandler(cashOutRepo, ownership, simulatedRail, railDelay, logger)
	r := buildIntegrationRouter(h, runnerID)

	body, _ := json.Marshal(dto.CashOutRequest{
		AmountMyrCents: 1000,
		DestinationID:  destID.String(),
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/payouts/cash-out", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusAccepted, w.Code)

	var resp dto.CashOutResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	cashOutID, err := uuid.Parse(resp.CashOutID)
	require.NoError(t, err)

	// Poll until the row transitions to 'completed' (up to 5 seconds).
	assert.Eventually(t, func() bool {
		var row repository.CashOutModel
		if dbErr := db.Where("id = ?", cashOutID).First(&row).Error; dbErr != nil {
			return false
		}
		return row.Status == "completed"
	}, 5*time.Second, 200*time.Millisecond, "cash-out row did not reach 'completed' within timeout")

	// Also assert simulated_rail_id is populated.
	var finalRow repository.CashOutModel
	require.NoError(t, db.Where("id = ?", cashOutID).First(&finalRow).Error)
	require.NotNil(t, finalRow.SimulatedRailID, "simulated_rail_id should be set after rail submission")
	assert.Contains(t, *finalRow.SimulatedRailID, "DN-", "txRef should follow DuitNow format")
	assert.NotNil(t, finalRow.CompletedAt, "completed_at should be set")
}
