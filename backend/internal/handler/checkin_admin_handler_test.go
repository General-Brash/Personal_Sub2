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
	updates      int
	lastExtended *service.DailyCheckinPolicyV2
	forceAll     bool
}

func (s *checkinAdminHandlerServiceStub) GetDailyCheckinPolicyV2(context.Context) (*service.DailyCheckinPolicy, service.DailyCheckinPolicyV2, error) {
	base := service.DefaultDailyCheckinPolicy()
	extended := service.DefaultDailyCheckinPolicyV2()
	if s.forceAll {
		extended.AutoForceAll = true
		extended.AutoFeeBps = 0
	}
	return &base, extended, nil
}

func (s *checkinAdminHandlerServiceStub) UpdateDailyCheckinPolicyV2(_ context.Context, _ *service.DailyCheckinPolicy, extended *service.DailyCheckinPolicyV2, _ ...string) error {
	s.updates++
	s.lastExtended = extended
	if extended != nil && extended.AutoForceAll {
		s.forceAll = true
	}
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

// auto_force_all 必须在 update DTO 里被显式接受：UpdateSettings 使用
// DisallowUnknownFields，缺字段时前端提交会直接 400。
func TestCheckinAdminHandler_UpdateAcceptsForcedAutoCheckinSwitch(t *testing.T) {
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
		"auto_fee_bps": 0,
		"auto_force_all": true,
		"expected_version": "checkin-v2-version"
	}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	h.UpdateSettings(ctx)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, 1, stub.updates)
	require.NotNil(t, stub.lastExtended)
	require.True(t, stub.lastExtended.AutoForceAll)
	require.Zero(t, stub.lastExtended.AutoFeeBps)
	require.Contains(t, recorder.Body.String(), `"auto_force_all":true`)
}
