package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCanonicalHost(t *testing.T) {
	tests := []struct {
		name         string
		method       string
		host         string
		target       string
		wantStatus   int
		wantLocation string
	}{
		{
			name:         "www redirects to apex preserving path and query",
			method:       http.MethodGet,
			host:         "www.duckgc.com",
			target:       "/courses/intro?x=1",
			wantStatus:   http.StatusPermanentRedirect,
			wantLocation: "https://duckgc.com/courses/intro?x=1",
		},
		{
			name:       "non-GET on www still redirects with 308",
			method:     http.MethodPost,
			host:       "www.duckgc.com",
			target:     "/login",
			wantStatus: http.StatusPermanentRedirect,
		},
		{
			name:       "apex falls through",
			method:     http.MethodGet,
			host:       "duckgc.com",
			target:     "/",
			wantStatus: http.StatusOK,
		},
		{
			name:       "localhost falls through",
			method:     http.MethodGet,
			host:       "localhost:8080",
			target:     "/",
			wantStatus: http.StatusOK,
		},
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.target, nil)
			req.Host = tt.host
			rec := httptest.NewRecorder()

			CanonicalHost(next).ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantLocation != "" {
				if got := rec.Header().Get("Location"); got != tt.wantLocation {
					t.Errorf("Location = %q, want %q", got, tt.wantLocation)
				}
			}
		})
	}
}
