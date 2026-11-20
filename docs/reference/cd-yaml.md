---
title: .cd.yaml
parent: Reference
nav_order: 1
---

# `.cd.yaml` reference
{: .no_toc }

`.cd.yaml` sits in the repository root.
It describes how the build results of a commit map onto targets: which chart to install and how image references become Helm values, which Compose file to ship, which playbook to run.
simple-cd reads it at the commit of each artifact and stores a snapshot together with the files it names, so later changes to the repository never change an existing artifact.

1. TOC
{:toc}

## Example

```yaml
helm:
  chart: oci://ghcr.io/example/charts/app
  set:
    server.image.repository: "{images.server.repository}"
    server.image.digest: "{images.server.digest}"
    client.image: "{images.client.ref}"
  targets:
    app-test:
      values: deploy/values-test.yaml
    app-previews:
      values: deploy/values-preview.yaml
      set:
        ingress.host: "pr-{cd.pr}.app.preview.example.org"

compose:
  path: deploy/docker-compose.yml

ansible:
  playbook: deploy/site.yml
```

All three sections are optional.
A target can only use an artifact whose `.cd.yaml` has the section for the target's type.

## Rules

- Unknown keys are errors.
- Paths are relative to the repository root and must stay inside it: no leading `/`, no `..` escape.
- Files simple-cd reads from the repository are limited to 1 MiB.
- An invalid `.cd.yaml` makes the artifact not deployable to any target; the error shows up as the reason.

## `helm`

| Field | Required | Meaning |
|---|---|---|
| `chart` | yes | the chart as an OCI reference, `oci://<host>/<path>/<name>`. The version comes from `deploy.json`. Must be under an allowed registry prefix of the project. |
| `set` | no | map from a dotted values path to a template string, for every Helm target |
| `targets.<id>.values` | no | path of a values file in the repository, for the target with this id |
| `targets.<id>.set` | no | more `set` entries for this target; they override entries of `helm.set` with the same key |

`set` keys are dotted paths: `server.image.digest: "..."` becomes

```yaml
server:
  image:
    digest: "..."
```

All `set` values are strings.
Keys under `targets` are target ids from the [target configuration](../admin/configuration.md); entries for unknown ids are ignored.

### Value precedence

Later entries win; maps merge, everything else replaces:

1. the chart's defaults,
2. `helm.targets.<id>.values` from the repository,
3. `values` of the target in the target configuration (inline or from Vault),
4. `helm.set` plus `helm.targets.<id>.set`, with placeholders filled in.

## Placeholders

`set` values may contain placeholders in curly braces.
They are plain text replacement, there is no template logic.

| Placeholder | Value |
|---|---|
| `{images.<name>.ref}` | the pinned reference `host/path@sha256:...` (without the tag) |
| `{images.<name>.repository}` | `host/path` |
| `{images.<name>.digest}` | `sha256:...` |
| `{images.<name>.tag}` | the tag if the reference in `deploy.json` has one before the digest (`host/path:1.2@sha256:...`), otherwise empty |
| `{cd.sha}` | the full commit SHA |
| `{cd.pr}` | the pull request number, only on preview targets |
| `{cd.target}` | the target id |
| `{cd.namespace}` | the Kubernetes namespace: the target's `namespace`, or the preview namespace `pr-<repo>-<pr>` |

`<name>` is the image name from `deploy.json`.
A placeholder that is unknown or empty for a target makes the artifact not deployable to that target, with the reason `placeholders without a value for this target`.
This way a deploy never goes out with an empty host name or image.

## `compose`

| Field | Required | Meaning |
|---|---|---|
| `path` | yes | path of the Compose file in the repository |

The Compose file must:

- use `${<NAME>_IMAGE}` (or a form like `${<NAME>_IMAGE:-...}`) for each image in `deploy.json`, where `<NAME>` is the image name in upper case with `-` replaced by `_`,
- not contain `build:` in any service.

The executor uploads it to the VM as `docker-compose.yml`, see [Compose VMs](../admin/compose.md).

## `ansible`

| Field | Required | Meaning |
|---|---|---|
| `playbook` | yes | path of the playbook in the repository |

Only used by Ansible targets with `ansible.source: repo`; targets with an external Git source take the playbook from the target configuration.
See [Ansible targets](../admin/ansible.md).
