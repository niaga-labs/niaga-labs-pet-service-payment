package application

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	subDomain "github.com/niaga-labs/niaga-labs-pet-service-payment/internal/domain/subscription"
	"go.uber.org/zap"
)

// SubscriptionDTO is the API response for a subscription.
type SubscriptionDTO struct {
	ID         uuid.UUID `json:"id"`
	UserID     uuid.UUID `json:"user_id"`
	Plan       string    `json:"plan"`
	PriceCents int64     `json:"price_cents"`
	StartedAt  time.Time `json:"started_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	Status     string    `json:"status"`
	AutoRenew  bool      `json:"auto_renew"`
	CreatedAt  time.Time `json:"created_at"`
}

// SubscribeRequest holds data to create a subscription.
type SubscribeRequest struct {
	Plan string `json:"plan" binding:"required"`
}

// SubscriptionService handles subscription use cases.
type SubscriptionService struct {
	repo   subDomain.SubscriptionRepository
	logger *zap.Logger
}

// NewSubscriptionService creates a new SubscriptionService.
func NewSubscriptionService(repo subDomain.SubscriptionRepository, logger *zap.Logger) *SubscriptionService {
	return &SubscriptionService{repo: repo, logger: logger}
}

// GetPlans returns all available subscription plans.
func (s *SubscriptionService) GetPlans() []subDomain.PlanInfo {
	return subDomain.AvailablePlans()
}

// Subscribe creates a new subscription for a user.
func (s *SubscriptionService) Subscribe(ctx context.Context, userID uuid.UUID, req SubscribeRequest) (*SubscriptionDTO, error) {
	// Check if user already has an active subscription
	existing, err := s.repo.FindActiveByUserID(ctx, userID)
	if err == nil && existing != nil && existing.IsActive() {
		return nil, fmt.Errorf("you already have an active %s subscription", existing.Plan())
	}

	sub, err := subDomain.NewSubscription(userID, subDomain.PlanType(req.Plan))
	if err != nil {
		return nil, err
	}

	if err := s.repo.Save(ctx, sub); err != nil {
		return nil, fmt.Errorf("failed to save subscription: %w", err)
	}

	s.logger.Info("subscription created",
		zap.String("user_id", userID.String()),
		zap.String("plan", req.Plan),
	)

	return toSubDTO(sub), nil
}

// GetMySubscription returns the user's active subscription.
func (s *SubscriptionService) GetMySubscription(ctx context.Context, userID uuid.UUID) (*SubscriptionDTO, error) {
	sub, err := s.repo.FindActiveByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("no active subscription found")
	}
	return toSubDTO(sub), nil
}

// CancelSubscription cancels the user's active subscription.
func (s *SubscriptionService) CancelSubscription(ctx context.Context, userID uuid.UUID) (*SubscriptionDTO, error) {
	sub, err := s.repo.FindActiveByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("no active subscription found")
	}

	sub.Cancel()
	if err := s.repo.Update(ctx, sub); err != nil {
		return nil, fmt.Errorf("failed to cancel subscription: %w", err)
	}

	s.logger.Info("subscription cancelled", zap.String("user_id", userID.String()))
	return toSubDTO(sub), nil
}

func toSubDTO(s *subDomain.Subscription) *SubscriptionDTO {
	return &SubscriptionDTO{
		ID: s.ID(), UserID: s.UserID(), Plan: string(s.Plan()),
		PriceCents: s.PriceCents(), StartedAt: s.StartedAt(), ExpiresAt: s.ExpiresAt(),
		Status: string(s.Status()), AutoRenew: s.AutoRenew(), CreatedAt: s.CreatedAt(),
	}
}
