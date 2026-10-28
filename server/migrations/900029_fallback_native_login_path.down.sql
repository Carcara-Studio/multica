DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
        WHERE table_schema = current_schema() AND table_name = 'agent_fallback_config'
          AND column_name = 'native_login_path') THEN
        IF EXISTS (SELECT 1 FROM agent_fallback_config WHERE native_login_path) THEN
            RAISE EXCEPTION 'Revert native-login path configurations through protected management before downgrade';
        END IF;
        ALTER TABLE agent_fallback_config DROP COLUMN native_login_path;
    END IF;
END;
$$;
