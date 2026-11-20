#!/usr/bin/env bash
# Creates the end-to-end environment: a kind cluster running Vault, Keycloak,
# Postgres, a fake GitHub API and simple-cd (installed with its own chart),
# next to a local OCI registry. Safe to run again; it rebuilds and redeploys.
#
# Needs docker, kubectl, helm, openssl and kind ($KIND, default "kind").
set -euo pipefail
cd "$(dirname "$0")/.."

KIND=${KIND:-kind}
CLUSTER=simple-cd-e2e
CTX=kind-$CLUSTER
REGISTRY=kind-registry           # container name; kind-registry:5000 inside the cluster
PUSH=127.0.0.1:5001              # the same registry from the host
TAG=${E2E_TAG:-$(date +%s)}
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

k() { kubectl --context "$CTX" "$@"; }
step() { printf '\n==> %s\n' "$*"; }

step "Registry and kind cluster"
if ! docker inspect "$REGISTRY" >/dev/null 2>&1; then
  docker run -d --restart=always --name "$REGISTRY" -p "$PUSH:5000" registry:3.1.2 >/dev/null
fi
if ! "$KIND" get clusters | grep -qx "$CLUSTER"; then
  "$KIND" create cluster --name "$CLUSTER" --config e2e/kind.yaml --wait 2m
fi
docker network connect kind "$REGISTRY" 2>/dev/null || true
# Let containerd pull kind-registry:5000/... over plain http.
for node in $("$KIND" get nodes --name "$CLUSTER"); do
  docker exec "$node" mkdir -p /etc/containerd/certs.d/kind-registry:5000
  printf '[host."http://kind-registry:5000"]\n' |
    docker exec -i "$node" cp /dev/stdin /etc/containerd/certs.d/kind-registry:5000/hosts.toml
done

step "Building images (tag $TAG)"
docker build -q -t "$PUSH/simple-cd:$TAG" -f build/server.Dockerfile .
docker build -q -t "$PUSH/simple-cd-executor:$TAG" -f build/executor.Dockerfile .
docker build -q -t "$PUSH/fake-github:$TAG" e2e/fakegithub
# The application the test deploys: any small long-running image will do.
docker tag "$PUSH/fake-github:$TAG" "$PUSH/e2e/demo:1"
for img in simple-cd:$TAG simple-cd-executor:$TAG fake-github:$TAG e2e/demo:1; do
  docker push -q "$PUSH/$img" >/dev/null
done
helm package e2e/testchart/demo -d "$TMP" >/dev/null
helm push "$TMP/demo-1.0.0.tgz" "oci://$PUSH/e2e/charts" --plain-http

step "Infrastructure"
sed "s/E2E_TAG/$TAG/g" e2e/manifests.yaml | k apply -f -
k -n simple-cd create configmap keycloak-realm --from-file=realm.json=dev/keycloak-realm.json \
  --dry-run=client -o yaml | k apply -f -
for d in postgres vault keycloak fake-github; do
  k -n simple-cd rollout status "deploy/$d" --timeout 5m
done

step "Vault: Kubernetes auth and secrets"
vault() { k -n simple-cd exec -i deploy/vault -- vault "$@"; }
vault auth list | grep -q '^kubernetes/' || vault auth enable kubernetes
vault write auth/kubernetes/config kubernetes_host=https://kubernetes.default.svc
printf 'path "secret/data/cd/*" { capabilities = ["read"] }\n' | vault policy write simple-cd-executor -
vault write auth/kubernetes/role/simple-cd-executor \
  bound_service_account_names=simple-cd-executor bound_service_account_namespaces=simple-cd \
  policies=simple-cd-executor ttl=15m

token=""
until [ -n "$token" ]; do
  token=$(k -n default get secret e2e-deployer-token -o jsonpath='{.data.token}' | base64 -d)
  [ -n "$token" ] || sleep 1
done
ca=$(k -n default get secret e2e-deployer-token -o jsonpath='{.data.ca\.crt}')
cat >"$TMP/kubeconfig" <<EOF
apiVersion: v1
kind: Config
clusters: [{name: kind, cluster: {server: "https://kubernetes.default.svc", certificate-authority-data: "$ca"}}]
users: [{name: deployer, user: {token: "$token"}}]
contexts: [{name: kind, context: {cluster: kind, user: deployer}}]
current-context: kind
EOF
vault kv put secret/cd/clusters/kind kubeconfig=- <"$TMP/kubeconfig" >/dev/null
vault kv put secret/cd/values/e2e-test values='greeting: hello from vault' >/dev/null

step "simple-cd"
openssl genrsa -out "$TMP/github.pem" 2048 2>/dev/null
k -n simple-cd create secret generic simple-cd \
  --from-literal=database-url='postgres://simplecd:simplecd@postgres:5432/simplecd?sslmode=disable' \
  --from-literal=oidc-client-secret=dev-secret \
  --from-file=github-private-key="$TMP/github.pem" \
  --from-literal=github-webhook-secret=e2e-webhook-secret \
  --dry-run=client -o yaml | k apply -f -
helm upgrade --install simple-cd deploy/helm/simple-cd --kube-context "$CTX" -n simple-cd \
  -f e2e/values.yaml --set image.tag="$TAG" --set executor.tag="$TAG" --wait --timeout 5m

step "Ready"
echo "simple-cd    http://127.0.0.1:18080"
echo "Keycloak     http://127.0.0.1:18081 (as http://keycloak:18081 for the issuer)"
echo "fake GitHub  http://127.0.0.1:18082"
echo "Run the test with: make e2e"
