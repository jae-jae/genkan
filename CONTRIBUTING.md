# Contributing

Thanks for helping out. Genkan is deliberately small: a single static Go binary with an embedded Preact UI and as few dependencies as possible. Changes that keep it that way are the easiest to merge. For anything bigger than a bug fix, please open an issue first.

## Setup

Requirements: Go 1.27+, Node 22+, Docker (for e2e and image builds).

```bash
make web      # build the frontend into web/dist (the Go binary embeds it, so do this before go build/test)
make build    # static binary -> bin/genkan
make run      # run locally, data in ./data
make test     # go vet + go test -race
make e2e      # Playwright end-to-end suite against a throw-away Docker container (~3 min)
make docker   # build the image
```

The design is documented in [`docs/SPEC.md`](docs/SPEC.md) (in Chinese). The stable `data-testid` hooks used by the e2e tests are listed in [`web/TESTIDS.md`](web/TESTIDS.md); keep both in sync with your change.

The Agent Skill in [`skills/genkan/`](skills/genkan) is fetched by users straight from GitHub (`main` is the default ref; release builds use their tag), so it must stay compatible (it is also embedded in the binary for the download in Settings → AI) with the REST API on `main`. When you change an endpoint, update `skills/genkan/reference.md` (and `SKILL.md` if the workflow changes) in the same PR, and keep the Skill free of MCP content and secrets.

### Frontend development with the mock server

`web/mock/` is a dev-only mock of the REST API (in-memory, not shipped) so you can iterate on the UI with hot reload and no backend:

```bash
cd web && npm ci
npm run mock    # mock API on :8080 (admin password: secret123)
npm run dev     # Vite on :5173, proxies /api to :8080
```

Alternatively run the real backend with `make run` instead of the mock.

### Screenshots and README images

```bash
cd e2e && npm ci && npx playwright install chromium
npm run screenshots   # raw captures -> .tmp/screenshots (gitignored; English UI, GENKAN_SHOTS_LANG=zh for Chinese)
npm run docs-images   # compose + optimize hero and gallery -> docs/images (JPEG, commit these)
```

Only the optimized files under `docs/images/` are committed; raw captures are several MB each.

## Pull requests

- Keep the change focused; add or update tests (Go tests next to the code, Playwright specs in `e2e/specs/`).
- `make test` must pass, and `make e2e` for UI or API changes.
- No new runtime dependencies without a good reason; the frontend should stay tiny (watch the gzip sizes printed by `npm run build`).
- Don't commit secrets, tokens, personal hostnames or local paths.

## Releases

Every merge to `main` is released automatically; nobody tags by hand. The pipeline lives in [`.github/workflows/ci.yml`](.github/workflows/ci.yml) and the contract in [`docs/UPDATES.md`](docs/UPDATES.md).

Labels on the merged PR decide the version (the next one after the highest `vX.Y.Z` tag; the first is `v1.0.0`):

| Label | Effect |
| --- | --- |
| `release:major` | bump `X` (also lists the PR under "Breaking changes" in the notes) |
| `release:minor` | bump `Y` |
| none | bump `Z` (patch). A direct push without a PR is a patch too |
| `release:skip` | no release and no `latest` image; only `sha-<commit>` is pushed. Use it for docs, CI and test-only changes |
| `feature`, `bug`, `dependencies` | only group the PR in the generated notes (see [`.github/release.yml`](.github/release.yml)) |

On merge the workflow runs the tests, then builds the linux amd64/arm64 tarballs, `checksums.txt` and its ed25519 signature `checksums.txt.sig`, pushes the multi-arch image (`vX.Y.Z`, `X.Y.Z`, `X.Y`, `X`, `latest`, `sha-<commit>`), and finally creates the GitHub Release with generated notes. Running installs pick the release up through Settings, which verifies the signature with the public key embedded in `internal/update`. A maintainer can also start a release from the Actions tab (Run workflow, choose `patch|minor|major`).

### One-time maintainer setup

1. Generate the signing key pair: `go run ./tools/relsign keygen`. It prints the private key (64 bytes) and the public key (32 bytes), both base64. Alternatively `-out PREFIX` writes `PREFIX.key` (0600) and `PREFIX.pub`.
2. Store the private key as the repository secret `GENKAN_RELEASE_KEY` (Settings, Secrets and variables, Actions). Keep a backup somewhere safe and never commit it. Without the secret the release job fails.
3. Put the public key into `internal/update` (the embedded key), and commit it before the first release. The public key cannot be changed for installs that already run, so do not lose the private key.
4. Create the labels: `release:major`, `release:minor`, `release:skip`, `feature`, `bug` (and `dependencies`, which Dependabot already uses).
5. Check locally with `go run ./tools/relsign verify checksums.txt checksums.txt.sig <public key>` on a downloaded release.

By contributing you agree that your contribution is licensed under the [MIT License](LICENSE).
