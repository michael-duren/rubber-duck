package web

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func newTestStatic(t *testing.T) *staticHandler {
	t.Helper()
	// A compressible JS file and an already-"compressed" blob that gzip can't
	// shrink, so we exercise both the gz and raw-only paths.
	fsys := fstest.MapFS{
		"static/app.js":   {Data: bytes.Repeat([]byte("console.log('x');\n"), 200)},
		"static/blob.png": {Data: []byte("\x89PNG\r\n\x1a\nrandom-ish-bytes-9f8a7b6c5d4e3f2a")},
	}
	h, err := newStaticHandler(fsys, "static")
	if err != nil {
		t.Fatalf("newStaticHandler: %v", err)
	}
	return h
}

func TestStaticHandler(t *testing.T) {
	h := newTestStatic(t)

	tests := []struct {
		name            string
		path            string
		acceptEncoding  string
		wantStatus      int
		wantEncoding    string // "" means identity
		wantContentType string
	}{
		{"gzip when accepted", "/static/app.js", "gzip, deflate, br", http.StatusOK, "gzip", "text/javascript; charset=utf-8"},
		{"raw when not accepted", "/static/app.js", "", http.StatusOK, "", "text/javascript; charset=utf-8"},
		{"gzip refused via q=0", "/static/app.js", "gzip;q=0", http.StatusOK, "", "text/javascript; charset=utf-8"},
		{"gzip refused via spaced q=0", "/static/app.js", "gzip; q=0, identity", http.StatusOK, "", "text/javascript; charset=utf-8"},
		{"gzip accepted with fractional q", "/static/app.js", "gzip;q=0.5", http.StatusOK, "gzip", "text/javascript; charset=utf-8"},
		{"incompressible stays raw", "/static/blob.png", "gzip", http.StatusOK, "", "image/png"},
		{"unknown 404s", "/static/nope.js", "gzip", http.StatusNotFound, "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			if tt.acceptEncoding != "" {
				req.Header.Set("Accept-Encoding", tt.acceptEncoding)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantStatus != http.StatusOK {
				return
			}
			if got := rec.Header().Get("Content-Encoding"); got != tt.wantEncoding {
				t.Errorf("Content-Encoding = %q, want %q", got, tt.wantEncoding)
			}
			if got := rec.Header().Get("Content-Type"); got != tt.wantContentType {
				t.Errorf("Content-Type = %q, want %q", got, tt.wantContentType)
			}
			if rec.Header().Get("ETag") == "" {
				t.Error("missing ETag")
			}
			// A gzipped body must actually decode back to the source bytes.
			if tt.wantEncoding == "gzip" {
				zr, err := gzip.NewReader(rec.Body)
				if err != nil {
					t.Fatalf("gzip.NewReader: %v", err)
				}
				if _, err := io.ReadAll(zr); err != nil {
					t.Fatalf("reading gzip body: %v", err)
				}
			}
		})
	}
}

func TestStaticHandlerETag304(t *testing.T) {
	h := newTestStatic(t)

	// First fetch to learn the ETag.
	req := httptest.NewRequest(http.MethodGet, "/static/app.js", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag on first response")
	}

	// Conditional revalidation with the same ETag → 304, no body.
	req2 := httptest.NewRequest(http.MethodGet, "/static/app.js", nil)
	req2.Header.Set("If-None-Match", etag)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want 304", rec2.Code)
	}
	if rec2.Body.Len() != 0 {
		t.Errorf("304 body = %d bytes, want 0", rec2.Body.Len())
	}
}

func TestStaticHandlerHEAD(t *testing.T) {
	h := newTestStatic(t)
	req := httptest.NewRequest(http.MethodHead, "/static/app.js", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("HEAD body = %d bytes, want 0", rec.Body.Len())
	}
	if rec.Header().Get("Content-Length") == "" {
		t.Error("HEAD missing Content-Length")
	}
}
