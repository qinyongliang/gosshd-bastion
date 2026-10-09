package server

import (
	"context"
	"net"
	"strconv"

	"github.com/hashicorp/yamux"
	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
	ssh "golang.org/x/crypto/ssh"
)

type tunnelAgentEndpoint struct {
	id      string
	session *yamux.Session
	hops    []protocol.TunnelSSHHop
}

// SSH machines behind an Agent are executed from that Agent, allowing the Agent-to-Agent
// segment to migrate to P2P while the remote SSH and destination TCP sockets stay intact.
func (a *App) tunnelAgentEndpoint(ctx context.Context, id string) (*tunnelAgentEndpoint, error) {
	if id == "" {
		return nil, nil
	}
	repo := a.store.Repository()
	target, err := repo.GetSSHTarget(ctx, id)
	if err != nil {
		return nil, err
	}
	original := target
	var targets []store.SSHTarget
	for target.TargetType != store.TargetAgent {
		targets = append([]store.SSHTarget{target}, targets...)
		if target.ProxyTargetID == "" {
			return nil, nil
		}
		target, err = repo.GetSSHTarget(ctx, target.ProxyTargetID)
		if err != nil {
			return nil, err
		}
		if len(targets) > 3 {
			return nil, nil
		}
	}
	session, err := a.registry.Get(target.AgentID)
	if err != nil {
		return nil, err
	}
	result := &tunnelAgentEndpoint{id: target.AgentID, session: session}
	if len(targets) == 0 {
		return result, nil
	}
	keys := map[string][]byte{}
	verify := a.targetHostKeyCallback()
	client, err := a.openTunnelSSHClient(ctx, original, 0, func(address string, remote net.Addr, key ssh.PublicKey) error {
		if err := verify(address, remote, key); err != nil {
			return err
		}
		keys[address] = append([]byte(nil), key.Marshal()...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	_ = client.Close()
	for _, hop := range targets {
		hop, err = a.resolveTargetCredential(ctx, hop)
		if err != nil {
			return nil, err
		}
		address := net.JoinHostPort(hop.Host, strconv.Itoa(hop.Port))
		result.hops = append(result.hops, protocol.TunnelSSHHop{Address: address, Username: hop.RemoteUsername, AuthType: hop.AuthType, Secret: hop.EncryptedSecret, HostKey: keys[address]})
	}
	return result, nil
}
