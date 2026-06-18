package store

var migrations = []string{
	`CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		email TEXT NOT NULL UNIQUE,
		display_name TEXT NOT NULL,
		password_hash BLOB NOT NULL,
		created_at TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		token_hash BLOB NOT NULL UNIQUE,
		expires_at TEXT NOT NULL,
		created_at TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS organizations (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		slug TEXT NOT NULL UNIQUE,
		owner_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		is_personal INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL
	)`,
	`ALTER TABLE organizations ADD COLUMN is_personal INTEGER NOT NULL DEFAULT 0`,
	`CREATE TABLE IF NOT EXISTS organization_members (
		organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		role TEXT NOT NULL,
		created_at TEXT NOT NULL,
		PRIMARY KEY (organization_id, user_id)
	)`,
	`CREATE TABLE IF NOT EXISTS organization_user_groups (
		id TEXT PRIMARY KEY,
		organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		slug TEXT NOT NULL,
		is_default INTEGER NOT NULL,
		created_at TEXT NOT NULL,
		UNIQUE (organization_id, slug)
	)`,
	`CREATE TABLE IF NOT EXISTS organization_user_group_members (
		group_id TEXT NOT NULL REFERENCES organization_user_groups(id) ON DELETE CASCADE,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		created_at TEXT NOT NULL,
		PRIMARY KEY (group_id, user_id)
	)`,
	`CREATE TABLE IF NOT EXISTS organization_invites (
		id TEXT PRIMARY KEY,
		organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
		code_hash BLOB NOT NULL UNIQUE,
		role TEXT NOT NULL,
		expires_at TEXT NOT NULL,
		created_by TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		created_at TEXT NOT NULL,
		consumed_at TEXT
	)`,
	`CREATE TABLE IF NOT EXISTS user_public_keys (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		authorized_key TEXT NOT NULL,
		fingerprint TEXT NOT NULL UNIQUE,
		created_at TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS ssh_targets (
		id TEXT PRIMARY KEY,
		owner_type TEXT NOT NULL,
		owner_id TEXT NOT NULL,
		name TEXT NOT NULL DEFAULT '',
		alias TEXT NOT NULL,
		target_type TEXT NOT NULL,
		host TEXT NOT NULL,
		port INTEGER NOT NULL,
		remote_username TEXT NOT NULL,
		auth_type TEXT NOT NULL,
		encrypted_secret BLOB,
		agent_id TEXT,
		created_by TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		UNIQUE (owner_type, owner_id, alias)
	)`,
	`ALTER TABLE ssh_targets ADD COLUMN name TEXT NOT NULL DEFAULT ''`,
	`UPDATE ssh_targets SET name = alias WHERE name = ''`,
	`CREATE TABLE IF NOT EXISTS target_tags (
		id TEXT PRIMARY KEY,
		owner_type TEXT NOT NULL,
		owner_id TEXT NOT NULL,
		name TEXT NOT NULL,
		created_at TEXT NOT NULL,
		UNIQUE (owner_type, owner_id, name)
	)`,
	`CREATE TABLE IF NOT EXISTS target_tag_bindings (
		target_id TEXT NOT NULL REFERENCES ssh_targets(id) ON DELETE CASCADE,
		tag_id TEXT NOT NULL REFERENCES target_tags(id) ON DELETE CASCADE,
		created_at TEXT NOT NULL,
		PRIMARY KEY (target_id, tag_id)
	)`,
	`CREATE TABLE IF NOT EXISTS agent_enrollments (
		id TEXT PRIMARY KEY,
		owner_type TEXT NOT NULL,
		owner_id TEXT NOT NULL,
		token_hash BLOB NOT NULL UNIQUE,
		label TEXT NOT NULL,
		default_host TEXT NOT NULL,
		default_port INTEGER NOT NULL,
		created_by TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		created_at TEXT NOT NULL,
		expires_at TEXT NOT NULL,
		consumed_agent_id TEXT
	)`,
	`CREATE TABLE IF NOT EXISTS agents (
		id TEXT PRIMARY KEY,
		owner_type TEXT NOT NULL,
		owner_id TEXT NOT NULL,
		enrollment_id TEXT REFERENCES agent_enrollments(id) ON DELETE SET NULL,
		label TEXT NOT NULL,
		current_runtime_id TEXT NOT NULL,
		last_seen_at TEXT NOT NULL,
		created_at TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS command_policies (
		id TEXT PRIMARY KEY,
		owner_type TEXT NOT NULL,
		owner_id TEXT NOT NULL,
		name TEXT NOT NULL,
		default_action TEXT NOT NULL,
		llm_config_id TEXT,
		llm_prompt_id TEXT,
		created_at TEXT NOT NULL
	)`,
	`ALTER TABLE command_policies ADD COLUMN llm_prompt_id TEXT`,
	`CREATE TABLE IF NOT EXISTS policy_rules (
		id TEXT PRIMARY KEY,
		policy_id TEXT NOT NULL REFERENCES command_policies(id) ON DELETE CASCADE,
		rule_type TEXT NOT NULL,
		pattern_type TEXT NOT NULL,
		pattern TEXT NOT NULL,
		created_at TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS policy_targets (
		policy_id TEXT NOT NULL REFERENCES command_policies(id) ON DELETE CASCADE,
		target_id TEXT NOT NULL REFERENCES ssh_targets(id) ON DELETE CASCADE,
		PRIMARY KEY (policy_id, target_id)
	)`,
	`CREATE TABLE IF NOT EXISTS policy_user_groups (
		policy_id TEXT NOT NULL REFERENCES command_policies(id) ON DELETE CASCADE,
		group_id TEXT NOT NULL REFERENCES organization_user_groups(id) ON DELETE CASCADE,
		PRIMARY KEY (policy_id, group_id)
	)`,
	`CREATE TABLE IF NOT EXISTS llm_policy_configs (
		id TEXT PRIMARY KEY,
		owner_type TEXT NOT NULL,
		owner_id TEXT NOT NULL,
		name TEXT NOT NULL,
		base_url TEXT NOT NULL,
		api_key_encrypted BLOB,
		model TEXT NOT NULL,
		timeout_seconds INTEGER NOT NULL,
		created_at TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS llm_prompt_resources (
		id TEXT PRIMARY KEY,
		owner_type TEXT NOT NULL,
		owner_id TEXT NOT NULL,
		title TEXT NOT NULL,
		content TEXT NOT NULL,
		is_default INTEGER NOT NULL,
		is_readonly INTEGER NOT NULL,
		created_at TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS command_audit_logs (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		target_id TEXT NOT NULL REFERENCES ssh_targets(id) ON DELETE CASCADE,
		organization_id TEXT,
		session_id TEXT NOT NULL,
		command TEXT NOT NULL,
		request_type TEXT NOT NULL,
		policy_decision TEXT NOT NULL,
		policy_reason TEXT NOT NULL,
		exit_code INTEGER,
		started_at TEXT NOT NULL,
		ended_at TEXT,
		remote_address TEXT NOT NULL
	)`,
}
