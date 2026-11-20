# Contributing to simple-cd

Thanks for helping.
The contributor documentation lives on the docs site:

- [Local development](https://mtze.me/simple-cd/contributors/development.html): run the stack with `make dev-up`, `make dev-seed`, `make web` and `make run`.
- [Codebase](https://mtze.me/simple-cd/contributors/codebase.html): repository layout and packages.
- [Testing](https://mtze.me/simple-cd/contributors/testing.html): `make test`, `make lint`, `bash cd-publish/test.sh`, `helm lint`.
- [Conventions](https://mtze.me/simple-cd/contributors/#conventions) and [releasing](https://mtze.me/simple-cd/contributors/releasing.html).

In short:

1. Open an issue first for larger changes, so we can agree on the approach.
2. Keep changes small and dependencies few.
3. Add tests for new code; `make test` and `make lint` must pass.
4. Update the docs in `docs/` and the **Unreleased** section of `CHANGELOG.md` in the same pull request.
5. Write in active voice and present tense, and use plain hyphens, never em or en dashes.

Report security issues privately, see [SECURITY.md](SECURITY.md).
