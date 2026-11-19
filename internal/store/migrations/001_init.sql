CREATE TABLE artifacts (
    id               BIGSERIAL PRIMARY KEY,
    repo             TEXT        NOT NULL,
    sha              TEXT        NOT NULL,
    source           TEXT        NOT NULL CHECK (source IN ('pr', 'branch', 'release')),
    ref_name         TEXT        NOT NULL,            -- branch or tag
    pr               INTEGER,
    pr_closed        BOOLEAN     NOT NULL DEFAULT false,
    run_id           BIGINT      NOT NULL,
    run_url          TEXT        NOT NULL DEFAULT '',
    workflow         TEXT        NOT NULL DEFAULT '',
    triggering_actor TEXT        NOT NULL DEFAULT '',
    other_workflows  BOOLEAN     NOT NULL DEFAULT false, -- another workflow also published for this SHA
    manifest         JSONB       NOT NULL,
    repo_config      JSONB,                            -- parsed .cd.yaml, NULL if missing
    files            JSONB       NOT NULL DEFAULT '{}', -- repo path -> content (NULL = missing)
    reasons          JSONB       NOT NULL DEFAULT '[]', -- target-independent incompleteness reasons
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (repo, sha)
);
CREATE INDEX artifacts_repo_created ON artifacts (repo, created_at DESC);

CREATE TABLE deployments (
    id                   BIGSERIAL PRIMARY KEY,
    kind                 TEXT        NOT NULL DEFAULT 'deploy' CHECK (kind IN ('deploy', 'teardown')),
    artifact_id          BIGINT REFERENCES artifacts (id) ON DELETE SET NULL,
    target_id            TEXT        NOT NULL,
    slot                 TEXT        NOT NULL DEFAULT '', -- '' or the PR number for previews
    actor                TEXT        NOT NULL,            -- username or 'auto-deploy' / 'system'
    actor_detail         TEXT        NOT NULL DEFAULT '', -- GitHub triggering actor for auto-deploys
    status               TEXT        NOT NULL CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'timed_out')),
    job_name             TEXT        NOT NULL DEFAULT '',
    snapshot             TEXT        NOT NULL DEFAULT '', -- compose file or values without secrets, for diffs
    log                  TEXT,
    error                TEXT        NOT NULL DEFAULT '',
    github_deployment_id BIGINT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at           TIMESTAMPTZ,
    finished_at          TIMESTAMPTZ
);
-- One active deployment per slot. Previews use the PR number as slot.
CREATE UNIQUE INDEX deployments_one_active ON deployments (target_id, slot) WHERE status IN ('pending', 'running');
CREATE INDEX deployments_target_created ON deployments (target_id, created_at DESC);

-- Waiting slot for auto-deploys: one entry per slot, the newest wins.
CREATE TABLE deploy_queue (
    target_id    TEXT        NOT NULL,
    slot         TEXT        NOT NULL,
    artifact_id  BIGINT      NOT NULL REFERENCES artifacts (id) ON DELETE CASCADE,
    actor_detail TEXT        NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (target_id, slot)
);

CREATE TABLE target_state (
    target_id          TEXT PRIMARY KEY,
    maintenance        BOOLEAN NOT NULL DEFAULT false,
    maintenance_reason TEXT    NOT NULL DEFAULT '',
    maintenance_by     TEXT    NOT NULL DEFAULT '',
    maintenance_since  TIMESTAMPTZ,
    lock_holder        TEXT    NOT NULL DEFAULT '',
    lock_until         TIMESTAMPTZ
);

CREATE TABLE lock_history (
    id            BIGSERIAL PRIMARY KEY,
    target_id     TEXT        NOT NULL,
    holder        TEXT        NOT NULL,
    action        TEXT        NOT NULL CHECK (action IN ('acquired', 'extended', 'released', 'overridden')),
    by_user       TEXT        NOT NULL,
    until_at      TIMESTAMPTZ,
    at            TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX lock_history_target ON lock_history (target_id, at DESC);

CREATE TABLE previews (
    id         BIGSERIAL PRIMARY KEY,
    repo       TEXT        NOT NULL,
    pr         INTEGER     NOT NULL,
    target_id  TEXT        NOT NULL,
    namespace  TEXT        NOT NULL,
    status     TEXT        NOT NULL CHECK (status IN ('active', 'deleting', 'deleted')),
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX previews_one_live ON previews (target_id, pr) WHERE status <> 'deleted';

CREATE TABLE audit_events (
    id      BIGSERIAL PRIMARY KEY,
    at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    actor   TEXT        NOT NULL,
    action  TEXT        NOT NULL,
    object  TEXT        NOT NULL DEFAULT '',
    details JSONB       NOT NULL DEFAULT '{}'
);
CREATE INDEX audit_events_at ON audit_events (at DESC);

-- Sessions are stored by the SHA-256 of the cookie value, so a database leak
-- does not leak usable session cookies.
CREATE TABLE sessions (
    id_hash    TEXT PRIMARY KEY,
    subject    TEXT        NOT NULL,
    username   TEXT        NOT NULL,
    name       TEXT        NOT NULL DEFAULT '',
    email      TEXT        NOT NULL DEFAULT '',
    roles      JSONB       NOT NULL DEFAULT '[]',
    id_token   TEXT        NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE auth_requests (
    state      TEXT PRIMARY KEY,
    verifier   TEXT        NOT NULL,
    nonce      TEXT        NOT NULL,
    return_to  TEXT        NOT NULL DEFAULT '/',
    expires_at TIMESTAMPTZ NOT NULL
);
