package gatewaytiming

import (
	"crypto/tls"
	"net/http"
	"net/http/httptrace"
	"sync"
	"time"
)

type tracePoint struct {
	count uint64
	at    time.Time
}

func (p *tracePoint) record(at time.Time) {
	p.count++
	if p.at.IsZero() {
		p.at = at
	}
}

type traceOperation struct {
	start      tracePoint
	done       tracePoint
	errorCount uint64
}

type operationSnapshot struct {
	StartCount         uint64   `json:"start_count"`
	DoneCount          uint64   `json:"done_count"`
	ErrorCount         uint64   `json:"error_count"`
	MultipleOperations bool     `json:"multiple_operations"`
	DurationMS         *float64 `json:"duration_ms,omitempty"`
}

type traceCounts struct {
	GetConn           uint64 `json:"get_conn"`
	GotConn           uint64 `json:"got_conn"`
	WroteHeaders      uint64 `json:"wrote_headers"`
	WroteRequest      uint64 `json:"wrote_request"`
	WriteErrors       uint64 `json:"write_errors"`
	FirstResponseByte uint64 `json:"first_response_byte"`
	CallbacksAfterDo  uint64 `json:"callbacks_after_do_return"`
}

type httpAttemptSnapshot struct {
	Attempt                        uint64            `json:"attempt"`
	AccountID                      int64             `json:"account_id"`
	StartOffsetMS                  *float64          `json:"start_offset_ms,omitempty"`
	DoReturnOffsetMS               *float64          `json:"do_return_offset_ms,omitempty"`
	DoReturnMS                     *float64          `json:"do_return_ms,omitempty"`
	ResponseHeadersMS              *float64          `json:"response_headers_ms,omitempty"`
	ResponseHeadersOffsetMS        *float64          `json:"response_headers_offset_ms,omitempty"`
	StatusCode                     *int              `json:"status_code,omitempty"`
	TransportError                 *bool             `json:"transport_error,omitempty"`
	RequestContentLength           *int64            `json:"request_content_length,omitempty"`
	ResponseContentLength          *int64            `json:"response_content_length,omitempty"`
	MultipleExchanges              bool              `json:"multiple_exchanges"`
	Trace                          traceCounts       `json:"trace"`
	DNS                            operationSnapshot `json:"dns"`
	TCPConnect                     operationSnapshot `json:"tcp_connect"`
	TLSHandshake                   operationSnapshot `json:"tls_handshake"`
	ConnectionReused               *bool             `json:"connection_reused,omitempty"`
	ConnectionWasIdle              *bool             `json:"connection_was_idle,omitempty"`
	ConnectionIdleMS               *float64          `json:"connection_idle_ms,omitempty"`
	TransportPrepareMS             *float64          `json:"transport_prepare_ms,omitempty"`
	AcquireMS                      *float64          `json:"acquire_ms,omitempty"`
	ConnectionToHeadersWrittenMS   *float64          `json:"connection_to_headers_written_ms,omitempty"`
	RequestWriteMS                 *float64          `json:"request_write_ms,omitempty"`
	HeadersToRequestWrittenMS      *float64          `json:"headers_to_request_written_ms,omitempty"`
	RequestWritten                 *bool             `json:"request_written,omitempty"`
	RequestWrittenOffsetMS         *float64          `json:"request_written_offset_ms,omitempty"`
	WriteCallbackOffsetMS          *float64          `json:"write_callback_offset_ms,omitempty"`
	HeadersToWriteCallbackMS       *float64          `json:"headers_to_write_callback_ms,omitempty"`
	FirstResponseByteOffsetMS      *float64          `json:"first_response_byte_offset_ms,omitempty"`
	ResponseBeforeRequestWritten   *bool             `json:"response_before_request_written,omitempty"`
	RequestWrittenToFirstByteMS    *float64          `json:"request_written_to_first_byte_ms,omitempty"`
	RequestWrittenToHeadersMS      *float64          `json:"request_written_to_headers_ms,omitempty"`
	FirstByteToHeadersMS           *float64          `json:"first_byte_to_headers_ms,omitempty"`
	FirstVisibleOffsetMS           *float64          `json:"first_visible_offset_ms,omitempty"`
	FirstVisibleMS                 *float64          `json:"first_visible_ms,omitempty"`
	RequestWrittenToFirstVisibleMS *float64          `json:"request_written_to_first_visible_ms,omitempty"`
	HeadersToFirstVisibleMS        *float64          `json:"headers_to_first_visible_ms,omitempty"`
}

type httpAttempt struct {
	mu               sync.Mutex
	number           uint64
	accountID        int64
	requestStartedAt time.Time
	startedAt        time.Time
	endedAt          time.Time
	hasResponse      bool
	transportError   bool
	statusCode       int
	requestLength    *int64
	responseLength   *int64
	firstVisibleAt   time.Time

	getConn           tracePoint
	gotConn           tracePoint
	wroteHeaders      tracePoint
	wroteRequest      tracePoint
	firstByte         tracePoint
	writeErrors       uint64
	callbacksAfterDo  uint64
	dns               traceOperation
	tcp               traceOperation
	tls               traceOperation
	connectionReused  bool
	connectionWasIdle bool
	connectionIdle    time.Duration
}

// StartHTTPAttempt instruments exactly one service-level Do invocation. The
// returned request is a shallow copy with only a client trace added to its
// context. Body, GetBody, Header, URL and ContentLength are unchanged, preserving
// net/http's reader, WriterTo and flush paths. ContentLength is declared metadata,
// not bytes sent. A successful WroteRequest (including HTTP/2) is only a transport
// callback, not proof that the peer has received the complete request body.
func StartHTTPAttempt(req *http.Request, accountID int64) (*http.Request, func(*http.Response, error)) {
	if req == nil {
		return req, finishNoop
	}
	c := FromContext(req.Context())
	if c == nil {
		return req, finishNoop
	}
	a := &httpAttempt{
		accountID:        accountID,
		requestStartedAt: c.startedAt,
		startedAt:        time.Now(),
	}
	// Zero with a non-empty Body is unknown for outgoing net/http requests.
	if req.ContentLength > 0 || (req.ContentLength == 0 && (req.Body == nil || req.Body == http.NoBody)) {
		a.requestLength = knownSize(req.ContentLength)
	}
	c.mu.Lock()
	c.attemptCount++
	a.number = c.attemptCount
	if len(c.attempts) < maxAttempts {
		c.attempts = append(c.attempts, a)
	} else {
		// Keep the first maxAttempts-1 attempts and the latest attempt.
		c.attempts[maxAttempts-1] = a
		c.truncated.HTTPAttempts++
	}
	c.latestAttempt = a
	c.mu.Unlock()

	traced := req.WithContext(httptrace.WithClientTrace(req.Context(), a.clientTrace()))
	return traced, func(response *http.Response, err error) {
		now := time.Now()
		a.mu.Lock()
		defer a.mu.Unlock()
		if !a.endedAt.IsZero() {
			return
		}
		a.endedAt = now
		a.transportError = err != nil
		if response != nil {
			a.hasResponse = true
			a.statusCode = response.StatusCode
			a.responseLength = knownSize(response.ContentLength)
		}
	}
}

func finishNoop(*http.Response, error) {}

// event serializes only collector updates, never the transport's work or an
// existing trace hook. Arguments containing addresses/errors are not retained.
func (a *httpAttempt) event(record func(time.Time)) {
	now := time.Now()
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.endedAt.IsZero() && now.After(a.endedAt) {
		a.callbacksAfterDo++
	}
	record(now)
}

func (a *httpAttempt) clientTrace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		GetConn: func(string) {
			a.event(func(now time.Time) { a.getConn.record(now) })
		},
		GotConn: func(info httptrace.GotConnInfo) {
			a.event(func(now time.Time) {
				a.gotConn.record(now)
				if a.gotConn.count == 1 {
					a.connectionReused = info.Reused
					a.connectionWasIdle = info.WasIdle
					a.connectionIdle = info.IdleTime
				}
			})
		},
		DNSStart: func(httptrace.DNSStartInfo) {
			a.event(func(now time.Time) { a.dns.start.record(now) })
		},
		DNSDone: func(info httptrace.DNSDoneInfo) {
			a.event(func(now time.Time) {
				a.dns.done.record(now)
				if info.Err != nil {
					a.dns.errorCount++
				}
			})
		},
		ConnectStart: func(string, string) {
			a.event(func(now time.Time) { a.tcp.start.record(now) })
		},
		ConnectDone: func(_, _ string, err error) {
			a.event(func(now time.Time) {
				a.tcp.done.record(now)
				if err != nil {
					a.tcp.errorCount++
				}
			})
		},
		TLSHandshakeStart: func() {
			a.event(func(now time.Time) { a.tls.start.record(now) })
		},
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
			a.event(func(now time.Time) {
				a.tls.done.record(now)
				if err != nil {
					a.tls.errorCount++
				}
			})
		},
		WroteHeaders: func() {
			a.event(func(now time.Time) { a.wroteHeaders.record(now) })
		},
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			a.event(func(now time.Time) {
				a.wroteRequest.record(now)
				if info.Err != nil {
					a.writeErrors++
				}
			})
		},
		GotFirstResponseByte: func() {
			a.event(func(now time.Time) { a.firstByte.record(now) })
		},
	}
}

func (operation traceOperation) snapshot(allowDuration bool, boundary time.Time) operationSnapshot {
	snapshot := operationSnapshot{
		StartCount:         operation.start.count,
		DoneCount:          operation.done.count,
		ErrorCount:         operation.errorCount,
		MultipleOperations: operation.start.count > 1 || operation.done.count > 1,
	}
	// Hooks have no unique DNS/TLS operation identifier. With parallel dials we
	// keep counts, not a fabricated pairing between different connections.
	if allowDuration && operation.start.count == 1 && operation.done.count == 1 &&
		(boundary.IsZero() || !operation.done.at.After(boundary)) {
		snapshot.DurationMS = durationMS(operation.start.at, operation.done.at)
	}
	return snapshot
}

func (a *httpAttempt) snapshot() httpAttemptSnapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	multiple := a.getConn.count > 1 || a.gotConn.count > 1 || a.wroteRequest.count > 1 ||
		a.wroteHeaders.count > 1 || a.firstByte.count > 1
	snapshot := httpAttemptSnapshot{
		Attempt:               a.number,
		AccountID:             a.accountID,
		StartOffsetMS:         durationMS(a.requestStartedAt, a.startedAt),
		DoReturnOffsetMS:      durationMS(a.requestStartedAt, a.endedAt),
		DoReturnMS:            durationMS(a.startedAt, a.endedAt),
		RequestContentLength:  copyInt64(a.requestLength),
		ResponseContentLength: copyInt64(a.responseLength),
		MultipleExchanges:     multiple,
		FirstVisibleOffsetMS:  durationMS(a.requestStartedAt, a.firstVisibleAt),
		FirstVisibleMS:        durationMS(a.startedAt, a.firstVisibleAt),
		Trace: traceCounts{
			GetConn: a.getConn.count, GotConn: a.gotConn.count,
			WroteHeaders: a.wroteHeaders.count, WroteRequest: a.wroteRequest.count,
			WriteErrors: a.writeErrors, FirstResponseByte: a.firstByte.count,
			CallbacksAfterDo: a.callbacksAfterDo,
		},
	}
	if !a.endedAt.IsZero() {
		failed := a.transportError
		snapshot.TransportError = &failed
	}
	if a.hasResponse {
		if a.statusCode > 0 {
			status := a.statusCode
			snapshot.StatusCode = &status
		}
		// Do returning a response is the available final-headers boundary. It
		// may include redirects/client work; it is not a socket-only duration.
		snapshot.ResponseHeadersMS = durationMS(a.startedAt, a.endedAt)
		snapshot.ResponseHeadersOffsetMS = durationMS(a.requestStartedAt, a.endedAt)
		snapshot.HeadersToFirstVisibleMS = durationMS(a.endedAt, a.firstVisibleAt)
	}
	// Even one background dial can lose to a reused pooled connection. Do not
	// attribute its DNS/TCP/TLS timings to acquisition of that pooled connection.
	allowConnectionStages := !multiple && (a.gotConn.count != 1 || !a.connectionReused)
	boundary := a.endedAt
	if a.gotConn.count == 1 && (boundary.IsZero() || a.gotConn.at.Before(boundary)) {
		boundary = a.gotConn.at
	}
	snapshot.DNS = a.dns.snapshot(allowConnectionStages, boundary)
	snapshot.TCPConnect = a.tcp.snapshot(allowConnectionStages, boundary)
	snapshot.TLSHandshake = a.tls.snapshot(allowConnectionStages, boundary)
	if multiple {
		return snapshot
	}
	if a.getConn.count == 1 {
		// Includes local client setup and URL validation before acquisition.
		snapshot.TransportPrepareMS = durationMS(a.startedAt, a.getConn.at)
	}
	if a.gotConn.count == 1 {
		reused, idle := a.connectionReused, a.connectionWasIdle
		snapshot.ConnectionReused, snapshot.ConnectionWasIdle = &reused, &idle
		if idle && a.connectionIdle >= 0 {
			idleMS := float64(a.connectionIdle) / float64(time.Millisecond)
			snapshot.ConnectionIdleMS = &idleMS
		}
		if a.getConn.count == 1 {
			snapshot.AcquireMS = durationMS(a.getConn.at, a.gotConn.at)
		}
		if a.wroteHeaders.count == 1 {
			snapshot.ConnectionToHeadersWrittenMS = durationMS(a.gotConn.at, a.wroteHeaders.at)
		}
	}
	if a.firstByte.count == 1 {
		snapshot.FirstResponseByteOffsetMS = durationMS(a.requestStartedAt, a.firstByte.at)
		if a.hasResponse {
			snapshot.FirstByteToHeadersMS = durationMS(a.firstByte.at, a.endedAt)
		}
	}
	if a.wroteRequest.count == 1 {
		written := a.writeErrors == 0
		snapshot.RequestWritten = &written
		snapshot.WriteCallbackOffsetMS = durationMS(a.requestStartedAt, a.wroteRequest.at)
		if a.wroteHeaders.count == 1 {
			snapshot.HeadersToWriteCallbackMS = durationMS(a.wroteHeaders.at, a.wroteRequest.at)
		}
		if written {
			// Success means the transport callback ran, not that the peer received
			// the complete body. Missing/failed callbacks leave ordering unknown.
			snapshot.RequestWrittenOffsetMS = durationMS(a.requestStartedAt, a.wroteRequest.at)
			if a.gotConn.count == 1 {
				snapshot.RequestWriteMS = durationMS(a.gotConn.at, a.wroteRequest.at)
			}
			if a.wroteHeaders.count == 1 {
				// Includes 100-continue waits, body reads, scheduling and buffered
				// writes/flushes. This is deliberately not named bandwidth time.
				snapshot.HeadersToRequestWrittenMS = durationMS(a.wroteHeaders.at, a.wroteRequest.at)
			}
			if a.firstByte.count == 1 {
				early := a.firstByte.at.Before(a.wroteRequest.at)
				snapshot.ResponseBeforeRequestWritten = &early
				snapshot.RequestWrittenToFirstByteMS = durationMS(a.wroteRequest.at, a.firstByte.at)
			}
			if a.hasResponse {
				snapshot.RequestWrittenToHeadersMS = durationMS(a.wroteRequest.at, a.endedAt)
			}
			snapshot.RequestWrittenToFirstVisibleMS = durationMS(a.wroteRequest.at, a.firstVisibleAt)
		}
	}
	return snapshot
}
