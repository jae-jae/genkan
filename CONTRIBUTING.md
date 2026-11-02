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

By contributing you agree that your contribution is licensed under the [MIT License](LICENSE).
