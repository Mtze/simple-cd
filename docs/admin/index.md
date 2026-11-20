---
title: Administrators
nav_order: 5
has_children: true
---

# Administrators

This section is for the platform team that installs and runs simple-cd.

## What you need

| Dependency | Used for | Page |
|---|---|---|
| Kubernetes cluster | runs the server and one executor Job per deployment | [Installation](installation.md) |
| PostgreSQL | all runtime state (CI tests against Postgres 18) | [Installation](installation.md) |
| Keycloak | login (OIDC) and roles | [Keycloak](keycloak.md) |
| GitHub App | webhooks, reading artifacts and files, reporting deployments | [GitHub App](github-app.md) |
| HashiCorp Vault | every secret the executor needs (KV v2, Kubernetes auth) | [Vault](vault.md) |
| Target clusters, VMs, hosts | where deployments go | [Target clusters](target-clusters.md), [Compose VMs](compose.md), [Ansible targets](ansible.md) |

## Setup order

1. Create the GitHub App, the Keycloak client and the Vault mount, policy and role.
2. Create the Kubernetes Secret and install the Helm chart.
3. Prepare target clusters and VMs and store their credentials in Vault.
4. Describe projects and targets in the [target configuration](configuration.md).

## Onboarding a project

1. Install the GitHub App on the repository.
2. Add the project and its targets to the target configuration and roll it out. The server reloads it without a restart.
3. Create the Keycloak roles for the project with the [script](keycloak.md#create-project-roles) and assign them.
4. For Compose targets: create `.env` and config files on the VM.
5. The developers add `.cd.yaml` and the `cd-publish` action, see [Getting started](../users/getting-started.md).

## Running it

- [Operations](operations.md): backups, retention, config reload, upgrades, scaling, health checks, logs.
- [Security model](security.md): trust boundaries and hardening.
- [Environment variables](../reference/environment.md): every setting of the server and the executor.
