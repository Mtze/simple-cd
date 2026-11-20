---
title: Local development
parent: Contributors
nav_order: 1
---

# Local development
{: .no_toc }

1. TOC
{:toc}

## Requirements

- Go (the version in `go.mod`)
- Node.js 26 and npm, for the UI
- Docker with Compose, for Postgres and Keycloak
- `make`, `openssl`

## Start the stack

```sh
make dev-up     # Postgres on 127.0.0.1:5434, Keycloak on 127.0.0.1:8081
make dev-seed   # demo artifacts, deployments and a preview for example/demo
make web        # build the UI into web/dist so the server embeds it
make run        # build both binaries and run the server on http://127.0.0.1:8080
```

`make dev-up` also creates a throwaway GitHub App key in `dev/github-app-dev.pem` (ignored by Git).
`make help` lists all targets; `make dev-down` stops the stack.

`make run` uses `dev/config.yaml` and these development settings:

| Setting | Value | Why |
|---|---|---|
| `SIMPLECD_RUNNER` | `process` | runs `bin/simple-cd-executor` as a local process instead of a Kubernetes Job |
| `SIMPLECD_REGISTRY_CHECK` | `off` | the demo images do not exist |
| `SIMPLECD_GITHUB_APP_ID` | `1` | no real GitHub App; webhooks use the secret `dev-webhook-secret` |
| `SIMPLECD_OIDC_*` | the `simple-cd` client of the dev realm, secret `dev-secret` | |

## Test users

The Keycloak realm `simple-cd` from `dev/keycloak-realm.json` has five users, all with the password `dev`:

| User | Roles | Use it to try |
|---|---|---|
| `alice` | `project:example/demo:developer` | deploying, locks |
| `bob` | `project:example/demo:developer` | a second developer, blocked by alice's lock |
| `mia` | `project:example/demo:maintainer`, `deploy:demo-prod` | lock takeover, maintenance, the extra target role |
| `root` | `platform-admin` | the admin page, audit log, orphaned previews |
| `guest` | none | the read-only view |

The Keycloak admin console runs at <http://127.0.0.1:8081> with `admin` / `admin`.

## UI development server

For hot reload, run Vite next to the server:

```sh
cd web
npm ci
npm run dev     # http://127.0.0.1:5173, proxies /api and /auth to 127.0.0.1:8080
```

The OIDC redirect and the CSRF origin check use `SIMPLECD_BASE_URL`.
`make run` sets it to `http://127.0.0.1:8080`, so with the dev server you can browse, but logins land on port 8080 and changes from port 5173 are rejected as cross-origin.
To work fully on port 5173, start the server with the base URL of the dev server; the dev realm already allows `http://127.0.0.1:5173/auth/callback`:

```sh
make build
SIMPLECD_CONFIG=dev/config.yaml \
SIMPLECD_DATABASE_URL='postgres://simplecd:simplecd@127.0.0.1:5434/simplecd?sslmode=disable' \
SIMPLECD_BASE_URL=http://127.0.0.1:5173 \
SIMPLECD_LISTEN=127.0.0.1:8080 \
SIMPLECD_OIDC_ISSUER=http://127.0.0.1:8081/realms/simple-cd \
SIMPLECD_OIDC_CLIENT_ID=simple-cd \
SIMPLECD_OIDC_CLIENT_SECRET=dev-secret \
SIMPLECD_GITHUB_APP_ID=1 \
SIMPLECD_GITHUB_PRIVATE_KEY_FILE=dev/github-app-dev.pem \
SIMPLECD_GITHUB_WEBHOOK_SECRET=dev-webhook-secret \
SIMPLECD_RUNNER=process \
SIMPLECD_REGISTRY_CHECK=off \
SIMPLECD_EXECUTOR_BINARY="$PWD/bin/simple-cd-executor" \
./bin/simple-cd
```

## Running deployments locally

With the process runner, a deployment runs `bin/simple-cd-executor` on your machine.
It needs Vault and the tools of the target type (`helm` and `kubectl`, or `ssh`) on your `PATH`.
The executor accepts a token instead of Kubernetes auth, so a Vault dev server is enough:

```sh
vault server -dev -dev-root-token-id=dev &
export VAULT_ADDR=http://127.0.0.1:8200 VAULT_TOKEN=dev
vault kv put secret/cd/clusters/test kubeconfig=@$HOME/.kube/config
make run
```

The server passes `VAULT_ADDR` and `VAULT_TOKEN` from its environment to the executor.
Without Vault, deployments fail at `==> Logging in to Vault`, which is still enough to try the UI flow.

## Sending a webhook by hand

There is no GitHub App locally, but you can send signed webhooks to test the ingestion code paths:

```sh
body='{"action":"closed","number":7,"repository":{"full_name":"example/demo"},"installation":{"id":1}}'
sig=$(printf '%s' "$body" | openssl dgst -sha256 -hmac dev-webhook-secret | sed 's/.* //')
curl -i http://127.0.0.1:8080/webhooks/github \
  -H "X-GitHub-Event: pull_request" -H "X-Hub-Signature-256: sha256=$sig" -d "$body"
```

`workflow_run` events need the GitHub API to download artifacts, so they only work against a real App.
