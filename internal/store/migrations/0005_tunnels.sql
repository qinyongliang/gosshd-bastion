CREATE TABLE tunnels (
 id TEXT PRIMARY KEY,
 organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
 created_by TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 -- Keep activation identity after account deletion so runtime permission checks fail closed.
 enabled_by TEXT,
 config TEXT NOT NULL,
 enabled INTEGER NOT NULL DEFAULT 0,
 expires_at INTEGER,
 revision INTEGER NOT NULL DEFAULT 1,
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL
);
CREATE INDEX idx_tunnels_organization ON tunnels(organization_id);
