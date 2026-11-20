---
title: "0002 Target configuration in Git, state in the database"
parent: Architecture decisions
nav_order: 2
---

# 0002 Target configuration in Git, state in the database

- **Status:** Accepted
- **Date:** 2026-11-20

## Context

Targets change rarely and should be reviewed like code. Locks and maintenance change all the time and must not need a commit.

## Decision

Targets, projects and defaults are a YAML file in a config repository, rolled out as a ConfigMap and reloaded without a restart; an invalid file never becomes active. Maintenance, locks and history are runtime state in Postgres, keyed by the target id.

## Consequences

No admin UI for targets. Renaming a target id starts a new history. A removed target keeps its history and leaves orphaned previews for a platform admin.
