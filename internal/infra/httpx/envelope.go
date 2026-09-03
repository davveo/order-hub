package httpx

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/davveo/order-hub/internal/domain"
)

type Envelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type errorEnvelope struct {
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func Decode(resp *http.Response, dest any) error {
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return domain.ErrUnauthorized
	}
	var env Envelope
	if json.Unmarshal(raw, &env) == nil && len(env.Data) > 0 && string(env.Data) != "null" {
		if env.Code != 0 {
			return mapHubError(env.Code, env.Message)
		}
		if dest == nil {
			return nil
		}
		return json.Unmarshal(env.Data, dest)
	}
	var errEnv errorEnvelope
	_ = json.Unmarshal(raw, &errEnv)
	if resp.StatusCode == 422 || env.Code == 42201 || env.Code == 42212 {
		return domain.ErrLedgerFreeze
	}
	if resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(raw))
		code := ""
		if env.Message != "" {
			msg = env.Message
		}
		if errEnv.Error != nil {
			if errEnv.Error.Message != "" {
				msg = errEnv.Error.Message
			}
			code = strings.ToUpper(errEnv.Error.Code)
		}
		switch {
		case resp.StatusCode == http.StatusNotFound || code == "NOT_FOUND":
			return domain.ErrOrderNotFound
		case code == "UNAUTHORIZED" || code == "API_KEY_REQUIRED":
			return domain.ErrUnauthorized
		case code == "QUOTE_STALE" || strings.Contains(strings.ToLower(msg), "stale"):
			return domain.ErrQuoteStale
		}
		return fmt.Errorf("http %d: %s", resp.StatusCode, truncate(msg, 256))
	}
	if dest == nil {
		return nil
	}
	return json.Unmarshal(raw, dest)
}

func mapHubError(code int, msg string) error {
	switch code {
	case 40100, 401:
		return domain.ErrUnauthorized
	case 42201, 42212:
		return domain.ErrLedgerFreeze
	case 42211:
		return domain.ErrOfferReserve
	case 42210:
		return domain.ErrQuoteStale
	default:
		if msg == "" {
			msg = fmt.Sprintf("hub error %d", code)
		}
		return fmt.Errorf("%s", msg)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
