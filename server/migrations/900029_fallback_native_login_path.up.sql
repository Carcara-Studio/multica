ALTER TABLE agent_fallback_config
    ADD COLUMN IF NOT EXISTS native_login_path BOOLEAN NOT NULL DEFAULT false;
