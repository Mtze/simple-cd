package api

import (
	"context"
	"strings"
	"time"

	"github.com/Mtze/simple-cd/internal/config"
	"github.com/Mtze/simple-cd/internal/core"
	"github.com/Mtze/simple-cd/internal/store"
)

type targetView struct {
	ID                  string             `json:"id"`
	Project             string             `json:"project"`
	Type                string             `json:"type"`
	Preview             bool               `json:"preview"`
	Lockable            bool               `json:"lockable"`
	LockDurationSeconds int64              `json:"lockDurationSeconds"`
	Role                string             `json:"role,omitempty"`
	AutoDeploy          *config.AutoDeploy `json:"autoDeploy,omitempty"`
	Status              string             `json:"status"`
	Maintenance         *maintenanceView   `json:"maintenance,omitempty"`
	Lock                *lockView          `json:"lock,omitempty"`
	Current             *deployedView      `json:"current,omitempty"`
	Active              []deploymentView   `json:"active"`
	Permissions         core.Permissions   `json:"permissions"`
}

type maintenanceView struct {
	Reason string     `json:"reason"`
	By     string     `json:"by"`
	Since  *time.Time `json:"since,omitempty"`
}

type lockView struct {
	Holder string    `json:"holder"`
	Until  time.Time `json:"until"`
}

type deployedView struct {
	DeploymentID int64      `json:"deploymentId"`
	ArtifactID   *int64     `json:"artifactId,omitempty"`
	SHA          string     `json:"sha,omitempty"`
	RefName      string     `json:"refName,omitempty"`
	Source       string     `json:"source,omitempty"`
	PR           *int       `json:"pr,omitempty"`
	Actor        string     `json:"actor"`
	FinishedAt   *time.Time `json:"finishedAt,omitempty"`
}

type artifactBrief struct {
	ID      int64  `json:"id"`
	Repo    string `json:"repo"`
	SHA     string `json:"sha"`
	Source  string `json:"source"`
	RefName string `json:"refName"`
	PR      *int   `json:"pr,omitempty"`
}

type deploymentView struct {
	*store.Deployment
	Artifact *artifactBrief `json:"artifact,omitempty"`
}

type artifactView struct {
	artifactBrief
	PRClosed        bool                `json:"prClosed"`
	RunURL          string              `json:"runUrl"`
	Workflow        string              `json:"workflow"`
	TriggeringActor string              `json:"triggeringActor"`
	OtherWorkflows  bool                `json:"otherWorkflows"`
	Images          []imageView         `json:"images"`
	HelmVersion     string              `json:"helmVersion,omitempty"`
	AutoDeploy      []string            `json:"autoDeploy"`
	CreatedAt       time.Time           `json:"createdAt"`
	Readiness       map[string][]string `json:"readiness"`
	DeployedTo      []string            `json:"deployedTo"`
}

type imageView struct {
	Name string `json:"name"`
	Ref  string `json:"ref"`
}

type previewView struct {
	*store.Preview
	Orphaned    bool `json:"orphaned"`
	CanTeardown bool `json:"canTeardown"`
}

func brief(a *store.Artifact) *artifactBrief {
	if a == nil {
		return nil
	}
	return &artifactBrief{ID: a.ID, Repo: a.Repo, SHA: a.SHA, Source: a.Source, RefName: a.RefName, PR: a.PR}
}

// snapshot is the state needed to render targets, loaded once per request.
type snapshot struct {
	cfg       *config.Config
	states    map[string]store.TargetState
	active    map[string][]*store.Deployment
	current   map[string]*store.Deployment
	artifacts map[int64]*store.Artifact
	now       time.Time
}

func (s *Server) loadSnapshot(ctx context.Context) (*snapshot, error) {
	snap := &snapshot{cfg: s.Config.Get(), active: map[string][]*store.Deployment{}, artifacts: map[int64]*store.Artifact{}, now: time.Now()}
	var err error
	if snap.states, err = s.Store.TargetStates(ctx); err != nil {
		return nil, err
	}
	if snap.current, err = s.Store.CurrentDeployments(ctx); err != nil {
		return nil, err
	}
	active, err := s.Store.AllActive(ctx)
	if err != nil {
		return nil, err
	}
	for _, d := range active {
		snap.active[d.TargetID] = append(snap.active[d.TargetID], d)
	}
	return snap, nil
}

func (snap *snapshot) artifact(ctx context.Context, st *store.Store, id *int64) *store.Artifact {
	if id == nil {
		return nil
	}
	if a, ok := snap.artifacts[*id]; ok {
		return a
	}
	a, _ := st.Artifact(ctx, *id)
	snap.artifacts[*id] = a
	return a
}

func (s *Server) deploymentView(ctx context.Context, snap *snapshot, d *store.Deployment) deploymentView {
	return deploymentView{Deployment: d, Artifact: brief(snap.artifact(ctx, s.Store, d.ArtifactID))}
}

func (s *Server) targetView(ctx context.Context, snap *snapshot, u core.User, t config.Target) targetView {
	st := snap.states[t.ID]
	v := targetView{
		ID: t.ID, Project: t.Project, Type: t.Type, Preview: t.Preview, Lockable: t.Lockable,
		LockDurationSeconds: int64(snap.cfg.LockDuration(t).Seconds()), Role: t.Role, AutoDeploy: t.AutoDeploy,
		Status:      core.TargetStatus(t, st, snap.active[t.ID], snap.now),
		Permissions: core.PermissionsFor(u, t), Active: []deploymentView{},
	}
	if st.Maintenance {
		v.Maintenance = &maintenanceView{Reason: st.MaintenanceReason, By: st.MaintenanceBy, Since: st.MaintenanceSince}
	}
	if t.Lockable && st.Locked(snap.now) {
		v.Lock = &lockView{Holder: st.LockHolder, Until: *st.LockUntil}
	}
	for _, d := range snap.active[t.ID] {
		v.Active = append(v.Active, s.deploymentView(ctx, snap, d))
	}
	if d := snap.current[t.ID]; d != nil && !t.Preview {
		cur := &deployedView{DeploymentID: d.ID, ArtifactID: d.ArtifactID, Actor: d.Actor, FinishedAt: d.FinishedAt}
		if a := snap.artifact(ctx, s.Store, d.ArtifactID); a != nil {
			cur.SHA, cur.RefName, cur.Source, cur.PR = a.SHA, a.RefName, a.Source, a.PR
		}
		v.Current = cur
	}
	return v
}

func (s *Server) artifactView(snap *snapshot, a *store.Artifact) artifactView {
	v := artifactView{
		artifactBrief: *brief(a), PRClosed: a.PRClosed, RunURL: a.RunURL, Workflow: a.Workflow,
		TriggeringActor: a.TriggeringActor, OtherWorkflows: a.OtherWorkflows, Images: []imageView{},
		AutoDeploy: a.Manifest.AutoDeploy, CreatedAt: a.CreatedAt, Readiness: map[string][]string{}, DeployedTo: []string{},
	}
	if v.AutoDeploy == nil {
		v.AutoDeploy = []string{}
	}
	for _, img := range a.Manifest.Images {
		v.Images = append(v.Images, imageView(img))
	}
	if a.Manifest.Helm != nil {
		v.HelmVersion = a.Manifest.Helm.Version
	}
	for _, t := range snap.cfg.TargetsOf(a.Repo) {
		r := core.Readiness(snap.cfg, t, a)
		if r == nil {
			r = []string{}
		}
		v.Readiness[t.ID] = r
		if d := snap.current[t.ID]; d != nil && d.ArtifactID != nil && *d.ArtifactID == a.ID {
			v.DeployedTo = append(v.DeployedTo, t.ID)
		}
	}
	return v
}

func (s *Server) previewView(cfg *config.Config, u core.User, p *store.Preview) previewView {
	t, known := cfg.Target(p.TargetID)
	can := u.IsAdmin() || (known && core.PermissionsFor(u, t).Deploy)
	return previewView{Preview: p, Orphaned: !known, CanTeardown: can && p.Status == store.PreviewActive}
}

func projectRole(u core.User, project string) string {
	if u.IsAdmin() {
		return core.RoleMaintainer
	}
	return u.ProjectRole(project)
}

func sameProject(a, b string) bool { return strings.EqualFold(a, b) }
