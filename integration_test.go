//go:build integration

package main_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/niaga-labs/niaga-labs-pet-lib-proto/events"
	"github.com/niaga-labs/niaga-labs-pet-service-payment/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeliveryConfirmed_ReleasesEscrow verifies that when a BookingDeliveryConfirmed
// event is published to booking.events, the payment service picks it up,
// runs the ReleaseEscrowSaga, and publishes an EscrowReleasedEvent.
func TestDeliveryConfirmed_ReleasesEscrow(t *testing.T) {
	infra := setupContainers(t)
	defer infra.Cleanup()

	stack := setupPaymentStack(t, infra.DB, infra.KafkaBrokers)
	defer stack.CleanupProducer()
	defer func() { _ = stack.Consumer.Close() }()

	// Seed a payment in "held" state.
	bookingID := uuid.New()
	ownerID := uuid.New()
	runnerID := uuid.New()
	seedPaymentInHeldState(t, infra.DB, bookingID, ownerID)

	// Start the consumer.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = stack.Consumer.Start(ctx) }()
	time.Sleep(3 * time.Second) // Wait for consumer group join.

	// Publish DeliveryConfirmedEvent.
	evt := events.DeliveryConfirmedEvent{
		BookingID:     bookingID,
		BookingNumber: "BK-INTTEST01",
		RunnerID:      runnerID,
		OwnerID:       ownerID,
		DeliveredAt:   time.Now().UTC(),
		OccurredAt:    time.Now().UTC(),
	}
	publishTestEvent(t, infra.KafkaBrokers, events.TopicBookingEvents,
		"service-booking", events.BookingDeliveryConfirmed, evt)

	// Assert: DB transitions to "released".
	model := waitForDBStatus(t, infra.DB, bookingID, "released", 15*time.Second)
	assert.NotNil(t, model.RunnerID, "runner_id should be set")
	assert.Equal(t, runnerID, *model.RunnerID)
	assert.NotNil(t, model.EscrowReleasedAt, "escrow_released_at should be set")

	// Assert: EscrowReleasedEvent on payment.events.
	ce := consumeOneEvent(t, infra.KafkaBrokers, events.TopicPaymentEvents,
		events.PaymentEscrowReleased, 15*time.Second)

	var released events.EscrowReleasedEvent
	require.NoError(t, ce.ParseData(&released))
	assert.Equal(t, bookingID, released.BookingID)
	assert.Equal(t, runnerID, released.RunnerID)
	assert.Equal(t, int64(127500), released.RunnerPayout)
	assert.Equal(t, int64(22500), released.PlatformFee)
	assert.Equal(t, "MYR", released.Currency)
}

// TestBookingCancelled_RefundsEscrow verifies that a BookingCancelled event
// triggers a refund on a held payment.
func TestBookingCancelled_RefundsEscrow(t *testing.T) {
	infra := setupContainers(t)
	defer infra.Cleanup()

	stack := setupPaymentStack(t, infra.DB, infra.KafkaBrokers)
	defer stack.CleanupProducer()
	defer func() { _ = stack.Consumer.Close() }()

	bookingID := uuid.New()
	ownerID := uuid.New()
	seedPaymentInHeldState(t, infra.DB, bookingID, ownerID)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = stack.Consumer.Start(ctx) }()
	time.Sleep(3 * time.Second)

	// Publish BookingCancelledEvent.
	evt := events.BookingCancelledEvent{
		BookingID:     bookingID,
		BookingNumber: "BK-INTTEST02",
		CancelledBy:   ownerID,
		Reason:        "owner cancelled",
		OccurredAt:    time.Now().UTC(),
	}
	publishTestEvent(t, infra.KafkaBrokers, events.TopicBookingEvents,
		"service-booking", events.BookingCancelled, evt)

	// Assert: DB transitions to "refunded".
	model := waitForDBStatus(t, infra.DB, bookingID, "refunded", 15*time.Second)
	assert.Contains(t, model.RefundReason, "booking cancelled")
	assert.NotNil(t, model.RefundedAt, "refunded_at should be set")

	// Assert: EscrowRefundedEvent on payment.events.
	ce := consumeOneEvent(t, infra.KafkaBrokers, events.TopicPaymentEvents,
		events.PaymentEscrowRefunded, 15*time.Second)

	var refunded events.EscrowRefundedEvent
	require.NoError(t, ce.ParseData(&refunded))
	assert.Equal(t, bookingID, refunded.BookingID)
	assert.Contains(t, refunded.RefundReason, "booking cancelled")
}

// TestBookingCancelled_NoPayment_Skips verifies that a cancel event with no
// matching payment does not cause errors.
func TestBookingCancelled_NoPayment_Skips(t *testing.T) {
	infra := setupContainers(t)
	defer infra.Cleanup()

	stack := setupPaymentStack(t, infra.DB, infra.KafkaBrokers)
	defer stack.CleanupProducer()
	defer func() { _ = stack.Consumer.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = stack.Consumer.Start(ctx) }()
	time.Sleep(3 * time.Second)

	// Publish cancel for a booking with no payment record.
	bookingID := uuid.New()
	evt := events.BookingCancelledEvent{
		BookingID:     bookingID,
		BookingNumber: "BK-INTTEST03",
		CancelledBy:   uuid.New(),
		Reason:        "no payment exists",
		OccurredAt:    time.Now().UTC(),
	}
	publishTestEvent(t, infra.KafkaBrokers, events.TopicBookingEvents,
		"service-booking", events.BookingCancelled, evt)

	// Give consumer time to process. No crash expected.
	time.Sleep(5 * time.Second)

	// Verify no payment was created.
	var count int64
	infra.DB.Model(&repository.PaymentModel{}).Where("booking_id = ?", bookingID).Count(&count)
	assert.Equal(t, int64(0), count, "no payment should exist")
}

// TestBookingCancelled_PendingPayment_NoRefund verifies that a cancel event
// does not refund a payment that is still in "pending" state.
func TestBookingCancelled_PendingPayment_NoRefund(t *testing.T) {
	infra := setupContainers(t)
	defer infra.Cleanup()

	stack := setupPaymentStack(t, infra.DB, infra.KafkaBrokers)
	defer stack.CleanupProducer()
	defer func() { _ = stack.Consumer.Close() }()

	bookingID := uuid.New()
	ownerID := uuid.New()
	seedPaymentInPendingState(t, infra.DB, bookingID, ownerID)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = stack.Consumer.Start(ctx) }()
	time.Sleep(3 * time.Second)

	evt := events.BookingCancelledEvent{
		BookingID:     bookingID,
		BookingNumber: "BK-INTTEST04",
		CancelledBy:   ownerID,
		Reason:        "pending payment test",
		OccurredAt:    time.Now().UTC(),
	}
	publishTestEvent(t, infra.KafkaBrokers, events.TopicBookingEvents,
		"service-booking", events.BookingCancelled, evt)

	// Wait and verify payment stays pending.
	time.Sleep(5 * time.Second)
	var model repository.PaymentModel
	require.NoError(t, infra.DB.Where("booking_id = ?", bookingID).First(&model).Error)
	assert.Equal(t, "pending", model.EscrowStatus, "payment should remain pending")
}
