-- 045 — Security audit trail, recovery reissue, and new-device notifications.
-- Identity spec §5 (2FA recovery) and §5.5 (device-awareness).

-- security_events records every sensitive operation for the user-facing
-- security log, new-device detection, and operator audit. Events include
-- login_success, login_failed, password_changed, credential_changed,
-- 2fa_enabled, 2fa_disabled, 2fa_recovery_reissued, account_deleted,
-- account_recovered, session_revoked, session_revoked_all, and admin_action.
CREATE TABLE IF NOT EXISTS security_events (
    id          BIGSERIAL PRIMARY KEY,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    event       TEXT NOT NULL,
    ip_address  INET,
    user_agent  TEXT,
    details     TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_security_events_user_time
    ON security_events(user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_security_events_ip_agent
    ON security_events(user_id, host(ip_address), lower(user_agent), event)
    WHERE event = 'login_success' AND ip_address IS NOT NULL;

-- recovery_code_reissues tracks how often a user has requested fresh codes
-- so we can enforce a 30-day cooldown (abuse prevention).
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS recovery_reissued_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_users_recovery_reissue
    ON users(recovery_reissued_at)
    WHERE recovery_reissued_at IS NOT NULL;