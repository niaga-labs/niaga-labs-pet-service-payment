package events

import (
	"context"
	"strings"

	"github.com/niaga-labs/niaga-labs-pet-lib-common/kafka"
	"github.com/niaga-labs/niaga-labs-pet-lib-proto/events"
	"github.com/niaga-labs/niaga-labs-pet-service-payment/internal/application"
	kafkago "github.com/segmentio/kafka-go"
	"go.uber.org/zap"
)

// BookingEventConsumer listens to booking events and triggers payment workflows.
type BookingEventConsumer struct {
	consumer       *kafka.Consumer
	paymentService *application.PaymentService
	logger         *zap.Logger
}

// NewBookingEventConsumer creates a new consumer for booking events.
func NewBookingEventConsumer(
	brokers []string,
	groupID string,
	paymentService *application.PaymentService,
	logger *zap.Logger,
) *BookingEventConsumer {
	consumer := kafka.NewConsumer(brokers, groupID, events.TopicBookingEvents, logger)
	return &BookingEventConsumer{
		consumer:       consumer,
		paymentService: paymentService,
		logger:         logger,
	}
}

// Start begins consuming booking events. It blocks until the context is cancelled.
func (c *BookingEventConsumer) Start(ctx context.Context) error {
	return c.consumer.Consume(ctx, c.handleMessage)
}

// handleMessage routes incoming Kafka messages to the appropriate handler.
func (c *BookingEventConsumer) handleMessage(ctx context.Context, msg kafkago.Message) error {
	cloudEvent, err := kafka.ParseCloudEvent(msg.Value)
	if err != nil {
		c.logger.Error("failed to parse cloud event from booking topic",
			zap.Error(err),
			zap.String("raw", string(msg.Value)),
		)
		return err
	}

	c.logger.Info("received booking event",
		zap.String("type", cloudEvent.Type),
		zap.String("id", cloudEvent.ID),
	)

	switch {
	case strings.EqualFold(cloudEvent.Type, events.BookingDeliveryConfirmed):
		return c.handleDeliveryConfirmed(ctx, cloudEvent)

	case strings.EqualFold(cloudEvent.Type, events.BookingCancelled):
		return c.handleBookingCancelled(ctx, cloudEvent)

	default:
		c.logger.Debug("ignoring unhandled booking event type",
			zap.String("type", cloudEvent.Type),
		)
		return nil
	}
}

// handleDeliveryConfirmed processes a DeliveryConfirmedEvent.
func (c *BookingEventConsumer) handleDeliveryConfirmed(ctx context.Context, ce kafka.CloudEvent) error {
	var event events.DeliveryConfirmedEvent
	if err := ce.ParseData(&event); err != nil {
		c.logger.Error("failed to parse DeliveryConfirmedEvent data", zap.Error(err))
		return err
	}

	return c.paymentService.HandleDeliveryConfirmed(ctx, event)
}

// handleBookingCancelled processes a BookingCancelledEvent.
func (c *BookingEventConsumer) handleBookingCancelled(ctx context.Context, ce kafka.CloudEvent) error {
	var event events.BookingCancelledEvent
	if err := ce.ParseData(&event); err != nil {
		c.logger.Error("failed to parse BookingCancelledEvent data", zap.Error(err))
		return err
	}

	return c.paymentService.HandleBookingCancelled(ctx, event)
}

// Close closes the underlying Kafka consumer.
func (c *BookingEventConsumer) Close() error {
	return c.consumer.Close()
}
