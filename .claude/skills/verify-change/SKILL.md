---
name: verify-change
description: Run every check CI runs on simple-cd, with a real Postgres, and the definition of done. Use before calling a change finished, before committing, or when asked to "run the tests", "verify", "check CI locally".
---

# Verify a change

CI (`.github/workflows/ci.yml`) runs all of this on every push. Run the parts your change touches, and all of it before a commit that touches Go.

## 1. Database for the Go tests

Tests in `store`, `core`, `api`, `auth`, `ingest` and `worker` need `SIMPLECD_TEST_DATABASE_URL` and are **skipped silently** without it. Check that it is set, or start one:

```sh
make dev-up                     # Postgres on 127.0.0.1:5434; make test-go uses it automatically
# or a throwaway one:
docker run -d --name simplecd-test-pg -e POSTGRES_PASSWORD=test -e POSTGRES_DB=simplecd -p 127.0.0.1:5433:5432 postgres:18
export SIMPLECD_TEST_DATABASE_URL='postgres://postgres:test@127.0.0.1:5433/simplecd?sslmode=disable'
```

Each test gets its own schema, so parallel runs are fine.

## 2. Checks

```sh
make lint                                    # gofmt and go vet
go test -race -count=1 ./...                 # look for "SKIP ... SIMPLECD_TEST_DATABASE_URL" with -v if unsure
go run ./cmd/simple-cd validate dev/config.yaml
make test-web                                # when web/ changed; CI also runs npm run build
bash cd-publish/test.sh                      # when cd-publish/ changed
helm lint deploy/helm/simple-cd --set github.appID=1
helm template ci deploy/helm/simple-cd --set github.appID=1 --set httpRoute.enabled=true \
  --set 'httpRoute.parentRefs[0].name=public' --set registryAuth=true >/dev/null
```

Run the end-to-end test (the `e2e` skill) when the change touches the executor, the runner, Vault, the registry check, login, webhooks, the chart or `e2e/`.

## 3. Done means

- New behaviour has a test, and that test fails without the change (revert the fix briefly and run it).
- Existing tests were changed only after checking that the test, not the code, was wrong.
- `docs/` describes the new behaviour; `CHANGELOG.md` **Unreleased** has an entry (mark breaking changes with **Breaking:**).
- API changes are in `docs/reference/api.md` and `web/src/api/types.ts`; settings in `docs/reference/environment.md`; target fields in `docs/admin/configuration.md`; chart values in `docs/admin/installation.md`.
- No em or en dashes in the diff:

  ```sh
  git diff | grep '^+' | grep -nP '[\x{2013}\x{2014}]'
  ```

- Changes at a trust boundary passed the `security-invariants` checklist.
