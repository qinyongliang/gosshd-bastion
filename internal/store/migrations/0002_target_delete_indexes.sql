CREATE INDEX IF NOT EXISTS idx_ssh_targets_proxy_target ON ssh_targets (proxy_target_id);
CREATE INDEX IF NOT EXISTS idx_policy_targets_target ON policy_targets (target_id);
CREATE INDEX IF NOT EXISTS idx_command_audit_target_started ON command_audit_logs (target_id, started_at DESC);
