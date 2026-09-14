package handler

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestEntitlementHTTPInputRequiresExplicitTierAndKeepsHeaderIdempotency(t *testing.T) {
	for _, body := range []string{`{"user_ids":[7]}`, `{"user_ids":[7],"tier":""}`, `{"user_ids":[7],"tier":"  "}`, `{"user_ids":[7],"tier":"gold"}`} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest("POST", "/entitlements/preview", strings.NewReader(body))
		ctx.Request.Header.Set("Content-Type", "application/json")
		_, ok := readEntitlementChange(ctx, false)
		require.False(t, ok)
		require.Equal(t, 400, recorder.Code)
	}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/entitlements/apply", strings.NewReader(`{"user_ids":[7,7],"tier":"premium","reason":"reason","preview_token":"signed-preview"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Request.Header.Set("Idempotency-Key", "same-request")
	request, ok := readEntitlementChange(ctx, true)
	require.True(t, ok)
	require.Equal(t, []int64{7}, request.UserIDs)
	require.Equal(t, "same-request", request.RequestID)
	require.Equal(t, "signed-preview", request.PreviewToken)
}

func TestEntitlementHTTPDomainErrorsHaveStableStatus(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{
		{"tier", service.ErrEntitlementTierUnknown, 400}, {"targets", service.ErrEntitlementTargetsInvalid, 400}, {"policy_input", service.ErrEntitlementPolicyInvalid, 400},
		{"permission", service.ErrAdminPermissionDenied, 403}, {"initial_missing", service.ErrUserNotFound, 404},
		{"preview_changed_or_expired", service.ErrEntitlementPreviewConflict, 409}, {"disabled", service.ErrEntitlementDisabled, 409}, {"cas", service.ErrEntitlementVersionConflict, 409},
		{"idempotency", service.ErrEntitlementIdempotencyConflict, 409}, {"last_superadmin", service.ErrLastSuperAdmin, 409}, {"target_changed", service.ErrAdminMutationConflict, 409},
		{"internal", errors.New("internal storage failure"), 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest("GET", "/", nil)
			response.ErrorFrom(ctx, tc.err)
			require.Equal(t, tc.status, recorder.Code)
		})
	}
}
