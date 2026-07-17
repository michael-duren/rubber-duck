package web

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/michael-duren/rubber-duck/internal/domain"
)

// fakeStore is an in-memory AuthStore.
type fakeStore struct {
	users          map[string]fakeUser // by username
	sessions       map[string]int64    // session token hash -> user id
	tokens         map[int64]fakeToken // CLI token id -> token
	nextTok        int64
	variant        *domain.Variant // set by tests that need VariantDetail to resolve
	variantSlug    string
	variantSource  string // raw markdown returned by VariantSource
	variantVersion int    // version returned by VariantSource / captured as proposal base
	submissions    map[int64]domain.Submission
	nextSub        int64
	rateLimit      func(userID, challengeID int64) bool // nil = never limited
	courses        []domain.CourseSummary               // returned by ListCourses (catalog tests)

	proposals map[int64]domain.Proposal
	reviews   map[int64]map[int64]domain.ProposalReview // proposal id -> reviewer id -> review
	nextProp  int64
	published []int64 // proposal IDs PublishProposal applied, in order
}

type fakeUser struct {
	id   int64
	hash string
	role string
}

type fakeToken struct {
	userID  int64
	name    string
	hash    string
	revoked bool
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		users: map[string]fakeUser{}, sessions: map[string]int64{},
		tokens: map[int64]fakeToken{}, submissions: map[int64]domain.Submission{},
		proposals: map[int64]domain.Proposal{}, reviews: map[int64]map[int64]domain.ProposalReview{},
	}
}

func (f *fakeStore) CreateUser(_ context.Context, username, passwordHash string) (domain.User, error) {
	if _, ok := f.users[username]; ok {
		return domain.User{}, domain.ErrUsernameTaken
	}
	id := int64(len(f.users) + 1)
	f.users[username] = fakeUser{id: id, hash: passwordHash, role: domain.RoleUser}
	return domain.User{ID: id, Username: username, Role: domain.RoleUser}, nil
}

// promote flips a user's role, mirroring `duckserver user promote`.
func (f *fakeStore) promote(username, role string) {
	u := f.users[username]
	u.role = role
	f.users[username] = u
}

func (f *fakeStore) userByID(id int64) (domain.User, bool) {
	for name, u := range f.users {
		if u.id == id {
			return domain.User{ID: id, Username: name, Role: u.role}, true
		}
	}
	return domain.User{}, false
}

func (f *fakeStore) UserByUsername(_ context.Context, username string) (domain.User, string, error) {
	u, ok := f.users[username]
	if !ok {
		return domain.User{}, "", domain.ErrNotFound
	}
	return domain.User{ID: u.id, Username: username, Role: u.role}, u.hash, nil
}

func (f *fakeStore) CreateSession(_ context.Context, tokenHash []byte, userID int64, _ time.Time) error {
	f.sessions[string(tokenHash)] = userID
	return nil
}

func (f *fakeStore) UserBySession(_ context.Context, tokenHash []byte) (domain.User, error) {
	id, ok := f.sessions[string(tokenHash)]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	if u, ok := f.userByID(id); ok {
		return u, nil
	}
	return domain.User{}, domain.ErrNotFound
}

func (f *fakeStore) DeleteSession(_ context.Context, tokenHash []byte) error {
	delete(f.sessions, string(tokenHash))
	return nil
}

func (f *fakeStore) CreateUserToken(_ context.Context, userID int64, name string, tokenHash []byte) (int64, error) {
	f.nextTok++
	f.tokens[f.nextTok] = fakeToken{userID: userID, name: name, hash: string(tokenHash)}
	return f.nextTok, nil
}

func (f *fakeStore) UserByToken(_ context.Context, tokenHash []byte) (domain.User, error) {
	for _, t := range f.tokens {
		if t.hash == string(tokenHash) && !t.revoked {
			if u, ok := f.userByID(t.userID); ok {
				return u, nil
			}
			return domain.User{ID: t.userID}, nil
		}
	}
	return domain.User{}, domain.ErrNotFound
}

func (f *fakeStore) ListUserTokens(_ context.Context, userID int64) ([]domain.UserToken, error) {
	var out []domain.UserToken
	for id, t := range f.tokens {
		if t.userID == userID {
			tok := domain.UserToken{ID: id, Name: t.name}
			if t.revoked {
				now := time.Now()
				tok.RevokedAt = &now
			}
			out = append(out, tok)
		}
	}
	return out, nil
}

func (f *fakeStore) RevokeUserToken(_ context.Context, userID, tokenID int64) error {
	t, ok := f.tokens[tokenID]
	if !ok || t.userID != userID || t.revoked {
		return domain.ErrNotFound
	}
	t.revoked = true
	f.tokens[tokenID] = t
	return nil
}

func (f *fakeStore) PasswordHash(_ context.Context, userID int64) (string, error) {
	for _, u := range f.users {
		if u.id == userID {
			return u.hash, nil
		}
	}
	return "", domain.ErrNotFound
}

func (f *fakeStore) UpdatePassword(_ context.Context, userID int64, passwordHash string) error {
	for name, u := range f.users {
		if u.id == userID {
			u.hash = passwordHash
			f.users[name] = u
			return nil
		}
	}
	return domain.ErrNotFound
}

func (f *fakeStore) DeleteOtherSessions(_ context.Context, userID int64, keepTokenHash []byte) error {
	for hash, uid := range f.sessions {
		if uid == userID && hash != string(keepTokenHash) {
			delete(f.sessions, hash)
		}
	}
	return nil
}

// CourseReader stubs: auth tests don't exercise course pages beyond the
// home page rendered at "/" (empty featured strip); catalog and home tests
// seed f.courses.
func (f *fakeStore) ListCourses(context.Context) ([]domain.CourseSummary, error) {
	return f.courses, nil
}

func (f *fakeStore) CourseBySlug(context.Context, string) (domain.Course, []domain.VariantSummary, error) {
	return domain.Course{}, nil, domain.ErrNotFound
}

func (f *fakeStore) VariantDetail(_ context.Context, slug, lang string) (domain.Course, domain.Variant, error) {
	if f.variant == nil || slug != f.variantSlug || lang != f.variant.Language {
		return domain.Course{}, domain.Variant{}, domain.ErrNotFound
	}
	return domain.Course{Slug: slug}, *f.variant, nil
}

// VariantSource backs the edit page's pre-filled textarea, and the version
// hidden field the form round-trips.
func (f *fakeStore) VariantSource(_ context.Context, slug, lang string) (string, int, error) {
	if f.variant == nil || slug != f.variantSlug || lang != f.variant.Language {
		return "", 0, domain.ErrNotFound
	}
	return f.variantSource, f.variantVersion, nil
}

// --- ProposalStore fake ---
// Mirrors the real store's semantics closely enough for handler tests:
// one open proposal per user per variant, revision bumps invalidating
// approvals, base-version staleness, the admin self-approve carve-out,
// and publish-through-the-variant with read-your-writes visibility.

// liveVersion is the fake's single variant's version as proposals see it:
// 0 when no variant is loaded (a new-course proposal), variantVersion
// otherwise.
func (f *fakeStore) liveVersion(slug, lang string) int {
	if f.variant == nil || slug != f.variantSlug || lang != f.variant.Language {
		return 0
	}
	return f.variantVersion
}

// countApprovals recomputes current-revision non-proposer approvals, the
// same aggregate the real store's proposal queries compute.
func (f *fakeStore) countApprovals(p domain.Proposal) int {
	n := 0
	for _, rv := range f.reviews[p.ID] {
		if rv.Verdict == domain.VerdictApprove && rv.Revision == p.Revision && rv.ReviewerID != p.ProposerID {
			n++
		}
	}
	return n
}

func (f *fakeStore) refreshProposal(p domain.Proposal) domain.Proposal {
	p.Approvals = f.countApprovals(p)
	p.LiveVersion = f.liveVersion(p.CourseSlug, p.Language)
	return p
}

func (f *fakeStore) CreateProposal(_ context.Context, proposerID int64, courseSlug, language, title, summary, markdown string) (domain.Proposal, error) {
	for _, p := range f.proposals {
		if p.ProposerID == proposerID && p.CourseSlug == courseSlug && p.Language == language && p.Status == domain.ProposalOpen {
			return domain.Proposal{}, domain.ErrDuplicateProposal
		}
	}
	f.nextProp++
	proposer, _ := f.userByID(proposerID)
	p := domain.Proposal{
		ID: f.nextProp, ProposerID: proposerID, ProposerUsername: proposer.Username,
		CourseSlug: courseSlug, Language: language, Title: title, SummaryMD: summary,
		ProposedMD: markdown, BaseVersion: f.liveVersion(courseSlug, language),
		Revision: 1, Status: domain.ProposalOpen,
	}
	f.proposals[p.ID] = p
	return f.refreshProposal(p), nil
}

func (f *fakeStore) UpdateProposalMarkdown(_ context.Context, proposalID, proposerID int64, title, summary, markdown string) (domain.Proposal, error) {
	p, ok := f.proposals[proposalID]
	if !ok || p.ProposerID != proposerID {
		return domain.Proposal{}, domain.ErrNotFound
	}
	if p.Status != domain.ProposalOpen {
		return domain.Proposal{}, domain.ErrProposalClosed
	}
	p.Title, p.SummaryMD, p.ProposedMD = title, summary, markdown
	p.Revision++
	p.BaseVersion = f.liveVersion(p.CourseSlug, p.Language)
	f.proposals[proposalID] = p
	return f.refreshProposal(p), nil
}

func (f *fakeStore) ProposalByID(_ context.Context, id int64) (domain.Proposal, error) {
	p, ok := f.proposals[id]
	if !ok {
		return domain.Proposal{}, domain.ErrNotFound
	}
	return f.refreshProposal(p), nil
}

func (f *fakeStore) ListProposals(_ context.Context, status string) ([]domain.Proposal, error) {
	var out []domain.Proposal
	for _, p := range f.proposals {
		if status == "" || p.Status == status {
			out = append(out, f.refreshProposal(p))
		}
	}
	return out, nil
}

func (f *fakeStore) ListProposalsByUser(_ context.Context, userID int64) ([]domain.Proposal, error) {
	var out []domain.Proposal
	for _, p := range f.proposals {
		if p.ProposerID == userID {
			out = append(out, f.refreshProposal(p))
		}
	}
	return out, nil
}

func (f *fakeStore) ListProposalReviews(_ context.Context, proposalID int64) ([]domain.ProposalReview, error) {
	var out []domain.ProposalReview
	for _, rv := range f.reviews[proposalID] {
		out = append(out, rv)
	}
	return out, nil
}

func (f *fakeStore) AddReview(_ context.Context, proposalID, reviewerID int64, verdict, comment string) (domain.ReviewOutcome, error) {
	p, ok := f.proposals[proposalID]
	if !ok {
		return domain.ReviewOutcome{}, domain.ErrNotFound
	}
	if p.Status != domain.ProposalOpen {
		return domain.ReviewOutcome{}, domain.ErrProposalClosed
	}
	reviewer, _ := f.userByID(reviewerID)
	isAdmin := reviewer.IsAdmin()
	if reviewerID == p.ProposerID && (!isAdmin || verdict != domain.VerdictApprove) {
		return domain.ReviewOutcome{}, domain.ErrSelfReview
	}
	if f.reviews[proposalID] == nil {
		f.reviews[proposalID] = map[int64]domain.ProposalReview{}
	}
	f.reviews[proposalID][reviewerID] = domain.ProposalReview{
		ProposalID: proposalID, ReviewerID: reviewerID, ReviewerUsername: reviewer.Username,
		ReviewerIsAdmin: isAdmin, Verdict: verdict, CommentMD: comment, Revision: p.Revision,
	}
	closed := false
	if isAdmin && verdict == domain.VerdictReject {
		p.Status = domain.ProposalRejected
		f.proposals[proposalID] = p
		closed = true
	}
	return domain.ReviewOutcome{Proposal: f.refreshProposal(p), ReviewerIsAdmin: isAdmin, Closed: closed}, nil
}

func (f *fakeStore) PublishProposal(_ context.Context, proposalID int64, course domain.Course, variant domain.Variant) (int, error) {
	p, ok := f.proposals[proposalID]
	if !ok {
		return 0, domain.ErrNotFound
	}
	if p.Status != domain.ProposalOpen {
		return 0, domain.ErrProposalClosed
	}
	if p.BaseVersion != f.liveVersion(p.CourseSlug, p.Language) {
		return 0, domain.ErrVersionConflict
	}
	f.variantSlug = course.Slug
	f.variant = &variant
	f.variantSource = variant.SourceMD
	f.variantVersion++
	version := f.variantVersion
	p.Status = domain.ProposalPublished
	p.PublishedVersion = &version
	f.proposals[proposalID] = p
	f.published = append(f.published, proposalID)
	return version, nil
}

func (f *fakeStore) WithdrawProposal(_ context.Context, proposalID, proposerID int64) error {
	p, ok := f.proposals[proposalID]
	if !ok || p.ProposerID != proposerID {
		return domain.ErrNotFound
	}
	if p.Status != domain.ProposalOpen {
		return domain.ErrProposalClosed
	}
	p.Status = domain.ProposalWithdrawn
	f.proposals[proposalID] = p
	return nil
}

// SubmissionStore stubs.
func (f *fakeStore) CreateSubmission(_ context.Context, userID, challengeID int64, code string) (int64, error) {
	f.nextSub++
	f.submissions[f.nextSub] = domain.Submission{ID: f.nextSub, UserID: userID, ChallengeID: challengeID, Code: code, Status: "passed", Score: 10}
	return f.nextSub, nil
}

func (f *fakeStore) CreateClaimedSubmission(_ context.Context, userID, challengeID int64, code, status, output string, score int, testsPassed, testsTotal *int) (int64, error) {
	f.nextSub++
	f.submissions[f.nextSub] = domain.Submission{
		ID: f.nextSub, UserID: userID, ChallengeID: challengeID, Code: code,
		Status: status, Output: output, Score: score,
		TestsPassed: testsPassed, TestsTotal: testsTotal, Claimed: true,
	}
	return f.nextSub, nil
}

func (f *fakeStore) SubmissionForUser(_ context.Context, id, userID int64) (domain.Submission, error) {
	sub, ok := f.submissions[id]
	if !ok || sub.UserID != userID {
		return domain.Submission{}, domain.ErrNotFound
	}
	return sub, nil
}

func (f *fakeStore) UserCourseScores(context.Context, int64) ([]domain.CourseScore, error) {
	return nil, nil
}

func (f *fakeStore) UserVariantProgress(context.Context, int64) ([]domain.VariantProgress, error) {
	return nil, nil
}

func (f *fakeStore) UserStats(_ context.Context, userID int64) (domain.UserStats, error) {
	var st domain.UserStats
	solved := map[int64]bool{}
	for _, sub := range f.submissions {
		if sub.UserID != userID {
			continue
		}
		st.TotalSubmissions++
		if sub.Status == "passed" {
			solved[sub.ChallengeID] = true
		}
	}
	st.ChallengesSolved = len(solved)
	return st, nil
}

func (f *fakeStore) SubmissionRateLimited(_ context.Context, userID, challengeID int64) (bool, error) {
	if f.rateLimit == nil {
		return false, nil
	}
	return f.rateLimit(userID, challengeID), nil
}

func (f *fakeStore) CompletedChallenges(_ context.Context, userID, variantID int64) (map[int64]bool, error) {
	completed := map[int64]bool{}
	for _, sub := range f.submissions {
		if sub.UserID == userID && sub.Status == "passed" {
			completed[sub.ChallengeID] = true
		}
	}
	return completed, nil
}

func (f *fakeStore) LatestSubmissionCodesByVariant(_ context.Context, userID, variantID int64) (map[int64]string, error) {
	result := make(map[int64]string)
	for _, sub := range f.submissions {
		if sub.UserID == userID && sub.Code != "" {
			if _, exists := result[sub.ChallengeID]; !exists {
				result[sub.ChallengeID] = sub.Code
			}
		}
	}
	return result, nil
}

func (f *fakeStore) SubmissionsForChallenge(_ context.Context, userID, challengeID int64) ([]domain.Submission, error) {
	var result []domain.Submission
	for _, sub := range f.submissions {
		if sub.UserID == userID && sub.ChallengeID == challengeID {
			result = append(result, sub)
		}
	}
	return result, nil
}

type noopEnqueuer struct{}

func (noopEnqueuer) Enqueue(int64) {}

// testThreshold is the approval threshold every web test runs with: small
// enough that a test can reach it with two reviewers.
const testThreshold = 2

func testMux(t *testing.T) (*http.ServeMux, *fakeStore) {
	t.Helper()
	mux := http.NewServeMux()
	fs := newFakeStore()
	Register(mux, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), fs, fs, fs, fs, noopEnqueuer{}, testThreshold)
	return mux, fs
}

// fetchCSRFCookie makes a throwaway GET to obtain a fresh double-submit
// CSRF cookie; its value is valid for any single subsequent request that
// presents both the cookie and a matching csrf_token form field.
func fetchCSRFCookie(mux *http.ServeMux) *http.Cookie {
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	for _, c := range rec.Result().Cookies() {
		if c.Name == csrfCookie {
			return c
		}
	}
	return nil
}

func postForm(mux *http.ServeMux, path string, form url.Values, cookie *http.Cookie) *httptest.ResponseRecorder {
	csrf := fetchCSRFCookie(mux)
	if csrf != nil {
		form = cloneValues(form)
		form.Set("csrf_token", csrf.Value)
	}
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if csrf != nil {
		req.AddCookie(csrf)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func cloneValues(v url.Values) url.Values {
	out := make(url.Values, len(v)+1)
	for k, vs := range v {
		out[k] = append([]string(nil), vs...)
	}
	return out
}

func TestSignup(t *testing.T) {
	cases := []struct {
		name       string
		username   string
		password   string
		preexists  bool
		wantStatus int
		wantBody   string
	}{
		{"ok", "alice", "supersecret", false, http.StatusSeeOther, ""},
		{"short username", "ab", "supersecret", false, http.StatusOK, "3-32 characters"},
		{"short password", "alice", "short", false, http.StatusOK, "at least 8"},
		// bcrypt rejects >72-byte inputs; the form must catch that first.
		{"long password", "alice", strings.Repeat("p", 73), false, http.StatusOK, "at most 72"},
		{"taken", "alice", "supersecret", true, http.StatusOK, "taken"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mux, fs := testMux(t)
			if c.preexists {
				fs.users[c.username] = fakeUser{id: 99, hash: "x"}
			}
			rec := postForm(mux, "/signup", url.Values{"username": {c.username}, "password": {c.password}}, nil)
			if rec.Code != c.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, c.wantStatus)
			}
			if c.wantBody != "" && !strings.Contains(rec.Body.String(), c.wantBody) {
				t.Errorf("body missing %q", c.wantBody)
			}
			if c.wantStatus == http.StatusSeeOther && len(fs.sessions) != 1 {
				t.Errorf("sessions = %d, want 1", len(fs.sessions))
			}
		})
	}
}

func TestLoginLogoutRoundTrip(t *testing.T) {
	mux, fs := testMux(t)

	// Sign up (also logs in) and grab the session cookie.
	rec := postForm(mux, "/signup", url.Values{"username": {"alice"}, "password": {"supersecret"}}, nil)
	res := rec.Result()
	var session *http.Cookie
	for _, c := range res.Cookies() {
		if c.Name == sessionCookie {
			session = c
		}
	}
	if session == nil {
		t.Fatal("no session cookie set on signup")
	}

	// Home page shows the username when logged in.
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(session)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "alice") {
		t.Error("home page does not show logged-in user")
	}

	// Wrong password fails.
	rec = postForm(mux, "/login", url.Values{"username": {"alice"}, "password": {"wrongwrong"}}, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Wrong username or password") {
		t.Errorf("bad login: status %d", rec.Code)
	}

	// Logout clears the session server-side.
	postForm(mux, "/logout", url.Values{}, session)
	if len(fs.sessions) != 0 {
		t.Errorf("sessions after logout = %d, want 0", len(fs.sessions))
	}
}
