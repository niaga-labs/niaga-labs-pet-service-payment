package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/niaga-labs/niaga-labs-pet-lib-common/auth"
	"github.com/niaga-labs/niaga-labs-pet-lib-common/middleware"
	"github.com/niaga-labs/niaga-labs-pet-lib-common/response"
	"github.com/niaga-labs/niaga-labs-pet-service-payment/internal/application"
)

// SubscriptionHandler handles HTTP requests for subscription operations.
type SubscriptionHandler struct {
	service *application.SubscriptionService
}

// NewSubscriptionHandler creates a new SubscriptionHandler.
func NewSubscriptionHandler(service *application.SubscriptionService) *SubscriptionHandler {
	return &SubscriptionHandler{service: service}
}

// RegisterRoutes registers all subscription routes.
func (h *SubscriptionHandler) RegisterRoutes(r *gin.RouterGroup, jwtManager *auth.JWTManager) {
	authMW := middleware.AuthMiddleware(jwtManager)

	subs := r.Group("/subscriptions")
	{
		subs.GET("/plans", h.GetPlans)
		subs.POST("", authMW, h.Subscribe)
		subs.GET("/me", authMW, h.GetMySubscription)
		subs.POST("/me/cancel", authMW, h.CancelSubscription)
	}
}

// GetPlans handles GET /api/v1/subscriptions/plans.
func (h *SubscriptionHandler) GetPlans(c *gin.Context) {
	plans := h.service.GetPlans()
	response.Success(c, plans)
}

// Subscribe handles POST /api/v1/subscriptions.
func (h *SubscriptionHandler) Subscribe(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req application.SubscribeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	result, err := h.service.Subscribe(c.Request.Context(), userID, req)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.Created(c, result)
}

// GetMySubscription handles GET /api/v1/subscriptions/me.
func (h *SubscriptionHandler) GetMySubscription(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	result, err := h.service.GetMySubscription(c.Request.Context(), userID)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.Success(c, result)
}

// CancelSubscription handles POST /api/v1/subscriptions/me/cancel.
func (h *SubscriptionHandler) CancelSubscription(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	result, err := h.service.CancelSubscription(c.Request.Context(), userID)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.Success(c, result)
}
