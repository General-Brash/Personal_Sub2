package repository

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
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

// ============================================================================
// OpenAI H2 保活行为测试（TestOpenAIHTTP2KeepAlive*）。
//
// 生产配置（openAIHTTP2ReadIdleTimeout/PingTimeout）是 const 语义默认常量，
// 测试不可改值；且配置层不暴露该时限。因此行为测试采用独立构造的短时限
// http2.Transport（300ms/200ms）验证 PING 机制本身；生产路径是否正确写入
// 10s/5s 已由 TestEnableOpenAIHTTP2KeepAlive_EnablesPingHealthCheck 覆盖。
// 使用毫秒级探测时限，不等待生产 10 秒的空闲窗口。
//
// peer 使用真实 TLS-ALPN HTTP/2 + Framer + hpack，单 goroutine 独占读取：
//   - 可选择是否自动回 PING ACK（验证静默流存活 / 失活关闭）
//   - 计数收到的应用请求 HEADERS（验证不重复请求 / 连接复用）
//   - 把非 ACK PING 数据发布到通道（验证 PING 与正文隔离）
//
// 说明：请求取消在 x/net 里是流级 RST，不关闭整条连接；只有失活的健康 PING
// 才触发 closeForLostPing 关闭连接。因此“取消/断连释放资源”按“请求立即结束、
// 不重放、连接不被错误重建”断言，而“无 ACK 关闭”单独断言服务端观察到连接关闭。
// ============================================================================

const (
	openAIHTTP2TestReadIdle = 300 * time.Millisecond
	openAIHTTP2TestPingAck  = 200 * time.Millisecond
)

// newOpenAIHTTP2KeepAliveTestTransport 构造短时限的 H2 transport，行为与生产
// enableOpenAIHTTP2KeepAlive 完全一致（同一个 http2.ConfigureTransports 路径），
// 仅 ReadIdleTimeout/PingTimeout 缩短以便测试。
func newOpenAIHTTP2KeepAliveTestTransport(t *testing.T) *http.Transport {
	t.Helper()
	tr := &http.Transport{
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true}, // #nosec G402 -- local self-signed
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		MaxIdleConnsPerHost:   5,
		MaxConnsPerHost:       10,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: time.Minute,
	}
	h2, err := http2.ConfigureTransports(tr)
	require.NoError(t, err)
	require.NotNil(t, h2)
	h2.ReadIdleTimeout = openAIHTTP2TestReadIdle
	h2.PingTimeout = openAIHTTP2TestPingAck
	return tr
}

// openAIHTTP2TestPeer 是测试专属的 TLS-ALPN HTTP/2 服务端 peer。
type openAIHTTP2TestPeer struct {
	t *testing.T

	addr    string
	connMu  sync.Mutex
	conn    net.Conn
	closing bool
	doneCh  chan struct{}
	fr      *http2.Framer
	enc     *hpack.Encoder
	buf     bytes.Buffer

	// 是否自动回 PING ACK
	ack bool
	// blockResponses=true 时收到 HEADERS 不写响应，用于取消测试（保持无响应）。
	blockResponses bool
	// silentResponse sends headers but holds DATA until two successful PINGs.
	silentResponse bool
	pendingStream  uint32 // owned by the frame-loop goroutine

	requests atomic.Int64
	pings    atomic.Int64
	// requestSeen 缓冲通知测试：收到应用请求 HEADERS 的流号。
	requestSeen chan uint32
	pingCh      chan [8]byte
	closedCh    chan struct{}
	closeOnce   sync.Once
}

var openAIHTTP2TestCertOnce sync.Once
var openAIHTTP2TestCert tls.Certificate
var openAIHTTP2TestCertErr error

// openAIHTTP2TestTLSCert 生成/缓存本地自签 TLS 证书（P-256，仅测试环回用）。
func openAIHTTP2TestTLSCert(t *testing.T) tls.Certificate {
	t.Helper()
	openAIHTTP2TestCertOnce.Do(func() {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			openAIHTTP2TestCertErr = err
			return
		}
		tmpl := &x509.Certificate{
			SerialNumber: big.NewInt(1),
			Subject:      pkix.Name{CommonName: "openai-http2-keepalive-test"},
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().Add(time.Hour),
			KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
			ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
			IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
		if err != nil {
			openAIHTTP2TestCertErr = err
			return
		}
		openAIHTTP2TestCert = tls.Certificate{
			Certificate: [][]byte{der},
			PrivateKey:  key,
		}
	})
	if openAIHTTP2TestCertErr != nil {
		t.Fatalf("生成测试 TLS 证书失败: %v", openAIHTTP2TestCertErr)
	}
	return openAIHTTP2TestCert
}

// startOpenAIHTTP2TestPeer 启动本地 H2 peer。ack 控制是否自动回 ACK；
// opts 可在 Listen 已建但尚未 accept/loop 前调整 peer 行为（无数据竞争）。
func startOpenAIHTTP2TestPeer(t *testing.T, ack bool, opts ...func(*openAIHTTP2TestPeer)) *openAIHTTP2TestPeer {
	t.Helper()
	cert := openAIHTTP2TestTLSCert(t)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		NextProtos:   []string{http2.NextProtoTLS},
		Certificates: []tls.Certificate{cert},
	})
	require.NoError(t, err)

	peer := &openAIHTTP2TestPeer{
		t:    t,
		addr: "https://" + ln.Addr().String(),
		ack:  ack,

		pingCh:      make(chan [8]byte, 8),
		closedCh:    make(chan struct{}),
		doneCh:      make(chan struct{}),
		requestSeen: make(chan uint32, 1),
	}
	for _, opt := range opts {
		opt(peer)
	}

	go func() {
		defer close(peer.doneCh)
		defer peer.markClosed()
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		peer.connMu.Lock()
		if peer.closing {
			peer.connMu.Unlock()
			return
		}
		peer.conn = conn
		peer.connMu.Unlock()
		tlsConn, ok := conn.(*tls.Conn)
		if !ok {
			_ = conn.Close()
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return
		}
		if got := tlsConn.ConnectionState().NegotiatedProtocol; got != http2.NextProtoTLS {
			_ = conn.Close()
			return
		}
		peer.fr = http2.NewFramer(conn, conn)
		peer.enc = hpack.NewEncoder(&peer.buf)
		// HTTP/2 客户端必须先在字节流上写 preface；Framer 解析帧前先消费掉。
		if _, err := io.ReadFull(conn, make([]byte, len(http2.ClientPreface))); err != nil {
			return
		}
		// 合法的服务端连接序：SETTINGS（首帧）→ SETTINGS ACK → WINDOW_UPDATE。
		// x/net transport 只要求首帧是 SETTINGS，不要求等客户端的 ACK。
		_ = peer.fr.WriteSettings(
			http2.Setting{ID: http2.SettingEnablePush, Val: 0},
			http2.Setting{ID: http2.SettingInitialWindowSize, Val: 1 << 20},
		)
		_ = peer.fr.WriteSettingsAck()
		_ = peer.fr.WriteWindowUpdate(0, 1<<20)
		peer.loop()
	}()

	t.Cleanup(func() {
		_ = ln.Close()
		peer.connMu.Lock()
		peer.closing = true
		conn := peer.conn
		peer.connMu.Unlock()
		if conn != nil {
			_ = conn.Close()
		}
		select {
		case <-peer.doneCh:
		case <-time.After(3 * time.Second):
			t.Error("HTTP/2 peer did not stop after cleanup")
		}
	})
	return peer
}

// markClosed 只关闭一次 closedCh。
func (p *openAIHTTP2TestPeer) markClosed() {
	p.closeOnce.Do(func() { close(p.closedCh) })
}

// loop 是 peer 唯一的帧读取者。
func (p *openAIHTTP2TestPeer) loop() {
	defer p.markClosed()
	for {
		frame, err := p.fr.ReadFrame()
		if err != nil {
			return
		}
		switch f := frame.(type) {
		case *http2.HeadersFrame:
			p.requests.Add(1)
			select {
			case p.requestSeen <- f.StreamID:
			default:
			}
			if p.blockResponses {
				continue
			}
			p.writeResponse(f.StreamID)
		case *http2.PingFrame:
			if !f.IsAck() {
				pingCount := p.pings.Add(1)
				select {
				case p.pingCh <- f.Data:
				default:
				}
				if p.ack {
					_ = p.fr.WritePing(true, f.Data)
					if p.pendingStream != 0 && pingCount >= 2 {
						_ = p.fr.WriteData(p.pendingStream, true, []byte("ok"))
						p.pendingStream = 0
					}
				}
			}
		}
	}
}

func (p *openAIHTTP2TestPeer) writeResponse(streamID uint32) {
	p.buf.Reset()
	_ = p.enc.WriteField(hpack.HeaderField{Name: ":status", Value: "200"})
	_ = p.enc.WriteField(hpack.HeaderField{Name: "content-type", Value: "text/plain"})
	_ = p.fr.WriteHeaders(http2.HeadersFrameParam{
		StreamID:      streamID,
		EndHeaders:    true,
		BlockFragment: p.buf.Bytes(),
	})
	if p.silentResponse {
		p.pendingStream = streamID
		return
	}
	_ = p.fr.WriteData(streamID, true, []byte("ok"))
}

func waitOpenAIHTTP2Ping(t *testing.T, peer *openAIHTTP2TestPeer, timeout time.Duration) [8]byte {
	t.Helper()
	select {
	case data := <-peer.pingCh:
		return data
	case <-time.After(timeout):
		t.Fatalf("未在 %v 内收到健康 PING", timeout)
		return [8]byte{}
	}
}

func waitOpenAIHTTP2ConnClosed(t *testing.T, peer *openAIHTTP2TestPeer, timeout time.Duration) {
	t.Helper()
	select {
	case <-peer.closedCh:
	case <-time.After(timeout):
		t.Fatalf("连接未在 %v 内被判定失活并关闭", timeout)
	}
}

func openAIHTTP2GetBody(t *testing.T, client *http.Client, url string) string {
	t.Helper()
	resp, err := client.Get(url)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(body)
}

// TestOpenAIHTTP2KeepAlive_HealthyPingKeepsIdleConn 验证：静默空闲的 H2 连接
// 在对端正常回 PING ACK 时不被误判失活；连接保持，后续请求复用同一连接。
func TestOpenAIHTTP2KeepAlive_HealthyPingKeepsIdleConn(t *testing.T) {
	peer := startOpenAIHTTP2TestPeer(t, true)
	client := &http.Client{Transport: newOpenAIHTTP2KeepAliveTestTransport(t), Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()

	require.Equal(t, "ok", openAIHTTP2GetBody(t, client, peer.addr+"/v1/chat/completions"))

	// 第一个健康 PING（空闲超过 ReadIdle，peer 自动回 ACK）。
	waitOpenAIHTTP2Ping(t, peer, 2*time.Second)

	// 继续静默超过 ReadIdle+PingTimeout，连接应仍存活。
	time.Sleep(2 * time.Second)
	waitOpenAIHTTP2Ping(t, peer, 2*time.Second)

	require.Equal(t, "ok", openAIHTTP2GetBody(t, client, peer.addr+"/v1/chat/completions"))

	// 只创建过一条连接：peer 只收到 2 个应用请求 HEADERS（第二个复用原连接）。
	require.Equal(t, int64(2), peer.requests.Load(), "健康 PING 不应导致连接重建或请求重复")
}

// TestOpenAIHTTP2KeepAlive_NoAckClosesIdleConn 验证：健康 PING 在 PingTimeout
// 内无 ACK 时，连接被判定失活并关闭（服务端侧可观测），且不自动重发请求。
func TestOpenAIHTTP2KeepAlive_NoAckClosesIdleConn(t *testing.T) {
	peer := startOpenAIHTTP2TestPeer(t, false)
	client := &http.Client{Transport: newOpenAIHTTP2KeepAliveTestTransport(t), Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()

	require.Equal(t, "ok", openAIHTTP2GetBody(t, client, peer.addr+"/v1/chat/completions"))

	waitOpenAIHTTP2Ping(t, peer, 2*time.Second) // 不回 ACK
	waitOpenAIHTTP2ConnClosed(t, peer, 3*time.Second)

	// 短暂窗口内不得出现第二次请求（连接级剔除，不是自动重试）。
	time.Sleep(200 * time.Millisecond)
	require.Equal(t, int64(1), peer.requests.Load(), "失活关闭后不得自动重发请求")
}

// TestOpenAIHTTP2KeepAlive_PingNotInResponseBody 验证：健康 PING/ACK 是连接级
// 协议帧，绝不混入应用响应正文；请求正文只包含服务端写入的内容。
func TestOpenAIHTTP2KeepAlive_PingNotInResponseBody(t *testing.T) {
	peer := startOpenAIHTTP2TestPeer(t, true)
	client := &http.Client{Transport: newOpenAIHTTP2KeepAliveTestTransport(t), Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()

	body := openAIHTTP2GetBody(t, client, peer.addr+"/v1/chat/completions")
	require.Equal(t, "ok", body)

	// 触发并完成一次 PING 往返，随后仍只读得到服务端正文。
	waitOpenAIHTTP2Ping(t, peer, 2*time.Second)
	require.Equal(t, "ok", openAIHTTP2GetBody(t, client, peer.addr+"/v1/chat/completions"))
	require.NotContains(t, body, "PING")
}

// TestOpenAIHTTP2KeepAlive_CancelReleasesConnAndNoRetry 验证：请求取消后立即
// 结束（不再占用请求资源），不自动重放；连接是流级修复而不是被错误重建重发。
func TestOpenAIHTTP2KeepAlive_CancelReleasesConnAndNoRetry(t *testing.T) {
	peer := startOpenAIHTTP2TestPeer(t, false, func(p *openAIHTTP2TestPeer) {
		p.blockResponses = true
	})
	client := &http.Client{Transport: newOpenAIHTTP2KeepAliveTestTransport(t), Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		peer.addr+"/v1/chat/completions", nil)
	require.NoError(t, err)

	result := make(chan error, 1)
	go func() {
		_, err := client.Do(req)
		result <- err
	}()

	// 等服务端已收到 HEADERS（请求确实发出且只发出一次），再取消。
	select {
	case streamID := <-peer.requestSeen:
		require.Greater(t, streamID, uint32(0), "请求流号必须有效")
	case <-time.After(3 * time.Second):
		t.Fatal("服务端未收到请求 HEADERS")
	}
	cancel()

	select {
	case err := <-result:
		require.Error(t, err, "取消后请求必须返回错误")
	case <-time.After(3 * time.Second):
		t.Fatal("请求取消后未及时释放（RoundTrip 未返回）")
	}

	// 不自动重发：短暂窗口后服务端仍只看到 1 个请求。
	time.Sleep(200 * time.Millisecond)
	require.Equal(t, int64(1), peer.requests.Load(), "取消后不得自动重发请求")
}

// Exercise a still-open response, not just an idle connection between requests.
// No application DATA is written until two connection-level PINGs are ACKed.
func TestOpenAIHTTP2KeepAlive_SilentActiveStream(t *testing.T) {
	for _, tc := range []struct {
		name string
		ack  bool
	}{
		{"ACK keeps a silent response alive", true},
		{"missing ACK aborts a silent response", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			peer := startOpenAIHTTP2TestPeer(t, tc.ack, func(p *openAIHTTP2TestPeer) {
				p.silentResponse = true
			})
			client := &http.Client{Transport: newOpenAIHTTP2KeepAliveTestTransport(t), Timeout: 5 * time.Second}
			defer client.CloseIdleConnections()
			resp, err := client.Get(peer.addr + "/v1/chat/completions")
			require.NoError(t, err)
			defer func() { _ = resp.Body.Close() }()
			require.Equal(t, 2, resp.ProtoMajor)

			body, readErr := io.ReadAll(resp.Body)
			if tc.ack {
				require.NoError(t, readErr)
				require.Equal(t, "ok", string(body), "PING frames must not enter the response body")
				require.GreaterOrEqual(t, peer.pings.Load(), int64(2))
			} else {
				require.Error(t, readErr)
				require.NotContains(t, readErr.Error(), "Client.Timeout", "the unacknowledged PING must close the connection first")
				require.Empty(t, body)
				waitOpenAIHTTP2ConnClosed(t, peer, 3*time.Second)
			}
			require.Equal(t, int64(1), peer.requests.Load(), "connection health checks must not replay a generation request")
		})
	}
}
