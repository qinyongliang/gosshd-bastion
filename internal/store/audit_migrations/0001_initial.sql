CREATE TABLE IF NOT EXISTS command_audit_logs (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    user_email TEXT NOT NULL DEFAULT '',
    user_display_name TEXT NOT NULL DEFAULT '',
    target_id TEXT NOT NULL,
    target_name TEXT NOT NULL DEFAULT '',
    target_alias TEXT NOT NULL DEFAULT '',
    target_host TEXT NOT NULL DEFAULT '',
    target_port INTEGER NOT NULL DEFAULT 0,
    target_username TEXT NOT NULL DEFAULT '',
    organization_id TEXT NOT NULL DEFAULT '',
    session_id TEXT NOT NULL,
    command TEXT NOT NULL,
    request_type TEXT NOT NULL,
    policy_decision TEXT NOT NULL,
    policy_reason TEXT NOT NULL,
    public_key_fingerprint TEXT NOT NULL DEFAULT '',
    public_key_name TEXT NOT NULL DEFAULT '',
    exit_code INTEGER,
    started_at TEXT NOT NULL,
    ended_at TEXT,
    remote_address TEXT NOT NULL DEFAULT '',
    recording_path TEXT NOT NULL DEFAULT '',
    recording_size INTEGER NOT NULL DEFAULT 0,
    recording_sha256 TEXT NOT NULL DEFAULT '',
    recording_duration_ms INTEGER NOT NULL DEFAULT 0,
    recording_width INTEGER NOT NULL DEFAULT 0,
    recording_height INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_command_audit_user_started ON command_audit_logs (user_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_command_audit_target_started ON command_audit_logs (target_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_command_audit_started ON command_audit_logs (started_at DESC);
