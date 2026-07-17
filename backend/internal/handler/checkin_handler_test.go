package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type checkinAPIServiceStub struct {
	status       *service.CheckinStatus
	statusMonth  string
	checkin      *service.CheckinResult
	checkinCalls int
	atomicStore  service.IdempotencyAtomicSuccessExecutor
}

type checkinAtomicResult int64

func (r checkinAtomicResult) LastInsertId() (int64, error) { return 0, nil }
func (r checkinAtomicResult) RowsAffected() (int64, error) { return int64(r), nil }

type checkinAtomicSuccessStore struct {
	repo *userMemoryIdempotencyRepoStub
	err  error
}

func (s *checkinAtomicSuccessStore) ExecContext(ctx context.Context, _ string, args ...any) (sql.Result, error) {
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
	return checkinAtomicResult(1), nil
}

func (s *checkinAPIServiceStub) GetStatus(_ context.Context, _ int64, month string) (*service.CheckinStatus, error) {
	s.statusMonth = month
	return s.status, nil
}

func (s *checkinAPIServiceStub) CheckInAtomic(ctx context.Context, _ int64, claim *service.IdempotencyAtomicClaim) (*service.CheckinResult, error) {
	s.checkinCalls++
	if s.atomicStore == nil {
		return nil, service.ErrIdempotencyStoreUnavail
	}
	if err := claim.PersistSuccess(ctx, s.atomicStore, s.checkin); err != nil {
		return nil, err
	}
	return s.checkin, nil
}

func TestCheckinHandler_GetStatusAndPostReplay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, time.July, 13, 16, 0, 0, 0, time.UTC)
	stub := &checkinAPIServiceStub{
		status: &service.CheckinStatus{
			Enabled:            true,
			NextRewardDay:      1,
			NextRewardAmount:   "1.00000000",
			MonthlyRewardTotal: "2.00000000",
			Calendar: []service.DailyCheckinCalendarEntry{{
				CheckinDate:  "2026-07-13",
				StreakDay:    1,
				RewardDay:    1,
				RewardAmount: "1.00000000",
			}},
		},
		checkin: &service.CheckinResult{
			CheckinDate:            "2026-07-14",
			StreakDay:              2,
			RewardDay:              2,
			RewardAmount:           "1.00000000",
			TemporaryCreditGrantID: 91,
			ExpiresAt:              now,
		},
	}
	h := NewCheckinHandler(stub)

	repo := newUserMemoryIdempotencyRepoStub()
	stub.atomicStore = &checkinAtomicSuccessStore{repo: repo}
	cfg := service.DefaultIdempotencyConfig()
	cfg.ObserveOnly = false
	service.SetDefaultIdempotencyCoordinator(service.NewIdempotencyCoordinator(repo, cfg))
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(nil) })

	router := gin.New()
	router.Use(withUserSubject(42))
	router.GET("/api/v1/user/check-in", h.GetStatus)
	router.POST("/api/v1/user/check-in", h.CheckIn)

	getRequest := httptest.NewRequest(http.MethodGet, "/api/v1/user/check-in?month=2026-07", nil)
	getRecorder := httptest.NewRecorder()
	router.ServeHTTP(getRecorder, getRequest)
	require.Equal(t, http.StatusOK, getRecorder.Code)
	require.Equal(t, "2026-07", stub.statusMonth)
	require.Contains(t, getRecorder.Body.String(), `"monthly_reward_total":"2.00000000"`)

	missingKey := httptest.NewRequest(http.MethodPost, "/api/v1/user/check-in", nil)
	missingKeyRecorder := httptest.NewRecorder()
	router.ServeHTTP(missingKeyRecorder, missingKey)
	require.Equal(t, http.StatusBadRequest, missingKeyRecorder.Code)
	require.Equal(t, 0, stub.checkinCalls)

	post := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/user/check-in", nil)
		req.Header.Set("Idempotency-Key", "checkin-2026-07-14")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		return recorder
	}
	first := post()
	second := post()
	require.Equal(t, http.StatusOK, first.Code)
	require.Equal(t, http.StatusOK, second.Code)
	require.Equal(t, "true", second.Header().Get("X-Idempotency-Replayed"))
	require.Equal(t, 1, stub.checkinCalls)
	require.Contains(t, first.Body.String(), `"temporary_credit_grant_id":91`)
	var envelope struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &envelope))
	require.Len(t, envelope.Data, 7)
	require.NotContains(t, envelope.Data, "temporary_credit_available")
}

func TestCheckinHandler_FailsClosedWhenAtomicSuccessPersistenceFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &checkinAPIServiceStub{checkin: &service.CheckinResult{CheckinDate: "2026-07-14", RewardAmount: "1.00000000"}}
	h := NewCheckinHandler(stub)

	repo := newUserMemoryIdempotencyRepoStub()
	stub.atomicStore = &checkinAtomicSuccessStore{repo: repo, err: errors.New("idempotency persistence temporarily unavailable")}
	cfg := service.DefaultIdempotencyConfig()
	cfg.ObserveOnly = false
	service.SetDefaultIdempotencyCoordinator(service.NewIdempotencyCoordinator(repo, cfg))
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(nil) })

	router := gin.New()
	router.Use(withUserSubject(42))
	router.POST("/api/v1/user/check-in", h.CheckIn)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/check-in", bytes.NewBufferString(`{}`))
	req.Header.Set("Idempotency-Key", "persistence-pending")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Equal(t, "1", recorder.Header().Get("Retry-After"))
	require.Empty(t, recorder.Header().Get("X-Idempotency-Persistence"))
	require.NotContains(t, recorder.Body.String(), `"data"`)
	require.Equal(t, 1, stub.checkinCalls)
}

func TestCheckinHandler_EmptyBodyAndEmptyJSONObjectReplayWithSameIdempotencyKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &checkinAPIServiceStub{checkin: &service.CheckinResult{CheckinDate: "2026-07-14", RewardAmount: "1.00000000"}}
	h := NewCheckinHandler(stub)

	repo := newUserMemoryIdempotencyRepoStub()
	stub.atomicStore = &checkinAtomicSuccessStore{repo: repo}
	cfg := service.DefaultIdempotencyConfig()
	cfg.ObserveOnly = false
	service.SetDefaultIdempotencyCoordinator(service.NewIdempotencyCoordinator(repo, cfg))
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(nil) })

	router := gin.New()
	router.Use(withUserSubject(42))
	router.POST("/api/v1/user/check-in", h.CheckIn)

	firstRequest := httptest.NewRequest(http.MethodPost, "/api/v1/user/check-in", nil)
	firstRequest.Header.Set("Idempotency-Key", "empty-and-object")
	firstRecorder := httptest.NewRecorder()
	router.ServeHTTP(firstRecorder, firstRequest)

	secondRequest := httptest.NewRequest(http.MethodPost, "/api/v1/user/check-in", bytes.NewBufferString(`{}`))
	secondRequest.Header.Set("Content-Type", "application/json")
	secondRequest.Header.Set("Idempotency-Key", "empty-and-object")
	secondRecorder := httptest.NewRecorder()
	router.ServeHTTP(secondRecorder, secondRequest)

	require.Equal(t, http.StatusOK, firstRecorder.Code)
	require.Equal(t, http.StatusOK, secondRecorder.Code)
	require.Equal(t, "true", secondRecorder.Header().Get("X-Idempotency-Replayed"))
	require.Equal(t, 1, stub.checkinCalls)
}

func TestCheckinHandler_RejectsNonEmptyOrMalformedJSONBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.SetDefaultIdempotencyCoordinator(nil)
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(nil) })

	stub := &checkinAPIServiceStub{checkin: &service.CheckinResult{CheckinDate: "2026-07-14", RewardAmount: "1.00000000"}}
	h := NewCheckinHandler(stub)
	router := gin.New()
	router.Use(withUserSubject(42))
	router.POST("/api/v1/user/check-in", h.CheckIn)

	for _, body := range []string{`{"unexpected":true}`, `{`} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/user/check-in", bytes.NewBufferString(body))
		req.Header.Set("Idempotency-Key", "invalid-body-"+body)
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		require.Equal(t, http.StatusBadRequest, recorder.Code, body)
	}
	require.Zero(t, stub.checkinCalls)
}

func TestCheckinHandler_FailsClosedWithoutCoordinator(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.SetDefaultIdempotencyCoordinator(nil)
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(nil) })

	stub := &checkinAPIServiceStub{}
	h := NewCheckinHandler(stub)
	router := gin.New()
	router.Use(withUserSubject(42))
	router.POST("/api/v1/user/check-in", h.CheckIn)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/user/check-in", bytes.NewBufferString(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "nil-coordinator")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Equal(t, "1", recorder.Header().Get("Retry-After"))
	require.Contains(t, recorder.Body.String(), `"reason":"IDEMPOTENCY_STORE_UNAVAILABLE"`)
	require.NotContains(t, recorder.Body.String(), `"data"`)
	require.Zero(t, stub.checkinCalls)
}
