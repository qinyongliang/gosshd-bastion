CREATE TABLE IF NOT EXISTS temporary_ssh_authorizations (
	id TEXT PRIMARY KEY,
	target_id TEXT NOT NULL REFERENCES ssh_targets(id) ON DELETE CASCADE,
	name TEXT NOT NULL,
	encrypted_token BLOB NOT NULL,
	token_hash BLOB NOT NULL UNIQUE,
	created_by TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	expires_at TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_temporary_ssh_authorizations_target
	ON temporary_ssh_authorizations (target_id);
