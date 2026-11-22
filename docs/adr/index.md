---
title: Architecture decisions
nav_order: 10
has_children: true
---

# Architecture decisions

Each decision about structure, technology or protocols gets a record with context, decision and consequences.
A later decision supersedes a record instead of editing it.
The [design record](../design.md) (German) holds the discussion that led to them.

| ADR | Decision | Status |
|---|---|---|
| [0001](0001-postgres-for-runtime-state.md) | Postgres for runtime state | Accepted |
| [0002](0002-target-config-in-git.md) | Target configuration in Git, state in the database | Accepted |
| [0003](0003-artifacts-through-github-app.md) | Artifacts through a GitHub App, provenance from GitHub | Accepted |
| [0004](0004-job-per-deployment-secrets-from-vault.md) | One Kubernetes Job per deployment, secrets from Vault | Accepted |
| [0005](0005-kubernetes-rest-without-client-go.md) | Kubernetes over plain REST | Accepted |
| [0006](0006-keycloak-sessions-and-client-roles.md) | Keycloak login with server-side sessions and client roles | Accepted |
| [0007](0007-slots-locks-and-waiting-place.md) | Deploy slots, opt-in locks and a waiting place for auto-deploys | Accepted |
| [0008](0008-digests-and-registry-allowlist.md) | Images pinned by digest, registries on an allowlist | Accepted |
| [0009](0009-hand-placed-structural-diagrams.md) | Hand-placed draw.io diagrams for structural views | Accepted |
| [0010](0010-gateway-api-httproute.md) | Gateway API HTTPRoute instead of Ingress | Accepted |
| [0011](0011-live-updates-postgres-notify.md) | Live UI updates through Postgres NOTIFY and Server-Sent Events | Accepted |

New records use the next number and the same sections: Status, Date, Context, Decision, Consequences.
