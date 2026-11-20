---
title: GitHub App
parent: Administrators
nav_order: 3
---

# GitHub App setup

simple-cd talks to GitHub as a GitHub App.
The App receives webhooks, reads workflow artifacts and repository files, and reports deployments.

## Create the App

In your organization, open **Settings > Developer settings > GitHub Apps > New GitHub App**:

| Setting | Value |
|---|---|
| Homepage URL | `https://cd.example.org` |
| Webhook | Active |
| Webhook URL | `https://cd.example.org/webhooks/github` |
| Webhook secret | a long random string, stored as `github-webhook-secret` in the Secret |
| Where can this App be installed | Only on this account |

Repository permissions:

| Permission | Access | Used for |
|---|---|---|
| Actions | Read-only | list workflow run artifacts and download `cd-deploy` |
| Contents | Read-only | read `.cd.yaml`, values files, the Compose file and the playbook at a commit; check whether a ref is a tag |
| Deployments | Read and write | show deployments on commits and pull requests |
| Pull requests | Read-only | find the pull request of a commit |
| Metadata | Read-only | mandatory for every App |

Subscribe to these events:

| Event | Used for |
|---|---|
| Workflow run | new artifacts (`completed` runs with conclusion `success`) |
| Pull request | `closed` pull requests: hide their artifacts and tear down their previews |

After creating the App:

1. Note the **App ID** and set `github.appID` in the Helm values.
2. Generate a **private key** and store the PEM file as `github-private-key` in the Secret.
3. Install the App on the repositories that simple-cd should deploy. You can grant it all repositories: simple-cd ignores repositories that are not projects in the target configuration.

## What simple-cd does with webhooks

simple-cd verifies the `X-Hub-Signature-256` header with the webhook secret and answers right away:

- `202` for `workflow_run` and `pull_request`, which it processes in the background,
- `204` for every other event, for example `ping` and `installation`,
- `401` when the signature is wrong.

Processing happens in memory after the answer.
If a server pod stops in between, redeliver the event under **Advanced > Recent deliveries**.

simple-cd also ignores workflow runs whose head repository differs from the repository, which is the case for pull requests from forks.
It records them as `webhook.fork_rejected` in the audit log.
For private repositories, also switch off **Allow forking** in the organization settings.

## GitHub Enterprise Server

Set `github.apiURL` to your API URL, for example `https://github.example.org/api/v3`, and `github.gitSSHHost` to the SSH host name used for cloning Ansible playbooks from app repositories.

## Deployments on GitHub

When a deployment starts, simple-cd creates a GitHub deployment for the commit:

- environment `<target-id>` for normal targets, `<target-id>/pr-<number>` for previews (marked as transient),
- state `in_progress`, then `success`, `failure` (failed) or `error` (timed out),
- a log URL pointing to `https://cd.example.org/deployments/<id>`.

When a preview is torn down, its last deployment becomes `inactive`.
A skipped auto-deploy appears as a deployment with state `error` and the reason.
Reporting is best effort: GitHub being down never blocks a deploy.
