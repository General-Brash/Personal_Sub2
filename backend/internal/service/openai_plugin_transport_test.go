package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIPluginRequestWasSent(t *testing.T) {
	postSend := &PluginTransportError{RequestSent: true}
	preSend := &PluginTransportError{RequestSent: false}
	require.True(t, openAIPluginRequestWasSent(postSend))
	require.True(t, openAIPluginRequestWasSent(errors.Join(errors.New("outer"), postSend)))
	require.False(t, openAIPluginRequestWasSent(preSend))
	require.False(t, openAIPluginRequestWasSent(errors.New("ordinary transport failure")))
	require.False(t, openAIPluginRequestWasSent(nil))
}

func TestDoOpenAIUpstreamWithPluginRouteHitMissAndRequestSentSemantics(t *testing.T) {
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.com/v1/responses", nil)
	require.NoError(t, err)
	account := &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth}

	t.Run("miss falls back exactly once", func(t *testing.T) {
		fallbackCalls := 0
		response, err := doOpenAIUpstreamWithPluginRoute(request.Context(), request, "", account,
			func(context.Context, *http.Request, string, *Account) (*http.Response, bool, error) {
				return nil, false, nil
			},
			func() (*http.Response, error) {
				fallbackCalls++
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("legacy"))}, nil
			},
		)
		require.NoError(t, err)
		require.NotNil(t, response)
		require.Equal(t, 1, fallbackCalls)
		_ = response.Body.Close()
	})

	for _, requestSent := range []bool{false, true} {
		requestSent := requestSent
		t.Run(map[bool]string{false: "hit pre-send failure", true: "hit post-send failure"}[requestSent], func(t *testing.T) {
			fallbackCalls := 0
			transportErr := &PluginTransportError{RequestSent: requestSent, Message: "plugin transport failed"}
			response, gotErr := doOpenAIUpstreamWithPluginRoute(request.Context(), request, "", account,
				func(context.Context, *http.Request, string, *Account) (*http.Response, bool, error) {
					return nil, true, transportErr
				},
				func() (*http.Response, error) {
					fallbackCalls++
					return nil, nil
				},
			)
			require.Nil(t, response)
			require.ErrorIs(t, gotErr, transportErr)
			require.Equal(t, requestSent, openAIPluginRequestWasSent(gotErr))
			require.Zero(t, fallbackCalls, "a plugin hit must never duplicate the request on the legacy transport")
		})
	}
}

func TestShouldForceOpenAIWSHTTPBridgeForPluginHitOnly(t *testing.T) {
	manager := &PluginManager{}
	manager.route.Store(&pluginRoute{pluginID: 1, rolloutPercent: 100, unavailable: "test"})
	svc := &OpenAIGatewayService{pluginManager: manager}

	require.True(t, svc.shouldForceOpenAIWSHTTPBridge(&Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}))
	require.False(t, svc.shouldForceOpenAIWSHTTPBridge(&Account{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}))
	require.False(t, svc.shouldForceOpenAIWSHTTPBridge(&Account{ID: 3, Platform: PlatformAnthropic, Type: AccountTypeOAuth}))
	require.True(t, svc.shouldForceOpenAIWSHTTPBridge(&Account{ID: 4, Platform: PlatformGrok, Type: AccountTypeOAuth}))
	require.False(t, svc.shouldForceOpenAIWSHTTPBridge(nil))
}
