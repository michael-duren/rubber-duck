package web

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/mduren/getcracked/internal/domain"
	"github.com/mduren/getcracked/internal/web/views"
)

// SubmissionStore is the store slice for the submission flow.
type SubmissionStore interface {
	CreateSubmission(ctx context.Context, userID, challengeID int64, code string) (int64, error)
	SubmissionForUser(ctx context.Context, id, userID int64) (domain.Submission, error)
	UserCourseScores(ctx context.Context, userID int64) ([]domain.CourseScore, error)
}

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
