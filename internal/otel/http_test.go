package otel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

type ctxKey struct{}

// cloning stands in for withUser/requireUser: r.WithContext hands the next
// handler a copy, so a mux behind it sets Pattern where ServerMetrics can't
// see it.
func cloning(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, true)))
	})
}

func TestServerMetricsAttributes(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) {}
	inner := http.NewServeMux()
	inner.HandleFunc("GET /{$}", ok)
	inner.HandleFunc("GET /courses/{slug}", ok)
	top := http.NewServeMux()
	top.HandleFunc("GET /api/v1/courses/{slug}", ok)
	top.Handle("/", cloning(Route(inner)))

	tests := []struct {
		name       string
		method     string
		path       string
		wantRoute  string // "" means the attribute must be absent
		wantStatus int64
		wantMethod string
	}{
		{"top-level match", "GET", "/api/v1/courses/go", "/api/v1/courses/{slug}", 200, "GET"},
		{"nested match", "GET", "/courses/go", "/courses/{slug}", 200, "GET"},
		{"exact-match marker stripped", "GET", "/", "/", 200, "GET"},
		{"nested 404 does not inherit mount", "GET", "/nope", "", 404, "GET"},
		{"nested 405", "POST", "/courses/go", "", 405, "POST"},
		{"unknown method", "BREW", "/courses/go", "", 405, "_OTHER"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := sdkmetric.NewManualReader()
			mw, err := ServerMetrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
			if err != nil {
				t.Fatal(err)
			}
			mw(Route(top)).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(tt.method, tt.path, nil))

			attrs := durationAttrs(t, reader)
			route, hasRoute := attrs.Value("http.route")
			switch {
			case tt.wantRoute == "" && hasRoute:
				t.Errorf("http.route = %q, want absent", route.AsString())
			case tt.wantRoute != "" && route.AsString() != tt.wantRoute:
				t.Errorf("http.route = %q, want %q", route.AsString(), tt.wantRoute)
			}
			if got, _ := attrs.Value("http.response.status_code"); got.AsInt64() != tt.wantStatus {
				t.Errorf("status = %d, want %d", got.AsInt64(), tt.wantStatus)
			}
			if got, _ := attrs.Value("http.request.method"); got.AsString() != tt.wantMethod {
				t.Errorf("method = %q, want %q", got.AsString(), tt.wantMethod)
			}
		})
	}
}

// durationAttrs returns the attribute set of the single request recorded.
func durationAttrs(t *testing.T, reader *sdkmetric.ManualReader) attribute.Set {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "http.server.request.duration" {
				continue
			}
			h := m.Data.(metricdata.Histogram[float64])
			if len(h.DataPoints) != 1 {
				t.Fatalf("%d duration data points, want 1", len(h.DataPoints))
			}
			return h.DataPoints[0].Attributes
		}
	}
	t.Fatal("no http.server.request.duration recorded")
	return attribute.Set{}
}
