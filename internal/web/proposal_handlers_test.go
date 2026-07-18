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
	// …and his POST bounces to the detail page the same way, before any
	// parse work (the store's proposer-scoped update is the backstop).
	rec = postForm(mux, "/proposals/1/edit", url.Values{"markdown": {seedMarkdown(t)}}, bob)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/proposals/1" {
		t.Errorf("bob POST edit = %d -> %q, want 303 to detail", rec.Code, rec.Header().Get("Location"))
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

// TestNonAdminRejectNeitherClosesNorPublishes: a regular user's reject is
// recorded and nothing else happens — the proposal stays open, unpublished.
func TestNonAdminRejectNeitherClosesNorPublishes(t *testing.T) {
	mux, fs := testMux(t)
	seedProposal(t, mux, fs)
	bob := loginAs(t, mux, "bob")

	rec := postForm(mux, "/proposals/1/reviews", url.Values{"verdict": {"reject"}, "comment": {"needs work"}, "revision": {"1"}}, bob)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("bob reject = %d: %s", rec.Code, rec.Body)
	}
	if fs.proposals[1].Status != domain.ProposalOpen {
		t.Errorf("status = %q, want still open", fs.proposals[1].Status)
	}
	if len(fs.published) != 0 {
		t.Error("a non-admin reject must not publish")
	}
	if len(fs.reviews[1]) != 1 {
		t.Error("the reject itself should be recorded")
	}
}

// TestRejectAtThresholdDoesNotPublish pins the verdict gate in the publish
// retry: a proposal already sitting at the approval threshold (a prior
// publish attempt failed transiently) must not be published by a REJECT
// arriving on top of the standing approvals.
func TestRejectAtThresholdDoesNotPublish(t *testing.T) {
	mux, fs := testMux(t)
	seedProposal(t, mux, fs)
	bob := loginAs(t, mux, "bob")
	carol := loginAs(t, mux, "carol")
	dave := loginAs(t, mux, "dave")
	_, _ = bob, carol

	// Standing approvals at the threshold, recorded directly — as if a
	// prior threshold-tipping review committed but its publish failed.
	fs.reviews[1] = map[int64]domain.ProposalReview{}
	for _, name := range []string{"bob", "carol"} {
		u := fs.users[name]
		fs.reviews[1][u.id] = domain.ProposalReview{
			ProposalID: 1, ReviewerID: u.id, ReviewerUsername: name,
			Verdict: domain.VerdictApprove, Revision: 1,
		}
	}

	rec := postForm(mux, "/proposals/1/reviews", url.Values{"verdict": {"reject"}, "revision": {"1"}}, dave)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("dave reject = %d: %s", rec.Code, rec.Body)
	}
	if len(fs.published) != 0 {
		t.Error("a reject must never trigger the publish retry")
	}
	if fs.proposals[1].Status != domain.ProposalOpen {
		t.Errorf("status = %q, want still open", fs.proposals[1].Status)
	}
}

// TestPublishInvalidDocumentBanner: a proposal that reached approval but
// whose document no longer parses (the ingest contract tightened since it
// was authored) renders the explanatory banner instead of publishing.
func TestPublishInvalidDocumentBanner(t *testing.T) {
	mux, fs := testMux(t)
	seedProposal(t, mux, fs)
	p := fs.proposals[1]
	p.ProposedMD = "not a course document"
	fs.proposals[1] = p

	admin := loginAs(t, mux, "root")
	fs.promote("root", domain.RoleAdmin)
	rec := postForm(mux, "/proposals/1/reviews", url.Values{"verdict": {"approve"}, "revision": {"1"}}, admin)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve on rotten doc = %d, want 200 (re-render)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "no longer validates") {
		t.Errorf("expected validation banner, got: %s", rec.Body.String())
	}
	if fs.proposals[1].Status != domain.ProposalOpen || len(fs.published) != 0 {
		t.Error("invalid document must not publish or close the proposal")
	}
	if len(fs.reviews[1]) != 1 {
		t.Error("the review itself should still be recorded")
	}
}

// TestPublishBrokenDiagramBanner: same degraded path for a document whose
// d2 diagram no longer compiles.
func TestPublishBrokenDiagramBanner(t *testing.T) {
	mux, fs := testMux(t)
	seedProposal(t, mux, fs)
	broken := strings.Replace(seedMarkdown(t),
		"# Lesson: Goroutines Basics {#goroutines-basics}\n",
		"# Lesson: Goroutines Basics {#goroutines-basics}\n\n```d2\na -> : {{{ not d2\n```\n", 1)
	p := fs.proposals[1]
	p.ProposedMD = broken
	fs.proposals[1] = p

	admin := loginAs(t, mux, "root")
	fs.promote("root", domain.RoleAdmin)
	rec := postForm(mux, "/proposals/1/reviews", url.Values{"verdict": {"approve"}, "revision": {"1"}}, admin)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve on broken diagram = %d, want 200 (re-render)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "no longer compiles") {
		t.Errorf("expected diagram banner, got: %s", rec.Body.String())
	}
	if fs.proposals[1].Status != domain.ProposalOpen || len(fs.published) != 0 {
		t.Error("broken diagram must not publish or close the proposal")
	}
}

// TestPublishRaceLoserRedirects: losing a double-publish race
// (ErrProposalClosed) or hitting a concurrent proposer revision
// (ErrStaleRevision) redirects to the detail page rather than 500ing.
func TestPublishRaceLoserRedirects(t *testing.T) {
	for _, raceErr := range []error{domain.ErrProposalClosed, domain.ErrStaleRevision} {
		mux, fs := testMux(t)
		seedProposal(t, mux, fs)
		fs.publishErr = raceErr

		admin := loginAs(t, mux, "root")
		fs.promote("root", domain.RoleAdmin)
		rec := postForm(mux, "/proposals/1/reviews", url.Values{"verdict": {"approve"}, "revision": {"1"}}, admin)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/proposals/1" {
			t.Errorf("%v: approve = %d -> %q, want 303 to detail", raceErr, rec.Code, rec.Header().Get("Location"))
		}
		if len(fs.published) != 0 {
			t.Errorf("%v: nothing should be published", raceErr)
		}
	}
}

// TestPreviewMarkdownStripsRawHTML pins that the live-preview endpoint
// never reflects raw HTML back: goldmark (without WithUnsafe) must strip
// script/event-handler content. If someone adds html.WithUnsafe to
// internal/markdown, this becomes a reflected-XSS endpoint and this test
// is the tripwire.
func TestPreviewMarkdownStripsRawHTML(t *testing.T) {
	mux, _ := testMux(t)
	alice := loginAs(t, mux, "alice")

	rec := postForm(mux, "/preview/markdown", url.Values{"markdown": {
		"hello <script>alert(1)</script>\n\n<img src=x onerror=alert(2)>\n\n[link](javascript:alert(3))",
	}}, alice)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, bad := range []string{"<script>", "onerror", "javascript:"} {
		if strings.Contains(body, bad) {
			t.Errorf("preview reflected %q: %s", bad, body)
		}
	}
}

// TestDuplicateProposalCollapsesIntoEditor: proposing a second time for the
// same variant redirects into the existing proposal's editor with the
// explanatory notice.
func TestDuplicateProposalCollapsesIntoEditor(t *testing.T) {
	mux, fs := testMux(t)
	alice := seedProposal(t, mux, fs)

	rec := postForm(mux, "/courses/intro-to-concurrency/go/edit",
		url.Values{"markdown": {seedMarkdown(t)}}, alice)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/proposals/1/edit?dup=1" {
		t.Fatalf("duplicate propose = %d -> %q, want 303 to editor with dup=1", rec.Code, rec.Header().Get("Location"))
	}
	body := get(mux, "/proposals/1/edit?dup=1", alice).Body.String()
	if !strings.Contains(body, "already had an open proposal") {
		t.Error("editor should explain the duplicate collapsed into this proposal")
	}
}

// TestProposalsStatusFilter: the queue defaults to open proposals and
// ?status= filters (or shows everything with all).
func TestProposalsStatusFilter(t *testing.T) {
	mux, fs := testMux(t)
	alice := seedProposal(t, mux, fs)
	if rec := postForm(mux, "/proposals/1/withdraw", url.Values{}, alice); rec.Code != http.StatusSeeOther {
		t.Fatalf("withdraw = %d", rec.Code)
	}

	if body := get(mux, "/proposals", nil).Body.String(); strings.Contains(body, "Great change") {
		t.Error("default (open) queue must not list the withdrawn proposal")
	}
	if body := get(mux, "/proposals?status=withdrawn", nil).Body.String(); !strings.Contains(body, "Great change") {
		t.Error("?status=withdrawn should list it")
	}
	if body := get(mux, "/proposals?status=all", nil).Body.String(); !strings.Contains(body, "Great change") {
		t.Error("?status=all should list it")
	}
}
