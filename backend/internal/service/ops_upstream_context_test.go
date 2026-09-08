package service

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSafeUpstreamURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"strips query", "https://api.anthropic.com/v1/messages?beta=true", "https://api.anthropic.com/v1/messages"},
		{"strips fragment", "https://api.openai.com/v1/responses#frag", "https://api.openai.com/v1/responses"},
		{"strips both", "https://host/path?token=secret#x", "https://host/path"},
		{"no query or fragment", "https://host/path", "https://host/path"},
		{"empty string", "", ""},
		{"whitespace only", "  ", ""},
		{"query before fragment", "https://h/p?a=1#f", "https://h/p"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, safeUpstreamURL(tt.input))
		})
	}
}

func TestOpsUpstreamProxyAttributionAndNormalization(t *testing.T) {
	managedID := int64(17)
	managed := &Account{ProxyID: &managedID, Proxy: &Proxy{ID: managedID, Name: " edge "}}
	gotID, gotName := opsUpstreamProxyAttribution(managed)
	require.NotNil(t, gotID)
	require.Equal(t, managedID, *gotID)
	require.Equal(t, "edge", gotName)
	_, gotName = opsUpstreamProxyAttribution(&Account{})
	require.Equal(t, opsProxyNameDirect, gotName)
	badID := int64(0)
	gotID, gotName = opsUpstreamProxyAttribution(&Account{ProxyID: &badID, Proxy: &Proxy{ID: badID}})
	require.Nil(t, gotID)
	require.Equal(t, opsProxyNameUnknown, gotName)

	ev := &OpsUpstreamErrorEvent{ProxyName: "forged"}
	normalizeOpsUpstreamProxyAttribution(ev)
	require.Nil(t, ev.ProxyID)
	require.Equal(t, opsProxyNameUnknown, ev.ProxyName)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	SetOpsUpstreamModel(c, " gpt-6 ")
	v, ok := c.Get(OpsUpstreamModelKey)
	require.True(t, ok)
	require.Equal(t, "gpt-6", v)
	ClearOpsUpstreamModel(c)
	v, _ = c.Get(OpsUpstreamModelKey)
	require.Equal(t, "", v)
}

func TestOpenAIWSPayloadStringViewSharesPayloadContent(t *testing.T) {
	payload := []byte(`{"type":"response.create"}`)
	require.Equal(t, string(payload), openAIWSPayloadStringView(payload))
	require.Empty(t, openAIWSPayloadStringView(nil))
}
