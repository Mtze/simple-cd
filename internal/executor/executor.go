package executor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type executor struct {
	out        io.Writer
	getenv     func(string) string
	vault      *vault
	secretsDir string
	env        []string // added to the environment of every child process
}

// Run executes one deploy or teardown. getenv is usually os.Getenv; progress
// and tool output go to out.
func Run(ctx context.Context, spec Spec, getenv func(string) string, out io.Writer) error {
	x := &executor{out: out, getenv: getenv, secretsDir: envOr(getenv, "SIMPLECD_SECRETS_DIR", "/secrets")}
	x.logf("==> Deployment %d: %s %s for target %s (repo %s, sha %s)", spec.DeploymentID, spec.Type, spec.Kind, spec.TargetID, spec.Repo, spec.SHA)

	var step func(context.Context) error
	switch {
	case spec.Type == "helm" && spec.Helm != nil && spec.Kind == "deploy":
		step = func(ctx context.Context) error { return x.helmDeploy(ctx, spec.Helm) }
	case spec.Type == "helm" && spec.Helm != nil && spec.Kind == "teardown":
		step = func(ctx context.Context) error { return x.helmTeardown(ctx, spec.Helm) }
	case spec.Type == "compose" && spec.Compose != nil && spec.Kind == "deploy":
		step = func(ctx context.Context) error { return x.composeDeploy(ctx, spec.Compose) }
	case spec.Type == "ansible" && spec.Ansible != nil && spec.Kind == "deploy":
		step = func(ctx context.Context) error { return x.ansibleDeploy(ctx, spec.Ansible) }
	default:
		return fmt.Errorf("unsupported spec: type %q, kind %q (or its %s section is missing)", spec.Type, spec.Kind, spec.Type)
	}

	if err := os.MkdirAll(x.secretsDir, 0o700); err != nil {
		return fmt.Errorf("secrets dir: %w", err)
	}
	x.logf("==> Logging in to Vault")
	v, err := vaultLogin(ctx, getenv)
	if err != nil {
		return err
	}
	x.vault = v
	return step(ctx)
}

func (x *executor) logf(format string, args ...any) {
	fmt.Fprintf(x.out, format+"\n", args...)
}

// writeSecret stores secret material in the in-memory secrets dir.
func (x *executor) writeSecret(name string, data []byte) (string, error) {
	p := filepath.Join(x.secretsDir, name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		return "", err
	}
	// WriteFile keeps the mode of an existing file, so enforce it.
	return p, os.Chmod(p, 0o600)
}

// run streams the child's output to out. stdin may be empty.
func (x *executor) run(ctx context.Context, dir, stdin, name string, args ...string) error {
	return x.exec(ctx, dir, stdin, x.out, name, args...)
}

// output returns the child's stdout and streams only its stderr.
func (x *executor) output(ctx context.Context, name string, args ...string) ([]byte, error) {
	var buf bytes.Buffer
	err := x.exec(ctx, "", "", &buf, name, args...)
	return buf.Bytes(), err
}

func (x *executor) exec(ctx context.Context, dir, stdin string, stdout io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), x.env...)
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Stdout = stdout
	cmd.Stderr = x.out
	// SIGTERM first so helm and ansible get a chance to clean up, then kill.
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 15 * time.Second
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%s %s: cancelled: %w", name, firstArg(args), ctx.Err())
		}
		return fmt.Errorf("%s %s: %w", name, firstArg(args), err)
	}
	return nil
}

func firstArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

// shellQuote quotes s for a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
