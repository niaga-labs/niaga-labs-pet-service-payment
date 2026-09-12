package application

import (
	"context"
	"fmt"
	"time"

	promoDomain "github.com/Kilat-Pet-Delivery/service-payment/internal/domain/promo"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// CreatePromoRequest holds data to create a promo code.
type CreatePromoRequest struct {
	Code             string `json:"code" binding:"required"`
	DiscountType     string `json:"discount_type" binding:"required"`
	DiscountValue    int64  `json:"discount_value" binding:"required"`
	MinAmountCents   int64  `json:"min_amount_cents"`
	MaxDiscountCents int64  `json:"max_discount_cents"`
	MaxUses          int    `json:"max_uses"`
	ValidFrom        string `json:"valid_from" binding:"required"`
	ValidUntil       string `json:"valid_until" binding:"required"`
}

// ValidatePromoRequest holds data to validate a promo code.
type ValidatePromoRequest struct {
	Code        string `json:"code" binding:"required"`
	AmountCents int64  `json:"amount_cents" binding:"required"`
}

// PromoDTO is the API response representation of a promo code.
type PromoDTO struct {
	ID               uuid.UUID `json:"id"`
	Code             string    `json:"code"`
	DiscountType     string    `json:"discount_type"`
	DiscountValue    int64     `json:"discount_value"`
	MinAmountCents   int64     `json:"min_amount_cents"`
	MaxDiscountCents int64     `json:"max_discount_cents"`
	MaxUses          int       `json:"max_uses"`
	CurrentUses      int       `json:"current_uses"`
	ValidFrom        time.Time `json:"valid_from"`
	ValidUntil       time.Time `json:"valid_until"`
	CreatedAt        time.Time `json:"created_at"`
}

// PromoValidationDTO is the result of validating a promo code.
type PromoValidationDTO struct {
	Valid         bool   `json:"valid"`
	Code          string `json:"code"`
	DiscountCents int64  `json:"discount_cents"`
	Message       string `json:"message,omitempty"`
}

// PromoService handles promo code use cases.
type PromoService struct {
	repo   promoDomain.PromoRepository
	logger *zap.Logger
}

// NewPromoService creates a new PromoService.
func NewPromoService(repo promoDomain.PromoRepository, logger *zap.Logger) *PromoService {
	return &PromoService{repo: repo, logger: logger}
}

// CreatePromo creates a new promo code (admin only).
func (s *PromoService) CreatePromo(ctx context.Context, createdBy uuid.UUID, req CreatePromoRequest) (*PromoDTO, error) {
	validFrom, err := time.Parse(time.RFC3339, req.ValidFrom)
	if err != nil {
		return nil, fmt.Errorf("invalid valid_from format (use RFC3339)")
	}
	validUntil, err := time.Parse(time.RFC3339, req.ValidUntil)
	if err != nil {
		return nil, fmt.Errorf("invalid valid_until format (use RFC3339)")
	}

	promo, err := promoDomain.NewPromoCode(
		req.Code,
		promoDomain.DiscountType(req.DiscountType),
		req.DiscountValue,
		req.MinAmountCents,
		req.MaxDiscountCents,
		req.MaxUses,
		validFrom,
		validUntil,
		createdBy,
	)
	if err != nil {
		return nil, err
	}

	if err := s.repo.Save(ctx, promo); err != nil {
		return nil, fmt.Errorf("failed to save promo: %w", err)
	}

	s.logger.Info("promo code created", zap.String("code", promo.Code()))
	return toPromoDTO(promo), nil
}

// ValidatePromo checks if a promo code is valid and calculates the discount.
func (s *PromoService) ValidatePromo(ctx context.Context, userID uuid.UUID, req ValidatePromoRequest) (*PromoValidationDTO, error) {
	promo, err := s.repo.FindByCode(ctx, req.Code)
	if err != nil {
		return &PromoValidationDTO{Valid: false, Code: req.Code, Message: "promo code not found"}, nil
	}

	if !promo.IsValid() {
		return &PromoValidationDTO{Valid: false, Code: req.Code, Message: "promo code is expired or fully used"}, nil
	}

	used, err := s.repo.HasUserUsedPromo(ctx, promo.ID(), userID)
	if err != nil {
		return nil, err
	}
	if used {
		return &PromoValidationDTO{Valid: false, Code: req.Code, Message: "you have already used this promo code"}, nil
	}

	discount, err := promo.CalculateDiscount(req.AmountCents)
	if err != nil {
		return &PromoValidationDTO{Valid: false, Code: req.Code, Message: err.Error()}, nil
	}

	return &PromoValidationDTO{
		Valid:         true,
		Code:          promo.Code(),
		DiscountCents: discount,
	}, nil
}

// GetActivePromos returns all currently active promo codes.
func (s *PromoService) GetActivePromos(ctx context.Context) ([]*PromoDTO, error) {
	promos, err := s.repo.FindActive(ctx)
	if err != nil {
		return nil, err
	}

	dtos := make([]*PromoDTO, len(promos))
	for i, p := range promos {
		dtos[i] = toPromoDTO(p)
	}
	return dtos, nil
}

func toPromoDTO(p *promoDomain.PromoCode) *PromoDTO {
	return &PromoDTO{
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
		CreatedAt:        p.CreatedAt(),
	}
}
