package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/davveo/order-hub/internal/application/port"
	"github.com/davveo/order-hub/internal/domain"
)

type CheckoutCmd struct {
	ClientOrderID  string
	Scene          string
	Channel        string
	QuoteID        string
	CouponIDs      []string
	Items          []domain.OrderLine
	PayMethod      domain.PayMethod
	LedgerPay      *LedgerPay
	Ext            map[string]any
	IdempotencyKey string
	TraceID        string
}

type CheckoutResult struct {
	OrderID       string              `json:"order_id"`
	Status        domain.Status       `json:"status"`
	ReservationID string              `json:"reservation_id,omitempty"`
	FreezeID      string              `json:"freeze_id,omitempty"`
	PaymentIntent *port.PaymentIntent `json:"payment_intent,omitempty"`
	PayableAmount int64               `json:"payable_amount"`
	Currency      string              `json:"currency"`
	ExpireAt      time.Time           `json:"expire_at"`
}

type CheckoutService struct {
	scenes  map[string]domain.SceneConfig
	repo    port.OrderRepository
	offer   port.OfferClient
	ledger  port.LedgerClient
	pay     port.PaymentAdapter
	fulfill port.FulfillmentRegistry
	cache   port.PreviewCache
	ids     port.IDGenerator
	clock   port.Clock
	lock    port.Locker
}

func NewCheckoutService(
	scenes map[string]domain.SceneConfig,
	repo port.OrderRepository,
	offer port.OfferClient,
	ledger port.LedgerClient,
	pay port.PaymentAdapter,
	fulfill port.FulfillmentRegistry,
	cache port.PreviewCache,
	ids port.IDGenerator,
	clock port.Clock,
	lock port.Locker,
) *CheckoutService {
	return &CheckoutService{
		scenes:  scenes,
		repo:    repo,
		offer:   offer,
		ledger:  ledger,
		pay:     pay,
		fulfill: fulfill,
		cache:   cache,
		ids:     ids,
		clock:   clock,
		lock:    lock,
	}
}

func (s *CheckoutService) Checkout(ctx context.Context, ident *port.Identity, cmd CheckoutCmd) (*CheckoutResult, error) {
	if cmd.ClientOrderID == "" {
		return nil, fmt.Errorf("%w: client_order_id required", domain.ErrInvalidArgument)
	}
	if cmd.IdempotencyKey == "" {
		cmd.IdempotencyKey = cmd.ClientOrderID
	}
	scene, ok := s.scenes[cmd.Scene]
	if !ok {
		return nil, fmt.Errorf("%w: %s", domain.ErrSceneNotFound, cmd.Scene)
	}
	if cmd.Channel == "" {
		cmd.Channel = "app"
	}

	var ledgerAmt int64
	var asset string
	if cmd.LedgerPay != nil {
		ledgerAmt = cmd.LedgerPay.Amount
		asset = cmd.LedgerPay.AssetCode
	}
	reqHash := domain.CheckoutHash(cmd.ClientOrderID, cmd.QuoteID, domain.HashInput{
		Scene:     cmd.Scene,
		Channel:   cmd.Channel,
		CouponIDs: cmd.CouponIDs,
		Items:     cmd.Items,
		LedgerPay: ledgerAmt,
		AssetCode: asset,
		Ext:       cmd.Ext,
	})
	if rec, err := s.repo.FindIdempotency(ctx, ident.TenantID, ident.UserID, cmd.IdempotencyKey); err == nil && rec != nil {
		if rec.RequestHash != reqHash {
			return nil, domain.ErrIdempotencyConflict
		}
		var out CheckoutResult
		if err := json.Unmarshal(rec.Response, &out); err != nil {
			return nil, err
		}
		return &out, nil
	}
	if existing, err := s.repo.FindByClientOrderID(ctx, ident.TenantID, ident.UserID, cmd.ClientOrderID); err == nil && existing != nil {
		return nil, domain.ErrClientOrderConflict
	} else if err != nil && !errors.Is(err, domain.ErrOrderNotFound) {
		return nil, err
	}

	lockKey := fmt.Sprintf("checkout:%s:%s:%s", ident.TenantID, ident.UserID, cmd.ClientOrderID)
	if s.lock != nil {
		unlock, ok, err := s.lock.TryLock(ctx, lockKey, 30*time.Second)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("%w: checkout in progress", domain.ErrIdempotencyConflict)
		}
		defer unlock()
	}

	if rec, err := s.repo.FindIdempotency(ctx, ident.TenantID, ident.UserID, cmd.IdempotencyKey); err == nil && rec != nil {
		if rec.RequestHash != reqHash {
			return nil, domain.ErrIdempotencyConflict
		}
		var out CheckoutResult
		_ = json.Unmarshal(rec.Response, &out)
		return &out, nil
	}

	items, quoteID, coupons, ledgerPay, asset, ext, promotions, err := s.resolveSnapshot(ctx, ident, scene, cmd)
	if err != nil {
		return nil, err
	}
	items = cloneLines(items)
	if err := validateLines(items); err != nil {
		return nil, err
	}
	original, err := domain.SumOriginal(items)
	if err != nil {
		return nil, err
	}

	now := s.clock.Now()
	orderID := s.ids.OrderID()

	quote, err := s.offer.Quote(ctx, port.QuoteRequest{
		TenantID:   ident.TenantID,
		UserID:     ident.UserID,
		OrderID:    orderID,
		Scene:      cmd.Scene,
		Channel:    cmd.Channel,
		Currency:   scene.Currency,
		CouponIDs:  coupons,
		AutoBest:   len(coupons) == 0,
		Items:      items,
		Attributes: ident.Attributes,
		Context:    ext,
	})
	if err != nil {
		return nil, err
	}
	if quote.Currency != scene.Currency || quote.OriginalAmount != original {
		return nil, domain.ErrQuoteStale
	}
	if quoteID != "" && quote.QuoteID != quoteID && s.cache != nil {
		if snap, e := s.cache.GetByQuote(ctx, ident.TenantID, quoteID); e == nil && snap != nil {
			if snap.DiscountAmount != quote.DiscountAmount || now.After(snap.ExpiresAt) {
				return nil, domain.ErrQuoteStale
			}
		}
	}
	if err := domain.AllocateDiscount(items, quote.DiscountAmount); err != nil {
		return nil, err
	}
	if scene.Ledger == domain.LedgerFreezeCapture && ledgerPay == 0 {
		ledgerPay = original - quote.DiscountAmount
		if asset == "" {
			asset = scene.Currency
		}
	}
	amounts, err := domain.BuildAmounts(scene.Currency, original, quote.DiscountAmount, ledgerPay)
	if err != nil {
		return nil, err
	}
	payMethod := cmd.PayMethod
	if payMethod == "" {
		payMethod = domain.ResolvePayMethod(amounts.LedgerPay, amounts.ChannelPay)
	}

	var (
		reservationID string
		freezeID      string
		reservedInv   bool
	)
	rollback := func() {
		rctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if reservedInv {
			if adapter := s.fulfill.ForScene(scene.Name); adapter != nil {
				_ = adapter.Release(rctx, &domain.Order{OrderID: orderID, TenantID: ident.TenantID, Scene: scene.Name, Lines: items})
			}
		}
		if freezeID != "" {
			_ = s.ledger.Release(rctx, freezeID, "order:release:"+orderID)
		}
		if reservationID != "" {
			_ = s.offer.Release(rctx, ident.TenantID, reservationID, orderID, "order:"+orderID+":release")
		}
	}

	if scene.NeedsOffer(quote.QuoteID != "", len(coupons)) && quote.DiscountAmount >= 0 {
		res, err := s.offer.Reserve(ctx, ident.TenantID, quote.QuoteID, orderID, "order:"+orderID+":reserve")
		if err != nil {
			return nil, fmt.Errorf("%w: %v", domain.ErrOfferReserve, err)
		}
		reservationID = res.ReservationID
	}

	if scene.NeedsLedger(amounts.LedgerPay) && amounts.LedgerPay > 0 {
		fid, err := s.ledger.Freeze(ctx, port.FreezeRequest{
			TenantID:  ident.TenantID,
			UserID:    ident.UserID,
			OrderID:   orderID,
			AssetCode: asset,
			Amount:    amounts.LedgerPay,
			BizNo:     "order:freeze:" + orderID,
		})
		if err != nil {
			rollback()
			return nil, fmt.Errorf("%w: %v", domain.ErrLedgerFreeze, err)
		}
		freezeID = fid
	}

	order := &domain.Order{
		OrderID:       orderID,
		TenantID:      ident.TenantID,
		Scene:         scene.Name,
		Channel:       cmd.Channel,
		BuyerUserID:   ident.UserID,
		ClientOrderID: cmd.ClientOrderID,
		Status:        domain.StatusPendingPay,
		Version:       1,
		Amounts:       amounts,
		Promotion: domain.PromotionRef{
			QuoteID:       quote.QuoteID,
			ReservationID: reservationID,
		},
		Ledger: domain.LedgerRef{
			FreezeID:  freezeID,
			AssetCode: asset,
			BizNo:     "order:freeze:" + orderID,
		},
		PayMethod:  payMethod,
		ExpireAt:   now.Add(scene.PayTimeout),
		Context:    ext,
		Lines:      items,
		Promotions: promotions,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if len(order.Promotions) == 0 {
		order.Promotions = quote.Promotions
	}
	if freezeID != "" {
		order.LedgerLegs = []domain.LedgerLeg{{
			Command:   "freeze",
			BizNo:     "order:freeze:" + orderID,
			FreezeID:  freezeID,
			AssetCode: asset,
			Amount:    amounts.LedgerPay,
			Status:    "accepted",
		}}
	}

	if scene.NeedsInventory() {
		adapter := s.fulfill.ForScene(scene.Name)
		if adapter != nil {
			if err := adapter.Reserve(ctx, order); err != nil {
				rollback()
				return nil, fmt.Errorf("%w: %v", domain.ErrInventoryReserve, err)
			}
			reservedInv = true
		}
	}

	result := &CheckoutResult{
		OrderID:       order.OrderID,
		Status:        order.Status,
		ReservationID: reservationID,
		FreezeID:      freezeID,
		PayableAmount: amounts.Payable,
		Currency:      amounts.Currency,
		ExpireAt:      order.ExpireAt,
	}
	respBytes, _ := json.Marshal(result)
	ev := domain.NewEvent(s.ids.EventID(), domain.EventCreated, ident.TenantID, cmd.TraceID, now, domain.OrderEventData(order))
	if err := s.repo.InsertCheckout(ctx, port.CheckoutPersist{
		Order: order,
		Idempotency: &port.IdempotencyRecord{
			TenantID:    ident.TenantID,
			Actor:       ident.UserID,
			Key:         cmd.IdempotencyKey,
			RequestHash: reqHash,
			Response:    respBytes,
			OrderID:     orderID,
			CreatedAt:   now,
		},
		Event: ev,
	}); err != nil {
		rollback()
		if errors.Is(err, domain.ErrIdempotencyConflict) {
			if rec, e := s.repo.FindIdempotency(ctx, ident.TenantID, ident.UserID, cmd.IdempotencyKey); e == nil && rec != nil {
				var out CheckoutResult
				_ = json.Unmarshal(rec.Response, &out)
				return &out, nil
			}
		}
		if errors.Is(err, domain.ErrClientOrderConflict) {
			return nil, err
		}
		return nil, err
	}

	if order.NeedsChannelPay() && s.pay != nil {
		intent, err := s.pay.CreateIntent(ctx, order)
		if err == nil && intent != nil {
			_ = s.repo.UpdatePaymentIntent(ctx, ident.TenantID, orderID, intent.IntentID, intent.Channel)
			result.PaymentIntent = intent
			respBytes, _ = json.Marshal(result)
			_ = s.repo.UpdateIdempotencyResponse(ctx, ident.TenantID, ident.UserID, cmd.IdempotencyKey, respBytes)
		}
	}
	return result, nil
}

func (s *CheckoutService) resolveSnapshot(ctx context.Context, ident *port.Identity, scene domain.SceneConfig, cmd CheckoutCmd) (
	items []domain.OrderLine, quoteID string, coupons []string, ledgerPay int64, asset string, ext map[string]any, promotions []domain.PromotionDetail, err error,
) {
	quoteID = cmd.QuoteID
	coupons = cmd.CouponIDs
	ext = cmd.Ext
	items = cmd.Items
	if cmd.LedgerPay != nil {
		ledgerPay = cmd.LedgerPay.Amount
		asset = cmd.LedgerPay.AssetCode
	}
	if quoteID != "" && s.cache != nil {
		snap, e := s.cache.GetByQuote(ctx, ident.TenantID, quoteID)
		if e != nil {
			return nil, "", nil, 0, "", nil, nil, domain.ErrQuoteStale
		}
		if s.clock.Now().After(snap.ExpiresAt) {
			return nil, "", nil, 0, "", nil, nil, domain.ErrQuoteStale
		}
		if len(items) == 0 {
			items = snap.Items
		}
		if len(coupons) == 0 {
			coupons = snap.CouponIDs
		}
		if cmd.LedgerPay == nil {
			ledgerPay = snap.LedgerPay
			asset = snap.AssetCode
		}
		if ext == nil {
			ext = snap.Ext
		}
		promotions = snap.Promotions
		if cmd.Scene != snap.Scene {
			return nil, "", nil, 0, "", nil, nil, domain.ErrQuoteStale
		}
		want := domain.ContextHash(domain.HashInput{
			Scene:     cmd.Scene,
			Channel:   cmd.Channel,
			Currency:  scene.Currency,
			CouponIDs: coupons,
			Items:     items,
			LedgerPay: ledgerPay,
			AssetCode: asset,
			Ext:       ext,
		})
		if want != snap.ContextHash {
			return nil, "", nil, 0, "", nil, nil, domain.ErrQuoteStale
		}
	}
	if len(items) == 0 {
		return nil, "", nil, 0, "", nil, nil, fmt.Errorf("%w: items required", domain.ErrInvalidArgument)
	}
	return items, quoteID, coupons, ledgerPay, asset, ext, promotions, nil
}

func cloneLines(in []domain.OrderLine) []domain.OrderLine {
	out := make([]domain.OrderLine, len(in))
	copy(out, in)
	return out
}
