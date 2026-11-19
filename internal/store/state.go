package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

type TargetState struct {
	TargetID          string     `db:"target_id" json:"-"`
	Maintenance       bool       `db:"maintenance" json:"maintenance"`
	MaintenanceReason string     `db:"maintenance_reason" json:"maintenanceReason,omitempty"`
	MaintenanceBy     string     `db:"maintenance_by" json:"maintenanceBy,omitempty"`
	MaintenanceSince  *time.Time `db:"maintenance_since" json:"maintenanceSince,omitempty"`
	LockHolder        string     `db:"lock_holder" json:"lockHolder,omitempty"`
	LockUntil         *time.Time `db:"lock_until" json:"lockUntil,omitempty"`
}

// Locked reports whether the lock is active at now.
func (t TargetState) Locked(now time.Time) bool {
	return t.LockHolder != "" && t.LockUntil != nil && t.LockUntil.After(now)
}

const stateCols = `target_id, maintenance, maintenance_reason, maintenance_by, maintenance_since, lock_holder, lock_until`

// LockTargetState returns the state row FOR UPDATE, creating it if missing.
// Call it inside WithTx: it serialises all decisions about one target.
func (s *Store) LockTargetState(ctx context.Context, target string) (TargetState, error) {
	if _, err := s.db.Exec(ctx, `INSERT INTO target_state (target_id) VALUES ($1) ON CONFLICT DO NOTHING`, target); err != nil {
		return TargetState{}, err
	}
	rows, err := s.db.Query(ctx, `SELECT `+stateCols+` FROM target_state WHERE target_id = $1 FOR UPDATE`, target)
	if err != nil {
		return TargetState{}, err
	}
	return pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[TargetState])
}

func (s *Store) SaveTargetState(ctx context.Context, t TargetState) error {
	_, err := s.db.Exec(ctx, `UPDATE target_state SET maintenance = $2, maintenance_reason = $3, maintenance_by = $4,
		maintenance_since = $5, lock_holder = $6, lock_until = $7 WHERE target_id = $1`,
		t.TargetID, t.Maintenance, t.MaintenanceReason, t.MaintenanceBy, t.MaintenanceSince, t.LockHolder, t.LockUntil)
	return err
}

// TargetStates returns all known state rows keyed by target id.
func (s *Store) TargetStates(ctx context.Context) (map[string]TargetState, error) {
	rows, err := s.db.Query(ctx, `SELECT `+stateCols+` FROM target_state`)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByName[TargetState])
	if err != nil {
		return nil, err
	}
	out := make(map[string]TargetState, len(list))
	for _, t := range list {
		out[t.TargetID] = t
	}
	return out, nil
}

type LockEvent struct {
	ID       int64      `db:"id" json:"id"`
	TargetID string     `db:"target_id" json:"targetId"`
	Holder   string     `db:"holder" json:"holder"`
	Action   string     `db:"action" json:"action"`
	ByUser   string     `db:"by_user" json:"byUser"`
	UntilAt  *time.Time `db:"until_at" json:"until,omitempty"`
	At       time.Time  `db:"at" json:"at"`
}

func (s *Store) AddLockEvent(ctx context.Context, e LockEvent) error {
	_, err := s.db.Exec(ctx, `INSERT INTO lock_history (target_id, holder, action, by_user, until_at) VALUES ($1,$2,$3,$4,$5)`,
		e.TargetID, e.Holder, e.Action, e.ByUser, e.UntilAt)
	return err
}

func (s *Store) LockEvents(ctx context.Context, target string, limit int) ([]LockEvent, error) {
	rows, err := s.db.Query(ctx, `SELECT id, target_id, holder, action, by_user, until_at, at FROM lock_history
		WHERE target_id = $1 ORDER BY at DESC LIMIT $2`, target, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[LockEvent])
}

// Preview statuses.
const (
	PreviewActive   = "active"
	PreviewDeleting = "deleting"
	PreviewDeleted  = "deleted"
)

type Preview struct {
	ID        int64     `db:"id" json:"id"`
	Repo      string    `db:"repo" json:"repo"`
	PR        int       `db:"pr" json:"pr"`
	TargetID  string    `db:"target_id" json:"targetId"`
	Namespace string    `db:"namespace" json:"namespace"`
	Status    string    `db:"status" json:"status"`
	ExpiresAt time.Time `db:"expires_at" json:"expiresAt"`
	CreatedAt time.Time `db:"created_at" json:"createdAt"`
	UpdatedAt time.Time `db:"updated_at" json:"updatedAt"`
}

const previewCols = `id, repo, pr, target_id, namespace, status, expires_at, created_at, updated_at`

// TouchPreview creates the live preview of a PR or resets its TTL.
func (s *Store) TouchPreview(ctx context.Context, repo string, pr int, target, ns string, expires time.Time) error {
	_, err := s.db.Exec(ctx, `INSERT INTO previews (repo, pr, target_id, namespace, status, expires_at)
		VALUES ($1,$2,$3,$4,'active',$5)
		ON CONFLICT (target_id, pr) WHERE status <> 'deleted'
		DO UPDATE SET expires_at = EXCLUDED.expires_at, status = 'active', updated_at = now()`,
		repo, pr, target, ns, expires)
	return err
}

func (s *Store) LivePreview(ctx context.Context, target string, pr int) (*Preview, error) {
	rows, err := s.db.Query(ctx, `SELECT `+previewCols+` FROM previews WHERE target_id = $1 AND pr = $2 AND status <> 'deleted'`, target, pr)
	if err != nil {
		return nil, err
	}
	p, err := pgx.CollectExactlyOneRow(rows, pgx.RowToAddrOfStructByName[Preview])
	return p, notFound(err)
}

// LivePreviews returns previews that are not deleted. repo "" means all.
func (s *Store) LivePreviews(ctx context.Context, repo string) ([]*Preview, error) {
	rows, err := s.db.Query(ctx, `SELECT `+previewCols+` FROM previews WHERE status <> 'deleted' AND ($1 = '' OR repo = $1) ORDER BY pr DESC`, repo)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToAddrOfStructByName[Preview])
}

// PreviewsOfPR returns the live previews of a PR across targets.
func (s *Store) PreviewsOfPR(ctx context.Context, repo string, pr int) ([]*Preview, error) {
	rows, err := s.db.Query(ctx, `SELECT `+previewCols+` FROM previews WHERE status = 'active' AND repo = $1 AND pr = $2`, repo, pr)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToAddrOfStructByName[Preview])
}

func (s *Store) ExpiredPreviews(ctx context.Context, now time.Time) ([]*Preview, error) {
	rows, err := s.db.Query(ctx, `SELECT `+previewCols+` FROM previews WHERE status = 'active' AND expires_at <= $1`, now)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToAddrOfStructByName[Preview])
}

func (s *Store) SetPreviewStatus(ctx context.Context, id int64, status string) error {
	_, err := s.db.Exec(ctx, `UPDATE previews SET status = $2, updated_at = now() WHERE id = $1`, id, status)
	return err
}

type AuditEvent struct {
	ID      int64           `db:"id" json:"id"`
	At      time.Time       `db:"at" json:"at"`
	Actor   string          `db:"actor" json:"actor"`
	Action  string          `db:"action" json:"action"`
	Object  string          `db:"object" json:"object"`
	Details json.RawMessage `db:"details" json:"details"`
}

// Audit records an event. Details must be JSON-serialisable.
func (s *Store) Audit(ctx context.Context, actor, action, object string, details map[string]any) error {
	if details == nil {
		details = map[string]any{}
	}
	_, err := s.db.Exec(ctx, `INSERT INTO audit_events (actor, action, object, details) VALUES ($1,$2,$3,$4)`, actor, action, object, details)
	return err
}

// AuditEvents returns events newest first. beforeID 0 starts at the newest.
func (s *Store) AuditEvents(ctx context.Context, beforeID int64, limit int) ([]AuditEvent, error) {
	rows, err := s.db.Query(ctx, `SELECT id, at, actor, action, object, details FROM audit_events
		WHERE ($1 = 0 OR id < $1) ORDER BY id DESC LIMIT $2`, beforeID, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[AuditEvent])
}

func (s *Store) DeleteOldAudit(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.db.Exec(ctx, `DELETE FROM audit_events WHERE at < $1`, before)
	return tag.RowsAffected(), err
}

// ExpirePreview sets a preview's expiry, e.g. to now so the worker retries its teardown.
func (s *Store) ExpirePreview(ctx context.Context, id int64, at time.Time) error {
	_, err := s.db.Exec(ctx, `UPDATE previews SET status = 'active', expires_at = $2, updated_at = now() WHERE id = $1`, id, at)
	return err
}

func (s *Store) Preview(ctx context.Context, id int64) (*Preview, error) {
	rows, err := s.db.Query(ctx, `SELECT `+previewCols+` FROM previews WHERE id = $1`, id)
	if err != nil {
		return nil, err
	}
	p, err := pgx.CollectExactlyOneRow(rows, pgx.RowToAddrOfStructByName[Preview])
	return p, notFound(err)
}
