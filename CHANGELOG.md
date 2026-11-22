# Changelog

All notable changes to this project are documented in this file.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Live updates: the project page reloads its artifacts as soon as a new build arrives, through `GET /api/events` (Server-Sent Events) fed by Postgres `NOTIFY`, so it works across replicas. Polling stays as a fallback.

## [0.1.0] - 2026-11-20

### Added

- Delivery server in Go with an embedded React UI.
- Target types: Helm (charts from OCI registries), Docker Compose (one file on a VM over SSH) and Ansible playbooks.
- Target configuration in YAML with strict validation, `simple-cd validate` and reload without restart; an invalid file never becomes active and shows a banner to platform admins.
- GitHub App integration: signed `workflow_run` and `pull_request` webhooks, fork rejection, source detection (pull request, branch, release), `.cd.yaml` snapshot per commit, deployments reported to GitHub.
- `cd-publish` GitHub Action that writes and uploads `deploy.json`.
- Readiness checks per artifact and target, including registry existence checks for image digests and chart versions, repeated right before each deploy.
- Confirmation diff of Helm values and Compose files against the last successful deployment.
- Opt-in locks with takeover and history, maintenance mode, one deployment per slot.
- Preview environments: one namespace per pull request, labelled and checked, with a TTL and teardown on PR close.
- Auto-deploy requested in `deploy.json` and allowed by a rule on the target, with a waiting place per slot where the newest artifact wins.
- Keycloak login (OIDC with PKCE), roles from client roles in the ID token: project developer and maintainer, optional target roles, platform admin.
- Executor as a hardened Kubernetes Job that reads its secrets from Vault (Kubernetes auth); a process runner for local development.
- Live deployment logs over Server-Sent Events, stored logs with retention.
- Audit log, retention for artifacts, logs and audit events, leader-elected background worker.
- Helm chart for simple-cd, container images for the server and both executors, CI.
- Documentation site with UML diagrams.
- End-to-end test in a kind cluster with Vault, Keycloak, a local registry and a fake GitHub API (`make e2e-up e2e`), run in CI.
- Docs page "Why simple-cd" with the intended users, reasons, limits and a sourced comparison to Argo CD, Flux, Kargo, Spinnaker, Octopus Deploy, GitHub and GitLab environments, Semaphore UI, Kamal, Coolify and Harness; a short version in the README.
- Screenshots of the UI in light and dark, in the README and the user and admin docs, taken with `make screenshots`.
- README quick start: try it locally, and install on Kubernetes in four steps.
- `AGENTS.md` (imported by `CLAUDE.md`) with the map, commands, rules and security invariants for coding agents, and project skills in `.claude/skills/`: `verify-change`, `e2e`, `security-invariants`, `architecture-diagrams`, `screenshots` and `release`.

### Changed

- **Breaking:** the Helm chart exposes simple-cd with a Gateway API `HTTPRoute` (`httpRoute.*`) instead of an `Ingress`; the `ingress.*` values are gone. The end-to-end test installs the Gateway API CRDs and validates the route.
- Light theme: darker page background, stronger borders and card shadows, and a framed readiness list per artifact, so cards and the targets of an artifact read as groups.
- Target cards in one row keep their buttons on one line at the bottom, whatever the card shows above them.
- `SIMPLECD_REGISTRY_PLAIN_HTTP` for registries without TLS in test setups, and `extraEnv` in the Helm chart.

### Security

Fixes from the security audit of 2026-11-20.

- A push counts as a release only when the tag of that name points at the built commit; before, a branch named like an existing tag was stored as a release and could auto-deploy to release targets.
- The registry existence check only contacts registries on the project's allowlist, follows redirects only within the same host and scheme and fetches tokens only from `https` realms; before, `deploy.json` could make the server send requests to any host.
- Image and chart repositories must follow the OCI name grammar (no `..`), and registry prefixes match whole path components, so `ghcr.io/example` no longer allows `ghcr.io/example-evil/`.
- The `return` target after login refuses control characters and backslashes, which browsers turned into a redirect to another site.
- **Breaking:** login state (`state`, PKCE verifier, nonce) moved from the `auth_requests` table into a 10-minute login cookie, so only the browser that started a login can finish it and anonymous login requests store nothing. Migration `002` drops the table; logins started before the upgrade must be retried.
- **Breaking:** users without any client role of simple-cd can no longer log in; give read-only users a role such as `viewer` (the dev realm gives one to `guest`). The usernames `system`, `auto-deploy` and `github` are refused.
- Log streams normalize carriage returns, so log output cannot forge `status` or `end` events.
- The process runner refuses to start with an `https://` base URL.
- `go-jose` 4.1.6 (three advisories, not reachable from simple-cd).
- Docs: Ansible `source: repo` runs repository code with access to all deployment secrets, and how to end a user's sessions at once.
