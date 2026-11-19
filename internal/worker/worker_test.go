package worker

import (
	"context"
	"io"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Mtze/simple-cd/internal/config"
	"github.com/Mtze/simple-cd/internal/core"
	"github.com/Mtze/simple-cd/internal/repoconfig"
	"github.com/Mtze/simple-cd/internal/runner"
	"github.com/Mtze/simple-cd/internal/store"
	"github.com/Mtze/simple-cd/internal/store/storetest"
)

const digest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

var ctx = context.Background()

type fakeRunner struct {
	mu      sync.Mutex
	started map[string]runner.Job
	states  map[string]runner.State
	deleted []string
}

func (f *fakeRunner) Start(_ context.Context, j runner.Job) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started[j.Name] = j
	f.states[j.Name] = runner.State{}
	return nil
}

func (f *fakeRunner) State(_ context.Context, name string) (runner.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.states[name]
	if !ok {
		return s, runner.ErrNotFound
	}
	return s, nil
}

func (f *fakeRunner) Logs(context.Context, string, bool) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("==> done\n")), nil
}

func (f *fakeRunner) Delete(_ context.Context, name string) error {
	f.deleted = append(f.deleted, name)
	return nil
}

type fakeReporter struct{ states []string }

func (f *fakeReporter) Started(context.Context, *store.Artifact, *store.Deployment) (int64, error) {
	return 55, nil
}

func (f *fakeReporter) Finished(_ context.Context, _ string, _ *store.Deployment, id int64, state, _ string) {
	f.states = append(f.states, state)
}

type okRegistry struct{}

func (okRegistry) Exists(context.Context, string, string) (bool, error) { return true, nil }

func setup(t *testing.T) (*Worker, *fakeRunner, *fakeReporter, *time.Time) {
	cfg, err := config.Parse([]byte(`
defaults: {registries: [ghcr.io/org/]}
targets:
  - {id: app-test, project: org/app, type: helm, cluster: "vault:c", namespace: app, jobTimeout: 10m}
  - {id: app-previews, project: org/app, type: helm, preview: true, cluster: "vault:c", autoDeploy: {from: [pr]}, previewTTL: 1h}
`))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	clock := func() time.Time { return now }
	s := storetest.New(t)
	holder := config.NewStaticHolder(cfg)
	svc := &core.Service{Store: s, Config: holder, Registry: okRegistry{}, Now: clock}
	r := &fakeRunner{started: map[string]runner.Job{}, states: map[string]runner.State{}}
	rep := &fakeReporter{}
	return &Worker{Store: s, Config: holder, Core: svc, Runner: r, Reporter: rep, Now: clock}, r, rep, &now
}

func artifact(t *testing.T, w *Worker, pr int) *store.Artifact {
	a, _, err := w.Store.UpsertArtifact(ctx, &store.Artifact{
		Repo: "org/app", SHA: "abc1234", Source: "pr", RefName: "f", PR: &pr, RunID: 1,
		Manifest: repoconfig.Manifest{Version: 1, Images: []repoconfig.Image{{Name: "app", Ref: "ghcr.io/org/app@" + digest}},
			Helm: &repoconfig.ManifestHelm{Version: "1.0.0"}, AutoDeploy: []string{"app-previews"}},
		RepoConfig: &repoconfig.RepoConfig{Helm: &repoconfig.HelmConfig{Chart: "oci://ghcr.io/org/charts/app"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestDeployLifecycle(t *testing.T) {
	w, r, rep, _ := setup(t)
	a := artifact(t, w, 3)
	d, err := w.Core.Deploy(ctx, core.User{Username: "alice", Roles: []string{"project:org/app:developer"}},
		core.DeployRequest{TargetID: "app-test", ArtifactID: a.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	job := r.started["simple-cd-"+itoa(d.ID)]
	if job.Timeout != 10*time.Minute || job.Spec.Helm.Namespace != "app" {
		t.Fatalf("job = %+v", job)
	}
	got, _ := w.Store.Deployment(ctx, d.ID)
	if got.Status != store.StatusRunning || *got.GitHubDeploymentID != 55 {
		t.Fatalf("after start: %+v", got)
	}
	r.states[job.Name] = runner.State{Done: true, Succeeded: true}
	w.Tick(ctx)
	got, _ = w.Store.Deployment(ctx, d.ID)
	if got.Status != store.StatusSucceeded || *got.Log != "==> done\n" || rep.states[0] != "success" {
		t.Fatalf("after finish: %+v %v", got, rep.states)
	}
}

func TestTimeoutAndDisappearedJobs(t *testing.T) {
	w, r, _, now := setup(t)
	a := artifact(t, w, 3)
	u := core.User{Username: "alice", Roles: []string{"project:org/app:developer"}}
	d, _ := w.Core.Deploy(ctx, u, core.DeployRequest{TargetID: "app-test", ArtifactID: a.ID})
	w.Tick(ctx)
	// The database clock decides started_at, so move our clock past it generously.
	*now = time.Now().Add(13 * time.Minute)
	w.Tick(ctx)
	got, _ := w.Store.Deployment(ctx, d.ID)
	if got.Status != store.StatusTimedOut || len(r.deleted) != 1 {
		t.Fatalf("timeout: %+v %v", got, r.deleted)
	}

	*now = time.Now()
	d2, err := w.Core.Deploy(ctx, u, core.DeployRequest{TargetID: "app-test", ArtifactID: a.ID})
	if err != nil {
		t.Fatal(err)
	}
	w.Tick(ctx)
	delete(r.states, "simple-cd-"+itoa(d2.ID))
	w.Tick(ctx)
	got, _ = w.Store.Deployment(ctx, d2.ID)
	if got.Status != store.StatusFailed || !strings.Contains(got.Error, "disappeared") {
		t.Fatalf("disappeared: %+v", got)
	}
}

func TestPreviewTeardown(t *testing.T) {
	w, r, rep, _ := setup(t)
	a := artifact(t, w, 3)
	w.Core.AutoDeploy(ctx, a)
	w.Tick(ctx)
	for name := range r.started {
		r.states[name] = runner.State{Done: true, Succeeded: true}
	}
	w.Tick(ctx)
	if err := w.Core.PRClosed(ctx, "org/app", 3); err != nil {
		t.Fatal(err)
	}
	w.Tick(ctx)
	var teardown runner.Job
	for _, j := range r.started {
		if j.Spec.Kind == store.KindTeardown {
			teardown = j
		}
	}
	if teardown.Spec.Helm == nil || teardown.Spec.Helm.Namespace != "pr-app-3" {
		t.Fatalf("teardown job = %+v", teardown)
	}
	r.states[teardown.Name] = runner.State{Done: true, Succeeded: true}
	w.Tick(ctx)
	if _, err := w.Store.LivePreview(ctx, "app-previews", 3); err != store.ErrNotFound {
		t.Fatalf("preview still live: %v", err)
	}
	if rep.states[len(rep.states)-1] != "inactive" {
		t.Fatalf("github states = %v", rep.states)
	}
}

func TestPendingForRemovedTargetFails(t *testing.T) {
	w, _, _, _ := setup(t)
	a := artifact(t, w, 3)
	d, _ := w.Store.InsertDeployment(ctx, &store.Deployment{ArtifactID: &a.ID, TargetID: "gone", Actor: "x"})
	w.Tick(ctx)
	got, _ := w.Store.Deployment(ctx, d.ID)
	if got.Status != store.StatusFailed || !strings.Contains(got.Error, "no longer exists") {
		t.Fatalf("got %+v", got)
	}
}

func itoa(i int64) string { return strconv.FormatInt(i, 10) }
