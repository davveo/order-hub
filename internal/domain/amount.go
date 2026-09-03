package domain

import "fmt"

// Amounts 使用最小货币单位整数，订单拥有最终应付金额决定权。
type Amounts struct {
	Currency     string
	Original     int64
	Discount     int64
	Payable      int64
	LedgerPay    int64
	ChannelPay   int64
	Paid         int64
	Refunded     int64
}

func Mul(a, b int64) (int64, error) {
	if a == 0 || b == 0 {
		return 0, nil
	}
	r := a * b
	if r/a != b {
		return 0, ErrOverflow
	}
	return r, nil
}

func Add(a, b int64) (int64, error) {
	r := a + b
	if (b > 0 && r < a) || (b < 0 && r > a) {
		return 0, ErrOverflow
	}
	return r, nil
}

func Sub(a, b int64) (int64, error) {
	return Add(a, -b)
}

func LineOriginal(unitPrice, quantity int64) (int64, error) {
	if unitPrice < 0 || quantity <= 0 {
		return 0, fmt.Errorf("%w: unit_price/quantity", ErrInvalidArgument)
	}
	return Mul(unitPrice, quantity)
}

func SumOriginal(lines []OrderLine) (int64, error) {
	var sum int64
	for i := range lines {
		orig, err := LineOriginal(lines[i].UnitPrice, lines[i].Quantity)
		if err != nil {
			return 0, err
		}
		if lines[i].OriginalAmount != 0 && lines[i].OriginalAmount != orig {
			return 0, fmt.Errorf("%w: line %s original mismatch", ErrAmountInvariant, lines[i].LineID)
		}
		lines[i].OriginalAmount = orig
		sum, err = Add(sum, orig)
		if err != nil {
			return 0, err
		}
	}
	return sum, nil
}

func BuildAmounts(currency string, original, discount, ledgerPay int64) (Amounts, error) {
	if currency == "" {
		return Amounts{}, fmt.Errorf("%w: currency required", ErrInvalidArgument)
	}
	if original < 0 || discount < 0 || ledgerPay < 0 {
		return Amounts{}, fmt.Errorf("%w: negative amount", ErrInvalidArgument)
	}
	if discount > original {
		return Amounts{}, fmt.Errorf("%w: discount > original", ErrAmountInvariant)
	}
	payable, err := Sub(original, discount)
	if err != nil {
		return Amounts{}, err
	}
	if ledgerPay > payable {
		return Amounts{}, fmt.Errorf("%w: ledger_pay > payable", ErrAmountInvariant)
	}
	channelPay, err := Sub(payable, ledgerPay)
	if err != nil {
		return Amounts{}, err
	}
	a := Amounts{
		Currency:   currency,
		Original:   original,
		Discount:   discount,
		Payable:    payable,
		LedgerPay:  ledgerPay,
		ChannelPay: channelPay,
	}
	return a, a.Validate()
}

func (a Amounts) Validate() error {
	if a.Original < 0 || a.Discount < 0 || a.Payable < 0 || a.LedgerPay < 0 || a.ChannelPay < 0 || a.Paid < 0 || a.Refunded < 0 {
		return fmt.Errorf("%w: negative", ErrAmountInvariant)
	}
	if a.Discount > a.Original {
		return fmt.Errorf("%w: discount > original", ErrAmountInvariant)
	}
	if a.Original-a.Discount != a.Payable {
		return fmt.Errorf("%w: payable != original - discount", ErrAmountInvariant)
	}
	if a.LedgerPay+a.ChannelPay != a.Payable {
		return fmt.Errorf("%w: payable != ledger + channel", ErrAmountInvariant)
	}
	if a.Paid > a.Payable {
		return fmt.Errorf("%w: paid > payable", ErrAmountInvariant)
	}
	if a.Refunded > a.Paid {
		return fmt.Errorf("%w: refunded > paid", ErrAmountInvariant)
	}
	return nil
}

func AllocateDiscount(lines []OrderLine, totalDiscount int64) error {
	if totalDiscount < 0 {
		return ErrInvalidArgument
	}
	var original int64
	var err error
	for i := range lines {
		if lines[i].OriginalAmount == 0 {
			lines[i].OriginalAmount, err = LineOriginal(lines[i].UnitPrice, lines[i].Quantity)
			if err != nil {
				return err
			}
		}
		original, err = Add(original, lines[i].OriginalAmount)
		if err != nil {
			return err
		}
	}
	if totalDiscount > original {
		return fmt.Errorf("%w: discount > original", ErrAmountInvariant)
	}
	if original == 0 {
		for i := range lines {
			lines[i].DiscountAmount = 0
			lines[i].PayableAmount = 0
		}
		return nil
	}
	var allocated int64
	last := len(lines) - 1
	for i := range lines {
		var d int64
		if i == last {
			d = totalDiscount - allocated
		} else {
			d = lines[i].OriginalAmount * totalDiscount / original
			allocated += d
		}
		lines[i].DiscountAmount = d
		lines[i].PayableAmount = lines[i].OriginalAmount - d
	}
	return nil
}
