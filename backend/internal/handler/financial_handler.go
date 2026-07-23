package handler

import (
	"context"
	"errors"
	"strconv"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

const fixedFinancialPageSize = 20

// MallFinancialService is the read-only finance surface consumed by HTTP
// handlers.  Keeping it narrow lets handler tests verify authorization and
// parameter contracts without opening a database.
type MallFinancialService interface {
	ListMallTransactions(ctx context.Context, userID int64, page int, productType string) ([]service.MallTransactionItem, int64, error)
	GetMallSalesAnalytics(ctx context.Context, days int) (*service.MallSalesAnalytics, error)
	GetFinancialLedger(ctx context.Context, userID int64, page int, days int, category string) (*service.FinancialLedgerResponse, error)
	GetAdminFinancialLedger(ctx context.Context, userID int64, page int, days int, category string) (*service.FinancialLedgerResponse, error)
}

type MallSalesService interface {
	GetMallSalesCounts(ctx context.Context) (map[service.MallSalesKey]int64, error)
}

// GetFinancialLedger returns the authenticated user's consolidated ledger.
// GET /api/v1/user/ledger
func (h *PaymentHandler) GetFinancialLedger(c *gin.Context) {
	subject, ok := requireAuth(c)
	if !ok {
		return
	}
	page, days, category, ok := parseFinancialLedgerQuery(c)
	if !ok {
		return
	}
	finance, ok := h.requireFinancialService(c)
	if !ok {
		return
	}
	result, err := finance.GetFinancialLedger(c.Request.Context(), subject.UserID, page, days, category)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// GetAdminFinancialLedger returns the all-site ledger or one user's drilldown.
// GET /api/v1/admin/payment/ledger
func (h *PaymentHandler) GetAdminFinancialLedger(c *gin.Context) {
	page, days, category, ok := parseFinancialLedgerQuery(c)
	if !ok {
		return
	}
	userID, err := parseFinanceOptionalPositiveID(c.Query("user_id"))
	if err != nil {
		response.BadRequest(c, "Invalid user_id")
		return
	}
	finance, ok := h.requireFinancialService(c)
	if !ok {
		return
	}
	result, err := finance.GetAdminFinancialLedger(c.Request.Context(), userID, page, days, category)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// ListAdminMallTransactions returns completed mall settlements with balance
// snapshots.  Page size is fixed at 20 regardless of client query parameters.
// GET /api/v1/admin/payment/mall/transactions
func (h *PaymentHandler) ListAdminMallTransactions(c *gin.Context) {
	page, err := parseFinancePage(c.Query("page"))
	if err != nil {
		response.BadRequest(c, "Invalid page")
		return
	}
	userID, err := parseFinanceOptionalPositiveID(c.Query("user_id"))
	if err != nil {
		response.BadRequest(c, "Invalid user_id")
		return
	}
	productType := strings.TrimSpace(c.Query("product_type"))
	if productType != "" && productType != string(service.MallProductTypeCurrency) && productType != string(service.MallProductTypeSubscription) {
		response.BadRequest(c, "Invalid product_type")
		return
	}
	finance, ok := h.requireFinancialService(c)
	if !ok {
		return
	}
	items, total, err := finance.ListMallTransactions(c.Request.Context(), userID, page, productType)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, total, page, fixedFinancialPageSize)
}

// GetAdminMallAnalytics returns mall sales analytics for a bounded date range.
// GET /api/v1/admin/payment/mall/analytics
func (h *PaymentHandler) GetAdminMallAnalytics(c *gin.Context) {
	days := 30
	if raw := strings.TrimSpace(c.Query("days")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 365 {
			response.BadRequest(c, "Invalid days")
			return
		}
		days = value
	}
	finance, ok := h.requireFinancialService(c)
	if !ok {
		return
	}
	result, err := finance.GetMallSalesAnalytics(c.Request.Context(), days)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *PaymentHandler) requireFinancialService(c *gin.Context) (MallFinancialService, bool) {
	if h == nil || h.financeService == nil {
		response.ErrorFrom(c, infraerrors.ServiceUnavailable("FINANCIAL_LEDGER_UNAVAILABLE", "financial ledger is unavailable"))
		return nil, false
	}
	return h.financeService, true
}

func parseFinancialLedgerQuery(c *gin.Context) (page, days int, category string, ok bool) {
	page, err := parseFinancePage(c.Query("page"))
	if err != nil {
		response.BadRequest(c, "Invalid page")
		return 0, 0, "", false
	}
	days = 7
	if raw := strings.TrimSpace(c.Query("days")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || (value != 1 && value != 7 && value != 15) {
			response.BadRequest(c, "Invalid days")
			return 0, 0, "", false
		}
		days = value
	}
	category = strings.TrimSpace(c.Query("category"))
	if category != "" && category != "model" && category != "mall" && category != "bank" && category != "settlement" {
		response.BadRequest(c, "Invalid category")
		return 0, 0, "", false
	}
	return page, days, category, true
}

func parseFinancePage(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 1, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return 0, errors.New("page must be positive")
	}
	return value, nil
}

func parseFinanceOptionalPositiveID(raw string) (int64, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 1 {
		return 0, errors.New("id must be positive")
	}
	return value, nil
}
