-- One bounded, explicitly published checkpoint, not a conversation archive.
ALTER TABLE project
    ADD COLUMN IF NOT EXISTS continuity_revision uuid,
    ADD COLUMN IF NOT EXISTS continuity_content text,
    ADD COLUMN IF NOT EXISTS continuity_reviewed_by uuid,
    ADD COLUMN IF NOT EXISTS continuity_reviewed_at timestamptz;
