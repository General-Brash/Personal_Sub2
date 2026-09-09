package repository

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func http2KeepAliveTestPoolSettings() poolSettings {
	return poolSettings{
		maxIdleConns:          10,
		maxIdleConnsPerHost:   5,
		maxConnsPerHost:       10,
		idleConnTimeout:       90 * time.Second,
		responseHeaderTimeout: time.Minute,
	}
}

// Codex/OpenAI 上游改走 HTTP/2 后，池化连接被代理/NAT 静默掐断会成为“死连接”：
// 两端都以为连接存活，请求落上去会挂到 TCP 重传超时（分钟级）才失败。Go 的
// http2.Transport 默认 ReadIdleTimeout=0（不发健康 PING），无法检测这种死连接。
// 必须显式启用主动 PING 探测，让死连接被提前剔除，而不是只靠 ResponseHeaderTimeout
// 事后兜底。
func TestEnableOpenAIHTTP2KeepAlive_EnablesPingHealthCheck(t *testing.T) {
	tr := &http.Transport{}

	h2, err := enableOpenAIHTTP2KeepAlive(tr)
	require.NoError(t, err)
	require.NotNil(t, h2, "必须返回已配置的 *http2.Transport")

	require.Positive(t, h2.ReadIdleTimeout, "必须启用空闲 PING 探测以剔除死连接")
	require.Equal(t, openAIHTTP2ReadIdleTimeout, h2.ReadIdleTimeout)
	require.Equal(t, openAIHTTP2PingTimeout, h2.PingTimeout, "PING 无响应必须有超时判定")
}

// openai_h2 模式必须在真实 TLS/ALPN 会话中协商到 HTTP/2。断言公开的响应协议，
// 而不是 http.Transport.TLSNextProto 的内部安装细节；后者在 Go 1.27/x/net
// 下不是稳定的测试契约。
func TestBuildUpstreamTransport_OpenAIH2_NegotiatesHTTP2(t *testing.T) {
	upstreamProtocol := make(chan string, 1)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamProtocol <- r.Proto
		w.WriteHeader(http.StatusNoContent)
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()

	tr, err := buildUpstreamTransport(http2KeepAliveTestPoolSettings(), nil, upstreamProtocolModeOpenAIH2)
	require.NoError(t, err)
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // #nosec G402 -- httptest self-signed certificate

	response, err := (&http.Client{Transport: tr}).Get(server.URL)
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()
	require.Equal(t, http.StatusNoContent, response.StatusCode)
	require.Equal(t, "HTTP/2.0", response.Proto)
	require.Equal(t, "HTTP/2.0", <-upstreamProtocol)
}

// 非 H2 模式不应因 keepalive 配置改变公共 Transport 行为：default 保持 Go 的
// 自动协商策略，而显式 H1 模式关闭主动 H2 尝试。避免波及非 OpenAI 热路径。
func TestBuildUpstreamTransport_NonOpenAIH2_PreservesProtocolMode(t *testing.T) {
	defaultTransport, err := buildUpstreamTransport(http2KeepAliveTestPoolSettings(), nil, upstreamProtocolModeDefault)
	require.NoError(t, err)
	require.False(t, defaultTransport.ForceAttemptHTTP2)

	h1Transport, err := buildUpstreamTransport(http2KeepAliveTestPoolSettings(), nil, upstreamProtocolModeOpenAIH1)
	require.NoError(t, err)
	require.False(t, h1Transport.ForceAttemptHTTP2)
}

// 死连接在经 HTTP 代理（CONNECT 隧道）时最高发，这是带 proxy 账号的真实生产路径：
// 显式 H2 尝试须与 Transport.Proxy 的公开路由行为同时保留。
func TestBuildUpstreamTransport_OpenAIH2_WithHTTPProxy_EnablesKeepAlive(t *testing.T) {
	proxyURL, err := url.Parse("http://127.0.0.1:8080")
	require.NoError(t, err)

	tr, err := buildUpstreamTransport(http2KeepAliveTestPoolSettings(), proxyURL, upstreamProtocolModeOpenAIH2)
	require.NoError(t, err)
	require.True(t, tr.ForceAttemptHTTP2)
	require.NotNil(t, tr.Proxy, "HTTP 代理仍须通过 Transport.Proxy 生效")
}
