---
title: Releasing
parent: Contributors
nav_order: 4
---

# Releasing

## Images

`.github/workflows/images.yml` builds three multi-arch images (`linux/amd64`, `linux/arm64`) and pushes them to GitHub Container Registry:

| Image | Dockerfile |
|---|---|
| `ghcr.io/mtze/simple-cd` | `build/server.Dockerfile` (UI and server, distroless) |
| `ghcr.io/mtze/simple-cd-executor` | `build/executor.Dockerfile` (executor, `helm`, `kubectl`, `ssh`) |
| `ghcr.io/mtze/simple-cd-executor-ansible` | `build/executor-ansible.Dockerfile` (executor, `ansible-core`, `git`, `ssh`) |

Tags:

| Trigger | Tags |
|---|---|
| push to `main` | `edge`, `sha-<short sha>` |
| tag `v1.2.3` | `1.2.3`, `1.2`, `latest`, `sha-<short sha>` |

## Cutting a release

1. Move the entries under **Unreleased** in `CHANGELOG.md` to a new version section with today's date.
2. Set `version` and `appVersion` in `deploy/helm/simple-cd/Chart.yaml` to the new version. The chart uses `appVersion` as the default image tag.
3. Merge to `main`.
4. Tag the merge commit and push the tag:
   ```sh
   git tag -a v0.2.0 -m "simple-cd 0.2.0"
   git push origin v0.2.0
   ```
5. Wait for the Images workflow, then create a GitHub release from the tag with the changelog section as notes.

The Helm chart is not published to a registry; users install it from a checkout of the tag.
The `cd-publish` action is versioned with the repository, so `Mtze/simple-cd/cd-publish@v0.2.0` works after the tag exists.

When you bump `helm`, `kubectl` or `ansible-core` in the executor Dockerfiles, update the pinned checksums next to the versions.

## Docs

`.github/workflows/pages.yml` builds `docs/` with Jekyll and deploys it to GitHub Pages on every push to `main` that touches `docs/`.
The site is served at <https://mtze.me/simple-cd/>.
