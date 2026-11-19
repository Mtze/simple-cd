---
title: HTTP API
parent: Reference
---

# HTTP API

The UI talks to the server through this JSON API. All endpoints live under
`/api` and need a session cookie, except the health checks and the GitHub
webhook.

## Conventions

- Times are RFC 3339 strings in UTC.
- Unauthenticated requests get `401 {"error": "unauthenticated"}`. The UI then
  navigates to `/auth/login?return=<current path>`. With an existing Keycloak
  SSO session the user is back without seeing a login form.
- Requests that change something (`POST`, `PUT`, `DELETE`) must send the
  header `X-Requested-With: simple-cd`. The server also checks the `Origin`
  header. Both protect against cross-site requests.
- Errors use `{"error": "<message>", "code": "<code>"}` plus extra fields
  described per endpoint.
- Every response for a target includes `permissions`. The UI hides controls
  the user cannot use; the server enforces the same rules.

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

`Artifact[]`, newest first. Without `closed=true`, artifacts of closed PRs are hidden.

### `GET /api/artifacts/{id}`

`Artifact & { "deployments": Deployment[] }`

### `GET /api/targets/{id}`

`{ "target": Target, "deployments": Deployment[], "lockHistory": LockEvent[] }` with
`LockEvent = { id, targetId, holder, action: "acquired"|"extended"|"released"|"overridden", byUser, until?, at }`.

### `GET /api/targets/{id}/plan?artifact={artifactId}`

What the deploy dialog shows before confirming:

```json
{ "targetId": "app-test", "artifactId": 12, "slot": "", "status": "locked",
  "reasons": [], "diff": "--- deployed\n+++ abc1234\n@@ ...", "needsOverride": true,
  "lockHolder": "bob", "lockUntil": "...", "permissions": { ... } }
```

`diff` is empty when nothing but images changed or for the first deploy.

### `POST /api/targets/{id}/deployments`

Body `{ "artifactId": 12, "confirm": true, "override": false }`.

- `201` with the `Deployment`.
- `403` `code: "forbidden"`.
- `409` with one of:
  - `code: "confirmation_required"`, `diff`
  - `code: "locked"`, `holder`, `until`
  - `code: "maintenance"`
  - `code: "slot_busy"`
- `422` `code: "not_deployable"`, `reasons: string[]`

### `POST /api/targets/{id}/lock/release`

`204`. The holder, a maintainer or a platform admin.

### `PUT /api/targets/{id}/maintenance`

Body `{ "enabled": true, "reason": "Docker upgrade" }`. `204`. Maintainers and platform admins.

### `GET /api/deployments/{id}`

`Deployment` with `artifact` and `target: Target`.

### `GET /api/deployments/{id}/log`

Server-Sent Events:

- `event: log` - `data` is one log line
- `event: status` - `data` is the deployment status
- `event: end` - no more events

For finished deployments the stored log is sent at once, followed by `end`.

### `DELETE /api/previews/{id}`

`202`. Schedules the teardown of a preview.

### `GET /api/admin/audit?before={id}` (platform admin)

`AuditEvent[]`, 100 per page, newest first: `{ id, at, actor, action, object, details }`.

### `GET /api/admin/previews` (platform admin)

All live previews including orphaned ones.

### Auth

- `GET /auth/login?return=/path` starts the OIDC login (authorization code with PKCE).
- `GET /auth/callback` finishes it and sets the session cookie.
- `POST /auth/logout` ends the session and returns `{ "logoutUrl": "..." }`, the
  Keycloak end-session URL the UI navigates to.

### Other

- `GET /healthz` liveness, `GET /readyz` readiness (database reachable).
- `POST /webhooks/github` GitHub App webhook, verified by signature.
