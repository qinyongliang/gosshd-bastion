-- Metadata only. Never store screenshots, accessibility text or typed input.
CREATE TABLE desktop_audit (
 id TEXT PRIMARY KEY,
 user_id TEXT NOT NULL,
 target_id TEXT NOT NULL,
 session_id TEXT NOT NULL,
 method TEXT NOT NULL,
 outcome TEXT NOT NULL,
 started_at TEXT NOT NULL,
 updated_at TEXT NOT NULL
);
CREATE INDEX desktop_audit_actor_time ON desktop_audit(user_id,started_at);
CREATE INDEX desktop_audit_target_time ON desktop_audit(target_id,started_at);
