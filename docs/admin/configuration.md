---
title: Target configuration
parent: Administrators
nav_order: 8
---

# Target configuration reference
{: .no_toc }

The target configuration is one YAML file that lists projects and targets.
It describes what a target is, never what state it is in: locks, maintenance and deployments live in Postgres.
Keep it in Git, roll it out with the Helm chart (`values.config`, rendered into a ConfigMap) and check it with `simple-cd validate` in CI.

1. TOC
{:toc}

## Rules for the whole file

- Unknown keys are errors, so a typo never changes behaviour silently.
- Validation reports every problem at once.
- Durations are Go durations (`90s`, `30m`, `1h30m`) or whole days with a `d` suffix (`90d`).
- Secret references have the form `vault:<mount>/<path>`, for example `vault:secret/cd/clusters/test`. See [Vault](vault.md#secret-conventions) for the keys each reference needs.

## Example

```yaml
defaults:
  lockDuration: 1h
  jobTimeout: 30m
  previewTTL: 72h
  logRetention: 90d
  auditRetention: 365d
  artifactRetention: 30d
  keepBranchArtifacts: 50
  registries: [ghcr.io/example/]

projects:
  example/app:
    registries: [ghcr.io/example/, registry.example.org/app/]

targets:
  - id: app-test
    project: example/app
    type: helm
    lockable: true
    cluster: vault:secret/cd/clusters/test
    namespace: app
    release: app
    values: vault:secret/cd/values/app-test

  - id: app-previews
    project: example/app
    type: helm
    preview: true
    previewTTL: 48h
    autoDeploy:
      from: [pr]
    cluster: vault:secret/cd/clusters/previews
    values:
      ingress:
        className: nginx

  - id: app-staging
    project: example/app
    type: helm
    autoDeploy:
      from: [branch]
      branches: [main]
    cluster: vault:secret/cd/clusters/staging
    namespace: app

  - id: app-prod
    project: example/app
    type: compose
    role: deploy:app-prod
    host: vm1.example.org
    ssh: vault:secret/cd/ssh/app-prod
    dir: /opt/app
    jobTimeout: 10m

  - id: tools-vm
    project: example/tools
    type: ansible
    ansible:
      source: git@github.com:example/infra.git
      ref: main
      playbook: playbooks/tools.yml
      inventory: inventories/prod
      credentials: vault:secret/cd/ssh/tools
```

## `defaults`

| Field | Default | Meaning |
|---|---|---|
| `lockDuration` | `1h` | how long a deploy locks a lockable target |
| `jobTimeout` | `30m` | maximum run time of a deployment; the executor Job is killed after it |
| `previewTTL` | `72h` | how long a preview lives after its last deploy |
| `logRetention` | `90d` | stored deployment logs are deleted after this time |
| `auditRetention` | `365d` | audit events are deleted after this time |
| `artifactRetention` | `30d` | artifacts of closed pull requests are deleted this long after their last update |
| `keepBranchArtifacts` | `50` | branch artifacts kept per branch, and release artifacts kept per project |
| `registries` | none | allowed registry prefixes for images and charts, used when a project sets none |

A missing or zero value takes the default.
Retention is explained in [Operations](operations.md#retention).

## `projects`

A map from `owner/repo` to project settings.
Listing a project is optional: every project named by a target exists too.
List a project when it needs its own registries, or to show it in the UI before it has targets.

| Field | Default | Meaning |
|---|---|---|
| `registries` | `defaults.registries` | allowed registry prefixes for this project's images and charts |

Registry prefixes are plain string prefixes of `host/path`.
End them with `/` (`ghcr.io/example/`), otherwise `ghcr.io/example` also matches `ghcr.io/example-evil/...`.

Validation: the key must match `owner/repo` (letters, digits, `_`, `.`, `-`).
GitHub names are case-insensitive; simple-cd matches repositories case-insensitively and uses the spelling from this file.

## `targets`

A list of targets.
Fields that apply to every type:

| Field | Required | Default | Meaning |
|---|---|---|---|
| `id` | yes | | stable key of the target. Lowercase letters, digits and `-`, starting and ending with a letter or digit, at most 63 characters, unique. |
| `project` | yes | | `owner/repo` of the project the target belongs to |
| `type` | yes | | `helm`, `compose` or `ansible` |
| `role` | no | none | an extra Keycloak client role needed to deploy, for example `deploy:app-prod` |
| `lockable` | no | `false` | a deploy locks the target for `lockDuration` |
| `lockDuration` | no | `defaults.lockDuration` | lock duration for this target, positive |
| `jobTimeout` | no | `defaults.jobTimeout` | maximum deployment run time for this target, positive |
| `autoDeploy.from` | with `autoDeploy` | | sources allowed to auto-deploy: any of `pr`, `branch`, `release`, not empty |
| `autoDeploy.branches` | no | all branches | only for `from: [branch]`: branch names that may auto-deploy |

Every target needs at least one allowed registry, from its project or from `defaults.registries`.

An auto-deploy needs both the target's rule and a request in `deploy.json`.
`branches` only filters branch artifacts; pull request and release artifacts are not filtered by branch.

### Helm targets

| Field | Required | Default | Meaning |
|---|---|---|---|
| `cluster` | yes | | `vault:` reference to a secret with a `kubeconfig` key |
| `namespace` | yes, except previews | | namespace of the release, a valid Kubernetes name. It must exist. Not allowed on preview targets. |
| `release` | no | repository name in lower case | Helm release name, a valid Kubernetes name. Set it when the repository name contains characters such as `_` or `.`. |
| `values` | no | none | either a `vault:` reference to a secret with a `values` key, or inline YAML values |
| `registryCredentials` | no | none | `vault:` reference to a secret with `username` and `password` for the chart registry |
| `preview` | no | `false` | one namespace per pull request, see below |
| `previewTTL` | no | `defaults.previewTTL` | only with `preview: true`, positive |

Values are merged in this order, later wins (maps merge, everything else replaces):

1. the chart's defaults,
2. the values file from the repository (`.cd.yaml`: `helm.targets.<id>.values`),
3. `values` from the target configuration (inline or from Vault),
4. the rendered `helm.set` entries from `.cd.yaml`, with `helm.targets.<id>.set` added.

The executor writes the result to one file and runs `helm upgrade --install <release> <chart> --version <version> --namespace <namespace> --values <file> --wait --timeout <t>`.
The Helm timeout `t` is `jobTimeout` minus 2 minutes, or three quarters of `jobTimeout` when it is 4 minutes or less, so Helm reports its own timeout before the Job is killed.

#### Preview targets

`preview: true` turns a Helm target into a preview target:

- it accepts only pull request artifacts,
- the namespace is `pr-<repo>-<pr>`, derived per pull request (so `namespace` must not be set),
- it cannot be `lockable`,
- each pull request is its own slot,
- previews are removed after `previewTTL` without a deploy, and when the pull request closes.

### Compose targets

| Field | Required | Meaning |
|---|---|---|
| `host` | yes | host name or IP address of the VM, SSH on port 22 |
| `ssh` | yes | `vault:` reference to a secret with `private_key`, `known_hosts` and optionally `user` |
| `dir` | yes | absolute directory on the VM for `docker-compose.yml`, `.cd.env` and your `.env` |

See [Compose VMs](compose.md).

### Ansible targets

| Field | Required | Meaning |
|---|---|---|
| `ansible.source` | yes | `repo` (the app repository at the artifact's commit) or a Git URL |
| `ansible.ref` | for Git URLs | branch, tag or commit to check out. Not validated, but the executor fails without it. |
| `ansible.playbook` | for Git URLs | playbook path in the repository. With `source: repo` the playbook comes from `.cd.yaml`. |
| `ansible.inventory` | yes | inventory path in the repository |
| `ansible.credentials` | yes | `vault:` reference to a secret with `private_key`, optionally `known_hosts` and `user` |

See [Ansible targets](ansible.md).

## Changing the configuration

- The server checks the mounted file every 10 seconds and reloads it when the content changed. No restart is needed.
- An invalid file never becomes active: the server keeps the previous configuration, logs the error and shows platform admins a banner with the message.
- A removed target is not cleaned up. Running deployments finish, the history stays, pending ones fail, and its previews show up as orphaned.
- A renamed target counts as removed and newly created.

More in [Operations](operations.md#configuration-reload).

## Validate

```sh
simple-cd validate config.yaml
# ok: 2 projects, 5 targets
```

The container image works too: `docker run --rm -v "$PWD":/work ghcr.io/mtze/simple-cd:<version> validate /work/config.yaml`.
