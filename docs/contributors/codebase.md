---
title: Codebase
parent: Contributors
nav_order: 2
---

# Codebase
{: .no_toc }

1. TOC
{:toc}

## Repository layout

| Path | Content |
|---|---|
| `cmd/simple-cd` | the server command: `serve`, `validate`, `migrate`, reads all `SIMPLECD_*` settings |
| `cmd/simple-cd-executor` | the executor command: reads `SIMPLECD_SPEC` and runs one deployment |
| `internal/` | all Go packages, see below |
| `web/` | the React UI (Vite, TypeScript); `web/embed.go` embeds `web/dist` into the server |
| `cd-publish/` | the GitHub Action: `action.yml`, `publish.sh` and its tests `test.sh` |
| `deploy/helm/simple-cd` | the Helm chart of simple-cd |
| `build/` | Dockerfiles of the server and the two executor images |
| `dev/` | local stack: `compose.yaml` (Postgres, Keycloak), `keycloak-realm.json`, `config.yaml`, `seed/` |
| `e2e/` | the [end-to-end test](testing.md#end-to-end-test): kind config, manifests, `up.sh`/`down.sh`, the fake GitHub API, a test chart and `e2e_test.go` |
| `docs/` | this site (Jekyll, just-the-docs), the draw.io UML views in `docs/architecture/uml` and the PlantUML dynamic views |
| `.github/workflows/` | CI, image builds and the docs deployment |
| `Makefile` | common tasks, `make help` lists them |

## Packages

The packages form layers: `api` and `worker` call `core`, which holds the rules and uses `store`; `ingest` turns webhooks into artifacts.
The [component diagram](../architecture/index.md#components) shows the same from above.

| Package | Responsibility |
|---|---|
| `config` | parses and validates the target configuration (`Parse`, `Validate`), applies defaults, and `Holder` reloads the file every 10 seconds, keeping the last good version and the last error |
| `repoconfig` | what a repository contributes: `.cd.yaml` (`ParseRepoConfig`), `deploy.json` (`ParseManifest`), image references, placeholders (`Render`, `RenderSet`), the values merge (`Merge`) and the Compose checks (`CheckCompose`). Pure functions shared by server and executor. |
| `core` | the domain rules: permissions (`PermissionsFor`), target status (`TargetStatus`), slots, preview namespaces and labels, readiness (`Readiness`), the diff snapshot, `PlanDeploy`, `Deploy`, `AutoDeploy`, the waiting place (`ProcessQueue`), `ReleaseLock`, `SetMaintenance`, preview teardown and `BuildSpec`, which turns a deployment into the executor's input |
| `store` | Postgres access with `pgx`: embedded SQL migrations, artifacts, deployments, the waiting place, target state, lock history, previews, audit events, sessions. `WithTx` runs a function in a transaction; `LockTargetState` takes the row lock that serializes all decisions about one target. |
| `ingest` | handles `workflow_run` and `pull_request` webhooks: rejects forks and unknown repositories, downloads `deploy.json`, derives the source, snapshots `.cd.yaml` and the files it names, checks the registry, stores the artifact and triggers auto-deploy |
| `github` | the GitHub App client: webhook signature check, App JWT, cached installation tokens, artifact download, file reads, PR lookup, tag check, and the `Reporter` that mirrors deployments to GitHub |
| `registry` | checks that a digest or chart version exists in an OCI registry with `HEAD /v2/<repo>/manifests/<ref>`, following the token flow; reads credentials from a Docker `config.json` |
| `auth` | Keycloak login: authorization code flow with PKCE, nonce and one-time state, sessions in Postgres keyed by the SHA-256 of the cookie, RP-initiated logout, the middleware that loads the user |
| `api` | the HTTP server: JSON API, SSE log stream, webhook endpoint, login routes, health checks, the embedded UI, security headers and CSRF protection. `views.go` shapes the JSON. |
| `worker` | background work on the leader replica (Postgres advisory lock): start pending deployments, watch running Jobs, enforce timeouts, store logs, report to GitHub, process the waiting place, retry preview teardowns, apply retention |
| `runner` | starts and watches executor runs. `Kube` creates Jobs over the Kubernetes REST API (no client-go); `Process` runs the executor as a local process for development. |
| `executor` | runs inside the Job: Vault login and secret reads, then `helm` (deploy and preview teardown), `docker compose` over `ssh`, or `ansible-playbook` |
| `diff` | a small line-based unified diff for the confirmation dialog |

## Following a deployment through the code

1. `api.Server.deploy` decodes the request and calls `core.Service.Deploy`.
2. `Deploy` checks `PermissionsFor`, `Readiness`, `RecheckRegistry` and the diff, then in one transaction `LockTargetState`, maintenance, the lock, `InsertDeployment` (the unique index on target and slot rejects a busy slot) and the lock update.
3. `worker.Worker.startPending` loads the pending row, calls `core.BuildSpec` and `runner.Kube.Start`, marks it running and creates the GitHub deployment.
4. In the Job, `executor.Run` logs in to Vault and runs the step for the target type.
5. `api.Server.deploymentLog` streams `runner.Logs` to the browser as Server-Sent Events.
6. `worker.Worker.checkRunning` sees the Job finish, stores status and log with `store.Finish`, updates preview bookkeeping and reports to GitHub.

The [sequence diagrams](../architecture/flows.md) show the same steps.

## UI

The UI is a single page app without a router library or state manager:

| Path | Content |
|---|---|
| `web/src/api/` | `client.ts` (fetch wrapper that sends `X-Requested-With`, redirects to login on 401) and `types.ts` (mirrors the [HTTP API](../reference/api.md)) |
| `web/src/pages/` | Projects, Project, Target, Deployment, Admin and Not found |
| `web/src/components/` | target card, deploy dialog, diff view, SSE log viewer, tables, modal, user menu |
| `web/src/lib/` | a small router, polling `useApi`, the current user context, theme and time helpers |

Keep `web/src/api/types.ts` and `docs/reference/api.md` in sync with `internal/api`.
