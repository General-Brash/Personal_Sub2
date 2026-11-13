package repository

import (
	"compress/gzip"
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/proxytraffic"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestHTTPUpstreamTrafficCountsWireBytesForCompressedStreamErrorsAndRetries(t *testing.T) {
	var upload, download atomic.Int64
	var route string
	unregister := proxytraffic.Register(func(_ context.Context, raw string) proxytraffic.Observer {
		if proxytraffic.Key(raw) != proxytraffic.Key(route) {
			return nil
		}
		return func(up, down int64) { upload.Add(up); download.Add(down) }
	})
	t.Cleanup(unregister)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		switch r.URL.Path {
		case "/compressed":
			w.Header().Set("Content-Encoding", "gzip")
			compressed := gzip.NewWriter(w)
			_, _ = io.WriteString(compressed, strings.Repeat("x", 1<<20))
			_ = compressed.Close()
		case "/stream":
			flusher, ok := w.(http.Flusher)
			if !ok {
				t.Error("stream response writer does not support flushing")
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: first\n\n")
			flusher.Flush()
			_, _ = io.WriteString(w, "data: second\n\n")
		case "/broken":
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				t.Error("broken response writer does not support hijacking")
				return
			}
			conn, _, err := hijacker.Hijack()
			if err == nil {
				_ = conn.Close()
			}
		default:
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, "upstream unavailable")
		}
	}))
	t.Cleanup(proxy.Close)
	route = proxy.URL
	upstream := NewHTTPUpstream(nil)

	req, err := http.NewRequest(http.MethodPost, "http://upstream.invalid/compressed", strings.NewReader(strings.Repeat("p", 4096)))
	require.NoError(t, err)
	resp, err := upstream.Do(req, route, 1, 1)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Len(t, body, 1<<20)
	require.Greater(t, upload.Load(), int64(4096))
	require.Greater(t, download.Load(), int64(0))
	require.Less(t, download.Load(), int64(10<<10), "count compressed network bytes, not the expanded body")

	beforeUp, beforeDown := upload.Load(), download.Load()
	req, err = http.NewRequest(http.MethodGet, "http://upstream.invalid/stream", nil)
	require.NoError(t, err)
	resp, err = upstream.Do(req, route, 1, 1)
	require.NoError(t, err)
	body, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "data: first\n\ndata: second\n\n", string(body))
	require.NoError(t, resp.Body.Close())
	require.Greater(t, upload.Load(), beforeUp)
	require.Greater(t, download.Load(), beforeDown)

	for range 2 {
		beforeUp, beforeDown = upload.Load(), download.Load()
		req, err = http.NewRequest(http.MethodPost, "http://upstream.invalid/error", strings.NewReader("request"))
		require.NoError(t, err)
		resp, err = upstream.Do(req, route, 1, 1)
		require.NoError(t, err)
		require.Equal(t, http.StatusBadGateway, resp.StatusCode)
		_, err = io.Copy(io.Discard, resp.Body)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		require.Greater(t, upload.Load(), beforeUp)
		require.Greater(t, download.Load(), beforeDown)
	}
	beforeUp = upload.Load()
	req, err = http.NewRequest(http.MethodPost, "http://upstream.invalid/broken", strings.NewReader("failed request"))
	require.NoError(t, err)
	resp, err = upstream.Do(req, route, 1, 1)
	require.Error(t, err)
	require.Nil(t, resp)
	require.Greater(t, upload.Load(), beforeUp)
	proxytraffic.CloseConnections()
}

func TestHTTPUpstreamTrafficSOCKSCountsBothDirections(t *testing.T) {
	proxyURL, _ := startTestSOCKS5Proxy(t)
	var upload, download atomic.Int64
	unregister := proxytraffic.Register(func(context.Context, string) proxytraffic.Observer {
		return func(up, down int64) { upload.Add(up); download.Add(down) }
	})
	t.Cleanup(unregister)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = io.WriteString(w, "response")
	}))
	t.Cleanup(target.Close)
	req, err := http.NewRequest(http.MethodPost, target.URL, strings.NewReader("payload"))
	require.NoError(t, err)
	resp, err := NewHTTPUpstream(nil).Do(req, proxyURL, 1, 1)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "response", string(body))
	require.NoError(t, resp.Body.Close())
	require.Greater(t, upload.Load(), int64(len("payload")))
	require.Greater(t, download.Load(), int64(len("response")))
	proxytraffic.CloseConnections()
}

func startTrafficConnectProxy(t *testing.T, targetAddr string) string {
	t.Helper()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		target, err := net.Dial("tcp", targetAddr)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Error("CONNECT response writer does not support hijacking")
			_ = target.Close()
			return
		}
		client, buffered, err := hijacker.Hijack()
		if err != nil {
			_ = target.Close()
			return
		}
		_, _ = buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		_ = buffered.Flush()
		go func() {
			_, _ = io.Copy(target, buffered)
			_ = target.Close()
			_ = client.Close()
		}()
		go func() {
			_, _ = io.Copy(client, target)
			_ = client.Close()
			_ = target.Close()
		}()
	}))
	t.Cleanup(proxy.Close)
	return proxy.URL
}

func TestHTTPUpstreamTrafficTLSHTTP2PreservesProtocolAndCountsEncryptedBytes(t *testing.T) {
	var upload, download atomic.Int64
	unregister := proxytraffic.Register(func(context.Context, string) proxytraffic.Observer {
		return func(up, down int64) { upload.Add(up); download.Add(down) }
	})
	t.Cleanup(unregister)
	target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, 2, r.ProtoMajor)
		_, _ = io.WriteString(w, "h2 response")
	}))
	target.EnableHTTP2 = true
	target.StartTLS()
	t.Cleanup(target.Close)
	proxyRaw := startTrafficConnectProxy(t, target.Listener.Addr().String())
	proxyURL, err := url.Parse(proxyRaw)
	require.NoError(t, err)
	transport, err := buildUpstreamTransport(defaultPoolSettings(nil), proxyURL, upstreamProtocolModeOpenAIH2)
	require.NoError(t, err)
	targetTransport, ok := target.Client().Transport.(*http.Transport)
	require.True(t, ok, "TLS test client must use an HTTP transport")
	transport.TLSClientConfig = &tls.Config{RootCAs: targetTransport.TLSClientConfig.RootCAs}
	client := &http.Client{Transport: transport}
	t.Cleanup(client.CloseIdleConnections)
	resp, err := client.Get(target.URL)
	require.NoError(t, err)
	require.Equal(t, 2, resp.ProtoMajor)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "h2 response", string(body))
	require.NoError(t, resp.Body.Close())
	require.Greater(t, upload.Load(), int64(100))
	require.Greater(t, download.Load(), int64(len(body)))
	proxytraffic.CloseConnections()
}

func TestHTTPUpstreamTrafficTLSFingerprintRecordsConnectAndFailedHandshake(t *testing.T) {
	var upload, download atomic.Int64
	unregister := proxytraffic.Register(func(context.Context, string) proxytraffic.Observer {
		return func(up, down int64) { upload.Add(up); download.Add(down) }
	})
	t.Cleanup(unregister)
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "untrusted")
	}))
	t.Cleanup(target.Close)
	proxyRaw := startTrafficConnectProxy(t, target.Listener.Addr().String())
	req, err := http.NewRequest(http.MethodGet, target.URL, nil)
	require.NoError(t, err)
	resp, err := NewHTTPUpstream(nil).DoWithTLS(req, proxyRaw, 1, 1, &tlsfingerprint.Profile{Name: "local-test"})
	require.Error(t, err, "self-signed local certificate must still be rejected")
	require.Nil(t, resp)
	require.Greater(t, upload.Load(), int64(0))
	require.Greater(t, download.Load(), int64(0))
	proxytraffic.CloseConnections()
}

func TestHTTPUpstreamTrafficNodeIdentitySeparatesSharedProxyPools(t *testing.T) {
	upstream, ok := NewHTTPUpstream(nil).(*httpUpstreamService)
	require.True(t, ok, "upstream must expose the concrete HTTP service")
	first, err := upstream.getClientEntryForNode("http://same.example:8080", 1, 1, service.HTTPUpstreamProfileDefault, false, false, 7)
	require.NoError(t, err)
	second, err := upstream.getClientEntryForNode("http://same.example:8080", 1, 1, service.HTTPUpstreamProfileDefault, false, false, 8)
	require.NoError(t, err)
	require.NotSame(t, first, second)
	firstAgain, err := upstream.getClientEntryForNode("http://same.example:8080", 1, 1, service.HTTPUpstreamProfileDefault, false, false, 7)
	require.NoError(t, err)
	require.Same(t, first, firstAgain)
}
