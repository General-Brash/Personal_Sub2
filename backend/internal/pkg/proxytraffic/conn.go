package proxytraffic

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
)

type Observer func(uploadBytes, downloadBytes int64)
type ObserverFactory func(ctx context.Context, proxyURL string) Observer

type observerRegistration struct {
	factory ObserverFactory
}

var registration atomic.Pointer[observerRegistration]
var connections = struct {
	sync.Mutex
	items map[*observedConn]struct{}
}{items: make(map[*observedConn]struct{})}

// Register installs the application collector without coupling transports to storage.
func Register(factory ObserverFactory) func() {
	entry := &observerRegistration{factory: factory}
	registration.Store(entry)
	return func() {
		connections.Lock()
		registration.CompareAndSwap(entry, nil)
		connections.Unlock()
	}
}

func WrapConn(conn net.Conn, proxyURL string) net.Conn {
	return WrapConnContext(context.Background(), conn, proxyURL)
}

func WrapConnContext(ctx context.Context, conn net.Conn, proxyURL string) net.Conn {
	if conn == nil || proxyURL == "" {
		return conn
	}
	if _, alreadyWrapped := conn.(*observedConn); alreadyWrapped {
		return conn
	}
	entry := registration.Load()
	if entry == nil || entry.factory == nil {
		return conn
	}
	observer := entry.factory(ctx, proxyURL)
	if observer == nil {
		return conn
	}
	wrapped := &observedConn{Conn: conn, observe: observer}
	connections.Lock()
	if registration.Load() != entry {
		connections.Unlock()
		_ = conn.Close()
		return conn
	}
	connections.items[wrapped] = struct{}{}
	connections.Unlock()
	return wrapped
}

// CloseConnections quiesces observed sockets before the final storage flush.
func CloseConnections() {
	connections.Lock()
	items := make([]*observedConn, 0, len(connections.items))
	for conn := range connections.items {
		items = append(items, conn)
	}
	connections.Unlock()
	for _, conn := range items {
		_ = conn.Close()
	}
}

type observedConn struct {
	net.Conn
	observe  Observer
	mu       sync.Mutex
	active   sync.WaitGroup
	closed   bool
	once     sync.Once
	closeErr error
}

func (c *observedConn) begin() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	c.active.Add(1)
	return true
}

func (c *observedConn) Read(p []byte) (int, error) {
	if !c.begin() {
		return 0, net.ErrClosed
	}
	defer c.active.Done()
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.observe(0, int64(n))
	}
	return n, err
}

func (c *observedConn) Write(p []byte) (int, error) {
	if !c.begin() {
		return 0, net.ErrClosed
	}
	defer c.active.Done()
	n, err := c.Conn.Write(p)
	if n > 0 {
		c.observe(int64(n), 0)
	}
	return n, err
}

func (c *observedConn) Close() error {
	c.once.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()
		c.closeErr = c.Conn.Close()
		c.active.Wait()
		connections.Lock()
		delete(connections.items, c)
		connections.Unlock()
	})
	return c.closeErr
}

type Dialer interface {
	Dial(network, address string) (net.Conn, error)
}

type observedDialer struct {
	base     Dialer
	proxyURL string
}

func WrapDialer(base Dialer, proxyURL string) *observedDialer {
	return &observedDialer{base: base, proxyURL: proxyURL}
}

func (d *observedDialer) Dial(network, address string) (net.Conn, error) {
	conn, err := d.base.Dial(network, address)
	return WrapConn(conn, d.proxyURL), err
}

func (d *observedDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if base, ok := d.base.(interface {
		DialContext(context.Context, string, string) (net.Conn, error)
	}); ok {
		conn, err := base.DialContext(ctx, network, address)
		return WrapConnContext(ctx, conn, d.proxyURL), err
	}
	conn, err := d.base.Dial(network, address)
	return WrapConnContext(ctx, conn, d.proxyURL), err
}

func WrapDialContext(base func(context.Context, string, string) (net.Conn, error), proxyURL string) func(context.Context, string, string) (net.Conn, error) {
	if base == nil {
		base = (&net.Dialer{}).DialContext
	}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := base(ctx, network, address)
		return WrapConnContext(ctx, conn, proxyURL), err
	}
}
