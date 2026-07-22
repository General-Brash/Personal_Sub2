//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

type currencyProductRedeemRepo struct {
	*redeemCodeRepoStub
	nextID int64
}

func (r *currencyProductRedeemRepo) Create(_ context.Context, code *RedeemCode) error {
	r.nextID++
	code.ID = r.nextID
	if r.codesByCode == nil {
		r.codesByCode = map[string]*RedeemCode{}
	}
	copy := *code
	r.codesByCode[code.Code] = &copy
	return nil
}

func (r *currencyProductRedeemRepo) GetByID(_ context.Context, id int64) (*RedeemCode, error) {
	for _, code := range r.codesByCode {
		if code.ID == id {
			copy := *code
			return &copy, nil
		}
	}
	return nil, ErrRedeemCodeNotFound
}

func TestCurrencyProductBalanceFulfillmentUsesEightDecimalSnapshot(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	user, err := client.User.Create().SetEmail("currency-fulfillment@example.com").SetPasswordHash("hash").SetUsername("currency-fulfillment").Save(ctx)
	require.NoError(t, err)
	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).SetUserEmail(user.Email).SetUserName(user.Username).
		SetAmount(12.34567891).SetPayAmount(12.34).SetFeeRate(0).
		SetRechargeCode("PAY-CURRENCY-PRECISE").SetOutTradeNo("sub2_currency_precise").
		SetPaymentType(payment.TypeAlipay).SetPaymentTradeNo("trade-currency").
		SetOrderType(payment.OrderTypeBalance).SetStatus(OrderStatusPaid).
		SetCurrencyProductID(7).SetCurrencyProductName("Exact balance").
		SetCurrencyProductPaymentPrice(12.34).SetCurrencyProductCreditedAmount(12.34567891).
		SetExpiresAt(time.Now().Add(time.Hour)).SetClientIP("127.0.0.1").SetSrcHost("example.com").Save(ctx)
	require.NoError(t, err)

	userRepo := &mockUserRepo{getByIDUser: &User{ID: user.ID, Email: user.Email, Username: user.Username}}
	var updatedBalance float64
	userRepo.updateBalanceFn = func(_ context.Context, _ int64, amount float64) error {
		updatedBalance = amount
		return nil
	}
	redeemRepo := &currencyProductRedeemRepo{redeemCodeRepoStub: &redeemCodeRepoStub{codesByCode: map[string]*RedeemCode{}}}
	svc := &PaymentService{
		entClient: client,
		userRepo:  userRepo,
		redeemService: &RedeemService{
			redeemRepo: redeemRepo,
			userRepo:   userRepo,
			entClient:  client,
		},
	}

	require.NoError(t, svc.ExecuteBalanceFulfillment(ctx, order.ID))
	require.Equal(t, "12.34567891", formatLedgerAmount(updatedBalance))
	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, reloaded.Status)
	require.Equal(t, "12.34567891", formatLedgerAmount(reloaded.Amount))
	code := redeemRepo.codesByCode[order.RechargeCode]
	require.NotNil(t, code)
	require.Equal(t, "12.34567891", formatLedgerAmount(code.Value))
}
