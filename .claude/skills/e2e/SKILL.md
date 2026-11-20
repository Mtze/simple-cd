---
name: e2e
description: Start, run, debug or extend the simple-cd end-to-end test in a kind cluster with Vault, Keycloak, a local OCI registry and a fake GitHub API. Use when a change touches the executor, runner, Vault, registry, login, webhooks or the Helm chart, or when the e2e CI job fails.
---

# End-to-end test

Background and the table of what runs where: `docs/contributors/testing.md#end-to-end-test`.

## Run

```sh
make e2e-up     # builds images, creates cluster simple-cd-e2e, installs everything; rerun after every code change
make e2e        # go test -tags e2e -count=1 -v ./e2e/
make e2e-down   # deletes the cluster and the kind-registry container
```

`make e2e-up` takes about 5 minutes the first time and 1 to 2 minutes after that. Run it in the background and wait for it to exit. It always rebuilds the images under a new tag, so the cluster runs the code of the working tree at the time the build started: rerun it after further edits.

Host ports, all on 127.0.0.1: simple-cd 18080, Keycloak 18081 (issuer `http://keycloak:18081/realms/simple-cd`), fake GitHub 18082, registry 5001.

## Debug a failure

```sh
k() { kubectl --context kind-simple-cd-e2e "$@"; }
k get pods,jobs -A
k -n simple-cd logs -l app.kubernetes.io/name=simple-cd --prefix --tail 300
k -n simple-cd logs -l app.kubernetes.io/name=simple-cd-executor --prefix --tail 300
k -n simple-cd logs deploy/fake-github --tail 100
k -n simple-cd exec deploy/postgres -- psql -U simplecd -d simplecd -c 'select id, target_id, status from deployments'
k -n simple-cd exec deploy/vault -- vault kv get secret/cd/values/e2e-test
```

Known traps:

- Keycloak imports `dev/keycloak-realm.json` only when the realm does not exist yet. After a realm change, restart it: `k -n simple-cd rollout restart deploy/keycloak`.
- The registry has no TLS; `SIMPLECD_REGISTRY_PLAIN_HTTP=kind-registry:5000` (set in `e2e/values.yaml`) is passed to the server and the executor. New registry code must honour it.
- The test reaches Keycloak as `keycloak:18081` through a custom dialer; a browser needs `127.0.0.1 keycloak` in `/etc/hosts`.
- `make bin/kind` does not work because the target is an absolute path; `make e2e-up` installs kind itself.
- The Gateway API CRDs are installed so the chart's `HTTPRoute` is validated; no Gateway controller runs, the test uses NodePorts.

## Extend

- Webhook payloads: `e2e/e2e_test.go` signs them with `e2e-webhook-secret`; the fake GitHub serves runs registered through `POST /_e2e/runs` and records statuses at `GET /_e2e/statuses`.
- GitHub endpoints simple-cd starts to use need a handler in `e2e/fakegithub/main.go` (standard library only).
- New targets go into `e2e/values.yaml`; their secrets into the Vault step of `e2e/up.sh`.
- Keep the test deterministic: wait on conditions with timeouts, never on fixed sleeps.
