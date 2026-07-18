package handler

import (
	"context"
	"encoding/json"
	"net/http"

	pkgerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	coderws "github.com/coder/websocket"
)

func billingPricingErrorDetails(err error) (status int, code, message string, ok bool) {
	if err == nil {
		return 0, "", "", false
	}
	code = pkgerrors.Reason(err)
	if code != "BILLING_PRICING_UNAVAILABLE" && code != "BILLING_PRICING_INVALID" {
		return 0, "", "", false
	}
	status = pkgerrors.Code(err)
	if status <= 0 {
		status = http.StatusServiceUnavailable
	}
	message = pkgerrors.Message(err)
	if message == "" {
		message = "billing pricing is temporarily unavailable"
	}
	return status, code, message, true
}

func writeBillingPricingWSError(ctx context.Context, conn *coderws.Conn, err error) (code, message string, ok bool) {
	_, code, message, ok = billingPricingErrorDetails(err)
	if !ok || conn == nil {
		return code, message, ok
	}
	payload, marshalErr := json.Marshal(map[string]any{
		"type": "error",
		"error": map[string]string{
			"type":    code,
			"code":    code,
			"message": message,
		},
	})
	if marshalErr == nil {
		_ = conn.Write(ctx, coderws.MessageText, payload)
	}
	return code, message, true
}

func advancePricingFailover(ctx context.Context, accountID int64, failedAccountIDs map[int64]struct{}, switchCount *int, maxSwitches int) FailoverAction {
	if ctx != nil && ctx.Err() != nil {
		return FailoverCanceled
	}
	failedAccountIDs[accountID] = struct{}{}
	if switchCount == nil || *switchCount >= maxSwitches {
		return FailoverExhausted
	}
	*switchCount++
	return FailoverContinue
}
