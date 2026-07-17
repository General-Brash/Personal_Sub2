package admin

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type adminTemporaryCreditServiceStub struct {
	grantResult *service.AdminTemporaryCreditGrantResult
	items       []service.TemporaryCreditAuditItem
	total       int64
	atomicStore service.IdempotencyAtomicSuccessExecutor
	grantCalls  int
	listCalls   int
	userID      int64
	adminID     int64
	amount      float64
	notes       string
	page        int
	pageSize    int
}

func (s *adminTemporaryCreditServiceStub) GrantAtomic(ctx context.Context, userID, adminID int64, amount float64, notes string, claim *service.IdempotencyAtomicClaim) (*service.AdminTemporaryCreditGrantResult, error) {
	s.grantCalls++
	s.userID = userID
	s.adminID = adminID
	s.amount = amount
	s.notes = notes
	if s.atomicStore == nil {
		return nil, service.ErrIdempotencyStoreUnavail
	}
	if err := claim.PersistSuccess(ctx, s.atomicStore, s.grantResult); err != nil {
		return nil, err
	}
	return s.grantResult, nil
}

func (s *adminTemporaryCreditServiceStub) ListAudit(_ context.Context, userID int64, page, pageSize int) ([]service.TemporaryCreditAuditItem, int64, error) {
	s.listCalls++
	s.userID = userID
	s.page = page
	s.pageSize = pageSize
	return s.items, s.total, nil
}

type adminAtomicResult int64

func (r adminAtomicResult) LastInsertId() (int64, error) { return 0, nil }
func (r adminAtomicResult) RowsAffected() (int64, error) { return int64(r), nil }

type adminAtomicSuccessStore struct {
	repo *memoryIdempotencyRepoStub
	err  error
}

func (s *adminAtomicSuccessStore) ExecContext(ctx context.Context, _ string, args ...any) (sql.Result, error) {
	if s.err != nil {
		return nil, s.err
	}
	id := args[0].(int64)
	status := args[2].(int)
	body := args[3].(string)
	expiresAt := args[4].(time.Time)
	if err := s.repo.MarkSucceeded(ctx, id, status, body, expiresAt); err != nil {
		return nil, err
	}
	return adminAtomicResult(1), nil
}

func TestAdminGrantTemporaryCreditStrictRequestActorAndReplay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMemoryIdempotencyRepoStub()
	cfg := service.DefaultIdempotencyConfig()
	cfg.ObserveOnly = false
	service.SetDefaultIdempotencyCoordinator(service.NewIdempotencyCoordinator(repo, cfg))
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(nil) })

	expiresAt := time.Date(2026, time.July, 16, 16, 0, 0, 0, time.UTC)
	stub := &adminTemporaryCreditServiceStub{
		grantResult: &service.AdminTemporaryCreditGrantResult{
			TemporaryCreditGrantID: 77,
			Amount:                 "1.25000000",
			RemainingAmount:        "1.25000000",
			ExpiresAt:              expiresAt,
			Notes:                  "",
		},
	}
	stub.atomicStore = &adminAtomicSuccessStore{repo: repo}
	handler := NewTemporaryCreditHandler(stub)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 99})
		c.Next()
	})
	router.POST("/api/v1/admin/users/:id/temporary-credits", handler.Grant)

	post := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/42/temporary-credits", bytes.NewBufferString(`{"amount":"1.25000000"}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", "manual-grant-42")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		return recorder
	}
	first := post()
	second := post()

	require.Equal(t, http.StatusOK, first.Code)
	require.Equal(t, http.StatusOK, second.Code)
	require.Equal(t, "true", second.Header().Get("X-Idempotency-Replayed"))
	require.Equal(t, 1, stub.grantCalls)
	require.Equal(t, int64(42), stub.userID)
	require.Equal(t, int64(99), stub.adminID)
	require.Equal(t, 1.25, stub.amount)
	require.Empty(t, stub.notes)
	require.JSONEq(t, `{"code":0,"message":"success","data":{"temporary_credit_grant_id":77,"amount":"1.25000000","remaining_amount":"1.25000000","expires_at":"2026-07-16T16:00:00Z","notes":""}}`, first.Body.String())
	require.JSONEq(t, first.Body.String(), second.Body.String())
	record, err := repo.GetByScopeActorScopeAndKeyHash(context.Background(), "admin.users.temporary_credits.grant", "admin:99", service.HashIdempotencyKey("manual-grant-42"))
	require.NoError(t, err)
	require.NotNil(t, record)
	require.Equal(t, service.IdempotencyStatusSucceeded, record.Status)
}

func TestAdminGrantTemporaryCreditRejectsInvalidJSONAmountAndNotes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &adminTemporaryCreditServiceStub{}
	handler := NewTemporaryCreditHandler(stub)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 99})
		c.Next()
	})
	router.POST("/api/v1/admin/users/:id/temporary-credits", handler.Grant)

	for _, body := range []string{
		`{}`,
		`{"amount":1.25}`,
		`{"amount":null}`,
		`{"amount":"01.0"}`,
		`{"amount":"1e2"}`,
		`{"amount":"1.000000000"}`,
		`{"amount":"1","notes":null}`,
		`{"amount":"1","notes":7}`,
		`{"amount":"1","expires_at":"2026-07-17T00:00:00+08:00"}`,
		`{"amount":"1"}{}`,
	} {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/42/temporary-credits", bytes.NewBufferString(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", "invalid-"+body)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusBadRequest, recorder.Code, body)
		require.Equal(t, "INVALID_TEMPORARY_CREDIT_AMOUNT", infraerrors.Reason(decodeApplicationError(t, recorder)), body)
		require.NotContains(t, recorder.Body.String(), `"data"`, body)
	}
	require.Zero(t, stub.grantCalls)
}

func TestAdminGrantTemporaryCreditFailsClosedWithoutCoordinator(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.SetDefaultIdempotencyCoordinator(nil)
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(nil) })
	stub := &adminTemporaryCreditServiceStub{}
	handler := NewTemporaryCreditHandler(stub)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 99})
		c.Next()
	})
	router.POST("/api/v1/admin/users/:id/temporary-credits", handler.Grant)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/42/temporary-credits", bytes.NewBufferString(`{"amount":"1.00000000"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "nil-coordinator")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Equal(t, "1", recorder.Header().Get("Retry-After"))
	require.Contains(t, recorder.Body.String(), `"reason":"IDEMPOTENCY_STORE_UNAVAILABLE"`)
	require.NotContains(t, recorder.Body.String(), `"data"`)
	require.Zero(t, stub.grantCalls)
}

func TestAdminGrantTemporaryCreditRequiresValidIdempotencyKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMemoryIdempotencyRepoStub()
	cfg := service.DefaultIdempotencyConfig()
	cfg.ObserveOnly = true
	service.SetDefaultIdempotencyCoordinator(service.NewIdempotencyCoordinator(repo, cfg))
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(nil) })
	stub := &adminTemporaryCreditServiceStub{}
	handler := NewTemporaryCreditHandler(stub)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 99})
		c.Next()
	})
	router.POST("/api/v1/admin/users/:id/temporary-credits", handler.Grant)

	for _, key := range []string{"", "contains space", strings.Repeat("x", 129)} {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/42/temporary-credits", bytes.NewBufferString(`{"amount":"1.00000000"}`))
		request.Header.Set("Content-Type", "application/json")
		if key != "" {
			request.Header.Set("Idempotency-Key", key)
		}
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusBadRequest, recorder.Code, key)
		if key == "" {
			require.Contains(t, recorder.Body.String(), `"reason":"IDEMPOTENCY_KEY_REQUIRED"`)
		} else {
			require.Contains(t, recorder.Body.String(), `"reason":"IDEMPOTENCY_KEY_INVALID"`)
		}
	}
	require.Zero(t, stub.grantCalls)
}

func TestTemporaryCreditAuditPaginationIsStrictAndDefaults(t *testing.T) {
	gin.SetMode(gin.TestMode)
	createdAt := time.Date(2026, time.July, 16, 8, 30, 0, 0, time.UTC)
	stub := &adminTemporaryCreditServiceStub{
		items: []service.TemporaryCreditAuditItem{{
			ID:              8,
			UserID:          42,
			Source:          service.TemporaryCreditSourceAdminGrant,
			Amount:          "2.00000000",
			RemainingAmount: "1.50000000",
			ExpiresAt:       time.Date(2026, time.July, 16, 16, 0, 0, 0, time.UTC),
			Notes:           "campaign",
			CreatedAt:       createdAt,
			UpdatedAt:       createdAt,
		}},
		total: 1,
	}
	handler := NewTemporaryCreditHandler(stub)
	router := gin.New()
	router.GET("/api/v1/admin/users/:id/temporary-credits", handler.ListAudit)

	defaultRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users/42/temporary-credits", nil)
	defaultRecorder := httptest.NewRecorder()
	router.ServeHTTP(defaultRecorder, defaultRequest)
	require.Equal(t, http.StatusOK, defaultRecorder.Code)
	require.Equal(t, 1, stub.page)
	require.Equal(t, 20, stub.pageSize)
	require.Contains(t, defaultRecorder.Body.String(), `"items":[{`)
	require.Contains(t, defaultRecorder.Body.String(), `"expires_at":"2026-07-16T16:00:00Z"`)

	for _, query := range []string{
		"page=0",
		"page=-1",
		"page=+1",
		"page=1.0",
		"page=",
		"page_size=0",
		"page_size=1001",
		"page_size=-1",
		"page_size=1.5",
	} {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users/42/temporary-credits?"+query, nil)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusBadRequest, recorder.Code, query)
		require.Contains(t, recorder.Body.String(), `"reason":"INVALID_PAGINATION"`, query)
	}
	require.Equal(t, 1, stub.listCalls)
}

func decodeApplicationError(t *testing.T, recorder *httptest.ResponseRecorder) error {
	t.Helper()
	var body struct {
		Code     int               `json:"code"`
		Message  string            `json:"message"`
		Reason   string            `json:"reason"`
		Metadata map[string]string `json:"metadata"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	return infraerrors.New(body.Code, body.Reason, body.Message).WithMetadata(body.Metadata)
}
