---
title: Troubleshooting
parent: Users
nav_order: 3
---

# Troubleshooting
{: .no_toc }

1. TOC
{:toc}

## The artifact does not show up

simple-cd only stores an artifact when all of this holds:

| Check | How to fix |
|---|---|
| The simple-cd GitHub App is installed on the repository. | Ask an administrator. |
| The repository is a project in the target configuration. | Ask an administrator. |
| The workflow run finished with conclusion `success`. | Fix the workflow; cancelled and failed runs are ignored. |
| The run uploaded a workflow artifact named `cd-deploy`. | Add the [`cd-publish`](../reference/deploy-json.md#cd-publish-inputs) step. |
| The run did not come from a fork. | Fork pull requests are rejected on purpose. Push the branch to the repository itself. |
| For pull request runs: the commit belongs to an open pull request. | simple-cd drops PR runs it cannot match to an open PR. |
| The webhook reached simple-cd. | An administrator can check and redeliver it under the GitHub App's **Advanced > Recent deliveries**. |

A run whose `deploy.json` or `.cd.yaml` is invalid still shows up, with the error as a reason.

## "Not deployable" reasons

The cross next to a target lists every reason at once.
Reasons from the first table apply to every target of the project; the second table is per target.

### Reasons from the artifact

| Reason | Fix |
|---|---|
| `.cd.yaml is missing at this commit` | Add [`.cd.yaml`](../reference/cd-yaml.md) to the repository root. |
| `.cd.yaml: ...` | The file does not parse. Unknown keys are errors, so check the spelling. `helm.chart must be an oci:// reference`: charts come from OCI registries only. `invalid path`: paths must be relative and stay inside the repository. |
| `deploy.json: ...` | The manifest is invalid, for example `must be pinned by a sha256 digest`, `must include the registry host` or `duplicate image name`. Use `cd-publish`, it validates the same rules. |
| `image missing in registry: <repo>@<digest>` | The digest does not exist in the registry, or it was deleted. Push it again, or build a new artifact. |
| `chart missing in registry: <repo>@<version>` | The chart version was not pushed under the name in `helm.chart`. Check the chart name and the `helm push` target. |
| `image not found or not accessible: ...` | The registry refused access. Registries such as GHCR answer the same way for missing and private images. For private images, an administrator has to give the server [registry credentials](../admin/installation.md#secret). |

simple-cd checks the registry when the artifact arrives and again right before each deploy.
A registry that is down at arrival does not mark the artifact; the check before the deploy catches it.

### Reasons per target

| Reason | Fix |
|---|---|
| `artifact belongs to <repo>, target to <project>` | You picked a target of another project. |
| `image <repo> is outside the allowed registries` | Images must live under one of the project's registry prefixes, for example `ghcr.io/example/`. Push there, or ask an administrator to extend `registries`. |
| `chart <chart> is outside the allowed registries` | Same rule for the chart. |
| `preview targets only accept pull request artifacts` | Previews run per pull request. Deploy a pull request artifact, or check that the commit was not also published by a `push` run (the newest run decides the source). |
| `.cd.yaml has no helm section` (or `compose`, `ansible`) | Add the section for this target type. |
| `deploy.json has no helm version` | Set the `helm-version` input of `cd-publish`. |
| `values file <path> does not exist at this commit` | `helm.targets.<id>.values` points to a missing file. |
| `placeholders without a value for this target: ...` | A placeholder is unknown or empty here. `{cd.pr}` only has a value on preview targets. `{images.<name>.tag}` is only set when the image reference has a tag before the digest. A typo in an image name also shows up here. |
| `compose file <path> does not exist at this commit` | Fix `compose.path`. |
| `compose file does not use ${<NAME>_IMAGE} for image "<name>"` | Every image in `deploy.json` must appear as `${<NAME>_IMAGE}` (or `${<NAME>_IMAGE:-...}`) in the Compose file. |
| `service "<name>" uses build:, the VM must not build images` | Remove `build:` from the Compose file. |
| `compose file is not valid YAML: ...` | Fix the YAML. |
| `playbook <path> does not exist at this commit` | Fix `ansible.playbook` in `.cd.yaml`. |
| `Not evaluated for this target` | The target is newer than the page. Reload. |

## The deploy button is missing or disabled

- No **Deploy** button: you lack the deploy permission. You need the developer or maintainer role of the project, plus the target's extra role if it sets one. Roles are read at login, so log out and in again after a role change.
- Disabled: the artifact is not deployable to this target.

## The deploy is refused

| Message | Meaning |
|---|---|
| The configuration changed | Review the diff and tick the confirmation box. |
| The target is locked by ... | Wait for the lock to expire, ask the holder to release it, or ask a maintainer. |
| The target is in maintenance | Ask the person in the maintenance note. Nobody deploys until it ends. |
| Another deployment is running for this slot | Wait until it finishes. |
| Registry check failed | The registry could not be reached right now (HTTP 502). Try again. |

## The deployment failed

Open the deployment page and read the log.
Each step starts with `==>`, the last lines before `==> FAILED:` tell what went wrong.

| Log message | Likely cause |
|---|---|
| `vault login (role ...)` | The executor cannot log in to Vault. Administrators: check the Vault Kubernetes auth role. |
| `vault secret "..." has no key "..."` | A secret in Vault lacks a key, see [secret conventions](../admin/vault.md#secret-conventions). |
| `namespace ... exists and is not managed by simple-cd` | A namespace with the preview name exists without simple-cd's labels. simple-cd never adopts or deletes it. |
| `helm upgrade` errors, `context deadline exceeded` | The chart failed to become ready within the timeout. Check the pods in the target namespace. |
| `Host key verification failed` | The VM's host key does not match `known_hosts` in Vault. |
| `timed out after ...`, status `timed_out` | The deployment ran longer than the target's `jobTimeout`. |

Ask a maintainer or administrator when the cause is outside your repository.

## The auto-deploy did not happen

Look at the commit or PR in GitHub: a skipped auto-deploy appears as a deployment with state `error` and the reason, for example `target does not allow auto-deploy from branch "feature-x"` or `target is locked by alice`.
Fix the reason and deploy by hand, or push again.
