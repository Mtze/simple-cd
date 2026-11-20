---
title: Deploying with the UI
parent: Users
nav_order: 2
---

# Deploying with the UI
{: .no_toc }

1. TOC
{:toc}

## Pages

| Page | URL | Shows |
|---|---|---|
| Projects | `/` | every project with its targets, their status and the number of live previews |
| Project | `/projects/<owner>/<repo>` | target cards, the artifact list and the previews of the project |
| Target | `/targets/<id>` | the target card, the last 50 deployments and the lock history |
| Deployment | `/deployments/<id>` | actor, artifact, timing, error and the live log |
| Admin | `/admin` | audit log and all previews, for platform admins only |

Your role in the project appears as a badge next to the project name.

![Project page: target cards, artifacts with readiness per target, previews]({{ '/assets/screenshots/project.png' | relative_url }})

The user menu at the top right shows your roles, switches between light, dark and system theme and logs you out.

![User menu with name, roles, theme switch and logout]({{ '/assets/screenshots/user-menu.png' | relative_url }})

## Target status

Every target card shows one status.
The first one that applies wins:

| Status | Meaning | Who can deploy |
|---|---|---|
| `maintenance` | blocked by hand, with a reason | nobody, no auto-deploy either |
| `deploying` | a deployment is pending or running | nobody else in the same slot |
| `locked` | someone holds the lock, the card shows who and for how long | the holder; maintainers and platform admins by taking over |
| `available` | free | developers and maintainers |

Preview targets never show `deploying`, because every pull request has its own slot.

The card also shows what is deployed (for non-preview targets), who deployed it and when, and links to active deployments.

![Target page of a locked target with its deployments and lock history]({{ '/assets/screenshots/target.png' | relative_url }})

## Artifacts

The project page lists artifacts in three tabs:

- **Pull requests:** artifacts from `pull_request` runs. Artifacts of closed PRs are hidden unless you tick **Show closed PRs**.
- **Branches:** artifacts from pushes to branches and other events.
- **Releases:** artifacts from `release` events or tag pushes.

simple-cd derives the source, the commit and the PR number from GitHub, never from `deploy.json`.

Each artifact row shows the commit, the branch, a link to the workflow run, who triggered the run, the chart version and the images.
A warning appears when several workflows publish for the same commit; the newest run wins.

On the right, every target of the project gets a readiness line:

- a check mark: the artifact is deployable to this target,
- a cross: it is not; open it to read the reasons ([what they mean](troubleshooting.md)),
- a **deployed** badge: this artifact is what the target currently runs,
- a **Deploy** button, if you have the deploy permission for the target.

Preview targets only appear in the **Pull requests** tab.

## The deploy dialog

**Deploy** opens a dialog that asks the server for a plan first.
It shows:

- the target, its status and, for previews, the PR slot,
- the reasons if the artifact is not deployable,
- a warning if you lack the permission or the target is in maintenance,
- the lock situation if someone else holds the lock,
- the configuration diff, if the configuration changed.

![Deploy dialog with the diff of the Helm values and the confirmation checkbox]({{ '/assets/screenshots/deploy-dialog.png' | relative_url }})

### Configuration diff and confirmation

simple-cd compares the configuration of this artifact with the last successful deployment in the same slot:

- **Helm targets:** the merged values from the repository values file and the inline target values, plus the `set` mapping from `.cd.yaml` before placeholders are filled in.
- **Compose targets:** the Compose file.

Image changes are left out on purpose, so a normal update needs no confirmation.
Values from Vault are left out too, because they are secret.
When something else changed, the dialog shows a unified diff and the **Deploy** button only works after you tick **I reviewed the configuration change**.
The first deploy to a target or a PR slot has nothing to compare with and shows no diff.

### What happens after you click Deploy

The server checks everything again in one database transaction: your permissions, readiness, that the images and the chart still exist in the registry, maintenance, the lock and the slot.
If the state changed since the dialog opened, the dialog shows the new reason and lets you react.
On success the browser opens the deployment page with the live log.

## Locks

Locks are opt-in per target (`lockable: true` in the target configuration).
On a lockable target:

1. Your deploy takes the lock. It starts with the deploy and lasts for the target's lock duration (default one hour). The card shows the holder and a countdown.
2. The lock stays when the deploy fails or times out.
3. While you hold the lock, you can deploy again. That extends the lock to a full duration again.
4. **Release lock** on the target card frees it early.
5. Others cannot deploy until the lock expires. Maintainers and platform admins can take it over in the deploy dialog (**Take over lock from ...**). The previous holder sees it in the lock history.
6. Auto-deploys never take a lock and are skipped while the target is locked.

The lock history on the target page lists every acquire, extend, release and takeover.

## Previews

A preview target (`preview: true`) gives every pull request its own Kubernetes namespace, named `pr-<repo>-<pr>`.
A preview starts by clicking **Deploy** on a pull request artifact, or by auto-deploy.

- Each deploy into a preview resets its TTL (default 72 hours, set per target).
- When the TTL runs out, simple-cd removes the Helm release and the namespace.
- When the pull request closes, simple-cd tears the preview down right away.
- GitHub shows the preview as a deployment in the PR timeline, in the environment `<target>/pr-<number>`.

The **Previews** table on the project page lists live previews with namespace, status and remaining time.
Anyone with the deploy permission on the target can tear a preview down early with **Tear down**.
DNS, ingress and quotas come from the preview cluster; your `.cd.yaml` sets the host name, for example with `{cd.pr}`.

## Auto-deploy

An auto-deploy needs both sides to agree:

- your workflow asks for it with the `auto-deploy` input of `cd-publish` (it ends up in `deploy.json` as `autoDeploy`),
- the target allows it with an `autoDeploy` rule, for example `from: [pr]` or `from: [branch]` with `branches: [main]`.

When an artifact arrives, simple-cd starts a deployment for each requested target whose rule matches.
It skips the target when the rule does not match, the artifact is not deployable, an image is missing, or the target is in maintenance or locked.
A skipped auto-deploy appears in GitHub as a deployment with state `error` and the reason, and in the audit log.

When the slot is busy, the artifact waits.
Each slot has one waiting place: a newer artifact replaces an older waiting one, so the newest push wins.
The deployment starts as soon as the slot is free.

Auto-deploys show `auto-deploy` as the actor, plus the GitHub user who triggered the workflow run.

## Rollback

There is no separate rollback.
To roll back, deploy an older artifact: find it in the artifact list (tick **Show closed PRs** for older pull request artifacts) and click **Deploy**.
If its configuration differs from what runs now, the dialog asks you to confirm the diff.

Old artifacts expire after a while, see [retention](../admin/operations.md#retention).
An artifact that is the last successful deployment of a target is never deleted.

## Live logs

The deployment page streams the executor output while the deployment runs and updates the status badge live.
After it finishes, simple-cd stores the log (up to 2 MiB) and shows the stored copy.

![Deployment page with the log of a Helm deploy]({{ '/assets/screenshots/deployment.png' | relative_url }})

Logs are deleted after the log retention period (default 90 days); the deployment record stays.

Deployment statuses:

| Status | Meaning |
|---|---|
| `pending` | waiting for the worker to start the executor (usually a few seconds) |
| `running` | the executor Job runs |
| `succeeded` | the executor finished without error |
| `failed` | the executor failed, could not start, or its Job disappeared |
| `timed_out` | the deployment ran longer than the target's job timeout (default 30 minutes) |

GitHub mirrors the status on the commit: `in_progress` while it runs, then `success`, `failure` or `error`.
