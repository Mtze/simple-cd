// Package runner starts executor runs and watches them. Kubernetes runs each
// deployment as a Job; the process runner is for local development only.
package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/Mtze/simple-cd/internal/executor"
)

// ErrNotFound means the job does not exist (anymore).
var ErrNotFound = errors.New("job not found")

// ErrExists means a job with this name was already started, e.g. before a worker restart.
var ErrExists = errors.New("job already exists")

type Job struct {
	Name    string
	Spec    executor.Spec
	Timeout time.Duration
}

type State struct {
	Done      bool
	Succeeded bool
	Message   string
}

type Runner interface {
	Start(ctx context.Context, j Job) error
	State(ctx context.Context, name string) (State, error)
	// Logs returns the executor output. With follow it streams until the run ends.
	Logs(ctx context.Context, name string, follow bool) (io.ReadCloser, error)
	Delete(ctx context.Context, name string) error
}

// JobName is stable per deployment, so a restarted worker finds its jobs again.
func JobName(deploymentID int64) string { return fmt.Sprintf("simple-cd-%d", deploymentID) }
