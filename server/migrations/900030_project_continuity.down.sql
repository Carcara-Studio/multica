ALTER TABLE project
    DROP COLUMN IF EXISTS continuity_revision,
    DROP COLUMN IF EXISTS continuity_content,
    DROP COLUMN IF EXISTS continuity_reviewed_by,
    DROP COLUMN IF EXISTS continuity_reviewed_at;
