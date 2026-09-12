package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/niaga-labs/niaga-labs-pet-lib-common/auth"
	"github.com/niaga-labs/niaga-labs-pet-lib-common/middleware"
	"github.com/niaga-labs/niaga-labs-pet-lib-common/response"
	"github.com/niaga-labs/niaga-labs-pet-service-payment/internal/application"
)

// AdminPaymentHandler handles admin HTTP requests for payment management.
type AdminPaymentHandler struct {
	paymentService *application.PaymentService
	promoService   *application.PromoService
}

// NewAdminPaymentHandler creates a new AdminPaymentHandler.
func NewAdminPaymentHandler(paymentService *application.PaymentService, promoService *application.PromoService) *AdminPaymentHandler {
	return &AdminPaymentHandler{
		paymentService: paymentService,
		promoService:   promoService,
	}
}

// RegisterRoutes registers admin payment routes.
func (h *AdminPaymentHandler) RegisterRoutes(r *gin.RouterGroup, jwtManager *auth.JWTManager) {
	authMW := middleware.AuthMiddleware(jwtManager)
	adminRole := middleware.RequireRole(auth.RoleAdmin)

	admin := r.Group("/admin")
	admin.Use(authMW, adminRole)
	{
		admin.GET("/payments", h.ListPayments)
		admin.GET("/stats/payments", h.PaymentStats)
		admin.GET("/promos", h.ListPromos)
	}
}

// ListPayments handles GET /api/v1/admin/payments.
func (h *AdminPaymentHandler) ListPayments(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}

	payments, total, err := h.paymentService.ListAllPayments(c.Request.Context(), page, limit)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.Paginated(c, payments, total, page, limit)
}

// PaymentStats handles GET /api/v1/admin/stats/payments.
func (h *AdminPaymentHandler) PaymentStats(c *gin.Context) {
	stats, err := h.paymentService.GetPaymentStats(c.Request.Context())
	if err != nil {
		response.Error(c, err)
		return
	}

	response.Success(c, stats)
}

// ListPromos handles GET /api/v1/admin/promos.
func (h *AdminPaymentHandler) ListPromos(c *gin.Context) {
	promos, err := h.promoService.GetActivePromos(c.Request.Context())
	if err != nil {
		response.Error(c, err)
		return
	}

	response.Success(c, promos)
}
