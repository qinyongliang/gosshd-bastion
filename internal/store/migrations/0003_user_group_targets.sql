CREATE TABLE IF NOT EXISTS organization_user_group_targets (
	group_id TEXT NOT NULL REFERENCES organization_user_groups(id) ON DELETE CASCADE,
	target_id TEXT NOT NULL REFERENCES ssh_targets(id) ON DELETE CASCADE,
	created_at TEXT NOT NULL,
	PRIMARY KEY (group_id, target_id)
);

CREATE INDEX IF NOT EXISTS idx_user_group_targets_target
	ON organization_user_group_targets (target_id);

CREATE INDEX IF NOT EXISTS idx_user_group_targets_group
	ON organization_user_group_targets (group_id);
