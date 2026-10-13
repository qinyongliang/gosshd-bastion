-- Convert stored HTTP bindings to the canonical listener list once.
-- Preserve routing, ownership, enablement and absolute expiration. A revision
-- bump makes connected Agents replace their previously cached deployments.
UPDATE tunnels
SET config = json_set(config, '$.http.listeners', json_array(json_object(
    'host', COALESCE(NULLIF(TRIM(json_extract(config, '$.listen_host')), ''), '0.0.0.0'),
    'port', json_extract(config, '$.listen_port')
))), revision = revision + 1
WHERE json_extract(config, '$.protocol') = 'http'
  AND (json_type(config, '$.http.listeners') IS NULL
       OR json_type(config, '$.http.listeners') = 'null'
       OR (json_type(config, '$.http.listeners') = 'array'
           AND json_array_length(config, '$.http.listeners') = 0));
