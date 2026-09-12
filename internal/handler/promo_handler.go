package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/niaga-labs/niaga-labs-pet-lib-common/auth"
	"github.com/niaga-labs/niaga-labs-pet-lib-common/middleware"
	"github.com/niaga-labs/niaga-labs-pet-lib-common/response"
	"github.com/niaga-labs/niaga-labs-pet-service-payment/internal/application"
)

// PromoHandler handles HTTP requests for promo code operations.
type PromoHandler struct {
	service *application.PromoService
}

// NewPromoHandler creates a new PromoHandler.
func NewPromoHandler(service *application.PromoService) *PromoHandler {
	return &PromoHandler{service: service}
}

// RegisterRoutes registers all promo routes.
func (h *PromoHandler) RegisterRoutes(r *gin.RouterGroup, jwtManager *auth.JWTManager) {
	authMW := middleware.AuthMiddleware(jwtManager)

	promos := r.Group("/promos")
	promos.Use(authMW)
	{
		promos.POST("", middleware.RequireRole(auth.RoleAdmin), h.CreatePromo)
		promos.POST("/validate", h.ValidatePromo)
		promos.GET("/active", h.GetActivePromos)
	}
}

// CreatePromo handles POST /api/v1/promos.
func (h *PromoHandler) CreatePromo(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req application.CreatePromoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	result, err := h.service.CreatePromo(c.Request.Context(), userID, req)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.Created(c, result)
}

// ValidatePromo handles POST /api/v1/promos/validate.
func (h *PromoHandler) ValidatePromo(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	_ = userID // suppress unused, used below

	var req application.ValidatePromoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	result, err := h.service.ValidatePromo(c.Request.Context(), userID, req)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.Success(c, result)
}

// GetActivePromos handles GET /api/v1/promos/active.
func (h *PromoHandler) GetActivePromos(c *gin.Context) {
	result, err := h.service.GetActivePromos(c.Request.Context())
	if err != nil {
		response.Error(c, err)
		return
	}

	response.Success(c, result)
}
