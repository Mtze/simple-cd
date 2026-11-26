package store

import (
	"context"
	"time"

	"github.com/Mtze/simple-cd/internal/repoconfig"
	"github.com/jackc/pgx/v5"
)

type Artifact struct {
	ID              int64                  `db:"id" json:"id"`
	Repo            string                 `db:"repo" json:"repo"`
	SHA             string                 `db:"sha" json:"sha"`
	Source          string                 `db:"source" json:"source"`
	RefName         string                 `db:"ref_name" json:"refName"`
	PR              *int                   `db:"pr" json:"pr,omitempty"`
	PRClosed        bool                   `db:"pr_closed" json:"prClosed"`
	PRTitle         string                 `db:"pr_title" json:"prTitle"`
	RunID           int64                  `db:"run_id" json:"runId"`
	RunURL          string                 `db:"run_url" json:"runUrl"`
	Workflow        string                 `db:"workflow" json:"workflow"`
	TriggeringActor string                 `db:"triggering_actor" json:"triggeringActor"`
	OtherWorkflows  bool                   `db:"other_workflows" json:"otherWorkflows"`
	Manifest        repoconfig.Manifest    `db:"manifest" json:"manifest"`
	RepoConfig      *repoconfig.RepoConfig `db:"repo_config" json:"repoConfig,omitempty"`
	Files           map[string]*string     `db:"files" json:"-"`
	Reasons         []string               `db:"reasons" json:"reasons"`
	CreatedAt       time.Time              `db:"created_at" json:"createdAt"`
	UpdatedAt       time.Time              `db:"updated_at" json:"updatedAt"`
}

const artifactCols = `id, repo, sha, source, ref_name, pr, pr_closed, pr_title, run_id, run_url, workflow, triggering_actor,
	other_workflows, manifest, repo_config, files, reasons, created_at, updated_at`

// UpsertArtifact stores an artifact. For an existing SHA the newer run wins;
// an older run arriving late is ignored. It returns the stored row and
// whether the incoming data was applied.
func (s *Store) UpsertArtifact(ctx context.Context, a *Artifact) (*Artifact, bool, error) {
	if a.Reasons == nil {
		a.Reasons = []string{}
	}
	if a.Files == nil {
		a.Files = map[string]*string{}
	}
	rows, err := s.db.Query(ctx, `
		INSERT INTO artifacts (repo, sha, source, ref_name, pr, run_id, run_url, workflow, triggering_actor,
			manifest, repo_config, files, reasons, pr_title)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		ON CONFLICT (repo, sha) DO UPDATE SET
			-- A push run for a commit that is also in a PR must not hide the PR:
			-- previews only accept PR artifacts.
			source = CASE WHEN artifacts.source = 'pr' AND EXCLUDED.source = 'branch' THEN 'pr' ELSE EXCLUDED.source END,
			ref_name = CASE WHEN artifacts.source = 'pr' AND EXCLUDED.source = 'branch' THEN artifacts.ref_name ELSE EXCLUDED.ref_name END,
			pr = COALESCE(EXCLUDED.pr, artifacts.pr),
			pr_title = CASE WHEN EXCLUDED.pr_title <> '' THEN EXCLUDED.pr_title ELSE artifacts.pr_title END,
			run_id = EXCLUDED.run_id, run_url = EXCLUDED.run_url, workflow = EXCLUDED.workflow,
			triggering_actor = EXCLUDED.triggering_actor,
			other_workflows = artifacts.other_workflows OR artifacts.workflow <> EXCLUDED.workflow,
			manifest = EXCLUDED.manifest, repo_config = EXCLUDED.repo_config, files = EXCLUDED.files,
			reasons = EXCLUDED.reasons, updated_at = now()
		WHERE EXCLUDED.run_id > artifacts.run_id
		RETURNING `+artifactCols,
		a.Repo, a.SHA, a.Source, a.RefName, a.PR, a.RunID, a.RunURL, a.Workflow, a.TriggeringActor,
		a.Manifest, a.RepoConfig, a.Files, a.Reasons, a.PRTitle)
	if err != nil {
		return nil, false, err
	}
	got, err := pgx.CollectRows(rows, pgx.RowToAddrOfStructByName[Artifact])
	if err != nil {
		return nil, false, err
	}
	if len(got) == 1 {
		s.notify(ctx, Event{Kind: EventArtifacts, Project: a.Repo})
		return got[0], true, nil
	}
	existing, err := s.ArtifactBySHA(ctx, a.Repo, a.SHA)
	return existing, false, err
}

func (s *Store) Artifact(ctx context.Context, id int64) (*Artifact, error) {
	return s.oneArtifact(ctx, `SELECT `+artifactCols+` FROM artifacts WHERE id = $1`, id)
}

func (s *Store) ArtifactBySHA(ctx context.Context, repo, sha string) (*Artifact, error) {
	return s.oneArtifact(ctx, `SELECT `+artifactCols+` FROM artifacts WHERE repo = $1 AND sha = $2`, repo, sha)
}

func (s *Store) oneArtifact(ctx context.Context, sql string, args ...any) (*Artifact, error) {
	rows, err := s.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	a, err := pgx.CollectExactlyOneRow(rows, pgx.RowToAddrOfStructByName[Artifact])
	return a, notFound(err)
}

type ArtifactFilter struct {
	Source        string // "" for all
	IncludeClosed bool
	Limit         int
}

func (s *Store) ListArtifacts(ctx context.Context, repo string, f ArtifactFilter) ([]*Artifact, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	rows, err := s.db.Query(ctx, `SELECT `+artifactCols+` FROM artifacts
		WHERE repo = $1 AND ($2 = '' OR source = $2) AND ($3 OR NOT pr_closed)
		ORDER BY created_at DESC LIMIT $4`, repo, f.Source, f.IncludeClosed, f.Limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToAddrOfStructByName[Artifact])
}

func (s *Store) MarkPRClosed(ctx context.Context, repo string, pr int) error {
	_, err := s.db.Exec(ctx, `UPDATE artifacts SET pr_closed = true WHERE repo = $1 AND pr = $2`, repo, pr)
	if err == nil {
		s.notify(ctx, Event{Kind: EventArtifacts, Project: repo})
	}
	return err
}

// SetPRTitle keeps the title of a pull request current after it was renamed.
func (s *Store) SetPRTitle(ctx context.Context, repo string, pr int, title string) error {
	tag, err := s.db.Exec(ctx, `UPDATE artifacts SET pr_title = $3 WHERE repo = $1 AND pr = $2 AND pr_title <> $3`, repo, pr, title)
	if err == nil && tag.RowsAffected() > 0 {
		s.notify(ctx, Event{Kind: EventArtifacts, Project: repo})
	}
	return err
}

// SetArtifactReasons updates the target-independent reasons, e.g. after a
// registry recheck found a missing image.
func (s *Store) SetArtifactReasons(ctx context.Context, id int64, reasons []string) error {
	if reasons == nil {
		reasons = []string{}
	}
	_, err := s.db.Exec(ctx, `UPDATE artifacts SET reasons = $2 WHERE id = $1`, id, reasons)
	return err
}

// DeleteOldArtifacts removes PR artifacts of closed PRs older than closedBefore
// and branch/release artifacts beyond the newest keep per ref. Artifacts that
// are deployed somewhere (last successful or active deployment) are kept.
func (s *Store) DeleteOldArtifacts(ctx context.Context, closedBefore time.Time, keep int) (int64, error) {
	tag, err := s.db.Exec(ctx, `
		WITH protected AS (
			SELECT DISTINCT ON (target_id, slot) artifact_id FROM deployments
			WHERE kind = 'deploy' AND status = 'succeeded' AND artifact_id IS NOT NULL
			ORDER BY target_id, slot, finished_at DESC
		), active AS (
			SELECT artifact_id FROM deployments WHERE status IN ('pending','running') AND artifact_id IS NOT NULL
		), ranked AS (
			SELECT id, row_number() OVER (PARTITION BY repo, source, CASE WHEN source = 'release' THEN '' ELSE ref_name END ORDER BY created_at DESC) AS n
			FROM artifacts WHERE source <> 'pr'
		)
		DELETE FROM artifacts a
		WHERE a.id NOT IN (SELECT artifact_id FROM protected)
		  AND a.id NOT IN (SELECT artifact_id FROM active)
		  AND ( (a.source = 'pr' AND a.pr_closed AND a.updated_at < $1)
		     OR a.id IN (SELECT id FROM ranked WHERE n > $2) )`, closedBefore, keep)
	return tag.RowsAffected(), err
}
