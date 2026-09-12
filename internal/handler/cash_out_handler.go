// Package handler contains HTTP request handlers for the payment service.
package handler

import (
	"context"
	"math"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/niaga-labs/niaga-labs-pet-lib-common/auth"
	"github.com/niaga-labs/niaga-labs-pet-lib-common/middleware"
	"github.com/niaga-labs/niaga-labs-pet-lib-common/response"
	"github.com/niaga-labs/niaga-labs-pet-lib-proto/dto"
	"github.com/niaga-labs/niaga-labs-pet-service-payment/internal/adapter"
	"github.com/niaga-labs/niaga-labs-pet-service-payment/internal/rail"
	"github.com/niaga-labs/niaga-labs-pet-service-payment/internal/repository"
	"go.uber.org/zap"
)

const cashOutFeeCents int64 = 50

// CashOutHandler handles HTTP requests for runner cash-out operations.
type CashOutHandler struct {
	repo      repository.CashOutRepository
	ownership adapter.DestinationOwnership
	rail      rail.Rail
	railDelay time.Duration
	logger    *zap.Logger
}

// NewCashOutHandler creates a new CashOutHandler with all required dependencies.
func NewCashOutHandler(
	repo repository.CashOutRepository,
	ownership adapter.DestinationOwnership,
	r rail.Rail,
	railDelay time.Duration,
	logger *zap.Logger,
) *CashOutHandler {
	return &CashOutHandler{
		repo:      repo,
		ownership: ownership,
		rail:      r,
		railDelay: railDelay,
		logger:    logger,
	}
}

// RegisterRoutes registers all cash-out routes on the given router group.
func (h *CashOutHandler) RegisterRoutes(r *gin.RouterGroup, jwtManager *auth.JWTManager) {
	payouts := r.Group("/payouts")
	payouts.Use(middleware.AuthMiddleware(jwtManager))
	{
		payouts.POST("/cash-out", middleware.RequireRole(auth.RoleRunner), h.CashOut)
	}
}

// CashOut handles POST /api/v1/payouts/cash-out.
// It validates the request, checks destination ownership and available balance,
// inserts a pending record, then hands off to the rail asynchronously.
func (h *CashOutHandler) CashOut(c *gin.Context) {
	// 1. Bind and validate DTO.
	var req dto.CashOutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if err := req.Validate(); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	// 2. Pull runner ID from auth context.
	runnerID, ok := middleware.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	// 3. Parse destination UUID.
	destID, err := uuid.Parse(req.DestinationID)
	if err != nil {
		response.BadRequest(c, "destinationId must be a valid UUID")
		return
	}

	ctx := c.Request.Context()

	// 4. Check destination ownership (cheap check — runs before DB balance query).
	owned, err := h.ownership.BelongsTo(ctx, destID, runnerID)
	if err != nil {
		h.logger.Error("destination ownership check failed",
			zap.String("runner_id", runnerID.String()),
			zap.String("destination_id", destID.String()),
			zap.Error(err),
		)
		response.Error(c, err)
		return
	}
	if !owned {
		c.JSON(http.StatusForbidden, gin.H{"error": "destination does not belong to runner"})
		return
	}

	// 5. Check available balance.
	balance, err := h.repo.GetAvailableBalanceCents(ctx, runnerID)
	if err != nil {
		h.logger.Error("failed to compute available balance",
			zap.String("runner_id", runnerID.String()),
			zap.Error(err),
		)
		response.Error(c, err)
		return
	}
	totalRequired := req.AmountMyrCents + cashOutFeeCents
	if totalRequired > balance {
		response.BadRequest(c, "amount exceeds available balance")
		return
	}

	// 6. Insert pending row.
	now := time.Now().UTC()
	cashOutID := uuid.New()
	model := &repository.CashOutModel{
		ID:             cashOutID,
		RunnerID:       runnerID,
		AmountMyrCents: req.AmountMyrCents,
		FeeMyrCents:    cashOutFeeCents,
		DestinationID:  destID,
		Status:         "pending",
		RequestedAt:    now,
	}
	if err := h.repo.Insert(ctx, model); err != nil {
		h.logger.Error("failed to insert cash-out request",
			zap.String("cash_out_id", cashOutID.String()),
			zap.Error(err),
		)
		response.Error(c, err)
		return
	}

	// 7. Hand off to the rail asynchronously. Use context.Background() — the
	// HTTP context will be cancelled before the goroutine completes.
	go h.processRail(context.Background(), cashOutID, runnerID, req.AmountMyrCents, destID)

	// 8. Return 202 Accepted.
	etaMinutes := int(math.Ceil(h.railDelay.Seconds() / 60))
	c.JSON(http.StatusAccepted, dto.CashOutResponse{
		CashOutID:  cashOutID.String(),
		EtaMinutes: etaMinutes,
	})
}

// processRail submits the transfer to the rail, persists the txRef, polls for
// completion, then marks the row completed. Runs in a goroutine.
// Deadline is max(2×railDelay, 10s) so the goroutine doesn't leak forever.
// Poll interval is min(railDelay/4, 1s) so short delays (tests) are caught fast.
func (h *CashOutHandler) processRail(ctx context.Context, cashOutID, runnerID uuid.UUID, amountCents int64, destID uuid.UUID) {
	txRef, err := h.rail.Submit(ctx, runnerID, amountCents, destID)
	if err != nil {
		h.logger.Error("rail.Submit failed",
			zap.String("cash_out_id", cashOutID.String()),
			zap.Error(err),
		)
		if updateErr := h.repo.UpdateStatus(ctx, cashOutID, "failed", nil); updateErr != nil {
			h.logger.Error("failed to mark cash-out as failed after rail error",
				zap.String("cash_out_id", cashOutID.String()),
				zap.Error(updateErr),
			)
		}
		return
	}

	// Persist processing status + rail reference.
	if err := h.repo.UpdateStatus(ctx, cashOutID, "processing", &txRef); err != nil {
		h.logger.Error("failed to update cash-out to processing",
			zap.String("cash_out_id", cashOutID.String()),
			zap.String("tx_ref", txRef),
			zap.Error(err),
		)
		// Continue polling — the row will still be marked completed if rail succeeds.
	}

	// Poll until completed or timeout.
	// deadline: at least 10s so production polls are sensible; also at least 2×delay.
	deadlineDur := 2 * h.railDelay
	if deadlineDur < 10*time.Second {
		deadlineDur = 10 * time.Second
	}
	// pollInterval: at most 1s; at most railDelay/4 so short test delays are observable.
	pollInterval := h.railDelay / 4
	if pollInterval > 1*time.Second {
		pollInterval = 1 * time.Second
	}
	if pollInterval < 50*time.Millisecond {
		pollInterval = 50 * time.Millisecond
	}

	deadline := time.After(deadlineDur)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			h.logger.Warn("rail completion polling timed out",
				zap.String("cash_out_id", cashOutID.String()),
				zap.String("tx_ref", txRef),
			)
			bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if updateErr := h.repo.UpdateStatus(bgCtx, cashOutID, "failed", nil); updateErr != nil {
				h.logger.Error("failed to mark cash-out as failed after polling timeout",
					zap.String("cash_out_id", cashOutID.String()),
					zap.Error(updateErr),
				)
			}
			return

		case <-ticker.C:
			status, err := h.rail.Status(ctx, txRef)
			if err != nil {
				h.logger.Error("rail.Status error during polling",
					zap.String("cash_out_id", cashOutID.String()),
					zap.String("tx_ref", txRef),
					zap.Error(err),
				)
				bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if updateErr := h.repo.UpdateStatus(bgCtx, cashOutID, "failed", nil); updateErr != nil {
					h.logger.Error("failed to mark cash-out as failed after rail.Status error",
						zap.String("cash_out_id", cashOutID.String()),
						zap.Error(updateErr),
					)
				}
				return
			}

			switch status {
			case rail.RailStatusCompleted:
				if err := h.repo.MarkCompleted(ctx, cashOutID); err != nil {
					h.logger.Error("failed to mark cash-out as completed",
						zap.String("cash_out_id", cashOutID.String()),
						zap.Error(err),
					)
				} else {
					h.logger.Info("cash-out completed",
						zap.String("cash_out_id", cashOutID.String()),
						zap.String("tx_ref", txRef),
					)
				}
				return

			case rail.RailStatusFailed:
				if err := h.repo.UpdateStatus(ctx, cashOutID, "failed", nil); err != nil {
					h.logger.Error("failed to mark cash-out as failed after rail failure",
						zap.String("cash_out_id", cashOutID.String()),
						zap.Error(err),
					)
				}
				return

			default:
				// still processing — keep polling
			}
		}
	}
}
