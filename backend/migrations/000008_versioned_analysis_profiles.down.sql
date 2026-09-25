DELETE FROM analysis_jobs WHERE algorithm_version LIKE 'signal-v3-p%';
ALTER TABLE recording_analyses DROP COLUMN IF EXISTS configuration;
ALTER TABLE analysis_jobs DROP COLUMN IF EXISTS configuration;
