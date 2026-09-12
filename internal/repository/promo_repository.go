package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	promoDomain "github.com/niaga-labs/niaga-labs-pet-service-payment/internal/domain/promo"
	"gorm.io/gorm"
)

// PromoModel is the GORM model for the promos table.
type PromoModel struct {
	ID               uuid.UUID `gorm:"type:uuid;primaryKey"`
	Code             string    `gorm:"type:varchar(50);uniqueIndex;not null"`
	DiscountType     string    `gorm:"type:varchar(20);not null"`
	DiscountValue    int64     `gorm:"not null"`
	MinAmountCents   int64     `gorm:"default:0"`
	MaxDiscountCents int64     `gorm:"default:0"`
	MaxUses          int       `gorm:"default:0"`
	CurrentUses      int       `gorm:"default:0"`
	ValidFrom        time.Time `gorm:"not null"`
	ValidUntil       time.Time `gorm:"not null"`
	CreatedBy        uuid.UUID `gorm:"type:uuid;not null"`
	CreatedAt        time.Time `gorm:"not null"`
	UpdatedAt        time.Time `gorm:"not null"`
}

// TableName sets the table name.
func (PromoModel) TableName() string { return "promos" }

// PromoUsageModel is the GORM model for the promo_usages table.
type PromoUsageModel struct {
	ID            uuid.UUID `gorm:"type:uuid;primaryKey"`
	PromoID       uuid.UUID `gorm:"type:uuid;not null;index"`
	UserID        uuid.UUID `gorm:"type:uuid;not null;index"`
	BookingID     uuid.UUID `gorm:"type:uuid;not null"`
	DiscountCents int64     `gorm:"not null"`
	UsedAt        time.Time `gorm:"not null"`
}

// TableName sets the table name.
func (PromoUsageModel) TableName() string { return "promo_usages" }

// GormPromoRepository implements PromoRepository using GORM.
type GormPromoRepository struct {
	db *gorm.DB
}

// NewGormPromoRepository creates a new GormPromoRepository.
func NewGormPromoRepository(db *gorm.DB) *GormPromoRepository {
	return &GormPromoRepository{db: db}
}

// Save persists a new promo code.
func (r *GormPromoRepository) Save(ctx context.Context, p *promoDomain.PromoCode) error {
	model := toPromoModel(p)
	return r.db.WithContext(ctx).Create(&model).Error
}

// Update updates a promo code.
func (r *GormPromoRepository) Update(ctx context.Context, p *promoDomain.PromoCode) error {
	model := toPromoModel(p)
	return r.db.WithContext(ctx).Save(&model).Error
}

// FindByCode returns a promo code by its code string.
func (r *GormPromoRepository) FindByCode(ctx context.Context, code string) (*promoDomain.PromoCode, error) {
	var model PromoModel
	if err := r.db.WithContext(ctx).Where("code = ?", code).First(&model).Error; err != nil {
		return nil, err
	}
	return toPromoDomain(&model), nil
}

// FindByID returns a promo code by ID.
func (r *GormPromoRepository) FindByID(ctx context.Context, id uuid.UUID) (*promoDomain.PromoCode, error) {
	var model PromoModel
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&model).Error; err != nil {
		return nil, err
	}
	return toPromoDomain(&model), nil
}

// FindActive returns all currently active promo codes.
func (r *GormPromoRepository) FindActive(ctx context.Context) ([]*promoDomain.PromoCode, error) {
	var models []PromoModel
	now := time.Now().UTC()
	if err := r.db.WithContext(ctx).
		Where("valid_from <= ? AND valid_until >= ?", now, now).
		Where("max_uses = 0 OR current_uses < max_uses").
		Find(&models).Error; err != nil {
		return nil, err
	}

	promos := make([]*promoDomain.PromoCode, len(models))
	for i, m := range models {
		promos[i] = toPromoDomain(&m)
	}
	return promos, nil
}

// SaveUsage persists a promo usage record.
func (r *GormPromoRepository) SaveUsage(ctx context.Context, usage *promoDomain.PromoUsage) error {
	model := PromoUsageModel{
		ID:            usage.ID,
		PromoID:       usage.PromoID,
		UserID:        usage.UserID,
		BookingID:     usage.BookingID,
		DiscountCents: usage.DiscountCents,
		UsedAt:        usage.UsedAt,
	}
	return r.db.WithContext(ctx).Create(&model).Error
}

// HasUserUsedPromo checks if a user has already used a specific promo.
func (r *GormPromoRepository) HasUserUsedPromo(ctx context.Context, promoID, userID uuid.UUID) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&PromoUsageModel{}).
		Where("promo_id = ? AND user_id = ?", promoID, userID).
		Count(&count).Error
	return count > 0, err
}

func toPromoModel(p *promoDomain.PromoCode) PromoModel {
	return PromoModel{
		ID:               p.ID(),
		Code:             p.Code(),
		DiscountType:     string(p.DiscountType()),
		DiscountValue:    p.DiscountValue(),
		MinAmountCents:   p.MinAmountCents(),
		MaxDiscountCents: p.MaxDiscountCents(),
		MaxUses:          p.MaxUses(),
		CurrentUses:      p.CurrentUses(),
		ValidFrom:        p.ValidFrom(),
		ValidUntil:       p.ValidUntil(),
		CreatedBy:        p.CreatedBy(),
		CreatedAt:        p.CreatedAt(),
		UpdatedAt:        p.UpdatedAt(),
	}
}

func toPromoDomain(m *PromoModel) *promoDomain.PromoCode {
	return promoDomain.Reconstruct(
		m.ID, m.Code, promoDomain.DiscountType(m.DiscountType),
		m.DiscountValue, m.MinAmountCents, m.MaxDiscountCents,
		m.MaxUses, m.CurrentUses,
		m.ValidFrom, m.ValidUntil, m.CreatedBy,
		m.CreatedAt, m.UpdatedAt,
	)
}
