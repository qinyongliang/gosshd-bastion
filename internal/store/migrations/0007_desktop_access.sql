CREATE TABLE desktop_access (
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 target_id TEXT NOT NULL REFERENCES ssh_targets(id) ON DELETE CASCADE,
 allow_read INTEGER NOT NULL DEFAULT 0,
 allow_control INTEGER NOT NULL DEFAULT 0,
 expires_at TEXT,
 granted_by TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 PRIMARY KEY(user_id,target_id),
 CHECK(allow_control = 0 OR allow_read = 1)
);
CREATE INDEX desktop_access_target ON desktop_access(target_id);
