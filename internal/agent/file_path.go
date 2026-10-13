package agent

import (
	"path/filepath"
	"runtime"
	"strings"

	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
)

// SFTP uses /C:/... on Windows; native filesystem calls require C:\... .
// Never apply this conversion to a delegated SSH/SFTP target's paths.
func localFilePath(remote string) string {
	if runtime.GOOS == "windows" && strings.HasPrefix(remote, "/") {
		drive := strings.TrimPrefix(remote, "/")
		if len(drive) >= 2 && filepath.VolumeName(drive) != "" && (len(drive) == 2 || drive[2] == '/' || drive[2] == '\\') {
			remote = drive
			if len(remote) == 2 {
				remote += "/"
			}
		}
	}
	return filepath.FromSlash(remote)
}

func (c *Client) localFileRequest(req protocol.StreamRequest) protocol.StreamRequest {
	if len(req.TunnelHops) != 0 {
		return req
	}
	resolve := func(remote string) string {
		if remote == "" {
			return ""
		}
		native := localFilePath(remote)
		if !filepath.IsAbs(native) {
			native = filepath.Join(c.cfg.Root, native)
		}
		return native
	}
	if req.Upload != nil {
		upload := *req.Upload
		upload.Path = resolve(upload.Path)
		req.Upload = &upload
	}
	if req.Download != nil {
		download := *req.Download
		download.Path = resolve(download.Path)
		req.Download = &download
	}
	return req
}
