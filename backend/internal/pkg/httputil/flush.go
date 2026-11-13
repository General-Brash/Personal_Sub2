package httputil

import "net/http"

// FlushResponse preserves error-aware wrapper hooks and unwraps void flushers
// (including Gin) before falling back to a transport's legacy Flush method.
func FlushResponse(w http.ResponseWriter) error {
	for w != nil {
		if flusher, ok := w.(interface{ FlushError() error }); ok {
			return flusher.FlushError()
		}
		if unwrapper, ok := w.(interface{ Unwrap() http.ResponseWriter }); ok {
			// Gin's Flush commits its staged status before flushing the transport.
			if writer, ok := w.(interface{ WriteHeaderNow() }); ok {
				writer.WriteHeaderNow()
			}
			w = unwrapper.Unwrap()
			continue
		}
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
			return nil
		}
		return http.ErrNotSupported
	}
	return http.ErrNotSupported
}
