-- Learning paths: curated, ordered tracks of existing courses ("start here,
-- then this"), published as markdown through the agent API like courses.
-- A path carries no learner data of its own — progress is derived from the
-- member courses' submissions — so membership can be replaced wholesale on
-- every re-publish instead of the archive-and-revive dance course content
-- needs (see 0007).
CREATE TABLE learning_paths (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    slug text UNIQUE NOT NULL,
    title text NOT NULL,
    description_md text NOT NULL,
    description_html text NOT NULL,
    -- The document's markdown body: the long-form overview shown on the
    -- path page. Optional, hence the default.
    overview_md text NOT NULL DEFAULT '',
    overview_html text NOT NULL DEFAULT '',
    -- The original document, verbatim, for agent-API round-tripping
    -- (same role as course_variants.source_md).
    source_md text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Membership references courses by slug — the durable identity path
-- documents are authored against. ON UPDATE CASCADE keeps membership
-- attached through a (rare, deliberate) course-slug rename done in SQL;
-- ON DELETE CASCADE means deleting a course simply drops it from every
-- path, leaving the rest of the track intact.
CREATE TABLE learning_path_courses (
    path_id bigint NOT NULL REFERENCES learning_paths (id) ON DELETE CASCADE,
    course_slug text NOT NULL REFERENCES courses (slug) ON DELETE CASCADE ON UPDATE CASCADE,
    position int NOT NULL,
    PRIMARY KEY (path_id, course_slug)
);
