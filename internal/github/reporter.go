package github

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/Mtze/simple-cd/internal/store"
)

// Reporter shows deployments on GitHub (environments and PR timeline).
// Everything here is best effort: GitHub being down never blocks a deploy.
type Reporter struct {
	App     *App
	BaseURL string // public simple-cd URL for log links

	mu            sync.Mutex
	installations map[string]int64
}

func (r *Reporter) client(ctx context.Context, repo string) (*Client, error) {
	r.mu.Lock()
	id, ok := r.installations[repo]
	r.mu.Unlock()
	if !ok {
		var err error
		if id, err = r.App.InstallationFor(ctx, repo); err != nil {
			return nil, err
		}
		r.mu.Lock()
		if r.installations == nil {
			r.installations = map[string]int64{}
		}
		r.installations[repo] = id
		r.mu.Unlock()
	}
	return r.App.Client(id), nil
}

// Environment names the GitHub environment: the target, or target/pr-N for previews.
func Environment(target, slot string) string {
	if slot == "" {
		return target
	}
	return target + "/pr-" + slot
}

func (r *Reporter) logURL(d *store.Deployment) string {
	return fmt.Sprintf("%s/deployments/%d", r.BaseURL, d.ID)
}

// Started creates the GitHub deployment and marks it in progress.
func (r *Reporter) Started(ctx context.Context, a *store.Artifact, d *store.Deployment) (int64, error) {
	c, err := r.client(ctx, a.Repo)
	if err != nil {
		return 0, err
	}
	id, err := c.CreateDeployment(ctx, a.Repo, DeploymentRequest{
		Ref: a.SHA, Environment: Environment(d.TargetID, d.Slot), Transient: d.Slot != "",
		Description: "simple-cd deployment " + fmt.Sprint(d.ID) + " by " + d.Actor,
	})
	if err != nil {
		return 0, err
	}
	return id, c.SetDeploymentStatus(ctx, a.Repo, id, "in_progress", "Deploying", r.logURL(d))
}

// Finished sets the final GitHub state.
func (r *Reporter) Finished(ctx context.Context, repo string, d *store.Deployment, ghID int64, state, description string) {
	c, err := r.client(ctx, repo)
	if err == nil {
		err = c.SetDeploymentStatus(ctx, repo, ghID, state, description, r.logURL(d))
	}
	if err != nil {
		slog.Warn("github deployment status", "deployment", d.ID, "err", err)
	}
}

// AutoDeploySkipped records a skipped auto-deploy as a failed GitHub deployment
// so the developer sees why nothing happened.
func (r *Reporter) AutoDeploySkipped(ctx context.Context, a *store.Artifact, target, reason string) {
	slot := ""
	if a.PR != nil {
		slot = fmt.Sprint(*a.PR)
	}
	c, err := r.client(ctx, a.Repo)
	if err != nil {
		slog.Warn("github skip report", "err", err)
		return
	}
	id, err := c.CreateDeployment(ctx, a.Repo, DeploymentRequest{
		Ref: a.SHA, Environment: Environment(target, slot), Transient: slot != "", Description: "simple-cd auto-deploy",
	})
	if err == nil {
		err = c.SetDeploymentStatus(ctx, a.Repo, id, "error", "Skipped: "+reason, r.BaseURL)
	}
	if err != nil {
		slog.Warn("github skip report", "err", err)
	}
}
