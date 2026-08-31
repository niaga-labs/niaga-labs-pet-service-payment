package payment

import (
	"time"

	"github.com/Kilat-Pet-Delivery/lib-common/domain"
	"github.com/google/uuid"
)

// EscrowStatus represents the state of an escrow payment.
type EscrowStatus string

const (
	EscrowPending  EscrowStatus = "pending"
	EscrowHeld     EscrowStatus = "held"
	EscrowReleased EscrowStatus = "released"
	EscrowRefunded EscrowStatus = "refunded"
	EscrowFailed   EscrowStatus = "failed"
)

// Payment is the aggregate root for the escrow payment domain.
type Payment struct {
	id                uuid.UUID
	bookingID         uuid.UUID
	ownerID           uuid.UUID
	runnerID          *uuid.UUID
	escrowStatus      EscrowStatus
	amountCents       int64
	platformFeeCents  int64
	runnerPayoutCents int64
	currency          string
	paymentMethod     string
	stripePaymentID   string
	escrowHeldAt      *time.Time
	escrowReleasedAt  *time.Time
	refundedAt        *time.Time
	refundReason      string
	version           int64
	createdAt         time.Time
	updatedAt         time.Time
}

// NewPayment creates a new Payment aggregate with calculated platform fee and runner payout.
// feePercent is the platform fee percentage (e.g. 15.0 for 15%).
func NewPayment(bookingID, ownerID uuid.UUID, amountCents int64, currency string, feePercent float64) *Payment {
	now := time.Now().UTC()
	platformFeeCents := int64(float64(amountCents) * feePercent / 100.0)
	runnerPayoutCents := amountCents - platformFeeCents

	return &Payment{
		id:                uuid.New(),
		bookingID:         bookingID,
		ownerID:           ownerID,
		escrowStatus:      EscrowPending,
		amountCents:       amountCents,
		platformFeeCents:  platformFeeCents,
		runnerPayoutCents: runnerPayoutCents,
		currency:          currency,
		version:           1,
		createdAt:         now,
		updatedAt:         now,
	}
}

// --- Getters ---

func (p *Payment) ID() uuid.UUID                { return p.id }
func (p *Payment) BookingID() uuid.UUID         { return p.bookingID }
func (p *Payment) OwnerID() uuid.UUID           { return p.ownerID }
func (p *Payment) RunnerID() *uuid.UUID         { return p.runnerID }
func (p *Payment) EscrowStatus() EscrowStatus   { return p.escrowStatus }
func (p *Payment) AmountCents() int64           { return p.amountCents }
func (p *Payment) PlatformFeeCents() int64      { return p.platformFeeCents }
func (p *Payment) RunnerPayoutCents() int64     { return p.runnerPayoutCents }
func (p *Payment) Currency() string             { return p.currency }
func (p *Payment) PaymentMethod() string        { return p.paymentMethod }
func (p *Payment) StripePaymentID() string      { return p.stripePaymentID }
func (p *Payment) EscrowHeldAt() *time.Time     { return p.escrowHeldAt }
func (p *Payment) EscrowReleasedAt() *time.Time { return p.escrowReleasedAt }
func (p *Payment) RefundedAt() *time.Time       { return p.refundedAt }
func (p *Payment) RefundReason() string         { return p.refundReason }
func (p *Payment) Version() int64               { return p.version }
func (p *Payment) CreatedAt() time.Time         { return p.createdAt }
func (p *Payment) UpdatedAt() time.Time         { return p.updatedAt }

// --- Behavior / State Transitions ---

// HoldEscrow transitions from pending to held after Stripe authorization.
func (p *Payment) HoldEscrow(stripePaymentID string) error {
	if p.escrowStatus != EscrowPending {
		return domain.NewInvalidStateError(string(p.escrowStatus), string(EscrowHeld))
	}
	now := time.Now().UTC()
	p.escrowStatus = EscrowHeld
	p.stripePaymentID = stripePaymentID
	p.escrowHeldAt = &now
	p.updatedAt = now
	return nil
}

// ReleaseToRunner transitions from held to released after delivery confirmation.
func (p *Payment) ReleaseToRunner(runnerID uuid.UUID) error {
	if p.escrowStatus != EscrowHeld {
		return domain.NewInvalidStateError(string(p.escrowStatus), string(EscrowReleased))
	}
	now := time.Now().UTC()
	p.escrowStatus = EscrowReleased
	p.runnerID = &runnerID
	p.escrowReleasedAt = &now
	p.updatedAt = now
	return nil
}

// Refund transitions from held to refunded when the booking is cancelled.
func (p *Payment) Refund(reason string) error {
	if p.escrowStatus != EscrowHeld {
		return domain.NewInvalidStateError(string(p.escrowStatus), string(EscrowRefunded))
	}
	now := time.Now().UTC()
	p.escrowStatus = EscrowRefunded
	p.refundedAt = &now
	p.refundReason = reason
	p.updatedAt = now
	return nil
}

// Fail transitions any non-terminal status to failed.
func (p *Payment) Fail(reason string) error {
	if p.escrowStatus == EscrowReleased || p.escrowStatus == EscrowRefunded || p.escrowStatus == EscrowFailed {
		return domain.NewInvalidStateError(string(p.escrowStatus), string(EscrowFailed))
	}
	now := time.Now().UTC()
	p.escrowStatus = EscrowFailed
	p.refundReason = reason
	p.updatedAt = now
	return nil
}

// IncrementVersion bumps the version for optimistic locking.
func (p *Payment) IncrementVersion() {
	p.version++
	p.updatedAt = time.Now().UTC()
}

// --- Reconstitution (used by repository to rebuild from persistence) ---

// Reconstitute rebuilds a Payment from persisted data.
func Reconstitute(
	id, bookingID, ownerID uuid.UUID,
	runnerID *uuid.UUID,
	escrowStatus EscrowStatus,
	amountCents, platformFeeCents, runnerPayoutCents int64,
	currency, paymentMethod, stripePaymentID string,
	escrowHeldAt, escrowReleasedAt, refundedAt *time.Time,
	refundReason string,
	version int64,
	createdAt, updatedAt time.Time,
) *Payment {
	return &Payment{
		id:                id,
		bookingID:         bookingID,
		ownerID:           ownerID,
		runnerID:          runnerID,
		escrowStatus:      escrowStatus,
		amountCents:       amountCents,
		platformFeeCents:  platformFeeCents,
		runnerPayoutCents: runnerPayoutCents,
		currency:          currency,
		paymentMethod:     paymentMethod,
		stripePaymentID:   stripePaymentID,
		escrowHeldAt:      escrowHeldAt,
		escrowReleasedAt:  escrowReleasedAt,
		refundedAt:        refundedAt,
		refundReason:      refundReason,
		version:           version,
		createdAt:         createdAt,
		updatedAt:         updatedAt,
	}
}
