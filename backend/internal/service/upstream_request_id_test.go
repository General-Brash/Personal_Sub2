package service

import (
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestUpstreamRequestIDUsesOnlyConfiguredHeader(t *testing.T) {
	h := http.Header{}
	h.Set("X-Request-ID", "local")
	h.Set("X-Oneapi-Request-Id", " upstream-1 ")
	require.Empty(t, UpstreamRequestIDFromHeaders(&Account{Platform: PlatformOpenAI}, h))
	require.Equal(t, "upstream-1", UpstreamRequestIDFromHeaders(&Account{Extra: map[string]any{AccountExtraUpstreamRequestIDHeader: " x-oneapi-request-id "}}, h))
}

func TestUsageUpstreamRequestIDPtrTruncatesUTF8AndSkipsWS(t *testing.T) {
	account := &Account{Extra: map[string]any{AccountExtraUpstreamRequestIDHeader: "X-Request-ID"}}
	h := http.Header{}
	h.Set("X-Request-ID", strings.Repeat("界", 100))
	got := usageUpstreamRequestIDPtr(account, h, false)
	require.NotNil(t, got)
	require.LessOrEqual(t, len([]byte(*got)), maxUsageUpstreamRequestIDLen)
	require.True(t, utf8.ValidString(*got))
	require.Nil(t, usageUpstreamRequestIDPtr(account, h, true))
}

func TestValidateUpstreamRequestIDHeaderExtra(t *testing.T) {
	extra := map[string]any{AccountExtraUpstreamRequestIDHeader: " X-Request-ID "}
	require.NoError(t, ValidateUpstreamRequestIDHeaderExtra(extra))
	require.Equal(t, "X-Request-ID", extra[AccountExtraUpstreamRequestIDHeader])
	require.Error(t, ValidateUpstreamRequestIDHeaderExtra(map[string]any{AccountExtraUpstreamRequestIDHeader: "bad header"}))
}
