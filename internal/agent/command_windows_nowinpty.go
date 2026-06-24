//go:build windows && !amd64 && !386

package agent

import (
	"bufio"
	"errors"
	"io"

	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
)

func (c *Client) handleWinPTYShell(stream io.ReadWriteCloser, reader *bufio.Reader, req protocol.StreamRequest) error {
	return errors.New("winpty is unavailable on this Windows architecture")
}
