---
title: "0004 One Kubernetes Job per deployment, secrets from Vault"
parent: Architecture decisions
nav_order: 4
---

# 0004 One Kubernetes Job per deployment, secrets from Vault

- **Status:** Accepted
- **Date:** 2026-11-20

## Context

Deployments need kubeconfigs, SSH keys and Helm values with secrets. The server should never hold them, and a compromised replica should not be able to read them.

## Decision

Every deployment runs as its own Job with the `simple-cd-executor` service account. The executor logs in to Vault with the Kubernetes auth method and reads only the paths of its target into an in-memory volume. The server only creates Jobs and reads their logs.

## Consequences

Vault Kubernetes auth is a hard prerequisite. Secrets never reach etcd or the server. The job specification travels as an environment variable and is limited to 120 KiB.
