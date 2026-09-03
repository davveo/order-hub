package httpserver

import (
	"crypto/subtle"
	_ "embed"
	"net/http"
	"strconv"

	"github.com/davveo/order-hub/internal/application"
	"github.com/davveo/order-hub/internal/application/port"
	"github.com/davveo/order-hub/internal/domain"
	"github.com/gin-gonic/gin"
)

//go:embed web/admin.html
var adminHTML []byte

func (h *Handlers) AdminPage(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", adminHTML)
}

func AdminAuth(token string) gin.HandlerFunc {
	return func(c *gin.Context) {
		got := c.GetHeader("X-Admin-Token")
		if got == "" {
			got = c.Query("token")
		}
		if token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			Fail(c, http.StatusUnauthorized, 40100, "admin unauthorized")
			c.Abort()
			return
		}
		c.Next()
	}
}

func adminOperator(c *gin.Context, tenantID string) *port.Identity {
	return &port.Identity{TenantID: tenantID, TraceID: requestID(c)}
}

func (h *Handlers) AdminStats(c *gin.Context) {
	st, err := h.QuerySvc.AdminStats(c.Request.Context(), c.Query("tenant_id"))
	if err != nil {
		writeAppError(c, err)
		return
	}
	OK(c, st)
}

func (h *Handlers) AdminListOrders(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	var st domain.Status
	if raw := c.Query("status"); raw != "" {
		parsed, ok := domain.ParseStatus(raw)
		if !ok {
			writeAppError(c, domain.ErrInvalidArgument)
			return
		}
		st = parsed
	}
	items, cursor, err := h.QuerySvc.AdminList(c.Request.Context(), port.AdminListFilter{
		TenantID: c.Query("tenant_id"),
		Status:   st,
		Scene:    c.Query("scene"),
		Query:    c.Query("q"),
		Cursor:   c.Query("cursor"),
		Limit:    limit,
	})
	if err != nil {
		writeAppError(c, err)
		return
	}
	views := make([]orderView, 0, len(items))
	for i := range items {
		views = append(views, viewOrder(&items[i]))
	}
	OK(c, gin.H{"items": views, "next_cursor": cursor})
}

func (h *Handlers) AdminGetOrder(c *gin.Context) {
	o, err := h.QuerySvc.GetInternal(c.Request.Context(), c.Query("tenant_id"), c.Param("order_id"))
	if err != nil {
		writeAppError(c, err)
		return
	}
	OK(c, viewOrder(o))
}

func (h *Handlers) AdminCancel(c *gin.Context) {
	o, err := h.QuerySvc.GetInternal(c.Request.Context(), "", c.Param("order_id"))
	if err != nil {
		writeAppError(c, err)
		return
	}
	out, err := h.CancelSvc.Cancel(c.Request.Context(), adminOperator(c, o.TenantID), o.OrderID)
	if err != nil {
		writeAppError(c, err)
		return
	}
	OK(c, viewOrder(out))
}

func (h *Handlers) AdminComplete(c *gin.Context) {
	o, err := h.QuerySvc.GetInternal(c.Request.Context(), "", c.Param("order_id"))
	if err != nil {
		writeAppError(c, err)
		return
	}
	out, err := h.PaymentSvc.Complete(c.Request.Context(), o.TenantID, o.OrderID)
	if err != nil {
		writeAppError(c, err)
		return
	}
	OK(c, viewOrder(out))
}

func (h *Handlers) AdminConfirmLedger(c *gin.Context) {
	o, err := h.QuerySvc.GetInternal(c.Request.Context(), "", c.Param("order_id"))
	if err != nil {
		writeAppError(c, err)
		return
	}
	out, err := h.PaymentSvc.ConfirmLedger(c.Request.Context(), adminOperator(c, o.TenantID), o.OrderID)
	if err != nil {
		writeAppError(c, err)
		return
	}
	OK(c, viewOrder(out))
}

func (h *Handlers) AdminRenew(c *gin.Context) {
	o, err := h.QuerySvc.GetInternal(c.Request.Context(), "", c.Param("order_id"))
	if err != nil {
		writeAppError(c, err)
		return
	}
	out, err := h.RenewSvc.Renew(c.Request.Context(), adminOperator(c, o.TenantID), o.OrderID)
	if err != nil {
		writeAppError(c, err)
		return
	}
	OK(c, viewOrder(out))
}

func (h *Handlers) AdminRetryPaid(c *gin.Context) {
	o, err := h.QuerySvc.GetInternal(c.Request.Context(), "", c.Param("order_id"))
	if err != nil {
		writeAppError(c, err)
		return
	}
	if err := h.PaymentSvc.RetryAfterPaid(c.Request.Context(), o.TenantID, o.OrderID); err != nil {
		writeAppError(c, err)
		return
	}
	got, err := h.QuerySvc.GetInternal(c.Request.Context(), o.TenantID, o.OrderID)
	if err != nil {
		writeAppError(c, err)
		return
	}
	OK(c, viewOrder(got))
}

func (h *Handlers) AdminRefund(c *gin.Context) {
	o, err := h.QuerySvc.GetInternal(c.Request.Context(), "", c.Param("order_id"))
	if err != nil {
		writeAppError(c, err)
		return
	}
	var req refundReq
	_ = c.ShouldBindJSON(&req)
	refund, err := h.RefundSvc.Refund(c.Request.Context(), adminOperator(c, o.TenantID), o.OrderID, application.RefundCmd{
		Amount: req.Amount,
		Reason: req.Reason,
		Lines:  toLineRefunds(req),
	})
	if err != nil {
		writeAppError(c, err)
		return
	}
	OK(c, refund)
}

func (h *Handlers) AdminSeed(c *gin.Context) {
	if h.SeedSvc == nil {
		writeAppError(c, domain.ErrNotImplemented)
		return
	}
	out, err := h.SeedSvc.Seed(c.Request.Context())
	if err != nil {
		writeAppError(c, err)
		return
	}
	OK(c, out)
}
