package service

import (
	"strings"
	"testing"
	"time"
)

func TestFinancialLedgerCTEAggregatesUsageByBeijingDayAndRequestedModel(t *testing.T) {
	start := time.Date(2026, 7, 9, 16, 0, 0, 0, time.UTC)
	end := time.Date(2026, 7, 24, 16, 0, 0, 0, time.UTC)
	cte := financialLedgerCTE(42, start, end)

	for _, fragment := range []string{
		"md5(concat_ws('|',",
		"COALESCE(NULLIF(ul.requested_model, ''), ul.model) || '模型费用' AS label",
		"SUM(ul.total_cost) AS amount",
		"COUNT(*)::bigint AS item_count",
		"(ul.created_at AT TIME ZONE 'Asia/Shanghai')::date",
		"COALESCE(NULLIF(ul.requested_model, ''), ul.model)",
		"AND ul.user_id = $1",
		"AND ul.created_at >= $2 AND ul.created_at < $3",
		"WHERE mp.status = 'completed'",
		"AND mp.created_at >= $2 AND mp.created_at < $3",
		"WHERE po.status = 'COMPLETED'",
		"'payment_order', 'mall'",
		"COALESCE(NULLIF(UPPER(BTRIM(po.provider_snapshot->>'currency')), ''), 'CNY'), 'fiat'::text",
		"''::text AS currency, 'credit'::text AS unit",
		"AND COALESCE(po.completed_at, po.paid_at, po.created_at) >= $2",
	} {
		if !strings.Contains(cte.sql, fragment) {
			t.Fatalf("financial ledger CTE missing %q", fragment)
		}
	}
	if strings.Contains(cte.sql, "MAX(ul.id) AS id") || strings.Contains(cte.sql, "ul.id AS id") {
		t.Fatal("aggregated model rows must not expose a branch-level usage_logs id")
	}
	if len(cte.args) != 3 || cte.args[0] != int64(42) || cte.args[1] != start || cte.args[2] != end {
		t.Fatalf("unexpected CTE args: %#v", cte.args)
	}
}

func TestFinancialLedgerWindowsQueryAggregatesInsideDatabase(t *testing.T) {
	todayStart := time.Date(2026, 7, 23, 0, 0, 0, 0, beijingLocation)
	sevenStart := todayStart.AddDate(0, 0, -6)
	fifteenStart := todayStart.AddDate(0, 0, -14)
	end := todayStart.AddDate(0, 0, 1)
	cte := financialLedgerCTE(0, fifteenStart, end)
	query, args := financialLedgerWindowsQuery(cte, todayStart, sevenStart, fifteenStart)

	for _, fragment := range []string{
		"requested_windows(window_name, start_at)",
		"SUM(ledger.cost_amount)::text",
		"SUM(ledger.item_count)::bigint",
		"ledger.currency, ledger.unit",
		"GROUP BY requested_windows.window_name, ledger.category, ledger.label, ledger.currency, ledger.unit",
	} {
		if !strings.Contains(query, fragment) {
			t.Fatalf("financial window query missing %q", fragment)
		}
	}
	if len(args) != 5 {
		t.Fatalf("window query args = %#v, want five bounded parameters", args)
	}
}

func TestMallTransactionsCTEIncludesOnlyCompletedDisjointSources(t *testing.T) {
	sql := mallTransactionsCTE()
	for _, fragment := range []string{
		"WHERE purchase.status = 'completed'",
		"WHERE payment_order.status = 'COMPLETED'",
		"payment_order.currency_product_id IS NOT NULL",
		"payment_order.order_type = 'subscription' AND payment_order.plan_id IS NOT NULL",
		"'mall_purchase'::text AS source",
		"'payment_order'::text",
		"SELECT -payment_order.id",
		"COALESCE(NULLIF(UPPER(BTRIM(payment_order.provider_snapshot->>'currency')), ''), 'CNY')::text",
		"'fiat'::text",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("mall transaction CTE missing %q", fragment)
		}
	}
}

func TestFinancialLedgerRowIDIsStableAndSourceQualified(t *testing.T) {
	mall := financialLedgerRowID("mall", 7)
	bank := financialLedgerRowID("bank", 7)
	payment := financialLedgerRowID("payment_order", -7)

	if mall != "mall:7" || bank != "bank:7" || payment != "payment_order:-7" {
		t.Fatalf("unexpected row ids: %q %q %q", mall, bank, payment)
	}
	if mall == bank || mall == payment || bank == payment {
		t.Fatal("row ids must stay unique when source-local numeric ids overlap")
	}
}

func TestMallRevenueBucketsDoNotMixCreditsAndCurrencies(t *testing.T) {
	buckets := make(map[string]MallRevenueTotal)
	addMallRevenueBucket(buckets, "", LedgerUnitCredit, "2.00000000", 1)
	addMallRevenueBucket(buckets, "CNY", LedgerUnitFiat, "3.00000000", 2)
	addMallRevenueBucket(buckets, "USD", LedgerUnitFiat, "4.00000000", 3)
	totals := sortedMallRevenueTotals(buckets)

	if len(totals) != 3 {
		t.Fatalf("totals = %#v, want three unit buckets", totals)
	}
	if got := singleMallRevenue(totals); got != "" {
		t.Fatalf("mixed-unit legacy revenue = %q, want empty", got)
	}
}

func TestFinancialLedgerLegacyTotalIncludesCreditsOnly(t *testing.T) {
	window := FinancialLedgerWindow{TotalAmount: "0.00000000", Totals: []FinancialLedgerTotal{}}
	addFinancialLedgerTotal(&window, "", LedgerUnitCredit, "2.00000000", 1)
	addFinancialLedgerTotal(&window, "CNY", LedgerUnitFiat, "3.00000000", 1)
	addFinancialLedgerTotal(&window, "USD", LedgerUnitFiat, "4.00000000", 1)

	if window.TotalAmount != "2.00000000" {
		t.Fatalf("legacy credit total = %q, want 2.00000000", window.TotalAmount)
	}
	if len(window.Totals) != 3 {
		t.Fatalf("totals = %#v, want three unit buckets", window.Totals)
	}
}

func TestFinancialLedgerBankLabelsRemainLocalizableOperationKeys(t *testing.T) {
	cte := financialLedgerCTE(0, time.Time{}, time.Now())
	if !strings.Contains(cte.sql, "bl.operation,\n") {
		t.Fatal("bank ledger rows must expose the operation as their stable label")
	}
	if strings.Contains(cte.sql, "'银行' || bl.operation") || strings.Contains(cte.sql, "'银行结算'") {
		t.Fatal("bank ledger labels must not mix localized text with operation keys")
	}
}
