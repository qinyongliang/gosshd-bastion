package agent

import (
	"context"

	"github.com/pkg/sftp"
	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
)

// A borrowed client belongs to the session. File cleanup only closes owned clients.
func openFileSFTP(ctx context.Context, hops []protocol.TunnelSSHHop, shared ...*sftp.Client) (*sftp.Client, func(), error) {
	if len(shared) > 0 {
		return shared[0], func() {}, nil
	}
	ssh, err := openTunnelSSH(ctx, hops)
	if err != nil || ssh == nil {
		return nil, func() {}, err
	}
	stop := context.AfterFunc(ctx, func() { _ = ssh.Close() })
	client, err := sftp.NewClient(ssh)
	cleanup := func() {
		stop()
		if client != nil {
			_ = client.Close()
		}
		_ = ssh.Close()
	}
	if err != nil {
		cleanup()
		return nil, func() {}, err
	}
	return client, cleanup, nil
}
