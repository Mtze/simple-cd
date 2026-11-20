---
title: "0003 Artifacts through a GitHub App, provenance from GitHub"
parent: Architecture decisions
nav_order: 3
---

# 0003 Artifacts through a GitHub App, provenance from GitHub

- **Status:** Accepted
- **Date:** 2026-11-20

## Context

simple-cd must learn which deployable artifacts exist without storing a server token in every repository, and a workflow must not be able to lie about where an artifact comes from.

## Decision

Workflows upload `deploy.json` as the workflow artifact `cd-deploy` with the `cd-publish` action. A GitHub App receives the `workflow_run` webhook, downloads the file and reads `.cd.yaml` at the commit. Repository, commit, source and pull request come from the webhook and the GitHub API, never from `deploy.json`. Runs from forks are rejected.

## Consequences

Repositories need the App installed and an entry in the target configuration. Webhooks are processed after the response; a lost event is redelivered from the App settings.
