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
helm template ci deploy/helm/simple-cd --set github.appID=1 --set httpRoute.enabled=true --set 'httpRoute.parentRefs[0].name=public' --set registryAuth=true
```

## End-to-end test

The end-to-end test runs a complete installation in a [kind](https://kind.sigs.k8s.io/) cluster and drives it like GitHub and a user would.

```sh
make e2e-up     # build images, create the cluster, install everything (about 5 minutes the first time)
make e2e        # go test -tags e2e ./e2e/
make e2e-down   # delete the cluster and the registry
```

`make e2e-up` installs kind into `bin/` and needs Docker, `kubectl`, `helm` and `openssl`.
It is safe to run again after a code change: it rebuilds the images under a new tag and upgrades the release.

What runs where:

| Piece | Where | Reached from the host at |
|---|---|---|
| simple-cd, 2 replicas, installed with `deploy/helm/simple-cd` and `e2e/values.yaml` | namespace `simple-cd` | `http://127.0.0.1:18080` |
| Keycloak with `dev/keycloak-realm.json` | namespace `simple-cd` | `http://127.0.0.1:18081`, issuer `http://keycloak:18081/realms/simple-cd` |
| fake GitHub API (`e2e/fakegithub`) | namespace `simple-cd` | `http://127.0.0.1:18082` |
| Vault in dev mode with Kubernetes auth, Postgres | namespace `simple-cd` | |
| OCI registry (Docker container `kind-registry`) | Docker network `kind`, `kind-registry:5000` | `127.0.0.1:5001` |
| deployed test app (`e2e/testchart/demo`) | namespace `e2e-app` and preview namespaces `pr-demo-<n>` | |

The kind cluster is both the delivery cluster and the target cluster.
Its kubeconfig for the target, with the token of a dedicated service account, sits in Vault at `secret/cd/clusters/kind`, as it would for a separate cluster.
The registry has no TLS, so the server and the executor get `SIMPLECD_REGISTRY_PLAIN_HTTP=kind-registry:5000`.

The test (`e2e/e2e_test.go`) covers:

- `workflow_run` webhooks signed with the webhook secret; simple-cd fetches `deploy.json`, `.cd.yaml` and the values file from the fake GitHub API.
- the registry check against a real registry: an artifact with a missing image digest is not deployable.
- the Keycloak login form and the session cookie.
- live updates: four `/api/events` streams, spread over both replicas, each receive the change event of a new build.
- a manual deploy through an executor Job: Vault Kubernetes auth, the cluster credentials and values from Vault, `helm upgrade --install` of a chart from the registry, the live log stream; the test checks the values that reached the release.
- a preview: auto-deploy from a pull request build into a labelled namespace, teardown on `pull_request` closed.
- the deployment statuses simple-cd reports to GitHub.

Keycloak's issuer must be the same URL for simple-cd and for the test, so the test dials `keycloak:18081` through `127.0.0.1:18081`.
To use the UI of the end-to-end installation in a browser, add `127.0.0.1 keycloak` to `/etc/hosts` and open `http://127.0.0.1:18080`.
When a run fails, look at the deployment log in the UI or with `kubectl --context kind-simple-cd-e2e -n simple-cd logs deploy/simple-cd`.

## Docs

```sh
python3 docs/architecture/uml/diagrams.py   # structural UML views (needs the drawio CLI)
docs/architecture/diagrams/render.sh       # sequence and state diagrams (PlantUML in Docker)
```

### Screenshots

The screenshots in `docs/assets/screenshots` come from `web/scripts/screenshots.mjs`, in light and dark (`-dark.png`):

```sh
make dev-up dev-seed run   # demo data, alice holds the lock on demo-test for one hour
make e2e-up e2e            # a real deployment log and audit log
make screenshots           # drives the installed Google Chrome with playwright-core
```

Retake them when the UI changes, and look at every image before you commit it.

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
