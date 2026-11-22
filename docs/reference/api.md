---
title: HTTP API
parent: Reference
nav_order: 4
---

# HTTP API
{: .no_toc }

The UI talks to the server through this JSON API. All endpoints live under
`/api` and need a session cookie, except the login endpoints, the health
checks and the GitHub webhook.

1. TOC
{:toc}

## Conventions

- Times are RFC 3339 strings in UTC.
- Unauthenticated requests get `401 {"error": "unauthenticated"}`. The UI then
  navigates to `/auth/login?return=<current path>`. With an existing Keycloak
  SSO session the user is back without seeing a login form.
- Requests that change something (`POST`, `PUT`, `DELETE`) must send the
  header `X-Requested-With: simple-cd`. If the request carries an `Origin`
  header, it must match `SIMPLECD_BASE_URL`. Both protect against cross-site
  requests; failures get `403` with `code: "csrf"`.
- Request bodies are JSON, at most 64 KiB, and unknown fields are rejected
  (`400`, `code: "bad_request"`).
- API responses carry `Cache-Control: no-store`.
- Errors use `{"error": "<message>", "code": "<code>"}` plus extra fields
  described per endpoint.
- Every response for a target includes `permissions`. The UI hides controls
  the user cannot use; the server enforces the same rules.

## Error codes

| Status | `code` | Meaning |
|---|---|---|
| 400 | `bad_request` | invalid body or query, or a missing maintenance reason |
| 401 | `unauthenticated` | no valid session |
| 403 | `forbidden` | missing permission |
| 403 | `csrf` | missing `X-Requested-With` header or foreign `Origin` |
| 404 | `not_found` | unknown project, target, artifact, deployment, preview or endpoint |
| 409 | `confirmation_required`, `locked`, `maintenance`, `slot_busy`, `not_active` | see the endpoints |
| 422 | `not_deployable` | the artifact cannot go to the target, with `reasons` |
| 500 | `internal` | unexpected error, details in the server log |
| 502 | `registry_unavailable` | the registry check before a deploy could not run |

## Types

```ts
type Permissions = { deploy: boolean; override: boolean; maintenance: boolean };

type TargetStatus = "maintenance" | "deploying" | "locked" | "available";

type Target = {
  id: string;
  project: string;               // "org/app"
  type: "helm" | "compose" | "ansible";
  preview: boolean;
  lockable: boolean;
  lockDurationSeconds: number;
  role?: string;                 // extra Keycloak role required to deploy
  autoDeploy?: { from: ("pr" | "branch" | "release")[]; branches?: string[] };
  status: TargetStatus;
  maintenance?: { reason: string; by: string; since: string };
  lock?: { holder: string; until: string };
  current?: Deployed;            // last successful deploy (non-preview targets)
  active: Deployment[];          // pending or running
  permissions: Permissions;
};

type Deployed = {
  deploymentId: number;
  artifactId?: number;
  sha?: string;
  refName?: string;
  source?: "pr" | "branch" | "release";
  pr?: number;
  actor: string;
  finishedAt: string;
};

type Deployment = {
  id: number;
  kind: "deploy" | "teardown";
  artifactId?: number;
  targetId: string;
  slot: string;                  // "" or the PR number for previews
  actor: string;                 // username, "auto-deploy" or "system"
  actorDetail?: string;          // GitHub user who triggered an auto-deploy
  status: "pending" | "running" | "succeeded" | "failed" | "timed_out";
  error?: string;
  createdAt: string;
  startedAt?: string;
  finishedAt?: string;
  artifact?: ArtifactBrief;
};

type ArtifactBrief = {
  id: number; repo: string; sha: string; source: "pr" | "branch" | "release";
  refName: string; pr?: number;
};

type Artifact = ArtifactBrief & {
  prClosed: boolean;
  runUrl: string;
  workflow: string;
  triggeringActor: string;
  otherWorkflows: boolean;       // show a warning: several workflows publish for this SHA
  images: { name: string; ref: string }[];
  helmVersion?: string;
  autoDeploy: string[];
  createdAt: string;
  readiness: Record<string, string[]>; // target id -> reasons; [] means deployable
  deployedTo: string[];          // targets where this is the last successful deploy
};

type Preview = {
  id: number; repo: string; pr: number; targetId: string; namespace: string;
  status: "active" | "deleting" | "deleted";
  expiresAt: string; createdAt: string; updatedAt: string;
  orphaned: boolean;             // target no longer in the config
  canTeardown: boolean;
};

type User = { username: string; name: string; email: string; roles: string[] };
```

## Endpoints

### `GET /api/me`

```json
{ "user": { "username": "alice", "name": "Alice", "email": "a@x", "roles": ["project:org/app:developer"] },
  "isAdmin": false,
  "configError": "only for platform admins, when the last config reload failed" }
```

### `GET /api/projects`

`[{ "name": "org/app", "role": "developer" | "maintainer" | "", "targets": Target[], "livePreviews": 3 }]`

### `GET /api/projects/{owner}/{repo}`

`{ "name": "org/app", "role": "...", "targets": Target[], "previews": Preview[] }`

### `GET /api/projects/{owner}/{repo}/artifacts?source=pr|branch|release&closed=true`

`Artifact[]`, newest first, at most 100. Without `closed=true`, artifacts of
closed PRs are hidden. `source` is optional; an unknown source gets `400`.

### `GET /api/artifacts/{id}`

`Artifact & { "deployments": Deployment[] }`

### `GET /api/targets/{id}`

`{ "target": Target, "deployments": Deployment[], "lockHistory": LockEvent[] }`
with the last 50 deployments and the last 20 lock events, newest first:

```ts
type LockEvent = {
  id: number; targetId: string; holder: string;
  action: "acquired" | "extended" | "released" | "overridden";
  byUser: string; until?: string; at: string;
};
```

### `GET /api/targets/{id}/plan?artifact={artifactId}`

What the deploy dialog shows before confirming:

```json
{ "targetId": "app-test", "artifactId": 12, "slot": "", "status": "locked",
  "reasons": [], "diff": "--- deployed\n+++ abc1234\n@@ ...", "needsOverride": true,
  "lockHolder": "bob", "lockUntil": "...", "permissions": { ... } }
```

`diff` is empty when nothing but images changed, for the first deploy in the
slot, or when `reasons` is not empty. `needsOverride` is true when someone else
holds the lock. The plan changes nothing; `artifact` is required (`400`).

### `POST /api/targets/{id}/deployments`

Body `{ "artifactId": 12, "confirm": true, "override": false }`.
`confirm` says the user saw the diff, `override` takes over someone else's lock
(maintainers and platform admins).

- `201` with the `Deployment` (without `artifact`).
- `403` `code: "forbidden"`.
- `404` `code: "not_found"` for an unknown target or artifact.
- `409` with one of:
  - `code: "confirmation_required"`, `diff`
  - `code: "locked"`, `holder`, `until`
  - `code: "maintenance"`
  - `code: "slot_busy"`
- `422` `code: "not_deployable"`, `reasons: string[]`
- `502` `code: "registry_unavailable"`

### `POST /api/targets/{id}/lock/release`

`204`. The holder, a maintainer or a platform admin. Releasing a target that
is not locked is a no-op.

### `PUT /api/targets/{id}/maintenance`

Body `{ "enabled": true, "reason": "Docker upgrade" }`. `204`. Maintainers and
platform admins. Switching it on without a reason gets `400`.

### `GET /api/events`

Server-Sent Events that tell the UI to reload, so new builds show up without waiting for the next poll:

- `event: change` - `data` is `{"kind": "artifacts", "project": "<owner>/<repo>"}`: the artifact list of the project changed (a new build, a newer run of a commit, or a closed pull request).

The stream starts with the comment `: connected` and sends `: keepalive` every 25 seconds.
Events carry no data beyond the project name; the UI reloads through the normal endpoints.
A slow reader may miss events, and none are replayed after a reconnect, so clients keep polling as a fallback.

### `GET /api/deployments/{id}`

`Deployment` with `artifact` and `target: Target`.

### `GET /api/deployments/{id}/log`

Server-Sent Events:

- `event: log` - `data` is one log line
- `event: status` - `data` is the deployment status
- `event: end` - no more events

For finished deployments the stored log is sent at once, followed by `end`.

### `DELETE /api/previews/{id}`

`202`. Schedules the teardown of a preview. Needs the deploy permission on
the target, or a platform admin. For an orphaned preview (target no longer in
the config) only platform admins may call it, and it only marks the preview as
deleted; the namespace has to be removed by hand. `409` `code: "not_active"` if
the preview is not active.

### `GET /api/admin/audit?before={id}` (platform admin)

`AuditEvent[]`, 100 per page, newest first: `{ id, at, actor, action, object, details }`.
Pass the smallest `id` of a page as `before` to get the next page.

### `GET /api/admin/previews` (platform admin)

All live previews including orphaned ones.

### Auth

- `GET /auth/login?return=/path` starts the OIDC login (authorization code with PKCE).
- `GET /auth/callback` finishes it and sets the session cookie.
- `POST /auth/logout` ends the session and returns `{ "logoutUrl": "..." }`, the
  Keycloak end-session URL the UI navigates to. It needs the
  `X-Requested-With: simple-cd` header like the API.

### Other

- `GET /healthz` liveness, always `200 ok`.
- `GET /readyz` readiness: `200 ok` when Postgres answers within 2 seconds, else `503`.
- `POST /webhooks/github` GitHub App webhook, verified with
  `X-Hub-Signature-256`. `202` for `workflow_run` and `pull_request` (processed
  in the background), `204` for other events, `401` for a bad signature.
  Bodies up to 5 MiB.
- Every other path serves the UI; unknown paths get the UI's `index.html`.
