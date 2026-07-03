package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestChangePasswordLogsOutOtherSessions(t *testing.T) {
	mux, fs := testMux(t)

	// Sign up, then log in a second time to get a second session.
	rec := postForm(mux, "/signup", url.Values{"username": {"alice"}, "password": {"originalpw"}}, nil)
	session1 := sessionFrom(rec)
	rec = postForm(mux, "/login", url.Values{"username": {"alice"}, "password": {"originalpw"}}, nil)
	session2 := sessionFrom(rec)
	if len(fs.sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(fs.sessions))
	}

	// Wrong current password is rejected and doesn't touch anything.
	rec = postForm(mux, "/settings", url.Values{"current_password": {"wrongwrong"}, "new_password": {"newpassword1"}}, session1)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Current password is wrong") {
		t.Fatalf("wrong current password: status %d body %s", rec.Code, rec.Body)
	}

	// Change the password over session1.
	rec = postForm(mux, "/settings", url.Values{"current_password": {"originalpw"}, "new_password": {"newpassword1"}}, session1)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Password changed") {
		t.Fatalf("change password: status %d body %s", rec.Code, rec.Body)
	}

	// session1 (the one that made the change) still works.
	req := httptest.NewRequest("GET", "/settings", nil)
	req.AddCookie(session1)
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusOK {
		t.Errorf("session1 after change = %d, want 200 (still logged in)", rec2.Code)
	}

	// session2 was logged out by the change.
	req = httptest.NewRequest("GET", "/settings", nil)
	req.AddCookie(session2)
	rec2 = httptest.NewRecorder()
	mux.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusSeeOther {
		t.Errorf("session2 after change = %d, want 303 redirect to /login", rec2.Code)
	}

	// The old password no longer works.
	rec = postForm(mux, "/login", url.Values{"username": {"alice"}, "password": {"originalpw"}}, nil)
	if !strings.Contains(rec.Body.String(), "Wrong username or password") {
		t.Error("old password still works after change")
	}

	// The new password does.
	rec = postForm(mux, "/login", url.Values{"username": {"alice"}, "password": {"newpassword1"}}, nil)
	if rec.Code != http.StatusSeeOther {
		t.Errorf("login with new password = %d, want 303", rec.Code)
	}
}

func sessionFrom(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			return c
		}
	}
	return nil
}
