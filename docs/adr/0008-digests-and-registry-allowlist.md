---
title: "0008 Images pinned by digest, registries on an allowlist"
parent: Architecture decisions
nav_order: 8
---

# 0008 Images pinned by digest, registries on an allowlist

- **Status:** Accepted
- **Date:** 2026-11-20

## Context

Tags move, and a workflow could point a deployment at any image on the internet.

## Decision

`deploy.json` accepts images only with a `sha256` digest. Images and charts must be under a registry prefix allowed for the project. The server checks that they exist when the artifact arrives and again right before a deploy; access denied counts as missing.

## Consequences

Private registries need credentials on the server for the check. Compose files must take images from `${<NAME>_IMAGE}` variables.
