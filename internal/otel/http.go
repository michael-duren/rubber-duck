package otel

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

const httpMeterName = "github.com/michael-duren/rubber-duck/internal/otel"

// ServerMetrics returns middleware recording HTTP server metrics. Instrument
// names, units and duration buckets match otelchi's metric package, so the
// exported series are interchangeable with the chi-based services':
// http.server.request.duration, http.server.active_requests,
// http.server.request.body.size and http.server.response.body.size.
func ServerMetrics(mp metric.MeterProvider) (func(http.Handler) http.Handler, error) {
	m := mp.Meter(httpMeterName)
	duration, err1 := m.Float64Histogram("http.server.request.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of HTTP server requests."),
		metric.WithExplicitBucketBoundaries(
			0.005, 0.01, 0.025, 0.05, 0.075, 0.1,
			0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10,
		))
	active, err2 := m.Int64UpDownCounter("http.server.active_requests",
		metric.WithUnit("{request}"),
		metric.WithDescription("Number of active HTTP server requests."))
	reqSize, err3 := m.Int64Histogram("http.server.request.body.size",
		metric.WithUnit("By"),
		metric.WithDescription("Size of HTTP server request bodies."))
	respSize, err4 := m.Int64Histogram("http.server.response.body.size",
		metric.WithUnit("By"),
		metric.WithDescription("Size of HTTP server response bodies."))
	if err := errors.Join(err1, err2, err3, err4); err != nil {
		return nil, err
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			// Route and status are unknown until the handler runs, so
			// in-flight requests carry only method and scheme.
			inflight := metric.WithAttributes(requestAttributes(r)...)
			active.Add(ctx, 1, inflight)
			defer active.Add(ctx, -1, inflight)

			rt := new(route)
			r = r.WithContext(context.WithValue(ctx, routeKey{}, rt))
			body := &countingBody{ReadCloser: r.Body}
			if r.Body != nil && r.Body != http.NoBody {
				r.Body = body
			}
			sw := &statusWriter{ResponseWriter: w}

			start := time.Now()
			next.ServeHTTP(sw, r)
			elapsed := time.Since(start)
			if sw.status == 0 {
				// Nothing written: net/http replies 200 on the handler's
				// behalf once it returns.
				sw.status = http.StatusOK
			}

			attrs := metric.WithAttributes(httpAttributes(r, rt, sw.status)...)
			duration.Record(ctx, elapsed.Seconds(), attrs)
			reqSize.Record(ctx, body.n, attrs)
			respSize.Record(ctx, sw.n, attrs)
		})
	}, nil
}

type routeKey struct{}

// route carries the innermost matched ServeMux pattern back out to
// ServerMetrics. set distinguishes "a mux ran and matched nothing" (a 404,
// which must not inherit an outer mount point) from "no Route wrapper ran".
type route struct {
	pattern string
	set     bool
}

// Route records the pattern mux matches as the request's http.route. Wrap
// muxes nested behind request-cloning middleware (r.WithContext hands the
// mux a copy, so ServerMetrics can't see the Pattern it sets); without it
// every such request would report the mount point. The innermost wrapper
// wins. The top-level mux needs no wrapper as long as the middleware
// between it and ServerMetrics passes the request through unchanged.
func Route(mux http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, r)
		if rt, ok := r.Context().Value(routeKey{}).(*route); ok && !rt.set {
			rt.pattern, rt.set = r.Pattern, true
		}
	})
}

var knownMethods = map[string]attribute.KeyValue{
	http.MethodGet:     semconv.HTTPRequestMethodGet,
	http.MethodHead:    semconv.HTTPRequestMethodHead,
	http.MethodPost:    semconv.HTTPRequestMethodPost,
	http.MethodPut:     semconv.HTTPRequestMethodPut,
	http.MethodPatch:   semconv.HTTPRequestMethodPatch,
	http.MethodDelete:  semconv.HTTPRequestMethodDelete,
	http.MethodOptions: semconv.HTTPRequestMethodOptions,
	http.MethodConnect: semconv.HTTPRequestMethodConnect,
	http.MethodTrace:   semconv.HTTPRequestMethodTrace,
}

// requestAttributes are known before routing: the normalized method (so
// arbitrary verbs can't mint series) and the scheme.
func requestAttributes(r *http.Request) []attribute.KeyValue {
	method, ok := knownMethods[r.Method]
	if !ok {
		method = semconv.HTTPRequestMethodOther
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return []attribute.KeyValue{method, semconv.URLScheme(scheme)}
}

// httpAttributes keeps metric cardinality bounded: http.route is the matched
// path template (absent when nothing matched) and the status code is only
// known once the handler has written.
func httpAttributes(r *http.Request, rt *route, status int) []attribute.KeyValue {
	attrs := requestAttributes(r)
	pattern := r.Pattern
	if rt.set {
		pattern = rt.pattern
	}
	if tmpl := pathTemplate(pattern); tmpl != "" {
		attrs = append(attrs, semconv.HTTPRoute(tmpl))
	}
	if status != 0 {
		attrs = append(attrs, semconv.HTTPResponseStatusCode(status))
	}
	return attrs
}

// pathTemplate reduces a ServeMux pattern ("[METHOD ][HOST]/path") to its
// path, which is what http.route holds; the method is its own attribute. The
// exact-match marker {$} is mux syntax, not part of the route.
func pathTemplate(pattern string) string {
	i := strings.IndexByte(pattern, '/')
	if i < 0 {
		return ""
	}
	return strings.TrimSuffix(pattern[i:], "{$}")
}

// statusWriter captures the response status and body size. Unwrap keeps
// http.ResponseController working through it.
type statusWriter struct {
	http.ResponseWriter
	status int
	n      int64
}

func (w *statusWriter) WriteHeader(code int) {
	// 1xx responses are interim; the final status comes later.
	if w.status == 0 && code >= 200 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.n += int64(n)
	return n, err
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// countingBody counts request body bytes the handler actually read.
type countingBody struct {
	io.ReadCloser
	n int64
}

func (b *countingBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.n += int64(n)
	return n, err
}
