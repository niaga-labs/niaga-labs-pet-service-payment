package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	subDomain "github.com/niaga-labs/niaga-labs-pet-service-payment/internal/domain/subscription"
	"gorm.io/gorm"
)

// SubscriptionModel is the GORM model for the subscriptions table.
type SubscriptionModel struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey"`
	UserID     uuid.UUID `gorm:"type:uuid;not null;index"`
	Plan       string    `gorm:"type:varchar(20);not null"`
	PriceCents int64     `gorm:"not null"`
	StartedAt  time.Time `gorm:"not null"`
	ExpiresAt  time.Time `gorm:"not null"`
	Status     string    `gorm:"type:varchar(20);not null;default:'active'"`
	AutoRenew  bool      `gorm:"default:true"`
	CreatedAt  time.Time `gorm:"not null"`
	UpdatedAt  time.Time `gorm:"not null"`
}

// TableName sets the table name.
func (SubscriptionModel) TableName() string { return "subscriptions" }

// GormSubscriptionRepository implements SubscriptionRepository using GORM.
type GormSubscriptionRepository struct {
	db *gorm.DB
}

// NewGormSubscriptionRepository creates a new GormSubscriptionRepository.
func NewGormSubscriptionRepository(db *gorm.DB) *GormSubscriptionRepository {
	return &GormSubscriptionRepository{db: db}
}

// Save persists a new subscription.
func (r *GormSubscriptionRepository) Save(ctx context.Context, s *subDomain.Subscription) error {
	model := toSubModel(s)
	return r.db.WithContext(ctx).Create(&model).Error
}

// Update updates a subscription.
func (r *GormSubscriptionRepository) Update(ctx context.Context, s *subDomain.Subscription) error {
	model := toSubModel(s)
	return r.db.WithContext(ctx).Save(&model).Error
}

// FindActiveByUserID returns the active subscription for a user.
func (r *GormSubscriptionRepository) FindActiveByUserID(ctx context.Context, userID uuid.UUID) (*subDomain.Subscription, error) {
	var model SubscriptionModel
	now := time.Now().UTC()
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND status = ? AND expires_at > ?", userID, "active", now).
		Order("created_at DESC").
		First(&model).Error; err != nil {
		return nil, err
	}
	return toSubDomain(&model), nil
}

// FindByID returns a subscription by ID.
func (r *GormSubscriptionRepository) FindByID(ctx context.Context, id uuid.UUID) (*subDomain.Subscription, error) {
	var model SubscriptionModel
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&model).Error; err != nil {
		return nil, err
	}
	return toSubDomain(&model), nil
}

func toSubModel(s *subDomain.Subscription) SubscriptionModel {
	return SubscriptionModel{
		ID: s.ID(), UserID: s.UserID(), Plan: string(s.Plan()),
		PriceCents: s.PriceCents(), StartedAt: s.StartedAt(), ExpiresAt: s.ExpiresAt(),
		Status: string(s.Status()), AutoRenew: s.AutoRenew(),
		CreatedAt: s.CreatedAt(), UpdatedAt: s.UpdatedAt(),
	}
}

func toSubDomain(m *SubscriptionModel) *subDomain.Subscription {
	return subDomain.Reconstruct(
		m.ID, m.UserID, subDomain.PlanType(m.Plan), m.PriceCents,
		m.StartedAt, m.ExpiresAt, subDomain.SubStatus(m.Status), m.AutoRenew,
		m.CreatedAt, m.UpdatedAt,
	)
}
