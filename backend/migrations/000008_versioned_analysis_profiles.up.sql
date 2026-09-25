ALTER TABLE analysis_jobs ADD COLUMN configuration JSONB NOT NULL DEFAULT '{}';
ALTER TABLE recording_analyses ADD COLUMN configuration JSONB NOT NULL DEFAULT '{}';

INSERT INTO analysis_profiles(project_id, updated_by)
SELECT p.id, p.created_by FROM projects p
ON CONFLICT (project_id) DO NOTHING;

INSERT INTO analysis_jobs(recording_id, algorithm_version, configuration)
SELECT r.id,
       'signal-v3-p' || p.version,
       jsonb_build_object(
           'silenceDbfs', p.silence_dbfs,
           'minimumPauseMs', p.minimum_pause_ms,
           'longPauseMs', p.long_pause_ms,
           'repetitionSimilarity', p.repetition_similarity,
           'speakerSeparation', p.speaker_separation
       )
FROM recordings r
JOIN analysis_profiles p ON p.project_id = r.project_id
ON CONFLICT (recording_id, algorithm_version) DO NOTHING;
