---
title: "0005 Kubernetes over plain REST"
parent: Architecture decisions
nav_order: 5
---

# 0005 Kubernetes over plain REST

- **Status:** Accepted
- **Date:** 2026-11-20

## Context

The server needs three operations on Jobs and pod logs. client-go would add a large dependency tree.

## Decision

The Job Launcher talks to the Kubernetes API with `net/http`, the service account token and the cluster CA. The same approach serves Vault, GitHub and OCI registries.

## Consequences

Few dependencies and a small binary. Changes in those APIs are handled by hand; tests run against `httptest` servers.
