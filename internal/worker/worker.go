// Package worker drives deployments in the background: it starts pending
// runs, watches running ones, processes the auto-deploy waiting places,
// tears down expired previews and applies retention. Exactly one replica
// does this at a time, chosen with a Postgres advisory lock.
package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/Mtze/simple-cd/internal/config"
	"github.com/Mtze/simple-cd/internal/core"
	"github.com/Mtze/simple-cd/internal/runner"
	"github.com/Mtze/simple-cd/internal/store"
)

const (
	leaderLockID = 7_341_002_002
	maxLogBytes  = 2 << 20
	// Kubernetes enforces the job timeout; this grace covers a stuck API or node.
	timeoutGrace   = 2 * time.Minute
	teardownRetry  = 10 * time.Minute
	retentionEvery = 10 * time.Minute
)

// Reporter mirrors deployments to GitHub. Optional.
type Reporter interface {
	Started(ctx context.Context, a *store.Artifact, d *store.Deployment) (int64, error)
	Finished(ctx context.Context, repo string, d *store.Deployment, ghID int64, state, description string)
}

type Worker struct {
	Store    *store.Store
	Config   *config.Holder
	Core     *core.Service
	Runner   runner.Runner
	Reporter Reporter
	// GitURL returns the clone URL for Ansible playbooks from the app repo.
	GitURL   func(repo string) string
	Interval time.Duration
	Now      func() time.Time

	lastRetention time.Time
}

func (w *Worker) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

// Run competes for leadership and ticks while leader, until ctx ends.
func (w *Worker) Run(ctx context.Context) {
	if w.Interval == 0 {
		w.Interval = 2 * time.Second
	}
	for ctx.Err() == nil {
		w.lead(ctx)
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
		}
	}
}

func (w *Worker) lead(ctx context.Context) {
	conn, err := w.Store.Pool.Acquire(ctx)
	if err != nil {
		return
	}
	defer conn.Release()
	var leader bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, leaderLockID).Scan(&leader); err != nil || !leader {
		return
	}
	// The lock is tied to this connection; releasing the connection without
	// unlocking would hand a locked session back to the pool.
	defer conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, leaderLockID)
	slog.Info("worker is leader")
	t := time.NewTicker(w.Interval)
	defer t.Stop()
	for {
		if err := conn.Ping(ctx); err != nil {
			slog.Warn("worker lost its database connection", "err", err)
			return
		}
		if err := w.Tick(ctx); err != nil && ctx.Err() == nil {
			slog.Error("worker tick", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Tick does one pass of all background work.
func (w *Worker) Tick(ctx context.Context) error {
	errs := []error{w.startPending(ctx), w.checkRunning(ctx), w.Core.ProcessQueue(ctx), w.Core.RetryPreviewTeardowns(ctx)}
	if w.now().Sub(w.lastRetention) > retentionEvery {
		errs = append(errs, w.retention(ctx))
		w.lastRetention = w.now()
	}
	return errors.Join(errs...)
}

func (w *Worker) startPending(ctx context.Context) error {
	pending, err := w.Store.DeploymentsByStatus(ctx, store.StatusPending)
	if err != nil {
		return err
	}
	cfg := w.Config.Get()
	for _, d := range pending {
		if err := w.start(ctx, cfg, d); err != nil {
			slog.Error("start deployment", "deployment", d.ID, "err", err)
			w.finish(ctx, d, nil, store.StatusFailed, "", err.Error())
		}
	}
	return nil
}

func (w *Worker) start(ctx context.Context, cfg *config.Config, d *store.Deployment) error {
	t, ok := cfg.Target(d.TargetID)
	if !ok {
		return fmt.Errorf("target %s no longer exists in the configuration", d.TargetID)
	}
	var a *store.Artifact
	if d.ArtifactID != nil {
		var err error
		if a, err = w.Store.Artifact(ctx, *d.ArtifactID); err != nil {
			return fmt.Errorf("load artifact: %w", err)
		}
	}
	repoURL := ""
	if w.GitURL != nil {
		repoURL = w.GitURL(t.Project)
	}
	spec, err := core.BuildSpec(cfg, t, a, d, repoURL)
	if err != nil {
		return err
	}
	name := runner.JobName(d.ID)
	err = w.Runner.Start(ctx, runner.Job{Name: name, Spec: spec, Timeout: cfg.JobTimeout(t)})
	if err != nil && !errors.Is(err, runner.ErrExists) {
		return err
	}
	if err := w.Store.MarkRunning(ctx, d.ID, name); err != nil {
		return err
	}
	w.Store.Audit(ctx, d.Actor, "deploy.started", d.TargetID, map[string]any{"deployment": d.ID, "kind": d.Kind})
	if w.Reporter != nil && a != nil && d.Kind == store.KindDeploy {
		if ghID, err := w.Reporter.Started(ctx, a, d); err != nil {
			slog.Warn("github deployment", "deployment", d.ID, "err", err)
		} else {
			w.Store.SetGitHubDeploymentID(ctx, d.ID, ghID)
		}
	}
	return nil
}

func (w *Worker) checkRunning(ctx context.Context) error {
	running, err := w.Store.DeploymentsByStatus(ctx, store.StatusRunning)
	if err != nil {
		return err
	}
	cfg := w.Config.Get()
	for _, d := range running {
		st, err := w.Runner.State(ctx, d.JobName)
		if errors.Is(err, runner.ErrNotFound) {
			w.finish(ctx, d, cfg, store.StatusFailed, w.logs(ctx, d), "the executor job disappeared")
			continue
		}
		if err != nil {
			slog.Error("job state", "deployment", d.ID, "err", err)
			continue
		}
		if !st.Done {
			timeout := cfg.Defaults.JobTimeout.D()
			if t, ok := cfg.Target(d.TargetID); ok {
				timeout = cfg.JobTimeout(t)
			}
			if d.StartedAt != nil && w.now().After(d.StartedAt.Add(timeout+timeoutGrace)) {
				logs := w.logs(ctx, d)
				w.Runner.Delete(ctx, d.JobName)
				w.finish(ctx, d, cfg, store.StatusTimedOut, logs, "timed out after "+timeout.String())
			}
			continue
		}
		status, msg := store.StatusSucceeded, ""
		if !st.Succeeded {
			status, msg = store.StatusFailed, st.Message
			if strings.Contains(st.Message, "DeadlineExceeded") {
				status = store.StatusTimedOut
			}
		}
		w.finish(ctx, d, cfg, status, w.logs(ctx, d), msg)
	}
	return nil
}

func (w *Worker) logs(ctx context.Context, d *store.Deployment) string {
	if d.JobName == "" {
		return ""
	}
	rc, err := w.Runner.Logs(ctx, d.JobName, false)
	if err != nil {
		return "(logs unavailable: " + err.Error() + ")\n"
	}
	defer rc.Close()
	data, _ := io.ReadAll(io.LimitReader(rc, maxLogBytes+1))
	if len(data) > maxLogBytes {
		return string(data[:maxLogBytes]) + "\n(log truncated at 2 MiB)\n"
	}
	return string(data)
}

// finish stores the result and does the bookkeeping that depends on it.
func (w *Worker) finish(ctx context.Context, d *store.Deployment, cfg *config.Config, status, logs, msg string) {
	if err := w.Store.Finish(ctx, d.ID, status, logs, msg); err != nil {
		slog.Error("finish deployment", "deployment", d.ID, "err", err)
		return
	}
	w.Store.Audit(ctx, d.Actor, "deploy.finished", d.TargetID, map[string]any{"deployment": d.ID, "status": status, "kind": d.Kind, "error": msg})

	if d.Slot != "" && cfg != nil {
		w.previewBookkeeping(ctx, cfg, d, status)
	}
	if w.Reporter == nil {
		return
	}
	t, _ := w.Config.Get().Target(d.TargetID)
	if d.Kind == store.KindDeploy && d.GitHubDeploymentID != nil {
		state := map[string]string{store.StatusSucceeded: "success", store.StatusFailed: "failure", store.StatusTimedOut: "error"}[status]
		w.Reporter.Finished(ctx, t.Project, d, *d.GitHubDeploymentID, state, strings.TrimSpace("Deployment "+status+" "+msg))
	}
	if d.Kind == store.KindTeardown && status == store.StatusSucceeded {
		if last, err := w.Store.LastSucceeded(ctx, d.TargetID, d.Slot); err == nil && last.GitHubDeploymentID != nil {
			w.Reporter.Finished(ctx, t.Project, last, *last.GitHubDeploymentID, "inactive", "Preview removed")
		}
	}
}

func (w *Worker) previewBookkeeping(ctx context.Context, cfg *config.Config, d *store.Deployment, status string) {
	t, ok := cfg.Target(d.TargetID)
	pr, err := strconv.Atoi(d.Slot)
	if !ok || err != nil {
		return
	}
	p, err := w.Store.LivePreview(ctx, d.TargetID, pr)
	if err != nil {
		return
	}
	switch {
	case d.Kind == store.KindTeardown && status == store.StatusSucceeded:
		w.Store.SetPreviewStatus(ctx, p.ID, store.PreviewDeleted)
	case d.Kind == store.KindTeardown:
		// Try again later instead of leaving the namespace behind.
		w.Store.ExpirePreview(ctx, p.ID, w.now().Add(teardownRetry))
	case status == store.StatusSucceeded && p.Status == store.PreviewActive:
		w.Store.TouchPreview(ctx, p.Repo, pr, t.ID, p.Namespace, w.now().Add(cfg.PreviewTTL(t)))
	}
}

func (w *Worker) retention(ctx context.Context) error {
	cfg := w.Config.Get()
	now := w.now()
	var errs []error
	n, err := w.Store.DeleteOldArtifacts(ctx, now.Add(-cfg.Defaults.ArtifactRetention.D()), cfg.Defaults.KeepBranchArtifacts)
	errs = append(errs, err)
	l, err := w.Store.DeleteOldLogs(ctx, now.Add(-cfg.Defaults.LogRetention.D()))
	errs = append(errs, err)
	a, err := w.Store.DeleteOldAudit(ctx, now.Add(-cfg.Defaults.AuditRetention.D()))
	errs = append(errs, err, w.Store.DeleteExpiredAuth(ctx))
	if n+l+a > 0 {
		slog.Info("retention", "artifacts", n, "logs", l, "auditEvents", a)
	}
	return errors.Join(errs...)
}
