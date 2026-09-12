package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/niaga-labs/niaga-labs-pet-lib-common/auth"
	"github.com/niaga-labs/niaga-labs-pet-lib-common/middleware"
	"github.com/niaga-labs/niaga-labs-pet-lib-common/response"
	"github.com/niaga-labs/niaga-labs-pet-service-payment/internal/application"
)

// PaymentHandler handles HTTP requests for payment operations.
type PaymentHandler struct {
	service *application.PaymentService
}

// NewPaymentHandler creates a new PaymentHandler.
func NewPaymentHandler(service *application.PaymentService) *PaymentHandler {
	return &PaymentHandler{service: service}
}

// RegisterRoutes registers all payment routes on the given router group.
func (h *PaymentHandler) RegisterRoutes(r *gin.RouterGroup, jwtManager *auth.JWTManager) {
	payments := r.Group("/payments")
	payments.Use(middleware.AuthMiddleware(jwtManager))
	{
		payments.POST("/initiate", middleware.RequireRole(auth.RoleOwner), h.InitiatePayment)
		payments.GET("/:id", h.GetPayment)
		payments.GET("/booking/:bookingId", h.GetPaymentByBooking)
		payments.POST("/:id/refund", middleware.RequireRole(auth.RoleAdmin), h.RefundPayment)
	}
}

// InitiatePayment handles POST /api/v1/payments/initiate
func (h *PaymentHandler) InitiatePayment(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req application.InitiatePaymentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	dto, err := h.service.InitiatePayment(c.Request.Context(), userID, req)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.Created(c, dto)
}

// GetPayment handles GET /api/v1/payments/:id
func (h *PaymentHandler) GetPayment(c *gin.Context) {
	idStr := c.Param("id")
	paymentID, err := uuid.Parse(idStr)
	if err != nil {
		response.BadRequest(c, "invalid payment ID")
		return
	}

	dto, err := h.service.GetPayment(c.Request.Context(), paymentID)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.Success(c, dto)
}

// GetPaymentByBooking handles GET /api/v1/payments/booking/:bookingId
func (h *PaymentHandler) GetPaymentByBooking(c *gin.Context) {
	idStr := c.Param("bookingId")
	bookingID, err := uuid.Parse(idStr)
	if err != nil {
		response.BadRequest(c, "invalid booking ID")
		return
	}

	dto, err := h.service.GetPaymentByBooking(c.Request.Context(), bookingID)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.Success(c, dto)
}

// RefundPayment handles POST /api/v1/payments/:id/refund
func (h *PaymentHandler) RefundPayment(c *gin.Context) {
	idStr := c.Param("id")
	paymentID, err := uuid.Parse(idStr)
	if err != nil {
		response.BadRequest(c, "invalid payment ID")
		return
	}

	var req struct {
		Reason string `json:"reason" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	dto, err := h.service.RefundPayment(c.Request.Context(), paymentID, req.Reason)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.Success(c, dto)
}
