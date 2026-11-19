package ingest

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Mtze/simple-cd/internal/config"
	"github.com/Mtze/simple-cd/internal/core"
	"github.com/Mtze/simple-cd/internal/github"
	"github.com/Mtze/simple-cd/internal/store"
	"github.com/Mtze/simple-cd/internal/store/storetest"
)

const digest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

var ctx = context.Background()

type fakeGH struct {
	manifest string
	files    map[string]string
	tags     map[string]bool
	prs      []int
}

func (f *fakeGH) DownloadArtifactFile(_ context.Context, _ string, _ int64, _, _ string) ([]byte, error) {
	if f.manifest == "" {
		return nil, github.ErrNotFound
	}
	return []byte(f.manifest), nil
}

func (f *fakeGH) File(_ context.Context, _, path, _ string) ([]byte, error) {
	c, ok := f.files[path]
	if !ok {
		return nil, github.ErrNotFound
	}
	return []byte(c), nil
}

func (f *fakeGH) PullRequestsForCommit(context.Context, string, string) ([]int, error) {
	return f.prs, nil
}

func (f *fakeGH) IsTag(_ context.Context, _, name string) (bool, error) { return f.tags[name], nil }

type fakeRegistry map[string]bool

func (f fakeRegistry) Exists(_ context.Context, repo, ref string) (bool, error) {
	return !f[repo+"@"+ref], nil
}

func setup(t *testing.T, gh *fakeGH, missing fakeRegistry) *Ingester {
	cfg, err := config.Parse([]byte(`
defaults: {registries: [ghcr.io/org/]}
targets:
  - {id: app-previews, project: Org/App, type: helm, preview: true, cluster: "vault:c", autoDeploy: {from: [pr]}}
`))
	if err != nil {
		t.Fatal(err)
	}
	s := storetest.New(t)
	holder := config.NewStaticHolder(cfg)
	svc := &core.Service{Store: s, Config: holder, Registry: missing}
	return &Ingester{Config: holder, Store: s, Core: svc, Registry: missing,
		GitHub: func(int64) RepoAPI { return gh }}
}

func event(t *testing.T, js string) github.WorkflowRunEvent {
	var ev github.WorkflowRunEvent
	if err := json.Unmarshal([]byte(js), &ev); err != nil {
		t.Fatal(err)
	}
	return ev
}

func run(event, branch, head string, extra string) string {
	return `{"action":"completed","repository":{"full_name":"org/app"},"installation":{"id":1},
	  "workflow_run":{"id":10,"event":"` + event + `","conclusion":"success","head_branch":"` + branch + `",
	  "head_sha":"abc1234","path":".github/workflows/ci.yml","html_url":"https://gh/run/10",
	  "head_repository":{"full_name":"` + head + `"},"triggering_actor":{"login":"octo"}` + extra + `}}`
}

var goodGH = func() *fakeGH {
	return &fakeGH{
		manifest: `{"version":1,"images":[{"name":"app","ref":"ghcr.io/org/app@` + digest + `"}],"helm":{"version":"1.0.0"},"autoDeploy":["app-previews"]}`,
		files: map[string]string{
			".cd.yaml":                    "helm:\n  chart: oci://ghcr.io/org/charts/app\n  targets:\n    app-previews:\n      values: deploy/values-preview.yaml\n      set: {host: 'pr-{cd.pr}.x'}\n",
			"deploy/values-preview.yaml": "replicas: 1\n",
		},
	}
}

func TestPRRunIsStoredAndAutoDeployed(t *testing.T) {
	i := setup(t, goodGH(), nil)
	if err := i.WorkflowRun(ctx, event(t, run("pull_request", "feature", "org/app", `,"pull_requests":[{"number":5}]`))); err != nil {
		t.Fatal(err)
	}
	// Stored under the config's spelling of the repo name.
	a, err := i.Store.ArtifactBySHA(ctx, "Org/App", "abc1234")
	if err != nil {
		t.Fatal(err)
	}
	if a.Source != "pr" || *a.PR != 5 || len(a.Reasons) != 0 || *a.Files["deploy/values-preview.yaml"] != "replicas: 1\n" || a.TriggeringActor != "octo" {
		t.Fatalf("artifact = %+v", a)
	}
	active, _ := i.Store.ActiveDeployments(ctx, "app-previews")
	if len(active) != 1 || active[0].Slot != "5" {
		t.Fatalf("auto-deploy not started: %+v", active)
	}
}

func TestPRNumberFromAPI(t *testing.T) {
	gh := goodGH()
	gh.prs = []int{9}
	i := setup(t, gh, nil)
	i.WorkflowRun(ctx, event(t, run("pull_request", "feature", "org/app", "")))
	a, _ := i.Store.ArtifactBySHA(ctx, "Org/App", "abc1234")
	if a == nil || *a.PR != 9 {
		t.Fatalf("artifact = %+v", a)
	}
}

func TestForkRejected(t *testing.T) {
	i := setup(t, goodGH(), nil)
	if err := i.WorkflowRun(ctx, event(t, run("pull_request", "feature", "evil/app", `,"pull_requests":[{"number":5}]`))); err != nil {
		t.Fatal(err)
	}
	if _, err := i.Store.ArtifactBySHA(ctx, "Org/App", "abc1234"); err == nil {
		t.Fatal("fork artifact stored")
	}
	ev, _ := i.Store.AuditEvents(ctx, 0, 10)
	if len(ev) != 1 || ev[0].Action != "webhook.fork_rejected" {
		t.Fatalf("audit = %+v", ev)
	}
}

func TestSourceDetection(t *testing.T) {
	gh := goodGH()
	gh.tags = map[string]bool{"v1.2.0": true}
	i := setup(t, gh, nil)
	i.WorkflowRun(ctx, event(t, run("push", "v1.2.0", "org/app", "")))
	a, _ := i.Store.ArtifactBySHA(ctx, "Org/App", "abc1234")
	if a.Source != "release" || a.RefName != "v1.2.0" {
		t.Fatalf("tag push: %+v", a)
	}

	i = setup(t, goodGH(), nil)
	i.WorkflowRun(ctx, event(t, run("push", "main", "org/app", "")))
	a, _ = i.Store.ArtifactBySHA(ctx, "Org/App", "abc1234")
	if a.Source != "branch" || a.PR != nil {
		t.Fatalf("branch push: %+v", a)
	}
	active, _ := i.Store.ActiveDeployments(ctx, "app-previews")
	if len(active) != 0 {
		t.Fatal("branch artifact auto-deployed to preview target")
	}
}

func TestIncompleteArtifacts(t *testing.T) {
	gh := goodGH()
	delete(gh.files, ".cd.yaml")
	i := setup(t, gh, fakeRegistry{"ghcr.io/org/app@" + digest: true})
	i.WorkflowRun(ctx, event(t, run("push", "main", "org/app", "")))
	a, _ := i.Store.ArtifactBySHA(ctx, "Org/App", "abc1234")
	r := strings.Join(a.Reasons, "|")
	if !strings.Contains(r, ".cd.yaml is missing") || !strings.Contains(r, "image missing in registry") {
		t.Fatalf("reasons = %v", a.Reasons)
	}

	gh = goodGH()
	gh.manifest = `{"version":1,"images":[{"name":"app","ref":"ghcr.io/org/app:latest"}]}`
	i = setup(t, gh, nil)
	i.WorkflowRun(ctx, event(t, run("push", "main", "org/app", "")))
	a, _ = i.Store.ArtifactBySHA(ctx, "Org/App", "abc1234")
	if len(a.Reasons) != 1 || !strings.Contains(a.Reasons[0], "digest") {
		t.Fatalf("reasons = %v", a.Reasons)
	}
}

func TestIgnoredRuns(t *testing.T) {
	i := setup(t, goodGH(), nil)
	failed := strings.Replace(run("push", "main", "org/app", ""), `"conclusion":"success"`, `"conclusion":"failure"`, 1)
	unknown := strings.Replace(run("push", "main", "org/app", ""), `"full_name":"org/app"},"installation"`, `"full_name":"org/other"},"installation"`, 1)
	for _, js := range []string{failed, unknown} {
		if err := i.WorkflowRun(ctx, event(t, js)); err != nil {
			t.Fatal(err)
		}
	}
	noArtifact := &fakeGH{}
	i.GitHub = func(int64) RepoAPI { return noArtifact }
	i.WorkflowRun(ctx, event(t, run("push", "main", "org/app", "")))
	arts, _ := i.Store.ListArtifacts(ctx, "Org/App", store.ArtifactFilter{IncludeClosed: true})
	if len(arts) != 0 {
		t.Fatalf("stored %d artifacts", len(arts))
	}
}

func TestPullRequestClosed(t *testing.T) {
	i := setup(t, goodGH(), nil)
	i.WorkflowRun(ctx, event(t, run("pull_request", "feature", "org/app", `,"pull_requests":[{"number":5}]`)))
	err := i.PullRequest(ctx, github.PullRequestEvent{Action: "closed", Number: 5, Repository: github.Repository{FullName: "org/app"}})
	if err != nil {
		t.Fatal(err)
	}
	arts, _ := i.Store.ListArtifacts(ctx, "Org/App", store.ArtifactFilter{})
	if len(arts) != 0 {
		t.Fatal("closed PR artifact still listed")
	}
}
