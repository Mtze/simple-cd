#!/usr/bin/env bash
# Builds deploy.json from the action inputs. Every value is validated against
# a strict pattern, so it can be written into JSON without escaping.
set -euo pipefail

fail() { echo "::error title=simple-cd publish::$*" >&2; exit 1; }

name_re='^[a-z0-9][a-z0-9_-]{0,62}$'
# The registry host needs a dot or a port, or is localhost (Docker's rule).
ref_re='^(localhost|[a-z0-9-]+(\.[a-z0-9-]+)+|[a-z0-9-]+:[0-9]+)(:[0-9]+)?/[a-z0-9._/-]+(:[A-Za-z0-9._-]+)?@sha256:[a-f0-9]{64}$'
version_re='^[A-Za-z0-9.+_-]{1,128}$'
target_re='^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$'

images=()
seen=" "
while IFS= read -r line || [[ -n "$line" ]]; do
  line="${line//[[:space:]]/}"
  [[ -z "$line" ]] && continue
  [[ "$line" == *=* ]] || fail "image line '$line' must be name=ref"
  name="${line%%=*}"
  ref="${line#*=}"
  [[ "$name" =~ $name_re ]] || fail "image name '$name' may only use lowercase letters, digits, - and _"
  [[ "$ref" =~ $ref_re ]] || fail "image '$name': '$ref' must include the registry and be pinned by digest (@sha256:...)"
  [[ "$seen" == *" $name "* ]] && fail "image name '$name' is used twice"
  seen+="$name "
  images+=("{\"name\":\"$name\",\"ref\":\"$ref\"}")
done <<< "${INPUT_IMAGES:-}"

helm=""
if [[ -n "${INPUT_HELM_VERSION:-}" ]]; then
  [[ "$INPUT_HELM_VERSION" =~ $version_re ]] || fail "helm-version '$INPUT_HELM_VERSION' is not a valid version"
  helm=",\"helm\":{\"version\":\"$INPUT_HELM_VERSION\"}"
fi

[[ ${#images[@]} -gt 0 || -n "$helm" ]] || fail "set images, helm-version or both"

targets=()
for t in ${INPUT_AUTO_DEPLOY//,/ }; do
  [[ "$t" =~ $target_re ]] || fail "auto-deploy target '$t' is not a valid target id"
  targets+=("\"$t\"")
done

join() { local IFS=,; echo "$*"; }
auto=""
[[ ${#targets[@]} -gt 0 ]] && auto=",\"autoDeploy\":[$(join "${targets[@]}")]"

mkdir -p "$OUT_DIR"
printf '{"version":1,"images":[%s]%s%s}\n' "$(join "${images[@]+"${images[@]}"}")" "$helm" "$auto" > "$OUT_DIR/deploy.json"
echo "deploy.json:"
cat "$OUT_DIR/deploy.json"
