ALTER TABLE projects
    ADD COLUMN institution_id UUID REFERENCES institutions(id),
    ADD COLUMN description TEXT NOT NULL DEFAULT '',
    ADD COLUMN status TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'archived'));

CREATE INDEX projects_institution_idx ON projects (institution_id, created_at DESC);

CREATE TABLE recordings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    original_filename TEXT NOT NULL,
    storage_key TEXT NOT NULL UNIQUE,
    media_type TEXT NOT NULL CHECK (media_type IN ('audio/mpeg', 'audio/wav', 'audio/x-wav', 'audio/flac', 'audio/ogg', 'audio/webm')),
    size_bytes BIGINT NOT NULL CHECK (size_bytes > 0 AND size_bytes <= 524288000),
    duration_ms BIGINT NOT NULL CHECK (duration_ms > 0 AND duration_ms <= 86400000),
    status TEXT NOT NULL DEFAULT 'ready' CHECK (status IN ('processing', 'ready', 'failed', 'archived')),
    uploaded_by UUID NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX recordings_project_created_idx ON recordings (project_id, created_at DESC);

CREATE TABLE tier_definitions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    tier_type TEXT NOT NULL CHECK (tier_type IN ('text', 'controlled_vocabulary', 'tag', 'comment')),
    parent_tier_id UUID REFERENCES tier_definitions(id),
    display_order INTEGER NOT NULL DEFAULT 0 CHECK (display_order >= 0),
    color TEXT NOT NULL DEFAULT '#39745a' CHECK (color ~ '^#[0-9A-Fa-f]{6}$'),
    is_required BOOLEAN NOT NULL DEFAULT FALSE,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, name)
);

CREATE INDEX tier_definitions_project_order_idx ON tier_definitions (project_id, display_order, id);

CREATE TABLE tier_values (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tier_id UUID NOT NULL REFERENCES tier_definitions(id) ON DELETE CASCADE,
    value TEXT NOT NULL,
    label TEXT NOT NULL,
    display_order INTEGER NOT NULL DEFAULT 0 CHECK (display_order >= 0),
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    UNIQUE (tier_id, value)
);

CREATE TABLE annotation_tasks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    recording_id UUID NOT NULL UNIQUE REFERENCES recordings(id) ON DELETE CASCADE,
    assigned_researcher_id UUID REFERENCES users(id),
    assigned_reviewer_id UUID REFERENCES users(id),
    status TEXT NOT NULL DEFAULT 'unassigned'
        CHECK (status IN ('unassigned', 'in_progress', 'submitted', 'changes_requested', 'approved')),
    submitted_at TIMESTAMPTZ,
    reviewed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (assigned_researcher_id IS NULL OR assigned_researcher_id <> assigned_reviewer_id)
);

CREATE INDEX annotation_tasks_researcher_idx ON annotation_tasks (assigned_researcher_id, status);
CREATE INDEX annotation_tasks_reviewer_idx ON annotation_tasks (assigned_reviewer_id, status);

CREATE TABLE annotations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    recording_id UUID NOT NULL REFERENCES recordings(id) ON DELETE CASCADE,
    tier_id UUID NOT NULL REFERENCES tier_definitions(id),
    start_ms BIGINT NOT NULL CHECK (start_ms >= 0),
    end_ms BIGINT NOT NULL CHECK (end_ms > start_ms),
    value TEXT NOT NULL CHECK (char_length(value) BETWEEN 1 AND 4000),
    created_by UUID NOT NULL REFERENCES users(id),
    updated_by UUID NOT NULL REFERENCES users(id),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);

CREATE INDEX annotations_recording_tier_time_idx
    ON annotations (recording_id, tier_id, start_ms, end_ms) WHERE deleted_at IS NULL;

CREATE TABLE annotation_revisions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    annotation_id UUID NOT NULL REFERENCES annotations(id) ON DELETE CASCADE,
    revision_number INTEGER NOT NULL CHECK (revision_number > 0),
    start_ms BIGINT NOT NULL,
    end_ms BIGINT NOT NULL,
    value TEXT NOT NULL,
    changed_by UUID NOT NULL REFERENCES users(id),
    change_type TEXT NOT NULL CHECK (change_type IN ('created', 'updated', 'deleted')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (annotation_id, revision_number)
);

CREATE TABLE review_comments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id UUID NOT NULL REFERENCES annotation_tasks(id) ON DELETE CASCADE,
    annotation_id UUID REFERENCES annotations(id) ON DELETE SET NULL,
    author_id UUID NOT NULL REFERENCES users(id),
    body TEXT NOT NULL CHECK (char_length(body) BETWEEN 1 AND 4000),
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'resolved')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at TIMESTAMPTZ,
    resolved_by UUID REFERENCES users(id)
);

CREATE INDEX review_comments_task_status_idx ON review_comments (task_id, status, created_at);
