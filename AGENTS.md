# AGENTS.md

Instructions for coding agents working on simple-cd.
Humans start with [CONTRIBUTING.md](CONTRIBUTING.md); the full contributor docs are in `docs/contributors/`.

simple-cd is a small continuous delivery server: GitHub Actions build and publish `deploy.json`, simple-cd stores the artifact, checks readiness and lets authorized people deploy it to Helm, Docker Compose and Ansible targets through an executor Kubernetes Job that reads its secrets from Vault.
Go server with an embedded React UI, Postgres, Keycloak (OIDC), a GitHub App, a Helm chart, docs on GitHub Pages (<https://mtze.me/simple-cd/>).

## Map

| Path | What |
|---|---|
| `cmd/simple-cd`, `cmd/simple-cd-executor` | server and executor commands, all `SIMPLECD_*` settings |
| `internal/core` | domain rules: permissions, readiness, slots, locks, deploy, auto-deploy |
| `internal/store` | Postgres with `pgx`, embedded migrations in `internal/store/migrations/NNN_*.sql` |
| `internal/api`, `internal/auth` | HTTP API, SSE log stream, webhooks, login and sessions |
| `internal/ingest`, `internal/github`, `internal/registry` | webhooks to artifacts, GitHub App client, OCI existence checks |
| `internal/worker`, `internal/runner`, `internal/executor` | background loop, Job creation, the code that runs inside the Job |
| `internal/config`, `internal/repoconfig` | target configuration; what a repository contributes (`.cd.yaml`, `deploy.json`) |
| `web/` | React UI (Vite, TypeScript), embedded from `web/dist` |
| `deploy/helm/simple-cd` | Helm chart (Gateway API `HTTPRoute`) |
| `e2e/` | end-to-end test in kind with Vault, Keycloak, a registry and a fake GitHub |
| `docs/` | Jekyll site; UML in `docs/architecture/uml`, PlantUML in `docs/architecture/diagrams`, ADRs in `docs/adr` |

`docs/contributors/codebase.md` explains every package and follows one deployment through the code.

## Commands

```sh
make help                        # all targets
make lint                        # gofmt and go vet
make test-go                     # go test -race ./... (DB tests use the dev Postgres from make dev-up)
make test-web                    # UI typecheck and Vitest
bash cd-publish/test.sh          # the GitHub Action
helm lint deploy/helm/simple-cd --set github.appID=1
go run ./cmd/simple-cd validate dev/config.yaml
make dev-up dev-seed web run     # local stack on http://127.0.0.1:8080, users alice/bob/mia/root/guest, password dev
make e2e-up e2e                  # full end-to-end run in kind (slow, needs Docker)
```

Tests that need Postgres read `SIMPLECD_TEST_DATABASE_URL` and are **skipped silently** without it.
A green `go test` without that variable proves little for `store`, `core`, `api`, `auth`, `ingest` and `worker`.

Bind local services to `127.0.0.1`, not `localhost`, and report URLs with `127.0.0.1`.

## Rules

- **Small and few dependencies.** Standard library plus `pgx`, `go-oidc`, `oauth2` and `yaml`. Kubernetes, Vault, GitHub and registries are reached over plain HTTP, not SDKs. React only in the UI.
- **Tests for every behaviour change.** Check that a new test fails without the fix. Before changing an existing test, verify the test is wrong and not the code.
- **Docs in the same change.** Update `docs/`, the **Unreleased** section of `CHANGELOG.md` and, for API changes, both `docs/reference/api.md` and `web/src/api/types.ts`.
- **New settings** go into `docs/reference/environment.md`; new target fields into `internal/config` (struct and `Validate`) and `docs/admin/configuration.md`; new chart values into `values.yaml` and `docs/admin/installation.md`.
- **Database changes** are a new numbered migration; never edit an applied one.
- **Decisions** about structure, technology or protocols get an ADR in `docs/adr/` and a line in `docs/adr/index.md`.
- **Writing style:** active voice, present tense, short sentences. Never use em or en dashes, only a plain hyphen. Say "UI" and "server". Use the terms from the terminology table in `docs/index.md` (target, project, artifact, deployment, slot, lock, maintenance, preview, auto-deploy, executor, platform admin, developer, maintainer).
- **Comments** explain why, for humans. A known limit gets `shortcut: <the limit>, <when to upgrade>`.

## Security invariants

These held in the security audit of 2026-11-20; keep them when you change the code around them.
The `security-invariants` skill has the full checklist.

- Provenance (repository, commit, source, PR number) comes from the signed webhook and the GitHub API, never from `deploy.json`. A push is a release only when the tag points at the built commit.
- Nothing from a repository makes the server contact a host before the registry allowlist accepted it.
- The server never reads deployment secrets; only the executor Job reads them from Vault.
- Every state-changing route checks the role on the server, and `POST`/`PUT`/`DELETE` need `X-Requested-With: simple-cd`.
- Login needs a client role; login state lives in a per-login cookie; `return` only accepts local paths.

## Skills

`.claude/skills/` holds step-by-step procedures for recurring work.
Claude Code loads them as project skills; other agents can read the `SKILL.md` files directly.

| Skill | Use it to |
|---|---|
| `verify-change` | run every check CI runs, with the database, before you call a change done |
| `e2e` | start, run, debug or extend the end-to-end test in kind |
| `security-invariants` | review a change that touches login, webhooks, registries, Vault, the executor or the API |
| `architecture-diagrams` | change the UML or PlantUML views and keep them readable |
| `screenshots` | refresh the UI screenshots in the README and docs |
| `release` | cut a release: changelog, chart version, tag, images |

## Git

- Commit messages: imperative subject line, a body that says why.
- Ask before committing, pushing, tagging or publishing anything.
