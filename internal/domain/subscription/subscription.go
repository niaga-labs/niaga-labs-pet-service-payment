package subscription

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// PlanType represents the subscription plan.
type PlanType string

const (
	PlanBasic   PlanType = "basic"
	PlanPremium PlanType = "premium"
)

// SubStatus represents the subscription status.
type SubStatus string

const (
	StatusActive    SubStatus = "active"
	StatusCancelled SubStatus = "cancelled"
	StatusExpired   SubStatus = "expired"
)

// PlanInfo defines the properties of a subscription plan.
type PlanInfo struct {
	Plan         PlanType `json:"plan"`
	PriceCents   int64    `json:"price_cents"`
	DurationDays int      `json:"duration_days"`
	DiscountPct  int      `json:"discount_percent"`
	Description  string   `json:"description"`
}

// AvailablePlans returns the list of subscription plans.
func AvailablePlans() []PlanInfo {
	return []PlanInfo{
		{Plan: PlanBasic, PriceCents: 1990, DurationDays: 30, DiscountPct: 5, Description: "5% off every booking, valid 30 days"},
		{Plan: PlanPremium, PriceCents: 4990, DurationDays: 30, DiscountPct: 15, Description: "15% off every booking + priority runner matching, valid 30 days"},
	}
}

// Subscription is the aggregate root for user subscriptions.
type Subscription struct {
	id         uuid.UUID
	userID     uuid.UUID
	plan       PlanType
	priceCents int64
	startedAt  time.Time
	expiresAt  time.Time
	status     SubStatus
	autoRenew  bool
	createdAt  time.Time
	updatedAt  time.Time
}

// NewSubscription creates a new subscription.
func NewSubscription(userID uuid.UUID, plan PlanType) (*Subscription, error) {
	var planInfo *PlanInfo
	for _, p := range AvailablePlans() {
		if p.Plan == plan {
			planInfo = &p
			break
		}
	}
	if planInfo == nil {
		return nil, fmt.Errorf("invalid plan: %s", plan)
	}

	now := time.Now().UTC()
	return &Subscription{
		id:         uuid.New(),
		userID:     userID,
		plan:       plan,
		priceCents: planInfo.PriceCents,
		startedAt:  now,
		expiresAt:  now.AddDate(0, 0, planInfo.DurationDays),
		status:     StatusActive,
		autoRenew:  true,
		createdAt:  now,
		updatedAt:  now,
	}, nil
}

// Reconstruct rebuilds a Subscription from persistence.
func Reconstruct(id, userID uuid.UUID, plan PlanType, priceCents int64, startedAt, expiresAt time.Time, status SubStatus, autoRenew bool, createdAt, updatedAt time.Time) *Subscription {
	return &Subscription{
		id: id, userID: userID, plan: plan, priceCents: priceCents,
		startedAt: startedAt, expiresAt: expiresAt, status: status,
		autoRenew: autoRenew, createdAt: createdAt, updatedAt: updatedAt,
	}
}

// Cancel cancels the subscription.
func (s *Subscription) Cancel() {
	s.status = StatusCancelled
	s.autoRenew = false
	s.updatedAt = time.Now().UTC()
}

// IsActive returns true if the subscription is currently active and not expired.
func (s *Subscription) IsActive() bool {
	return s.status == StatusActive && time.Now().UTC().Before(s.expiresAt)
}

// Getters.
func (s *Subscription) ID() uuid.UUID        { return s.id }
func (s *Subscription) UserID() uuid.UUID    { return s.userID }
func (s *Subscription) Plan() PlanType       { return s.plan }
func (s *Subscription) PriceCents() int64    { return s.priceCents }
func (s *Subscription) StartedAt() time.Time { return s.startedAt }
func (s *Subscription) ExpiresAt() time.Time { return s.expiresAt }
func (s *Subscription) Status() SubStatus    { return s.status }
func (s *Subscription) AutoRenew() bool      { return s.autoRenew }
func (s *Subscription) CreatedAt() time.Time { return s.createdAt }
func (s *Subscription) UpdatedAt() time.Time { return s.updatedAt }
