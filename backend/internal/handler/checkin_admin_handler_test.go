package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type checkinAdminHandlerServiceStub struct {
	updates int
}

func (s *checkinAdminHandlerServiceStub) GetDailyCheckinPolicyV2(context.Context) (*service.DailyCheckinPolicy, service.DailyCheckinPolicyV2, error) {
	base := service.DefaultDailyCheckinPolicy()
	extended := service.DefaultDailyCheckinPolicyV2()
	return &base, extended, nil
}

func (s *checkinAdminHandlerServiceStub) UpdateDailyCheckinPolicyV2(context.Context, *service.DailyCheckinPolicy, *service.DailyCheckinPolicyV2, ...string) error {
	s.updates++
	return nil
}

func TestCheckinAdminHandler_UpdateRejectsRetiredGameFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &checkinAdminHandlerServiceStub{}
	h := NewCheckinAdminHandler(stub)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings/checkin/v2", bytes.NewBufferString(`{
		"enabled": true,
		"max_reward_day": 1,
		"reward_tiers": [{"day":1,"amount":"1.00000000","permanent_amount":"0.00000000"}],
		"version": "checkin-v2-version",
		"refresh_time": "00:00",
		"auto_fee_bps": 500,
		"expected_version": "checkin-v2-version",
		"normal": {"enabled": true},
		"super": {"enabled": true, "cost": "1.00000000"},
		"reviewed": true
	}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	h.UpdateSettings(ctx)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"reason":"INVALID_DAILY_CHECKIN_POLICY"`)
	require.Zero(t, stub.updates)
}
