---
title: Why simple-cd
nav_order: 2
description: Who simple-cd is for, why you would use it, when you should not, and how it compares to other delivery tools.
---

# Why simple-cd
{: .no_toc }

1. TOC
{:toc}

## Who it is for

simple-cd is for teams that already build and test in GitHub Actions and now need a safe, visible way to put those builds onto shared environments.
It fits best when:

- **Several people share a few test systems.** Developers, testers and student teams want to put *their* build, often from a pull request, onto a test system, keep it there while they test, and see who has what deployed where.
- **The targets are mixed.** Some applications run on Kubernetes with Helm, others as one Docker Compose file on a VM, a few are still installed with Ansible.
- **A platform team owns the infrastructure, the project teams own their releases.** The platform team defines targets, credentials and rules once; developers deploy within those rules without cluster access or SSH keys.
- **The organization already runs Keycloak and Vault.** simple-cd reuses them instead of bringing its own user management or secret store.
- **The people who deploy are not all Kubernetes experts.** A button, a diff and a live log are easier to use and to teach than editing GitOps repositories or running `helm` by hand.

Typical settings are university chairs and teaching projects with many short-lived student teams, internal tools of small and mid-sized companies, and agencies that run many small applications for customers.

## Why use it

**One click for any finished build.**
Every successful workflow run that publishes a `deploy.json` becomes an artifact in the UI: pull request builds, branch builds and releases.
Anyone with the role deploys any of them to any allowed target, including going back to an older one.
There is no commit to a deployment repository and no pipeline to re-run.

**Exactly the build that was tested.**
Images are pinned by digest, the chart by version, and simple-cd checks the registry before every deploy.
Repository, commit and pull request come from the GitHub App, never from files the workflow could fake; builds from forks are rejected.

**Shared test systems without stepping on each other.**
A deploy can take an opt-in lock on a target for an hour, the card shows who holds it and for how long, and maintainers can take it over visibly.
Maintenance mode blocks a target completely.
One deployment per target runs at a time.

**Previews per pull request.**
A preview target gives every pull request its own namespace, deployed automatically when the workflow asks for it, removed when the pull request closes or the TTL runs out.
simple-cd only deletes namespaces it created and labelled.

**No surprises in configuration.**
Before a deploy, the dialog shows the diff of the Helm values or the Compose file against what runs now and asks for confirmation.

**Secrets stay where they are.**
Each deployment runs in its own short-lived Kubernetes Job that logs in to Vault with its service account and reads only the secrets of its target.
The server never sees them, and developers never need them.

**Roles from the identity provider you already have.**
Keycloak client roles decide per project who is a developer or maintainer, per target who may deploy to production, and who administers the platform.
The UI hides what you may not do; the server enforces it on every request.
Every action lands in an audit log.

**Small enough to understand.**
One Go binary with an embedded UI, one Postgres database, one executor image per target type, a Helm chart.
The target configuration is a YAML file in Git, validated in CI.
Few dependencies, readable code, an [end-to-end test](contributors/testing.md#end-to-end-test) that runs the whole system in a kind cluster.

## When simple-cd is not the right tool

Be honest with yourself about these, because the alternatives below handle them better:

- **You want GitOps.** simple-cd pushes a deployment when someone asks for it. It does not continuously reconcile a cluster against Git and does not detect or repair drift. For platform components and anything that must always match a repository, use Argo CD or Flux.
- **You need progressive delivery.** There are no canary releases, blue-green switches, automated analysis or automatic rollback. Use Argo Rollouts, Flagger, Spinnaker or a commercial platform.
- **Your code is not on GitHub.** simple-cd only understands GitHub Apps and GitHub Actions.
- **You do not have Keycloak, Vault and Kubernetes.** simple-cd needs all three. Other OIDC providers or secret stores would need code changes.
- **You need multi-stage promotion pipelines with gates.** simple-cd has no "promote from test to staging to production" workflow, no approvals and no scheduled deploy windows. A person with the right role decides.
- **You need a mature product with support.** simple-cd is young, has a small community and no commercial support.

## Comparison with other tools

Many tools solve parts of the same problem.
They differ mostly in **how a deployment starts** (a person or pipeline pushes it, or a controller pulls the state from Git), in **which targets** they reach, and in **how much you have to run and learn**.
The facts below were checked against the official documentation in November 2026; features and plans change, so follow the links before you decide.

### At a glance

| Tool | Model | Targets | Pick any build in a UI and deploy it | Previews per pull request | Block deploys | Vault | What you run |
|---|---|---|---|---|---|---|---|
| **simple-cd** | push, on click or auto-deploy rule | Helm on Kubernetes, Docker Compose over SSH, Ansible | yes, every published build | yes, namespace per PR with TTL | ad hoc locks with takeover, maintenance mode | native, per Job | server, Postgres, executor Jobs |
| [Argo CD](https://argo-cd.readthedocs.io/) | GitOps pull | Kubernetes | rollback to a revision from the app history | yes, ApplicationSet pull request generator | scheduled sync windows | through operators in the cluster | controllers in the cluster, no database |
| [Flux](https://fluxcd.io/) | GitOps pull | Kubernetes | no | with the Flux Operator (ResourceSets) | suspend reconciliation | SOPS with Vault keys, or operators | controllers in the cluster, no database |
| [Kargo](https://docs.kargo.io/) | promotion on top of GitOps | Kubernetes | yes, promote Freight to a Stage | no built-in feature | promotion windows (Enterprise only) | no native integration | API server, controllers, cert-manager, usually Argo CD |
| [Spinnaker](https://spinnaker.io/) | push pipelines | Kubernetes and cloud VMs (AWS, GCP, Azure and others) | run a pipeline with parameters | no built-in feature | not covered here | not covered here | about ten services, 18 GB RAM recommended |
| [Octopus Deploy](https://octopus.com/) | push, releases and lifecycles | Kubernetes, SSH, Windows and Linux agents, Azure, AWS ECS | yes, pick a release and deploy | yes, ephemeral environments | deployment freezes | community step templates | server, SQL Server, file storage; commercial |
| [GitHub Actions environments](https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments) | push, in the workflow | anything a runner reaches | no, re-run or dispatch a workflow | scripted by you | concurrency groups queue jobs | OIDC with `vault-action` | GitHub, runners |
| [GitLab environments](https://docs.gitlab.com/ci/environments/) | push, in the pipeline | anything a runner reaches | re-deploy a past deployment | yes, review apps | resource groups queue jobs, deploy freezes | `secrets:vault` (Premium) | GitLab, runners |
| [Semaphore UI](https://semaphoreui.com/) | push, task runner | anything Ansible or Terraform reaches | run a task template | no | no | Pro plan | one binary and a database |
| [Kamal](https://kamal-deploy.org/) | push, CLI | Docker containers on VMs over SSH | CLI only (`kamal rollback VERSION`) | no | `kamal lock` | no adapter | nothing beyond the CLI |
| [Coolify](https://coolify.io/) | push, self-hosted PaaS | Docker and Compose on your servers over SSH | deploys from Git, not from your CI builds | yes | not covered here | not covered here | Coolify instance |
| [Harness CD](https://www.harness.io/) | push pipelines, Argo CD based GitOps | Kubernetes, Helm, cloud services, SSH, WinRM | yes, in pipelines | not covered here | freeze windows | native connector | SaaS plus a Delegate; commercial |

"Not covered here" means the documentation we checked did not show the feature clearly, not that it is missing.

### Argo CD

Argo CD keeps Kubernetes clusters in sync with Git.
It is a CNCF graduated project, Apache-2.0 licensed and very widely used.
It continuously compares the cluster with the repository, shows drift and repairs it, and its ApplicationSet pull request generator creates an application per pull request.
Access rules map OIDC groups to roles per project and application; sync windows block syncs on a schedule.

**Where Argo CD is stronger:** reconciliation and drift detection, multi-cluster management, the size of its ecosystem, and its maturity.

**Where simple-cd differs:** In Argo CD the desired state lives in Git, so deploying an older or a pull request build means changing a Git repository or overriding parameters; there is no list of builds to pick from.
There is no ad hoc lock for "I am testing on this system for the next hour", only scheduled windows.
Argo CD only targets Kubernetes, and it [advises](https://argo-cd.readthedocs.io/en/stable/operator-manual/secret-management/) to fetch secrets with operators in the target cluster rather than through Argo CD.

**Together:** Run platform components and long-lived production services with Argo CD, and let teams put builds onto shared test and preview environments with simple-cd.

### Flux

Flux is the other CNCF graduated GitOps engine, a set of Kubernetes controllers maintained by the community since Weaveworks closed in 2024.
Flux itself has no web UI; the [Flux Operator](https://fluxoperator.dev/web-ui/) by ControlPlane (AGPL-3.0) adds one with OIDC login, and its ResourceSets create preview environments for pull requests.
Reconciliation of a resource can be suspended.

**Where Flux is stronger:** a small, composable, controller-only design, image automation that writes new tags back to Git, and SOPS decryption built in.

**Where simple-cd differs:** the same as for Argo CD: Git is the interface, not a list of builds, and the scope is Kubernetes.

### Kargo

Kargo is the closest relative in spirit.
It promotes *Freight*, a versioned set of artifacts, through *Stages*, and its UI lets you pick a piece of Freight and promote it, with roles that limit who may promote to which Stage.
It is Apache-2.0 licensed by Akuity, runs in Kubernetes, and works best together with Argo CD.

**Where Kargo is stronger:** structured promotion from one stage to the next, verification of a stage before the next promotion, and Git-based history of every promotion.

**Where simple-cd differs:** simple-cd has no stage pipeline; any allowed build goes to any allowed target, which suits test systems that many people share.
simple-cd also covers VMs with Compose and Ansible, has locks and per pull request previews built in, and reads secrets from Vault itself.
Kargo's promotion windows are an Enterprise feature.

### Spinnaker

Spinnaker is a mature multi-cloud delivery platform under the CD Foundation, with pipelines, manual judgment stages and deep integrations into AWS, Google Cloud and Azure.
Harness acquired the assets of Armory, the main commercial Spinnaker company, in 2024; the project continues with several companies in its technical oversight committee.

**Where Spinnaker is stronger:** large multi-cloud estates, complex pipelines and advanced deployment strategies.

**Where simple-cd differs:** Spinnaker runs about ten services and [recommends](https://spinnaker.io/docs/setup/install/) at least 18 GB of memory; simple-cd is one server and Postgres.
Spinnaker does not target Docker Compose on VMs.

### Octopus Deploy

Octopus Deploy is the closest commercial match: releases are snapshots, and you pick a release and deploy it to an environment.
It reaches Kubernetes, SSH and Windows targets, has ephemeral environments for pull requests, deployment freezes, and teams with roles per project and environment that can come from Keycloak groups.
A [free plan](https://octopus.com/pricing) covers 10 projects, 10 machines and 10 users.

**Where Octopus is stronger:** breadth of targets including Windows, runbooks, multi-tenancy, approvals, and commercial support.

**Where simple-cd differs:** simple-cd is open source (MIT) without plan limits, has ad hoc locks for shared test systems, reads secrets from Vault natively, and needs Postgres instead of SQL Server.

### GitHub Actions environments

Many teams deploy from the workflow that built the code: a deployment job targets an *environment* with its own secrets and protection rules.
Concurrency groups make sure only one deployment runs at a time, and Vault can be reached with OIDC tokens that name the environment.

**Where GitHub Actions is stronger:** no extra system at all, and anything a runner can reach is a target.

**Where simple-cd differs:**

- There is no place to pick an older build and deploy it; you re-run or dispatch a workflow.
- Required reviewers and wait timers for private repositories need a [GitHub Enterprise plan](https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments).
- Concurrency queues jobs; it does not let a tester reserve a system.
- Deployment credentials live in GitHub secrets or come through the runner, while simple-cd keeps them in Vault and executes inside your cluster.
- Previews you script yourself.

simple-cd keeps GitHub Actions for what it does well, building and testing, and takes over from there.

### GitLab environments

GitLab CI/CD has the most complete built-in model among the CI systems: environments with deployment history and re-deploy, review apps per merge request that stop automatically, resource groups that serialize deployments, and deploy freezes.
Protected environments, deployment approvals and the native Vault keyword are Premium features.

**Where GitLab is stronger:** everything in one product if your code already lives in GitLab.

**Where simple-cd differs:** simple-cd is for code on GitHub.
It adds ad hoc locks and per-target roles from Keycloak without a paid tier.

### Semaphore UI

Semaphore UI is a web interface for running Ansible, Terraform and scripts, MIT licensed, a single binary with a database.
OIDC login and Vault secret storage are part of the Pro plan; group-to-role mapping is Enterprise.

**Where Semaphore is stronger:** running arbitrary playbooks and infrastructure code, with schedules and inventories.

**Where simple-cd differs:** Semaphore runs tasks; it does not know builds, image digests, pull requests or which version runs where.
simple-cd treats Ansible as one target type next to Helm and Compose.

### Kamal and Coolify

Both put containers onto your own servers over SSH.
[Kamal](https://kamal-deploy.org/) (MIT) is a command line tool with zero-downtime switching, rollback to a version and a deploy lock; there is no web UI and no role model.
[Coolify](https://coolify.io/) (Apache-2.0) is a self-hosted platform with a web UI that builds from Git, Dockerfiles or Compose files and creates previews for pull requests.

**Where they are stronger:** the simplest way to run a few applications on a few servers, including proxies and certificates.

**Where simple-cd differs:** simple-cd does not build anything and does not manage proxies; it deploys what your CI already built and tested, by digest, with roles, locks, Vault and an audit log, and the same model works for Kubernetes.

### Harness CD

Harness is a commercial delivery platform with pipelines, approvals including Jira and ServiceNow, freeze windows, fine-grained roles, a native Vault connector and a GitOps mode based on Argo CD.
It reaches Kubernetes, many cloud services, SSH and WinRM targets.

**Where Harness is stronger:** enterprise governance, approvals, deployment verification and support.

**Where simple-cd differs:** simple-cd is self-hosted open source with a small, readable code base and no SaaS component.

### Discontinued tools

[HashiCorp Waypoint](https://github.com/hashicorp/waypoint) (open source edition, archived in January 2024) and [Keptn](https://www.cncf.io/archived-projects/) (archived by the CNCF) aimed at similar problems and are no longer maintained.

## Summary

Choose **simple-cd** when your builds come from GitHub Actions, several people share a few test systems, your targets mix Kubernetes and VMs, and you already run Keycloak and Vault.
Choose **Argo CD or Flux** when Git should be the single source of truth for a cluster, and **Kargo** when you want structured promotion on top of it.
Choose **Octopus Deploy or Harness** when you need commercial support, approvals and the widest range of targets.
Stay with **GitHub Actions or GitLab environments** when a pipeline per deployment is enough and nobody needs to pick builds or reserve systems.
