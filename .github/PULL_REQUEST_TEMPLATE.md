## What and why

<!-- One or two sentences. Link the issue if there is one (Fixes #123). -->

## Checklist

- [ ] `make test` passes (and `make e2e` if the UI or API changed)
- [ ] Updated `docs/SPEC.md` / `web/TESTIDS.md` / README if behavior or test ids changed
- [ ] UI change: screenshot attached

## Release

Merging to `main` publishes a release automatically (patch bump by default). Maintainers: set one label before merging, see [Releases](../CONTRIBUTING.md#releases).

- `release:major` / `release:minor`: bump that part of the version (breaking change / new feature)
- `release:skip`: no release (docs, CI, tests, refactors); only a `sha-<commit>` image is pushed
- Add `feature`, `bug` or `dependencies` so the change lands in the right section of the release notes
