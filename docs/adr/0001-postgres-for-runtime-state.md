---
title: "0001 Postgres for runtime state"
parent: Architecture decisions
nav_order: 1
---

# 0001 Postgres for runtime state

- **Status:** Accepted
- **Date:** 2026-11-20

## Context

simple-cd keeps locks, deployments, previews, audit events and sessions. It should run with several replicas for availability. SQLite was the simplest option discussed.

## Decision

Runtime state lives in PostgreSQL. Row locks (`SELECT ... FOR UPDATE` on the target state) serialise decisions per target, a partial unique index enforces one active deployment per slot, and an advisory lock elects the replica that runs the background worker. No Redis or message queue.

## Consequences

Any replica serves any request. Operators run or rent a Postgres instance; backups of that database are the backups of simple-cd. Tests need a real Postgres.
