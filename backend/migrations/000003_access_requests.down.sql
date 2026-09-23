DROP TABLE password_setup_tokens;
DROP TABLE access_requests;
ALTER TABLE users
    DROP COLUMN approved_at,
    DROP COLUMN approved_by,
    DROP COLUMN must_change_password,
    DROP COLUMN institution_id;
DROP TABLE institutions;
