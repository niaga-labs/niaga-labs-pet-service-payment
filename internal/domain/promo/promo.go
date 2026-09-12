package promo

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// DiscountType represents the type of discount.
type DiscountType string

const (
	DiscountTypePercentage DiscountType = "percentage"
	DiscountTypeFixed      DiscountType = "fixed"
)

// PromoCode is the aggregate root for promotional codes.
type PromoCode struct {
	id               uuid.UUID
	code             string
	discountType     DiscountType
	discountValue    int64 // percentage (1-100) or fixed amount in cents
	minAmountCents   int64
	maxDiscountCents int64
	maxUses          int
	currentUses      int
	validFrom        time.Time
	validUntil       time.Time
	createdBy        uuid.UUID
	createdAt        time.Time
	updatedAt        time.Time
}

// NewPromoCode creates a new promo code.
func NewPromoCode(code string, discountType DiscountType, discountValue, minAmountCents, maxDiscountCents int64, maxUses int, validFrom, validUntil time.Time, createdBy uuid.UUID) (*PromoCode, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return nil, fmt.Errorf("promo code is required")
	}
	if discountType != DiscountTypePercentage && discountType != DiscountTypeFixed {
		return nil, fmt.Errorf("invalid discount type: %s", discountType)
	}
	if discountValue <= 0 {
		return nil, fmt.Errorf("discount value must be positive")
	}
	if discountType == DiscountTypePercentage && discountValue > 100 {
		return nil, fmt.Errorf("percentage discount cannot exceed 100")
	}
	if validUntil.Before(validFrom) {
		return nil, fmt.Errorf("valid_until must be after valid_from")
	}

	now := time.Now().UTC()
	return &PromoCode{
		id:               uuid.New(),
		code:             code,
		discountType:     discountType,
		discountValue:    discountValue,
		minAmountCents:   minAmountCents,
		maxDiscountCents: maxDiscountCents,
		maxUses:          maxUses,
		currentUses:      0,
		validFrom:        validFrom,
		validUntil:       validUntil,
		createdBy:        createdBy,
		createdAt:        now,
		updatedAt:        now,
	}, nil
}

// Reconstruct rebuilds a PromoCode from persistence.
func Reconstruct(id uuid.UUID, code string, discountType DiscountType, discountValue, minAmountCents, maxDiscountCents int64, maxUses, currentUses int, validFrom, validUntil time.Time, createdBy uuid.UUID, createdAt, updatedAt time.Time) *PromoCode {
	return &PromoCode{
		id: id, code: code, discountType: discountType, discountValue: discountValue,
		minAmountCents: minAmountCents, maxDiscountCents: maxDiscountCents,
		maxUses: maxUses, currentUses: currentUses,
		validFrom: validFrom, validUntil: validUntil,
		createdBy: createdBy, createdAt: createdAt, updatedAt: updatedAt,
	}
}

// IsValid checks if the promo code is currently valid.
func (p *PromoCode) IsValid() bool {
	now := time.Now().UTC()
	return now.After(p.validFrom) && now.Before(p.validUntil) && (p.maxUses == 0 || p.currentUses < p.maxUses)
}

// CalculateDiscount calculates the discount amount for a given total.
func (p *PromoCode) CalculateDiscount(totalCents int64) (int64, error) {
	if !p.IsValid() {
		return 0, fmt.Errorf("promo code is no longer valid")
	}
	if totalCents < p.minAmountCents {
		return 0, fmt.Errorf("minimum amount of %d cents required", p.minAmountCents)
	}

	var discount int64
	switch p.discountType {
	case DiscountTypePercentage:
		discount = totalCents * p.discountValue / 100
	case DiscountTypeFixed:
		discount = p.discountValue
	}

	if p.maxDiscountCents > 0 && discount > p.maxDiscountCents {
		discount = p.maxDiscountCents
	}
	if discount > totalCents {
		discount = totalCents
	}

	return discount, nil
}

// IncrementUses increments the usage count.
func (p *PromoCode) IncrementUses() {
	p.currentUses++
	p.updatedAt = time.Now().UTC()
}

// Getters.
func (p *PromoCode) ID() uuid.UUID              { return p.id }
func (p *PromoCode) Code() string               { return p.code }
func (p *PromoCode) DiscountType() DiscountType { return p.discountType }
func (p *PromoCode) DiscountValue() int64       { return p.discountValue }
func (p *PromoCode) MinAmountCents() int64      { return p.minAmountCents }
func (p *PromoCode) MaxDiscountCents() int64    { return p.maxDiscountCents }
func (p *PromoCode) MaxUses() int               { return p.maxUses }
func (p *PromoCode) CurrentUses() int           { return p.currentUses }
func (p *PromoCode) ValidFrom() time.Time       { return p.validFrom }
func (p *PromoCode) ValidUntil() time.Time      { return p.validUntil }
func (p *PromoCode) CreatedBy() uuid.UUID       { return p.createdBy }
func (p *PromoCode) CreatedAt() time.Time       { return p.createdAt }
func (p *PromoCode) UpdatedAt() time.Time       { return p.updatedAt }
