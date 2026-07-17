package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/michael-duren/rubber-duck/internal/domain"
)

// loginAs signs up a fresh user and returns their session cookie.
func loginAs(t *testing.T, mux *http.ServeMux, username string) *http.Cookie {
	t.Helper()
	rec := postForm(mux, "/signup", url.Values{"username": {username}, "password": {"supersecret"}}, nil)
	session := sessionFrom(rec)
	if session == nil {
		t.Fatalf("signup %s did not set a session cookie", username)
	}
	return session
}

func get(mux *http.ServeMux, path string, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// seedProposal opens a proposal by alice through the real propose flow and
// returns her session.
func seedProposal(t *testing.T, mux *http.ServeMux, fs *fakeStore) *http.Cookie {
	t.Helper()
	alice := loginAs(t, mux, "alice")
	rec := postForm(mux, "/courses/intro-to-concurrency/go/edit",
		url.Values{"markdown": {seedMarkdown(t)}, "title": {"Great change"}, "summary": {"trust me"}}, alice)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("seed propose = %d: %s", rec.Code, rec.Body)
	}
	if len(fs.proposals) != 1 {
		t.Fatalf("seed proposals = %d, want 1", len(fs.proposals))
	}
	return alice
}

func TestProposalsPageAnonymous(t *testing.T) {
	mux, fs := testMux(t)
	seedProposal(t, mux, fs)

	rec := get(mux, "/proposals", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /proposals = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Great change") {
		t.Error("queue should list the open proposal")
	}

	rec = get(mux, "/proposals/1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /proposals/1 = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Great change") || !strings.Contains(body, "trust me") {
		t.Error("detail should show title and summary")
	}
	if !strings.Contains(body, "Log in") {
		t.Error("anonymous detail should invite login instead of showing the review form")
	}

	// ?mine=1 needs an account.
	rec = get(mux, "/proposals?mine=1", nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Errorf("anonymous ?mine=1 = %d -> %q, want 303 /login", rec.Code, rec.Header().Get("Location"))
	}

	if rec := get(mux, "/proposals/99", nil); rec.Code != http.StatusNotFound {
		t.Errorf("GET missing proposal = %d, want 404", rec.Code)
	}
}

func TestReviewRequiresLogin(t *testing.T) {
	mux, fs := testMux(t)
	seedProposal(t, mux, fs)

	rec := postForm(mux, "/proposals/1/reviews", url.Values{"verdict": {"approve"}, "revision": {"1"}}, nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Errorf("anonymous review = %d -> %q, want 303 /login", rec.Code, rec.Header().Get("Location"))
	}
	if len(fs.reviews[1]) != 0 {
		t.Error("anonymous review must not be recorded")
	}
}

func TestSelfReviewBlocked(t *testing.T) {
	mux, fs := testMux(t)
	alice := seedProposal(t, mux, fs)

	rec := postForm(mux, "/proposals/1/reviews", url.Values{"verdict": {"approve"}, "revision": {"1"}}, alice)
	if rec.Code != http.StatusOK {
		t.Fatalf("self review = %d, want 200 (re-render with error)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "review your own proposal") {
		t.Errorf("expected self-review message, got: %s", rec.Body.String())
	}
	if len(fs.reviews[1]) != 0 {
		t.Error("self review must not be recorded")
	}
}

// TestThresholdPublishes: approvals from two distinct regular users (the
// test threshold) publish the proposal through the real parse+publish path.
func TestThresholdPublishes(t *testing.T) {
	mux, fs := testMux(t)
	seedProposal(t, mux, fs)
	bob := loginAs(t, mux, "bob")
	carol := loginAs(t, mux, "carol")

	rec := postForm(mux, "/proposals/1/reviews", url.Values{"verdict": {"approve"}, "comment": {"lgtm"}, "revision": {"1"}}, bob)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("bob approve = %d: %s", rec.Code, rec.Body)
	}
	if fs.proposals[1].Status != domain.ProposalOpen {
		t.Fatalf("after 1/2 approvals status = %q, want open", fs.proposals[1].Status)
	}
	if len(fs.published) != 0 {
		t.Fatal("must not publish below the threshold")
	}

	rec = postForm(mux, "/proposals/1/reviews", url.Values{"verdict": {"approve"}, "revision": {"1"}}, carol)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("carol approve = %d: %s", rec.Code, rec.Body)
	}
	p := fs.proposals[1]
	if p.Status != domain.ProposalPublished {
		t.Fatalf("after 2/2 approvals status = %q, want published", p.Status)
	}
	if len(fs.published) != 1 || fs.variantSource != seedMarkdown(t) {
		t.Error("publish should write the proposed document to the variant")
	}
}

func TestAdminApprovePublishesInstantly(t *testing.T) {
	mux, fs := testMux(t)
	seedProposal(t, mux, fs)
	admin := loginAs(t, mux, "root")
	fs.promote("root", domain.RoleAdmin)

	rec := postForm(mux, "/proposals/1/reviews", url.Values{"verdict": {"approve"}, "revision": {"1"}}, admin)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("admin approve = %d: %s", rec.Code, rec.Body)
	}
	if fs.proposals[1].Status != domain.ProposalPublished {
		t.Errorf("status = %q, want published after one admin approval", fs.proposals[1].Status)
	}
}

func TestAdminRejectCloses(t *testing.T) {
	mux, fs := testMux(t)
	seedProposal(t, mux, fs)
	admin := loginAs(t, mux, "root")
	fs.promote("root", domain.RoleAdmin)

	rec := postForm(mux, "/proposals/1/reviews", url.Values{"verdict": {"reject"}, "comment": {"nope"}, "revision": {"1"}}, admin)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("admin reject = %d: %s", rec.Code, rec.Body)
	}
	if fs.proposals[1].Status != domain.ProposalRejected {
		t.Errorf("status = %q, want rejected", fs.proposals[1].Status)
	}
	if len(fs.published) != 0 {
		t.Error("a rejection must not publish")
	}
}

// TestAdminSelfApprovePublishes covers the bootstrap carve-out: an admin's
// own proposal publishes on their own approval.
func TestAdminSelfApprovePublishes(t *testing.T) {
	mux, fs := testMux(t)
	admin := loginAs(t, mux, "root")
	fs.promote("root", domain.RoleAdmin)

	if rec := postForm(mux, "/courses/intro-to-concurrency/go/edit",
		url.Values{"markdown": {seedMarkdown(t)}}, admin); rec.Code != http.StatusSeeOther {
		t.Fatalf("admin propose = %d", rec.Code)
	}
	rec := postForm(mux, "/proposals/1/reviews", url.Values{"verdict": {"approve"}, "revision": {"1"}}, admin)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("admin self-approve = %d: %s", rec.Code, rec.Body)
	}
	if fs.proposals[1].Status != domain.ProposalPublished {
		t.Errorf("status = %q, want published", fs.proposals[1].Status)
	}
}

// TestStalePublishDeferred: when the live variant moved past the proposal's
// base, a threshold-reaching approval records the review but defers the
// publish — the proposal stays open and the detail page shows the rebase
// banner.
func TestStalePublishDeferred(t *testing.T) {
	mux, fs := testMux(t)
	seedProposal(t, mux, fs)
	// The live variant moves after the proposal captured base 0: someone
	// published something else there.
	fs.variantSlug = "intro-to-concurrency"
	fs.variant = &fakeVariantGo
	fs.variantSource = "someone else's newer content"
	fs.variantVersion = 7

	admin := loginAs(t, mux, "root")
	fs.promote("root", domain.RoleAdmin)
	rec := postForm(mux, "/proposals/1/reviews", url.Values{"verdict": {"approve"}, "revision": {"1"}}, admin)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("approve on stale = %d: %s", rec.Code, rec.Body)
	}
	p := fs.proposals[1]
	if p.Status != domain.ProposalOpen {
		t.Fatalf("stale proposal status = %q, want still open", p.Status)
	}
	if fs.variantSource != "someone else's newer content" {
		t.Error("stale publish must not overwrite the newer live content")
	}
	if len(fs.reviews[1]) != 1 {
		t.Error("the review itself must be recorded even when publish is deferred")
	}
	// Detail page shows the rebase banner.
	body := get(mux, "/proposals/1", nil).Body.String()
	if !strings.Contains(body, "publish until the proposer updates it") {
		t.Errorf("expected rebase banner, got: %s", body)
	}
}

// TestProposalEditResetsApprovals: the proposer updating content bumps the
// revision, so prior approvals show as outdated and the count restarts.
func TestProposalEditResetsApprovals(t *testing.T) {
	mux, fs := testMux(t)
	alice := seedProposal(t, mux, fs)
	bob := loginAs(t, mux, "bob")

	if rec := postForm(mux, "/proposals/1/reviews", url.Values{"verdict": {"approve"}, "revision": {"1"}}, bob); rec.Code != http.StatusSeeOther {
		t.Fatalf("bob approve = %d", rec.Code)
	}
	edited := strings.Replace(seedMarkdown(t), "Introduction to Concurrency", "Introduction to Concurrency v2", 1)
	rec := postForm(mux, "/proposals/1/edit", url.Values{"markdown": {edited}, "title": {"Great change"}}, alice)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("proposer edit = %d: %s", rec.Code, rec.Body)
	}
	p := fs.proposals[1]
	if p.Revision != 2 {
		t.Errorf("revision = %d, want 2", p.Revision)
	}
	if fs.countApprovals(p) != 0 {
		t.Error("approvals must reset after a content update")
	}
	body := get(mux, "/proposals/1", nil).Body.String()
	if !strings.Contains(body, "outdated") {
		t.Error("prior review should display as outdated")
	}
}

// TestStaleRevisionReviewRefused: a verdict submitted against a revision
// the proposer has since replaced is refused (nothing recorded), so an
// approval can never count toward — or instantly publish — content the
// reviewer never saw. Re-reviewing the current revision works.
func TestStaleRevisionReviewRefused(t *testing.T) {
	mux, fs := testMux(t)
	alice := seedProposal(t, mux, fs)
	bob := loginAs(t, mux, "bob")

	edited := strings.Replace(seedMarkdown(t), "Introduction to Concurrency", "Introduction to Concurrency v2", 1)
	if rec := postForm(mux, "/proposals/1/edit", url.Values{"markdown": {edited}}, alice); rec.Code != http.StatusSeeOther {
		t.Fatalf("proposer edit = %d", rec.Code)
	}

	// Bob's page showed revision 1; the proposal is now revision 2.
	rec := postForm(mux, "/proposals/1/reviews", url.Values{"verdict": {"approve"}, "revision": {"1"}}, bob)
	if rec.Code != http.StatusOK {
		t.Fatalf("stale review = %d, want 200 (re-render with error)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "updated while you were reviewing") {
		t.Errorf("expected stale-revision message, got: %s", rec.Body.String())
	}
	if len(fs.reviews[1]) != 0 {
		t.Error("stale review must not be recorded")
	}

	rec = postForm(mux, "/proposals/1/reviews", url.Values{"verdict": {"approve"}, "revision": {"2"}}, bob)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("current-revision review = %d: %s", rec.Code, rec.Body)
	}
	if len(fs.reviews[1]) != 1 {
		t.Error("current-revision review should be recorded")
	}
}

func TestProposalEditProposerOnly(t *testing.T) {
	mux, fs := testMux(t)
	seedProposal(t, mux, fs)
	bob := loginAs(t, mux, "bob")

	// Bob can't open alice's proposal editor…
	rec := get(mux, "/proposals/1/edit", bob)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/proposals/1" {
		t.Errorf("bob GET edit = %d -> %q, want 303 to detail", rec.Code, rec.Header().Get("Location"))
	}
	// …and his POST is a 404 (the store scopes the update to the proposer).
	rec = postForm(mux, "/proposals/1/edit", url.Values{"markdown": {seedMarkdown(t)}}, bob)
	if rec.Code != http.StatusNotFound {
		t.Errorf("bob POST edit = %d, want 404", rec.Code)
	}
	if fs.proposals[1].Revision != 1 {
		t.Error("bob's edit must not be applied")
	}
}

// TestProposalEditCannotRetarget: a proposal's document must keep pointing
// at the same course variant — changing the frontmatter is rejected.
func TestProposalEditCannotRetarget(t *testing.T) {
	mux, fs := testMux(t)
	alice := seedProposal(t, mux, fs)

	retargeted := strings.Replace(seedMarkdown(t), "course: intro-to-concurrency", "course: other-course", 1)
	rec := postForm(mux, "/proposals/1/edit", url.Values{"markdown": {retargeted}}, alice)
	if rec.Code != http.StatusOK {
		t.Fatalf("retarget edit = %d, want 200 (re-render)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "change which course variant it targets") {
		t.Errorf("expected retarget error, got: %s", rec.Body.String())
	}
	if fs.proposals[1].Revision != 1 {
		t.Error("retargeting edit must not be applied")
	}
}

func TestWithdrawProposal(t *testing.T) {
	mux, fs := testMux(t)
	alice := seedProposal(t, mux, fs)
	bob := loginAs(t, mux, "bob")

	// Bob can't withdraw alice's proposal.
	if rec := postForm(mux, "/proposals/1/withdraw", url.Values{}, bob); rec.Code != http.StatusNotFound {
		t.Errorf("bob withdraw = %d, want 404", rec.Code)
	}

	rec := postForm(mux, "/proposals/1/withdraw", url.Values{}, alice)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/proposals?mine=1" {
		t.Errorf("withdraw = %d -> %q, want 303 to mine", rec.Code, rec.Header().Get("Location"))
	}
	if fs.proposals[1].Status != domain.ProposalWithdrawn {
		t.Errorf("status = %q, want withdrawn", fs.proposals[1].Status)
	}

	// Reviews on a closed proposal bounce back to the detail page.
	if rec := postForm(mux, "/proposals/1/reviews", url.Values{"verdict": {"approve"}, "revision": {"1"}}, bob); rec.Code != http.StatusSeeOther {
		t.Errorf("review after close = %d, want 303 redirect", rec.Code)
	}
}

// TestMyProposalsFilter: ?mine=1 lists only the caller's proposals.
func TestMyProposalsFilter(t *testing.T) {
	mux, fs := testMux(t)
	seedProposal(t, mux, fs)
	bob := loginAs(t, mux, "bob")

	body := get(mux, "/proposals?mine=1", bob).Body.String()
	if strings.Contains(body, "Great change") {
		t.Error("bob's ?mine=1 must not list alice's proposal")
	}
}
