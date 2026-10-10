package agent

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/sftp"
	"github.com/qinyongliang/gosshd-bastion/internal/filetransfer"
	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
)

type uploadDestination struct {
	io.WriteCloser
	commit func() error
	abort  func()
}

func openUploadDestination(ctx context.Context, req protocol.StreamRequest, shared ...*sftp.Client) (*uploadDestination, error) {
	if req.Upload == nil || req.Upload.Path == "" || req.Upload.Size < 0 || req.Upload.Size > filetransfer.MaxSize {
		return nil, errors.New("invalid upload request")
	}
	dest := req.Upload.Path
	client, cleanup, err := openFileSFTP(ctx, req.TunnelHops, shared...)
	if err != nil {
		return nil, err
	}
	if client != nil {
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
	stable, relay, cleanup := startFileTransfer(ctx, cancel, stream, reader, req)
	defer cleanup()
	send := func(status protocol.FileTransferStatus) error {
		return sendFileStatus(stable, relay, status, false)
	}
	_ = send(receiveFileUpload(stable, dst, req.Upload.Size, send))
}

func receiveFileUpload(src io.Reader, dst *uploadDestination, size int64, send func(protocol.FileTransferStatus) error) protocol.FileTransferStatus {
	var loaded int64
	lastProgress := time.Now()
	checksum, err := filetransfer.Receive(src, dst, size, func(n int64) {
		loaded = n
		if time.Since(lastProgress) >= 100*time.Millisecond || n == size {
			lastProgress = time.Now()
			if send(protocol.FileTransferStatus{Type: "progress", Loaded: n}) != nil {
				_ = dst.Close()
			}
		}
	})
	if err == nil {
		err = dst.Close()
	}
	if err == nil {
		err = dst.commit()
	}
	status := protocol.FileTransferStatus{Type: "complete", Loaded: loaded, SHA256: checksum}
	if err != nil {
		status.Type = "error"
		status.Error = err.Error()
		status.SHA256 = ""
	}
	return status
}
