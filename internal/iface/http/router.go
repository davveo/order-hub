package httpserver

import (
	"github.com/davveo/order-hub/internal/application/port"
	"github.com/gin-gonic/gin"
)

func NewRouter(h *Handlers, auth port.AuthClient) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(Recover(), gin.Logger(), RequestID())
	r.GET("/", h.AdminPage)
	r.GET("/healthz", Health)
	r.GET("/readyz", h.Ready)
	r.GET("/admin", h.AdminPage)
	r.GET("/admin/", h.AdminPage)

	v1 := r.Group("/api/v1")
	v1.Use(Auth(auth))
	{
		v1.POST("/orders/preview", h.Preview)
		v1.POST("/orders", h.Create)
		v1.GET("/orders", h.List)
		v1.GET("/orders/:order_id", h.Get)
		v1.POST("/orders/:order_id/cancel", h.Cancel)
		v1.POST("/orders/:order_id/pay-intent", h.PayIntent)
		v1.POST("/orders/:order_id/confirm-ledger", h.ConfirmLedger)
		v1.POST("/orders/:order_id/complete", h.Complete)
		v1.POST("/orders/:order_id/refunds", h.Refund)
		v1.POST("/orders/:order_id/renew", h.Renew)
		v1.POST("/orders/:order_id/close", h.Close)
	}

	internal := r.Group("/internal/v1")
	internal.POST("/orders/callbacks/payment", h.PaymentCallback)
	internal.GET("/compensations", h.ListCompensations)
	internal.POST("/compensations/:id/retry", h.RetryCompensation)

	admin := r.Group("/admin/v1")
	admin.Use(AdminAuth(h.AdminToken))
	{
		admin.GET("/stats", h.AdminStats)
		admin.GET("/orders", h.AdminListOrders)
		admin.GET("/orders/:order_id", h.AdminGetOrder)
		admin.POST("/orders/:order_id/cancel", h.AdminCancel)
		admin.POST("/orders/:order_id/complete", h.AdminComplete)
		admin.POST("/orders/:order_id/confirm-ledger", h.AdminConfirmLedger)
		admin.POST("/orders/:order_id/renew", h.AdminRenew)
		admin.POST("/orders/:order_id/retry-paid", h.AdminRetryPaid)
		admin.POST("/orders/:order_id/refunds", h.AdminRefund)
		admin.POST("/orders/:order_id/close", h.AdminClose)
		admin.GET("/compensations", h.ListCompensations)
		admin.POST("/compensations/:id/retry", h.RetryCompensation)
		admin.GET("/reconcile/offer", h.AdminReconcileOffer)
		admin.POST("/reconcile/offer", h.AdminReconcileOffer)
		admin.GET("/outbox", h.AdminOutbox)
		admin.POST("/seed", h.AdminSeed)
	}
	return r
}
