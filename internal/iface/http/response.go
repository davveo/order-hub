package httpserver

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type Envelope struct {
	Code      int    `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Data      any    `json:"data,omitempty"`
}

func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Envelope{
		Code:      0,
		Message:   "ok",
		RequestID: requestID(c),
		Data:      data,
	})
}

func Fail(c *gin.Context, httpStatus, code int, msg string) {
	c.JSON(httpStatus, Envelope{
		Code:      code,
		Message:   msg,
		RequestID: requestID(c),
	})
}

func requestID(c *gin.Context) string {
	if v, ok := c.Get(ctxRequestID); ok {
		if s, _ := v.(string); s != "" {
			return s
		}
	}
	return c.GetHeader("X-Request-Id")
}
