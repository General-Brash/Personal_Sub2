package proxytraffic

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type partialConn struct {
	net.Conn
}

func (*partialConn) Read(p []byte) (int, error) {
	return copy(p, "abc"), io.EOF
}

func (*partialConn) Write([]byte) (int, error) {
	return 2, io.ErrShortWrite
}

func (*partialConn) Close() error { return nil }

func TestObservedConnCountsActualBytesAndPreservesErrors(t *testing.T) {
	var upload, download int64
	unregister := Register(func(_ context.Context, raw string) Observer {
		require.Equal(t, "http://node.example:8080", raw)
		return func(up, down int64) { upload += up; download += down }
	})
	t.Cleanup(unregister)
	base := &partialConn{}
	conn := WrapConn(base, "http://node.example:8080")
	require.Same(t, conn, WrapConn(conn, "http://node.example:8080"))
	buffer := make([]byte, 8)
	n, err := conn.Read(buffer)
	require.Equal(t, 3, n)
	require.ErrorIs(t, err, io.EOF)
	n, err = conn.Write([]byte("longer payload"))
	require.Equal(t, 2, n)
	require.ErrorIs(t, err, io.ErrShortWrite)
	require.Equal(t, int64(2), upload)
	require.Equal(t, int64(3), download)
	require.NoError(t, conn.Close())
}

func TestObservedConnSkipsDirectUnmanagedAndFailedDials(t *testing.T) {
	base := &partialConn{}
	require.Same(t, base, WrapConn(base, ""))
	unregister := Register(func(context.Context, string) Observer { return nil })
	t.Cleanup(unregister)
	require.Same(t, base, WrapConn(base, "http://unmanaged.example:80"))
	dialErr := errors.New("dial failed")
	dial := WrapDialContext(func(context.Context, string, string) (net.Conn, error) {
		return nil, dialErr
	}, "http://node.example:80")
	conn, err := dial(context.Background(), "tcp", "node.example:80")
	require.Nil(t, conn)
	require.ErrorIs(t, err, dialErr)
}

func TestProxyTrafficKeyNormalizesRoutes(t *testing.T) {
	require.Equal(t, Key("http://user:pass@node.example:80"), Key("HTTP://user:pass@NODE.example/path?ignored=1#fragment"))
	require.Equal(t, Key("socks5h://[::1]:1080"), Key("socks5://[::1]"))
	require.NotEqual(t, Key("http://user:one@node.example:80"), Key("http://user:two@node.example:80"))
	require.NotEqual(t, Key("http://node.example:80"), Key("https://node.example:443"))
	require.Empty(t, Key(""))
	require.Empty(t, Key("://invalid"))
}

func TestCloseConnectionsWaitsForInFlightObservation(t *testing.T) {
	client, server := net.Pipe()
	t.Cleanup(func() { _ = server.Close() })
	observing := make(chan struct{})
	release := make(chan struct{})
	unregister := Register(func(context.Context, string) Observer {
		return func(_, download int64) {
			if download > 0 {
				close(observing)
				<-release
			}
		}
	})
	t.Cleanup(unregister)
	conn := WrapConn(client, "http://node.example:80")
	readDone := make(chan struct{})
	go func() {
		_, _ = conn.Read(make([]byte, 8))
		close(readDone)
	}()
	go func() { _, _ = server.Write([]byte("abc")) }()
	<-observing
	closed := make(chan struct{})
	go func() { CloseConnections(); close(closed) }()
	select {
	case <-closed:
		t.Fatal("connection closed before its final bytes were observed")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("connection close did not finish")
	}
	<-readDone
	_, err := conn.Write([]byte("after close"))
	require.ErrorIs(t, err, net.ErrClosed)
}
