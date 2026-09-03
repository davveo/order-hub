package application

import (
	"context"
	"fmt"
	"time"

	"github.com/davveo/order-hub/internal/application/port"
	"github.com/davveo/order-hub/internal/domain"
)

type PreviewCmd struct {
	Scene     string
	Channel   string
	CouponIDs []string
	AutoBest  bool
	Items     []domain.OrderLine
	LedgerPay *LedgerPay
	Ext       map[string]any
}

type LedgerPay struct {
	AssetCode string
	Amount    int64
}

type PreviewResult struct {
	QuoteID          string
	OriginalAmount   int64
	DiscountAmount   int64
	PayableAmount    int64
	Allocations      []domain.Allocation
	LedgerPayAmount  int64
	ChannelPayAmount int64
	Currency         string
	ExpiresAt        time.Time
	LedgerBalance    *int64 `json:"ledger_balance,omitempty"`
	ContextHash      string
}

type PreviewService struct {
	scenes   map[string]domain.SceneConfig
	offer    port.OfferClient
	ledger   port.LedgerClient
	cache    port.PreviewCache
	clock    port.Clock
	quoteTTL time.Duration
}

func NewPreviewService(
	scenes map[string]domain.SceneConfig,
	offer port.OfferClient,
	ledger port.LedgerClient,
	cache port.PreviewCache,
	clock port.Clock,
) *PreviewService {
	return &PreviewService{
		scenes:   scenes,
		offer:    offer,
		ledger:   ledger,
		cache:    cache,
		clock:    clock,
		quoteTTL: 5 * time.Minute,
	}
}

func (s *PreviewService) Preview(ctx context.Context, ident *port.Identity, cmd PreviewCmd) (*PreviewResult, error) {
	scene, ok := s.scenes[cmd.Scene]
	if !ok {
		return nil, fmt.Errorf("%w: %s", domain.ErrSceneNotFound, cmd.Scene)
	}
	if cmd.Channel == "" {
		cmd.Channel = "app"
	}
	if err := validateLines(cmd.Items); err != nil {
		return nil, err
	}
	original, err := domain.SumOriginal(cmd.Items)
	if err != nil {
		return nil, err
	}

	quote, err := s.offer.Quote(ctx, port.QuoteRequest{
		TenantID:   ident.TenantID,
		UserID:     ident.UserID,
		OrderID:    "preview:" + ident.UserID,
		Scene:      cmd.Scene,
		Channel:    cmd.Channel,
		Currency:   scene.Currency,
		CouponIDs:  cmd.CouponIDs,
		AutoBest:   cmd.AutoBest,
		Items:      cmd.Items,
		Attributes: ident.Attributes,
		Context:    cmd.Ext,
	})
	if err != nil {
		return nil, err
	}
	if quote.Currency != scene.Currency || quote.OriginalAmount != original {
		return nil, domain.ErrQuoteStale
	}
	if err := domain.AllocateDiscount(cmd.Items, quote.DiscountAmount); err != nil {
		return nil, err
	}

	var ledgerPay int64
	var asset string
	if cmd.LedgerPay != nil {
		ledgerPay = cmd.LedgerPay.Amount
		asset = cmd.LedgerPay.AssetCode
	}
	amounts, err := domain.BuildAmounts(scene.Currency, original, quote.DiscountAmount, ledgerPay)
	if err != nil {
		return nil, err
	}
	if scene.Ledger == domain.LedgerFreezeCapture && ledgerPay == 0 {
		amounts, err = domain.BuildAmounts(scene.Currency, original, quote.DiscountAmount, amounts.Payable)
		if err != nil {
			return nil, err
		}
		ledgerPay = amounts.LedgerPay
		if asset == "" {
			asset = scene.Currency
		}
	}

	var balance *int64
	if asset != "" && s.ledger != nil {
		b, err := s.ledger.GetBalance(ctx, ident.TenantID, ident.UserID, asset)
		if err == nil {
			balance = &b
		}
	}

	hash := domain.ContextHash(domain.HashInput{
		Scene:     cmd.Scene,
		Channel:   cmd.Channel,
		Currency:  scene.Currency,
		CouponIDs: cmd.CouponIDs,
		Items:     cmd.Items,
		LedgerPay: ledgerPay,
		AssetCode: asset,
		Ext:       cmd.Ext,
	})
	expires := quote.ExpiresAt
	if expires.IsZero() {
		expires = s.clock.Now().Add(s.quoteTTL)
	}
	snap := port.PreviewSnapshot{
		QuoteID:        quote.QuoteID,
		ContextHash:    hash,
		Scene:          cmd.Scene,
		Channel:        cmd.Channel,
		Currency:       scene.Currency,
		CouponIDs:      cmd.CouponIDs,
		Items:          cmd.Items,
		Allocations:    quote.Allocations,
		Promotions:     quote.Promotions,
		OriginalAmount: amounts.Original,
		DiscountAmount: amounts.Discount,
		PayableAmount:  amounts.Payable,
		LedgerPay:      amounts.LedgerPay,
		AssetCode:      asset,
		ChannelPay:     amounts.ChannelPay,
		Ext:            cmd.Ext,
		ExpiresAt:      expires,
		SubjectAttrs:   ident.Attributes,
	}
	if s.cache != nil {
		_ = s.cache.Put(ctx, ident.TenantID, ident.UserID, quote.QuoteID, snap, time.Until(expires))
	}
	return &PreviewResult{
		QuoteID:          quote.QuoteID,
		OriginalAmount:   amounts.Original,
		DiscountAmount:   amounts.Discount,
		PayableAmount:    amounts.Payable,
		Allocations:      quote.Allocations,
		LedgerPayAmount:  amounts.LedgerPay,
		ChannelPayAmount: amounts.ChannelPay,
		Currency:         amounts.Currency,
		ExpiresAt:        expires,
		LedgerBalance:    balance,
		ContextHash:      hash,
	}, nil
}

func validateLines(items []domain.OrderLine) error {
	if len(items) == 0 {
		return fmt.Errorf("%w: items required", domain.ErrInvalidArgument)
	}
	seen := make(map[string]struct{}, len(items))
	for _, it := range items {
		if it.LineID == "" || it.ObjectType == "" || it.ObjectID == "" {
			return fmt.Errorf("%w: line fields", domain.ErrInvalidArgument)
		}
		if _, ok := seen[it.LineID]; ok {
			return fmt.Errorf("%w: duplicate line_id %s", domain.ErrInvalidArgument, it.LineID)
		}
		seen[it.LineID] = struct{}{}
		if it.Quantity <= 0 || it.UnitPrice < 0 {
			return fmt.Errorf("%w: quantity/unit_price", domain.ErrInvalidArgument)
		}
	}
	return nil
}
