DELETE FROM analysis_jobs WHERE algorithm_version = 'signal-v2';
ALTER TABLE recording_analyses
    DROP COLUMN IF EXISTS priority_breakdown,
    DROP COLUMN IF EXISTS priority_score,
    DROP COLUMN IF EXISTS speech_chunks;
