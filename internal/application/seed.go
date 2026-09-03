package application

import (
	"context"
	"fmt"
	"time"

	"github.com/davveo/order-hub/internal/application/port"
	"github.com/davveo/order-hub/internal/domain"
)

type SeedService struct {
	checkout *CheckoutService
	payment  *PaymentService
	cancel   *CancelService
	refund   *RefundService
}

func NewSeedService(checkout *CheckoutService, payment *PaymentService, cancel *CancelService, refund *RefundService) *SeedService {
	return &SeedService{checkout: checkout, payment: payment, cancel: cancel, refund: refund}
}

type SeedResult struct {
	Created int           `json:"created"`
	Failed  int           `json:"failed"`
	Orders  []seededOrder `json:"orders"`
	Errors  []string      `json:"errors,omitempty"`
}

type seededOrder struct {
	OrderID  string `json:"order_id"`
	Scene    string `json:"scene"`
	Status   string `json:"status"`
	Buyer    string `json:"buyer"`
	TenantID string `json:"tenant_id"`
	Note     string `json:"note"`
}

type seedCase struct {
	user, tenant, scene, channel, note string
	coupons                            []string
	items                              []domain.OrderLine
	ledger                             *LedgerPay
	after                              string
}

func mallSKU(id string, qty, price int64) []domain.OrderLine {
	return []domain.OrderLine{
		{LineID: "line_1", ObjectType: "sku", ObjectID: id, Quantity: qty, UnitPrice: price},
		{LineID: "line_2", ObjectType: "service", ObjectID: "delivery", Quantity: 1, UnitPrice: 6800},
	}
}

func oneLine(line, typ, id string, qty, price int64) []domain.OrderLine {
	return []domain.OrderLine{{LineID: line, ObjectType: typ, ObjectID: id, Quantity: qty, UnitPrice: price}}
}

func (s *SeedService) Seed(ctx context.Context) (*SeedResult, error) {
	if s == nil || s.checkout == nil {
		return nil, domain.ErrNotImplemented
	}
	cases := []seedCase{
		{user: "u_123", tenant: "tenant_001", scene: "mall_checkout", note: "商城待支付+券", coupons: []string{"coupon_001"}, items: mallSKU("sku_1001", 2, 10000), after: "pending"},
		{user: "u_123", tenant: "tenant_001", scene: "mall_checkout", note: "商城待支付无券", items: mallSKU("sku_1002", 1, 8900), after: "pending"},
		{user: "u_101", tenant: "tenant_001", scene: "mall_checkout", note: "商城履约中", coupons: []string{"coupon_001"}, items: mallSKU("sku_2001", 1, 15900), after: "pay"},
		{user: "u_101", tenant: "tenant_001", scene: "mall_checkout", note: "商城已完成", items: mallSKU("sku_2002", 3, 3200), after: "complete"},
		{user: "u_888", tenant: "tenant_001", scene: "mall_checkout", note: "商城已取消", items: mallSKU("sku_3001", 1, 12800), after: "cancel"},
		{user: "u_888", tenant: "tenant_001", scene: "mall_checkout", note: "商城全额退", coupons: []string{"coupon_001"}, items: mallSKU("sku_3002", 1, 19900), after: "refund"},
		{user: "u_123", tenant: "tenant_001", scene: "mall_checkout", note: "商城部分退", items: mallSKU("sku_4001", 2, 9900), after: "partial_refund"},
		{user: "u_123", tenant: "tenant_001", scene: "mall_checkout", note: "混合支付履约中", items: mallSKU("sku_5001", 1, 26800), ledger: &LedgerPay{AssetCode: "CNY", Amount: 8000}, after: "pay"},
		{user: "u_101", tenant: "tenant_001", scene: "point_mall", note: "积分商城已完成", items: oneLine("l1", "sku_point", "point_sku_1", 1, 500), after: "confirm_ledger"},
		{user: "u_888", tenant: "tenant_001", scene: "point_mall", note: "积分商城待确认", items: oneLine("l1", "sku_point", "point_sku_2", 2, 200), after: "pending"},
		{user: "u_123", tenant: "tenant_001", scene: "membership", note: "会员年卡已完成", items: oneLine("l1", "membership", "mem_year", 1, 19900), after: "pay"},
		{user: "u_101", tenant: "tenant_001", scene: "course", note: "课程已完成", items: oneLine("l1", "course", "course_go", 1, 9900), after: "pay"},
		{user: "u_888", tenant: "tenant_001", scene: "saas_subscription", note: "SaaS 订阅已完成", items: oneLine("l1", "plan", "plan_pro", 1, 29900), after: "pay"},
		{user: "u_201", tenant: "tenant_002", scene: "mall_checkout", note: "租户二待支付", coupons: []string{"coupon_001"}, items: mallSKU("sku_8001", 1, 5600), after: "pending"},
		{user: "u_201", tenant: "tenant_002", scene: "mall_checkout", note: "租户二已完成", items: mallSKU("sku_8002", 2, 4500), after: "complete"},
		{user: "u_123", tenant: "tenant_001", scene: "mall_checkout", note: "小件待支付", items: mallSKU("sku_9001", 1, 1990), after: "pending"},
		{user: "u_101", tenant: "tenant_001", scene: "mall_checkout", note: "配件待支付", items: mallSKU("sku_9002", 4, 1200), after: "pending"},
		{user: "u_888", tenant: "tenant_001", scene: "mall_checkout", note: "数码履约中", items: mallSKU("sku_9003", 1, 49900), after: "pay"},
		{user: "u_123", tenant: "tenant_001", scene: "course", note: "课程待支付", items: oneLine("l1", "course", "course_rust", 1, 12900), after: "pending"},
		{user: "u_101", tenant: "tenant_001", scene: "membership", note: "月卡已取消", items: oneLine("l1", "membership", "mem_month", 1, 2900), after: "cancel"},
		{user: "u_888", tenant: "tenant_001", scene: "saas_subscription", note: "基础版待支付", items: oneLine("l1", "plan", "plan_basic", 1, 9900), after: "pending"},
		{user: "u_201", tenant: "tenant_002", scene: "point_mall", note: "租户二积分已完成", items: oneLine("l1", "sku_point", "point_sku_9", 1, 120), after: "confirm_ledger"},
		{user: "u_123", tenant: "tenant_001", scene: "mall_checkout", note: "已完成大单", coupons: []string{"coupon_001"}, items: mallSKU("sku_vip", 1, 89900), after: "complete"},
		{user: "u_101", tenant: "tenant_001", scene: "mall_checkout", note: "已取消无券", items: mallSKU("sku_out", 1, 3300), after: "cancel"},
	}

	out := &SeedResult{Orders: make([]seededOrder, 0, len(cases))}
	stamp := time.Now().UnixNano()
	for i, c := range cases {
		so, err := s.runCase(ctx, stamp, i, c)
		if err != nil {
			out.Failed++
			out.Errors = append(out.Errors, fmt.Sprintf("%s: %v", c.note, err))
			continue
		}
		out.Created++
		out.Orders = append(out.Orders, *so)
	}
	return out, nil
}

func (s *SeedService) runCase(ctx context.Context, stamp int64, i int, c seedCase) (*seededOrder, error) {
	ident := &port.Identity{UserID: c.user, TenantID: c.tenant, TraceID: "seed"}
	cli := fmt.Sprintf("seed_%d_%02d", stamp, i)
	created, err := s.checkout.Checkout(ctx, ident, CheckoutCmd{
		ClientOrderID:  cli,
		Scene:          c.scene,
		Channel:        "app",
		CouponIDs:      c.coupons,
		Items:          c.items,
		LedgerPay:      c.ledger,
		IdempotencyKey: cli,
		TraceID:        "seed",
	})
	if err != nil {
		return nil, err
	}
	status := string(created.Status)
	switch c.after {
	case "pay":
		o, err := s.payment.OnPaymentCallback(ctx, PaymentCallback{
			OrderID: created.OrderID, TenantID: c.tenant, Success: true, TraceID: "seed",
		})
		if err != nil {
			return nil, err
		}
		status = string(o.Status)
	case "complete":
		if _, err := s.payment.OnPaymentCallback(ctx, PaymentCallback{
			OrderID: created.OrderID, TenantID: c.tenant, Success: true, TraceID: "seed",
		}); err != nil {
			return nil, err
		}
		o, err := s.payment.Complete(ctx, c.tenant, created.OrderID)
		if err != nil {
			return nil, err
		}
		status = string(o.Status)
	case "cancel":
		o, err := s.cancel.Cancel(ctx, ident, created.OrderID)
		if err != nil {
			return nil, err
		}
		status = string(o.Status)
	case "refund":
		if _, err := s.payment.OnPaymentCallback(ctx, PaymentCallback{
			OrderID: created.OrderID, TenantID: c.tenant, Success: true, TraceID: "seed",
		}); err != nil {
			return nil, err
		}
		if _, err := s.payment.Complete(ctx, c.tenant, created.OrderID); err != nil {
			return nil, err
		}
		if _, err := s.refund.Refund(ctx, ident, created.OrderID, RefundCmd{Reason: "seed full refund"}); err != nil {
			return nil, err
		}
		status = string(domain.StatusRefunded)
	case "partial_refund":
		if _, err := s.payment.OnPaymentCallback(ctx, PaymentCallback{
			OrderID: created.OrderID, TenantID: c.tenant, Success: true, TraceID: "seed",
		}); err != nil {
			return nil, err
		}
		if _, err := s.payment.Complete(ctx, c.tenant, created.OrderID); err != nil {
			return nil, err
		}
		if _, err := s.refund.Refund(ctx, ident, created.OrderID, RefundCmd{Amount: 1000, Reason: "seed partial"}); err != nil {
			return nil, err
		}
		status = string(domain.StatusPartialRefunded)
	case "confirm_ledger":
		o, err := s.payment.ConfirmLedger(ctx, ident, created.OrderID)
		if err != nil {
			return nil, err
		}
		status = string(o.Status)
	}
	return &seededOrder{
		OrderID: created.OrderID, Scene: c.scene, Status: status,
		Buyer: c.user, TenantID: c.tenant, Note: c.note,
	}, nil
}
