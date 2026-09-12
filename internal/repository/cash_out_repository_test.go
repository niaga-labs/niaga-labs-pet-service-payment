//go:build integration

// Package repository contains integration tests for the cash-out repository.
// These tests require a live PostgreSQL instance (started via testcontainers).
package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// setupRepoTestDB starts a PostgreSQL testcontainer, runs uuid-ossp extension
// and auto-migrates the models required for cash-out repo tests.
func setupRepoTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	ctx := context.Background()

	pgReq := testcontainers.ContainerRequest{
		Image:        "postgis/postgis:16-3.4-alpine",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "test",
			"POSTGRES_PASSWORD": "test",
			"POSTGRES_DB":       "test_repo",
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

	dsn := fmt.Sprintf("host=%s port=%s user=test password=test dbname=test_repo sslmode=disable", pgHost, pgPort.Port())

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
	require.NoError(t, db.AutoMigrate(&PaymentModel{}, &CashOutModel{}))

	return db
}

// TestCashOutRepo_InsertAndGet verifies round-trip insert + status retrieval.
func TestCashOutRepo_InsertAndGet(t *testing.T) {
	db := setupRepoTestDB(t)
	repo := NewGormCashOutRepository(db)
	ctx := context.Background()

	runnerID := uuid.New()
	destID := uuid.New()
	id := uuid.New()
	now := time.Now().UTC().Truncate(time.Microsecond)

	model := &CashOutModel{
		ID:             id,
		RunnerID:       runnerID,
		AmountMyrCents: 5000,
		FeeMyrCents:    50,
		DestinationID:  destID,
		Status:         "pending",
		RequestedAt:    now,
	}
	require.NoError(t, repo.Insert(ctx, model))

	var fetched CashOutModel
	require.NoError(t, db.Where("id = ?", id).First(&fetched).Error)
	assert.Equal(t, "pending", fetched.Status)
	assert.Equal(t, int64(5000), fetched.AmountMyrCents)
	assert.Equal(t, int64(50), fetched.FeeMyrCents)
	assert.Nil(t, fetched.SimulatedRailID)
}

// TestCashOutRepo_GetAvailableBalance_OnlyReleasedPayments verifies that only
// payments with escrow_status = 'released' count toward the runner's balance.
func TestCashOutRepo_GetAvailableBalance_OnlyReleasedPayments(t *testing.T) {
	db := setupRepoTestDB(t)
	repo := NewGormCashOutRepository(db)
	ctx := context.Background()

	runnerID := uuid.New()

	seedPayment := func(status string, payoutCents int64) {
		now := time.Now().UTC()
		m := PaymentModel{
			ID:                uuid.New(),
			BookingID:         uuid.New(),
			OwnerID:           uuid.New(),
			RunnerID:          &runnerID,
			EscrowStatus:      status,
			AmountCents:       payoutCents + 1000,
			PlatformFeeCents:  1000,
			RunnerPayoutCents: payoutCents,
			Currency:          "MYR",
			Version:           1,
			CreatedAt:         now,
			UpdatedAt:         now,
		}
		require.NoError(t, db.Create(&m).Error)
	}

	seedPayment("released", 10000) // should count
	seedPayment("released", 5000)  // should count
	seedPayment("held", 99999)     // should NOT count
	seedPayment("pending", 99999)  // should NOT count
	seedPayment("refunded", 99999) // should NOT count

	balance, err := repo.GetAvailableBalanceCents(ctx, runnerID)
	require.NoError(t, err)
	assert.Equal(t, int64(15000), balance, "only released payments should contribute to balance")
}

// TestCashOutRepo_GetAvailableBalance_DeductsOpenCashOuts verifies that
// pending/processing/completed cash-outs reduce the balance, but failed do not.
func TestCashOutRepo_GetAvailableBalance_DeductsOpenCashOuts(t *testing.T) {
	db := setupRepoTestDB(t)
	repo := NewGormCashOutRepository(db)
	ctx := context.Background()

	runnerID := uuid.New()

	// Seed released payment of 50000 cents.
	now := time.Now().UTC()
	payment := PaymentModel{
		ID:                uuid.New(),
		BookingID:         uuid.New(),
		OwnerID:           uuid.New(),
		RunnerID:          &runnerID,
		EscrowStatus:      "released",
		AmountCents:       60000,
		PlatformFeeCents:  10000,
		RunnerPayoutCents: 50000,
		Currency:          "MYR",
		Version:           1,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	require.NoError(t, db.Create(&payment).Error)

	seedCashOut := func(status string, amountCents, feeCents int64) {
		m := CashOutModel{
			ID:             uuid.New(),
			RunnerID:       runnerID,
			AmountMyrCents: amountCents,
			FeeMyrCents:    feeCents,
			DestinationID:  uuid.New(),
			Status:         status,
			RequestedAt:    now,
		}
		require.NoError(t, db.Create(&m).Error)
	}

	seedCashOut("pending", 5000, 50)    // deduct 5050
	seedCashOut("processing", 3000, 50) // deduct 3050
	seedCashOut("completed", 2000, 50)  // deduct 2050
	seedCashOut("failed", 99000, 50)    // should NOT deduct

	// Expected: 50000 - (5050 + 3050 + 2050) = 50000 - 10150 = 39850
	balance, err := repo.GetAvailableBalanceCents(ctx, runnerID)
	require.NoError(t, err)
	assert.Equal(t, int64(39850), balance)
}

// TestCashOutRepo_UpdateStatus_SetsRailID verifies that UpdateStatus correctly
// transitions status and persists the rail reference.
func TestCashOutRepo_UpdateStatus_SetsRailID(t *testing.T) {
	db := setupRepoTestDB(t)
	repo := NewGormCashOutRepository(db)
	ctx := context.Background()

	id := uuid.New()
	runnerID := uuid.New()
	now := time.Now().UTC()
	model := &CashOutModel{
		ID:             id,
		RunnerID:       runnerID,
		AmountMyrCents: 1000,
		FeeMyrCents:    50,
		DestinationID:  uuid.New(),
		Status:         "pending",
		RequestedAt:    now,
	}
	require.NoError(t, repo.Insert(ctx, model))

	railRef := "DN-test-ref"
	require.NoError(t, repo.UpdateStatus(ctx, id, "processing", &railRef))

	var updated CashOutModel
	require.NoError(t, db.Where("id = ?", id).First(&updated).Error)
	assert.Equal(t, "processing", updated.Status)
	require.NotNil(t, updated.SimulatedRailID)
	assert.Equal(t, "DN-test-ref", *updated.SimulatedRailID)
}

// TestCashOutRepo_GetByID_RoundTrip inserts a row and verifies GetByID returns
// the same fields.
func TestCashOutRepo_GetByID_RoundTrip(t *testing.T) {
	db := setupRepoTestDB(t)
	repo := NewGormCashOutRepository(db)
	ctx := context.Background()

	id := uuid.New()
	runnerID := uuid.New()
	destID := uuid.New()
	now := time.Now().UTC().Truncate(time.Microsecond)

	model := &CashOutModel{
		ID:             id,
		RunnerID:       runnerID,
		AmountMyrCents: 7500,
		FeeMyrCents:    50,
		DestinationID:  destID,
		Status:         "pending",
		RequestedAt:    now,
	}
	require.NoError(t, repo.Insert(ctx, model))

	fetched, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, fetched)
	assert.Equal(t, id, fetched.ID)
	assert.Equal(t, runnerID, fetched.RunnerID)
	assert.Equal(t, destID, fetched.DestinationID)
	assert.Equal(t, int64(7500), fetched.AmountMyrCents)
	assert.Equal(t, int64(50), fetched.FeeMyrCents)
	assert.Equal(t, "pending", fetched.Status)
}

// TestCashOutRepo_GetByID_NotFound verifies that GetByID returns an error when
// no row matches the given UUID.
func TestCashOutRepo_GetByID_NotFound(t *testing.T) {
	db := setupRepoTestDB(t)
	repo := NewGormCashOutRepository(db)
	ctx := context.Background()

	randomID := uuid.New()
	fetched, err := repo.GetByID(ctx, randomID)
	require.Error(t, err)
	assert.Nil(t, fetched)
}

// TestCashOutRepo_MarkCompleted sets status = completed and completed_at.
func TestCashOutRepo_MarkCompleted(t *testing.T) {
	db := setupRepoTestDB(t)
	repo := NewGormCashOutRepository(db)
	ctx := context.Background()

	id := uuid.New()
	runnerID := uuid.New()
	now := time.Now().UTC()
	model := &CashOutModel{
		ID:             id,
		RunnerID:       runnerID,
		AmountMyrCents: 1000,
		FeeMyrCents:    50,
		DestinationID:  uuid.New(),
		Status:         "processing",
		RequestedAt:    now,
	}
	require.NoError(t, repo.Insert(ctx, model))

	require.NoError(t, repo.MarkCompleted(ctx, id))

	var completed CashOutModel
	require.NoError(t, db.Where("id = ?", id).First(&completed).Error)
	assert.Equal(t, "completed", completed.Status)
	require.NotNil(t, completed.CompletedAt)
	assert.WithinDuration(t, time.Now().UTC(), *completed.CompletedAt, 5*time.Second)
}
