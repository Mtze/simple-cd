---
title: Environment variables
parent: Reference
nav_order: 3
---

# Environment variables
{: .no_toc }

The Helm chart sets all of these for you; this page is for custom deployments and debugging.

1. TOC
{:toc}

## Server (`simple-cd`)

### Commands

| Command | Does |
|---|---|
| `simple-cd` or `simple-cd serve` | runs the server (migrations first) |
| `simple-cd validate <config.yaml>` | checks a target configuration and prints `ok: <n> projects, <m> targets`, exit status 1 on errors |
| `simple-cd migrate` | applies database migrations and exits; needs only `SIMPLECD_DATABASE_URL` |

### `_FILE` variants

Every `SIMPLECD_*` setting of the server below also accepts `<NAME>_FILE`: the path of a file whose content (trimmed) is the value.
`_FILE` wins over the plain variable.
The chart uses it for all secrets.
An unreadable file stops the server.
The pass-through variables for the executor (`VAULT_*`, `SIMPLECD_VAULT_*`) are read directly and have no `_FILE` variant.

### Settings

| Variable | Required | Default | Meaning |
|---|---|---|---|
| `SIMPLECD_CONFIG` | yes | | path of the [target configuration](../admin/configuration.md) |
| `SIMPLECD_DATABASE_URL` | yes | | Postgres connection URL |
| `SIMPLECD_BASE_URL` | yes | | public URL, e.g. `https://cd.example.org`. Used for the OIDC redirect (`/auth/callback`), the post logout redirect, the CSRF origin check, cookie flags and HSTS (with `https://`) and log links on GitHub. |
| `SIMPLECD_OIDC_ISSUER` | yes | | Keycloak realm URL |
| `SIMPLECD_OIDC_CLIENT_ID` | yes | | Keycloak client ID; roles are read from `resource_access.<client id>.roles` |
| `SIMPLECD_OIDC_CLIENT_SECRET` | yes | | Keycloak client secret |
| `SIMPLECD_GITHUB_APP_ID` | yes | | GitHub App ID (a number) |
| `SIMPLECD_GITHUB_PRIVATE_KEY` | yes | | GitHub App private key, PEM (PKCS#1 or PKCS#8 RSA) |
| `SIMPLECD_GITHUB_WEBHOOK_SECRET` | yes | | GitHub App webhook secret |
| `SIMPLECD_SESSION_TTL` | no | `4h` | session lifetime, Go duration or days (`1d`) |
| `SIMPLECD_LISTEN` | no | `:8080` | listen address |
| `SIMPLECD_GITHUB_API_URL` | no | `https://api.github.com` | GitHub API base URL, for GitHub Enterprise Server |
| `SIMPLECD_GIT_SSH_HOST` | no | `github.com` | SSH host for cloning app repositories for Ansible targets with `source: repo` |
| `SIMPLECD_REGISTRY_AUTH_FILE` | no | | Docker `config.json` with registry credentials for the existence checks |
| `SIMPLECD_REGISTRY_CHECK` | no | `on` | `off` disables all registry checks. Local development only. |
| `SIMPLECD_RUNNER` | no | `kubernetes` | `kubernetes` (Jobs) or `process` (local child process, single replica, development only) |
| `SIMPLECD_EXECUTOR_IMAGE` | with `kubernetes` | | executor image for Helm and Compose targets |
| `SIMPLECD_EXECUTOR_ANSIBLE_IMAGE` | no | `SIMPLECD_EXECUTOR_IMAGE` | executor image for Ansible targets |
| `SIMPLECD_EXECUTOR_SERVICE_ACCOUNT` | no | `simple-cd-executor` | service account of the executor Jobs |
| `SIMPLECD_JOB_NAMESPACE` | no | the server pod's namespace | namespace for executor Jobs. The chart's Role only covers the release namespace. |
| `SIMPLECD_EXECUTOR_BINARY` | no | `simple-cd-executor` | executor binary for the `process` runner |

### Passed through to the executor

The server copies these from its own environment into every executor run, if set:

| Variable | Meaning |
|---|---|
| `VAULT_ADDR` | Vault base URL |
| `VAULT_NAMESPACE` | Vault Enterprise namespace |
| `SIMPLECD_VAULT_ROLE` | Vault Kubernetes auth role |
| `SIMPLECD_VAULT_AUTH_MOUNT` | Vault Kubernetes auth mount |
| `VAULT_TOKEN` | only with the `process` runner, for local development |

The Kubernetes runner reads `KUBERNETES_SERVICE_HOST`, `KUBERNETES_SERVICE_PORT` and the pod's service account files, which Kubernetes provides.

## Executor (`simple-cd-executor`)

| Variable | Default | Meaning |
|---|---|---|
| `SIMPLECD_SPEC` | | the deployment as JSON, set by the server (at most 120 KiB) |
| `VAULT_ADDR` | | Vault base URL, required |
| `VAULT_TOKEN` | | a Vault token; when set, Kubernetes auth is skipped (local development) |
| `VAULT_NAMESPACE` | | Vault Enterprise namespace |
| `SIMPLECD_VAULT_ROLE` | | Vault Kubernetes auth role, required without `VAULT_TOKEN` |
| `SIMPLECD_VAULT_AUTH_MOUNT` | `kubernetes` | Vault auth mount |
| `SIMPLECD_SA_TOKEN_FILE` | `/var/run/secrets/kubernetes.io/serviceaccount/token` | service account token for the Vault login |
| `SIMPLECD_SECRETS_DIR` | `/secrets` | where secret material is written (mode `0600`, an in-memory volume in Jobs) |

The executor prints its progress as lines starting with `==>`, ends with `==> Done` or `==> FAILED: <reason>` and exits with status 0, 1 (deployment failed) or 2 (invalid spec).
