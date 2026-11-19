package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Deployment statuses.
const (
	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusTimedOut  = "timed_out"
)

// Deployment kinds.
const (
	KindDeploy   = "deploy"
	KindTeardown = "teardown"
)

type Deployment struct {
	ID                 int64      `db:"id" json:"id"`
	Kind               string     `db:"kind" json:"kind"`
	ArtifactID         *int64     `db:"artifact_id" json:"artifactId,omitempty"`
	TargetID           string     `db:"target_id" json:"targetId"`
	Slot               string     `db:"slot" json:"slot"`
	Actor              string     `db:"actor" json:"actor"`
	ActorDetail        string     `db:"actor_detail" json:"actorDetail,omitempty"`
	Status             string     `db:"status" json:"status"`
	JobName            string     `db:"job_name" json:"-"`
	Snapshot           string     `db:"snapshot" json:"-"`
	Log                *string    `db:"log" json:"-"`
	Error              string     `db:"error" json:"error,omitempty"`
	GitHubDeploymentID *int64     `db:"github_deployment_id" json:"-"`
	CreatedAt          time.Time  `db:"created_at" json:"createdAt"`
	StartedAt          *time.Time `db:"started_at" json:"startedAt,omitempty"`
	FinishedAt         *time.Time `db:"finished_at" json:"finishedAt,omitempty"`
}

const deploymentCols = `id, kind, artifact_id, target_id, slot, actor, actor_detail, status, job_name, snapshot,
	log, error, github_deployment_id, created_at, started_at, finished_at`

// InsertDeployment creates a pending deployment. It returns ErrSlotBusy when
// the slot already has a pending or running deployment.
func (s *Store) InsertDeployment(ctx context.Context, d *Deployment) (*Deployment, error) {
	if d.Kind == "" {
		d.Kind = KindDeploy
	}
	rows, err := s.db.Query(ctx, `
		INSERT INTO deployments (kind, artifact_id, target_id, slot, actor, actor_detail, status, snapshot)
		VALUES ($1,$2,$3,$4,$5,$6,'pending',$7)
		ON CONFLICT (target_id, slot) WHERE status IN ('pending','running') DO NOTHING
		RETURNING `+deploymentCols,
		d.Kind, d.ArtifactID, d.TargetID, d.Slot, d.Actor, d.ActorDetail, d.Snapshot)
	if err != nil {
		return nil, err
	}
	// DO NOTHING instead of a unique violation keeps a surrounding transaction usable.
	out, err := pgx.CollectExactlyOneRow(rows, pgx.RowToAddrOfStructByName[Deployment])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSlotBusy
	}
	return out, err
}

func (s *Store) Deployment(ctx context.Context, id int64) (*Deployment, error) {
	rows, err := s.db.Query(ctx, `SELECT `+deploymentCols+` FROM deployments WHERE id = $1`, id)
	if err != nil {
		return nil, err
	}
	d, err := pgx.CollectExactlyOneRow(rows, pgx.RowToAddrOfStructByName[Deployment])
	return d, notFound(err)
}

func (s *Store) listDeployments(ctx context.Context, where string, args ...any) ([]*Deployment, error) {
	rows, err := s.db.Query(ctx, `SELECT `+deploymentCols+` FROM deployments WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToAddrOfStructByName[Deployment])
}

// TargetDeployments returns the newest deployments of a target.
func (s *Store) TargetDeployments(ctx context.Context, target string, limit int) ([]*Deployment, error) {
	return s.listDeployments(ctx, `target_id = $1 ORDER BY created_at DESC LIMIT $2`, target, limit)
}

// ArtifactDeployments returns the deployments of an artifact.
func (s *Store) ArtifactDeployments(ctx context.Context, artifact int64) ([]*Deployment, error) {
	return s.listDeployments(ctx, `artifact_id = $1 ORDER BY created_at DESC LIMIT 50`, artifact)
}

// DeploymentsByStatus returns deployments in a status, oldest first.
func (s *Store) DeploymentsByStatus(ctx context.Context, status string) ([]*Deployment, error) {
	return s.listDeployments(ctx, `status = $1 ORDER BY created_at`, status)
}

// ActiveDeployments returns the pending and running deployments of a target.
func (s *Store) ActiveDeployments(ctx context.Context, target string) ([]*Deployment, error) {
	return s.listDeployments(ctx, `target_id = $1 AND status IN ('pending','running') ORDER BY created_at`, target)
}

// LastSucceeded returns the newest successful deploy in a slot, or ErrNotFound.
func (s *Store) LastSucceeded(ctx context.Context, target, slot string) (*Deployment, error) {
	rows, err := s.db.Query(ctx, `SELECT `+deploymentCols+` FROM deployments
		WHERE target_id = $1 AND slot = $2 AND kind = 'deploy' AND status = 'succeeded'
		ORDER BY finished_at DESC LIMIT 1`, target, slot)
	if err != nil {
		return nil, err
	}
	d, err := pgx.CollectExactlyOneRow(rows, pgx.RowToAddrOfStructByName[Deployment])
	return d, notFound(err)
}

func (s *Store) MarkRunning(ctx context.Context, id int64, job string) error {
	_, err := s.db.Exec(ctx, `UPDATE deployments SET status = 'running', job_name = $2, started_at = now() WHERE id = $1 AND status = 'pending'`, id, job)
	return err
}

func (s *Store) Finish(ctx context.Context, id int64, status, log, errMsg string) error {
	_, err := s.db.Exec(ctx, `UPDATE deployments SET status = $2, log = $3, error = $4, finished_at = now()
		WHERE id = $1 AND status IN ('pending','running')`, id, status, log, errMsg)
	return err
}

func (s *Store) SetGitHubDeploymentID(ctx context.Context, id, ghID int64) error {
	_, err := s.db.Exec(ctx, `UPDATE deployments SET github_deployment_id = $2 WHERE id = $1`, id, ghID)
	return err
}

// DeleteOldLogs drops stored logs of deployments finished before t.
func (s *Store) DeleteOldLogs(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.db.Exec(ctx, `UPDATE deployments SET log = NULL WHERE log IS NOT NULL AND finished_at < $1`, before)
	return tag.RowsAffected(), err
}

type QueueEntry struct {
	TargetID    string    `db:"target_id"`
	Slot        string    `db:"slot"`
	ArtifactID  int64     `db:"artifact_id"`
	ActorDetail string    `db:"actor_detail"`
	CreatedAt   time.Time `db:"created_at"`
}

// Enqueue puts an auto-deploy into the slot's waiting place, replacing any
// entry that was waiting there: the newest wins.
func (s *Store) Enqueue(ctx context.Context, q QueueEntry) error {
	_, err := s.db.Exec(ctx, `INSERT INTO deploy_queue (target_id, slot, artifact_id, actor_detail) VALUES ($1,$2,$3,$4)
		ON CONFLICT (target_id, slot) DO UPDATE SET artifact_id = EXCLUDED.artifact_id,
			actor_detail = EXCLUDED.actor_detail, created_at = now()`, q.TargetID, q.Slot, q.ArtifactID, q.ActorDetail)
	return err
}

// QueuedReady returns waiting entries whose slot is free.
func (s *Store) QueuedReady(ctx context.Context) ([]QueueEntry, error) {
	rows, err := s.db.Query(ctx, `SELECT q.target_id, q.slot, q.artifact_id, q.actor_detail, q.created_at FROM deploy_queue q
		WHERE NOT EXISTS (SELECT 1 FROM deployments d WHERE d.target_id = q.target_id AND d.slot = q.slot
			AND d.status IN ('pending','running')) ORDER BY q.created_at`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[QueueEntry])
}

func (s *Store) Dequeue(ctx context.Context, target, slot string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM deploy_queue WHERE target_id = $1 AND slot = $2`, target, slot)
	return err
}
