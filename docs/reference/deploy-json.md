---
title: deploy.json and cd-publish
parent: Reference
nav_order: 2
---

# `deploy.json` and `cd-publish`
{: .no_toc }

`deploy.json` carries the results of one build: image digests, the chart version and auto-deploy requests.
The `cd-publish` action writes it and uploads it as the workflow artifact `cd-deploy`.

1. TOC
{:toc}

## `deploy.json`

```json
{
  "version": 1,
  "images": [
    { "name": "server", "ref": "ghcr.io/example/app-server@sha256:3f1c..." },
    { "name": "client", "ref": "ghcr.io/example/app-client@sha256:9ab0..." }
  ],
  "helm": { "version": "0.1.0-42.gabc1234" },
  "autoDeploy": ["app-previews"]
}
```

| Field | Required | Meaning |
|---|---|---|
| `version` | yes | always `1` |
| `images` | `images` or `helm` | list of images built by this run |
| `images[].name` | yes | lowercase letters, digits, `-` and `_`, starting with a letter or digit, at most 63 characters, unique |
| `images[].ref` | yes | `host/path[:tag]@sha256:<64 hex>`. The host needs a dot or a port, or is `localhost`. |
| `helm.version` | with `helm` | the chart version pushed by this run; the chart itself is named in `.cd.yaml` |
| `autoDeploy` | no | target ids to deploy automatically; each target must allow it with its `autoDeploy` rule |

Unknown fields are errors.
The repository, commit, source and pull request number are not part of `deploy.json`: simple-cd takes them from GitHub.

### How simple-cd picks it up

- Only from workflow runs that completed with conclusion `success`.
- From the newest non-expired workflow artifact named `cd-deploy` of the run, file `deploy.json`, at most 1 MiB.
- One artifact per commit: when several runs publish for the same commit, the run with the higher run id wins, and the UI warns when they come from different workflows.
- simple-cd stores the content right away, so the artifact's expiry on GitHub does not matter afterwards.

## cd-publish inputs

Pin the action to a release tag (`Mtze/simple-cd/cd-publish@v0.1.0`) or a commit SHA; `@main` runs whatever is on `main` of this repository.

| Input | Default | Meaning |
|---|---|---|
| `images` | `""` | one image per line as `name=ref`. Blank lines and spaces are ignored. The ref must be pinned by digest, for example from the `digest` output of `docker/build-push-action`. |
| `helm-version` | `""` | the version of the Helm chart this build pushed, `[A-Za-z0-9.+_-]`, at most 128 characters |
| `auto-deploy` | `""` | target ids to deploy automatically, separated by spaces or commas |

At least one of `images` and `helm-version` must be set.

The action validates every value at least as strictly as the server, fails the step with an error annotation on invalid input, prints the resulting `deploy.json` and uploads it as `cd-deploy` with a retention of 14 days.

{% raw %}
```yaml
- uses: Mtze/simple-cd/cd-publish@v0.1.0
  with:
    images: |
      server=ghcr.io/example/app-server@${{ steps.server.outputs.digest }}
      client=ghcr.io/example/app-client@${{ steps.client.outputs.digest }}
    helm-version: ${{ steps.chart.outputs.version }}
    auto-deploy: app-previews
```
{% endraw %}

The action accepts only lowercase registry paths.
Run it once per workflow run: the artifact name `cd-deploy` is fixed.

A complete workflow is in [Getting started](../users/getting-started.md#step-2-publish-from-your-workflow).
