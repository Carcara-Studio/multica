DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_attribute
        WHERE attrelid = to_regclass('agent_fallback_config')
          AND attname = 'native_login_path' AND NOT attisdropped) THEN
        IF EXISTS (SELECT 1 FROM agent_fallback_config WHERE native_login_path) THEN
            RAISE EXCEPTION 'Revert native-login path configurations through protected management before downgrade';
        END IF;
        ALTER TABLE agent_fallback_config DROP COLUMN native_login_path;
    END IF;
END;
$$;
