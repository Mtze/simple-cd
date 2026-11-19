// Package ingest turns GitHub webhooks into stored artifacts: it rejects
// forks and unknown repos, downloads deploy.json, derives the source from
// GitHub, snapshots the repo files a deploy needs and checks the registry.
package ingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/Mtze/simple-cd/internal/config"
	"github.com/Mtze/simple-cd/internal/core"
	"github.com/Mtze/simple-cd/internal/github"
	"github.com/Mtze/simple-cd/internal/repoconfig"
	"github.com/Mtze/simple-cd/internal/store"
)

// RepoAPI is the part of the GitHub client ingest needs.
type RepoAPI interface {
	DownloadArtifactFile(ctx context.Context, repo string, runID int64, artifact, file string) ([]byte, error)
	File(ctx context.Context, repo, path, ref string) ([]byte, error)
	PullRequestsForCommit(ctx context.Context, repo, sha string) ([]int, error)
	IsTag(ctx context.Context, repo, name string) (bool, error)
}

type Ingester struct {
	Config   *config.Holder
	Store    *store.Store
	Core     *core.Service
	Registry core.RegistryChecker
	GitHub   func(installation int64) RepoAPI
}

// WorkflowRun handles a workflow_run webhook.
func (i *Ingester) WorkflowRun(ctx context.Context, ev github.WorkflowRunEvent) error {
	run := ev.WorkflowRun
	repo := ev.Repository.FullName
	if ev.Action != "completed" || run.Conclusion != "success" {
		return nil
	}
	repo, known := i.Config.Get().Project(repo)
	if !known {
		slog.Info("ignoring workflow run of unknown repository", "repo", ev.Repository.FullName)
		return nil
	}
	if !strings.EqualFold(run.HeadRepository.FullName, repo) {
		return i.Store.Audit(ctx, "github", "webhook.fork_rejected", repo, map[string]any{
			"headRepository": run.HeadRepository.FullName, "run": run.ID, "sha": run.HeadSHA,
		})
	}
	gh := i.GitHub(ev.Installation.ID)
	raw, err := gh.DownloadArtifactFile(ctx, repo, run.ID, repoconfig.ArtifactName, "deploy.json")
	if errors.Is(err, github.ErrNotFound) {
		return nil // this workflow does not publish deployable artifacts
	}
	if err != nil {
		return fmt.Errorf("download deploy.json: %w", err)
	}

	a := &store.Artifact{
		Repo: repo, SHA: run.HeadSHA, RunID: run.ID, RunURL: run.HTMLURL, Workflow: run.Path,
		TriggeringActor: run.TriggeringActor.Login, RefName: run.HeadBranch,
	}
	if err := i.source(ctx, gh, ev, a); err != nil {
		return err
	}
	if m, err := repoconfig.ParseManifest(raw); err != nil {
		a.Reasons = append(a.Reasons, err.Error())
		a.Manifest = repoconfig.Manifest{Version: 1}
	} else {
		a.Manifest = *m
	}
	if err := i.snapshot(ctx, gh, a); err != nil {
		return err
	}
	i.checkRegistry(ctx, a)

	stored, applied, err := i.Store.UpsertArtifact(ctx, a)
	if err != nil {
		return err
	}
	if !applied {
		return nil // an older run arrived after a newer one
	}
	slog.Info("artifact stored", "repo", repo, "sha", a.SHA, "source", a.Source, "reasons", len(stored.Reasons))
	i.Core.AutoDeploy(ctx, stored)
	return nil
}

// source derives pr/branch/release from GitHub, never from deploy.json.
func (i *Ingester) source(ctx context.Context, gh RepoAPI, ev github.WorkflowRunEvent, a *store.Artifact) error {
	run := ev.WorkflowRun
	switch run.Event {
	case "pull_request", "pull_request_target":
		a.Source = config.SourcePR
		if len(run.PullRequests) > 0 {
			n := run.PullRequests[0].Number
			a.PR = &n
			return nil
		}
		prs, err := gh.PullRequestsForCommit(ctx, a.Repo, a.SHA)
		if err != nil {
			return fmt.Errorf("find pull request: %w", err)
		}
		if len(prs) == 0 {
			return fmt.Errorf("no open pull request for %s", a.SHA)
		}
		a.PR = &prs[0]
	case "release":
		a.Source = config.SourceRelease
	default:
		a.Source = config.SourceBranch
		if run.HeadBranch != "" {
			tag, err := gh.IsTag(ctx, a.Repo, run.HeadBranch)
			if err != nil {
				return fmt.Errorf("check tag: %w", err)
			}
			if tag {
				a.Source = config.SourceRelease
			}
		}
	}
	return nil
}

// snapshot reads .cd.yaml and the files it points to at the artifact's SHA.
func (i *Ingester) snapshot(ctx context.Context, gh RepoAPI, a *store.Artifact) error {
	a.Files = map[string]*string{}
	raw, err := gh.File(ctx, a.Repo, repoconfig.FileName, a.SHA)
	if errors.Is(err, github.ErrNotFound) {
		a.Reasons = append(a.Reasons, repoconfig.FileName+" is missing at this commit")
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", repoconfig.FileName, err)
	}
	rc, err := repoconfig.ParseRepoConfig(raw)
	if err != nil {
		a.Reasons = append(a.Reasons, err.Error())
		return nil
	}
	a.RepoConfig = rc

	var paths []string
	if rc.Helm != nil {
		for _, t := range rc.Helm.Targets {
			if t.Values != "" {
				paths = append(paths, t.Values)
			}
		}
	}
	if rc.Compose != nil {
		paths = append(paths, rc.Compose.Path)
	}
	if rc.Ansible != nil {
		paths = append(paths, rc.Ansible.Playbook)
	}
	for _, p := range paths {
		if _, done := a.Files[p]; done {
			continue
		}
		content, err := gh.File(ctx, a.Repo, p, a.SHA)
		switch {
		case errors.Is(err, github.ErrNotFound):
			a.Files[p] = nil
		case err != nil:
			return fmt.Errorf("read %s: %w", p, err)
		default:
			s := string(content)
			a.Files[p] = &s
		}
	}
	return nil
}

// checkRegistry records missing images and charts. A registry that is down
// does not mark the artifact incomplete; the check before deploy catches it.
func (i *Ingester) checkRegistry(ctx context.Context, a *store.Artifact) {
	if i.Registry == nil {
		return
	}
	check := func(repo, ref, what string) {
		ok, err := i.Registry.Exists(ctx, repo, ref)
		if err != nil {
			slog.Warn("registry check failed", "repo", repo, "ref", ref, "err", err)
			return
		}
		if !ok {
			a.Reasons = append(a.Reasons, fmt.Sprintf("%s missing in registry: %s@%s", what, repo, ref))
		}
	}
	for _, img := range a.Manifest.Images {
		if r, err := repoconfig.ParseImageRef(img.Ref); err == nil {
			check(r.Repository, r.Digest, "image")
		}
	}
	if a.RepoConfig != nil && a.RepoConfig.Helm != nil && a.Manifest.Helm != nil {
		check(repoconfig.ChartRef(a.RepoConfig.Helm.Chart), a.Manifest.Helm.Version, "chart")
	}
}

// PullRequest handles pull_request webhooks; only "closed" matters.
func (i *Ingester) PullRequest(ctx context.Context, ev github.PullRequestEvent) error {
	repo, known := i.Config.Get().Project(ev.Repository.FullName)
	if ev.Action != "closed" || !known {
		return nil
	}
	return i.Core.PRClosed(ctx, repo, ev.Number)
}
