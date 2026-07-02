package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/mduren/getcracked/internal/domain"
)

type fakeStore struct {
	versions map[string]int // "slug/lang" -> version
	courses  map[string]domain.Course
	sources  map[string]string
}

func newFakeStore() *fakeStore {
	return &fakeStore{versions: map[string]int{}, courses: map[string]domain.Course{}, sources: map[string]string{}}
}

func (f *fakeStore) key(slug, lang string) string { return slug + "/" + lang }

func (f *fakeStore) UpsertVariant(_ context.Context, c domain.Course, v domain.Variant) (int, error) {
	k := f.key(c.Slug, v.Language)
	f.versions[k]++
	f.courses[c.Slug] = c
	f.sources[k] = v.SourceMD
	return f.versions[k], nil
}

func (f *fakeStore) ListCourses(context.Context) ([]domain.CourseSummary, error) {
	var out []domain.CourseSummary
	for slug, c := range f.courses {
		out = append(out, domain.CourseSummary{Slug: slug, Title: c.Title, Tags: c.Tags})
	}
	return out, nil
}

func (f *fakeStore) CourseBySlug(_ context.Context, slug string) (domain.Course, []domain.VariantSummary, error) {
	c, ok := f.courses[slug]
	if !ok {
		return domain.Course{}, nil, domain.ErrNotFound
	}
	return c, nil, nil
}

func (f *fakeStore) VariantSource(_ context.Context, slug, lang string) (string, error) {
	src, ok := f.sources[f.key(slug, lang)]
	if !ok {
		return "", domain.ErrNotFound
	}
	return src, nil
}

func (f *fakeStore) DeleteCourse(_ context.Context, slug string) error {
	if _, ok := f.courses[slug]; !ok {
		return domain.ErrNotFound
	}
	delete(f.courses, slug)
	return nil
}

func (f *fakeStore) DeleteVariant(_ context.Context, slug, lang string) error {
	if _, ok := f.sources[f.key(slug, lang)]; !ok {
		return domain.ErrNotFound
	}
	delete(f.sources, f.key(slug, lang))
	return nil
}

func (f *fakeStore) ListTags(context.Context) ([]string, error) { return []string{"backend"}, nil }

// allowAllKeys accepts any bearer token.
type allowAllKeys struct{}

func (allowAllKeys) APIKeyValid(context.Context, []byte) (bool, error) { return true, nil }

func testAPI(t *testing.T) (*http.ServeMux, *fakeStore) {
	t.Helper()
	mux := http.NewServeMux()
	fs := newFakeStore()
	Register(mux, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), allowAllKeys{}, fs)
	return mux, fs
}

func doJSON(mux *http.ServeMux, method, path string, body any) *httptest.ResponseRecorder {
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("Authorization", "Bearer gc_test")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func seedDoc(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile("../../seed/intro-to-go.md")
	if err != nil {
		t.Fatal(err)
	}
	return string(src)
}

func TestPutVariant(t *testing.T) {
	doc := seedDoc(t)
	put := func(mux *http.ServeMux, path, markdown string) *httptest.ResponseRecorder {
		return doJSON(mux, "PUT", path, map[string]string{"markdown": markdown})
	}

	t.Run("create then update bumps version", func(t *testing.T) {
		mux, _ := testAPI(t)
		rec := put(mux, "/api/v1/courses/intro-to-concurrency/variants/go", doc)
		if rec.Code != http.StatusCreated {
			t.Fatalf("first put = %d body %s", rec.Code, rec.Body)
		}
		var resp map[string]any
		json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp["version"].(float64) != 1 || resp["lessons"].(float64) != 2 ||
			resp["challenges"].(float64) != 3 || resp["total_points"].(float64) != 75 {
			t.Errorf("summary = %v", resp)
		}

		rec = put(mux, "/api/v1/courses/intro-to-concurrency/variants/go", doc)
		if rec.Code != http.StatusOK {
			t.Fatalf("second put = %d", rec.Code)
		}
		json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp["version"].(float64) != 2 {
			t.Errorf("version = %v, want 2", resp["version"])
		}
	})

	t.Run("slug mismatch is 409", func(t *testing.T) {
		mux, _ := testAPI(t)
		rec := put(mux, "/api/v1/courses/other-course/variants/go", doc)
		if rec.Code != http.StatusConflict {
			t.Errorf("status = %d, want 409", rec.Code)
		}
	})

	t.Run("language mismatch is 409", func(t *testing.T) {
		mux, _ := testAPI(t)
		rec := put(mux, "/api/v1/courses/intro-to-concurrency/variants/python", doc)
		if rec.Code != http.StatusConflict {
			t.Errorf("status = %d, want 409", rec.Code)
		}
	})

	t.Run("invalid markdown is 422 with line details", func(t *testing.T) {
		mux, _ := testAPI(t)
		bad := strings.Replace(doc, "### Tests", "### Test", 1) // break one challenge
		rec := put(mux, "/api/v1/courses/intro-to-concurrency/variants/go", bad)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d body %s", rec.Code, rec.Body)
		}
		var resp struct {
			Error struct {
				Code    string
				Details []struct {
					Line    int
					Message string
				}
			}
		}
		json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Error.Code != "invalid_course_markdown" || len(resp.Error.Details) == 0 {
			t.Fatalf("error = %+v", resp.Error)
		}
		if resp.Error.Details[0].Line == 0 {
			t.Errorf("detail missing line: %+v", resp.Error.Details[0])
		}
	})

	t.Run("non-json body is 400", func(t *testing.T) {
		mux, _ := testAPI(t)
		req := httptest.NewRequest("PUT", "/api/v1/courses/x/variants/go", strings.NewReader("plain text"))
		req.Header.Set("Authorization", "Bearer gc_test")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})
}

func TestRoundTripAndDelete(t *testing.T) {
	mux, _ := testAPI(t)
	doc := seedDoc(t)
	doJSON(mux, "PUT", "/api/v1/courses/intro-to-concurrency/variants/go", map[string]string{"markdown": doc})

	rec := doJSON(mux, "GET", "/api/v1/courses/intro-to-concurrency/variants/go", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get source = %d", rec.Code)
	}
	var got map[string]string
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got["markdown"] != doc {
		t.Error("round-tripped markdown differs from submitted document")
	}

	if rec := doJSON(mux, "DELETE", "/api/v1/courses/intro-to-concurrency/variants/go", nil); rec.Code != http.StatusNoContent {
		t.Errorf("delete variant = %d", rec.Code)
	}
	if rec := doJSON(mux, "DELETE", "/api/v1/courses/intro-to-concurrency/variants/go", nil); rec.Code != http.StatusNotFound {
		t.Errorf("second delete = %d, want 404", rec.Code)
	}
	if rec := doJSON(mux, "DELETE", "/api/v1/courses/intro-to-concurrency", nil); rec.Code != http.StatusNoContent {
		t.Errorf("delete course = %d", rec.Code)
	}
}
