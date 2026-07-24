package server_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/server/routes"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCheckinAndTemporaryCreditHTTPContract(t *testing.T) {
	gin.SetMode(gin.TestMode)

	expiresAt := time.Date(2026, time.July, 18, 16, 0, 0, 0, time.UTC)
	checkinService := &successfulCheckinContractService{
		status: &service.CheckinStatus{
			Enabled: true,
			RewardTiers: []service.DailyCheckinRewardTierStatus{
				{Day: 1, Amount: "1.00000000", PermanentAmount: "0.00000000"},
				{Day: 2, Amount: "2.00000000", PermanentAmount: "0.25000000"},
			},
		},
		result: &service.CheckinResult{
			AlreadyCheckedIn:       false,
			CheckinDate:            "2026-07-18",
			StreakDay:              3,
			RewardDay:              3,
			RewardAmount:           "3.25000000",
			PermanentRewardAmount:  "0.50000000",
			TemporaryCreditGrantID: 77,
			ExpiresAt:              expiresAt,
		},
	}
	previousCoordinator := service.DefaultIdempotencyCoordinator()
	service.SetDefaultIdempotencyCoordinator(service.NewIdempotencyCoordinator(
		&idempotencyContractRepository{},
		service.DefaultIdempotencyConfig(),
	))
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(previousCoordinator) })

	handlers := &handler.Handlers{
		Checkin: handler.NewCheckinHandler(checkinService),
		Admin: &handler.AdminHandlers{
			TemporaryCredit: adminhandler.NewTemporaryCreditHandler(nil),
		},
	}
	engine := gin.New()
	v1 := engine.Group("/api/v1")
	allowUser := middleware.JWTAuthMiddleware(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42, Concurrency: 5})
		c.Next()
	})
	denyAdmin := middleware.AdminAuthMiddleware(func(c *gin.Context) {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED"})
	})
	auditLog := middleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() })
	stepUp := middleware.StepUpAuthMiddleware(func(c *gin.Context) { c.Next() })
	routes.RegisterUserRoutes(v1, handlers, allowUser, auditLog, nil)
	routes.RegisterAdminRoutes(v1, handlers, denyAdmin, auditLog, stepUp, nil)

	registered := make(map[string]bool)
	for _, route := range engine.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	for _, expected := range []string{
		"GET /api/v1/user/check-in",
		"POST /api/v1/user/check-in",
		"POST /api/v1/admin/users/:id/temporary-credits",
		"GET /api/v1/admin/users/:id/temporary-credits",
	} {
		require.True(t, registered[expected], expected)
	}

	statusRequest := httptest.NewRequest(http.MethodGet, "/api/v1/user/check-in?month=2026-07", nil)
	statusRecorder := httptest.NewRecorder()
	engine.ServeHTTP(statusRecorder, statusRequest)
	require.Equal(t, http.StatusOK, statusRecorder.Code, statusRecorder.Body.String())
	var statusEnvelope struct {
		Code int                        `json:"code"`
		Data map[string]json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(statusRecorder.Body.Bytes(), &statusEnvelope))
	require.Zero(t, statusEnvelope.Code)
	require.JSONEq(t, `[
		{"day":1,"amount":"1.00000000","permanent_amount":"0.00000000"},
		{"day":2,"amount":"2.00000000","permanent_amount":"0.25000000"}
	]`, string(statusEnvelope.Data["reward_tiers"]))

	request := httptest.NewRequest(http.MethodPost, "/api/v1/user/check-in", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "checkin-contract-key")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, int64(42), checkinService.userID)
	require.True(t, checkinService.persisted, "handler must persist the success DTO through the atomic claim")

	var envelope struct {
		Code int                        `json:"code"`
		Data map[string]json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Zero(t, envelope.Code)
	require.Len(t, envelope.Data, 8)
	require.Equal(t, json.RawMessage("false"), envelope.Data["already_checked_in"])
	require.Equal(t, json.RawMessage(`"2026-07-18"`), envelope.Data["checkin_date"])
	require.Equal(t, json.RawMessage("3"), envelope.Data["streak_day"])
	require.Equal(t, json.RawMessage("3"), envelope.Data["reward_day"])
	require.Equal(t, json.RawMessage(`"3.25000000"`), envelope.Data["reward_amount"])
	require.Equal(t, json.RawMessage(`"0.50000000"`), envelope.Data["permanent_reward_amount"])
	require.Equal(t, json.RawMessage("77"), envelope.Data["temporary_credit_grant_id"])
	require.Equal(t, json.RawMessage(`"2026-07-18T16:00:00Z"`), envelope.Data["expires_at"])
	_, hasAvailable := envelope.Data["temporary_credit_available"]
	require.False(t, hasAvailable)
}

type successfulCheckinContractService struct {
	status    *service.CheckinStatus
	result    *service.CheckinResult
	userID    int64
	persisted bool
}

func (s *successfulCheckinContractService) GetStatus(context.Context, int64, string) (*service.CheckinStatus, error) {
	return s.status, nil
}

func (s *successfulCheckinContractService) CheckInAtomic(ctx context.Context, userID int64, claim *service.IdempotencyAtomicClaim) (*service.CheckinResult, error) {
	s.userID = userID
	if err := claim.PersistSuccess(ctx, successfulAtomicExecutor{}, s.result); err != nil {
		return nil, err
	}
	s.persisted = true
	return s.result, nil
}

type successfulAtomicExecutor struct{}

func (successfulAtomicExecutor) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return oneRowResult{}, nil
}

type oneRowResult struct{}

func (oneRowResult) LastInsertId() (int64, error) { return 0, nil }
func (oneRowResult) RowsAffected() (int64, error) { return 1, nil }

type idempotencyContractRepository struct{}

func (*idempotencyContractRepository) CreateProcessing(_ context.Context, record *service.IdempotencyRecord) (bool, error) {
	record.ID = 1
	return true, nil
}

func (*idempotencyContractRepository) GetByScopeActorScopeAndKeyHash(context.Context, string, string, string) (*service.IdempotencyRecord, error) {
	return nil, nil
}

func (*idempotencyContractRepository) TryReclaim(context.Context, int64, string, time.Time, time.Time, time.Time) (bool, error) {
	return false, nil
}

func (*idempotencyContractRepository) ExtendProcessingLock(context.Context, int64, string, time.Time, time.Time) (bool, error) {
	return false, nil
}

func (*idempotencyContractRepository) MarkSucceeded(context.Context, int64, int, string, time.Time) error {
	return nil
}

func (*idempotencyContractRepository) MarkFailedRetryable(context.Context, int64, string, time.Time, time.Time) error {
	return nil
}

func (*idempotencyContractRepository) DeleteExpired(context.Context, time.Time, int) (int64, error) {
	return 0, nil
}
