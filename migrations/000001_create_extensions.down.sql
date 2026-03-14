-- 000001_create_extensions.down.sql
-- Drop extensions (only safe if no dependent objects exist)

DROP EXTENSION IF EXISTS "pgcrypto";
DROP EXTENSION IF EXISTS "uuid-ossp";
