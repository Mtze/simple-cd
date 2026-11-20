# Changelog

All notable changes to this project are documented in this file.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Delivery server in Go with an embedded React UI.
- Target types: Helm (charts from OCI registries), Docker Compose (one file on a VM over SSH) and Ansible playbooks.
- Target configuration in YAML with strict validation, `simple-cd validate` and reload without restart; an invalid file never becomes active and shows a banner to platform admins.
- GitHub App integration: signed `workflow_run` and `pull_request` webhooks, fork rejection, source detection (pull request, branch, release), `.cd.yaml` snapshot per commit, deployments reported to GitHub.
- `cd-publish` GitHub Action that writes and uploads `deploy.json`.
- Readiness checks per artifact and target, including registry existence checks for image digests and chart versions, repeated right before each deploy.
- Confirmation diff of Helm values and Compose files against the last successful deployment.
- Opt-in locks with takeover and history, maintenance mode, one deployment per slot.
- Preview environments: one namespace per pull request, labelled and checked, with a TTL and teardown on PR close.
- Auto-deploy requested in `deploy.json` and allowed by a rule on the target, with a waiting place per slot where the newest artifact wins.
- Keycloak login (OIDC with PKCE), roles from client roles in the ID token: project developer and maintainer, optional target roles, platform admin.
- Executor as a hardened Kubernetes Job that reads its secrets from Vault (Kubernetes auth); a process runner for local development.
- Live deployment logs over Server-Sent Events, stored logs with retention.
- Audit log, retention for artifacts, logs and audit events, leader-elected background worker.
- Helm chart for simple-cd, container images for the server and both executors, CI.
- Documentation site with UML diagrams.
- End-to-end test in a kind cluster with Vault, Keycloak, a local registry and a fake GitHub API (`make e2e-up e2e`), run in CI.
- Screenshots of the UI in light and dark, in the README and the user and admin docs, taken with `make screenshots`.

### Changed

- Light theme: darker page background, stronger borders and card shadows, and a framed readiness list per artifact, so cards and the targets of an artifact read as groups.
- Target cards in one row keep their buttons on one line at the bottom, whatever the card shows above them.
- `SIMPLECD_REGISTRY_PLAIN_HTTP` for registries without TLS in test setups, and `extraEnv` in the Helm chart.
