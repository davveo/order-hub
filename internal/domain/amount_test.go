package domain_test

import (
	"testing"

	"github.com/davveo/order-hub/internal/domain"
)

func TestBuildAmounts(t *testing.T) {
	a, err := domain.BuildAmounts("CNY", 26800, 4000, 500)
	if err != nil {
		t.Fatal(err)
	}
	if a.Payable != 22800 || a.ChannelPay != 22300 {
		t.Fatalf("got %+v", a)
	}
}

func TestBuildAmountsRejectsDiscountGTOriginal(t *testing.T) {
	_, err := domain.BuildAmounts("CNY", 100, 200, 0)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAllocateDiscountRemainderOnLastLine(t *testing.T) {
	lines := []domain.OrderLine{
		{LineID: "l1", UnitPrice: 100, Quantity: 1, OriginalAmount: 100},
		{LineID: "l2", UnitPrice: 100, Quantity: 1, OriginalAmount: 100},
		{LineID: "l3", UnitPrice: 100, Quantity: 1, OriginalAmount: 100},
	}
	if err := domain.AllocateDiscount(lines, 10); err != nil {
		t.Fatal(err)
	}
	var sum int64
	for _, l := range lines {
		sum += l.DiscountAmount
		if l.OriginalAmount-l.DiscountAmount != l.PayableAmount {
			t.Fatalf("line payable mismatch %+v", l)
		}
	}
	if sum != 10 {
		t.Fatalf("discount sum %d", sum)
	}
}

func TestStateMachinePaidCannotCancel(t *testing.T) {
	o := &domain.Order{Status: domain.StatusPaid, Version: 1}
	if err := domain.Transition(o, domain.StatusCancelled); err != domain.ErrAlreadyPaid && err != domain.ErrStatusNotAllowed {
		t.Fatalf("got %v", err)
	}
}

func TestStateMachineCheckoutHappyPath(t *testing.T) {
	o := &domain.Order{Status: domain.StatusPendingPay, Version: 1}
	if err := domain.Transition(o, domain.StatusPaid); err != nil {
		t.Fatal(err)
	}
	if err := domain.Transition(o, domain.StatusFulfilling); err != nil {
		t.Fatal(err)
	}
	if err := domain.Transition(o, domain.StatusCompleted); err != nil {
		t.Fatal(err)
	}
	if o.Version != 4 {
		t.Fatalf("version %d", o.Version)
	}
}

func TestContextHashStable(t *testing.T) {
	in := domain.HashInput{
		Scene:    "mall_checkout",
		Channel:  "app",
		Currency: "CNY",
		Items: []domain.OrderLine{
			{LineID: "a", ObjectType: "sku", ObjectID: "1", Quantity: 1, UnitPrice: 10},
		},
	}
	h1 := domain.ContextHash(in)
	h2 := domain.ContextHash(in)
	if h1 != h2 || h1 == "" {
		t.Fatal("hash not stable")
	}
}
