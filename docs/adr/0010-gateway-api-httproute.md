---
title: "0010 Gateway API HTTPRoute instead of Ingress"
parent: Architecture decisions
nav_order: 10
---

# 0010 Gateway API HTTPRoute instead of Ingress

- **Status:** Accepted
- **Date:** 2026-11-20

## Context

The chart exposed simple-cd with an `Ingress` and controller-specific annotations.
Gateway API (`gateway.networking.k8s.io/v1`) is the standard successor: platform teams run shared Gateways with TLS, and applications attach routes to them without knowing the controller.
The Ingress-NGINX controller, the most common Ingress implementation, is retired.

## Decision

The chart renders an optional `HTTPRoute` that attaches to an existing Gateway named in `httpRoute.parentRefs`.
The chart creates no Gateway and no Ingress; TLS terminates at the Gateway listener.
The end-to-end cluster installs the Gateway API CRDs (v1.7.0) and enables the route, so the API server validates it on every run.

## Consequences

Installations need a Gateway API implementation and a Gateway whose listener allows routes from the release namespace.
Upgrading from the Ingress values is a breaking change: `ingress.*` is gone and must be replaced by `httpRoute.*`.
The long-lived log stream (Server-Sent Events) depends on the Gateway implementation's request and idle timeouts, which the chart cannot set portably.
