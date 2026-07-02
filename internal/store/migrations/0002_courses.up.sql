CREATE TABLE courses (
    id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    slug             text UNIQUE NOT NULL,
    title            text NOT NULL,
    description_md   text NOT NULL,
    description_html text NOT NULL,
    duration_hours   numeric,
    extended_reading jsonb NOT NULL DEFAULT '[]',
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE tags (
    id   bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name text UNIQUE NOT NULL
);

CREATE TABLE course_tags (
    course_id bigint REFERENCES courses ON DELETE CASCADE,
    tag_id    bigint REFERENCES tags ON DELETE CASCADE,
    PRIMARY KEY (course_id, tag_id)
);

CREATE TABLE course_variants (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    course_id  bigint NOT NULL REFERENCES courses ON DELETE CASCADE,
    language   text NOT NULL,
    source_md  text NOT NULL,
    version    int NOT NULL DEFAULT 1,
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (course_id, language)
);

CREATE TABLE lessons (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    variant_id   bigint NOT NULL REFERENCES course_variants ON DELETE CASCADE,
    slug         text NOT NULL,
    title        text NOT NULL,
    position     int NOT NULL,
    content_md   text NOT NULL,
    content_html text NOT NULL,
    UNIQUE (variant_id, slug)
);

CREATE TABLE challenges (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    variant_id   bigint NOT NULL REFERENCES course_variants ON DELETE CASCADE,
    lesson_id    bigint REFERENCES lessons ON DELETE CASCADE,
    slug         text NOT NULL,
    title        text NOT NULL,
    position     int NOT NULL,
    prompt_md    text NOT NULL,
    prompt_html  text NOT NULL,
    starter_code text NOT NULL,
    test_code    text NOT NULL,
    points       int NOT NULL CHECK (points > 0),
    UNIQUE (variant_id, slug)
);

-- lesson_id IS NULL marks the course final challenge; allow only one per variant.
CREATE UNIQUE INDEX one_final_per_variant
    ON challenges (variant_id) WHERE lesson_id IS NULL;
