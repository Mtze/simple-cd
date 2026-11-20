#!/usr/bin/env bash
# Removes the end-to-end cluster and the local registry.
set -euo pipefail
"${KIND:-kind}" delete cluster --name simple-cd-e2e
docker rm -f kind-registry >/dev/null 2>&1 || true
