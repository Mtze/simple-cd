# simple-cd

simple-cd is a small continuous delivery server.
It builds nothing and runs no tests: your GitHub Actions workflows do that.
simple-cd sees the artifacts they publish, checks that they are complete, and lets authorized people deploy them to Helm, Docker Compose and Ansible targets with one click.

**Documentation: <https://mtze.me/simple-cd/>**

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/screenshots/project-dark.png">
  <img alt="The project page: target cards with their status, artifacts with readiness per target, and live previews" src="docs/assets/screenshots/project.png">
</picture>

## Features

- Helm releases from OCI charts, a single Docker Compose file on a VM, or Ansible playbooks
- Images pinned by digest, provenance taken from GitHub, forks rejected
- Readiness per artifact and target, with the reasons when something is missing
- Confirmation diff when the Helm values or the Compose file change
- Opt-in locks per target, maintenance mode, one deployment per slot
- Preview namespaces per pull request with a TTL and automatic teardown
- Auto-deploy on request of the workflow and with a rule on the target
- Live logs over Server-Sent Events, deployments mirrored to GitHub
- Keycloak roles per project (developer, maintainer), per target and for platform admins
- Secrets read from Vault by the executor Job, never by the server
- Audit log, retention, leader-elected background worker, stateless replicas

## Who it is for

simple-cd is for teams that build and test in GitHub Actions and need a safe, visible way to put those builds onto shared environments:

- several people share a few test systems and want to deploy *their* build, often from a pull request, and keep it there while they test,
- the targets are mixed: Helm on Kubernetes, a Compose file on a VM, some Ansible,
- a platform team defines targets, credentials and rules once, and developers deploy within them without cluster access or SSH keys,
- Keycloak and Vault are already there.

It is not a GitOps engine, has no canary or blue-green rollouts and no multi-stage promotion with approvals.

| | simple-cd | Argo CD / Flux | Kargo | Octopus Deploy | GitHub / GitLab environments |
|---|---|---|---|---|---|
| Model | push on click or rule | GitOps pull | promotion on GitOps | push, releases | push in the pipeline |
| Targets | Helm, Compose over SSH, Ansible | Kubernetes | Kubernetes | Kubernetes, SSH, Windows, cloud | anything a runner reaches |
| Pick any build in a UI | yes | rollback to history (Argo CD) | yes | yes | re-run or re-deploy |
| Previews per PR | yes | yes (ApplicationSet, Flux Operator) | no | yes | scripted / review apps |
| Reserve a test system | ad hoc locks | scheduled sync windows / suspend | windows (Enterprise) | freezes | queueing only |
| License | MIT | Apache-2.0 | Apache-2.0 | commercial, free tier | part of GitHub / GitLab |

The [detailed comparison](https://mtze.me/simple-cd/why.html) covers these and Spinnaker, Semaphore UI, Kamal, Coolify and Harness, with sources.

## Screenshots

The UI follows the system theme; GitHub shows the screenshots in the theme you use.

| Deploy with a configuration diff | Live executor log |
|---|---|
| <picture><source media="(prefers-color-scheme: dark)" srcset="docs/assets/screenshots/deploy-dialog-dark.png"><img alt="Deploy dialog showing the Helm values diff and the confirmation checkbox" src="docs/assets/screenshots/deploy-dialog.png"></picture> | <picture><source media="(prefers-color-scheme: dark)" srcset="docs/assets/screenshots/deployment-dark.png"><img alt="Deployment page with the log of helm upgrade" src="docs/assets/screenshots/deployment.png"></picture> |
| **A locked target with its history** | **User menu with roles and theme** |
| <picture><source media="(prefers-color-scheme: dark)" srcset="docs/assets/screenshots/target-dark.png"><img alt="Target page showing the lock holder, deployments and lock history" src="docs/assets/screenshots/target.png"></picture> | <picture><source media="(prefers-color-scheme: dark)" srcset="docs/assets/screenshots/user-menu-dark.png"><img alt="User menu with name, roles, theme switch and logout" src="docs/assets/screenshots/user-menu.png"></picture> |

## How it works

![simple-cd components](docs/architecture/uml/components-server.svg)

A workflow builds images and a chart and runs the [`cd-publish`](cd-publish/action.yml) action, which uploads `deploy.json` with the image digests.
The simple-cd GitHub App receives the `workflow_run` webhook, reads `deploy.json` and `.cd.yaml` at the commit and stores the artifact.
A deploy starts a Kubernetes Job that logs in to Vault and runs `helm`, `docker compose` over SSH, or `ansible-playbook`.

```yaml
# .github/workflows/build.yml (excerpt)
- uses: Mtze/simple-cd/cd-publish@main
  with:
    images: |
      server=ghcr.io/example/app-server@${{ steps.server.outputs.digest }}
    helm-version: ${{ steps.chart.outputs.version }}
```

## Quick start (development)

Needs Go, Node.js 26 and Docker.

```sh
make dev-up     # Postgres and Keycloak on 127.0.0.1
make dev-seed   # demo data
make web        # build the UI
make run        # server on http://127.0.0.1:8080
```

Log in as `alice`, `bob`, `mia`, `root` or `guest`, password `dev`.
Details in [Local development](https://mtze.me/simple-cd/contributors/development.html).

## Documentation

| For | Start here |
|---|---|
| Developers who deploy | [Getting started](https://mtze.me/simple-cd/users/getting-started.html) |
| Project maintainers | [Maintainers](https://mtze.me/simple-cd/maintainers/) |
| Platform operators | [Administrators](https://mtze.me/simple-cd/admin/) |
| Contributors | [Contributors](https://mtze.me/simple-cd/contributors/), [CONTRIBUTING.md](CONTRIBUTING.md) |
| File formats, API, settings | [Reference](https://mtze.me/simple-cd/reference/) |
| Design | [Architecture](https://mtze.me/simple-cd/architecture/) |

Security issues: see [SECURITY.md](SECURITY.md).
Changes: see [CHANGELOG.md](CHANGELOG.md).

## License

[MIT](LICENSE)
