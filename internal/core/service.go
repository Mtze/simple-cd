package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/Mtze/simple-cd/internal/config"
	"github.com/Mtze/simple-cd/internal/diff"
	"github.com/Mtze/simple-cd/internal/repoconfig"
	"github.com/Mtze/simple-cd/internal/store"
)

var (
	ErrForbidden   = errors.New("you are not allowed to do this")
	ErrNotFound    = errors.New("not found")
	ErrMaintenance = errors.New("target is in maintenance")
	ErrSlotBusy    = store.ErrSlotBusy
	ErrNeedReason  = errors.New("a reason is required")
)

// NotDeployableError lists why an artifact cannot be deployed.
type NotDeployableError struct{ Reasons []string }

func (e *NotDeployableError) Error() string {
	return "artifact is not deployable: " + strings.Join(e.Reasons, "; ")
}

// LockedError is returned when someone else holds the target's lock.
type LockedError struct {
	Holder string
	Until  time.Time
}

func (e *LockedError) Error() string {
	return fmt.Sprintf("target is locked by %s until %s", e.Holder, e.Until.Format(time.RFC3339))
}

// ConfirmationError is returned when the config changed since the last
// deploy and the user has not confirmed the diff yet.
type ConfirmationError struct{ Diff string }

func (e *ConfirmationError) Error() string { return "configuration changed, confirmation required" }

// RegistryChecker checks that an image or chart version exists.
type RegistryChecker interface {
	Exists(ctx context.Context, repository, reference string) (bool, error)
}

// Notifier reports outcomes back to GitHub. All methods are best effort.
type Notifier interface {
	AutoDeploySkipped(ctx context.Context, a *store.Artifact, target, reason string)
}

type Service struct {
	Store    *store.Store
	Config   *config.Holder
	Registry RegistryChecker
	Notifier Notifier
	Now      func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) target(id string) (*config.Config, config.Target, error) {
	cfg := s.Config.Get()
	t, ok := cfg.Target(id)
	if !ok {
		return cfg, t, ErrNotFound
	}
	return cfg, t, nil
}

func (s *Service) artifact(ctx context.Context, id int64) (*store.Artifact, error) {
	a, err := s.Store.Artifact(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrNotFound
	}
	return a, err
}

// RecheckRegistry verifies right before a deploy that images and chart still
// exist. A missing one is stored as a reason, so the UI shows "image missing".
func (s *Service) RecheckRegistry(ctx context.Context, a *store.Artifact) ([]string, error) {
	if s.Registry == nil {
		return nil, nil
	}
	var missing []string
	for _, img := range a.Manifest.Images {
		r, err := repoconfig.ParseImageRef(img.Ref)
		if err != nil {
			continue
		}
		ok, err := s.Registry.Exists(ctx, r.Repository, r.Digest)
		if err != nil {
			return nil, fmt.Errorf("registry check for %s: %w", r.Repository, err)
		}
		if !ok {
			missing = append(missing, "image missing in registry: "+r.String())
		}
	}
	if rc := a.RepoConfig; rc != nil && rc.Helm != nil && a.Manifest.Helm != nil {
		repo := repoconfig.ChartRef(rc.Helm.Chart)
		ok, err := s.Registry.Exists(ctx, repo, a.Manifest.Helm.Version)
		if err != nil {
			return nil, fmt.Errorf("registry check for %s: %w", repo, err)
		}
		if !ok {
			missing = append(missing, fmt.Sprintf("chart missing in registry: %s:%s", repo, a.Manifest.Helm.Version))
		}
	}
	if len(missing) > 0 {
		if err := s.Store.SetArtifactReasons(ctx, a.ID, append(a.Reasons, missing...)); err != nil {
			return nil, err
		}
	}
	return missing, nil
}

// Plan is what the deploy dialog shows before the user confirms.
type Plan struct {
	TargetID    string      `json:"targetId"`
	ArtifactID  int64       `json:"artifactId"`
	Slot        string      `json:"slot"`
	Status      string      `json:"status"`
	Reasons     []string    `json:"reasons"`
	Diff        string      `json:"diff"`
	NeedsLock   bool        `json:"needsOverride"`
	LockHolder  string      `json:"lockHolder,omitempty"`
	LockUntil   *time.Time  `json:"lockUntil,omitempty"`
	Permissions Permissions `json:"permissions"`
}

// PlanDeploy computes readiness, diff and lock situation without changing anything.
func (s *Service) PlanDeploy(ctx context.Context, u User, targetID string, artifactID int64) (*Plan, error) {
	cfg, t, err := s.target(targetID)
	if err != nil {
		return nil, err
	}
	a, err := s.artifact(ctx, artifactID)
	if err != nil {
		return nil, err
	}
	p := &Plan{TargetID: t.ID, ArtifactID: a.ID, Slot: Slot(t, a), Permissions: PermissionsFor(u, t)}
	p.Reasons = Readiness(cfg, t, a)
	if len(p.Reasons) == 0 {
		p.Diff, err = s.diffFor(ctx, t, a)
		if err != nil {
			return nil, err
		}
	}
	states, err := s.Store.TargetStates(ctx)
	if err != nil {
		return nil, err
	}
	st := states[t.ID]
	active, err := s.Store.ActiveDeployments(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	p.Status = TargetStatus(t, st, active, s.now())
	if t.Lockable && st.Locked(s.now()) && st.LockHolder != u.Username {
		p.NeedsLock, p.LockHolder, p.LockUntil = true, st.LockHolder, st.LockUntil
	}
	return p, nil
}

func (s *Service) diffFor(ctx context.Context, t config.Target, a *store.Artifact) (string, error) {
	snap, err := Snapshot(t, a)
	if err != nil || snap == "" {
		return "", err
	}
	last, err := s.Store.LastSucceeded(ctx, t.ID, Slot(t, a))
	if errors.Is(err, store.ErrNotFound) {
		return "", nil // first deploy: nothing to compare with
	}
	if err != nil {
		return "", err
	}
	return diff.Unified("deployed", a.SHA[:min(7, len(a.SHA))], last.Snapshot, snap), nil
}

type DeployRequest struct {
	TargetID   string `json:"targetId"`
	ArtifactID int64  `json:"artifactId"`
	Confirm    bool   `json:"confirm"`  // the user saw the diff
	Override   bool   `json:"override"` // take over someone else's lock
}

// Deploy starts a manual deployment. The worker picks the pending row up.
func (s *Service) Deploy(ctx context.Context, u User, req DeployRequest) (*store.Deployment, error) {
	cfg, t, err := s.target(req.TargetID)
	if err != nil {
		return nil, err
	}
	perms := PermissionsFor(u, t)
	if !perms.Deploy {
		return nil, ErrForbidden
	}
	a, err := s.artifact(ctx, req.ArtifactID)
	if err != nil {
		return nil, err
	}
	if r := Readiness(cfg, t, a); len(r) > 0 {
		return nil, &NotDeployableError{r}
	}
	if missing, err := s.RecheckRegistry(ctx, a); err != nil {
		return nil, err
	} else if len(missing) > 0 {
		return nil, &NotDeployableError{missing}
	}
	d, err := s.diffFor(ctx, t, a)
	if err != nil {
		return nil, err
	}
	if d != "" && !req.Confirm {
		return nil, &ConfirmationError{Diff: d}
	}
	snap, err := Snapshot(t, a)
	if err != nil {
		return nil, err
	}

	var out *store.Deployment
	now := s.now()
	err = s.Store.WithTx(ctx, func(tx *store.Store) error {
		st, err := tx.LockTargetState(ctx, t.ID)
		if err != nil {
			return err
		}
		if st.Maintenance {
			return ErrMaintenance
		}
		lockAction := "acquired"
		if t.Lockable && st.Locked(now) {
			switch {
			case st.LockHolder == u.Username:
				lockAction = "extended"
			case !req.Override:
				return &LockedError{Holder: st.LockHolder, Until: *st.LockUntil}
			case !perms.Override:
				return ErrForbidden
			default:
				lockAction = "overridden"
				if err := tx.Audit(ctx, u.Username, "lock.override", t.ID, map[string]any{"previousHolder": st.LockHolder}); err != nil {
					return err
				}
			}
		}
		out, err = tx.InsertDeployment(ctx, &store.Deployment{
			ArtifactID: &a.ID, TargetID: t.ID, Slot: Slot(t, a), Actor: u.Username, Snapshot: snap,
		})
		if err != nil {
			return err
		}
		if t.Lockable {
			until := now.Add(cfg.LockDuration(t))
			st.LockHolder, st.LockUntil = u.Username, &until
			if err := tx.SaveTargetState(ctx, st); err != nil {
				return err
			}
			if err := tx.AddLockEvent(ctx, store.LockEvent{TargetID: t.ID, Holder: u.Username, Action: lockAction, ByUser: u.Username, UntilAt: &until}); err != nil {
				return err
			}
		}
		if err := s.touchPreview(ctx, tx, cfg, t, a); err != nil {
			return err
		}
		return tx.Audit(ctx, u.Username, "deploy.requested", t.ID, map[string]any{
			"deployment": out.ID, "artifact": a.ID, "sha": a.SHA, "confirmedDiff": d != "",
		})
	})
	return out, err
}

func (s *Service) touchPreview(ctx context.Context, tx *store.Store, cfg *config.Config, t config.Target, a *store.Artifact) error {
	if !t.Preview || a.PR == nil {
		return nil
	}
	return tx.TouchPreview(ctx, a.Repo, *a.PR, t.ID, PreviewNamespace(a.Repo, *a.PR), s.now().Add(cfg.PreviewTTL(t)))
}

// AutoDeploy handles the targets an artifact asks for in deploy.json.
func (s *Service) AutoDeploy(ctx context.Context, a *store.Artifact) {
	for _, id := range a.Manifest.AutoDeploy {
		if reason := s.autoDeployOne(ctx, a, id); reason != "" {
			slog.Info("auto-deploy skipped", "target", id, "artifact", a.ID, "reason", reason)
			s.Store.Audit(ctx, AutoDeployActor, "deploy.skipped", id, map[string]any{
				"artifact": a.ID, "sha": a.SHA, "reason": reason, "triggeringActor": a.TriggeringActor,
			})
			if s.Notifier != nil {
				s.Notifier.AutoDeploySkipped(ctx, a, id, reason)
			}
		}
	}
}

// autoDeployOne returns a skip reason, or "" when the deploy was started or queued.
func (s *Service) autoDeployOne(ctx context.Context, a *store.Artifact, targetID string) string {
	cfg, t, err := s.target(targetID)
	if err != nil {
		return "unknown target"
	}
	if !strings.EqualFold(t.Project, a.Repo) {
		return "target belongs to another project"
	}
	if !t.AllowsAutoDeploy(a.Source, a.RefName) {
		return fmt.Sprintf("target does not allow auto-deploy from %s %q", a.Source, a.RefName)
	}
	if r := Readiness(cfg, t, a); len(r) > 0 {
		return "not deployable: " + strings.Join(r, "; ")
	}
	if missing, err := s.RecheckRegistry(ctx, a); err != nil {
		return err.Error()
	} else if len(missing) > 0 {
		return strings.Join(missing, "; ")
	}
	snap, err := Snapshot(t, a)
	if err != nil {
		return err.Error()
	}
	reason := ""
	err = s.Store.WithTx(ctx, func(tx *store.Store) error {
		st, err := tx.LockTargetState(ctx, t.ID)
		if err != nil {
			return err
		}
		if st.Maintenance {
			reason = "target is in maintenance"
			return nil
		}
		if t.Lockable && st.Locked(s.now()) {
			reason = "target is locked by " + st.LockHolder
			return nil
		}
		d, err := tx.InsertDeployment(ctx, &store.Deployment{
			ArtifactID: &a.ID, TargetID: t.ID, Slot: Slot(t, a), Actor: AutoDeployActor,
			ActorDetail: a.TriggeringActor, Snapshot: snap,
		})
		if errors.Is(err, store.ErrSlotBusy) {
			return tx.Enqueue(ctx, store.QueueEntry{TargetID: t.ID, Slot: Slot(t, a), ArtifactID: a.ID, ActorDetail: a.TriggeringActor})
		}
		if err != nil {
			return err
		}
		if err := s.touchPreview(ctx, tx, cfg, t, a); err != nil {
			return err
		}
		return tx.Audit(ctx, AutoDeployActor, "deploy.requested", t.ID, map[string]any{
			"deployment": d.ID, "artifact": a.ID, "sha": a.SHA, "triggeringActor": a.TriggeringActor,
		})
	})
	if err != nil {
		// The InsertDeployment error case rolls the transaction back; report it.
		return err.Error()
	}
	return reason
}

// ProcessQueue starts waiting auto-deploys whose slot became free.
func (s *Service) ProcessQueue(ctx context.Context) error {
	ready, err := s.Store.QueuedReady(ctx)
	if err != nil {
		return err
	}
	for _, q := range ready {
		if err := s.Store.Dequeue(ctx, q.TargetID, q.Slot); err != nil {
			return err
		}
		a, err := s.Store.Artifact(ctx, q.ArtifactID)
		if err != nil {
			continue
		}
		if reason := s.autoDeployOne(ctx, a, q.TargetID); reason != "" {
			s.Store.Audit(ctx, AutoDeployActor, "deploy.skipped", q.TargetID, map[string]any{"artifact": a.ID, "reason": reason})
			if s.Notifier != nil {
				s.Notifier.AutoDeploySkipped(ctx, a, q.TargetID, reason)
			}
		}
	}
	return nil
}

// ReleaseLock frees a lock early. The holder may always do it, others need
// the override permission.
func (s *Service) ReleaseLock(ctx context.Context, u User, targetID string) error {
	_, t, err := s.target(targetID)
	if err != nil {
		return err
	}
	return s.Store.WithTx(ctx, func(tx *store.Store) error {
		st, err := tx.LockTargetState(ctx, t.ID)
		if err != nil {
			return err
		}
		if !st.Locked(s.now()) {
			return nil
		}
		if st.LockHolder != u.Username && !PermissionsFor(u, t).Override {
			return ErrForbidden
		}
		holder := st.LockHolder
		st.LockHolder, st.LockUntil = "", nil
		if err := tx.SaveTargetState(ctx, st); err != nil {
			return err
		}
		if err := tx.AddLockEvent(ctx, store.LockEvent{TargetID: t.ID, Holder: holder, Action: "released", ByUser: u.Username}); err != nil {
			return err
		}
		return tx.Audit(ctx, u.Username, "lock.release", t.ID, map[string]any{"holder": holder})
	})
}

// SetMaintenance switches maintenance on or off.
func (s *Service) SetMaintenance(ctx context.Context, u User, targetID string, on bool, reason string) error {
	_, t, err := s.target(targetID)
	if err != nil {
		return err
	}
	if !PermissionsFor(u, t).Maintenance {
		return ErrForbidden
	}
	if on && strings.TrimSpace(reason) == "" {
		return ErrNeedReason
	}
	return s.Store.WithTx(ctx, func(tx *store.Store) error {
		st, err := tx.LockTargetState(ctx, t.ID)
		if err != nil {
			return err
		}
		now := s.now()
		if on {
			st.Maintenance, st.MaintenanceReason, st.MaintenanceBy, st.MaintenanceSince = true, reason, u.Username, &now
		} else {
			st.Maintenance, st.MaintenanceReason, st.MaintenanceBy, st.MaintenanceSince = false, "", "", nil
		}
		if err := tx.SaveTargetState(ctx, st); err != nil {
			return err
		}
		action := "maintenance.off"
		if on {
			action = "maintenance.on"
		}
		return tx.Audit(ctx, u.Username, action, t.ID, map[string]any{"reason": reason})
	})
}

// TeardownPreview schedules the removal of a preview. With a user, the user
// needs deploy permission on the target; without one it runs as the system.
// Orphaned previews (target gone from config) need a platform admin.
func (s *Service) TeardownPreview(ctx context.Context, u *User, p *store.Preview) error {
	cfg := s.Config.Get()
	t, known := cfg.Target(p.TargetID)
	actor := SystemActor
	if u != nil {
		actor = u.Username
		allowed := u.IsAdmin() || (known && PermissionsFor(*u, t).Deploy)
		if !allowed {
			return ErrForbidden
		}
	}
	if !known {
		// The executor needs the target's cluster credentials, which are gone
		// with the target. Mark it deleted; the admin removes the namespace.
		if err := s.Store.SetPreviewStatus(ctx, p.ID, store.PreviewDeleted); err != nil {
			return err
		}
		return s.Store.Audit(ctx, actor, "preview.forget", p.TargetID, map[string]any{"pr": p.PR, "namespace": p.Namespace})
	}
	return s.Store.WithTx(ctx, func(tx *store.Store) error {
		d, err := tx.InsertDeployment(ctx, &store.Deployment{
			Kind: store.KindTeardown, TargetID: t.ID, Slot: strconv.Itoa(p.PR), Actor: actor,
		})
		if err != nil {
			return err
		}
		if err := tx.Dequeue(ctx, t.ID, strconv.Itoa(p.PR)); err != nil {
			return err
		}
		if err := tx.SetPreviewStatus(ctx, p.ID, store.PreviewDeleting); err != nil {
			return err
		}
		return tx.Audit(ctx, actor, "preview.teardown", t.ID, map[string]any{"pr": p.PR, "deployment": d.ID})
	})
}

// PRClosed hides the PR's artifacts and tears down its previews.
func (s *Service) PRClosed(ctx context.Context, repo string, pr int) error {
	if err := s.Store.MarkPRClosed(ctx, repo, pr); err != nil {
		return err
	}
	previews, err := s.Store.PreviewsOfPR(ctx, repo, pr)
	if err != nil {
		return err
	}
	var errs []error
	for _, p := range previews {
		err := s.TeardownPreview(ctx, nil, p)
		if errors.Is(err, store.ErrSlotBusy) {
			// A deploy is still running in the slot: expire it so the worker retries.
			err = s.Store.ExpirePreview(ctx, p.ID, s.now())
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// RetryPreviewTeardowns tears down expired previews and previews of closed PRs
// whose teardown could not start because their slot was busy.
func (s *Service) RetryPreviewTeardowns(ctx context.Context) error {
	expired, err := s.Store.ExpiredPreviews(ctx, s.now())
	if err != nil {
		return err
	}
	for _, p := range expired {
		if err := s.TeardownPreview(ctx, nil, p); err != nil && !errors.Is(err, store.ErrSlotBusy) {
			slog.Error("preview teardown", "preview", p.ID, "err", err)
		}
	}
	return nil
}
