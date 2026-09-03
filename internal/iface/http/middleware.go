package httpserver

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	"github.com/davveo/order-hub/internal/application/port"
	"github.com/davveo/order-hub/internal/domain"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	ctxRequestID = "request_id"
	ctxIdentity  = "identity"
	ctxToken     = "token"
)

func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-Id")
		if id == "" {
			id = c.GetHeader("traceparent")
		}
		if id == "" {
			id = uuid.NewString()
		}
		c.Set(ctxRequestID, id)
		c.Header("X-Request-Id", id)
		c.Next()
	}
}

func Recover() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, _ any) {
		Fail(c, http.StatusInternalServerError, 50000, "internal error")
	})
}

func Auth(auth port.AuthClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := c.GetHeader("Authorization")
		token := strings.TrimSpace(strings.TrimPrefix(raw, "Bearer "))
		if token == "" {
			token = c.GetHeader("X-User-Id")
		}
		ident, err := auth.Introspect(c.Request.Context(), token)
		if err != nil {
			writeAppError(c, domain.ErrUnauthorized)
			c.Abort()
			return
		}
		if t := c.GetHeader("X-Tenant-Id"); t != "" {
			ident.TenantID = t
		}
		if ident.TenantID == "" {
			writeAppError(c, domain.ErrUnauthorized)
			c.Abort()
			return
		}
		ident.TraceID = requestID(c)
		c.Set(ctxIdentity, ident)
		c.Set(ctxToken, token)
		c.Next()
	}
}

func identity(c *gin.Context) *port.Identity {
	v, _ := c.Get(ctxIdentity)
	ident, _ := v.(*port.Identity)
	return ident
}

func writeAppError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, domain.ErrUnauthorized):
		Fail(c, http.StatusUnauthorized, 40100, "unauthorized")
	case errors.Is(err, domain.ErrInvalidArgument), errors.Is(err, domain.ErrAmountInvariant), errors.Is(err, domain.ErrOverflow):
		Fail(c, http.StatusBadRequest, 40001, err.Error())
	case errors.Is(err, domain.ErrIdempotencyConflict):
		Fail(c, http.StatusConflict, 40910, "idempotency key conflict")
	case errors.Is(err, domain.ErrClientOrderConflict):
		Fail(c, http.StatusConflict, 40920, "client_order_id conflict")
	case errors.Is(err, domain.ErrQuoteStale):
		Fail(c, http.StatusUnprocessableEntity, 42210, "QUOTE_STALE")
	case errors.Is(err, domain.ErrOfferReserve):
		Fail(c, http.StatusUnprocessableEntity, 42211, err.Error())
	case errors.Is(err, domain.ErrLedgerFreeze):
		Fail(c, http.StatusUnprocessableEntity, 42212, err.Error())
	case errors.Is(err, domain.ErrInventoryReserve):
		Fail(c, http.StatusUnprocessableEntity, 42213, err.Error())
	case errors.Is(err, domain.ErrAlreadyPaid), errors.Is(err, domain.ErrStatusNotAllowed), errors.Is(err, domain.ErrVersionConflict):
		Fail(c, http.StatusConflict, 40930, err.Error())
	case errors.Is(err, domain.ErrOrderNotFound), errors.Is(err, domain.ErrSceneNotFound):
		Fail(c, http.StatusNotFound, 40401, err.Error())
	case errors.Is(err, domain.ErrRefundExceedsPaid):
		Fail(c, http.StatusBadRequest, 40001, err.Error())
	default:
		Fail(c, http.StatusInternalServerError, 50000, err.Error())
	}
}

func verifyPaymentSig(secret string, body []byte, sig string) bool {
	if secret == "" {
		return true
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(want), []byte(strings.ToLower(sig)))
}
