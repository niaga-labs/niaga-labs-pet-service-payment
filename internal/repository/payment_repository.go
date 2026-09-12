package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/niaga-labs/niaga-labs-pet-lib-common/domain"
	paymentDomain "github.com/niaga-labs/niaga-labs-pet-service-payment/internal/domain/payment"
	"gorm.io/gorm"
)

// PaymentModel is the GORM persistence model for the payments table.
type PaymentModel struct {
	ID                uuid.UUID  `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	BookingID         uuid.UUID  `gorm:"type:uuid;uniqueIndex;not null"`
	OwnerID           uuid.UUID  `gorm:"type:uuid;not null"`
	RunnerID          *uuid.UUID `gorm:"type:uuid"`
	EscrowStatus      string     `gorm:"type:varchar(20);not null;default:'pending'"`
	AmountCents       int64      `gorm:"not null"`
	PlatformFeeCents  int64      `gorm:"not null"`
	RunnerPayoutCents int64      `gorm:"not null"`
	Currency          string     `gorm:"type:varchar(3);not null;default:'MYR'"`
	PaymentMethod     string     `gorm:"type:varchar(50)"`
	StripePaymentID   string     `gorm:"type:varchar(255)"`
	EscrowHeldAt      *time.Time `gorm:"type:timestamptz"`
	EscrowReleasedAt  *time.Time `gorm:"type:timestamptz"`
	RefundedAt        *time.Time `gorm:"type:timestamptz"`
	RefundReason      string     `gorm:"type:text"`
	Version           int64      `gorm:"not null;default:1"`
	CreatedAt         time.Time  `gorm:"type:timestamptz;not null;default:now()"`
	UpdatedAt         time.Time  `gorm:"type:timestamptz;not null;default:now()"`
}

// TableName specifies the table name for GORM.
func (PaymentModel) TableName() string {
	return "payments"
}

// PaymentRepositoryImpl is the GORM-based implementation of PaymentRepository.
type PaymentRepositoryImpl struct {
	db *gorm.DB
}

// NewPaymentRepository creates a new GORM-based payment repository.
func NewPaymentRepository(db *gorm.DB) *PaymentRepositoryImpl {
	return &PaymentRepositoryImpl{db: db}
}

// FindByID retrieves a payment by its unique ID.
func (r *PaymentRepositoryImpl) FindByID(ctx context.Context, id uuid.UUID) (*paymentDomain.Payment, error) {
	var model PaymentModel
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.NewNotFoundError("Payment", id.String())
		}
		return nil, err
	}
	return toDomain(&model), nil
}

// FindByBookingID retrieves a payment by the associated booking ID.
func (r *PaymentRepositoryImpl) FindByBookingID(ctx context.Context, bookingID uuid.UUID) (*paymentDomain.Payment, error) {
	var model PaymentModel
	if err := r.db.WithContext(ctx).Where("booking_id = ?", bookingID).First(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.NewNotFoundError("Payment", bookingID.String())
		}
		return nil, err
	}
	return toDomain(&model), nil
}

// Save persists a new payment aggregate.
func (r *PaymentRepositoryImpl) Save(ctx context.Context, payment *paymentDomain.Payment) error {
	model := toModel(payment)
	if err := r.db.WithContext(ctx).Create(model).Error; err != nil {
		return err
	}
	return nil
}

// Update persists changes to an existing payment with optimistic locking.
func (r *PaymentRepositoryImpl) Update(ctx context.Context, payment *paymentDomain.Payment) error {
	model := toModel(payment)
	previousVersion := payment.Version() - 1

	result := r.db.WithContext(ctx).
		Model(&PaymentModel{}).
		Where("id = ? AND version = ?", model.ID, previousVersion).
		Updates(model)

	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		return domain.NewConflictError("payment was modified by another transaction")
	}

	return nil
}

// ListAll retrieves all payments with pagination (admin).
func (r *PaymentRepositoryImpl) ListAll(ctx context.Context, page, limit int) ([]*paymentDomain.Payment, int64, error) {
	var total int64
	r.db.WithContext(ctx).Model(&PaymentModel{}).Count(&total)

	var models []PaymentModel
	offset := (page - 1) * limit
	if err := r.db.WithContext(ctx).Order("created_at DESC").Offset(offset).Limit(limit).Find(&models).Error; err != nil {
		return nil, 0, err
	}

	payments := make([]*paymentDomain.Payment, len(models))
	for i := range models {
		payments[i] = toDomain(&models[i])
	}
	return payments, total, nil
}

// GetRevenueStats returns payment statistics (admin).
func (r *PaymentRepositoryImpl) GetRevenueStats(ctx context.Context) (int64, map[string]int64, error) {
	// Total revenue from released escrows
	var totalRevenue int64
	r.db.WithContext(ctx).Model(&PaymentModel{}).
		Where("escrow_status = ?", "released").
		Select("COALESCE(SUM(amount_cents), 0)").
		Scan(&totalRevenue)

	// Count by status
	type statusCount struct {
		EscrowStatus string
		Count        int64
	}
	var results []statusCount
	if err := r.db.WithContext(ctx).Model(&PaymentModel{}).
		Select("escrow_status, count(*) as count").
		Group("escrow_status").
		Find(&results).Error; err != nil {
		return 0, nil, err
	}

	counts := make(map[string]int64)
	for _, sc := range results {
		counts[sc.EscrowStatus] = sc.Count
	}
	return totalRevenue, counts, nil
}

// toDomain maps a PaymentModel to the domain Payment aggregate.
func toDomain(model *PaymentModel) *paymentDomain.Payment {
	return paymentDomain.Reconstitute(
		model.ID,
		model.BookingID,
		model.OwnerID,
		model.RunnerID,
		paymentDomain.EscrowStatus(model.EscrowStatus),
		model.AmountCents,
		model.PlatformFeeCents,
		model.RunnerPayoutCents,
		model.Currency,
		model.PaymentMethod,
		model.StripePaymentID,
		model.EscrowHeldAt,
		model.EscrowReleasedAt,
		model.RefundedAt,
		model.RefundReason,
		model.Version,
		model.CreatedAt,
		model.UpdatedAt,
	)
}

// toModel maps a domain Payment aggregate to a PaymentModel for persistence.
func toModel(p *paymentDomain.Payment) *PaymentModel {
	return &PaymentModel{
		ID:                p.ID(),
		BookingID:         p.BookingID(),
		OwnerID:           p.OwnerID(),
		RunnerID:          p.RunnerID(),
		EscrowStatus:      string(p.EscrowStatus()),
		AmountCents:       p.AmountCents(),
		PlatformFeeCents:  p.PlatformFeeCents(),
		RunnerPayoutCents: p.RunnerPayoutCents(),
		Currency:          p.Currency(),
		PaymentMethod:     p.PaymentMethod(),
		StripePaymentID:   p.StripePaymentID(),
		EscrowHeldAt:      p.EscrowHeldAt(),
		EscrowReleasedAt:  p.EscrowReleasedAt(),
		RefundedAt:        p.RefundedAt(),
		RefundReason:      p.RefundReason(),
		Version:           p.Version(),
		CreatedAt:         p.CreatedAt(),
		UpdatedAt:         p.UpdatedAt(),
	}
}
