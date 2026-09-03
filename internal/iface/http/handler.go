package httpserver

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"

	"github.com/davveo/order-hub/internal/application"
	"github.com/davveo/order-hub/internal/domain"
	"github.com/gin-gonic/gin"
)

type Handlers struct {
	PreviewSvc       *application.PreviewService
	CheckoutSvc      *application.CheckoutService
	QuerySvc         *application.QueryService
	CancelSvc        *application.CancelService
	PaymentSvc       *application.PaymentService
	RefundSvc        *application.RefundService
	RenewSvc         *application.RenewService
	CloseSvc         *application.CloseService
	Compensate       *application.CompensateWorker
	ReconSvc         *application.ReconService
	SeedSvc          *application.SeedService
	PaySecret        string
	AllowUnsignedPay bool
	ReadyFn          func(ctx context.Context) error
	AdminToken       string
}

func (h *Handlers) Preview(c *gin.Context) {
	var req previewReq
	if err := c.ShouldBindJSON(&req); err != nil {
		writeAppError(c, domain.ErrInvalidArgument)
		return
	}
	out, err := h.PreviewSvc.Preview(c.Request.Context(), identity(c), application.PreviewCmd{
		Scene:     req.Scene,
		Channel:   req.Channel,
		CouponIDs: req.CouponIDs,
		AutoBest:  req.AutoBest,
		Items:     toLines(req.Items),
		LedgerPay: toLedger(req.LedgerPay),
		Ext:       req.Ext,
	})
	if err != nil {
		writeAppError(c, err)
		return
	}
	OK(c, out)
}

func (h *Handlers) Create(c *gin.Context) {
	var req checkoutReq
	if err := c.ShouldBindJSON(&req); err != nil {
		writeAppError(c, domain.ErrInvalidArgument)
		return
	}
	out, err := h.CheckoutSvc.Checkout(c.Request.Context(), identity(c), application.CheckoutCmd{
		ClientOrderID:  req.ClientOrderID,
		Scene:          req.Scene,
		Channel:        req.Channel,
		QuoteID:        req.QuoteID,
		CouponIDs:      req.CouponIDs,
		Items:          toLines(req.Items),
		PayMethod:      domain.PayMethod(req.PayMethod),
		LedgerPay:      toLedger(req.LedgerPay),
		Ext:            req.Ext,
		IdempotencyKey: c.GetHeader("Idempotency-Key"),
		TraceID:        requestID(c),
	})
	if err != nil {
		writeAppError(c, err)
		return
	}
	OK(c, out)
}

func (h *Handlers) Get(c *gin.Context) {
	o, err := h.QuerySvc.Get(c.Request.Context(), identity(c), c.Param("order_id"))
	if err != nil {
		writeAppError(c, err)
		return
	}
	OK(c, viewOrder(o))
}

func (h *Handlers) List(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	items, cursor, err := h.QuerySvc.List(c.Request.Context(), identity(c),
		c.Query("status"), c.Query("scene"), c.Query("cursor"), limit)
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

func (h *Handlers) Cancel(c *gin.Context) {
	o, err := h.CancelSvc.Cancel(c.Request.Context(), identity(c), c.Param("order_id"))
	if err != nil {
		writeAppError(c, err)
		return
	}
	OK(c, viewOrder(o))
}

func (h *Handlers) PayIntent(c *gin.Context) {
	intent, err := h.PaymentSvc.RecreateIntent(c.Request.Context(), identity(c), c.Param("order_id"))
	if err != nil {
		writeAppError(c, err)
		return
	}
	OK(c, intent)
}

func (h *Handlers) ConfirmLedger(c *gin.Context) {
	o, err := h.PaymentSvc.ConfirmLedger(c.Request.Context(), identity(c), c.Param("order_id"))
	if err != nil {
		writeAppError(c, err)
		return
	}
	OK(c, viewOrder(o))
}

func (h *Handlers) Complete(c *gin.Context) {
	ident := identity(c)
	o, err := h.PaymentSvc.Complete(c.Request.Context(), ident.TenantID, c.Param("order_id"))
	if err != nil {
		writeAppError(c, err)
		return
	}
	OK(c, viewOrder(o))
}

func (h *Handlers) Refund(c *gin.Context) {
	var req refundReq
	_ = c.ShouldBindJSON(&req)
	refund, err := h.RefundSvc.Refund(c.Request.Context(), identity(c), c.Param("order_id"), application.RefundCmd{
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

func (h *Handlers) PaymentCallback(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		writeAppError(c, domain.ErrInvalidArgument)
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	sig := c.GetHeader("X-Payment-Signature")
	if h.PaySecret == "" {
		if !h.AllowUnsignedPay {
			Fail(c, http.StatusUnauthorized, 40100, "payment secret not configured")
			return
		}
	} else if !verifyPaymentSig(h.PaySecret, body, sig) {
		Fail(c, http.StatusUnauthorized, 40100, "invalid payment signature")
		return
	}
	var req paymentCallbackReq
	if err := c.ShouldBindJSON(&req); err != nil {
		writeAppError(c, domain.ErrInvalidArgument)
		return
	}
	o, err := h.PaymentSvc.OnPaymentCallback(c.Request.Context(), application.PaymentCallback{
		OrderID:         req.OrderID,
		TenantID:        req.TenantID,
		PaymentIntentID: req.PaymentIntentID,
		Success:         req.Success,
		PaidAmount:      req.PaidAmount,
		Channel:         req.Channel,
		TraceID:         requestID(c),
	})
	if err != nil {
		writeAppError(c, err)
		return
	}
	OK(c, viewOrder(o))
}

func (h *Handlers) Close(c *gin.Context) {
	if h.CloseSvc == nil {
		writeAppError(c, domain.ErrNotImplemented)
		return
	}
	o, err := h.CloseSvc.Close(c.Request.Context(), identity(c), c.Param("order_id"))
	if err != nil {
		writeAppError(c, err)
		return
	}
	OK(c, viewOrder(o))
}

func (h *Handlers) Renew(c *gin.Context) {
	if h.RenewSvc == nil {
		writeAppError(c, domain.ErrNotImplemented)
		return
	}
	o, err := h.RenewSvc.Renew(c.Request.Context(), identity(c), c.Param("order_id"))
	if err != nil {
		writeAppError(c, err)
		return
	}
	OK(c, viewOrder(o))
}

func (h *Handlers) ListCompensations(c *gin.Context) {
	if h.Compensate == nil {
		OK(c, gin.H{"items": []any{}})
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	items, err := h.Compensate.List(c.Request.Context(), c.Query("status"), limit)
	if err != nil {
		writeAppError(c, err)
		return
	}
	OK(c, gin.H{"items": items})
}

func (h *Handlers) RetryCompensation(c *gin.Context) {
	if h.Compensate == nil {
		writeAppError(c, domain.ErrNotImplemented)
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		writeAppError(c, domain.ErrInvalidArgument)
		return
	}
	if err := h.Compensate.Retry(c.Request.Context(), id); err != nil {
		writeAppError(c, err)
		return
	}
	OK(c, gin.H{"id": id, "status": "done"})
}

func Health(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) }

func (h *Handlers) Ready(c *gin.Context) {
	if h.ReadyFn == nil {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
		return
	}
	if err := h.ReadyFn(c.Request.Context()); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not_ready", "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func toLineRefunds(req refundReq) []domain.LineRefund {
	if len(req.Lines) == 0 {
		return nil
	}
	out := make([]domain.LineRefund, 0, len(req.Lines))
	for _, l := range req.Lines {
		out = append(out, domain.LineRefund{LineID: l.LineID, Amount: l.Amount})
	}
	return out
}
