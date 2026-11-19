package runner

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// Process runs the executor binary as a local child process.
// shortcut: state lives in memory, so it only works with a single replica; it is meant for local development.
type Process struct {
	Binary string            // path to simple-cd-executor
	Env    map[string]string // extra environment, e.g. VAULT_ADDR and VAULT_TOKEN
	Dir    string            // where log files go

	mu   sync.Mutex
	runs map[string]*run
}

type run struct {
	log    string
	done   chan struct{}
	state  State
	cancel context.CancelFunc
}

func (p *Process) Start(_ context.Context, j Job) error {
	spec, err := json.Marshal(j.Spec)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(p.Dir, 0o700); err != nil {
		return err
	}
	logPath := filepath.Join(p.Dir, j.Name+".log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	secrets, err := os.MkdirTemp("", j.Name+"-secrets-")
	if err != nil {
		f.Close()
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), j.Timeout)
	cmd := exec.CommandContext(ctx, p.Binary)
	cmd.Env = append(os.Environ(), "SIMPLECD_SPEC="+string(spec), "SIMPLECD_SECRETS_DIR="+secrets)
	for k, v := range p.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Stdout, cmd.Stderr = f, f
	if err := cmd.Start(); err != nil {
		cancel()
		f.Close()
		return err
	}
	r := &run{log: logPath, done: make(chan struct{}), cancel: cancel}
	p.mu.Lock()
	if p.runs == nil {
		p.runs = map[string]*run{}
	}
	p.runs[j.Name] = r
	p.mu.Unlock()
	go func() {
		err := cmd.Wait()
		f.Close()
		os.RemoveAll(secrets)
		cancel()
		p.mu.Lock()
		r.state = State{Done: true, Succeeded: err == nil}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			r.state.Message = "DeadlineExceeded"
		} else if err != nil {
			r.state.Message = err.Error()
		}
		p.mu.Unlock()
		close(r.done)
	}()
	return nil
}

func (p *Process) get(name string) (*run, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	r, ok := p.runs[name]
	if !ok {
		return nil, ErrNotFound
	}
	return r, nil
}

func (p *Process) State(_ context.Context, name string) (State, error) {
	r, err := p.get(name)
	if err != nil {
		return State{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return r.state, nil
}

func (p *Process) Logs(ctx context.Context, name string, follow bool) (io.ReadCloser, error) {
	r, err := p.get(name)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(r.log)
	if err != nil {
		return nil, err
	}
	if !follow {
		return f, nil
	}
	pr, pw := io.Pipe()
	go func() {
		defer f.Close()
		buf := make([]byte, 32<<10)
		for {
			n, err := f.Read(buf)
			if n > 0 {
				if _, werr := pw.Write(buf[:n]); werr != nil {
					return
				}
			}
			if err == io.EOF {
				select {
				case <-r.done:
					// Drain what was written after the last read, then stop.
					rest, _ := io.ReadAll(f)
					pw.Write(rest)
					pw.Close()
					return
				case <-ctx.Done():
					pw.CloseWithError(ctx.Err())
					return
				case <-time.After(200 * time.Millisecond):
				}
			} else if err != nil {
				pw.CloseWithError(err)
				return
			}
		}
	}()
	return pr, nil
}

func (p *Process) Delete(_ context.Context, name string) error {
	r, err := p.get(name)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	r.cancel()
	return nil
}
