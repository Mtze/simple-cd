---
title: Operations
parent: Administrators
nav_order: 9
---

# Operations
{: .no_toc }

1. TOC
{:toc}

## What holds state

| Where | What | Backup |
|---|---|---|
| Postgres | artifacts, deployments with logs, locks, maintenance, previews, waiting auto-deploys, audit log, sessions | **yes**, this is the only backup you need |
| Target configuration | projects and targets | in Git |
| Vault | every deployment secret | your Vault backup |
| Kubernetes | running executor Jobs, deleted one hour after they finish | no |

The server pods are stateless.

## Backups

Back up the Postgres database with your usual tooling, for example `pg_dump` or the backup feature of your managed Postgres.
After a restore:

- deployments that were `running` and whose Job is gone end as `failed` with "the executor job disappeared",
- users have to log in again if their sessions were not in the backup,
- previews created after the backup are unknown to simple-cd; remove their namespaces (label `cd/managed=true`) by hand.

## Retention

The worker applies retention every 10 minutes:

| Data | Setting | Rule |
|---|---|---|
| Pull request artifacts | `artifactRetention` (30d) | deleted when the PR is closed and the artifact was not updated for this long |
| Branch artifacts | `keepBranchArtifacts` (50) | the newest 50 per branch are kept |
| Release artifacts | `keepBranchArtifacts` (50) | the newest 50 per project are kept |
| Deployment logs | `logRetention` (90d) | the log text of deployments that finished this long ago is removed; the deployment record stays |
| Audit events | `auditRetention` (365d) | deleted |
| Sessions | session TTL | expired ones are deleted |

An artifact that is the last successful deployment of a target or slot, or that a pending or running deployment uses, is never deleted.
Deleting an artifact keeps its deployments; they just lose the link to it.

## Audit log

Platform admins read it under **Admin > Audit log** or with `GET /api/admin/audit`.

![Admin page with the audit log]({{ '/assets/screenshots/admin.png' | relative_url }})

The actions are listed in [Maintainers: auditability](../maintainers/index.md#auditability).
For longer retention or a SIEM, export the `audit_events` table from Postgres.

## Configuration reload

- Every replica checks the configuration file every 10 seconds and compares its content. ConfigMap updates take up to a minute to reach the pod, so expect a short delay after `helm upgrade`.
- An invalid file is never applied. The previous configuration stays active, the server logs `config reload failed, keeping previous config` once per broken version, and platform admins see a banner with the error until a valid version arrives.
- At start, an invalid configuration stops the server.

## Orphaned previews

When a preview target disappears from the configuration, its live previews show up as **orphaned** under **Admin > Previews**.
simple-cd cannot tear them down anymore, because the cluster credentials were part of the removed target.

1. Delete the namespace yourself, for example `kubectl delete namespace pr-app-42` (check the labels `cd/managed`, `cd/target`, `cd/pr` first).
2. Click **Forget** on the orphaned preview. simple-cd marks it deleted and records `preview.forget`.

A teardown that fails because a namespace has foreign labels is retried every 10 minutes.
Fix the namespace by hand or remove it; the next retry then succeeds.

## Upgrading

1. Read the [changelog](https://github.com/Mtze/simple-cd/blob/main/CHANGELOG.md).
2. Set the new `image.tag` and `executor.tag` and run `helm upgrade`.

Database migrations run automatically when a server starts, guarded by a Postgres advisory lock so replicas do not race.
To run them ahead of the rollout, run `simple-cd migrate` with `SIMPLECD_DATABASE_URL` set, for example as a one-off Job with the new image.

Running deployments survive a server restart: executor Jobs have stable names (`simple-cd-<deployment id>`) and the new leader picks them up.
Webhooks that arrive while a pod shuts down may be lost; redeliver them from the GitHub App settings.

## Scaling

- All replicas serve the UI, the API and webhooks.
- Background work (starting and watching Jobs, waiting auto-deploys, preview TTLs and retries, retention) runs on exactly one replica, the leader, chosen with a Postgres advisory lock. When the leader dies, another replica takes over within seconds.
- Two replicas are enough for availability. More replicas only add API capacity.
- The `process` runner keeps state in memory and supports a single replica only. It is for local development.

## Health checks

| Endpoint | Meaning |
|---|---|
| `GET /healthz` | liveness: the process answers |
| `GET /readyz` | readiness: Postgres answers within 2 seconds |

## Logs

The server writes JSON lines to stderr (Go `log/slog`), for example:

```json
{"time":"2026-11-20T10:00:00Z","level":"INFO","msg":"artifact stored","repo":"example/app","sha":"4f2c...","source":"pr","reasons":0}
```

Useful messages:

| Message | Meaning |
|---|---|
| `simple-cd listening` | the server started |
| `worker is leader` | this replica runs the background work |
| `config reloaded`, `config reload failed, keeping previous config` | configuration reload |
| `artifact stored`, `ignoring workflow run of unknown repository` | webhook processing |
| `webhook processing failed` | a webhook could not be processed, with the delivery id |
| `auto-deploy skipped` | with target and reason |
| `registry checks are off` | `SIMPLECD_REGISTRY_CHECK=off`, never in production |
| `registry is reached over plain http` | `SIMPLECD_REGISTRY_PLAIN_HTTP` is set, never in production |

The executor log of each deployment is in the UI and, while the Job exists, in `kubectl logs job/simple-cd-<id>`.
