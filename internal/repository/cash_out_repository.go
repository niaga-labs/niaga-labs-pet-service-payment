// Package repository provides GORM-based persistence implementations for the
// payment service domain models.
package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// CashOutModel is the GORM persistence model for the cash_out_requests table.
type CashOutModel struct {
	ID              uuid.UUID  `gorm:"type:uuid;primaryKey"`
	RunnerID        uuid.UUID  `gorm:"type:uuid;not null;index"`
	AmountMyrCents  int64      `gorm:"not null"`
	FeeMyrCents     int64      `gorm:"not null;default:50"`
	DestinationID   uuid.UUID  `gorm:"type:uuid;not null"`
	Status          string     `gorm:"type:varchar(20);not null;default:'pending'"`
	RequestedAt     time.Time  `gorm:"type:timestamptz;not null"`
	CompletedAt     *time.Time `gorm:"type:timestamptz"`
	SimulatedRailID *string    `gorm:"type:varchar(255)"`
}

// TableName specifies the table name for GORM.
func (CashOutModel) TableName() string {
	return "cash_out_requests"
}

// CashOutRepository defines the persistence operations for cash-out requests.
type CashOutRepository interface {
	// Insert persists a new cash-out request row.
	Insert(ctx context.Context, model *CashOutModel) error

	// UpdateStatus transitions the status of a cash-out request and optionally
	// sets the simulated rail ID.
	UpdateStatus(ctx context.Context, id uuid.UUID, status string, railID *string) error

	// MarkCompleted sets status = 'completed' and completed_at = now for the row.
	MarkCompleted(ctx context.Context, id uuid.UUID) error

	// GetAvailableBalanceCents computes the runner's available balance in cents:
	//   released payments - (pending + processing + completed) cash-out requests.
	GetAvailableBalanceCents(ctx context.Context, runnerID uuid.UUID) (int64, error)

	// GetByID retrieves a cash-out request by its primary key.
	// Returns gorm.ErrRecordNotFound when no row matches.
	GetByID(ctx context.Context, id uuid.UUID) (*CashOutModel, error)
}

// GormCashOutRepository is the GORM-backed implementation of CashOutRepository.
type GormCashOutRepository struct {
	db *gorm.DB
}

// NewGormCashOutRepository creates a new GormCashOutRepository.
func NewGormCashOutRepository(db *gorm.DB) *GormCashOutRepository {
	return &GormCashOutRepository{db: db}
}

// Insert persists a new cash-out request row.
func (r *GormCashOutRepository) Insert(ctx context.Context, model *CashOutModel) error {
	return r.db.WithContext(ctx).Create(model).Error
}

// UpdateStatus transitions the status of a cash-out request and optionally
// sets the simulated rail reference.
func (r *GormCashOutRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status string, railID *string) error {
	updates := map[string]interface{}{
		"status": status,
	}
	if railID != nil {
		updates["simulated_rail_id"] = *railID
	}
	return r.db.WithContext(ctx).
		Model(&CashOutModel{}).
		Where("id = ?", id).
		Updates(updates).Error
}

// MarkCompleted sets status = 'completed' and completed_at = now().
func (r *GormCashOutRepository) MarkCompleted(ctx context.Context, id uuid.UUID) error {
	now := time.Now().UTC()
	return r.db.WithContext(ctx).
		Model(&CashOutModel{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":       "completed",
			"completed_at": now,
		}).Error
}

// GetByID retrieves a cash-out request row by its primary key.
// Returns gorm.ErrRecordNotFound when no matching row exists.
func (r *GormCashOutRepository) GetByID(ctx context.Context, id uuid.UUID) (*CashOutModel, error) {
	var model CashOutModel
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&model).Error; err != nil {
		return nil, err
	}
	return &model, nil
}

// GetAvailableBalanceCents computes:
//
//	COALESCE(SUM(payments.runner_payout_cents), 0)
//	  WHERE runner_id = ? AND escrow_status = 'released'
//
//	minus
//
//	COALESCE(SUM(amount_myr_cents + fee_myr_cents), 0)
//	  WHERE runner_id = ? AND status IN ('pending','processing','completed')
func (r *GormCashOutRepository) GetAvailableBalanceCents(ctx context.Context, runnerID uuid.UUID) (int64, error) {
	// Step 1: total released payout for this runner.
	var totalReleased int64
	if err := r.db.WithContext(ctx).
		Model(&PaymentModel{}).
		Where("runner_id = ? AND escrow_status = ?", runnerID, "released").
		Select("COALESCE(SUM(runner_payout_cents), 0)").
		Scan(&totalReleased).Error; err != nil {
		return 0, err
	}

	// Step 2: total already-committed cash-outs (pending, processing, completed).
	var totalCommitted int64
	if err := r.db.WithContext(ctx).
		Model(&CashOutModel{}).
		Where("runner_id = ? AND status IN ?", runnerID, []string{"pending", "processing", "completed"}).
		Select("COALESCE(SUM(amount_myr_cents + fee_myr_cents), 0)").
		Scan(&totalCommitted).Error; err != nil {
		return 0, err
	}

	return totalReleased - totalCommitted, nil
}
