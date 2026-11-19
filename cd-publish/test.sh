#!/usr/bin/env bash
# Tests for publish.sh. Run: bash cd-publish/test.sh
set -uo pipefail
cd "$(dirname "$0")"
D=sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
out=$(mktemp -d)
failures=0

run() { INPUT_IMAGES="$1" INPUT_HELM_VERSION="$2" INPUT_AUTO_DEPLOY="$3" OUT_DIR="$out" bash publish.sh >/dev/null 2>&1; }

expect_ok() {
  if run "$1" "$2" "$3" && [[ "$(cat "$out/deploy.json")" == "$4" ]]; then echo "ok   $5"
  else echo "FAIL $5: got $(cat "$out/deploy.json" 2>/dev/null)"; failures=$((failures+1)); fi
}
expect_fail() {
  if run "$1" "$2" "$3"; then echo "FAIL $4: accepted"; failures=$((failures+1)); else echo "ok   $4"; fi
}

expect_ok "app=ghcr.io/org/app@$D" "" "" \
  "{\"version\":1,\"images\":[{\"name\":\"app\",\"ref\":\"ghcr.io/org/app@$D\"}]}" "single image"
expect_ok $'server=ghcr.io/org/server:1.2@'"$D"$'\n\n  web-ui = localhost:5000/org/web@'"$D" "1.2.0-pr4.abc" "app-previews, app-test" \
  "{\"version\":1,\"images\":[{\"name\":\"server\",\"ref\":\"ghcr.io/org/server:1.2@$D\"},{\"name\":\"web-ui\",\"ref\":\"localhost:5000/org/web@$D\"}],\"helm\":{\"version\":\"1.2.0-pr4.abc\"},\"autoDeploy\":[\"app-previews\",\"app-test\"]}" \
  "two images, helm and auto-deploy"
expect_ok "" "2.0.0" "" "{\"version\":1,\"images\":[],\"helm\":{\"version\":\"2.0.0\"}}" "helm only"
expect_fail "app=ghcr.io/org/app:latest" "" "" "tag without digest"
expect_fail "app=org/app@$D" "" "" "missing registry"
expect_fail "App=ghcr.io/org/app@$D" "" "" "uppercase name"
expect_fail $'a=ghcr.io/o/a@'"$D"$'\na=ghcr.io/o/b@'"$D" "" "" "duplicate name"
expect_fail "app=ghcr.io/org/app@$D\"},{\"x" "" "" "JSON injection in ref"
expect_fail "" "1.0\"" "" "JSON injection in version"
expect_fail "app=ghcr.io/org/app@$D" "" "Bad_Target" "invalid target"
expect_fail "" "" "" "nothing to publish"

rm -rf "$out"
[[ $failures -eq 0 ]] && echo "all passed" || { echo "$failures failed"; exit 1; }
