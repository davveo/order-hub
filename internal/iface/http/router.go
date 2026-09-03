package httpserver

import (
	"github.com/davveo/order-hub/internal/application/port"
	"github.com/gin-gonic/gin"
)

func NewRouter(h *Handlers, auth port.AuthClient) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(Recover(), gin.Logger(), RequestID())
	r.GET("/healthz", Health)
	r.GET("/readyz", Health)

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
	}

	internal := r.Group("/internal/v1")
	internal.POST("/orders/callbacks/payment", h.PaymentCallback)
	return r
}
