-- Port forwarding replaces both legacy implicit TCP and explicit TCP tunnels.
-- Bump revisions so online Agents replace cached TCP deployments.
UPDATE tunnels
SET config = json_set(config, '$.protocol', 'port', '$.transports', json_array('tcp'))
WHERE COALESCE(json_extract(config, '$.protocol'), '') IN ('', 'tcp');

-- Network-qualified route grants replace the old deployment DTO for HTTP too.
-- Force cached deployments to refresh without changing any HTTP configuration.
UPDATE tunnels SET revision = revision + 1;
