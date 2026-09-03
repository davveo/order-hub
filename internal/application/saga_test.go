package application_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/davveo/order-hub/internal/application"
	"github.com/davveo/order-hub/internal/application/port"
	"github.com/davveo/order-hub/internal/domain"
	"github.com/davveo/order-hub/internal/infra/cache"
	"github.com/davveo/order-hub/internal/infra/clock"
	"github.com/davveo/order-hub/internal/infra/fulfillment"
	"github.com/davveo/order-hub/internal/infra/idgen"
	"github.com/davveo/order-hub/internal/infra/ledgerclient"
	"github.com/davveo/order-hub/internal/infra/memory"
	"github.com/davveo/order-hub/internal/infra/offerclient"
	"github.com/davveo/order-hub/internal/infra/payment"
)

type seqID struct{ n int }

func (s *seqID) next(prefix string) string {
	s.n++
	return prefix + itoa(s.n)
}
func (s *seqID) OrderID() string  { return s.next("ord_") }
func (s *seqID) EventID() string  { return s.next("evt_") }
func (s *seqID) RefundID() string { return s.next("rfd_") }
func (s *seqID) IntentID() string { return s.next("pi_") }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func testHarness() (*application.PreviewService, *application.CheckoutService, *application.CancelService, *application.PaymentService, *application.QueryService, *memory.OrderRepo, *port.Identity) {
	scenes := domain.DefaultScenes()
	repo := memory.NewOrderRepo()
	offer := offerclient.NewMock()
	ledger := ledgerclient.NewMock()
	pay := payment.NewMock()
	fulfill := fulfillment.NewRegistry(scenes)
	ids := &seqID{}
	clk := clock.System{}
	previewCache := cache.NewPreviewStore(nil)
	lock := cache.NewLocker(nil)
	preview := application.NewPreviewService(scenes, offer, ledger, previewCache, clk)
	checkout := application.NewCheckoutService(scenes, repo, offer, ledger, pay, fulfill, previewCache, ids, clk, lock)
	cancel := application.NewCancelService(scenes, repo, offer, ledger, pay, fulfill, ids, clk)
	paymentSvc := application.NewPaymentService(scenes, repo, offer, ledger, pay, fulfill, ids, clk)
	query := application.NewQueryService(repo)
	ident := &port.Identity{UserID: "u_123", TenantID: "tenant_001", Attributes: map[string]any{"member_level": "gold"}}
	return preview, checkout, cancel, paymentSvc, query, repo, ident
}

func mallItems() []domain.OrderLine {
	return []domain.OrderLine{
		{LineID: "line_1", ObjectType: "sku", ObjectID: "sku_1001", Quantity: 2, UnitPrice: 10000},
		{LineID: "line_2", ObjectType: "service", ObjectID: "delivery", Quantity: 1, UnitPrice: 6800},
	}
}

func TestCheckoutMallThenPayThenCannotCancel(t *testing.T) {
	ctx := context.Background()
	preview, checkout, cancel, pay, query, _, ident := testHarness()
	pr, err := preview.Preview(ctx, ident, application.PreviewCmd{
		Scene: "mall_checkout", Channel: "app", CouponIDs: []string{"coupon_001"}, Items: mallItems(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if pr.OriginalAmount != 26800 || pr.DiscountAmount != 2680 || pr.PayableAmount != 24120 {
		t.Fatalf("preview amounts %+v", pr)
	}
	created, err := checkout.Checkout(ctx, ident, application.CheckoutCmd{
		ClientOrderID: "cli_1", Scene: "mall_checkout", Channel: "app", QuoteID: pr.QuoteID,
		CouponIDs: []string{"coupon_001"}, Items: mallItems(), IdempotencyKey: "idem_1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != domain.StatusPendingPay || created.PaymentIntent == nil {
		t.Fatalf("checkout %+v", created)
	}
	o, err := pay.OnPaymentCallback(ctx, application.PaymentCallback{
		OrderID: created.OrderID, TenantID: ident.TenantID, Success: true, PaidAmount: created.PaymentIntent.Amount,
	})
	if err != nil {
		t.Fatal(err)
	}
	if o.Status != domain.StatusFulfilling && o.Status != domain.StatusPaid && o.Status != domain.StatusCompleted {
		t.Fatalf("status %s", o.Status)
	}
	if _, err := cancel.Cancel(ctx, ident, created.OrderID); err == nil {
		t.Fatal("expected cancel after paid to fail")
	}
	got, err := query.Get(ctx, ident, created.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Amounts.Paid != got.Amounts.Payable {
		t.Fatalf("paid mismatch %+v", got.Amounts)
	}
}

func TestCheckoutIdempotent(t *testing.T) {
	ctx := context.Background()
	_, checkout, _, _, _, _, ident := testHarness()
	cmd := application.CheckoutCmd{
		ClientOrderID: "cli_dup", Scene: "mall_checkout", Channel: "app",
		Items: mallItems(), IdempotencyKey: "same-key",
	}
	a, err := checkout.Checkout(ctx, ident, cmd)
	if err != nil {
		t.Fatal(err)
	}
	cmd2 := application.CheckoutCmd{
		ClientOrderID: "cli_dup", Scene: "mall_checkout", Channel: "app",
		Items: mallItems(), IdempotencyKey: "same-key",
	}
	b, err := checkout.Checkout(ctx, ident, cmd2)
	if err != nil {
		t.Fatal(err)
	}
	if a.OrderID != b.OrderID {
		t.Fatalf("%s != %s", a.OrderID, b.OrderID)
	}
	cmd.Items[0].Quantity = 3
	if _, err := checkout.Checkout(ctx, ident, cmd); err != domain.ErrIdempotencyConflict {
		t.Fatalf("want idempotency conflict, got %v", err)
	}
}

func TestPointMallLedgerConfirm(t *testing.T) {
	ctx := context.Background()
	_, checkout, _, pay, _, _, ident := testHarness()
	created, err := checkout.Checkout(ctx, ident, application.CheckoutCmd{
		ClientOrderID: "cli_point", Scene: "point_mall", Channel: "app",
		Items:          []domain.OrderLine{{LineID: "l1", ObjectType: "sku_point", ObjectID: "p1", Quantity: 1, UnitPrice: 500}},
		IdempotencyKey: "k_point",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.FreezeID == "" || created.PaymentIntent != nil {
		t.Fatalf("point mall should freeze without channel intent: %+v", created)
	}
	o, err := pay.ConfirmLedger(ctx, ident, created.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	if o.Status != domain.StatusCompleted {
		t.Fatalf("status %s", o.Status)
	}
}

func TestCancelReleasesResources(t *testing.T) {
	ctx := context.Background()
	_, checkout, cancel, _, _, _, ident := testHarness()
	created, err := checkout.Checkout(ctx, ident, application.CheckoutCmd{
		ClientOrderID: "cli_cancel", Scene: "mall_checkout", Items: mallItems(), IdempotencyKey: "k_c",
	})
	if err != nil {
		t.Fatal(err)
	}
	o, err := cancel.Cancel(ctx, ident, created.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	if o.Status != domain.StatusCancelled {
		t.Fatalf("status %s", o.Status)
	}
}

func TestTimeoutWorkerClosesExpired(t *testing.T) {
	scenes := domain.DefaultScenes()
	sc := scenes["mall_checkout"]
	sc.PayTimeout = time.Millisecond
	scenes["mall_checkout"] = sc
	repo := memory.NewOrderRepo()
	offer := offerclient.NewMock()
	ledger := ledgerclient.NewMock()
	pay := payment.NewMock()
	fulfill := fulfillment.NewRegistry(scenes)
	ids := idgen.NewSnowflake()
	clk := clock.System{}
	checkout := application.NewCheckoutService(scenes, repo, offer, ledger, pay, fulfill, cache.NewPreviewStore(nil), ids, clk, cache.NewLocker(nil))
	cancel := application.NewCancelService(scenes, repo, offer, ledger, pay, fulfill, ids, clk)
	ident := &port.Identity{UserID: "u_1", TenantID: "t1"}
	created, err := checkout.Checkout(context.Background(), ident, application.CheckoutCmd{
		ClientOrderID: "cli_exp", Scene: "mall_checkout", Items: mallItems(), IdempotencyKey: "k_exp",
	})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	w := application.NewTimeoutWorker(repo, cancel, clk)
	n, err := w.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("closed %d", n)
	}
	o, err := repo.FindByID(context.Background(), ident.TenantID, created.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	if o.Status != domain.StatusCancelled {
		t.Fatalf("status %s", o.Status)
	}
}

func TestLedgerInsufficientFailsBeforePersist(t *testing.T) {
	scenes := domain.DefaultScenes()
	repo := memory.NewOrderRepo()
	offer := offerclient.NewMock()
	ledger := ledgerclient.NewMock()
	pay := payment.NewMock()
	fulfill := fulfillment.NewRegistry(scenes)
	ids := &seqID{}
	clk := clock.System{}
	checkout := application.NewCheckoutService(scenes, repo, offer, ledger, pay, fulfill, cache.NewPreviewStore(nil), ids, clk, cache.NewLocker(nil))
	ident := &port.Identity{UserID: "u_poor", TenantID: "t1"}
	_, err := checkout.Checkout(context.Background(), ident, application.CheckoutCmd{
		ClientOrderID: "cli_poor", Scene: "mall_checkout",
		Items:          []domain.OrderLine{{LineID: "l1", ObjectType: "sku", ObjectID: "big", Quantity: 1, UnitPrice: 200000}},
		LedgerPay:      &application.LedgerPay{AssetCode: "POINT", Amount: 150000},
		IdempotencyKey: "k_poor",
	})
	if err == nil {
		t.Fatal("expected freeze failure")
	}
	if repo.Count() != 0 {
		t.Fatal("must not persist order when freeze fails")
	}
}

type commitOnceFail struct {
	port.OfferClient
	n int
}

func (c *commitOnceFail) Commit(ctx context.Context, tenantID, reservationID, orderID, idemKey string) (string, error) {
	c.n++
	if c.n == 1 {
		return "", fmt.Errorf("commit down")
	}
	return c.OfferClient.Commit(ctx, tenantID, reservationID, orderID, idemKey)
}

func TestAfterPaidCompensationRetry(t *testing.T) {
	ctx := context.Background()
	scenes := domain.DefaultScenes()
	repo := memory.NewOrderRepo()
	offer := &commitOnceFail{OfferClient: offerclient.NewMock()}
	ledger := ledgerclient.NewMock()
	payAd := payment.NewMock()
	fulfill := fulfillment.NewRegistry(scenes)
	ids := &seqID{}
	clk := clock.System{}
	checkout := application.NewCheckoutService(scenes, repo, offer, ledger, payAd, fulfill, cache.NewPreviewStore(nil), ids, clk, cache.NewLocker(nil))
	paySvc := application.NewPaymentService(scenes, repo, offer, ledger, payAd, fulfill, ids, clk)
	ident := &port.Identity{UserID: "u_1", TenantID: "t1"}
	created, err := checkout.Checkout(ctx, ident, application.CheckoutCmd{
		ClientOrderID: "cli_comp", Scene: "mall_checkout", Items: mallItems(), IdempotencyKey: "k_comp",
	})
	if err != nil {
		t.Fatal(err)
	}
	o, err := paySvc.OnPaymentCallback(ctx, application.PaymentCallback{
		OrderID: created.OrderID, TenantID: ident.TenantID, Success: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if o.Status != domain.StatusCompensating {
		t.Fatalf("want COMPENSATING got %s", o.Status)
	}
	w := application.NewCompensateWorker(repo, paySvc, offer, ledger, fulfill, clk)
	n, err := w.Tick(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("compensated %d", n)
	}
	got, err := repo.FindByID(ctx, ident.TenantID, created.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.StatusFulfilling {
		t.Fatalf("status %s", got.Status)
	}
}

func TestMixPayAndLineRefund(t *testing.T) {
	ctx := context.Background()
	preview, checkout, _, paySvc, _, _, ident := testHarness()
	_, err := preview.Preview(ctx, ident, application.PreviewCmd{
		Scene: "mall_checkout", Channel: "app", Items: mallItems(),
		LedgerPay: &application.LedgerPay{AssetCode: "POINT", Amount: 500},
	})
	if err != nil {
		t.Fatal(err)
	}
	created, err := checkout.Checkout(ctx, ident, application.CheckoutCmd{
		ClientOrderID: "cli_mix", Scene: "mall_checkout", Channel: "app",
		Items: mallItems(), LedgerPay: &application.LedgerPay{AssetCode: "POINT", Amount: 500},
		IdempotencyKey: "k_mix",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.FreezeID == "" || created.PaymentIntent == nil {
		t.Fatalf("mix should freeze and create channel intent: %+v", created)
	}
	o, err := paySvc.OnPaymentCallback(ctx, application.PaymentCallback{
		OrderID: created.OrderID, TenantID: ident.TenantID, Success: true, PaidAmount: created.PaymentIntent.Amount,
	})
	if err != nil {
		t.Fatal(err)
	}
	if o.Amounts.LedgerPay != 500 || o.Amounts.ChannelPay != o.Amounts.Payable-500 {
		t.Fatalf("mix amounts %+v", o.Amounts)
	}
}

func TestLineRefund(t *testing.T) {
	ctx := context.Background()
	scenes := domain.DefaultScenes()
	repo := memory.NewOrderRepo()
	offer := offerclient.NewMock()
	ledger := ledgerclient.NewMock()
	payAd := payment.NewMock()
	fulfill := fulfillment.NewRegistry(scenes)
	ids := &seqID{}
	clk := clock.System{}
	checkout := application.NewCheckoutService(scenes, repo, offer, ledger, payAd, fulfill, cache.NewPreviewStore(nil), ids, clk, cache.NewLocker(nil))
	paySvc := application.NewPaymentService(scenes, repo, offer, ledger, payAd, fulfill, ids, clk)
	refundSvc := application.NewRefundService(repo, offer, ledger, payAd, ids, clk)
	ident := &port.Identity{UserID: "u_1", TenantID: "t1"}
	created, err := checkout.Checkout(ctx, ident, application.CheckoutCmd{
		ClientOrderID: "cli_rf", Scene: "mall_checkout", Items: mallItems(), IdempotencyKey: "k_rf",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = paySvc.OnPaymentCallback(ctx, application.PaymentCallback{
		OrderID: created.OrderID, TenantID: ident.TenantID, Success: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	rf, err := refundSvc.Refund(ctx, ident, created.OrderID, application.RefundCmd{
		Lines: []domain.LineRefund{{LineID: "line_1", Amount: 1000}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rf.Amount != 1000 || len(rf.Lines) != 1 {
		t.Fatalf("%+v", rf)
	}
	got, err := repo.FindByID(ctx, ident.TenantID, created.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.StatusPartialRefunded || got.Amounts.Refunded != 1000 {
		t.Fatalf("status %s refunded %d", got.Status, got.Amounts.Refunded)
	}
}

func TestReservationRenew(t *testing.T) {
	ctx := context.Background()
	scenes := domain.DefaultScenes()
	repo := memory.NewOrderRepo()
	offer := offerclient.NewMock()
	ledger := ledgerclient.NewMock()
	payAd := payment.NewMock()
	fulfill := fulfillment.NewRegistry(scenes)
	ids := &seqID{}
	clk := clock.System{}
	checkout := application.NewCheckoutService(scenes, repo, offer, ledger, payAd, fulfill, cache.NewPreviewStore(nil), ids, clk, cache.NewLocker(nil))
	renew := application.NewRenewService(repo, offer, clk)
	ident := &port.Identity{UserID: "u_1", TenantID: "t1"}
	created, err := checkout.Checkout(ctx, ident, application.CheckoutCmd{
		ClientOrderID: "cli_rn", Scene: "mall_checkout", Items: mallItems(), IdempotencyKey: "k_rn",
	})
	if err != nil {
		t.Fatal(err)
	}
	o, err := renew.Renew(ctx, ident, created.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	if o.RenewCount != 1 {
		t.Fatalf("renew_count %d", o.RenewCount)
	}
	if offer.RenewCount(created.ReservationID) != 1 {
		t.Fatalf("offer renew %d", offer.RenewCount(created.ReservationID))
	}
}

func TestOfferReconCommitGap(t *testing.T) {
	ctx := context.Background()
	scenes := domain.DefaultScenes()
	repo := memory.NewOrderRepo()
	offer := offerclient.NewMock()
	ledger := ledgerclient.NewMock()
	payAd := payment.NewMock()
	fulfill := fulfillment.NewRegistry(scenes)
	ids := &seqID{}
	clk := clock.System{}
	checkout := application.NewCheckoutService(scenes, repo, offer, ledger, payAd, fulfill, cache.NewPreviewStore(nil), ids, clk, cache.NewLocker(nil))
	paySvc := application.NewPaymentService(scenes, repo, offer, ledger, payAd, fulfill, ids, clk)
	ident := &port.Identity{UserID: "u_1", TenantID: "t1"}
	created, err := checkout.Checkout(ctx, ident, application.CheckoutCmd{
		ClientOrderID: "cli_recon", Scene: "mall_checkout", CouponIDs: []string{"coupon_001"},
		Items: mallItems(), IdempotencyKey: "k_recon",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := paySvc.OnPaymentCallback(ctx, application.PaymentCallback{
		OrderID: created.OrderID, TenantID: ident.TenantID, Success: true,
	}); err != nil {
		t.Fatal(err)
	}
	offer.ForceStatus(created.ReservationID, offerclient.ReservationActive)
	recon := application.NewReconService(repo, offer)
	out, err := recon.Run(ctx, ident.TenantID, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Diffs) == 0 {
		t.Fatal("expected commit gap")
	}
	found := false
	for _, d := range out.Diffs {
		if d.Kind == "offer_commit_missing" && d.TicketCreated {
			found = true
		}
	}
	if !found {
		t.Fatalf("diffs %+v", out.Diffs)
	}
}

func TestCloseMembershipAfterPaid(t *testing.T) {
	ctx := context.Background()
	scenes := domain.DefaultScenes()
	repo := memory.NewOrderRepo()
	offer := offerclient.NewMock()
	ledger := ledgerclient.NewMock()
	payAd := payment.NewMock()
	fulfill := fulfillment.NewRegistry(scenes)
	ids := &seqID{}
	clk := clock.System{}
	checkout := application.NewCheckoutService(scenes, repo, offer, ledger, payAd, fulfill, cache.NewPreviewStore(nil), ids, clk, cache.NewLocker(nil))
	paySvc := application.NewPaymentService(scenes, repo, offer, ledger, payAd, fulfill, ids, clk)
	closeSvc := application.NewCloseService(scenes, repo, offer, ledger, fulfill, ids, clk)
	ident := &port.Identity{UserID: "u_1", TenantID: "t1"}
	created, err := checkout.Checkout(ctx, ident, application.CheckoutCmd{
		ClientOrderID: "cli_close", Scene: "membership",
		Items:          []domain.OrderLine{{LineID: "l1", ObjectType: "membership", ObjectID: "mem_year", Quantity: 1, UnitPrice: 19900}},
		IdempotencyKey: "k_close",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := paySvc.OnPaymentCallback(ctx, application.PaymentCallback{
		OrderID: created.OrderID, TenantID: ident.TenantID, Success: true,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := closeSvc.Close(ctx, ident, created.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.StatusClosed {
		t.Fatalf("status %s", got.Status)
	}
	if _, err := closeSvc.Close(ctx, ident, created.OrderID); err != nil {
		t.Fatalf("idempotent close: %v", err)
	}
}
