package desktop

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

type Config struct {
	Enabled     bool
	HelperPath  string
	IdleTimeout time.Duration
}

// CommandFactory is injectable for tests and for independently packaged helpers.
type CommandFactory func(context.Context, Config) (*exec.Cmd, func(), error)

type process struct {
	closeNative func()
	cmd         *exec.Cmd
	in          io.WriteCloser
	out         *bufio.Scanner
	cleanup     func()
	once        sync.Once
	done        chan error
}

func startProcess(ctx context.Context, cfg Config, factory CommandFactory) (*process, error) {
	cmd, cleanup, err := factory(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if cleanup == nil {
		cleanup = func() {}
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		cleanup()
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		in.Close()
		cleanup()
		return nil, err
	}
	// Never forward helper diagnostics or potentially sensitive desktop text into logs.
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		in.Close()
		cleanup()
		return nil, err
	}
	scan := bufio.NewScanner(out)
	scan.Buffer(make([]byte, 65536), MaxMessage)
	p := &process{cmd: cmd, in: in, out: scan, cleanup: cleanup, done: make(chan error, 1)}
	go func() { p.done <- cmd.Wait() }()
	return p, nil
}
func (p *process) close() {
	p.once.Do(func() {
		if p.closeNative != nil {
			p.closeNative()
			return
		}
		p.in.Close()
		if p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
		<-p.done
		p.cleanup()
	})
}
func (p *process) call(req Request) (Response, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return Response{}, err
	}
	if len(b) > MaxMessage {
		return Response{}, errors.New("desktop request too large")
	}
	if _, err = p.in.Write(append(b, '\n')); err != nil {
		return Response{}, err
	}
	if !p.out.Scan() {
		if err = p.out.Err(); err == nil {
			err = io.ErrUnexpectedEOF
		}
		return Response{}, err
	}
	var res Response
	if err = json.Unmarshal(p.out.Bytes(), &res); err != nil {
		return res, errors.New("invalid desktop helper response")
	}
	if res.ID != req.ID {
		return res, errors.New("desktop helper response id mismatch")
	}
	return res, nil
}

// Serve bridges a single actor-owned stream to a private helper subprocess.
// It never accepts shell commands or arbitrary helper paths from remote callers.
func Serve(ctx context.Context, rw io.ReadWriteCloser, cfg Config) error {
	return serveWithStarter(ctx, rw, cfg, startNativeProcess)
}
func ServeWithFactory(ctx context.Context, rw io.ReadWriteCloser, cfg Config, factory CommandFactory) error {
	return serveWithStarter(ctx, rw, cfg, func(ctx context.Context, cfg Config) (*process, error) { return startProcess(ctx, cfg, factory) })
}
func serveWithStarter(ctx context.Context, rw io.ReadWriteCloser, cfg Config, start func(context.Context, Config) (*process, error)) error {
	defer rw.Close()
	if !cfg.Enabled {
		return writeResponse(rw, Response{ID: "init", Error: "desktop capability is disabled on this Agent"})
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { rw.Close() })
	defer stop()
	requests := make(chan Request, 1)
	readError := make(chan error, 1)
	go func() {
		defer close(requests)
		defer cancel() // A peer disconnect must also stop a helper busy in an action.
		scan := bufio.NewScanner(rw)
		scan.Buffer(make([]byte, 65536), MaxMessage)
		for scan.Scan() {
			var req Request
			if err := json.Unmarshal(scan.Bytes(), &req); err != nil {
				readError <- errors.New("invalid desktop request")
				return
			}
			select {
			case requests <- req:
			case <-ctx.Done():
				return
			}
		}
		readError <- scan.Err()
	}()
	p, err := start(ctx, cfg)
	if err != nil {
		return writeResponse(rw, Response{ID: "init", Error: "desktop helper unavailable: " + err.Error()})
	}
	defer p.close()
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 5 * time.Minute
	}
	timer := time.AfterFunc(cfg.IdleTimeout, func() { cancel(); rw.Close() })
	defer timer.Stop()
	if err = writeResponse(rw, Response{ID: "init", OK: true}); err != nil {
		return err
	}

	for req := range requests {
		timer.Reset(cfg.IdleTimeout)
		if err = Validate(req); err != nil {
			if err = writeResponse(rw, Response{ID: req.ID, Error: err.Error()}); err != nil {
				return err
			}
			continue
		}
		operation := time.AfterFunc(30*time.Second, func() { cancel(); rw.Close() })
		res, callError := p.call(req)
		operation.Stop()
		if callError != nil {
			return fmt.Errorf("desktop helper failed: %w", callError)
		}
		if err = writeResponse(rw, res); err != nil {
			return err
		}
	}
	select {
	case err := <-readError:
		return err
	default:
		return ctx.Err()
	}

}
func writeResponse(w io.Writer, res Response) error {
	b, err := json.Marshal(res)
	if err != nil {
		return err
	}
	if len(b) > MaxMessage {
		return errors.New("desktop response too large")
	}
	_, err = w.Write(append(b, '\n'))
	return err
}
