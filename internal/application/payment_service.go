package application

import (
	"context"
	"time"

	"github.com/Kilat-Pet-Delivery/lib-common/domain"
	"github.com/Kilat-Pet-Delivery/lib-proto/events"
	"github.com/Kilat-Pet-Delivery/service-payment/internal/domain/payment"
	"github.com/Kilat-Pet-Delivery/service-payment/internal/saga"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// InitiatePaymentRequest is the DTO for initiating a new escrow payment.
type InitiatePaymentRequest struct {
	BookingID     uuid.UUID `json:"booking_id" binding:"required"`
	AmountCents   int64     `json:"amount_cents" binding:"required,gt=0"`
	Currency      string    `json:"currency" binding:"required"`
	CustomerEmail string    `json:"customer_email" binding:"required,email"`
}

// PaymentDTO is the API response DTO for payment data.
type PaymentDTO struct {
	ID                uuid.UUID  `json:"id"`
	BookingID         uuid.UUID  `json:"booking_id"`
	OwnerID           uuid.UUID  `json:"owner_id"`
	RunnerID          *uuid.UUID `json:"runner_id,omitempty"`
	EscrowStatus      string     `json:"escrow_status"`
	AmountCents       int64      `json:"amount_cents"`
	PlatformFeeCents  int64      `json:"platform_fee_cents"`
	RunnerPayoutCents int64      `json:"runner_payout_cents"`
	Currency          string     `json:"currency"`
	PaymentMethod     string     `json:"payment_method,omitempty"`
	StripePaymentID   string     `json:"stripe_payment_id,omitempty"`
	EscrowHeldAt      *time.Time `json:"escrow_held_at,omitempty"`
	EscrowReleasedAt  *time.Time `json:"escrow_released_at,omitempty"`
	RefundedAt        *time.Time `json:"refunded_at,omitempty"`
	RefundReason      string     `json:"refund_reason,omitempty"`
	Version           int64      `json:"version"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// PaymentService is the application service that orchestrates payment use cases.
type PaymentService struct {
	repo    payment.PaymentRepository
	sagaSvc *saga.PaymentSagaService
	logger  *zap.Logger
}

// NewPaymentService creates a new PaymentService.
func NewPaymentService(
	repo payment.PaymentRepository,
	sagaSvc *saga.PaymentSagaService,
	logger *zap.Logger,
) *PaymentService {
	return &PaymentService{
		repo:    repo,
		sagaSvc: sagaSvc,
		logger:  logger,
	}
}

// InitiatePayment starts the escrow payment process for a booking.
func (s *PaymentService) InitiatePayment(ctx context.Context, ownerID uuid.UUID, req InitiatePaymentRequest) (*PaymentDTO, error) {
	s.logger.Info("initiating payment",
		zap.String("booking_id", req.BookingID.String()),
		zap.String("owner_id", ownerID.String()),
		zap.Int64("amount_cents", req.AmountCents),
	)

	p, err := s.sagaSvc.CreateEscrowSaga(ctx, req.BookingID, ownerID, req.AmountCents, req.Currency, req.CustomerEmail)
	if err != nil {
		s.logger.Error("failed to initiate payment", zap.Error(err))
		return nil, err
	}

	dto := toPaymentDTO(p)
	return &dto, nil
}

// GetPayment retrieves a payment by its ID.
func (s *PaymentService) GetPayment(ctx context.Context, paymentID uuid.UUID) (*PaymentDTO, error) {
	p, err := s.repo.FindByID(ctx, paymentID)
	if err != nil {
		return nil, err
	}

	dto := toPaymentDTO(p)
	return &dto, nil
}

// GetPaymentByBooking retrieves a payment by its associated booking ID.
func (s *PaymentService) GetPaymentByBooking(ctx context.Context, bookingID uuid.UUID) (*PaymentDTO, error) {
	p, err := s.repo.FindByBookingID(ctx, bookingID)
	if err != nil {
		return nil, err
	}

	dto := toPaymentDTO(p)
	return &dto, nil
}

// RefundPayment initiates a refund for a held escrow payment.
func (s *PaymentService) RefundPayment(ctx context.Context, paymentID uuid.UUID, reason string) (*PaymentDTO, error) {
	s.logger.Info("refunding payment",
		zap.String("payment_id", paymentID.String()),
		zap.String("reason", reason),
	)

	if err := s.sagaSvc.RefundEscrowSaga(ctx, paymentID, reason); err != nil {
		s.logger.Error("failed to refund payment", zap.Error(err))
		return nil, err
	}

	// Reload after saga completes
	p, err := s.repo.FindByID(ctx, paymentID)
	if err != nil {
		return nil, err
	}

	dto := toPaymentDTO(p)
	return &dto, nil
}

// HandleDeliveryConfirmed handles the DeliveryConfirmedEvent from the booking service.
// It releases the escrow to the runner.
func (s *PaymentService) HandleDeliveryConfirmed(ctx context.Context, event events.DeliveryConfirmedEvent) error {
	s.logger.Info("handling delivery confirmed event",
		zap.String("booking_id", event.BookingID.String()),
		zap.String("runner_id", event.RunnerID.String()),
	)

	p, err := s.repo.FindByBookingID(ctx, event.BookingID)
	if err != nil {
		if domErr, ok := err.(*domain.DomainError); ok && domErr.Err == domain.ErrNotFound {
			s.logger.Warn("no payment found for booking, skipping release",
				zap.String("booking_id", event.BookingID.String()),
			)
			return nil
		}
		return err
	}

	return s.sagaSvc.ReleaseEscrowSaga(ctx, p.ID(), event.RunnerID)
}

// HandleBookingCancelled handles the BookingCancelledEvent from the booking service.
// It refunds the escrow if funds are held.
func (s *PaymentService) HandleBookingCancelled(ctx context.Context, event events.BookingCancelledEvent) error {
	s.logger.Info("handling booking cancelled event",
		zap.String("booking_id", event.BookingID.String()),
		zap.String("reason", event.Reason),
	)

	p, err := s.repo.FindByBookingID(ctx, event.BookingID)
	if err != nil {
		if domErr, ok := err.(*domain.DomainError); ok && domErr.Err == domain.ErrNotFound {
			s.logger.Warn("no payment found for booking, skipping refund",
				zap.String("booking_id", event.BookingID.String()),
			)
			return nil
		}
		return err
	}

	// Only refund if the escrow is currently held
	if p.EscrowStatus() == payment.EscrowHeld {
		reason := "booking cancelled: " + event.Reason
		return s.sagaSvc.RefundEscrowSaga(ctx, p.ID(), reason)
	}

	s.logger.Info("payment not in held state, skipping refund",
		zap.String("payment_id", p.ID().String()),
		zap.String("escrow_status", string(p.EscrowStatus())),
	)
	return nil
}

// --- Admin methods ---

// PaymentStatsDTO holds payment statistics for the admin dashboard.
type PaymentStatsDTO struct {
	TotalRevenueCents int64            `json:"total_revenue_cents"`
	TotalPayments     int64            `json:"total_payments"`
	ByStatus          map[string]int64 `json:"by_status"`
}

// ListAllPayments returns a paginated list of all payments (admin).
func (s *PaymentService) ListAllPayments(ctx context.Context, page, limit int) ([]PaymentDTO, int64, error) {
	payments, total, err := s.repo.ListAll(ctx, page, limit)
	if err != nil {
		return nil, 0, err
	}

	dtos := make([]PaymentDTO, len(payments))
	for i, p := range payments {
		dtos[i] = toPaymentDTO(p)
	}
	return dtos, total, nil
}

// GetPaymentStats returns aggregate payment statistics (admin).
func (s *PaymentService) GetPaymentStats(ctx context.Context) (*PaymentStatsDTO, error) {
	revenue, counts, err := s.repo.GetRevenueStats(ctx)
	if err != nil {
		return nil, err
	}

	var total int64
	for _, c := range counts {
		total += c
	}

	return &PaymentStatsDTO{
		TotalRevenueCents: revenue,
		TotalPayments:     total,
		ByStatus:          counts,
	}, nil
}

// toPaymentDTO maps a domain Payment to a PaymentDTO.
func toPaymentDTO(p *payment.Payment) PaymentDTO {
	return PaymentDTO{
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
