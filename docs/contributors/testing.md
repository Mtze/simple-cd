---
title: Testing
parent: Contributors
nav_order: 3
---

# Testing

CI (`.github/workflows/ci.yml`) runs every check below on each push to `main` and on every pull request.

## Go

```sh
make dev-up     # provides Postgres on 127.0.0.1:5434
make test-go    # go test -race ./...
make lint       # gofmt and go vet
```

Tests that need a database read `SIMPLECD_TEST_DATABASE_URL` and are **skipped** without it.
`make test-go` points it at the development database unless you set it yourself.
Each test gets a fresh, migrated Postgres schema that is dropped afterwards, so tests can run in parallel against one database.

CI runs `go test -race -count=1 ./...` against Postgres 18 and also checks `go run ./cmd/simple-cd validate dev/config.yaml`.

Where the tests live:

| Area | Tests |
|---|---|
| Domain rules (permissions, status precedence, readiness, locks, maintenance, diff confirmation, registry recheck, auto-deploy waiting place, PR close) | `internal/core/core_test.go` |
| Executor (Helm, Compose, Ansible commands, secret handling) | `internal/executor/executor_test.go`, with fake `helm`, `kubectl`, `ssh`, `git`, `ansible-playbook` and Vault |
| Job manifest and process runner | `internal/runner/runner_test.go` |
| Worker loop | `internal/worker/worker_test.go` |
| HTTP API, CSRF, security headers | `internal/api/server_test.go` |
| Login and sessions | `internal/auth/auth_test.go` |
| Config, `.cd.yaml`, `deploy.json` | `internal/config`, `internal/repoconfig` |
| GitHub, registry, ingest, store, diff | the `_test.go` file of each package |

## UI

```sh
make test-web            # npm run typecheck && npm test
cd web && npx vitest     # watch mode
```

The UI tests use Vitest with jsdom and Testing Library (`web/src/**/*.test.ts(x)`).
CI also runs `npm run build`.

## cd-publish action

```sh
bash cd-publish/test.sh
```

The script feeds valid and invalid inputs to `publish.sh` and compares the generated `deploy.json`, including JSON injection attempts.

## Helm chart

```sh
helm lint deploy/helm/simple-cd --set github.appID=1
helm template ci deploy/helm/simple-cd --set github.appID=1 --set ingress.enabled=true --set registryAuth=true
```

## Docs

```sh
python3 docs/architecture/uml/diagrams.py   # structural UML views (needs the drawio CLI)
docs/architecture/diagrams/render.sh       # sequence and state diagrams (PlantUML in Docker)
```

The docs site builds with GitHub Pages on every push to `main` that touches `docs/` (`.github/workflows/pages.yml`).
To preview it locally, run the same Jekyll build in Docker:

```sh
mkdir -p docs/_site
docker run --rm --platform linux/amd64 \
  -v "$PWD/docs":/github/workspace/docs:ro -v "$PWD/docs/_site":/github/workspace/_site \
  -e GITHUB_WORKSPACE=/github/workspace -e INPUT_SOURCE=docs -e INPUT_DESTINATION=_site \
  -e INPUT_FUTURE=false -e INPUT_VERBOSE=false -e INPUT_BUILD_REVISION=local -e INPUT_TOKEN= \
  -e GITHUB_REPOSITORY=Mtze/simple-cd -e GITHUB_API_URL=https://api.github.com \
  ghcr.io/actions/jekyll-build-pages:v1.0.13
```

`docs/_site` is ignored by Git.
The site expects the path `/simple-cd/`, so serve it from a folder with that name:

```sh
mkdir -p /tmp/pages && ln -sfn "$PWD/docs/_site" /tmp/pages/simple-cd
python3 -m http.server --directory /tmp/pages 4000   # open http://127.0.0.1:4000/simple-cd/
```
