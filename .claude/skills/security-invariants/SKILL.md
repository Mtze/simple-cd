---
name: security-invariants
description: Checklist of the security properties of simple-cd (from the audit of 2026-11-20) to review a change that touches login, sessions, webhooks, GitHub provenance, registries, deploy.json or .cd.yaml parsing, Vault, the executor, the Helm chart or API authorization. Use before finishing such a change or when asked for a security review.
---

# Security invariants

Go through every item the change touches. For each one, point to the code or test that keeps it, or fix the change. A broken invariant needs a test that fails without the fix.

## Trust in repository content

Attacker model: a **developer** can push any branch of a project repository, run workflows and write any `deploy.json`, `.cd.yaml`, values file, Compose file or playbook. They may have no simple-cd account.

- [ ] Repository, commit, source (`pr`, `branch`, `release`) and PR number come from the signed webhook and the GitHub API, never from `deploy.json` (`internal/ingest`).
- [ ] A push is a `release` only when `TagCommit` of the branch name equals the built commit. Branches may share names with tags.
- [ ] Runs from forks are rejected (`webhook.fork_rejected`).
- [ ] No host from `deploy.json` or `.cd.yaml` is contacted before `repoconfig.AllowedBy` accepted it. Registry redirects stay on the same host and scheme; token realms are https (or a listed plain-http test host).
- [ ] Image and chart repositories match `repositoryPattern` (OCI grammar, no `..`); allowlist prefixes match whole path components.
- [ ] Paths from `.cd.yaml` stay inside the repository (`safeRepoPath`); refs and paths passed to `git` cannot start with `-`.
- [ ] Hosts, users, directories and env names that reach a remote shell match strict patterns and values are quoted.
- [ ] Auto-deploy needs both the request in `deploy.json` and a matching rule on the target; it never overrides maintenance or locks.

## Secrets

- [ ] The server never reads deployment secrets; only the executor Job reads them from Vault. The Job spec contains Vault paths, never values.
- [ ] Secrets are written with mode 0600 to the in-memory volume; errors name keys, never values; the diff snapshot excludes Vault values.
- [ ] Code that runs inside the Job and comes from a repository (Ansible `source: repo`) can read every secret of the shared Vault role. Do not widen this; document it where it applies.
- [ ] The process runner (local development) refuses an https base URL, because it hands the server's environment to the executor.

## Login and sessions

- [ ] Login state (state, PKCE verifier, nonce, return) lives only in the per-login cookie `simplecd_login-<state prefix>` and must match the `state` Keycloak sends back.
- [ ] `safeReturn` only returns local paths: no scheme or host, no control characters, no backslash, nothing under `/auth/`.
- [ ] A login without a client role, or with a reserved name (`system`, `auto-deploy`, `github`), gets 403.
- [ ] Session cookies are `HttpOnly`, `SameSite=Lax`, and `__Host-` plus `Secure` with an https base URL; the database stores only the SHA-256 of the session id.

## API and browser

- [ ] Every state-changing route checks the role on the server (`core.PermissionsFor`); the UI hiding a button is not a check.
- [ ] `POST`, `PUT`, `DELETE` require `X-Requested-With: simple-cd` and a matching `Origin`; no CORS headers.
- [ ] Request bodies are decoded with unknown fields rejected; actor, target and slot never come from the body.
- [ ] Decisions about one target (lock, maintenance, slot) happen in one transaction after `LockTargetState`.
- [ ] SSE output normalizes `\r` and `\r\n`, so log text cannot start new fields.
- [ ] The UI renders no HTML from data (`dangerouslySetInnerHTML` stays unused) and loads no inline scripts, so the strict CSP keeps working.
- [ ] Body and download limits stay in place (API 64 KiB, webhooks 5 MiB, GitHub files 1 MiB, logs 2 MiB).

## Supply chain

- [ ] GitHub Actions are pinned to commit SHAs; inputs reach `run:` steps through environment variables.
- [ ] New downloads in Dockerfiles are pinned and checksum-verified.
- [ ] New dependencies are justified (the project keeps them few) and free of known advisories.

Record anything that stays open in `docs/admin/security.md` under "Known limits".
