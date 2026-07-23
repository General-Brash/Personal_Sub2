package service

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

const financialLedgerPageSize = 20

const (
	LedgerUnitCredit = "credit"
	LedgerUnitFiat   = "fiat"
)

type MallSalesKey struct {
	ProductType MallProductType
	ProductID   int64
}

type MallSalesCount struct {
	ProductType MallProductType `json:"product_type"`
	ProductID   int64           `json:"product_id"`
	SalesCount  int64           `json:"sales_count"`
}

type MallTransactionItem struct {
	ID                      int64      `json:"id"`
	RowID                   string     `json:"row_id"`
	UserID                  int64      `json:"user_id"`
	Username                string     `json:"username"`
	Email                   string     `json:"email,omitempty"`
	Source                  string     `json:"source"`
	ProductType             string     `json:"product_type"`
	ProductID               int64      `json:"product_id"`
	ProductName             string     `json:"product_name"`
	PaymentCreditType       string     `json:"payment_credit_type"`
	Currency                string     `json:"currency"`
	Unit                    string     `json:"unit"`
	Price                   string     `json:"price"`
	PermanentCreditedAmount string     `json:"permanent_credited_amount"`
	TemporaryCreditedAmount string     `json:"temporary_credited_amount"`
	PermanentBalanceBefore  *string    `json:"permanent_balance_before,omitempty"`
	PermanentBalanceAfter   *string    `json:"permanent_balance_after,omitempty"`
	TemporaryBalanceBefore  *string    `json:"temporary_balance_before,omitempty"`
	TemporaryBalanceAfter   *string    `json:"temporary_balance_after,omitempty"`
	SubscriptionExpiresAt   *time.Time `json:"subscription_expires_at,omitempty"`
	Status                  string     `json:"status"`
	CreatedAt               time.Time  `json:"created_at"`
}

func mallTransactionsCTE() string {
	return `
WITH transactions AS (
    SELECT purchase.id, purchase.user_id,
           COALESCE(NULLIF(users.username, ''), users.email) AS username,
           users.email, 'mall_purchase'::text AS source,
           purchase.product_type, purchase.product_id,
           COALESCE(NULLIF(purchase.product_name, ''),
               CASE WHEN purchase.product_type = 'currency' THEN currency_product.name ELSE subscription_plan.name END,
               purchase.product_type || '#' || purchase.product_id::text) AS product_name,
           purchase.payment_credit_type, ''::text AS currency, 'credit'::text AS unit, purchase.price,
           CASE WHEN purchase.product_type = 'currency' AND purchase.credited_type = 'permanent'
                THEN purchase.credited_amount ELSE 0 END AS permanent_credited_amount,
           CASE WHEN purchase.product_type = 'currency' AND purchase.credited_type = 'temporary'
                THEN purchase.credited_amount
                WHEN purchase.product_type = 'subscription' AND purchase.benefit_type = 'daily_temporary_credit'
                THEN COALESCE(purchase.daily_temporary_credit_amount, 0) * COALESCE(purchase.benefit_days, 0)
                ELSE 0 END AS temporary_credited_amount,
           purchase.permanent_balance_before, purchase.permanent_balance_after,
           purchase.temporary_balance_before, purchase.temporary_balance_after,
           purchase.subscription_expires_at, 'completed'::text AS status, purchase.created_at
    FROM mall_purchases AS purchase
    JOIN users ON users.id = purchase.user_id
    LEFT JOIN currency_products AS currency_product
      ON purchase.product_type = 'currency' AND currency_product.id = purchase.product_id
    LEFT JOIN subscription_plans AS subscription_plan
      ON purchase.product_type = 'subscription' AND subscription_plan.id = purchase.product_id
    WHERE purchase.status = 'completed'

    UNION ALL

    SELECT -payment_order.id, payment_order.user_id,
           COALESCE(NULLIF(users.username, ''), NULLIF(payment_order.user_name, ''), users.email, payment_order.user_email),
           COALESCE(NULLIF(payment_order.user_email, ''), users.email, ''),
           'payment_order'::text,
           CASE WHEN payment_order.currency_product_id IS NOT NULL THEN 'currency' ELSE 'subscription' END,
           COALESCE(payment_order.currency_product_id, payment_order.plan_id),
           CASE
               WHEN payment_order.currency_product_id IS NOT NULL THEN
                   COALESCE(NULLIF(payment_order.currency_product_name, ''), currency_product.name,
                       'currency#' || payment_order.currency_product_id::text)
               ELSE COALESCE(NULLIF(subscription_plan.name, ''), 'subscription#' || payment_order.plan_id::text)
           END,
           COALESCE(NULLIF(payment_order.payment_type, ''), 'external'),
           COALESCE(NULLIF(UPPER(BTRIM(payment_order.provider_snapshot->>'currency')), ''), 'CNY')::text,
           'fiat'::text,
           CASE WHEN payment_order.currency_product_id IS NOT NULL
                THEN COALESCE(payment_order.currency_product_payment_price, payment_order.amount)
                ELSE payment_order.amount END,
           CASE WHEN payment_order.currency_product_id IS NOT NULL
                THEN COALESCE(payment_order.currency_product_credited_amount, payment_order.amount)
                ELSE 0 END,
           0::numeric,
           NULL::numeric, NULL::numeric, NULL::numeric, NULL::numeric,
           NULL::timestamptz, 'completed'::text,
           COALESCE(payment_order.completed_at, payment_order.paid_at, payment_order.created_at)
    FROM payment_orders AS payment_order
    LEFT JOIN users ON users.id = payment_order.user_id
    LEFT JOIN currency_products AS currency_product
      ON payment_order.currency_product_id IS NOT NULL AND currency_product.id = payment_order.currency_product_id
    LEFT JOIN subscription_plans AS subscription_plan
      ON payment_order.currency_product_id IS NULL AND subscription_plan.id = payment_order.plan_id
    WHERE payment_order.status = 'COMPLETED'
      AND (
          payment_order.currency_product_id IS NOT NULL
          OR (payment_order.order_type = 'subscription' AND payment_order.plan_id IS NOT NULL)
      )
)
`
}

type MallSalesDailyStat struct {
	Date       string `json:"date"`
	SalesCount int64  `json:"sales_count"`
	Revenue    string `json:"revenue"`
	Currency   string `json:"currency"`
	Unit       string `json:"unit"`
}

type MallSalesProductStat struct {
	ProductType string `json:"product_type"`
	ProductID   int64  `json:"product_id"`
	ProductName string `json:"product_name"`
	SalesCount  int64  `json:"sales_count"`
	Revenue     string `json:"revenue"`
	Currency    string `json:"currency"`
	Unit        string `json:"unit"`
}

type MallRevenueTotal struct {
	Currency   string `json:"currency"`
	Unit       string `json:"unit"`
	Revenue    string `json:"revenue"`
	SalesCount int64  `json:"sales_count"`
}

type MallSalesAnalytics struct {
	Days                int                    `json:"days"`
	TotalSales          int64                  `json:"total_sales"`
	TotalRevenue        string                 `json:"total_revenue"`
	CurrencySales       int64                  `json:"currency_sales"`
	SubscriptionSales   int64                  `json:"subscription_sales"`
	CurrencyRevenue     string                 `json:"currency_revenue"`
	SubscriptionRevenue string                 `json:"subscription_revenue"`
	RevenueTotals       []MallRevenueTotal     `json:"revenue_totals"`
	Daily               []MallSalesDailyStat   `json:"daily"`
	Products            []MallSalesProductStat `json:"products"`
}

type FinancialLedgerCategory struct {
	Category string `json:"category"`
	Label    string `json:"label"`
	Amount   string `json:"amount"`
	Count    int64  `json:"count"`
	Currency string `json:"currency"`
	Unit     string `json:"unit"`
}

type FinancialLedgerTotal struct {
	Currency string `json:"currency"`
	Unit     string `json:"unit"`
	Amount   string `json:"amount"`
	Count    int64  `json:"count"`
}

type FinancialLedgerWindow struct {
	TotalAmount string                    `json:"total_amount"`
	Count       int64                     `json:"count"`
	Categories  []FinancialLedgerCategory `json:"categories"`
	Totals      []FinancialLedgerTotal    `json:"totals"`
}

type FinancialLedgerItem struct {
	ID                     int64     `json:"id"`
	RowID                  string    `json:"row_id"`
	UserID                 int64     `json:"user_id"`
	Username               string    `json:"username,omitempty"`
	Email                  string    `json:"email,omitempty"`
	Source                 string    `json:"source"`
	Category               string    `json:"category"`
	Label                  string    `json:"label"`
	Amount                 string    `json:"amount"`
	CostAmount             string    `json:"cost_amount"`
	Currency               string    `json:"currency"`
	Unit                   string    `json:"unit"`
	ProductType            *string   `json:"product_type,omitempty"`
	ProductID              *int64    `json:"product_id,omitempty"`
	Operation              *string   `json:"operation,omitempty"`
	Model                  *string   `json:"model,omitempty"`
	PermanentDelta         *string   `json:"permanent_delta,omitempty"`
	TemporaryDelta         *string   `json:"temporary_delta,omitempty"`
	DebtDelta              *string   `json:"debt_delta,omitempty"`
	PermanentBalanceBefore *string   `json:"permanent_balance_before,omitempty"`
	PermanentBalanceAfter  *string   `json:"permanent_balance_after,omitempty"`
	TemporaryBalanceBefore *string   `json:"temporary_balance_before,omitempty"`
	TemporaryBalanceAfter  *string   `json:"temporary_balance_after,omitempty"`
	DebtBefore             *string   `json:"debt_before,omitempty"`
	DebtAfter              *string   `json:"debt_after,omitempty"`
	Count                  int64     `json:"count"`
	CreatedAt              time.Time `json:"created_at"`
}

type FinancialLedgerResponse struct {
	UserID   int64                            `json:"user_id"`
	Timezone string                           `json:"timezone"`
	Days     int                              `json:"days"`
	Windows  map[string]FinancialLedgerWindow `json:"windows"`
	Summary  []FinancialLedgerCategory        `json:"summary"`
	Items    []FinancialLedgerItem            `json:"items"`
	Total    int64                            `json:"total"`
	Page     int                              `json:"page"`
	PageSize int                              `json:"page_size"`
	Pages    int                              `json:"pages"`
}

func (s *MallService) ListMallTransactions(ctx context.Context, userID int64, page int, productType string) ([]MallTransactionItem, int64, error) {
	if s == nil || s.db == nil {
		return nil, 0, fmt.Errorf("mall service database is nil")
	}
	if page < 1 {
		page = 1
	}
	filters := make([]string, 0, 2)
	args := make([]any, 0, 4)
	if userID > 0 {
		args = append(args, userID)
		filters = append(filters, fmt.Sprintf("user_id = $%d", len(args)))
	}
	if strings.TrimSpace(productType) != "" {
		args = append(args, strings.TrimSpace(productType))
		filters = append(filters, fmt.Sprintf("product_type = $%d", len(args)))
	}
	where := ""
	if len(filters) > 0 {
		where = " WHERE " + strings.Join(filters, " AND ")
	}
	var total int64
	if err := s.db.QueryRowContext(ctx, mallTransactionsCTE()+"SELECT COUNT(*) FROM transactions"+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count mall transactions: %w", err)
	}
	query := mallTransactionsCTE() + `
SELECT id, user_id, username, email, source, product_type, product_id, product_name,
       payment_credit_type, currency, unit, price::text, permanent_credited_amount::text, temporary_credited_amount::text,
       permanent_balance_before::text, permanent_balance_after::text,
       temporary_balance_before::text, temporary_balance_after::text,
       subscription_expires_at, status, created_at
FROM transactions` + where
	limitArg := len(args) + 1
	offsetArg := len(args) + 2
	query += fmt.Sprintf(" ORDER BY created_at DESC, source ASC, id DESC LIMIT $%d OFFSET $%d", limitArg, offsetArg)
	args = append(args, financialLedgerPageSize, (page-1)*financialLedgerPageSize)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list mall transactions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]MallTransactionItem, 0, financialLedgerPageSize)
	for rows.Next() {
		var item MallTransactionItem
		var permanentCredited, temporaryCredited, price string
		var permanentBefore, permanentAfter, temporaryBefore, temporaryAfter sql.NullString
		var expiresAt sql.NullTime
		if err := rows.Scan(&item.ID, &item.UserID, &item.Username, &item.Email, &item.Source, &item.ProductType, &item.ProductID,
			&item.ProductName, &item.PaymentCreditType, &item.Currency, &item.Unit, &price, &permanentCredited, &temporaryCredited,
			&permanentBefore, &permanentAfter, &temporaryBefore, &temporaryAfter, &expiresAt, &item.Status, &item.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan mall transaction: %w", err)
		}
		item.Price = normalizeLedgerText(price)
		item.RowID = financialLedgerRowID(item.Source, item.ID)
		item.PermanentCreditedAmount = normalizeLedgerText(permanentCredited)
		item.TemporaryCreditedAmount = normalizeLedgerText(temporaryCredited)
		item.PermanentBalanceBefore = nullableLedgerText(permanentBefore)
		item.PermanentBalanceAfter = nullableLedgerText(permanentAfter)
		item.TemporaryBalanceBefore = nullableLedgerText(temporaryBefore)
		item.TemporaryBalanceAfter = nullableLedgerText(temporaryAfter)
		if expiresAt.Valid {
			value := expiresAt.Time.UTC()
			item.SubscriptionExpiresAt = &value
		}
		item.CreatedAt = item.CreatedAt.UTC()
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate mall transactions: %w", err)
	}
	return items, total, nil
}

func (s *MallService) GetMallSalesCounts(ctx context.Context) (map[MallSalesKey]int64, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("mall service database is nil")
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT product_type, product_id, SUM(sales_count)::bigint
FROM (
    SELECT product_type, product_id, COUNT(*)::bigint AS sales_count
    FROM mall_purchases
    WHERE status = 'completed'
    GROUP BY product_type, product_id
    UNION ALL
    SELECT 'currency', currency_product_id, COUNT(*)::bigint
    FROM payment_orders
    WHERE status = 'COMPLETED' AND currency_product_id IS NOT NULL
    GROUP BY currency_product_id
    UNION ALL
    SELECT 'subscription', plan_id, COUNT(*)::bigint
    FROM payment_orders
    WHERE status = 'COMPLETED' AND order_type = 'subscription' AND plan_id IS NOT NULL
    GROUP BY plan_id
) AS sales
GROUP BY product_type, product_id`)
	if err != nil {
		return nil, fmt.Errorf("load mall sales counts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make(map[MallSalesKey]int64)
	for rows.Next() {
		var productType string
		var productID, count int64
		if err := rows.Scan(&productType, &productID, &count); err != nil {
			return nil, fmt.Errorf("scan mall sales count: %w", err)
		}
		out[MallSalesKey{ProductType: MallProductType(productType), ProductID: productID}] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mall sales counts: %w", err)
	}
	return out, nil
}

func (s *MallService) GetMallSalesAnalytics(ctx context.Context, days int) (*MallSalesAnalytics, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("mall service database is nil")
	}
	if days < 1 {
		days = 30
	}
	if days > 365 {
		days = 365
	}
	end := time.Now().In(beijingLocation)
	end = time.Date(end.Year(), end.Month(), end.Day()+1, 0, 0, 0, 0, beijingLocation)
	start := end.AddDate(0, 0, -days)
	rows, err := s.db.QueryContext(ctx, `
SELECT product_type, product_id, product_name, currency, unit, sales_count::bigint, revenue::text
FROM (
    SELECT product_type, product_id, MAX(product_name) AS product_name, currency, unit,
           COUNT(*)::bigint AS sales_count, SUM(price)::text AS revenue
    FROM (
        SELECT product_type, product_id, product_name, ''::text AS currency, 'credit'::text AS unit,
               price, created_at
        FROM mall_purchases
        WHERE status = 'completed' AND created_at >= $1 AND created_at < $2
        UNION ALL
        SELECT 'currency', currency_product_id,
               COALESCE(NULLIF(currency_product_name, ''), 'currency#' || currency_product_id::text),
               COALESCE(NULLIF(UPPER(BTRIM(provider_snapshot->>'currency')), ''), 'CNY'), 'fiat',
               COALESCE(currency_product_payment_price, amount), COALESCE(completed_at, paid_at, created_at)
        FROM payment_orders
        WHERE status = 'COMPLETED' AND currency_product_id IS NOT NULL
          AND COALESCE(completed_at, paid_at, created_at) >= $1
          AND COALESCE(completed_at, paid_at, created_at) < $2
        UNION ALL
        SELECT 'subscription', payment_orders.plan_id, COALESCE(NULLIF(subscription_plan.name, ''), 'subscription#' || payment_orders.plan_id::text),
               COALESCE(NULLIF(UPPER(BTRIM(payment_orders.provider_snapshot->>'currency')), ''), 'CNY'), 'fiat',
               payment_orders.amount, COALESCE(payment_orders.completed_at, payment_orders.paid_at, payment_orders.created_at)
        FROM payment_orders
        LEFT JOIN subscription_plans AS subscription_plan ON subscription_plan.id = payment_orders.plan_id
        WHERE payment_orders.status = 'COMPLETED' AND payment_orders.order_type = 'subscription' AND payment_orders.plan_id IS NOT NULL
          AND COALESCE(payment_orders.completed_at, payment_orders.paid_at, payment_orders.created_at) >= $1
          AND COALESCE(payment_orders.completed_at, payment_orders.paid_at, payment_orders.created_at) < $2
    ) AS all_sales
    GROUP BY product_type, product_id, currency, unit
) AS products
ORDER BY sales_count DESC, product_type, product_id, unit, currency`, start, end)
	if err != nil {
		return nil, fmt.Errorf("load mall sales analytics products: %w", err)
	}
	defer func() { _ = rows.Close() }()
	analytics := &MallSalesAnalytics{
		Days: days, Daily: []MallSalesDailyStat{}, Products: []MallSalesProductStat{}, RevenueTotals: []MallRevenueTotal{},
	}
	totalBuckets := make(map[string]MallRevenueTotal)
	currencyBuckets := make(map[string]MallRevenueTotal)
	subscriptionBuckets := make(map[string]MallRevenueTotal)
	for rows.Next() {
		var item MallSalesProductStat
		var revenue string
		if err := rows.Scan(&item.ProductType, &item.ProductID, &item.ProductName, &item.Currency, &item.Unit, &item.SalesCount, &revenue); err != nil {
			return nil, fmt.Errorf("scan mall sales analytics product: %w", err)
		}
		item.Revenue = normalizeLedgerText(revenue)
		analytics.Products = append(analytics.Products, item)
		analytics.TotalSales += item.SalesCount
		addMallRevenueBucket(totalBuckets, item.Currency, item.Unit, item.Revenue, item.SalesCount)
		if item.ProductType == "currency" {
			analytics.CurrencySales += item.SalesCount
			addMallRevenueBucket(currencyBuckets, item.Currency, item.Unit, item.Revenue, item.SalesCount)
		} else {
			analytics.SubscriptionSales += item.SalesCount
			addMallRevenueBucket(subscriptionBuckets, item.Currency, item.Unit, item.Revenue, item.SalesCount)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mall sales analytics products: %w", err)
	}
	analytics.RevenueTotals = sortedMallRevenueTotals(totalBuckets)
	analytics.TotalRevenue = singleMallRevenue(analytics.RevenueTotals)
	analytics.CurrencyRevenue = singleMallRevenue(sortedMallRevenueTotals(currencyBuckets))
	analytics.SubscriptionRevenue = singleMallRevenue(sortedMallRevenueTotals(subscriptionBuckets))
	// Daily revenue/counts are intentionally a separate compact query so the
	// product ranking remains index-friendly on large purchase tables.
	dailyRows, err := s.db.QueryContext(ctx, `
	SELECT day::text, currency, unit, COUNT(*)::bigint, SUM(revenue)::text
FROM (
    SELECT (created_at AT TIME ZONE 'Asia/Shanghai')::date AS day,
           ''::text AS currency, 'credit'::text AS unit, price AS revenue
    FROM mall_purchases
    WHERE status = 'completed' AND created_at >= $1 AND created_at < $2
    UNION ALL
    SELECT (COALESCE(completed_at, paid_at, created_at) AT TIME ZONE 'Asia/Shanghai')::date,
           COALESCE(NULLIF(UPPER(BTRIM(provider_snapshot->>'currency')), ''), 'CNY'), 'fiat',
           CASE WHEN currency_product_id IS NOT NULL
                THEN COALESCE(currency_product_payment_price, amount)
                ELSE amount END
    FROM payment_orders
    WHERE status = 'COMPLETED'
      AND ((currency_product_id IS NOT NULL) OR (order_type = 'subscription' AND plan_id IS NOT NULL))
      AND COALESCE(completed_at, paid_at, created_at) >= $1
      AND COALESCE(completed_at, paid_at, created_at) < $2
) AS daily
GROUP BY day, currency, unit ORDER BY day, unit, currency`, start, end)
	if err != nil {
		return nil, fmt.Errorf("load mall sales analytics daily: %w", err)
	}
	defer func() { _ = dailyRows.Close() }()
	for dailyRows.Next() {
		var item MallSalesDailyStat
		var revenue string
		if err := dailyRows.Scan(&item.Date, &item.Currency, &item.Unit, &item.SalesCount, &revenue); err != nil {
			return nil, fmt.Errorf("scan mall sales analytics daily: %w", err)
		}
		item.Revenue = normalizeLedgerText(revenue)
		analytics.Daily = append(analytics.Daily, item)
	}
	if err := dailyRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mall sales analytics daily: %w", err)
	}
	return analytics, nil
}

func mallRevenueBucketKey(currency, unit string) string {
	return unit + "\x00" + currency
}

func addMallRevenueBucket(buckets map[string]MallRevenueTotal, currency, unit, revenue string, salesCount int64) {
	key := mallRevenueBucketKey(currency, unit)
	bucket := buckets[key]
	bucket.Currency = currency
	bucket.Unit = unit
	bucket.Revenue = addLedgerText(bucket.Revenue, revenue)
	bucket.SalesCount += salesCount
	buckets[key] = bucket
}

func sortedMallRevenueTotals(buckets map[string]MallRevenueTotal) []MallRevenueTotal {
	keys := make([]string, 0, len(buckets))
	for key := range buckets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	totals := make([]MallRevenueTotal, 0, len(keys))
	for _, key := range keys {
		totals = append(totals, buckets[key])
	}
	return totals
}

func singleMallRevenue(totals []MallRevenueTotal) string {
	switch len(totals) {
	case 0:
		return "0.00000000"
	case 1:
		return totals[0].Revenue
	default:
		return ""
	}
}

func addLedgerText(left, right string) string {
	l, _ := strconv.ParseFloat(left, 64)
	r, _ := strconv.ParseFloat(right, 64)
	value, err := normalizeLedgerAmount(l + r)
	if err != nil {
		return formatLedgerAmount(l + r)
	}
	return formatLedgerAmount(value)
}

func financialLedgerRowID(source string, id int64) string {
	return fmt.Sprintf("%s:%d", source, id)
}

func (s *MallService) GetFinancialLedger(ctx context.Context, userID int64, page int, days int, category string) (*FinancialLedgerResponse, error) {
	return s.queryFinancialLedger(ctx, userID, page, days, category)
}

func (s *MallService) GetAdminFinancialLedger(ctx context.Context, userID int64, page int, days int, category string) (*FinancialLedgerResponse, error) {
	return s.queryFinancialLedger(ctx, userID, page, days, category)
}

func (s *MallService) queryFinancialLedger(ctx context.Context, userID int64, page int, days int, category string) (*FinancialLedgerResponse, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("mall service database is nil")
	}
	if page < 1 {
		page = 1
	}
	if days != 1 && days != 7 && days != 15 {
		days = 15
	}
	end, todayStart, sevenStart, fifteenStart := financialWindowBounds()
	itemStart := fifteenStart
	if days == 1 {
		itemStart = todayStart
	} else if days == 7 {
		itemStart = sevenStart
	}
	cte := financialLedgerCTE(userID, itemStart, end)
	filter := ""
	queryArgs := append([]any{}, cte.args...)
	if strings.TrimSpace(category) != "" {
		queryArgs = append(queryArgs, strings.TrimSpace(category))
		filter = fmt.Sprintf(" WHERE category = $%d", len(queryArgs))
	}
	countQuery := "WITH ledger AS (" + cte.sql + ") SELECT COUNT(*) FROM ledger" + filter
	var total int64
	if err := s.db.QueryRowContext(ctx, countQuery, queryArgs...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count financial ledger: %w", err)
	}
	query := "WITH ledger AS (" + cte.sql + ") SELECT id,user_id,username,email,source,category,label,currency,unit,amount::text,cost_amount::text,product_type,product_id,operation,model,permanent_delta::text,temporary_delta::text,debt_delta::text,permanent_balance_before,permanent_balance_after,temporary_balance_before,temporary_balance_after,debt_before::text,debt_after::text,item_count,created_at FROM ledger" + filter
	limitArg := len(queryArgs) + 1
	offsetArg := len(queryArgs) + 2
	query += fmt.Sprintf(" ORDER BY created_at DESC, source ASC, id DESC LIMIT $%d OFFSET $%d", limitArg, offsetArg)
	queryArgs = append(queryArgs, financialLedgerPageSize, (page-1)*financialLedgerPageSize)
	rows, err := s.db.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("list financial ledger: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]FinancialLedgerItem, 0, financialLedgerPageSize)
	for rows.Next() {
		item, err := scanFinancialLedgerItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate financial ledger: %w", err)
	}
	windows, err := s.loadFinancialWindows(ctx, userID, todayStart, sevenStart, fifteenStart, end)
	if err != nil {
		return nil, err
	}
	summaryWindow := "fifteen_days"
	if days == 1 {
		summaryWindow = "today"
	} else if days == 7 {
		summaryWindow = "seven_days"
	}
	summary := windows[summaryWindow].Categories
	pages := int(math.Ceil(float64(total) / float64(financialLedgerPageSize)))
	if pages < 1 {
		pages = 1
	}
	return &FinancialLedgerResponse{UserID: userID, Timezone: "Asia/Shanghai", Days: days, Windows: windows, Summary: summary, Items: items, Total: total, Page: page, PageSize: financialLedgerPageSize, Pages: pages}, nil
}

type financialLedgerCTEData struct {
	sql  string
	args []any
}

func financialLedgerCTE(userID int64, start, end time.Time) financialLedgerCTEData {
	userFilterUsage, userFilterMall, userFilterPayment, userFilterBank := "", "", "", ""
	args := make([]any, 0, 3)
	if userID > 0 {
		args = append(args, userID)
		userFilterUsage = " AND ul.user_id = $1"
		userFilterMall = " AND mp.user_id = $1"
		userFilterPayment = " AND po.user_id = $1"
		userFilterBank = " AND bl.user_id = $1"
	}
	startPosition := len(args) + 1
	args = append(args, start)
	endPosition := len(args) + 1
	args = append(args, end)
	usageTimeFilter := fmt.Sprintf(" AND ul.created_at >= $%d AND ul.created_at < $%d", startPosition, endPosition)
	mallTimeFilter := fmt.Sprintf(" AND mp.created_at >= $%d AND mp.created_at < $%d", startPosition, endPosition)
	paymentTimeFilter := fmt.Sprintf(" AND COALESCE(po.completed_at, po.paid_at, po.created_at) >= $%d AND COALESCE(po.completed_at, po.paid_at, po.created_at) < $%d", startPosition, endPosition)
	bankTimeFilter := fmt.Sprintf(" AND bl.created_at >= $%d AND bl.created_at < $%d", startPosition, endPosition)
	sqlText := `
	SELECT -(('x' || substr(md5(concat_ws('|',
	           ul.user_id::text,
	           (ul.created_at AT TIME ZONE 'Asia/Shanghai')::date::text,
	           COALESCE(NULLIF(ul.requested_model, ''), ul.model))), 1, 15))::bit(60)::bigint) AS id,
	       ul.user_id, COALESCE(NULLIF(u.username, ''), u.email) AS username, u.email,
       'usage' AS source, 'model' AS category,
       COALESCE(NULLIF(ul.requested_model, ''), ul.model) || '模型费用' AS label,
	       ''::text AS currency, 'credit'::text AS unit,
	       SUM(ul.total_cost) AS amount, SUM(ul.total_cost) AS cost_amount,
       NULL::text AS product_type, NULL::bigint AS product_id, NULL::text AS operation,
       COALESCE(NULLIF(ul.requested_model, ''), ul.model) AS model,
       NULL::numeric AS permanent_delta, NULL::numeric AS temporary_delta, NULL::numeric AS debt_delta,
       NULL::text AS permanent_balance_before, NULL::text AS permanent_balance_after,
       NULL::text AS temporary_balance_before, NULL::text AS temporary_balance_after,
	       NULL::numeric AS debt_before, NULL::numeric AS debt_after,
	       COUNT(*)::bigint AS item_count, MAX(ul.created_at) AS created_at
FROM usage_logs AS ul JOIN users AS u ON u.id = ul.user_id
WHERE ul.total_cost > 0` + userFilterUsage + usageTimeFilter + `
GROUP BY ul.user_id, u.username, u.email,
         (ul.created_at AT TIME ZONE 'Asia/Shanghai')::date,
         COALESCE(NULLIF(ul.requested_model, ''), ul.model)
UNION ALL
SELECT mp.id, mp.user_id, COALESCE(NULLIF(u.username, ''), u.email), u.email,
       'mall', 'mall', COALESCE(NULLIF(mp.product_name, ''),
           CASE WHEN mp.product_type = 'currency' THEN cp.name ELSE sp.name END,
           mp.product_type || '#' || mp.product_id::text) || '消费',
	       ''::text, 'credit'::text,
       mp.price, mp.price, mp.product_type, mp.product_id, NULL::text, NULL::text,
       NULL::numeric, NULL::numeric, NULL::numeric,
       mp.permanent_balance_before::text, mp.permanent_balance_after::text,
       mp.temporary_balance_before::text, mp.temporary_balance_after::text,
	       NULL::numeric, NULL::numeric, 1::bigint, mp.created_at
FROM mall_purchases AS mp JOIN users AS u ON u.id = mp.user_id
LEFT JOIN currency_products AS cp ON mp.product_type = 'currency' AND cp.id = mp.product_id
LEFT JOIN subscription_plans AS sp ON mp.product_type = 'subscription' AND sp.id = mp.product_id
WHERE mp.status = 'completed'` + userFilterMall + mallTimeFilter + `
UNION ALL
SELECT -po.id, po.user_id,
       COALESCE(NULLIF(u.username, ''), NULLIF(po.user_name, ''), u.email, po.user_email),
       COALESCE(NULLIF(po.user_email, ''), u.email, ''),
       'payment_order', 'mall',
       CASE
           WHEN po.currency_product_id IS NOT NULL THEN
               COALESCE(NULLIF(po.currency_product_name, ''), cp.name,
                   'currency#' || po.currency_product_id::text)
           ELSE COALESCE(NULLIF(sp.name, ''), 'subscription#' || po.plan_id::text)
       END || '消费',
	       COALESCE(NULLIF(UPPER(BTRIM(po.provider_snapshot->>'currency')), ''), 'CNY'), 'fiat'::text,
       CASE WHEN po.currency_product_id IS NOT NULL
            THEN COALESCE(po.currency_product_payment_price, po.amount)
            ELSE po.amount END,
       CASE WHEN po.currency_product_id IS NOT NULL
            THEN COALESCE(po.currency_product_payment_price, po.amount)
            ELSE po.amount END,
       CASE WHEN po.currency_product_id IS NOT NULL THEN 'currency' ELSE 'subscription' END,
       COALESCE(po.currency_product_id, po.plan_id), NULL::text, NULL::text,
       NULL::numeric, NULL::numeric, NULL::numeric,
       NULL::text, NULL::text, NULL::text, NULL::text,
       NULL::numeric, NULL::numeric, 1::bigint,
       COALESCE(po.completed_at, po.paid_at, po.created_at)
FROM payment_orders AS po
LEFT JOIN users AS u ON u.id = po.user_id
LEFT JOIN currency_products AS cp
  ON po.currency_product_id IS NOT NULL AND cp.id = po.currency_product_id
LEFT JOIN subscription_plans AS sp
  ON po.currency_product_id IS NULL AND sp.id = po.plan_id
WHERE po.status = 'COMPLETED'
  AND (po.currency_product_id IS NOT NULL OR (po.order_type = 'subscription' AND po.plan_id IS NOT NULL))` + userFilterPayment + paymentTimeFilter + `
UNION ALL
SELECT bl.id, bl.user_id, COALESCE(NULLIF(u.username, ''), u.email), u.email,
       'bank', CASE WHEN bl.operation IN ('permanent_settlement', 'unused_advance_repayment') THEN 'settlement' ELSE 'bank' END,
	       bl.operation,
	       ''::text, 'credit'::text,
       CASE
           WHEN bl.operation IN ('exchange', 'early_repay_permanent', 'permanent_settlement') THEN GREATEST(-bl.permanent_delta, 0)
           WHEN bl.operation IN ('early_repay_temporary', 'debt_offset', 'unused_advance_repayment') THEN GREATEST(-bl.temporary_delta, 0)
           ELSE 0
       END,
       CASE
           WHEN bl.operation IN ('exchange', 'early_repay_permanent', 'permanent_settlement') THEN GREATEST(-bl.permanent_delta, 0)
           WHEN bl.operation IN ('early_repay_temporary', 'debt_offset', 'unused_advance_repayment') THEN GREATEST(-bl.temporary_delta, 0)
           ELSE 0
       END,
       NULL::text, NULL::bigint, bl.operation, NULL::text,
       bl.permanent_delta, bl.temporary_delta, bl.debt_delta,
       bl.permanent_balance_before::text, bl.permanent_balance_after::text,
       bl.temporary_balance_before::text, bl.temporary_balance_after::text,
	       bl.debt_before, bl.debt_after, 1::bigint, bl.created_at
FROM bank_ledger AS bl JOIN users AS u ON u.id = bl.user_id
WHERE TRUE` + userFilterBank + bankTimeFilter
	return financialLedgerCTEData{sql: sqlText, args: args}
}

func scanFinancialLedgerItem(rows *sql.Rows) (FinancialLedgerItem, error) {
	var item FinancialLedgerItem
	var amount, cost string
	var currency, unit string
	var productType, operation, model sql.NullString
	var productID sql.NullInt64
	var permanentDelta, temporaryDelta, debtDelta sql.NullString
	var permanentBefore, permanentAfter, temporaryBefore, temporaryAfter sql.NullString
	var debtBefore, debtAfter sql.NullString
	if err := rows.Scan(&item.ID, &item.UserID, &item.Username, &item.Email, &item.Source, &item.Category, &item.Label,
		&currency, &unit, &amount, &cost, &productType, &productID, &operation, &model, &permanentDelta, &temporaryDelta, &debtDelta,
		&permanentBefore, &permanentAfter, &temporaryBefore, &temporaryAfter, &debtBefore, &debtAfter, &item.Count, &item.CreatedAt); err != nil {
		return item, fmt.Errorf("scan financial ledger item: %w", err)
	}
	item.Amount = normalizeLedgerText(amount)
	item.CostAmount = normalizeLedgerText(cost)
	item.Currency = currency
	item.Unit = unit
	item.RowID = financialLedgerRowID(item.Source, item.ID)
	if productType.Valid {
		item.ProductType = &productType.String
	}
	if productID.Valid {
		item.ProductID = &productID.Int64
	}
	if operation.Valid {
		item.Operation = &operation.String
	}
	if model.Valid {
		item.Model = &model.String
	}
	item.PermanentDelta = nullableLedgerText(permanentDelta)
	item.TemporaryDelta = nullableLedgerText(temporaryDelta)
	item.DebtDelta = nullableLedgerText(debtDelta)
	item.PermanentBalanceBefore = nullableLedgerText(permanentBefore)
	item.PermanentBalanceAfter = nullableLedgerText(permanentAfter)
	item.TemporaryBalanceBefore = nullableLedgerText(temporaryBefore)
	item.TemporaryBalanceAfter = nullableLedgerText(temporaryAfter)
	item.DebtBefore = nullableLedgerText(debtBefore)
	item.DebtAfter = nullableLedgerText(debtAfter)
	item.CreatedAt = item.CreatedAt.UTC()
	return item, nil
}

func financialWindowBounds() (time.Time, time.Time, time.Time, time.Time) {
	now := time.Now().In(beijingLocation)
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, beijingLocation)
	return todayStart.AddDate(0, 0, 1), todayStart, todayStart.AddDate(0, 0, -6), todayStart.AddDate(0, 0, -14)
}

func (s *MallService) loadFinancialWindows(ctx context.Context, userID int64, todayStart, sevenStart, fifteenStart, end time.Time) (map[string]FinancialLedgerWindow, error) {
	cte := financialLedgerCTE(userID, fifteenStart, end)
	query, args := financialLedgerWindowsQuery(cte, todayStart, sevenStart, fifteenStart)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("load financial ledger windows: %w", err)
	}
	defer func() { _ = rows.Close() }()
	windows := map[string]FinancialLedgerWindow{
		"today":        {TotalAmount: "0.00000000", Categories: []FinancialLedgerCategory{}, Totals: []FinancialLedgerTotal{}},
		"seven_days":   {TotalAmount: "0.00000000", Categories: []FinancialLedgerCategory{}, Totals: []FinancialLedgerTotal{}},
		"fifteen_days": {TotalAmount: "0.00000000", Categories: []FinancialLedgerCategory{}, Totals: []FinancialLedgerTotal{}},
	}
	for rows.Next() {
		var windowName, category, label, currency, unit, amount string
		var count int64
		if err := rows.Scan(&windowName, &category, &label, &currency, &unit, &amount, &count); err != nil {
			return nil, fmt.Errorf("scan financial ledger window: %w", err)
		}
		window, ok := windows[windowName]
		if !ok {
			continue
		}
		amount = normalizeLedgerText(amount)
		window.Count += count
		window.Categories = append(window.Categories, FinancialLedgerCategory{Category: category, Label: label, Amount: amount, Count: count, Currency: currency, Unit: unit})
		addFinancialLedgerTotal(&window, currency, unit, amount, count)
		windows[windowName] = window
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate financial ledger windows: %w", err)
	}
	return windows, nil
}

func financialLedgerWindowsQuery(cte financialLedgerCTEData, todayStart, sevenStart, fifteenStart time.Time) (string, []any) {
	args := append([]any{}, cte.args...)
	todayPosition := len(args) + 1
	args = append(args, todayStart)
	sevenPosition := len(args) + 1
	args = append(args, sevenStart)
	fifteenPosition := len(args) + 1
	args = append(args, fifteenStart)
	query := fmt.Sprintf(`WITH ledger AS (%s), requested_windows(window_name, start_at) AS (
    VALUES ('today', $%d::timestamptz), ('seven_days', $%d::timestamptz), ('fifteen_days', $%d::timestamptz)
)
SELECT requested_windows.window_name, ledger.category, ledger.label,
       ledger.currency, ledger.unit,
       SUM(ledger.cost_amount)::text, SUM(ledger.item_count)::bigint
FROM requested_windows
JOIN ledger ON ledger.created_at >= requested_windows.start_at
GROUP BY requested_windows.window_name, ledger.category, ledger.label, ledger.currency, ledger.unit
ORDER BY CASE requested_windows.window_name
    WHEN 'today' THEN 1 WHEN 'seven_days' THEN 2 ELSE 3 END,
    ledger.category DESC, ledger.label ASC, ledger.unit ASC, ledger.currency ASC`, cte.sql, todayPosition, sevenPosition, fifteenPosition)
	return query, args
}

func addFinancialLedgerTotal(window *FinancialLedgerWindow, currency, unit, amount string, count int64) {
	for index := range window.Totals {
		if window.Totals[index].Currency == currency && window.Totals[index].Unit == unit {
			window.Totals[index].Amount = addLedgerText(window.Totals[index].Amount, amount)
			window.Totals[index].Count += count
			if unit == LedgerUnitCredit {
				window.TotalAmount = addLedgerText(window.TotalAmount, amount)
			}
			return
		}
	}
	window.Totals = append(window.Totals, FinancialLedgerTotal{Currency: currency, Unit: unit, Amount: amount, Count: count})
	if unit == LedgerUnitCredit {
		window.TotalAmount = addLedgerText(window.TotalAmount, amount)
	}
}
