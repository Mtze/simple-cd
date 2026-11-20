---
title: Getting started
parent: Users
nav_order: 1
---

# Getting started
{: .no_toc }

This page walks through onboarding a repository, from zero to the first deploy.

1. TOC
{:toc}

## What the platform team does first

Ask your simple-cd administrators to:

1. Install the simple-cd GitHub App on your repository ([GitHub App setup](../admin/github-app.md)).
2. Add your project and its targets to the target configuration ([target configuration](../admin/configuration.md)).
3. Create the Keycloak roles `project:<owner>/<repo>:developer` and `project:<owner>/<repo>:maintainer` and assign them ([Keycloak setup](../admin/keycloak.md)).
4. For Compose targets: create the deploy user, `.env` and other config files on the VM ([Compose VMs](../admin/compose.md)).

Then you add two things to the repository: a `.cd.yaml` file and the `cd-publish` step in your workflow.

## Step 1: add `.cd.yaml`

`.cd.yaml` lives in the repository root and describes how your artifacts map onto targets.
simple-cd reads it at the commit of each artifact, so changes to it travel with the code.
The full format is in the [`.cd.yaml` reference](../reference/cd-yaml.md).

A Helm example with two images, a test target and a preview target:

```yaml
helm:
  chart: oci://ghcr.io/example/charts/app
  set:                                   # applies to every helm target
    server.image.repository: "{images.server.repository}"
    server.image.digest: "{images.server.digest}"
    client.image: "{images.client.ref}"
  targets:                               # per target id, overrides helm.set
    app-test:
      values: deploy/values-test.yaml
    app-previews:
      values: deploy/values-preview.yaml
      set:
        ingress.host: "pr-{cd.pr}.app.preview.example.org"
```

A Compose example:

```yaml
compose:
  path: deploy/docker-compose.yml
```

Placeholders such as `{images.server.digest}` are plain text replacement.
If a placeholder has no value for a target, for example `{cd.pr}` on a target that is not a preview target, the artifact is not deployable to that target.

### Helm charts take digests

Your chart has to accept the image by digest, for example with values like `server.image.repository` and `server.image.digest`:

{% raw %}
```yaml
# templates/deployment.yaml in your chart
image: "{{ .Values.server.image.repository }}@{{ .Values.server.image.digest }}"
```
{% endraw %}

### Compose files take the image from a variable

For each image in `deploy.json`, the Compose file has to use `${<NAME>_IMAGE}`, where `<NAME>` is the image name in upper case with `-` replaced by `_`.
An image named `server` becomes `${SERVER_IMAGE}`, `web-ui` becomes `${WEB_UI_IMAGE}`.
Compose files must not contain `build:`.

```yaml
services:
  server:
    image: ${SERVER_IMAGE}
    env_file: .env
    ports: ["80:8080"]
```

## Step 2: publish from your workflow

Add the `cd-publish` action as the last step of the workflow that builds your images.
It writes `deploy.json` and uploads it as the workflow artifact `cd-deploy`.
When the run finishes successfully, simple-cd picks it up.

A complete example that builds two images, pushes an OCI Helm chart and publishes:

{% raw %}
```yaml
name: Build

on:
  pull_request:
  push:
    branches: [main]
    tags: ["v*"]

permissions:
  contents: read
  packages: write

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7

      - uses: docker/login-action@v4
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - uses: docker/setup-buildx-action@v4

      - id: server
        uses: docker/build-push-action@v7
        with:
          context: server
          push: true
          tags: ghcr.io/example/app-server:${{ github.sha }}

      - id: client
        uses: docker/build-push-action@v7
        with:
          context: client
          push: true
          tags: ghcr.io/example/app-client:${{ github.sha }}

      - uses: azure/setup-helm@v5

      - id: chart
        name: Push Helm chart
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          version="0.1.0-${GITHUB_RUN_NUMBER}.g${GITHUB_SHA::7}"
          echo "$GH_TOKEN" | helm registry login ghcr.io --username "$GITHUB_ACTOR" --password-stdin
          helm package deploy/chart --version "$version" --destination "$RUNNER_TEMP/chart"
          helm push "$RUNNER_TEMP/chart/app-$version.tgz" oci://ghcr.io/example/charts
          echo "version=$version" >> "$GITHUB_OUTPUT"

      - uses: Mtze/simple-cd/cd-publish@main
        with:
          images: |
            server=ghcr.io/example/app-server@${{ steps.server.outputs.digest }}
            client=ghcr.io/example/app-client@${{ steps.client.outputs.digest }}
          helm-version: ${{ steps.chart.outputs.version }}
          auto-deploy: ${{ github.event_name == 'pull_request' && 'app-previews' || '' }}
```
{% endraw %}

Things to get right:

- **Digests, not tags.** Use the `digest` output of `docker/build-push-action`. `cd-publish` rejects references without `@sha256:`.
- **Lowercase registry paths with the host.** `ghcr.io/example/app-server`, never `example/app-server`. `github.repository_owner` may contain upper case letters, so write the path out or lowercase it.
- **The chart name matches `.cd.yaml`.** `helm push app-<version>.tgz oci://ghcr.io/example/charts` ends up at `oci://ghcr.io/example/charts/app`, which is what `helm.chart` names. The chart version must be unique per build; a semver pre-release such as `0.1.0-42.gabc1234` works well.
- **One publishing workflow per commit.** simple-cd keeps one artifact per commit. If several workflow runs upload `cd-deploy` for the same commit, the newest run wins and the UI shows a warning.
- **Running the same commit as `push` and `pull_request` is fine.** Both runs publish for the same commit and the newer one wins, but a branch run never turns a pull request artifact back into a branch artifact, so previews keep working. Restricting `push` to your main branch and tags, as in the example, still saves CI time.
- **The run must succeed.** simple-cd ignores runs that did not conclude with `success`.
- **Pin actions.** In production workflows, pin `Mtze/simple-cd/cd-publish` and the other actions to a commit SHA.

The inputs of `cd-publish` are documented in the [deploy.json reference](../reference/deploy-json.md#cd-publish-inputs).

## Step 3: deploy

Open the simple-cd UI, pick your project and find the artifact under **Pull requests**, **Branches** or **Releases**.
Each target shows a check mark when the artifact is deployable there, or a cross with the reasons.
Click **Deploy** next to a target; [Deploying with the UI](deploying.md) explains the dialog.

If nothing shows up, see [Troubleshooting](troubleshooting.md#the-artifact-does-not-show-up).
