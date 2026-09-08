package service

import (
	"net/http"

	"strings"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"golang.org/x/net/http/httpguts"
)

// AccountExtraUpstreamRequestIDHeader is the account extra key containing the
// explicitly configured direct-upstream response header name.
const AccountExtraUpstreamRequestIDHeader = "upstream_request_id_header"

const (
	maxUpstreamRequestIDHeaderNameLen = 64
	maxUsageUpstreamRequestIDLen      = 128
)

// UpstreamRequestIDHeaderName returns the configured response header name.
func UpstreamRequestIDHeaderName(account *Account) string {
	if account == nil {
		return ""
	}
	return strings.TrimSpace(account.GetExtraString(AccountExtraUpstreamRequestIDHeader))
}

// UpstreamRequestIDFromHeaders reads only the explicitly configured header.
func UpstreamRequestIDFromHeaders(account *Account, headers http.Header) string {
	name := UpstreamRequestIDHeaderName(account)
	if name == "" || len(headers) == 0 {
		return ""
	}
	return strings.TrimSpace(headers.Get(name))
}

// usageUpstreamRequestIDPtr prepares the nullable usage-log value. WS turns do
// not have a single HTTP response header and therefore intentionally return nil.
func usageUpstreamRequestIDPtr(account *Account, headers http.Header, wsMode bool) *string {
	if wsMode {
		return nil
	}
	id := UpstreamRequestIDFromHeaders(account, headers)
	if id == "" {
		return nil
	}
	if len(id) > maxUsageUpstreamRequestIDLen {
		id = id[:maxUsageUpstreamRequestIDLen]
		for len(id) > 0 && !utf8.ValidString(id) {
			id = id[:len(id)-1]
		}
	}
	if id == "" {
		return nil
	}
	return &id
}

// ValidateUpstreamRequestIDHeaderExtra validates and normalizes account extra.
func ValidateUpstreamRequestIDHeaderExtra(extra map[string]any) error {
	if extra == nil {
		return nil
	}
	raw, ok := extra[AccountExtraUpstreamRequestIDHeader]
	if !ok || raw == nil {
		return nil
	}
	name, ok := raw.(string)
	if !ok {
		return infraerrors.BadRequest("INVALID_UPSTREAM_REQUEST_ID_HEADER", "upstream_request_id_header must be a string")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		delete(extra, AccountExtraUpstreamRequestIDHeader)
		return nil
	}
	if len(name) > maxUpstreamRequestIDHeaderNameLen || !httpguts.ValidHeaderFieldName(name) {
		return infraerrors.BadRequest("INVALID_UPSTREAM_REQUEST_ID_HEADER", "upstream_request_id_header must be a valid HTTP header name of at most 64 bytes")
	}
	extra[AccountExtraUpstreamRequestIDHeader] = name
	return nil
}
