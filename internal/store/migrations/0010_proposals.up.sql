-- Course changes are proposed, reviewed, and only then published. A proposal
-- carries a full course markdown document (same contract as the old agent
-- PUT), the live variant version it was authored against (base_version; 0
-- means the variant doesn't exist yet — a new course/variant proposal), and
-- a revision counter that bumps whenever the proposer updates the content so
-- reviews of older content stop counting toward the approval threshold.
--
-- Status is a one-way door out of 'open': published (approved and live),
-- rejected (an admin said no), or withdrawn (the proposer gave up). Closed
-- proposals keep their reviews as history.
CREATE TABLE proposals (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    proposer_id       bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    course_slug       text NOT NULL,
    language          text NOT NULL,
    title             text NOT NULL,
    summary_md        text NOT NULL DEFAULT '',
    proposed_md       text NOT NULL,
    base_version      int NOT NULL,
    revision          int NOT NULL DEFAULT 1,
    status            text NOT NULL DEFAULT 'open'
                      CHECK (status IN ('open', 'published', 'rejected', 'withdrawn')),
    published_version int,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    closed_at         timestamptz
);

CREATE INDEX proposals_status ON proposals (status, created_at DESC);
CREATE INDEX proposals_proposer ON proposals (proposer_id, created_at DESC);

-- One open proposal per user per variant: a second "propose" is an update
-- to the existing one, not a fork of it.
CREATE UNIQUE INDEX one_open_proposal_per_user_variant
    ON proposals (proposer_id, course_slug, language) WHERE status = 'open';

-- One review row per reviewer per proposal; re-reviewing (e.g. after the
-- proposer revises) upserts the row. revision records which proposal
-- revision the reviewer saw: only current-revision approvals count toward
-- the publish threshold, older ones display as stale.
CREATE TABLE proposal_reviews (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    proposal_id bigint NOT NULL REFERENCES proposals (id) ON DELETE CASCADE,
    reviewer_id bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    verdict     text NOT NULL CHECK (verdict IN ('approve', 'reject')),
    comment_md  text NOT NULL DEFAULT '',
    revision    int NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (proposal_id, reviewer_id)
);
