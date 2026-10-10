package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/sftp"
	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
	"github.com/qinyongliang/gosshd-bastion/internal/tunnel"
	"github.com/qinyongliang/gosshd-bastion/internal/upload"
)

type uploadDestination struct {
	io.WriteCloser
	commit func() error
	abort  func()
}

func openUploadDestination(ctx context.Context, req protocol.StreamRequest) (*uploadDestination, error) {
	if req.Upload == nil || req.Upload.Path == "" || req.Upload.Size < 0 || req.Upload.Size > upload.MaxSize {
		return nil, errors.New("invalid upload request")
	}
	dest := req.Upload.Path
	sshClient, err := openTunnelSSH(ctx, req.TunnelHops)
	if err != nil {
		return nil, err
	}
	if sshClient != nil {
		stop := context.AfterFunc(ctx, func() { _ = sshClient.Close() })
		client, err := sftp.NewClient(sshClient)
		if err != nil {
			stop()
			_ = sshClient.Close()
			return nil, err
		}
		cleanup := func() { stop(); _ = client.Close(); _ = sshClient.Close() }
		temp := path.Join(path.Dir(dest), ".gosshd-upload-"+uuid.NewString())
		file, err := client.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
		if err != nil {
			cleanup()
			return nil, err
		}
		if info, err := client.Stat(dest); err == nil {
			if info.IsDir() {
				_ = file.Close()
				_ = client.Remove(temp)
				cleanup()
				return nil, errors.New("destination is a directory")
			}
			if err := file.Chmod(info.Mode().Perm()); err != nil {
				_ = file.Close()
				_ = client.Remove(temp)
				cleanup()
				return nil, err
			}
		}
		committed := false
		return &uploadDestination{WriteCloser: file, commit: func() error {
			var err error
			if _, supported := client.HasExtension("posix-rename@openssh.com"); supported {
				err = client.PosixRename(temp, dest)
			} else {
				// Never delete the original before a successful commit.
				err = client.Rename(temp, dest)
			}
			committed = err == nil
			return err
		}, abort: func() {
			_ = file.Close()
			if !committed {
				err := client.Remove(temp)
				if err != nil && ctx.Err() != nil {
					// Cancellation closes SSH to unblock a stalled write. Reconnect
					// briefly to remove the temporary file using the same pinned hops.
					cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					if ssh, err := openTunnelSSH(cleanupCtx, req.TunnelHops); err == nil && ssh != nil {
						defer ssh.Close()
						stopCleanup := context.AfterFunc(cleanupCtx, func() { _ = ssh.Close() })
						defer stopCleanup()
						if sftpClient, err := sftp.NewClient(ssh); err == nil {
							_ = sftpClient.Remove(temp)
							_ = sftpClient.Close()
						}
					}
				}
			}
			cleanup()
		}}, nil
	}
	// Use the same working directory as the existing Agent SFTP server.
	temp := filepath.Join(filepath.Dir(dest), ".gosshd-upload-"+uuid.NewString())
	file, err := os.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0666)
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(dest); err == nil {
		if info.IsDir() {
			_ = file.Close()
			_ = os.Remove(temp)
			return nil, errors.New("destination is a directory")
		}
		if err := file.Chmod(info.Mode().Perm()); err != nil {
			_ = file.Close()
			_ = os.Remove(temp)
			return nil, err
		}
	}
	return &uploadDestination{WriteCloser: file, commit: func() error { return os.Rename(temp, dest) }, abort: func() { _ = file.Close(); _ = os.Remove(temp) }}, nil
}

func (c *Client) handleFileUpload(stream io.ReadWriteCloser, reader *bufio.Reader, req protocol.StreamRequest) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	setupTimeout := time.AfterFunc(20*time.Second, cancel)
	defer setupTimeout.Stop()
	dst, err := openUploadDestination(ctx, req)
	if err != nil {
		_ = protocol.WriteJSONLine(stream, protocol.StreamResponse{Error: err.Error()})
		return
	}
	defer dst.abort()
	setupTimeout.Stop()
	if protocol.WriteJSONLine(stream, protocol.StreamResponse{OK: true, Peer: true}) != nil {
		return
	}
	relay := &tunnel.Relay{Reader: reader, Writer: stream, Closer: stream}
	stable := tunnel.NewConn(relay)
	negotiator := tunnel.NewNegotiator(stable, false, req.STUNServers)
	defer stable.Close()
	defer negotiator.Close()
	go stable.Run()
	go negotiator.Run()
	stopWatch := make(chan struct{})
	defer close(stopWatch)
	go func() {
		select {
		case <-stable.Done():
			cancel()
		case <-stopWatch:
		}
	}()
	send := func(status protocol.FileUploadStatus) error {
		status.Direct = stable.Direct()
		status.DirectBytes = stable.Counters().ReceivedDirect
		body, _ := json.Marshal(status)
		return relay.Send(tunnel.Packet{Kind: tunnel.UploadStatus, Body: body})
	}
	var loaded int64
	lastProgress := time.Now()
	checksum, err := upload.Receive(stable, dst, req.Upload.Size, func(n int64) {
		loaded = n
		if time.Since(lastProgress) >= 100*time.Millisecond || n == req.Upload.Size {
			lastProgress = time.Now()
			if send(protocol.FileUploadStatus{Type: "progress", Loaded: n}) != nil {
				_ = stream.Close()
			}
		}
	})
	if err == nil {
		err = dst.Close()
	}
	if err == nil {
		err = dst.commit()
	}
	status := protocol.FileUploadStatus{Type: "complete", Loaded: loaded, SHA256: checksum}
	if err != nil {
		status.Type = "error"
		status.Error = err.Error()
		status.SHA256 = ""
	}
	_ = send(status)
}
