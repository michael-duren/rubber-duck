package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/michael-duren/rubber-duck/internal/domain"
)

// proposalCols is the shared SELECT for loading proposals with their two
// computed fields: Approvals (current-revision approve verdicts from users
// other than the proposer — what the publish threshold compares against) and
// LiveVersion (the variant's current version, 0 if it doesn't exist, so
// callers can spot a stale proposal without a second query).
const proposalCols = `
	SELECT p.id, p.proposer_id, u.username, p.course_slug, p.language,
	       p.title, p.summary_md, p.proposed_md, p.base_version, p.revision,
	       p.status, p.published_version, p.created_at, p.updated_at, p.closed_at,
	       (SELECT count(*) FROM proposal_reviews r
	        WHERE r.proposal_id = p.id AND r.verdict = 'approve'
	          AND r.revision = p.revision AND r.reviewer_id != p.proposer_id) AS approvals,
	       COALESCE((SELECT cv.version FROM course_variants cv
	                 JOIN courses c ON c.id = cv.course_id
	                 WHERE c.slug = p.course_slug AND cv.language = p.language), 0) AS live_version
	FROM proposals p
	JOIN users u ON u.id = p.proposer_id`

// querier lets the proposal loaders run on the pool or inside a transaction.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func scanProposal(row pgx.Row) (domain.Proposal, error) {
	var p domain.Proposal
	err := row.Scan(&p.ID, &p.ProposerID, &p.ProposerUsername, &p.CourseSlug, &p.Language,
		&p.Title, &p.SummaryMD, &p.ProposedMD, &p.BaseVersion, &p.Revision,
		&p.Status, &p.PublishedVersion, &p.CreatedAt, &p.UpdatedAt, &p.ClosedAt,
		&p.Approvals, &p.LiveVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Proposal{}, domain.ErrNotFound
	}
	return p, err
}

func proposalByID(ctx context.Context, q querier, id int64) (domain.Proposal, error) {
	return scanProposal(q.QueryRow(ctx, proposalCols+` WHERE p.id = $1`, id))
}

// CreateProposal opens a proposal for one course variant. base_version is
// captured from the live variant in the same statement as the insert (0 when
// the variant doesn't exist — a new-course proposal), so it can't race a
// concurrent publish. A user gets one open proposal per variant; a second
// create returns ErrDuplicateProposal and the caller should route the user
// to updating the existing one.
func (s *Store) CreateProposal(ctx context.Context, proposerID int64, courseSlug, language, title, summary, markdown string) (domain.Proposal, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO proposals (proposer_id, course_slug, language, title, summary_md, proposed_md, base_version)
		VALUES ($1, $2, $3, $4, $5, $6,
			COALESCE((SELECT cv.version FROM course_variants cv
			          JOIN courses c ON c.id = cv.course_id
			          WHERE c.slug = $2 AND cv.language = $3), 0))
		RETURNING id`,
		proposerID, courseSlug, language, title, summary, markdown,
	).Scan(&id)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" &&
		pgErr.ConstraintName == "one_open_proposal_per_user_variant" {
		return domain.Proposal{}, domain.ErrDuplicateProposal
	}
	if err != nil {
		return domain.Proposal{}, err
	}
	return proposalByID(ctx, s.pool, id)
}

// UpdateProposalMarkdown replaces an open proposal's content. Scoped to the
// proposer in the WHERE clause, so "not yours" and "doesn't exist" are the
// same ErrNotFound. The revision bump is what invalidates standing
// approvals (they recorded the old revision), and base_version re-captures
// the live variant version, so updating is also how a proposer rebases
// after the live course moved past their base.
func (s *Store) UpdateProposalMarkdown(ctx context.Context, proposalID, proposerID int64, title, summary, markdown string) (domain.Proposal, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE proposals SET
			title = $3, summary_md = $4, proposed_md = $5,
			revision = revision + 1,
			base_version = COALESCE((SELECT cv.version FROM course_variants cv
			                         JOIN courses c ON c.id = cv.course_id
			                         WHERE c.slug = proposals.course_slug
			                           AND cv.language = proposals.language), 0),
			updated_at = now()
		WHERE id = $1 AND proposer_id = $2 AND status = 'open'`,
		proposalID, proposerID, title, summary, markdown)
	if err != nil {
		return domain.Proposal{}, err
	}
	if tag.RowsAffected() == 0 {
		// Distinguish a closed proposal (a real state the UI explains) from
		// one that isn't the caller's to touch (or doesn't exist) — both
		// matched zero rows above.
		p, err := proposalByID(ctx, s.pool, proposalID)
		if err == nil && p.ProposerID == proposerID && p.Status != domain.ProposalOpen {
			return domain.Proposal{}, domain.ErrProposalClosed
		}
		return domain.Proposal{}, domain.ErrNotFound
	}
	return proposalByID(ctx, s.pool, proposalID)
}

func (s *Store) ProposalByID(ctx context.Context, id int64) (domain.Proposal, error) {
	return proposalByID(ctx, s.pool, id)
}

// ListProposals returns proposals newest-first, filtered to one status when
// status is non-empty ("" lists all). Two query shapes rather than one
// status-or-empty OR predicate so the filtered case can use the
// proposals_status index.
func (s *Store) ListProposals(ctx context.Context, status string) ([]domain.Proposal, error) {
	if status == "" {
		return s.listProposals(ctx, proposalCols+` ORDER BY p.created_at DESC`)
	}
	return s.listProposals(ctx,
		proposalCols+` WHERE p.status = $1 ORDER BY p.created_at DESC`, status)
}

// ListProposalsByUser returns one user's proposals newest-first, every
// status — the "my pending and merged changes" view.
func (s *Store) ListProposalsByUser(ctx context.Context, userID int64) ([]domain.Proposal, error) {
	return s.listProposals(ctx,
		proposalCols+` WHERE p.proposer_id = $1 ORDER BY p.created_at DESC`, userID)
}

func (s *Store) listProposals(ctx context.Context, query string, args ...any) ([]domain.Proposal, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var proposals []domain.Proposal
	for rows.Next() {
		p, err := scanProposal(rows)
		if err != nil {
			return nil, err
		}
		proposals = append(proposals, p)
	}
	return proposals, rows.Err()
}

// ListProposalReviews returns a proposal's reviews latest-activity-first,
// with the reviewer's username and role joined in (the UI badges admin
// verdicts and stale-revision reviews). created_at is the first review's
// time; updated_at moves when the reviewer re-reviews.
func (s *Store) ListProposalReviews(ctx context.Context, proposalID int64) ([]domain.ProposalReview, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.proposal_id, r.reviewer_id, u.username, u.role = 'admin',
		       r.verdict, r.comment_md, r.revision, r.created_at, r.updated_at
		FROM proposal_reviews r
		JOIN users u ON u.id = r.reviewer_id
		WHERE r.proposal_id = $1
		ORDER BY r.updated_at DESC`, proposalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reviews []domain.ProposalReview
	for rows.Next() {
		var rv domain.ProposalReview
		if err := rows.Scan(&rv.ID, &rv.ProposalID, &rv.ReviewerID, &rv.ReviewerUsername,
			&rv.ReviewerIsAdmin, &rv.Verdict, &rv.CommentMD, &rv.Revision, &rv.CreatedAt, &rv.UpdatedAt); err != nil {
			return nil, err
		}
		reviews = append(reviews, rv)
	}
	return reviews, rows.Err()
}

// AddReview records (or replaces) one reviewer's verdict on an open
// proposal. The reviewer's role is read inside the transaction rather than
// trusted from the caller. seenRevision is the proposal revision the
// reviewer's verdict was formed against (the detail page embeds it in the
// form); if the proposer updated the proposal in the meantime the verdict
// is refused with ErrStaleRevision rather than silently counted toward
// content the reviewer never read. Self-review returns ErrSelfReview with
// one carve-out: an admin approving their own proposal (the bootstrap case
// for a site with one admin and no quorum). An admin rejection closes the
// proposal here; publishing on approval is deliberately NOT done here —
// the approval threshold is the web layer's config, so the caller compares
// the returned outcome against it and calls PublishProposal.
func (s *Store) AddReview(ctx context.Context, proposalID, reviewerID int64, verdict, comment string, seenRevision int) (domain.ReviewOutcome, error) {
	if verdict != domain.VerdictApprove && verdict != domain.VerdictReject {
		// The web handler validates too; this guard keeps a future caller's
		// bad verdict from surfacing as a CHECK-constraint 500.
		return domain.ReviewOutcome{}, fmt.Errorf("invalid verdict %q", verdict)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.ReviewOutcome{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var proposerID int64
	var status string
	var revision int
	err = tx.QueryRow(ctx,
		`SELECT proposer_id, status, revision FROM proposals WHERE id = $1 FOR UPDATE`,
		proposalID).Scan(&proposerID, &status, &revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ReviewOutcome{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.ReviewOutcome{}, err
	}
	if status != domain.ProposalOpen {
		return domain.ReviewOutcome{}, domain.ErrProposalClosed
	}
	if seenRevision != revision {
		return domain.ReviewOutcome{}, domain.ErrStaleRevision
	}

	var role string
	if err := tx.QueryRow(ctx, `SELECT role FROM users WHERE id = $1`, reviewerID).Scan(&role); err != nil {
		return domain.ReviewOutcome{}, fmt.Errorf("reviewer role: %w", err)
	}
	isAdmin := role == domain.RoleAdmin

	if reviewerID == proposerID && (!isAdmin || verdict != domain.VerdictApprove) {
		return domain.ReviewOutcome{}, domain.ErrSelfReview
	}

	// created_at survives the upsert (it's the first review's timestamp);
	// updated_at is what re-reviewing moves.
	if _, err := tx.Exec(ctx, `
		INSERT INTO proposal_reviews (proposal_id, reviewer_id, verdict, comment_md, revision)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (proposal_id, reviewer_id) DO UPDATE SET
			verdict = EXCLUDED.verdict,
			comment_md = EXCLUDED.comment_md,
			revision = EXCLUDED.revision,
			updated_at = now()`,
		proposalID, reviewerID, verdict, comment, revision); err != nil {
		return domain.ReviewOutcome{}, fmt.Errorf("upsert review: %w", err)
	}

	closed := false
	if isAdmin && verdict == domain.VerdictReject {
		if _, err := tx.Exec(ctx, `
			UPDATE proposals SET status = 'rejected', closed_at = now(), updated_at = now()
			WHERE id = $1`, proposalID); err != nil {
			return domain.ReviewOutcome{}, err
		}
		closed = true
	}

	p, err := proposalByID(ctx, tx, proposalID)
	if err != nil {
		return domain.ReviewOutcome{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.ReviewOutcome{}, err
	}
	return domain.ReviewOutcome{Proposal: p, ReviewerIsAdmin: isAdmin, Closed: closed}, nil
}

// PublishProposal makes an open proposal's content live and closes it, all
// in one transaction. The caller has already parsed the proposal's markdown
// into the course/variant pair (the store doesn't import ingest), and
// passes the revision that content came from: if the proposer revised the
// proposal after the caller read it, the parsed content no longer matches
// the stored document and publishing fails with ErrStaleRevision instead of
// shipping the older revision and marking the newer one published. The
// write goes through the same upsert path as every publish, attributed to
// the proposer, with the proposal's base_version as expectedVersion — so a
// proposal whose base the live variant has moved past fails with
// ErrVersionConflict ("needs rebase": the proposer updates, which
// re-captures base_version) rather than overwriting newer content. Losing a
// double-publish race surfaces as ErrProposalClosed, which callers can
// treat as already-done.
func (s *Store) PublishProposal(ctx context.Context, proposalID int64, expectedRevision int, course domain.Course, variant domain.Variant) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var proposerID int64
	var baseVersion int
	var revision int
	var status string
	err = tx.QueryRow(ctx,
		`SELECT proposer_id, base_version, revision, status FROM proposals WHERE id = $1 FOR UPDATE`,
		proposalID).Scan(&proposerID, &baseVersion, &revision, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, domain.ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if status != domain.ProposalOpen {
		return 0, domain.ErrProposalClosed
	}
	if revision != expectedRevision {
		return 0, domain.ErrStaleRevision
	}

	version, err := upsertVariantTx(ctx, tx, course, variant, &proposerID, &baseVersion)
	if err != nil {
		return 0, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE proposals SET status = 'published', published_version = $2, closed_at = now(), updated_at = now()
		WHERE id = $1`, proposalID, version); err != nil {
		return 0, err
	}
	return version, tx.Commit(ctx)
}

// WithdrawProposal closes an open proposal at its proposer's request.
// Proposer-scoped like UpdateProposalMarkdown: someone else's proposal is
// ErrNotFound, an already-closed one is ErrProposalClosed.
func (s *Store) WithdrawProposal(ctx context.Context, proposalID, proposerID int64) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE proposals SET status = 'withdrawn', closed_at = now(), updated_at = now()
		WHERE id = $1 AND proposer_id = $2 AND status = 'open'`,
		proposalID, proposerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		p, err := proposalByID(ctx, s.pool, proposalID)
		if err == nil && p.ProposerID == proposerID && p.Status != domain.ProposalOpen {
			return domain.ErrProposalClosed
		}
		return domain.ErrNotFound
	}
	return nil
}
