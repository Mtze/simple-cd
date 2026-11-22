---
title: "0011 Live UI updates through Postgres NOTIFY and Server-Sent Events"
parent: Architecture decisions
nav_order: 11
---

# 0011 Live UI updates through Postgres NOTIFY and Server-Sent Events

- **Status:** Accepted
- **Date:** 2026-11-22

## Context

The UI polled the artifact list every 30 seconds, so a new build appeared up to half a minute after its webhook.
A webhook reaches one replica, but the browser may hold its connection to another one, so an in-memory broadcast is not enough.
simple-cd keeps all shared state in Postgres ([ADR 0001](0001-postgres-for-runtime-state.md)) and avoids new infrastructure.

## Decision

- The store sends `pg_notify('simplecd_events', ...)` when the artifact list of a project changes. Postgres delivers it after the commit.
- Every replica holds one connection with `LISTEN simplecd_events` and passes events to an in-memory hub.
- `GET /api/events` streams them as Server-Sent Events. Events only name the project; the UI reloads through the normal endpoints, so the stream reveals nothing the API does not.
- The UI opens one `EventSource` per tab and keeps polling as a fallback for missed events.

## Consequences

- New builds show up within a second on every replica, without Redis or a message broker.
- Each replica uses one more database connection, and each open page holds one HTTP connection; proxies must allow long-lived streams.
- Delivery is best effort: events sent while a listener reconnects or a stream lags are lost, and polling closes the gap.
- Further kinds of events (deployments, locks) only need a `notify` call in the store and a filter in the UI.
