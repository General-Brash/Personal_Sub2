package service

import (
	"context"
	"errors"
	"net/http"
)

type openAIPluginRoundTripFunc func(context.Context, *http.Request, string, *Account) (*http.Response, bool, error)

func doOpenAIUpstreamWithPluginRoute(
	ctx context.Context,
	request *http.Request,
	proxyURL string,
	account *Account,
	route openAIPluginRoundTripFunc,
	fallback func() (*http.Response, error),
) (*http.Response, error) {
	if route != nil {
		response, handled, err := route(ctx, request, proxyURL, account)
		if handled {
			return response, err
		}
	}
	return fallback()
}

func (s *OpenAIGatewayService) shouldForceOpenAIWSHTTPBridge(account *Account) bool {
	return account != nil && (account.Platform == PlatformGrok ||
		(s.pluginManager != nil && s.pluginManager.ShouldRouteOpenAIOAuth(account)))
}

func (s *OpenAIGatewayService) SetPluginManager(manager *PluginManager) {
	s.pluginManager = manager
}

// doOpenAIUpstream 只在 OpenAI OAuth 能力绑定已启用时把真实请求交给插件。
// 插件返回标准 http.Response，响应解析、错误映射、SSE 和计费仍由现有核心链处理。
func (s *OpenAIGatewayService) doOpenAIUpstream(request *http.Request, proxyURL string, account *Account) (*http.Response, error) {
	var route openAIPluginRoundTripFunc
	if s.pluginManager != nil {
		route = s.pluginManager.RoundTripOpenAIOAuth
	}
	return doOpenAIUpstreamWithPluginRoute(request.Context(), request, proxyURL, account, route, func() (*http.Response, error) {
		return s.httpUpstream.Do(request, proxyURL, account.ID, account.Concurrency)
	})
}

// doOpenAIAccountTestUpstream 让 OpenAI OAuth 账号测试与真实转发使用同一插件路径。
// API Key 和未命中插件的账号保持各自原有的 HTTPUpstream 行为。
func (s *AccountTestService) doOpenAIAccountTestUpstream(
	request *http.Request,
	proxyURL string,
	account *Account,
	useTLSFallback bool,
) (*http.Response, error) {
	var route openAIPluginRoundTripFunc
	if s.pluginManager != nil {
		route = s.pluginManager.RoundTripOpenAIOAuth
	}
	return doOpenAIUpstreamWithPluginRoute(request.Context(), request, proxyURL, account, route, func() (*http.Response, error) {
		if useTLSFallback {
			return s.httpUpstream.DoWithTLS(
				request,
				proxyURL,
				account.ID,
				account.Concurrency,
				s.tlsFPProfileService.ResolveTLSProfile(account),
			)
		}
		return s.httpUpstream.Do(request, proxyURL, account.ID, account.Concurrency)
	})
}

// openAIPluginRequestWasSent distinguishes plugin transport failures after the
// request may have crossed the plugin boundary. Such failures must not be
// converted into a WS account failover/retry, which could duplicate a turn.
func openAIPluginRequestWasSent(err error) bool {
	var transportErr *PluginTransportError
	return errors.As(err, &transportErr) && transportErr.RequestSent
}
