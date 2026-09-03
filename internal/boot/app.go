package boot

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/davveo/order-hub/internal/application"
	"github.com/davveo/order-hub/internal/application/port"
	"github.com/davveo/order-hub/internal/conf"
	"github.com/davveo/order-hub/internal/domain"
	"github.com/davveo/order-hub/internal/infra/authclient"
	"github.com/davveo/order-hub/internal/infra/cache"
	"github.com/davveo/order-hub/internal/infra/clock"
	"github.com/davveo/order-hub/internal/infra/fulfillment"
	"github.com/davveo/order-hub/internal/infra/idgen"
	"github.com/davveo/order-hub/internal/infra/ledgerclient"
	"github.com/davveo/order-hub/internal/infra/memory"
	"github.com/davveo/order-hub/internal/infra/offerclient"
	"github.com/davveo/order-hub/internal/infra/outbox"
	"github.com/davveo/order-hub/internal/infra/payment"
	"github.com/davveo/order-hub/internal/infra/persistence"
	httpserver "github.com/davveo/order-hub/internal/iface/http"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type Runtime struct {
	Config  conf.Config
	Engine  *gin.Engine
	Timeout *application.TimeoutWorker
	Outbox  *application.OutboxWorker
	Close   func()
}

func Build(cfg conf.Config) (*Runtime, error) {
	scenes := domain.DefaultScenes()
	clk := clock.System{}
	ids := idgen.NewSnowflake()

	var rdb *redis.Client
	if cfg.RedisAddr != "" {
		rdb = redis.NewClient(&redis.Options{
			Addr:         cfg.RedisAddr,
			Password:     cfg.RedisPassword,
			PoolSize:     64,
			MinIdleConns: 8,
			ReadTimeout:  200 * time.Millisecond,
			WriteTimeout: 200 * time.Millisecond,
		})
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err := rdb.Ping(ctx).Err()
		cancel()
		if err != nil {
			log.Printf("redis unavailable, fallback to memory cache: %v", err)
			_ = rdb.Close()
			rdb = nil
		}
	}
	preview := cache.NewPreviewStore(rdb)
	lock := cache.NewLocker(rdb)

	var repo port.OrderRepository
	var db *gorm.DB
	if cfg.PostgresDSN != "" && cfg.PostgresDSN != "memory" {
		opened, err := persistence.Open(cfg.PostgresDSN)
		if err != nil {
			if cfg.MockDeps {
				log.Printf("postgres unavailable, using in-memory store: %v", err)
				repo = memory.NewOrderRepo()
			} else {
				return nil, fmt.Errorf("postgres: %w", err)
			}
		} else {
			db = opened
			if cfg.AutoMigrate {
				if err := persistence.AutoMigrate(db); err != nil {
					return nil, fmt.Errorf("migrate: %w", err)
				}
			}
			repo = persistence.NewOrderRepo(db)
		}
	} else {
		repo = memory.NewOrderRepo()
	}

	var auth port.AuthClient
	var offer port.OfferClient
	var ledger port.LedgerClient
	if cfg.MockDeps {
		auth = authclient.Mock{DefaultTenant: "tenant_001"}
		offer = offerclient.NewMock()
		ledger = ledgerclient.NewMock()
	} else {
		auth = authclient.New(cfg.AuthHubURL, cfg.ServiceAPIKey)
		offer = offerclient.New(cfg.OfferHubURL, cfg.ServiceAPIKey)
		ledger = ledgerclient.New(cfg.LedgerHubURL, cfg.ServiceAPIKey)
	}
	pay := payment.NewMock()
	fulfill := fulfillment.NewRegistry(scenes)

	previewSvc := application.NewPreviewService(scenes, offer, ledger, preview, clk)
	checkoutSvc := application.NewCheckoutService(scenes, repo, offer, ledger, pay, fulfill, preview, ids, clk, lock)
	querySvc := application.NewQueryService(repo)
	cancelSvc := application.NewCancelService(scenes, repo, offer, ledger, pay, fulfill, ids, clk)
	paySvc := application.NewPaymentService(scenes, repo, offer, ledger, pay, fulfill, ids, clk)
	refundSvc := application.NewRefundService(repo, offer, ledger, pay, ids, clk)

	h := &httpserver.Handlers{
		PreviewSvc:  previewSvc,
		CheckoutSvc: checkoutSvc,
		QuerySvc:    querySvc,
		CancelSvc:   cancelSvc,
		PaymentSvc:  paySvc,
		RefundSvc:   refundSvc,
		PaySecret:   cfg.PaymentCallbackSK,
	}

	return &Runtime{
		Config:  cfg,
		Engine:  httpserver.NewRouter(h, auth),
		Timeout: application.NewTimeoutWorker(repo, cancelSvc, clk),
		Outbox:  application.NewOutboxWorker(repo, outbox.LogPublisher{}),
		Close: func() {
			if rdb != nil {
				_ = rdb.Close()
			}
			if db != nil {
				sqlDB, err := db.DB()
				if err == nil {
					_ = sqlDB.Close()
				}
			}
		},
	}, nil
}
