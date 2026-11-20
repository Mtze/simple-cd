---
title: Security model
parent: Administrators
nav_order: 10
---

# Security model
{: .no_toc }

simple-cd sits between source code and running systems.
This page lists what it trusts, what it checks and how it is hardened.
To report a vulnerability, follow [SECURITY.md](https://github.com/Mtze/simple-cd/blob/main/SECURITY.md).

1. TOC
{:toc}

## Trust boundaries

| Input | Trusted for | Checks |
|---|---|---|
| GitHub webhooks | which run finished | HMAC signature (`X-Hub-Signature-256`) with the App's webhook secret, compared in constant time |
| GitHub API (as the App) | repository, commit, source, PR number, files at a commit | only repositories that are projects in the target configuration |
| `deploy.json` | image digests, chart version, auto-deploy requests | strict schema, digests required, never provenance |
| `.cd.yaml` | chart, value mapping, file paths | strict schema, `oci://` charts only, paths must stay inside the repository |
| Target configuration | everything about targets | strict schema, reviewed in Git |
| Keycloak ID token | identity and roles | signature, issuer, audience and nonce |

### Forks are rejected

A workflow run whose head repository differs from the repository itself, which is the case for pull requests from forks, is ignored and recorded as `webhook.fork_rejected`.
Fork code therefore never reaches a target through simple-cd.
For private repositories, also switch off **Allow forking** in the organization.

### Provenance comes from GitHub

The repository, commit, source (`pr`, `branch`, `release`) and pull request number come from the webhook payload and the GitHub API, never from `deploy.json`.
A workflow cannot claim to be a release or another pull request.

### Registry prefixes

Every image and the chart must be under an allowed registry prefix of the project (`registries`).
A compromised workflow cannot point a target at an arbitrary image.

### Digest pinning

Images must be referenced by `sha256` digest; tags are not enough, because they can move.
Helm values get the digest through placeholders, Compose gets it through `.cd.env`.
Right before each deploy simple-cd checks again that the images and the chart version exist.

### Who may deploy

Roles come from Keycloak client roles, see the [permission matrix](../maintainers/index.md#permissions).
The server checks permissions on every request; the UI only hides what the user cannot do.
Locks, maintenance and the slot are checked inside one database transaction that locks the target's state row, so two requests cannot both win.
Auto-deploys need a request from the repository and a matching rule on the target, and they never override maintenance or locks.

## Secrets

- Deployment secrets live in Vault only. The server has no Vault access.
- The executor logs in with its own service account (Vault Kubernetes auth) and reads the paths of its deployment.
- Secrets are written to an in-memory `emptyDir` (`medium: Memory`) with mode `0600` and never reach etcd: the Job spec contains Vault paths only.
- The snapshot used for the confirmation diff excludes Vault values.
- Error messages name missing keys but never print secret content.
- The server's own secrets (database URL, OIDC client secret, GitHub App key, webhook secret, registry credentials) come from a Kubernetes Secret mounted as files (`*_FILE` variables).

## Sessions and browser

- Login uses the authorization code flow with PKCE (`S256`), a one-time `state` that expires after 10 minutes and a `nonce`.
- The `return` target after login only accepts local paths, so the login cannot redirect elsewhere.
- Sessions live in Postgres, stored under the SHA-256 hash of the cookie value; a database leak does not reveal usable cookies. They expire after `SIMPLECD_SESSION_TTL` (default 4 hours).
- The session cookie is `HttpOnly`, `SameSite=Lax` and, with an `https://` base URL, `Secure` with the `__Host-` prefix.
- Logout deletes the session and ends the Keycloak session (RP-initiated logout).
- **CSRF:** every `POST`, `PUT` and `DELETE` needs the header `X-Requested-With: simple-cd`, which browsers cannot send cross-site without CORS (simple-cd never allows CORS), and a present `Origin` header must match the base URL.
- **Security headers:** a strict Content Security Policy (`default-src 'self'`, no inline scripts, `frame-ancestors 'none'`, `object-src 'none'`, `base-uri 'none'`, `form-action 'self'`), `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: same-origin`, `Cross-Origin-Opener-Policy: same-origin`, a restrictive `Permissions-Policy`, and HSTS with an `https://` base URL.
- API responses carry `Cache-Control: no-store`.
- Request bodies are limited (64 KiB for the API, 5 MiB for webhooks), downloads from GitHub to 1 MiB.

## Executor Job hardening

Each deployment runs in its own Job:

- `backoffLimit: 0` (no retries), `activeDeadlineSeconds` set to the job timeout, removed one hour after it finishes,
- runs as uid and gid 10001, `runAsNonRoot`, `RuntimeDefault` seccomp profile,
- `allowPrivilegeEscalation: false`, read-only root filesystem, all capabilities dropped,
- writable only `/secrets` (memory, 16 MiB) and `/tmp` (1 GiB),
- memory limit 512 MiB,
- its service account has no Kubernetes permissions; the token only serves the Vault login.

The server's own service account may only manage Jobs and read pods and logs in its namespace.

## SSH

- `known_hosts` is required for Compose targets and for SSH Git URLs; `StrictHostKeyChecking=yes` and `BatchMode=yes` are always set. There is no trust on first use.
- Ansible runs with `ANSIBLE_HOST_KEY_CHECKING=True`.
- Host names, directories, user names and environment variable names are checked against strict patterns before they reach a remote shell, and values are quoted.

## Kubernetes targets

- Normal Helm targets use a service account with `admin` in one namespace.
- Preview targets need wider rights; run them in a separate cluster without production workloads, see [target clusters](target-clusters.md#preview-clusters).
- simple-cd never adopts or deletes a namespace that lacks its labels `cd/managed=true`, `cd/target` and `cd/pr`.

## Known limits

- One Vault role for all executor Jobs: every Job can read every secret below `secret/cd/`.
- Webhook processing happens in memory after the `202` answer; a crash in between loses the event until it is redelivered.
- Locks coordinate people; they are not an access control.
- `SIMPLECD_REGISTRY_CHECK=off` and `SIMPLECD_RUNNER=process` are for local development only.
