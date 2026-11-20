---
title: "0007 Deploy slots, opt-in locks and a waiting place for auto-deploys"
parent: Architecture decisions
nav_order: 7
---

# 0007 Deploy slots, opt-in locks and a waiting place for auto-deploys

- **Status:** Accepted
- **Date:** 2026-11-20

## Context

Two people must not deploy to the same environment at once, test environments need a reservation, and previews of different pull requests must not block each other.

## Decision

A slot is the target, or the target plus the pull request for previews. One deployment may be pending or running per slot. Lockable targets lock for the deploying user from the start of a deploy, also after a failure; maintainers and platform admins can take over. Auto-deploys never lock and wait in one place per slot, where the newest replaces an older one.

## Consequences

Manual deploys to a busy slot are rejected instead of queued. Previews are never lockable.
