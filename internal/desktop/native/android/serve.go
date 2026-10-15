package android

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/qinyongliang/gosshd-bastion/internal/desktop/contract"
)

// Serve owns a private JSON-lines stream. Closing/canceling the stream stops
// idle reads and any active platform command; all native refs are session-local.
func Serve(ctx context.Context, in io.Reader, out io.Writer, h *Helper) error {
	if h == nil {
		h = NewHelper()
	}
	stop := context.AfterFunc(ctx, func() {
		if c, ok := in.(io.Closer); ok {
			c.Close()
		}
		if c, ok := out.(io.Closer); ok {
			c.Close()
		}
	})
	defer stop()
	defer func() { h.gate.Lock(); h.observed = nil; h.gate.Unlock() }()
	scan := bufio.NewScanner(in)
	scan.Buffer(make([]byte, 65536), contract.MaxMessage)
	for scan.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var req contract.Request
		res := contract.Response{}
		err := json.Unmarshal(scan.Bytes(), &req)
		res.ID = req.ID
		if err == nil {
			err = contract.Validate(req)
		}
		if err != nil {
			h.gate.Lock()
			h.observed = nil
			h.gate.Unlock()
			res.Error = err.Error()
			var failure *contract.ProtocolError
			if errors.As(err, &failure) {
				res.ErrorCode = failure.Code
			}
		} else {
			opCtx, cancel := context.WithTimeout(ctx, 30e9)
			value, dispatchErr := h.Dispatch(opCtx, req.Method, req.Params)
			cancel()
			if dispatchErr != nil {
				res.Error = dispatchErr.Error()
				var failure *contract.ProtocolError
				if errors.As(dispatchErr, &failure) {
					res.ErrorCode = failure.Code
				}
			} else {
				res.Result, err = json.Marshal(value)
				res.OK = err == nil
				if err != nil {
					res.Error = "cannot encode desktop result"
				}
			}
		}
		data, err := json.Marshal(res)
		if err != nil {
			return err
		}
		if len(data) >= contract.MaxMessage {
			return errors.New("desktop response exceeds limit")
		}
		if _, err = out.Write(append(data, '\n')); err != nil {
			return err
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return scan.Err()
}
