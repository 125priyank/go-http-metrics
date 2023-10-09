package gomux

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"bufio"
	"errors"
	"net"

	"github.com/gorilla/mux"
	"github.com/slok/go-http-metrics/middleware"
)

// Handler returns an gomux measuring http.Handler.
func Handler(handlerID string, m middleware.Middleware, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wi := &responseWriterInterceptor{
			statusCode:     http.StatusOK,
			ResponseWriter: w,
		}
		reporter := &stdReporter{
			w: wi,
			r: r,
		}

		m.Measure(handlerID, reporter, func() {
			h.ServeHTTP(wi, r)
		})
	})
}

// HandlerProvider is a helper method that returns a handler provider.
func HandlerProvider(handlerID string, m middleware.Middleware) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return Handler(handlerID, m, next)
	}
}

type stdReporter struct {
	w *responseWriterInterceptor
	r *http.Request
}

func (s *stdReporter) Method() string { return s.r.Method }

func (s *stdReporter) Context() context.Context { return s.r.Context() }

// URLPath returns the template of the gomux endpoint.
func (s *stdReporter) URLPath() string {
	route := mux.CurrentRoute(s.r)
	if route == nil {
		return s.r.URL.Path
	}
	path, err := route.GetPathTemplate()
	if err != nil {
		return s.r.URL.Path
	}
	return path
}

func (s *stdReporter) StatusCode() int { return s.w.statusCode }

func (s *stdReporter) BytesWritten() int64 { return int64(s.w.bytesWritten) }

// GetBody returns a copy of the http request body.
func (s *stdReporter) GetBody() io.ReadCloser {
	buf, _ := io.ReadAll(s.r.Body)
	// Resetting the request body to allow the HTTP handler to read it again, as HTTP bodies can only be read once.
	defer func() {
		s.r.Body = io.NopCloser(bytes.NewBuffer(buf))
	}()
	return io.NopCloser(bytes.NewBuffer(buf))
}

// responseWriterInterceptor is a simple wrapper to intercept set data on a
// ResponseWriter.
type responseWriterInterceptor struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int
}

func (w *responseWriterInterceptor) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *responseWriterInterceptor) Write(p []byte) (int, error) {
	w.bytesWritten += len(p)
	return w.ResponseWriter.Write(p)
}

func (w *responseWriterInterceptor) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("type assertion failed http.ResponseWriter not a http.Hijacker")
	}
	return h.Hijack()
}

func (w *responseWriterInterceptor) Flush() {
	f, ok := w.ResponseWriter.(http.Flusher)
	if !ok {
		return
	}

	f.Flush()
}

// Check interface implementations.
var (
	_ http.ResponseWriter = &responseWriterInterceptor{}
	_ http.Hijacker       = &responseWriterInterceptor{}
	_ http.Flusher        = &responseWriterInterceptor{}
)
