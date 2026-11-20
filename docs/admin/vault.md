---
title: Vault
parent: Administrators
nav_order: 4
---

# Vault setup
{: .no_toc }

Every secret a deployment needs lives in Vault: kubeconfigs, SSH keys, secret Helm values and registry credentials.
The server never reads them.
Each executor Job logs in to Vault with its Kubernetes service account token, reads only what its deployment needs and writes it to an in-memory volume.

1. TOC
{:toc}

## KV v2 mount

simple-cd reads KV version 2 secrets.
A reference in the target configuration such as `vault:secret/cd/clusters/test` means:

- `secret` is the mount (always the first path segment),
- `cd/clusters/test` is the path inside the mount,
- the executor reads `GET /v1/secret/data/cd/clusters/test`.

KV version 1 and mounts with a `/` in their path are not supported.

```sh
vault secrets enable -path=secret kv-v2
```

## Kubernetes auth for the executor

The executor Jobs run in the simple-cd namespace as the service account `simple-cd-executor`.
Configure Kubernetes auth against the cluster that runs simple-cd:

```sh
vault auth enable kubernetes

# When Vault runs outside that cluster, also pass token_reviewer_jwt and kubernetes_ca_cert.
vault write auth/kubernetes/config kubernetes_host=https://kubernetes.platform.example.org:6443
```

A policy that only allows reading below `secret/data/cd/`:

```sh
vault policy write simple-cd-executor - <<'EOF'
path "secret/data/cd/*" {
  capabilities = ["read"]
}
EOF
```

A role bound to the executor service account:

```sh
vault write auth/kubernetes/role/simple-cd-executor \
  bound_service_account_names=simple-cd-executor \
  bound_service_account_namespaces=simple-cd \
  token_policies=simple-cd-executor \
  token_ttl=1h
```

Set the role name in `vault.role` (`SIMPLECD_VAULT_ROLE`) and the auth mount in `vault.authMount` (`SIMPLECD_VAULT_AUTH_MOUNT`, default `kubernetes`).
For Vault Enterprise namespaces set `vault.namespace`.

v1 uses one role for all executor Jobs.
Every Job can therefore read every secret below `secret/cd/`.
Keep that path for simple-cd only.

## Secret conventions

Each kind of reference expects fixed keys.
All values are strings.

| Referenced from | Keys | Notes |
|---|---|---|
| `cluster` (Helm targets) | `kubeconfig` | a complete kubeconfig, see [target clusters](target-clusters.md#build-the-kubeconfig) |
| `values` as a `vault:` reference (Helm targets) | `values` | a YAML document, merged over the repository values and below `helm.set` |
| `registryCredentials` (Helm targets) | `username`, `password` | used for `helm registry login` to the chart's registry host |
| `ssh` (Compose targets) | `private_key`, `known_hosts`, `user` (optional, default `deploy`) | `known_hosts` is required, there is no trust on first use |
| `ansible.credentials` (Ansible targets) | `private_key`, `known_hosts` (required for SSH git URLs), `user` (optional) | `known_hosts` should list the git host and every managed host |

Missing required keys fail the deployment with a message that names the key, never a value.

### Examples

```sh
# Helm target: cluster access and secret values
vault kv put secret/cd/clusters/test kubeconfig=@kubeconfig-test.yaml
vault kv put secret/cd/values/app-test values=@values-app-test.yaml

# Helm target: credentials for a private chart registry
vault kv put secret/cd/registries/ghcr-example username=cd-bot password=@ghcr-token.txt

# Compose target: SSH to the VM
ssh-keyscan -t ed25519 vm1.example.org > known_hosts   # verify the fingerprint out of band
vault kv put secret/cd/ssh/app-prod private_key=@id_ed25519 known_hosts=@known_hosts user=deploy

# Ansible target: SSH key for git and the managed hosts
vault kv put secret/cd/ssh/tools private_key=@id_ed25519 known_hosts=@known_hosts_tools user=ansible
```

## How the executor handles secrets

- It logs in with the service account token from `/var/run/secrets/kubernetes.io/serviceaccount/token`.
- It writes secret material with mode `0600` to `/secrets`, an `emptyDir` with `medium: Memory` (16 MiB limit), so nothing touches a disk or etcd.
- It merges secret Helm values into the values file in `/secrets` and keeps registry logins there too.
- The Job spec only carries Vault paths, never secret values.
- Vault error messages never contain secret data; YAML errors in secret values are reported without their content.

For local development the executor also accepts `VAULT_TOKEN` instead of Kubernetes auth, see [local development](../contributors/development.md).
