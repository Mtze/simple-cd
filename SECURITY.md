# Security policy

## Reporting a vulnerability

Please do not open a public issue for security problems.
Report them privately through GitHub: open the repository's **Security** tab and click **Report a vulnerability** ([direct link](https://github.com/Mtze/simple-cd/security/advisories/new)).

Include what you found, how to reproduce it and the version or commit you tested.
You get an answer as soon as possible; we fix confirmed issues and publish an advisory together with the fix.

## Supported versions

simple-cd is young.
Fixes go into `main` and the latest release.

## Security model in short

- **Forks are rejected:** workflow runs from forks never become artifacts.
- **Provenance from GitHub:** repository, commit, source and pull request come from the GitHub App and signed webhooks, never from `deploy.json`.
- **Registry prefixes and digests:** images and charts must be under allowed registry prefixes, and images are pinned by `sha256` digest and checked again before each deploy.
- **Roles from Keycloak:** per project developer and maintainer roles, optional per target roles, checked on every request.
- **Secrets stay in Vault:** each deployment runs as its own Kubernetes Job that reads only its secrets from Vault into memory; secrets never reach the server or etcd.
- **Sessions:** stored hashed in Postgres, `HttpOnly`, `SameSite=Lax` and `__Host-`/`Secure` cookies over HTTPS.
- **Browser protection:** CSRF header and origin check, strict Content Security Policy, frame denial, HSTS.
- **Hardened Jobs:** non-root, read-only root filesystem, no capabilities, no Kubernetes permissions.
- **SSH:** `known_hosts` required, strict host key checking.

The full description is in the [security model](https://mtze.me/simple-cd/admin/security.html).
