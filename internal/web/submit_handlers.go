package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/michael-duren/rubber-duck/internal/domain"
	"github.com/michael-duren/rubber-duck/internal/grader"
	"github.com/michael-duren/rubber-duck/internal/web/views"
)

// SubmissionStore is the store slice for the submission flow.
type SubmissionStore interface {
	CreateSubmission(ctx context.Context, userID, challengeID int64, code string) (int64, error)
	CreateClaimedSubmission(ctx context.Context, userID, challengeID int64, code, status, output string, score int, testsPassed, testsTotal *int) (int64, error)
	SubmissionForUser(ctx context.Context, id, userID int64) (domain.Submission, error)
	UserCourseScores(ctx context.Context, userID int64) ([]domain.CourseScore, error)
	UserStats(ctx context.Context, userID int64) (domain.UserStats, error)
	UserVariantProgress(ctx context.Context, userID int64) ([]domain.VariantProgress, error)
	SubmissionRateLimited(ctx context.Context, userID, challengeID int64) (bool, error)
	CompletedChallenges(ctx context.Context, userID, variantID int64) (map[int64]bool, error)
	LatestSubmissionCodesByVariant(ctx context.Context, userID, variantID int64) (map[int64]string, error)
	SubmissionsForChallenge(ctx context.Context, userID, challengeID int64) ([]domain.Submission, error)
}

const rateLimitMessage = "Too many submissions — you can submit each challenge up to 10 times per day, or let your in-flight submissions finish (max 10 at a time)."

// Enqueuer hands a stored submission to the grading pool.
type Enqueuer interface {
	Enqueue(id int64)
}

const maxSubmissionBytes = 128 << 10

func (h *handlers) submit(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	challengeID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	code := r.FormValue("code")
	if strings.TrimSpace(code) == "" || len(code) > maxSubmissionBytes {
		http.Error(w, "solution must be non-empty and under 128 KiB", http.StatusBadRequest)
		return
	}
	limited, err := h.submissions.SubmissionRateLimited(r.Context(), user.ID, challengeID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if limited {
		http.Error(w, rateLimitMessage, http.StatusTooManyRequests)
		return
	}

	id, err := h.submissions.CreateSubmission(r.Context(), user.ID, challengeID, code)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.enqueuer.Enqueue(id)
	http.Redirect(w, r, "/submissions/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (h *handlers) submissionPage(w http.ResponseWriter, r *http.Request) {
	sub, ok := h.loadSubmission(w, r)
	if !ok {
		return
	}
	back := r.Referer()
	h.render(w, r, views.SubmissionPage(currentUser(r), sub, back))
}

// submissionFragment serves the polled status partial.
func (h *handlers) submissionFragment(w http.ResponseWriter, r *http.Request) {
	sub, ok := h.loadSubmission(w, r)
	if !ok {
		return
	}
	h.render(w, r, views.SubmissionResult(sub))
}

func (h *handlers) loadSubmission(w http.ResponseWriter, r *http.Request) (domain.Submission, bool) {
	user := currentUser(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return domain.Submission{}, false
	}
	sub, err := h.submissions.SubmissionForUser(r.Context(), id, user.ID)
	if errors.Is(err, domain.ErrNotFound) {
		http.NotFound(w, r)
		return domain.Submission{}, false
	}
	if err != nil {
		h.serverError(w, r, err)
		return domain.Submission{}, false
	}
	return sub, true
}

func (h *handlers) profile(w http.ResponseWriter, r *http.Request) {
	h.renderProfile(w, r, "")
}

// submitBySlug is the CLI-facing counterpart to submit: challenges are
// addressed by slug (stable across re-publishes) rather than the internal
// numeric ID a browser form embeds, so `duck submit` never needs to know it.
//
// When the CLI ran the tests locally it sends claimed_status/claimed_output
// alongside the code: the claim becomes the verdict immediately (scored from
// the claimed output) and the enqueued grading run becomes a background
// audit instead of the thing the user waits on.
func (h *handlers) submitBySlug(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	_, variant, err := h.courses.VariantDetail(r.Context(), r.PathValue("slug"), r.PathValue("lang"))
	if errors.Is(err, domain.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	challenge, ok := findChallenge(variant, r.PathValue("challenge"))
	if !ok {
		http.NotFound(w, r)
		return
	}

	code := r.FormValue("code")
	if strings.TrimSpace(code) == "" || len(code) > maxSubmissionBytes {
		http.Error(w, "solution must be non-empty and under 128 KiB", http.StatusBadRequest)
		return
	}
	claimedStatus := r.FormValue("claimed_status")
	if claimedStatus != "" && claimedStatus != grader.StatusPassed && claimedStatus != grader.StatusFailed {
		http.Error(w, "claimed_status must be \"passed\" or \"failed\"", http.StatusBadRequest)
		return
	}

	limited, err := h.submissions.SubmissionRateLimited(r.Context(), user.ID, challenge.ID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if limited {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": rateLimitMessage})
		return
	}

	if claimedStatus == "" {
		id, err := h.submissions.CreateSubmission(r.Context(), user.ID, challenge.ID, code)
		if err != nil {
			h.serverError(w, r, err)
			return
		}
		h.enqueuer.Enqueue(id)
		writeJSON(w, http.StatusCreated, map[string]any{
			"id":  id,
			"url": "/submissions/" + strconv.FormatInt(id, 10),
		})
		return
	}

	output := grader.TruncateOutput(r.FormValue("claimed_output"))
	passed, total := grader.ParseTestCounts(variant.Language, output)
	score := grader.Score(challenge.Points, grader.Result{
		Status: claimedStatus, TestsPassed: passed, TestsTotal: total,
	})
	id, err := h.submissions.CreateClaimedSubmission(r.Context(), user.ID, challenge.ID, code, claimedStatus, output, score, passed, total)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.enqueuer.Enqueue(id) // background audit, nobody waits on it
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":           id,
		"url":          "/submissions/" + strconv.FormatInt(id, 10),
		"status":       claimedStatus,
		"score":        score,
		"tests_passed": passed,
		"tests_total":  total,
	})
}

func findChallenge(v domain.Variant, slug string) (domain.Challenge, bool) {
	for _, l := range v.Lessons {
		for _, c := range l.Challenges {
			if c.Slug == slug {
				return c, true
			}
		}
	}
	if v.Final.Slug == slug {
		return v.Final, true
	}
	return domain.Challenge{}, false
}

// submissionStatus is the polled JSON counterpart to submissionFragment,
// for `duck submit` to poll without parsing HTML.
func (h *handlers) submissionStatus(w http.ResponseWriter, r *http.Request) {
	sub, ok := h.loadSubmission(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":       sub.Status,
		"score":        sub.Score,
		"output":       sub.Output,
		"tests_passed": sub.TestsPassed,
		"tests_total":  sub.TestsTotal,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
