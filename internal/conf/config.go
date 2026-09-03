package conf

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	HTTPAddr          string
	PostgresDSN       string
	RedisAddr         string
	RedisPassword     string
	MockDeps          bool
	AutoMigrate       bool
	AuthHubURL        string
	OfferHubURL       string
	LedgerHubURL      string
	ServiceAPIKey     string
	PaymentCallbackSK string
	LogOutbox         bool
	AdminToken        string
	EventWebhookURL   string
}

func Load() Config {
	c := Config{
		HTTPAddr: env("HTTP_ADDR", ":8080"),
		PostgresDSN: env("POSTGRES_DSN", "host=127.0.0.1 user=order "+
			"password=order dbname=order_hub port=5432 sslmode=disable TimeZone=UTC"),
		RedisAddr:         env("REDIS_ADDR", ""),
		RedisPassword:     env("REDIS_PASSWORD", ""),
		MockDeps:          envBool("MOCK_DEPENDENCIES", true),
		AutoMigrate:       envBool("AUTO_MIGRATE", true),
		AuthHubURL:        env("AUTH_HUB_URL", ""),
		OfferHubURL:       env("OFFER_HUB_URL", ""),
		LedgerHubURL:      env("LEDGER_HUB_URL", ""),
		ServiceAPIKey:     env("SERVICE_API_KEY", ""),
		PaymentCallbackSK: env("PAYMENT_CALLBACK_SECRET", ""),
		LogOutbox:         envBool("LOG_OUTBOX", true),
		AdminToken:        env("ADMIN_TOKEN", "dev-admin"),
		EventWebhookURL:   env("EVENT_WEBHOOK_URL", ""),
	}
	return c
}

func env(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func envBool(k string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}
