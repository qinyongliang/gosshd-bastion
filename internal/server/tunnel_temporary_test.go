package server

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/store"
	ssh "golang.org/x/crypto/ssh"
)

func TestTemporarySSHForwardAttributionTrafficAndStop(t *testing.T) {
	app, user, org := tunnelFixture(t)
	target := tunnelAgentTarget(t, app, user, org, "temporary-agent")
	temporary, finish := app.registerSSHForward(user.ID, "SHA256:test-key", target, "192.0.2.1:3456", "local", "127.0.0.1", 8080)
	defer finish()
	rows, err := app.listTunnelViews(context.Background(), user, org.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || !rows[0].Temporary || rows[0].CreatedBy != user.ID || rows[0].PublicKeyFingerprint != "SHA256:test-key" || rows[0].ListenHost != "" || rows[0].ListenPort != 0 {
		t.Fatalf("attribution/invented listener: %+v", rows)
	}
	client, entry := net.Pipe()
	exit, destination := net.Pipe()
	defer client.Close()
	defer destination.Close()
	done := make(chan struct{})
	go func() { bridgeSSHForward(entry, exit, temporary); close(done) }()
	go func() { io.Copy(destination, destination) }()
	client.SetDeadline(time.Now().Add(3 * time.Second))
	payload := []byte("temporary tunnel")
	if _, err := client.Write(payload); err != nil {
		t.Fatal(err)
	}
	received := make([]byte, len(payload))
	if _, err := io.ReadFull(client, received); err != nil {
		t.Fatal(err)
	}
	if string(received) != string(payload) {
		t.Fatal("corrupted data")
	}
	view := app.tunnelView(temporary.config)
	deadline := time.Now().Add(time.Second)
	for view.Traffic.RelayDown != int64(len(payload)) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		view = app.tunnelView(temporary.config)
	}
	if view.Connections != 1 || view.Traffic.RelayUp != int64(len(payload)) || view.Traffic.RelayDown != int64(len(payload)) {
		t.Fatalf("traffic not tracked: %+v", view)
	}
	temporary.stop()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("stop failed to close both directions")
	}
	finish()
	rows, err = app.listTunnelViews(context.Background(), user, org.ID)
	if err != nil || len(rows) != 0 {
		t.Fatalf("temporary forward leaked: %v %+v", err, rows)
	}
	total, err := app.audit.Repository().TunnelTrafficTotal(context.Background(), temporary.config.ID)
	if err != nil || total.ConnectionsOpened != 1 || total.RelayUp != int64(len(payload)) || total.RelayDown != int64(len(payload)) {
		t.Fatalf("audit statistics lost: %+v %v", total, err)
	}
	sources, err := app.audit.Repository().TunnelTrafficSources(context.Background(), temporary.config.ID, 0, time.Now().Unix()+300, "192.0.2.1")
	if err != nil || len(sources) != 1 || sources[0].ConnectionsOpened != 1 || sources[0].RelayUp != int64(len(payload)) || sources[0].RelayDown != int64(len(payload)) {
		t.Fatalf("SSH source attribution: %+v %v", sources, err)
	}
	configs, err := app.store.Repository().ListTunnels(context.Background(), org.ID)
	if err != nil || len(configs) != 0 {
		t.Fatal("temporary forward became persistent")
	}
}
func TestTemporarySSHRemoteListenerStopsWithoutDeadlock(t *testing.T) {
	app, user, org := tunnelFixture(t)
	target := tunnelAgentTarget(t, app, user, org, "remote-agent")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	temp, finish := app.registerSSHForward(user.ID, "key", target, "192.0.2.2", "remote", "127.0.0.1", ln.Addr().(*net.TCPAddr).Port)
	wrapped := &trackedForwardListener{Listener: ln, tunnel: temp, finish: finish}
	temp.track(wrapped)
	done := make(chan struct{})
	go func() { temp.stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("recursive listener close deadlocked")
	}
	if _, err = ln.Accept(); err == nil {
		t.Fatal("listener still open")
	}
	if _, err = app.temporaryTunnel(context.Background(), user, temp.config.ID); err != store.ErrNotFound {
		t.Fatalf("listener not removed: %v", err)
	}
}

func TestTemporarySSHForwardsThroughBastion(t *testing.T) {
	app, _, sshAddress, stop := startBastionTestApp(t)
	defer stop()
	ctx := context.Background()
	signer := testSSHSigner(t)
	user := seedBastionUserWithKey(t, app, signer)
	org, err := app.store.Repository().GetPersonalOrganizationForUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	target := tunnelSSHTarget(t, app, user, org, "forward-box", tunnelTestSSH(t), "")
	groups, err := app.store.Repository().ListOrganizationUserGroups(ctx, org.ID)
	if err != nil || len(groups) == 0 {
		t.Fatal("missing group")
	}
	policy, err := app.store.Repository().CreateCommandPolicy(ctx, store.CreateCommandPolicyParams{OwnerType: store.OwnerOrganization, OwnerID: org.ID, Name: "forward", DefaultAction: store.DecisionAllow, AllowPortForward: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = app.store.Repository().AttachPolicyToTarget(ctx, policy.ID, target.ID); err != nil {
		t.Fatal(err)
	}
	if err = app.store.Repository().AttachPolicyToUserGroup(ctx, policy.ID, groups[0].ID); err != nil {
		t.Fatal(err)
	}
	client, err := ssh.Dial("tcp", sshAddress, &ssh.ClientConfig{User: target.Alias, Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)}, HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	echo := tunnelEcho(t)
	forwarded, err := client.Dial("tcp", echo.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer forwarded.Close()
	roundtripTunnel(t, forwarded)
	rows, err := app.listTunnelViews(ctx, user, org.ID)
	if err != nil || len(rows) != 1 || !rows[0].Temporary || rows[0].CreatedBy != user.ID || rows[0].ForwardType != "local" {
		t.Fatalf("local SSH not registered: %v %+v", err, rows)
	}
	localID := rows[0].ID
	app.tunnels.mu.Lock()
	local := app.tunnels.temporary[localID]
	app.tunnels.mu.Unlock()
	local.stop()
	readDone := make(chan error, 1)
	go func() { var b [1]byte; _, err := forwarded.Read(b[:]); readDone <- err }()
	select {
	case err = <-readDone:
		if err == nil {
			t.Fatal("local forward survived stop")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("local forward did not close")
	}
	remote, err := client.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rows, err = app.listTunnelViews(ctx, user, org.ID)
	if err != nil {
		t.Fatal(err)
	}
	var remoteID string
	for _, r := range rows {
		if r.ForwardType == "remote" {
			remoteID = r.ID
			if r.ListenPort == 0 || r.DestinationHost != "" {
				t.Fatalf("remote metadata: %+v", r)
			}
		}
	}
	if remoteID == "" {
		t.Fatalf("remote listener absent: %+v", rows)
	}
	go func() {
		c, err := remote.Accept()
		if err == nil {
			defer c.Close()
			_, _ = io.Copy(c, c)
		}
	}()
	peer, err := net.Dial("tcp", remote.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	roundtripTunnel(t, peer)
	// Closing a client remote listener sends cancel-tcpip-forward; the server must actually release it.
	if err = remote.Close(); err != nil {
		t.Fatal(err)
	}
	peer.SetReadDeadline(time.Now().Add(time.Second))
	var data [1]byte
	if _, err = peer.Read(data[:]); err == nil {
		t.Fatal("cancel did not close active remote TCP")
	}
	total, err := app.audit.Repository().TunnelTrafficTotal(ctx, remoteID)
	if err != nil || total.RelayUp != 56000 || total.RelayDown != 56000 || total.ConnectionsOpened != 1 {
		t.Fatalf("remote statistics lost: %+v %v", total, err)
	}
	sources, err := app.audit.Repository().TunnelTrafficSources(ctx, remoteID, 0, time.Now().Unix()+300, "127.0.0.1")
	if err != nil || len(sources) != 1 || sources[0].RelayUp != 56000 || sources[0].RelayDown != 56000 || sources[0].ConnectionsOpened != 1 {
		t.Fatalf("remote SSH source attribution: %+v %v", sources, err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		app.tunnels.mu.Lock()
		exists := app.tunnels.temporary[remoteID] != nil
		app.tunnels.mu.Unlock()
		if !exists {
			return
		}
		time.Sleep(time.Millisecond * 5)
	}
	t.Fatal("SSH cancellation left temporary listener registered")
}
