# AGENTS.md

Guidance for AI coding agents (and humans) working on Genkan. Read this before you change anything. [`CONTRIBUTING.md`](CONTRIBUTING.md) has the setup details; this file covers the rules that are easy to miss.

## What Genkan is

A self-hosted homelab start page, shipped as **one static Go binary with an embedded Preact UI**. The Docker image is `FROM scratch` with a read-only root fs, so only `/data` is writable. Keep it small, with as few dependencies as possible.

| Path | What |
|---|---|
| `cmd/genkan/` | entry point: `main.version` comes from ldflags; the self-update launcher runs first |
| `internal/server/` | REST API, auth, CSRF, SSE |
| `internal/store/` | the JSON store (`<data>/genkan.json`), `SchemaVersion` |
| `internal/plugin/` | JS plugin runtime (moejs), sandbox, catalog, market |
| `internal/update/` | update check, verified download, launcher and rollback |
| `internal/mcp/` | MCP server (`/mcp`) |
| `plugins/catalog/` | built-in plugins (embedded) |
| `web/` | Preact UI (`web/src`), dev mock API (`web/mock`), test ids (`web/TESTIDS.md`) |
| `e2e/` | Playwright suite, run against a throw-away Docker container |
| `skills/genkan/` | Agent Skill, fetched by users straight from GitHub: `SKILL.md` (entry), `reference.md` (REST API) and `plugin-dev.md` (plugin development guide for AI) |
| `docs/` | `SPEC.md` (product spec, Chinese), `PLUGINS.md` (plugin contract), `UPDATES.md` (release + self-update contract) |

## Build and test

```bash
make web     # frontend -> web/dist; the Go binary embeds it, so run this before go build/test
make test    # go vet + go test -race
make e2e     # Playwright against a fresh container (several minutes)
cd web && npm run mock & npm run dev   # UI with hot reload against the mock API
```

- `make test` must pass for every PR. `make e2e` must also pass when the UI or API changed.
- Run **only one e2e suite at a time** on a machine: runs share `.tmp/e2e-state.json`.
- For manual testing, use a throw-away data dir (`GENKAN_DATA=./data` or a temp dir) and a local port. Never point tools at someone's real instance or reuse their tokens.
- Stop only the processes you started (by PID). Never `pkill` by name.

## Rules that keep the project healthy

- **Size budgets.** First-screen JS must stay ≤ 60 KB gzip (`npm run build` prints the sizes). Put heavy, rarely used UI in lazy chunks (`web/src/lazy.ts`). The Docker image is ~20 MB, so justify any new Go or npm dependency in the PR.
- **i18n.** Every UI string goes in `web/src/i18n.ts` in **both** `en` and `zh`. Built-in plugins use their `messages` table and `ctx.t` (see `docs/PLUGINS.md` §12).
- **Test ids.** New interactive UI gets a `data-testid`, documented in `web/TESTIDS.md`.
- **API changes** update, in the same PR:
  - `skills/genkan/reference.md` (REST only; no MCP content or secrets in the Skill), and `skills/genkan/plugin-dev.md` when the plugin contract changes (manifest, `ctx`, host API, widget blocks, limits);
  - the MCP tool descriptions in `internal/mcp` if a tool is affected;
  - `web/mock` so the mock stays usable;
  - the relevant doc in `docs/`.
- **Store changes** must be additive. Missing fields must load with sensible defaults; add a test that loads an old `genkan.json`. Bump `SchemaVersion` only when an older binary would misread the data, and mention it in the PR, because rollbacks and downgrades depend on it.
- **Icons.** Lucide icons in `web/src/lucide.ts` are copied verbatim from `lucide-static`. Don't hand-draw them.
- **Security.**
  - Mutating API calls need the CSRF header `X-Requested-With: genkan`, and admin-only data stays admin-only.
  - Plugin secrets are never returned or logged.
  - Plugin fetch is deny-by-default.
  - Never commit tokens, personal hostnames, local paths, `data/`, `demo-data/` or `.tmp/`.
- **Docs.** `README.md` and `README.zh-CN.md` change together. User-visible behavior changes go into `docs/SPEC.md` (Chinese) or the matching contract doc.

## Branches, commits and PRs

- Never commit to `main` directly. Branch from an up-to-date `main` with a name like `feat/<topic>`, `fix/<topic>`, `docs/<topic>` or `ci/<topic>`.
- Commit messages are short, imperative, sentence case, for example `Add update check to Settings → About` or `docs: clarify plugin storage limits`. Keep commits focused.
- Open the PR against `main` using the template. The checklist must be true: tests run, docs and test ids updated, and a screenshot for UI changes.
- **The PR title is the changelog entry.** On merge, the pipeline publishes a release whose notes are the PR titles, and every Genkan instance shows them in its update panel. Write the title in English, user-facing and specific: `Add a media widget for video and audio`, not `update stuff` or `wip`.
- **One merged PR = one release** (see `docs/UPDATES.md` §2). Pick the version bump with labels:
  - no label → patch (`v1.2.3` → `v1.2.4`), for fixes and small improvements;
  - `release:minor` → new features;
  - `release:major` → breaking changes (API, store or plugin contract; the PR description must say what breaks and how to migrate);
  - `release:skip` → no release, for docs-only, CI-only or test-only changes.
  - Add `feature` or `bug` as well, so the notes land in the right section.
- Merges use merge commits (not squash), so the pipeline can find the PR behind each push to `main`.
- **Never create tags or GitHub releases by hand.** Never change the release public key in `internal/update` without the maintainer. Don't create repos, labels or secrets, or push anything, unless the maintainer asked for it.

## Before you open the PR

1. `make web && make test`, plus `make e2e` for UI or API changes.
2. Check the first-screen gzip size in the `npm run build` output.
3. `git status`: no stray data dirs, test output or local files.
4. Title, labels and description match the rules above.
