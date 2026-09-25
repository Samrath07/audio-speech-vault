ALTER TABLE recording_analyses
    ADD COLUMN speech_chunks JSONB NOT NULL DEFAULT '[]',
    ADD COLUMN priority_score DOUBLE PRECISION NOT NULL DEFAULT 0,
    ADD COLUMN priority_breakdown JSONB NOT NULL DEFAULT '{}';

CREATE INDEX recording_analyses_priority_idx ON recording_analyses(priority_score DESC);

INSERT INTO analysis_jobs(recording_id, algorithm_version)
SELECT id, 'signal-v2' FROM recordings
ON CONFLICT (recording_id, algorithm_version) DO NOTHING;
