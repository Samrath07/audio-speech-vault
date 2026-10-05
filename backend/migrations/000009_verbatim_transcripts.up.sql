CREATE TABLE recording_transcripts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    recording_id UUID NOT NULL UNIQUE REFERENCES recordings(id) ON DELETE CASCADE,
    convention_version INTEGER NOT NULL DEFAULT 1 CHECK (convention_version > 0),
    target_speaker_source TEXT CHECK (target_speaker_source IN ('speaker_a','speaker_b','manual')),
    target_speaker_confirmed_by UUID REFERENCES users(id),
    target_speaker_confirmed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE transcript_segments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    transcript_id UUID NOT NULL REFERENCES recording_transcripts(id) ON DELETE CASCADE,
    recording_id UUID NOT NULL REFERENCES recordings(id) ON DELETE CASCADE,
    start_ms BIGINT NOT NULL CHECK (start_ms >= 0),
    end_ms BIGINT NOT NULL CHECK (end_ms > start_ms),
    speaker_role TEXT NOT NULL CHECK (speaker_role IN ('target','interviewer','other','unknown','overlap')),
    text TEXT NOT NULL CHECK (char_length(text) BETWEEN 1 AND 8000),
    source TEXT NOT NULL DEFAULT 'human' CHECK (source IN ('human','machine')),
    created_by UUID NOT NULL REFERENCES users(id),
    updated_by UUID NOT NULL REFERENCES users(id),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);

CREATE INDEX transcript_segments_recording_time_idx
    ON transcript_segments(recording_id, start_ms, end_ms) WHERE deleted_at IS NULL;

CREATE TABLE transcript_segment_revisions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    segment_id UUID NOT NULL REFERENCES transcript_segments(id) ON DELETE CASCADE,
    revision_number INTEGER NOT NULL CHECK (revision_number > 0),
    start_ms BIGINT NOT NULL,
    end_ms BIGINT NOT NULL,
    speaker_role TEXT NOT NULL,
    text TEXT NOT NULL,
    changed_by UUID NOT NULL REFERENCES users(id),
    change_type TEXT NOT NULL CHECK (change_type IN ('created','updated','deleted')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(segment_id, revision_number)
);
