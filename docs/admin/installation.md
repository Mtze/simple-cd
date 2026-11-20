---
title: Installation
parent: Administrators
nav_order: 1
---

# Installation with the Helm chart
{: .no_toc }

1. TOC
{:toc}

## Prerequisites

- A Kubernetes cluster for simple-cd itself, with an ingress controller and TLS.
- A PostgreSQL database and a user that may create tables. simple-cd runs its migrations at start.
- The [Keycloak client](keycloak.md), the [GitHub App](github-app.md) and the [Vault setup](vault.md).
- Network access from the simple-cd pods to Postgres, Keycloak, the GitHub API, your OCI registries and the Kubernetes API, and from the executor pods to Vault, the target clusters and the target VMs.

## Images

The release workflow publishes three images for `linux/amd64` and `linux/arm64`:

| Image | Contents |
|---|---|
| `ghcr.io/mtze/simple-cd` | the server with the embedded UI (distroless, runs as uid 65532) |
| `ghcr.io/mtze/simple-cd-executor` | the executor with `helm`, `kubectl` and the `ssh` client, for Helm and Compose targets |
| `ghcr.io/mtze/simple-cd-executor-ansible` | the executor with `ansible-core`, `git` and the `ssh` client, for Ansible targets |

The executor image has no Docker CLI: Compose deployments run `docker compose` on the VM over SSH.

## Secret

Create the Secret before installing the chart.
The chart never renders it, so you can also manage it with an external secrets operator.

| Key | Content |
|---|---|
| `database-url` | Postgres URL, e.g. `postgres://simplecd:...@db.example.org:5432/simplecd?sslmode=require` |
| `oidc-client-secret` | the client secret of the Keycloak client |
| `github-private-key` | the GitHub App private key (PEM) |
| `github-webhook-secret` | the GitHub App webhook secret |
| `registry-auth` | optional: a Docker `config.json` with credentials for private registries. Set `registryAuth: true` to use it. |

```sh
kubectl create namespace simple-cd
kubectl -n simple-cd create secret generic simple-cd \
  --from-literal=database-url='postgres://simplecd:secret@db.example.org:5432/simplecd?sslmode=require' \
  --from-literal=oidc-client-secret='...' \
  --from-file=github-private-key=simple-cd.private-key.pem \
  --from-literal=github-webhook-secret='...' \
  --from-file=registry-auth=registry-config.json
```

The server uses `registry-auth` only to check that images and charts exist.
Target clusters pull images with their own credentials, and Helm targets log in to the chart registry with `registryCredentials` from Vault.

## Values

The chart lives in the repository at `deploy/helm/simple-cd`.

| Value | Default | Meaning |
|---|---|---|
| `baseURL` | `https://cd.example.org` | public URL of simple-cd, used for the OIDC redirect, cookies, the CSRF origin check and log links |
| `replicaCount` | `2` | server replicas, see [scaling](operations.md#scaling) |
| `image.repository`, `image.tag`, `image.pullPolicy` | `ghcr.io/mtze/simple-cd`, chart `appVersion`, `IfNotPresent` | server image |
| `executor.image` | `ghcr.io/mtze/simple-cd-executor` | executor image for Helm and Compose |
| `executor.ansibleImage` | `ghcr.io/mtze/simple-cd-executor-ansible` | executor image for Ansible |
| `executor.tag` | chart `appVersion` | tag of both executor images |
| `executor.serviceAccount` | `simple-cd-executor` | service account of the executor Jobs, bound to the Vault role |
| `executor.serviceAccountAnnotations` | `{}` | annotations for that service account |
| `vault.address` | `https://vault.example.org` | Vault URL (`VAULT_ADDR`) |
| `vault.role` | `simple-cd-executor` | Vault Kubernetes auth role |
| `vault.authMount` | `kubernetes` | Vault auth mount path |
| `vault.namespace` | `""` | Vault Enterprise namespace |
| `oidc.issuer` | `https://keycloak.example.org/realms/main` | Keycloak realm URL |
| `oidc.clientID` | `simple-cd` | Keycloak client ID; roles are read from this client |
| `oidc.sessionTTL` | `4h` | session lifetime |
| `github.appID` | `""` (required) | GitHub App ID |
| `github.apiURL` | `https://api.github.com` | GitHub API URL, change for GitHub Enterprise Server |
| `github.gitSSHHost` | `github.com` | SSH host for cloning Ansible playbooks from the app repository |
| `existingSecret` | `simple-cd` | name of the Secret above |
| `registryAuth` | `false` | mount `registry-auth` from the Secret |
| `config` | one default registry, no targets | the [target configuration](configuration.md), rendered into a ConfigMap |
| `service.type`, `service.port` | `ClusterIP`, `80` | Service |
| `ingress.enabled`, `className`, `annotations`, `host`, `tls.secretName` | `false`, `""`, `{}`, `cd.example.org`, `""` | Ingress |
| `resources` | requests 50m CPU, 64Mi; limit 256Mi | server pod resources |
| `podAnnotations`, `nodeSelector`, `tolerations`, `affinity` | empty | scheduling |

A minimal values file:

```yaml
baseURL: https://cd.example.org
github:
  appID: "123456"
oidc:
  issuer: https://keycloak.example.org/realms/main
vault:
  address: https://vault.example.org
ingress:
  enabled: true
  className: nginx
  host: cd.example.org
  tls:
    secretName: cd-example-org-tls
config:
  defaults:
    registries: [ghcr.io/example/]
  targets:
    - id: app-test
      project: example/app
      type: helm
      lockable: true
      cluster: vault:secret/cd/clusters/test
      namespace: app
```

Install from a checkout of the repository:

```sh
git clone https://github.com/Mtze/simple-cd.git
helm upgrade --install simple-cd simple-cd/deploy/helm/simple-cd \
  --namespace simple-cd --values my-values.yaml
```

Pin `image.tag` and `executor.tag` to a release (for example `0.1.0`) instead of relying on the chart's `appVersion` when you track `main`.

## What the chart creates

| Object | Purpose |
|---|---|
| Deployment | the server pods, non-root (uid 65532), read-only root filesystem, all capabilities dropped, `RuntimeDefault` seccomp profile. `/readyz` is the readiness probe, `/healthz` the liveness probe. |
| ConfigMap `<release>-config` | `config.yaml` from `values.config`, mounted at `/config` |
| ServiceAccount `<release>` and a Role | the server may create, get, list and delete Jobs and read pods and pod logs, in its own namespace only |
| ServiceAccount `simple-cd-executor` | identity of the executor Jobs for the Vault login. It has no Kubernetes permissions. |
| Service, optional Ingress | HTTP on port 8080 behind the Service |

Executor Jobs run in the release namespace.

## Check the installation

1. `kubectl -n simple-cd get pods` shows the pods ready. The logs are JSON lines and show `simple-cd listening`.
2. Open `baseURL`. You are redirected to Keycloak and back.
3. In the GitHub App settings, **Advanced > Recent deliveries** shows successful deliveries (status 202 or 204).
4. Log in as a platform admin and open **Admin**. No config error banner appears.

## Validate the target configuration in CI

Keep the target configuration in a Git repository and check it in CI before rolling it out:

```sh
docker run --rm -v "$PWD":/work ghcr.io/mtze/simple-cd:0.1.0 validate /work/config.yaml
# ok: 2 projects, 5 targets
```

`validate` prints every problem at once and exits with status 1 when the file is invalid.
