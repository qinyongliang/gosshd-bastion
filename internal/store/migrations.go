package store

var migrations = []string{
	`CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		email TEXT NOT NULL UNIQUE,
		display_name TEXT NOT NULL,
		password_hash BLOB NOT NULL,
		is_system_admin INTEGER NOT NULL DEFAULT 0,
		auth_provider TEXT NOT NULL DEFAULT 'local',
		disabled_at TEXT,
		created_at TEXT NOT NULL
	)`,
	`ALTER TABLE users ADD COLUMN is_system_admin INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE users ADD COLUMN auth_provider TEXT NOT NULL DEFAULT 'local'`,
	`ALTER TABLE users ADD COLUMN disabled_at TEXT`,
	`CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		token_hash BLOB NOT NULL UNIQUE,
		expires_at TEXT NOT NULL,
		created_at TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS mcp_tokens (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		token_hash BLOB NOT NULL UNIQUE,
		token_value TEXT NOT NULL DEFAULT '',
		tool_groups TEXT NOT NULL DEFAULT 'session',
		last_used_at TEXT,
		created_at TEXT NOT NULL
	)`,
	`ALTER TABLE mcp_tokens ADD COLUMN tool_groups TEXT NOT NULL DEFAULT 'session'`,
	`ALTER TABLE mcp_tokens ADD COLUMN token_value TEXT NOT NULL DEFAULT ''`,
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
	`ALTER TABLE ssh_targets ADD COLUMN proxy_target_id TEXT`,
	`ALTER TABLE ssh_targets ADD COLUMN credential_id TEXT`,
	`ALTER TABLE ssh_targets ADD COLUMN folder_id TEXT`,
	`CREATE TABLE IF NOT EXISTS ssh_credentials (
		id TEXT PRIMARY KEY,
		owner_type TEXT NOT NULL,
		owner_id TEXT NOT NULL,
		name TEXT NOT NULL,
		username TEXT NOT NULL,
		auth_type TEXT NOT NULL,
		encrypted_secret BLOB,
		created_by TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		UNIQUE (owner_type, owner_id, name)
	)`,
	`CREATE TABLE IF NOT EXISTS target_folders (
		id TEXT PRIMARY KEY,
		owner_type TEXT NOT NULL,
		owner_id TEXT NOT NULL,
		parent_id TEXT,
		name TEXT NOT NULL,
		created_by TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		UNIQUE (owner_type, owner_id, parent_id, name)
	)`,
	`CREATE TABLE IF NOT EXISTS user_settings (
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		key TEXT NOT NULL,
		value_json TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		PRIMARY KEY (user_id, key)
	)`,
	`CREATE TABLE IF NOT EXISTS batch_command_histories (
		id TEXT PRIMARY KEY,
		owner_type TEXT NOT NULL,
		owner_id TEXT NOT NULL,
		command TEXT NOT NULL,
		execute_count INTEGER NOT NULL,
		created_by TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		UNIQUE (owner_type, owner_id, command)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_batch_command_histories_owner_count ON batch_command_histories (owner_type, owner_id, execute_count DESC, updated_at DESC)`,
	`CREATE TABLE IF NOT EXISTS target_tags (
		id TEXT PRIMARY KEY,
		owner_type TEXT NOT NULL,
		owner_id TEXT NOT NULL,
		name TEXT NOT NULL,
		color TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		UNIQUE (owner_type, owner_id, name)
	)`,
	`ALTER TABLE target_tags ADD COLUMN color TEXT NOT NULL DEFAULT ''`,
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
		ip_allowlist TEXT NOT NULL DEFAULT '',
		allow_port_forward INTEGER NOT NULL DEFAULT 0,
		allow_upload INTEGER NOT NULL DEFAULT 0,
		allow_download INTEGER NOT NULL DEFAULT 0,
		allow_ssh_interactive INTEGER NOT NULL DEFAULT 0,
		allow_web_terminal INTEGER NOT NULL DEFAULT 0,
		allow_manual_review INTEGER NOT NULL DEFAULT 0,
		manual_review_timeout_seconds INTEGER NOT NULL DEFAULT 30,
		created_at TEXT NOT NULL
	)`,
	`ALTER TABLE command_policies ADD COLUMN llm_prompt_id TEXT`,
	`ALTER TABLE command_policies ADD COLUMN ip_allowlist TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE command_policies ADD COLUMN allow_port_forward INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE command_policies ADD COLUMN allow_upload INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE command_policies ADD COLUMN allow_download INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE command_policies ADD COLUMN allow_ssh_interactive INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE command_policies ADD COLUMN allow_web_terminal INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE command_policies ADD COLUMN allow_manual_review INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE command_policies ADD COLUMN manual_review_timeout_seconds INTEGER NOT NULL DEFAULT 30`,
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
	`CREATE TABLE IF NOT EXISTS policy_target_tags (
		policy_id TEXT NOT NULL REFERENCES command_policies(id) ON DELETE CASCADE,
		tag_id TEXT NOT NULL REFERENCES target_tags(id) ON DELETE CASCADE,
		PRIMARY KEY (policy_id, tag_id)
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
		public_key_fingerprint TEXT NOT NULL DEFAULT '',
		exit_code INTEGER,
		started_at TEXT NOT NULL,
		ended_at TEXT,
		remote_address TEXT NOT NULL
	)`,
	`ALTER TABLE command_audit_logs ADD COLUMN public_key_fingerprint TEXT NOT NULL DEFAULT ''`,
	`CREATE TABLE IF NOT EXISTS external_identities (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		provider TEXT NOT NULL,
		subject TEXT NOT NULL,
		email TEXT NOT NULL,
		display_name TEXT NOT NULL,
		raw_profile_json TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		UNIQUE (provider, subject)
	)`,
	`CREATE TABLE IF NOT EXISTS system_settings (
		key TEXT PRIMARY KEY,
		value_json TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		updated_by TEXT
	)`,
	`CREATE TABLE IF NOT EXISTS oauth_states (
		state_hash BLOB NOT NULL,
		provider TEXT NOT NULL,
		redirect_after TEXT NOT NULL,
		expires_at TEXT NOT NULL,
		created_at TEXT NOT NULL,
		PRIMARY KEY (provider, state_hash)
	)`,
}
