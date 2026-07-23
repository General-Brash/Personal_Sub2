//go:build integration

package repository

import (
	"context"
	"strconv"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestFinancialLedgerAggregatesUsageByBeijingDayAndRequestedModel(t *testing.T) {
	ctx := context.Background()
	user := newTemporaryCreditTestUser(t)
	account, err := integrationEntClient.Account.Create().
		SetName("financial-ledger-" + uuid.NewString()).
		SetPlatform("anthropic").
		SetType("api_key").
		Save(ctx)
	require.NoError(t, err)
	apiKey, err := integrationEntClient.APIKey.Create().
		SetUserID(user.ID).
		SetKey("sk-financial-ledger-" + uuid.NewString()).
		SetName("financial-ledger").
		Save(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM usage_logs WHERE user_id = $1", user.ID)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM api_keys WHERE id = $1", apiKey.ID)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM accounts WHERE id = $1", account.ID)
	})

	beijing, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	now := time.Now().In(beijing)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, beijing).AddDate(0, 0, -1)
	for index, entry := range []struct {
		model, requested string
		cost             float64
	}{
		{model: "provider-branch-a", requested: "gpt-5", cost: 0.1},
		{model: "gpt-5", requested: "", cost: 0.2},
	} {
		_, err := integrationEntClient.UsageLog.Create().
			SetUserID(user.ID).
			SetAPIKeyID(apiKey.ID).
			SetAccountID(account.ID).
			SetRequestID("financial-ledger-" + uuid.NewString()).
			SetModel(entry.model).
			SetRequestedModel(entry.requested).
			SetTotalCost(entry.cost).
			SetCreatedAt(day.Add(time.Duration(10+index) * time.Hour)).
			Save(ctx)
		require.NoError(t, err)
	}

	mall := service.NewMallService(integrationDB, nil, nil)
	ledger, err := mall.GetFinancialLedger(ctx, user.ID, 1, 7, "model")
	require.NoError(t, err)
	require.Equal(t, int64(1), ledger.Total)
	require.Len(t, ledger.Items, 1)
	item := ledger.Items[0]
	require.Negative(t, item.ID)
	require.Equal(t, "usage:"+strconv.FormatInt(item.ID, 10), item.RowID)
	require.Equal(t, "gpt-5模型费用", item.Label)
	require.Equal(t, "gpt-5", *item.Model)
	require.Empty(t, item.Currency)
	require.Equal(t, service.LedgerUnitCredit, item.Unit)
	require.Equal(t, "0.30000000", item.CostAmount)
	require.Equal(t, int64(2), item.Count)
	require.Equal(t, int64(2), ledger.Windows["seven_days"].Count)
	require.Equal(t, "0.30000000", ledger.Windows["seven_days"].TotalAmount)
	require.Equal(t, map[string]string{"credit:": "0.30000000"}, financialTotalsByUnit(ledger.Windows["seven_days"].Totals))
}

func TestBankLedgerSnapshotTriggerCapturesRepresentativeBalances(t *testing.T) {
	ctx := context.Background()
	user := newTemporaryCreditTestUser(t)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM bank_ledger WHERE user_id = $1", user.ID)
	})

	var triggerInstalled bool
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1 FROM pg_trigger
    WHERE tgrelid = 'bank_ledger'::regclass
      AND tgname = 'bank_ledger_balance_snapshot_trigger'
      AND NOT tgisinternal
)`).Scan(&triggerInstalled))
	require.True(t, triggerInstalled)
	require.NoError(t, setBankSnapshotUserState(ctx, user.ID, "100.00000000", "0.00000000"))

	exchangeGrantID := insertBankSnapshotGrant(t, user.ID, "bank_exchange", "10.00000000")
	require.NoError(t, setBankSnapshotUserState(ctx, user.ID, "95.00000000", "0.00000000"))
	exchangeLedgerID := insertBankSnapshotLedger(t, user.ID, exchangeGrantID, "exchange", "-5.00000000", "10.00000000", "0.00000000", "0.00000000", "0.00000000")
	assertBankSnapshot(t, exchangeLedgerID, 100, 95, 0, 10)

	advanceGrantID := insertBankSnapshotGrant(t, user.ID, "bank_advance", "20.00000000")
	require.NoError(t, setBankSnapshotUserState(ctx, user.ID, "95.00000000", "20.00000000"))
	advanceLedgerID := insertBankSnapshotLedger(t, user.ID, advanceGrantID, "advance", "0.00000000", "20.00000000", "20.00000000", "0.00000000", "20.00000000")
	assertBankSnapshot(t, advanceLedgerID, 95, 95, 10, 30)

	_, err := integrationDB.ExecContext(ctx, `
UPDATE temporary_credit_grants
SET remaining_amount = remaining_amount - 5, updated_at = clock_timestamp()
WHERE id = $1`, exchangeGrantID)
	require.NoError(t, err)
	require.NoError(t, setBankSnapshotUserState(ctx, user.ID, "95.00000000", "15.00000000"))
	repayLedgerID := insertBankSnapshotLedger(t, user.ID, advanceGrantID, "early_repay_temporary", "0.00000000", "-5.00000000", "-5.00000000", "20.00000000", "15.00000000")
	assertBankSnapshot(t, repayLedgerID, 95, 95, 30, 25)
}

func insertBankSnapshotGrant(t *testing.T, userID int64, source, amount string) int64 {
	t.Helper()
	var id int64
	require.NoError(t, integrationDB.QueryRow(`
INSERT INTO temporary_credit_grants
    (user_id, source, amount, remaining_amount, available_at, expires_at, notes)
VALUES ($1, $2, $3, $3, clock_timestamp() - INTERVAL '1 minute', clock_timestamp() + INTERVAL '1 day', '')
RETURNING id`, userID, source, amount).Scan(&id))
	return id
}

func insertBankSnapshotLedger(t *testing.T, userID, grantID int64, operation, permanentDelta, temporaryDelta, debtDelta, debtBefore, debtAfter string) int64 {
	t.Helper()
	var id int64
	require.NoError(t, integrationDB.QueryRow(`
INSERT INTO bank_ledger
    (user_id, operation, grant_id, actor_id, permanent_delta, temporary_delta,
     debt_delta, debt_before, debt_after)
VALUES ($1, $2, $3, $1, $4, $5, $6, $7, $8)
RETURNING id`, userID, operation, grantID, permanentDelta, temporaryDelta, debtDelta, debtBefore, debtAfter).Scan(&id))
	return id
}

func setBankSnapshotUserState(ctx context.Context, userID int64, permanent, debt string) error {
	_, err := integrationDB.ExecContext(ctx, `
UPDATE users
SET balance = $1, temporary_credit_debt = $2, updated_at = clock_timestamp()
WHERE id = $3`, permanent, debt, userID)
	return err
}

func assertBankSnapshot(t *testing.T, ledgerID int64, permanentBefore, permanentAfter, temporaryBefore, temporaryAfter float64) {
	t.Helper()
	var gotPermanentBefore, gotPermanentAfter, gotTemporaryBefore, gotTemporaryAfter float64
	require.NoError(t, integrationDB.QueryRow(`
SELECT permanent_balance_before, permanent_balance_after,
       temporary_balance_before, temporary_balance_after
FROM bank_ledger
WHERE id = $1`, ledgerID).Scan(
		&gotPermanentBefore,
		&gotPermanentAfter,
		&gotTemporaryBefore,
		&gotTemporaryAfter,
	))
	require.Equal(t, permanentBefore, gotPermanentBefore)
	require.Equal(t, permanentAfter, gotPermanentAfter)
	require.Equal(t, temporaryBefore, gotTemporaryBefore)
	require.Equal(t, temporaryAfter, gotTemporaryAfter)
}

func TestMallTransactionsAndLedgerIncludeOnlyCompletedExternalProducts(t *testing.T) {
	ctx := context.Background()
	user := newTemporaryCreditTestUser(t)
	registerMallUserCleanup(t, user.ID)
	require.NoError(t, setMallUserBalance(ctx, user.ID, "20.00000000"))
	currencyProductID := createMallCurrencyProduct(t, service.MallCreditTypePermanent, service.MallCreditTypePermanent, "1.00000000", "2.00000000", 0)
	groupID := createMallSubscriptionGroup(t)
	planID := createMallSub2Plan(t, groupID, "5.00000000", 7)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM payment_orders WHERE user_id = $1", user.ID)
	})

	mall, coordinator := newMallIntegrationServices()
	_, err := executeMallAtomic(ctx, coordinator, mall, user.ID, "financial-ledger-internal-"+uuid.NewString(), service.MallPurchaseRequest{
		ProductType: service.MallProductTypeCurrency,
		ProductID:   currencyProductID,
	})
	require.NoError(t, err)

	completedCurrency := createFinancialLedgerPaymentOrder(t, user, service.OrderStatusCompleted, payment.OrderTypeBalance, currencyProductID, 0, 1, 2, "CNY")
	completedSubscription := createFinancialLedgerPaymentOrder(t, user, service.OrderStatusCompleted, payment.OrderTypeSubscription, 0, planID, 5, 0, "USD")
	_ = createFinancialLedgerPaymentOrder(t, user, service.OrderStatusPending, payment.OrderTypeBalance, currencyProductID, 0, 1, 2, "CNY")
	_ = createFinancialLedgerPaymentOrder(t, user, service.OrderStatusCompleted, payment.OrderTypeBalance, 0, 0, 9, 0, "CNY")

	items, total, err := mall.ListMallTransactions(ctx, user.ID, 1, "")
	require.NoError(t, err)
	require.Equal(t, int64(3), total)
	require.Len(t, items, 3)
	sourceCounts := map[string]int{}
	var externalCurrency, externalSubscription *service.MallTransactionItem
	for index := range items {
		item := &items[index]
		sourceCounts[item.Source]++
		if item.ID == -completedCurrency.ID {
			externalCurrency = item
		}
		if item.ID == -completedSubscription.ID {
			externalSubscription = item
		}
	}
	require.Equal(t, 1, sourceCounts["mall_purchase"])
	require.Equal(t, 2, sourceCounts["payment_order"])
	require.NotNil(t, externalCurrency)
	require.Equal(t, "payment_order:"+strconv.FormatInt(-completedCurrency.ID, 10), externalCurrency.RowID)
	require.Equal(t, "currency", externalCurrency.ProductType)
	require.Equal(t, "CNY", externalCurrency.Currency)
	require.Equal(t, service.LedgerUnitFiat, externalCurrency.Unit)
	require.Equal(t, "1.00000000", externalCurrency.Price)
	require.Equal(t, "2.00000000", externalCurrency.PermanentCreditedAmount)
	require.Nil(t, externalCurrency.PermanentBalanceBefore)
	require.Nil(t, externalCurrency.PermanentBalanceAfter)
	require.Nil(t, externalCurrency.TemporaryBalanceBefore)
	require.Nil(t, externalCurrency.TemporaryBalanceAfter)
	require.NotNil(t, externalSubscription)
	require.Equal(t, "subscription", externalSubscription.ProductType)
	require.Equal(t, "USD", externalSubscription.Currency)
	require.Equal(t, service.LedgerUnitFiat, externalSubscription.Unit)
	require.Equal(t, "5.00000000", externalSubscription.Price)
	currencyItems, currencyTotal, err := mall.ListMallTransactions(ctx, 0, 1, "currency")
	require.NoError(t, err)
	require.Equal(t, int64(2), currencyTotal)
	require.Len(t, currencyItems, 2)

	sales, err := mall.GetMallSalesCounts(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(2), sales[service.MallSalesKey{ProductType: service.MallProductTypeCurrency, ProductID: currencyProductID}])
	require.Equal(t, int64(1), sales[service.MallSalesKey{ProductType: service.MallProductTypeSubscription, ProductID: planID}])
	analytics, err := mall.GetMallSalesAnalytics(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, int64(3), analytics.TotalSales)
	require.Empty(t, analytics.TotalRevenue)
	require.Empty(t, analytics.CurrencyRevenue)
	require.Equal(t, "5.00000000", analytics.SubscriptionRevenue)
	require.Equal(t, map[string]string{
		"credit:":  "1.00000000",
		"fiat:CNY": "1.00000000",
		"fiat:USD": "5.00000000",
	}, mallRevenueTotalsByUnit(analytics.RevenueTotals))

	ledger, err := mall.GetFinancialLedger(ctx, user.ID, 1, 1, "mall")
	require.NoError(t, err)
	require.Equal(t, int64(3), ledger.Total)
	require.Len(t, ledger.Items, 3)
	require.Equal(t, int64(3), ledger.Windows["today"].Count)
	require.Equal(t, "1.00000000", ledger.Windows["today"].TotalAmount)
	require.Equal(t, map[string]string{
		"credit:":  "1.00000000",
		"fiat:CNY": "1.00000000",
		"fiat:USD": "5.00000000",
	}, financialTotalsByUnit(ledger.Windows["today"].Totals))
	rowIDs := make(map[string]struct{}, len(ledger.Items))
	for _, item := range ledger.Items {
		require.NotEmpty(t, item.RowID)
		require.Equal(t, item.Source+":"+strconv.FormatInt(item.ID, 10), item.RowID)
		_, exists := rowIDs[item.RowID]
		require.False(t, exists, "row_id must be unique across ledger sources")
		rowIDs[item.RowID] = struct{}{}
	}
	allSite, err := mall.GetAdminFinancialLedger(ctx, 0, 1, 1, "mall")
	require.NoError(t, err)
	require.Equal(t, int64(3), allSite.Total)
	require.Equal(t, int64(3), allSite.Windows["today"].Count)
}

func createFinancialLedgerPaymentOrder(t *testing.T, user *service.User, status, orderType string, currencyProductID, planID int64, price, credited float64, currency string) *dbent.PaymentOrder {
	t.Helper()
	now := time.Now().UTC()
	builder := integrationEntClient.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(func() float64 {
			if currencyProductID > 0 {
				return credited
			}
			return price
		}()).
		SetPayAmount(price).
		SetFeeRate(0).
		SetRechargeCode("FINANCIAL-" + uuid.NewString()).
		SetOutTradeNo("financial_" + uuid.NewString()).
		SetPaymentType(payment.TypeAlipay).
		SetProviderSnapshot(map[string]any{"currency": currency}).
		SetPaymentTradeNo("trade-" + uuid.NewString()).
		SetOrderType(orderType).
		SetStatus(status).
		SetExpiresAt(now.Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("financial-ledger.test")
	if status == service.OrderStatusCompleted {
		builder.SetPaidAt(now).SetCompletedAt(now)
	}
	if currencyProductID > 0 {
		builder.SetCurrencyProductID(currencyProductID).
			SetCurrencyProductName("External currency").
			SetCurrencyProductPaymentPrice(price).
			SetCurrencyProductCreditedAmount(credited)
	}
	if planID > 0 {
		builder.SetPlanID(planID).SetSubscriptionDays(7)
	}
	order, err := builder.Save(context.Background())
	require.NoError(t, err)
	return order
}

func mallRevenueTotalsByUnit(totals []service.MallRevenueTotal) map[string]string {
	out := make(map[string]string, len(totals))
	for _, total := range totals {
		out[total.Unit+":"+total.Currency] = total.Revenue
	}
	return out
}

func financialTotalsByUnit(totals []service.FinancialLedgerTotal) map[string]string {
	out := make(map[string]string, len(totals))
	for _, total := range totals {
		out[total.Unit+":"+total.Currency] = total.Amount
	}
	return out
}
