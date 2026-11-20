---
title: Maintainers
nav_order: 4
---

# Maintainers
{: .no_toc }

Project maintainers hold the role `project:<owner>/<repo>:maintainer`.
They can do everything a developer can, and they keep the project's targets in order: take over locks, switch maintenance mode and clean up previews.

1. TOC
{:toc}

## Permissions

Everyone who can log in sees every project, target, artifact and deployment.
Changing anything needs a role.

| Action | Logged in, no role | Developer | Maintainer | Platform admin |
|---|:-:|:-:|:-:|:-:|
| See projects, targets, artifacts, logs | yes | yes | yes | yes |
| Deploy to an `available` target | - | yes ¹ | yes ¹ | yes |
| Start or update a preview | - | yes ¹ | yes ¹ | yes |
| Deploy again while holding the lock (extends it) | - | yes ¹ | yes ¹ | yes |
| Release your own lock | holder | holder | holder | yes |
| Take over or release someone else's lock | - | - | yes ¹ | yes |
| Deploy during `maintenance` | - | - | - | - |
| Switch maintenance on or off | - | - | yes | yes |
| Tear down a preview | - | yes ¹ | yes ¹ | yes |
| Forget an orphaned preview | - | - | - | yes |
| Read the audit log, see all previews | - | - | - | yes |
| See the config reload error banner | - | - | - | yes |

¹ If the target sets `role` (for example `deploy:app-prod`), developers and maintainers also need that role.
Without it they cannot deploy to the target, take over its lock or tear down its previews.
Maintenance does not need the target role.

Roles are Keycloak client roles, see [Keycloak setup](../admin/keycloak.md).
simple-cd reads them at login, so a role change takes effect at the next login.

## Locks and takeover

A lockable target is locked by the person who deployed last, for the target's lock duration.
When a teammate has to deploy to a locked target urgently:

1. Open the deploy dialog. It shows the holder and the remaining time.
2. Tick **Take over lock from ...** and deploy.
3. You become the new holder and the lock duration starts again.

The takeover is recorded in the lock history (`overridden`) and in the audit log (`lock.override` with the previous holder).

To free a lock without deploying, click **Release lock** on the target card.
Releasing someone else's lock needs the same permission as a takeover.

Locks are a courtesy between people, not a security boundary: auto-deploys skip locked targets, but a maintainer can always take over.

## Maintenance mode

Use maintenance when nobody may deploy, for example during a VM upgrade or an incident.

- **Start maintenance** on the target card asks for a reason. The card then shows the reason, who started it and when.
- While a target is in maintenance, nobody deploys: not developers, not maintainers, not platform admins, not auto-deploys.
- Deployments that already run are not stopped.
- **End maintenance** switches it off. To deploy during maintenance, end it first.

Both switches are audited as `maintenance.on` and `maintenance.off`.

## Previews

Previews disappear on their own when the pull request closes or the TTL runs out.
To remove one early, click **Tear down** in the **Previews** table of the project page.
simple-cd then runs a teardown deployment that removes the Helm release and the namespace.

simple-cd only deletes namespaces that carry its labels `cd/managed=true`, `cd/target=<id>` and `cd/pr=<number>`.
If the labels do not match, the teardown fails, simple-cd retries every 10 minutes and the namespace stays.
An administrator has to fix such a namespace by hand.

When a preview target is removed from the configuration, its previews become **orphaned**.
simple-cd no longer has the cluster credentials for them, so only a platform admin can **Forget** them, and the namespace has to be deleted by hand.
See [orphaned previews](../admin/operations.md#orphaned-previews).

## Auditability

simple-cd records who did what:

- **Deployment history** per target (target page) and per artifact, with actor, auto-deploy trigger, timing, status and log.
- **Lock history** per target: every acquire, extend, release and takeover.
- **GitHub deployments** on each commit and pull request, with a link back to the log.
- **Audit log** (platform admins, **Admin > Audit log**), newest first, 100 events per page.

| Audit action | Recorded when |
|---|---|
| `deploy.requested` | a person or auto-deploy creates a deployment |
| `deploy.skipped` | an auto-deploy is skipped, with the reason |
| `deploy.started` | the worker starts the executor |
| `deploy.finished` | the deployment ends, with status and error |
| `lock.override` | a maintainer takes over a lock |
| `lock.release` | a lock is released early |
| `maintenance.on`, `maintenance.off` | maintenance is switched |
| `preview.teardown` | a preview teardown is scheduled |
| `preview.forget` | an orphaned preview is forgotten |
| `webhook.fork_rejected` | a workflow run from a fork is ignored |
| `auth.login`, `auth.logout` | a user logs in (with roles) or out |

Audit events are kept for `auditRetention` (default 365 days).
