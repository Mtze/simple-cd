---
title: Contributors
nav_order: 6
has_children: true
---

# Contributors

This section is for people who change simple-cd itself.

- [Local development](development.md): run the server, the UI and the dependencies on your machine.
- [Codebase](codebase.md): repository layout and a walk through the Go packages.
- [Testing](testing.md): Go tests, UI tests, action tests and chart checks.
- [Releasing](releasing.md): tags, images and the docs site.

The [Architecture](../architecture/index.md) section has the UML diagrams, and the [design record](../design.md) (German) explains why things are the way they are.

## Working with coding agents

[`AGENTS.md`](https://github.com/Mtze/simple-cd/blob/main/AGENTS.md) in the repository root tells coding agents how the repository is laid out, which commands to run, the rules below and the security invariants; `CLAUDE.md` imports it for Claude Code.
`.claude/skills/` holds step-by-step skills for recurring work:

| Skill | For |
|---|---|
| `verify-change` | every check CI runs, with the database, and the definition of done |
| `e2e` | starting, debugging and extending the end-to-end test |
| `security-invariants` | the checklist for changes at trust boundaries |
| `architecture-diagrams` | the hand-placed UML views and the PlantUML diagrams |
| `screenshots` | refreshing the UI screenshots |
| `release` | cutting a release |

Keep them in sync when a command, a convention or a security property changes.

## Conventions

- **Keep it small.** simple-cd has few dependencies on purpose: the Go server uses the standard library plus `pgx`, `go-oidc`, `oauth2` and `yaml`. It talks to Kubernetes, Vault, GitHub and registries over plain HTTP instead of their SDKs. The UI uses React and nothing else at runtime. Add a dependency only when a few lines of code cannot replace it.
- **Tests for new code.** Every change to behaviour comes with a test. Before you change an existing test, make sure the test is wrong and not the code.
- **Comments for humans.** Explain why, not what. A known limit gets a comment of the form `shortcut: <the limit>, <when to upgrade>`.
- **Consistent terms.** Use the words from the [terminology table](../index.md#terminology): target, project, artifact, deployment, slot, lock, maintenance, preview, auto-deploy, executor, platform admin, developer, maintainer.
- **Writing style.** Active voice, present tense, short sentences. No em dashes or en dashes anywhere, use a plain hyphen. Say "UI" and "server".
- **Formatting.** `gofmt` and `go vet` must be clean (`make lint`); the UI must pass `npm run typecheck`.
- **Docs with the change.** Update the docs in `docs/`, the API reference and the changelog in the same pull request.
- **Diagrams.** Structural views (object model, components, deployment, service specs) are hand-placed: change the coordinates in `docs/architecture/uml/diagrams.py`, run it and look at every preview before committing. Sequence and state diagrams are PlantUML in `docs/architecture/diagrams/` (`render.sh`). Commit sources and `.svg` files together. Record decisions about structure, technology or protocols as an [ADR](../adr/).
