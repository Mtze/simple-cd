---
title: Target clusters
parent: Administrators
nav_order: 5
---

# Target cluster preparation
{: .no_toc }

Helm targets deploy to a Kubernetes cluster with a kubeconfig that the executor reads from Vault.
Give simple-cd as few rights as possible: one service account per cluster, or per namespace.

1. TOC
{:toc}

## Normal Helm targets

The executor runs `helm upgrade --install --wait` into the target's `namespace`.
It does not create the namespace; your platform team does.

Create a service account with the built-in `admin` role, bound only to that namespace:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: simple-cd
  namespace: app
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: simple-cd-admin
  namespace: app
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: admin
subjects:
  - kind: ServiceAccount
    name: simple-cd
    namespace: app
---
apiVersion: v1
kind: Secret
metadata:
  name: simple-cd-token
  namespace: app
  annotations:
    kubernetes.io/service-account.name: simple-cd
type: kubernetes.io/service-account-token
```

`admin` covers everything a typical application chart creates inside a namespace, including the Helm release secrets.
Charts that create cluster-scoped objects (CRDs, ClusterRoles, other namespaces) fail with this setup; install those parts separately.

## Preview clusters

A preview target creates one namespace per pull request, named `pr-<repo>-<pr>` (shortened with a hash to stay within 63 characters).
The executor:

1. checks the namespace with `kubectl get namespace`,
2. creates it with `kubectl create` and the labels `cd/managed=true`, `cd/target=<target-id>` and `cd/pr=<number>`, or fails if it exists without exactly these labels,
3. installs the chart with Helm,
4. on teardown checks the labels again, runs `helm uninstall` and `kubectl delete namespace`.

The service account therefore needs to get, create and delete namespaces, and `admin` rights inside the namespaces it creates.
Kubernetes RBAC cannot grant rights only in namespaces that do not exist yet, so the practical setup binds `admin` cluster-wide:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: simple-cd
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: simple-cd-previews
  namespace: simple-cd
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: simple-cd-namespaces
rules:
  - apiGroups: [""]
    resources: ["namespaces"]
    verbs: ["get", "list", "create", "delete"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: simple-cd-namespaces
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: simple-cd-namespaces
subjects:
  - kind: ServiceAccount
    name: simple-cd-previews
    namespace: simple-cd
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: simple-cd-previews-admin
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: admin
subjects:
  - kind: ServiceAccount
    name: simple-cd-previews
    namespace: simple-cd
---
apiVersion: v1
kind: Secret
metadata:
  name: simple-cd-previews-token
  namespace: simple-cd
  annotations:
    kubernetes.io/service-account.name: simple-cd-previews
type: kubernetes.io/service-account-token
```

{: .warning }
> This service account can read and change namespaced objects, secrets included, in every namespace of the cluster.
> Use a **separate preview cluster** without production workloads or shared secrets.

The label check protects foreign namespaces from simple-cd's own teardown, but it is no access control.

The preview cluster also provides what previews need around them: wildcard DNS (for example `*.app.preview.example.org`), a Gateway (or ingress controller) with a wildcard certificate, and quotas or LimitRanges.
simple-cd manages none of this.

## Build the kubeconfig

Build a kubeconfig from the token Secret and store it in Vault:

```sh
#!/bin/sh
# Usage: make-kubeconfig.sh <namespace> <token-secret> <api-server-url> <vault-path>
# Example: make-kubeconfig.sh app simple-cd-token https://api.test.example.org:6443 secret/cd/clusters/test
set -eu
NS=$1 SECRET=$2 SERVER=$3 VAULT_PATH=$4
TOKEN=$(kubectl -n "$NS" get secret "$SECRET" -o jsonpath='{.data.token}' | base64 -d)
CA=$(kubectl -n "$NS" get secret "$SECRET" -o jsonpath='{.data.ca\.crt}')
FILE=$(mktemp)
trap 'rm -f "$FILE"' EXIT
cat > "$FILE" <<EOF
apiVersion: v1
kind: Config
clusters:
  - name: target
    cluster:
      server: $SERVER
      certificate-authority-data: $CA
users:
  - name: simple-cd
    user:
      token: $TOKEN
contexts:
  - name: simple-cd
    context:
      cluster: target
      user: simple-cd
      namespace: $NS
current-context: simple-cd
EOF
vault kv put "$VAULT_PATH" kubeconfig=@"$FILE"
```

The API server URL must be reachable from the executor pods.
The executor image contains only `helm` and `kubectl`, so exec-based credential plugins (for example cloud provider login helpers) do not work; use a token.
Rotate the token by deleting the token Secret, creating it again and running the script once more.

## Image pulls

simple-cd checks that images exist, but the target cluster pulls them.
For private registries, configure image pull secrets in the namespace or in your chart, for example through the target's Helm values.
