package handler

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestOIDCHandlerRejectsUnsupportedAuthorizationParameters(t *testing.T) {
	values := url.Values{"client_id": {"client"}, "request": {"signed"}}
	if !hasUnsupportedAuthorizationParameters(values) {
		t.Fatal("request parameter must be rejected")
	}
	values = url.Values{"client_id": {"client"}, "claims": {"{}"}}
	if !hasUnsupportedAuthorizationParameters(values) {
		t.Fatal("claims parameter must be rejected")
	}
}

func TestOIDCHandlerRejectsDuplicateSingletonParameters(t *testing.T) {
	values := url.Values{"client_id": {"one", "two"}}
	if !hasDuplicateOIDCParameters(values, []string{"client_id"}) {
		t.Fatal("duplicate client_id must be rejected")
	}
}

func TestOIDCHandlerAuthorizationErrorMappingIsComplete(t *testing.T) {
	cases := map[error]string{
		service.ErrOIDCUnsupportedResponseType:  "unsupported_response_type",
		service.ErrOIDCInteractionRequired:      "interaction_required",
		service.ErrOIDCAccountSelectionRequired: "account_selection_required",
		service.ErrOIDCServerError:              "server_error",
		service.ErrOIDCTemporarilyUnavailable:   "temporarily_unavailable",
		service.ErrOIDCLoginRequired:            "login_required",
		service.ErrOIDCConsentRequired:          "consent_required",
		service.ErrOIDCAccessDenied:             "access_denied",
		service.ErrOIDCKeyUnavailable:           "temporarily_unavailable",
	}
	for err, want := range cases {
		if got := tokenErrorCode(err); got != want {
			t.Errorf("tokenErrorCode(%v) = %q, want %q", err, got, want)
		}
	}
}

func TestOIDCTokenReplayMapsToInvalidGrantAndBadRequest(t *testing.T) {
	if got := statusForTokenError(service.ErrOIDCReplayDetected); got != http.StatusBadRequest {
		t.Fatalf("statusForTokenError(replay) = %d, want %d", got, http.StatusBadRequest)
	}
	if got := tokenErrorCode(service.ErrOIDCReplayDetected); got != "invalid_grant" {
		t.Fatalf("tokenErrorCode(replay) = %q, want invalid_grant", got)
	}
}
