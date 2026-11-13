package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/proxytraffic"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

func TestOpenAIWSTrafficCountsCompressedSocketBytes(t *testing.T) {
	var upload, download atomic.Int64
	unregister := proxytraffic.Register(func(context.Context, string) proxytraffic.Observer {
		return func(up, down int64) { upload.Add(up); download.Add(down) }
	})
	t.Cleanup(unregister)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, &coderws.AcceptOptions{CompressionMode: coderws.CompressionContextTakeover})
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		kind, payload, err := conn.Read(ctx)
		if err == nil {
			_ = conn.Write(ctx, kind, payload)
		}
	}))
	t.Cleanup(proxy.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dialer := newDefaultOpenAIWSClientDialer()
	conn, _, _, err := dialer.Dial(ctx, "ws://upstream.invalid/realtime", nil, proxy.URL)
	require.NoError(t, err)
	input := map[string]string{"input": strings.Repeat("x", 16<<10)}
	require.NoError(t, conn.WriteJSON(ctx, input))
	payload, err := conn.ReadMessage(ctx)
	require.NoError(t, err)
	require.Contains(t, string(payload), input["input"])
	require.NoError(t, conn.Close())
	require.Greater(t, upload.Load(), int64(0))
	require.Greater(t, download.Load(), int64(0))
	require.Less(t, upload.Load(), int64(8<<10))
	require.Less(t, download.Load(), int64(8<<10))
	proxytraffic.CloseConnections()
}
