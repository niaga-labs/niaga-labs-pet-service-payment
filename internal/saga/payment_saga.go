package saga

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/niaga-labs/niaga-labs-pet-lib-common/kafka"
	"github.com/niaga-labs/niaga-labs-pet-lib-proto/events"
	"github.com/niaga-labs/niaga-labs-pet-service-payment/internal/adapter"
	"github.com/niaga-labs/niaga-labs-pet-service-payment/internal/domain/payment"
	"go.uber.org/zap"
)

// SagaStep represents a single step in a saga with execute and compensate actions.
type SagaStep struct {
	Name       string
	Execute    func(ctx context.Context) error
	Compensate func(ctx context.Context) error
}

// Saga orchestrates a sequence of steps with compensating transactions on failure.
type Saga struct {
	name   string
	steps  []SagaStep
	logger *zap.Logger
}

// NewSaga creates a new saga orchestrator.
func NewSaga(name string, logger *zap.Logger) *Saga {
	return &Saga{
		name:   name,
		steps:  make([]SagaStep, 0),
		logger: logger,
	}
}

// AddStep appends a step to the saga.
func (s *Saga) AddStep(step SagaStep) {
	s.steps = append(s.steps, step)
}

// Execute runs all saga steps in order. On failure, it compensates executed steps in reverse order.
func (s *Saga) Execute(ctx context.Context) error {
	s.logger.Info("saga started", zap.String("saga", s.name))

	executedSteps := make([]SagaStep, 0, len(s.steps))

	for _, step := range s.steps {
		s.logger.Info("executing saga step",
			zap.String("saga", s.name),
			zap.String("step", step.Name),
		)

		if err := step.Execute(ctx); err != nil {
			s.logger.Error("saga step failed, starting compensation",
				zap.String("saga", s.name),
				zap.String("step", step.Name),
				zap.Error(err),
			)

			// Compensate executed steps in reverse order
			for i := len(executedSteps) - 1; i >= 0; i-- {
				compensateStep := executedSteps[i]
				if compensateStep.Compensate != nil {
					s.logger.Info("compensating saga step",
						zap.String("saga", s.name),
						zap.String("step", compensateStep.Name),
					)
					if compErr := compensateStep.Compensate(ctx); compErr != nil {
						s.logger.Error("compensation failed",
							zap.String("saga", s.name),
							zap.String("step", compensateStep.Name),
							zap.Error(compErr),
						)
					}
				}
			}

			return fmt.Errorf("saga '%s' failed at step '%s': %w", s.name, step.Name, err)
		}

		executedSteps = append(executedSteps, step)
	}

	s.logger.Info("saga completed successfully", zap.String("saga", s.name))
	return nil
}

// PaymentSagaService orchestrates payment saga workflows.
type PaymentSagaService struct {
	repo               payment.PaymentRepository
	stripe             adapter.StripeAdapter
	producer           *kafka.Producer
	platformFeePercent float64
	logger             *zap.Logger
}

// NewPaymentSagaService creates a new PaymentSagaService.
func NewPaymentSagaService(
	repo payment.PaymentRepository,
	stripe adapter.StripeAdapter,
	producer *kafka.Producer,
	platformFeePercent float64,
	logger *zap.Logger,
) *PaymentSagaService {
	return &PaymentSagaService{
		repo:               repo,
		stripe:             stripe,
		producer:           producer,
		platformFeePercent: platformFeePercent,
		logger:             logger,
	}
}

// CreateEscrowSaga creates a payment, authorizes it with Stripe, holds the escrow, and publishes an event.
func (s *PaymentSagaService) CreateEscrowSaga(
	ctx context.Context,
	bookingID, ownerID uuid.UUID,
	amountCents int64,
	currency, customerEmail string,
) (*payment.Payment, error) {
	p := payment.NewPayment(bookingID, ownerID, amountCents, currency, s.platformFeePercent)
	var stripePaymentID string

	saga := NewSaga("create_escrow", s.logger)

	// Step 1: Save payment to database
	saga.AddStep(SagaStep{
		Name: "save_payment",
		Execute: func(ctx context.Context) error {
			return s.repo.Save(ctx, p)
		},
		Compensate: func(ctx context.Context) error {
			// Mark payment as failed in DB as compensation
			_ = p.Fail("saga compensation: escrow creation failed")
			return s.repo.Update(ctx, p)
		},
	})

	// Step 2: Create Stripe PaymentIntent with manual capture
	saga.AddStep(SagaStep{
		Name: "create_stripe_payment_intent",
		Execute: func(ctx context.Context) error {
			var err error
			stripePaymentID, _, err = s.stripe.CreatePaymentIntent(ctx, amountCents, currency, customerEmail)
			return err
		},
		Compensate: func(ctx context.Context) error {
			if stripePaymentID != "" {
				return s.stripe.CancelPaymentIntent(ctx, stripePaymentID)
			}
			return nil
		},
	})

	// Step 3: Hold escrow in domain model and persist
	saga.AddStep(SagaStep{
		Name: "hold_escrow",
		Execute: func(ctx context.Context) error {
			if err := p.HoldEscrow(stripePaymentID); err != nil {
				return err
			}
			p.IncrementVersion()
			return s.repo.Update(ctx, p)
		},
		Compensate: func(ctx context.Context) error {
			// Cancel the Stripe intent and mark as failed
			_ = s.stripe.CancelPaymentIntent(ctx, stripePaymentID)
			_ = p.Fail("saga compensation: hold escrow failed")
			return s.repo.Update(ctx, p)
		},
	})

	// Step 4: Publish EscrowHeldEvent
	saga.AddStep(SagaStep{
		Name: "publish_escrow_held_event",
		Execute: func(ctx context.Context) error {
			event := events.EscrowHeldEvent{
				PaymentID:       p.ID(),
				BookingID:       p.BookingID(),
				StripePaymentID: p.StripePaymentID(),
				AmountCents:     p.AmountCents(),
				Currency:        p.Currency(),
				OccurredAt:      time.Now().UTC(),
			}
			cloudEvent, err := kafka.NewCloudEvent("service-payment", events.PaymentEscrowHeld, event)
			if err != nil {
				return fmt.Errorf("failed to create cloud event: %w", err)
			}
			return s.producer.PublishEvent(ctx, events.TopicPaymentEvents, cloudEvent)
		},
		Compensate: nil, // Event publishing has no compensating action
	})

	if err := saga.Execute(ctx); err != nil {
		// Publish a failure event
		s.publishFailedEvent(ctx, p.ID(), p.BookingID(), err.Error())
		return nil, err
	}

	return p, nil
}

// ReleaseEscrowSaga captures the Stripe payment, releases funds to the runner, and publishes an event.
func (s *PaymentSagaService) ReleaseEscrowSaga(ctx context.Context, paymentID, runnerID uuid.UUID) error {
	p, err := s.repo.FindByID(ctx, paymentID)
	if err != nil {
		return err
	}

	saga := NewSaga("release_escrow", s.logger)

	// Step 1: Capture Stripe payment
	saga.AddStep(SagaStep{
		Name: "capture_stripe_payment",
		Execute: func(ctx context.Context) error {
			return s.stripe.CapturePaymentIntent(ctx, p.StripePaymentID())
		},
		Compensate: func(ctx context.Context) error {
			// Attempt to create refund if capture succeeded
			return s.stripe.CreateRefund(ctx, p.StripePaymentID(), p.AmountCents())
		},
	})

	// Step 2: Release to runner in domain model and persist
	saga.AddStep(SagaStep{
		Name: "release_to_runner",
		Execute: func(ctx context.Context) error {
			if err := p.ReleaseToRunner(runnerID); err != nil {
				return err
			}
			p.IncrementVersion()
			return s.repo.Update(ctx, p)
		},
		Compensate: nil, // Cannot undo a domain state change once persisted at this point
	})

	// Step 3: Publish EscrowReleasedEvent
	saga.AddStep(SagaStep{
		Name: "publish_escrow_released_event",
		Execute: func(ctx context.Context) error {
			event := events.EscrowReleasedEvent{
				PaymentID:    p.ID(),
				BookingID:    p.BookingID(),
				RunnerID:     runnerID,
				RunnerPayout: p.RunnerPayoutCents(),
				PlatformFee:  p.PlatformFeeCents(),
				Currency:     p.Currency(),
				OccurredAt:   time.Now().UTC(),
			}
			cloudEvent, err := kafka.NewCloudEvent("service-payment", events.PaymentEscrowReleased, event)
			if err != nil {
				return fmt.Errorf("failed to create cloud event: %w", err)
			}
			return s.producer.PublishEvent(ctx, events.TopicPaymentEvents, cloudEvent)
		},
		Compensate: nil,
	})

	if err := saga.Execute(ctx); err != nil {
		s.publishFailedEvent(ctx, p.ID(), p.BookingID(), err.Error())
		return err
	}

	return nil
}

// RefundEscrowSaga cancels the Stripe payment, refunds in the domain, and publishes an event.
func (s *PaymentSagaService) RefundEscrowSaga(ctx context.Context, paymentID uuid.UUID, reason string) error {
	p, err := s.repo.FindByID(ctx, paymentID)
	if err != nil {
		return err
	}

	saga := NewSaga("refund_escrow", s.logger)

	// Step 1: Cancel Stripe PaymentIntent
	saga.AddStep(SagaStep{
		Name: "cancel_stripe_payment",
		Execute: func(ctx context.Context) error {
			return s.stripe.CancelPaymentIntent(ctx, p.StripePaymentID())
		},
		Compensate: nil, // Cannot undo a Stripe cancellation
	})

	// Step 2: Refund in domain model and persist
	saga.AddStep(SagaStep{
		Name: "refund_in_domain",
		Execute: func(ctx context.Context) error {
			if err := p.Refund(reason); err != nil {
				return err
			}
			p.IncrementVersion()
			return s.repo.Update(ctx, p)
		},
		Compensate: nil,
	})

	// Step 3: Publish EscrowRefundedEvent
	saga.AddStep(SagaStep{
		Name: "publish_escrow_refunded_event",
		Execute: func(ctx context.Context) error {
			event := events.EscrowRefundedEvent{
				PaymentID:    p.ID(),
				BookingID:    p.BookingID(),
				OwnerID:      p.OwnerID(),
				AmountCents:  p.AmountCents(),
				Currency:     p.Currency(),
				RefundReason: reason,
				OccurredAt:   time.Now().UTC(),
			}
			cloudEvent, err := kafka.NewCloudEvent("service-payment", events.PaymentEscrowRefunded, event)
			if err != nil {
				return fmt.Errorf("failed to create cloud event: %w", err)
			}
			return s.producer.PublishEvent(ctx, events.TopicPaymentEvents, cloudEvent)
		},
		Compensate: nil,
	})

	if err := saga.Execute(ctx); err != nil {
		s.publishFailedEvent(ctx, p.ID(), p.BookingID(), err.Error())
		return err
	}

	return nil
}

// publishFailedEvent publishes a PaymentFailedEvent to Kafka.
func (s *PaymentSagaService) publishFailedEvent(ctx context.Context, paymentID, bookingID uuid.UUID, reason string) {
	event := events.PaymentFailedEvent{
		PaymentID:  paymentID,
		BookingID:  bookingID,
		Reason:     reason,
		OccurredAt: time.Now().UTC(),
	}

	cloudEvent, err := kafka.NewCloudEvent("service-payment", events.PaymentFailed, event)
	if err != nil {
		s.logger.Error("failed to create payment failed cloud event", zap.Error(err))
		return
	}

	if err := s.producer.PublishEvent(ctx, events.TopicPaymentEvents, cloudEvent); err != nil {
		s.logger.Error("failed to publish payment failed event", zap.Error(err))
	}
}
