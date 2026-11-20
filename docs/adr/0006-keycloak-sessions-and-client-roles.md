---
title: "0006 Keycloak login with server-side sessions and client roles"
parent: Architecture decisions
nav_order: 6
---

# 0006 Keycloak login with server-side sessions and client roles

- **Status:** Accepted
- **Date:** 2026-11-20

## Context

Users already have Keycloak accounts. Permissions differ per project and per target.

## Decision

The server uses the OIDC authorization code flow with PKCE and keeps sessions in Postgres, keyed by the SHA-256 of the cookie. Roles are client roles in the ID token: `project:<repo>:developer`, `project:<repo>:maintainer`, optional target roles and `platform-admin`.

## Consequences

Logging in is instant with an existing SSO session. The Keycloak client needs a mapper that puts client roles into the ID token. Role changes take effect at the next login.
