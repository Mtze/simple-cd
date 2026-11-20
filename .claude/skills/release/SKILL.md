---
name: release
description: Cut a simple-cd release - move the changelog, bump the chart version, tag, wait for the multi-arch images and create the GitHub release. Use when asked to release, tag a version or publish images with a version number.
---

# Release

Tagging and publishing are outward-facing: confirm the version number and each push with the user before doing it.
Background: `docs/contributors/releasing.md`.

## Steps

1. **Check main is green.** `gh run list --branch main --limit 5`; the CI, Images and Docs runs of the release commit must pass. Run the `verify-change` skill locally if in doubt.
2. **Pick the version** (Semantic Versioning). Breaking entries in the changelog (**Breaking:**) mean a minor bump before 1.0 and a major bump after.
3. **Changelog.** Move everything under `## [Unreleased]` into `## [X.Y.Z] - YYYY-MM-DD` and leave an empty Unreleased section.
4. **Chart.** Set `version` and `appVersion` in `deploy/helm/simple-cd/Chart.yaml` to `X.Y.Z`. The chart uses `appVersion` as the default image tag.
5. **Docs.** Update version examples that name the previous release (README install, `docs/admin/installation.md`, `Mtze/simple-cd/cd-publish@...` in the user docs). Once a release exists, the install instructions should pin it instead of `edge`.
6. **Commit** "Release X.Y.Z" on main and push.
7. **Tag** the commit and push the tag:

   ```sh
   git tag -a vX.Y.Z -m "simple-cd X.Y.Z"
   git push origin vX.Y.Z
   ```

8. **Images.** The tag triggers `.github/workflows/images.yml`, which pushes `X.Y.Z`, `X.Y` and `sha-<sha>` for `ghcr.io/mtze/simple-cd`, `simple-cd-executor` and `simple-cd-executor-ansible`. Wait for it and check the tags exist:

   ```sh
   gh api /users/Mtze/packages/container/simple-cd/versions --jq '.[0].metadata.container.tags'
   ```

9. **GitHub release** from the tag with the changelog section as notes:

   ```sh
   gh release create vX.Y.Z --title "simple-cd X.Y.Z" --notes-file <section.md>
   ```

The chart is not published to a registry; users install it from a checkout of the tag.
When executor tool versions change (`helm`, `kubectl`, `ansible-core`), the pinned checksums in the Dockerfiles change with them.
