-- 000048_anonymous_md_features.down.sql
-- Revert Anonymous.md privacy and security features.

DROP TABLE IF EXISTS panic_codes CASCADE;
DROP TABLE IF EXISTS anonymous_addresses CASCADE;
DROP TABLE IF EXISTS incognito_sessions CASCADE;
DROP TABLE IF EXISTS single_use_invites CASCADE;
DROP TABLE IF EXISTS transport_isolation_config CASCADE;
DROP TABLE IF EXISTS censorship_bridges CASCADE;
DROP TABLE IF EXISTS censorship_domains CASCADE;
DROP TABLE IF EXISTS contact_verification_saf CASCADE;
DROP TABLE IF EXISTS multi_device_sessions CASCADE;
DROP TABLE IF EXISTS security_events CASCADE;

DROP INDEX IF EXISTS idx_anonymous_map;
DROP INDEX IF EXISTS idx_incognito_user;
DROP INDEX IF EXISTS idx_emergency_user;