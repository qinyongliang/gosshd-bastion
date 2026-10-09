CREATE TABLE tunnel_traffic (
 tunnel_id TEXT NOT NULL,
 organization_id TEXT NOT NULL,
 bucket_start INTEGER NOT NULL,
 relay_up INTEGER NOT NULL DEFAULT 0,
 relay_down INTEGER NOT NULL DEFAULT 0,
 direct_up INTEGER NOT NULL DEFAULT 0,
 direct_down INTEGER NOT NULL DEFAULT 0,
 connections_opened INTEGER NOT NULL DEFAULT 0,
 peak_connections INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(tunnel_id,bucket_start)
);
CREATE INDEX idx_tunnel_traffic_org_time ON tunnel_traffic(organization_id,bucket_start);
CREATE TABLE tunnel_traffic_totals (
 tunnel_id TEXT PRIMARY KEY,
 organization_id TEXT NOT NULL,
 relay_up INTEGER NOT NULL DEFAULT 0,
 relay_down INTEGER NOT NULL DEFAULT 0,
 direct_up INTEGER NOT NULL DEFAULT 0,
 direct_down INTEGER NOT NULL DEFAULT 0,
 connections_opened INTEGER NOT NULL DEFAULT 0,
 peak_connections INTEGER NOT NULL DEFAULT 0
);
