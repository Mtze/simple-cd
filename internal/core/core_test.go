package core

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Mtze/simple-cd/internal/config"
	"github.com/Mtze/simple-cd/internal/repoconfig"
	"github.com/Mtze/simple-cd/internal/store"
	"github.com/Mtze/simple-cd/internal/store/storetest"
)

const digest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

const testConfig = `
defaults:
  registries: [ghcr.io/org/]
targets:
  - id: app-test
    project: org/app
    type: helm
    lockable: true
    cluster: vault:secret/cd/clusters/test
    namespace: app
    values: {resources: {cpu: 500m}}
  - id: app-previews
    project: org/app
    type: helm
    preview: true
    autoDeploy: {from: [pr]}
    cluster: vault:secret/cd/clusters/dev
  - id: app-prod
    project: org/app
    type: compose
    role: deploy:app-prod
    host: vm1
    ssh: vault:secret/cd/ssh/prod
    dir: /opt/app
`

var (
	ctx        = context.Background()
	alice      = User{Username: "alice", Roles: []string{"project:org/app:developer"}}
	bob        = User{Username: "bob", Roles: []string{"project:org/app:developer"}}
	maint      = User{Username: "mia", Roles: []string{"project:org/app:maintainer"}}
	stranger   = User{Username: "sam"}
	admin      = User{Username: "root", Roles: []string{RolePlatformAdmin}}
	composeYML = "services:\n  app:\n    image: ${APP_IMAGE}\n"
)

type fakeRegistry struct{ missing map[string]bool }

func (f fakeRegistry) Exists(_ context.Context, repo, ref string) (bool, error) {
	return !f.missing[repo+"@"+ref], nil
}

type fakeNotifier struct{ skipped []string }

func (f *fakeNotifier) AutoDeploySkipped(_ context.Context, _ *store.Artifact, target, reason string) {
	f.skipped = append(f.skipped, target+": "+reason)
}

func newService(t *testing.T) (*Service, *fakeNotifier, *time.Time) {
	cfg, err := config.Parse([]byte(testConfig))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	n := &fakeNotifier{}
	s := &Service{
		Store: storetest.New(t), Config: config.NewStaticHolder(cfg),
		Registry: fakeRegistry{}, Notifier: n, Now: func() time.Time { return now },
	}
	return s, n, &now
}

func addArtifact(t *testing.T, s *Service, sha string, pr int, values string) *store.Artifact {
	t.Helper()
	files := map[string]*string{"deploy/docker-compose.yml": &composeYML}
	if values != "" {
		files["deploy/values-test.yaml"] = &values
	}
	a := &store.Artifact{
		Repo: "org/app", SHA: sha, Source: config.SourcePR, RefName: "feature", PR: &pr, RunID: time.Now().UnixNano(),
		TriggeringActor: "octocat",
		Manifest: repoconfig.Manifest{
			Version: 1, Images: []repoconfig.Image{{Name: "app", Ref: "ghcr.io/org/app@" + digest}},
			Helm: &repoconfig.ManifestHelm{Version: "1.0.0"}, AutoDeploy: []string{"app-previews"},
		},
		RepoConfig: &repoconfig.RepoConfig{
			Helm: &repoconfig.HelmConfig{
				Chart: "oci://ghcr.io/org/charts/app",
				Set:   map[string]string{"image": "{images.app.ref}"},
				Targets: map[string]repoconfig.HelmTarget{
					"app-test":     {Values: "deploy/values-test.yaml"},
					"app-previews": {Set: map[string]string{"host": "pr-{cd.pr}.example.org"}},
				},
			},
			Compose: &repoconfig.ComposeConfig{Path: "deploy/docker-compose.yml"},
		},
		Files: files,
	}
	out, _, err := s.Store.UpsertArtifact(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func finish(t *testing.T, s *Service, d *store.Deployment, status string) {
	t.Helper()
	if err := s.Store.Finish(ctx, d.ID, status, "", ""); err != nil {
		t.Fatal(err)
	}
}

func TestPermissions(t *testing.T) {
	cfg, _ := config.Parse([]byte(testConfig))
	test, _ := cfg.Target("app-test")
	prod, _ := cfg.Target("app-prod")
	cases := []struct {
		u    User
		t    config.Target
		want Permissions
	}{
		{alice, test, Permissions{Deploy: true}},
		{alice, prod, Permissions{}},
		{User{Username: "x", Roles: []string{"project:org/app:developer", "deploy:app-prod"}}, prod, Permissions{Deploy: true}},
		{maint, test, Permissions{Deploy: true, Override: true, Maintenance: true}},
		{maint, prod, Permissions{Maintenance: true}},
		{stranger, test, Permissions{}},
		{admin, prod, Permissions{Deploy: true, Override: true, Maintenance: true}},
		{User{Roles: []string{"project:other/app:maintainer"}}, test, Permissions{}},
	}
	for i, c := range cases {
		if got := PermissionsFor(c.u, c.t); got != c.want {
			t.Errorf("case %d: got %+v want %+v", i, got, c.want)
		}
	}
}

func TestTargetStatusPrecedence(t *testing.T) {
	lockable := config.Target{Lockable: true}
	preview := config.Target{Preview: true}
	now := time.Now()
	later := now.Add(time.Hour)
	locked := store.TargetState{LockHolder: "a", LockUntil: &later}
	active := []*store.Deployment{{}}
	cases := []struct {
		t      config.Target
		st     store.TargetState
		active []*store.Deployment
		want   string
	}{
		{lockable, store.TargetState{Maintenance: true, LockHolder: "a", LockUntil: &later}, active, StatusMaintenance},
		{lockable, locked, active, StatusDeploying},
		{lockable, locked, nil, StatusLocked},
		{config.Target{}, locked, nil, StatusAvailable}, // not lockable: lock row ignored
		{preview, store.TargetState{}, active, StatusAvailable},
	}
	for i, c := range cases {
		if got := TargetStatus(c.t, c.st, c.active, now); got != c.want {
			t.Errorf("case %d: got %s want %s", i, got, c.want)
		}
	}
}

func TestPreviewNamespace(t *testing.T) {
	if got := PreviewNamespace("Org/My_App", 42); got != "pr-my-app-42" {
		t.Errorf("got %s", got)
	}
	long := PreviewNamespace("org/"+strings.Repeat("a", 80), 12345)
	if len(long) > 63 || !strings.HasPrefix(long, "pr-aaa") || !strings.HasSuffix(long, "-12345") {
		t.Errorf("long = %s (%d)", long, len(long))
	}
	if PreviewNamespace("org/"+strings.Repeat("a", 80), 1) == PreviewNamespace("other/"+strings.Repeat("a", 80), 1) {
		t.Error("hash does not separate repos")
	}
}

func TestReadiness(t *testing.T) {
	s, _, _ := newService(t)
	cfg := s.Config.Get()
	a := addArtifact(t, s, "aaa1111", 7, "replicas: 2\n")
	for _, id := range []string{"app-test", "app-previews", "app-prod"} {
		tg, _ := cfg.Target(id)
		if r := Readiness(cfg, tg, a); len(r) != 0 {
			t.Errorf("%s: %v", id, r)
		}
	}
	// Branch artifact on a preview target, evil registry, missing values file.
	a.Source, a.PR = config.SourceBranch, nil
	a.Manifest.Images[0].Ref = "docker.io/evil/app@" + digest
	delete(a.Files, "deploy/values-test.yaml")
	prev, _ := cfg.Target("app-previews")
	r := strings.Join(Readiness(cfg, prev, a), "|")
	for _, want := range []string{"only accept pull request", "outside the allowed registries", "cd.pr"} { // host needs the PR
		if !strings.Contains(r, want) {
			t.Errorf("missing %q in %s", want, r)
		}
	}
	test, _ := cfg.Target("app-test")
	if r := strings.Join(Readiness(cfg, test, a), "|"); !strings.Contains(r, "values file") {
		t.Errorf("values file reason missing: %s", r)
	}
}

func TestDeployLockingAndOverride(t *testing.T) {
	s, _, now := newService(t)
	a := addArtifact(t, s, "aaa1111", 7, "replicas: 2\n")
	req := DeployRequest{TargetID: "app-test", ArtifactID: a.ID}

	if _, err := s.Deploy(ctx, stranger, req); !errors.Is(err, ErrForbidden) {
		t.Fatalf("stranger: %v", err)
	}
	d1, err := s.Deploy(ctx, alice, req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Deploy(ctx, alice, req); !errors.Is(err, ErrSlotBusy) {
		t.Fatalf("second while running: %v", err)
	}
	finish(t, s, d1, store.StatusFailed) // lock stays after a failure

	var locked *LockedError
	if _, err := s.Deploy(ctx, bob, req); !errors.As(err, &locked) || locked.Holder != "alice" {
		t.Fatalf("bob: %v", err)
	}
	if _, err := s.Deploy(ctx, bob, DeployRequest{TargetID: "app-test", ArtifactID: a.ID, Override: true}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("developer override: %v", err)
	}
	d2, err := s.Deploy(ctx, alice, req) // holder redeploys and extends
	if err != nil {
		t.Fatal(err)
	}
	finish(t, s, d2, store.StatusSucceeded)
	d3, err := s.Deploy(ctx, maint, DeployRequest{TargetID: "app-test", ArtifactID: a.ID, Override: true})
	if err != nil {
		t.Fatal(err)
	}
	finish(t, s, d3, store.StatusSucceeded)
	states, _ := s.Store.TargetStates(ctx)
	if st := states["app-test"]; st.LockHolder != "mia" || !st.LockUntil.Equal(now.Add(time.Hour)) {
		t.Fatalf("lock not transferred: %+v", st)
	}
	events, _ := s.Store.LockEvents(ctx, "app-test", 10)
	var actions []string
	for _, e := range events {
		actions = append(actions, e.Action)
	}
	if strings.Join(actions, ",") != "overridden,extended,acquired" {
		t.Fatalf("lock history = %v", actions)
	}

	// Lock expires: bob may deploy again.
	*now = now.Add(2 * time.Hour)
	if _, err := s.Deploy(ctx, bob, req); err != nil {
		t.Fatalf("after expiry: %v", err)
	}
}

func TestDeployMaintenanceAndRelease(t *testing.T) {
	s, _, _ := newService(t)
	a := addArtifact(t, s, "aaa1111", 7, "replicas: 2\n")
	req := DeployRequest{TargetID: "app-test", ArtifactID: a.ID}

	if err := s.SetMaintenance(ctx, alice, "app-test", true, "upgrade"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("developer maintenance: %v", err)
	}
	if err := s.SetMaintenance(ctx, maint, "app-test", true, ""); err == nil {
		t.Fatal("maintenance without reason")
	}
	if err := s.SetMaintenance(ctx, maint, "app-test", true, "upgrade"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Deploy(ctx, maint, req); !errors.Is(err, ErrMaintenance) {
		t.Fatalf("deploy during maintenance: %v", err)
	}
	s.SetMaintenance(ctx, maint, "app-test", false, "")
	d, err := s.Deploy(ctx, alice, req)
	if err != nil {
		t.Fatal(err)
	}
	finish(t, s, d, store.StatusSucceeded)
	if err := s.ReleaseLock(ctx, bob, "app-test"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("bob released alice's lock: %v", err)
	}
	if err := s.ReleaseLock(ctx, alice, "app-test"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Deploy(ctx, bob, req); err != nil {
		t.Fatalf("after release: %v", err)
	}
}

func TestDeployRequiresConfirmationWhenConfigChanges(t *testing.T) {
	s, _, _ := newService(t)
	a := addArtifact(t, s, "aaa1111", 7, "replicas: 2\n")
	d, err := s.Deploy(ctx, alice, DeployRequest{TargetID: "app-test", ArtifactID: a.ID})
	if err != nil {
		t.Fatal(err)
	}
	finish(t, s, d, store.StatusSucceeded)
	s.ReleaseLock(ctx, alice, "app-test")

	same := addArtifact(t, s, "bbb2222", 7, "replicas: 2\n")
	d, err = s.Deploy(ctx, alice, DeployRequest{TargetID: "app-test", ArtifactID: same.ID})
	if err != nil {
		t.Fatalf("unchanged config needed confirmation: %v", err)
	}
	finish(t, s, d, store.StatusSucceeded)

	changed := addArtifact(t, s, "ccc3333", 7, "replicas: 5\n")
	var ce *ConfirmationError
	if _, err := s.Deploy(ctx, alice, DeployRequest{TargetID: "app-test", ArtifactID: changed.ID}); !errors.As(err, &ce) {
		t.Fatalf("expected confirmation: %v", err)
	}
	if !strings.Contains(ce.Diff, "-  replicas: 2") || !strings.Contains(ce.Diff, "+  replicas: 5") {
		t.Fatalf("diff = %s", ce.Diff)
	}
	plan, err := s.PlanDeploy(ctx, alice, "app-test", changed.ID)
	if err != nil || plan.Diff != ce.Diff || !plan.Permissions.Deploy {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
	if _, err := s.Deploy(ctx, alice, DeployRequest{TargetID: "app-test", ArtifactID: changed.ID, Confirm: true}); err != nil {
		t.Fatal(err)
	}
}

func TestDeployRechecksRegistry(t *testing.T) {
	s, _, _ := newService(t)
	a := addArtifact(t, s, "aaa1111", 7, "replicas: 2\n")
	s.Registry = fakeRegistry{missing: map[string]bool{"ghcr.io/org/app@" + digest: true}}
	var nd *NotDeployableError
	if _, err := s.Deploy(ctx, alice, DeployRequest{TargetID: "app-test", ArtifactID: a.ID}); !errors.As(err, &nd) {
		t.Fatalf("expected not deployable: %v", err)
	}
	stored, _ := s.Store.Artifact(ctx, a.ID)
	if len(stored.Reasons) != 1 || !strings.Contains(stored.Reasons[0], "image missing") {
		t.Fatalf("reasons = %v", stored.Reasons)
	}
}

func TestAutoDeployQueueAndSkip(t *testing.T) {
	s, n, _ := newService(t)
	a := addArtifact(t, s, "aaa1111", 7, "")
	a.Manifest.AutoDeploy = []string{"app-previews", "app-test", "missing"}
	s.AutoDeploy(ctx, a)
	if len(n.skipped) != 2 || !strings.Contains(n.skipped[0], "does not allow auto-deploy") || !strings.Contains(n.skipped[1], "unknown target") {
		t.Fatalf("skipped = %v", n.skipped)
	}
	active, _ := s.Store.ActiveDeployments(ctx, "app-previews")
	if len(active) != 1 || active[0].Slot != "7" || active[0].Actor != AutoDeployActor || active[0].ActorDetail != "octocat" {
		t.Fatalf("active = %+v", active)
	}
	if p, err := s.Store.LivePreview(ctx, "app-previews", 7); err != nil || p.Namespace != "pr-app-7" {
		t.Fatalf("preview = %+v, %v", p, err)
	}

	// Two more pushes while the first runs: only the newest waits.
	b := addArtifact(t, s, "bbb2222", 7, "")
	c := addArtifact(t, s, "ccc3333", 7, "")
	s.AutoDeploy(ctx, b)
	s.AutoDeploy(ctx, c)
	// Another PR has its own slot and starts right away.
	other := addArtifact(t, s, "ddd4444", 8, "")
	s.AutoDeploy(ctx, other)
	active, _ = s.Store.ActiveDeployments(ctx, "app-previews")
	if len(active) != 2 {
		t.Fatalf("active = %d", len(active))
	}
	finish(t, s, active[0], store.StatusSucceeded)
	if err := s.ProcessQueue(ctx); err != nil {
		t.Fatal(err)
	}
	active, _ = s.Store.ActiveDeployments(ctx, "app-previews")
	var started *store.Deployment
	for _, d := range active {
		if d.Slot == "7" {
			started = d
		}
	}
	if started == nil || *started.ArtifactID != c.ID {
		t.Fatalf("queued deploy = %+v", started)
	}
}

func TestPRClosedTearsDownPreview(t *testing.T) {
	s, _, _ := newService(t)
	a := addArtifact(t, s, "aaa1111", 7, "")
	s.AutoDeploy(ctx, a)
	active, _ := s.Store.ActiveDeployments(ctx, "app-previews")

	// Deploy still running: the teardown waits and is retried.
	if err := s.PRClosed(ctx, "org/app", 7); err != nil {
		t.Fatal(err)
	}
	finish(t, s, active[0], store.StatusSucceeded)
	if err := s.RetryPreviewTeardowns(ctx); err != nil {
		t.Fatal(err)
	}
	active, _ = s.Store.ActiveDeployments(ctx, "app-previews")
	if len(active) != 1 || active[0].Kind != store.KindTeardown || active[0].Actor != SystemActor {
		t.Fatalf("teardown not scheduled: %+v", active)
	}
	p, _ := s.Store.LivePreview(ctx, "app-previews", 7)
	if p.Status != store.PreviewDeleting {
		t.Fatalf("preview status = %s", p.Status)
	}
	arts, _ := s.Store.ListArtifacts(ctx, "org/app", store.ArtifactFilter{})
	if len(arts) != 0 {
		t.Fatal("closed PR artifacts still listed")
	}
}

func TestBuildSpec(t *testing.T) {
	s, _, _ := newService(t)
	cfg := s.Config.Get()
	a := addArtifact(t, s, "aaa1111", 7, "replicas: 2\nresources: {cpu: 1}\n")
	test, _ := cfg.Target("app-test")
	spec, err := BuildSpec(cfg, test, a, &store.Deployment{ID: 1, Kind: store.KindDeploy}, "")
	if err != nil {
		t.Fatal(err)
	}
	h := spec.Helm
	if h.ClusterSecret != "secret/cd/clusters/test" || h.Namespace != "app" || h.Chart != "oci://ghcr.io/org/charts/app" || h.Version != "1.0.0" {
		t.Fatalf("helm = %+v", h)
	}
	// Inline target values win over the repo file, maps are merged.
	if h.Values["replicas"] != 2 || h.Values["resources"].(map[string]any)["cpu"] != "500m" {
		t.Fatalf("values = %v", h.Values)
	}
	if h.Set["image"] != "ghcr.io/org/app@"+digest {
		t.Fatalf("set = %v", h.Set)
	}

	prev, _ := cfg.Target("app-previews")
	spec, err = BuildSpec(cfg, prev, a, &store.Deployment{ID: 2, Kind: store.KindDeploy}, "")
	if err != nil || !spec.Helm.Preview || spec.Helm.Namespace != "pr-app-7" || spec.Helm.Set["host"] != "pr-7.example.org" || spec.Helm.Labels["cd/pr"] != "7" {
		t.Fatalf("preview spec = %+v, %v", spec.Helm, err)
	}

	prod, _ := cfg.Target("app-prod")
	spec, err = BuildSpec(cfg, prod, a, &store.Deployment{ID: 3, Kind: store.KindDeploy}, "")
	if err != nil || spec.Compose.Env["APP_IMAGE"] != "ghcr.io/org/app@"+digest || spec.Compose.ComposeFile != composeYML {
		t.Fatalf("compose spec = %+v, %v", spec.Compose, err)
	}

	spec, err = BuildSpec(cfg, prev, nil, &store.Deployment{ID: 4, Kind: store.KindTeardown, Slot: "7"}, "")
	if err != nil || spec.Helm.Namespace != "pr-app-7" || spec.Helm.Labels["cd/managed"] != "true" {
		t.Fatalf("teardown spec = %+v, %v", spec.Helm, err)
	}
}
