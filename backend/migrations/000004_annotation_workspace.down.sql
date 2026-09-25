DROP TABLE IF EXISTS review_comments;
DROP TABLE IF EXISTS annotation_revisions;
DROP TABLE IF EXISTS annotations;
DROP TABLE IF EXISTS annotation_tasks;
DROP TABLE IF EXISTS tier_values;
DROP TABLE IF EXISTS tier_definitions;
DROP TABLE IF EXISTS recordings;
DROP INDEX IF EXISTS projects_institution_idx;
ALTER TABLE projects DROP COLUMN IF EXISTS status, DROP COLUMN IF EXISTS description, DROP COLUMN IF EXISTS institution_id;
