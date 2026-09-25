CREATE TABLE analysis_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    recording_id UUID NOT NULL REFERENCES recordings(id) ON DELETE CASCADE,
    algorithm_version TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','processing','completed','failed','cancelled')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    max_attempts INTEGER NOT NULL DEFAULT 3 CHECK (max_attempts BETWEEN 1 AND 10),
    available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    heartbeat_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    error_code TEXT,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (recording_id, algorithm_version)
);

CREATE INDEX analysis_jobs_claim_idx ON analysis_jobs(status, available_at, created_at);

CREATE TABLE recording_analyses (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    recording_id UUID NOT NULL REFERENCES recordings(id) ON DELETE CASCADE,
    job_id UUID NOT NULL REFERENCES analysis_jobs(id) ON DELETE CASCADE,
    algorithm_version TEXT NOT NULL,
    audio_sha256 TEXT NOT NULL CHECK (audio_sha256 ~ '^[a-f0-9]{64}$'),
    duration_ms BIGINT NOT NULL CHECK (duration_ms > 0),
    sample_rate INTEGER NOT NULL CHECK (sample_rate > 0),
    channels INTEGER NOT NULL CHECK (channels > 0),
    pause_count INTEGER NOT NULL DEFAULT 0 CHECK (pause_count >= 0),
    silence_ms BIGINT NOT NULL DEFAULT 0 CHECK (silence_ms >= 0),
    loudness_envelope JSONB NOT NULL DEFAULT '[]',
    rhythm_vector JSONB NOT NULL DEFAULT '[]',
    metadata_path TEXT NOT NULL,
    report_path TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (recording_id, algorithm_version)
);

CREATE TABLE signal_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    analysis_id UUID NOT NULL REFERENCES recording_analyses(id) ON DELETE CASCADE,
    recording_id UUID NOT NULL REFERENCES recordings(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL CHECK (event_type IN ('pause','long_pause','repetition_candidate','speaker_turn','speaker_a','speaker_b')),
    start_ms BIGINT NOT NULL CHECK (start_ms >= 0),
    end_ms BIGINT NOT NULL CHECK (end_ms > start_ms),
    score DOUBLE PRECISION,
    payload JSONB NOT NULL DEFAULT '{}',
    review_state TEXT NOT NULL DEFAULT 'unreviewed' CHECK (review_state IN ('unreviewed','confirmed','rejected','uncertain')),
    reviewed_by UUID REFERENCES users(id),
    reviewed_at TIMESTAMPTZ,
    review_comment TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX signal_events_recording_time_idx ON signal_events(recording_id, start_ms, end_ms);
CREATE INDEX signal_events_review_idx ON signal_events(recording_id, event_type, review_state);

INSERT INTO analysis_jobs(recording_id, algorithm_version)
SELECT id, 'signal-v1' FROM recordings
ON CONFLICT (recording_id, algorithm_version) DO NOTHING;
