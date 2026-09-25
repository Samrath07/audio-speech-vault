CREATE TABLE project_participant_roles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    code TEXT NOT NULL CHECK (code ~ '^[a-z][a-z0-9_-]{1,31}$'),
    label TEXT NOT NULL CHECK (char_length(label) BETWEEN 1 AND 80),
    display_order INTEGER NOT NULL DEFAULT 0 CHECK (display_order >= 0),
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_by UUID NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, code),
    UNIQUE (project_id, label)
);

CREATE TABLE speaker_mappings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    analysis_id UUID NOT NULL REFERENCES recording_analyses(id) ON DELETE CASCADE,
    recording_id UUID NOT NULL REFERENCES recordings(id) ON DELETE CASCADE,
    speaker_cluster TEXT NOT NULL CHECK (speaker_cluster IN ('speaker_a','speaker_b')),
    participant_role_id UUID NOT NULL REFERENCES project_participant_roles(id),
    mapped_by UUID NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (analysis_id, speaker_cluster)
);

CREATE INDEX speaker_mappings_recording_idx ON speaker_mappings(recording_id, analysis_id);

CREATE TABLE analysis_profiles (
    project_id UUID PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    silence_dbfs DOUBLE PRECISION NOT NULL DEFAULT -35 CHECK (silence_dbfs BETWEEN -80 AND -10),
    minimum_pause_ms INTEGER NOT NULL DEFAULT 300 CHECK (minimum_pause_ms BETWEEN 100 AND 10000),
    long_pause_ms INTEGER NOT NULL DEFAULT 2000 CHECK (long_pause_ms BETWEEN 500 AND 60000),
    repetition_similarity DOUBLE PRECISION NOT NULL DEFAULT 0.72 CHECK (repetition_similarity BETWEEN 0.5 AND 0.99),
    speaker_separation DOUBLE PRECISION NOT NULL DEFAULT 0.5 CHECK (speaker_separation BETWEEN 0.1 AND 5),
    updated_by UUID NOT NULL REFERENCES users(id),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (long_pause_ms >= minimum_pause_ms)
);

CREATE TABLE export_audits (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    requested_by UUID NOT NULL REFERENCES users(id),
    export_type TEXT NOT NULL CHECK (export_type IN ('summary_csv','event_csv','gold_csv','batch_zip')),
    algorithm_version TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('started','completed','failed')),
    output_sha256 TEXT CHECK (output_sha256 IS NULL OR output_sha256 ~ '^[a-f0-9]{64}$'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ
);

CREATE INDEX export_audits_project_created_idx ON export_audits(project_id, created_at DESC);
