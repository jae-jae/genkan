# Releases and self-update

This is the contract between the release pipeline, the server and the UI. Keep it in sync with the code.

## 1. Versions

- Releases are semver tags `vMAJOR.MINOR.PATCH`. The first release is `v1.0.0`.
- The binary's version comes from `-ldflags "-X main.version=..."`. A version that is not a plain `vX.Y.Z` (`dev`, `sha-1a2b3c4`, `v1.2.3-4-gabc-dirty`, …) is a **development build**. It never checks for or applies updates.
- Pre-releases and drafts are ignored.

## 2. Release pipeline (`.github/workflows/ci.yml`)

- On a pull request, only the `test` job runs: vet, race tests and the frontend build.
- On a push to `main`, `test` runs first and then the `release` job. The job runs one at a time (a concurrency group with no cancelling), so two merges never compute the same version.
  1. Work out the bump from **every** merge since the last release, not only the pushed commit, because GitHub keeps just one queued run per concurrency group and may drop a run in between. Take the first-parent commits of `<highest v* tag>..<pushed sha>` on `main`. For each, find its merged PRs (`GET /repos/{repo}/commits/{sha}/pulls`) and read their labels:
     - `release:major` / `release:minor` bump that part; a PR without either bumps the patch. The highest bump over the whole range wins (major > minor > patch).
     - A commit without a PR (a direct push) counts as a patch.
     - A PR labelled `release:skip` contributes nothing. The release is skipped only when every PR in the range is `release:skip`: no tag, no GitHub Release, and only the `sha-<short>` image is pushed, never `latest`. If any other commit or PR is in the range, the release happens and the skipped PRs are left out of its notes.
     - When there is no previous tag the version is `v1.0.0` whatever the labels say.
  2. The next version is that bump of the highest existing `v*` tag (`vX.Y.Z` only), or `v1.0.0` when there is none. A commit that is already contained in a release tag is never released again (re-runs).
  3. Build `genkan_<X.Y.Z>_linux_amd64.tar.gz` and `genkan_<X.Y.Z>_linux_arm64.tar.gz`. Each holds a single file named `genkan` (mode 0755), built with `CGO_ENABLED=0 -trimpath -ldflags "-s -w -X main.version=vX.Y.Z"`.
  4. Write `checksums.txt` in `sha256sum` format (`<hex>  <file name>`, one line per tarball) and `checksums.txt.sig`. The signature is the base64 (std, then `\n`) of the ed25519 signature over the exact bytes of `checksums.txt`. It is made with `go run ./tools/relsign sign`, using the secret `GENKAN_RELEASE_KEY`, which holds the base64 (std) of the 64-byte ed25519 private key. If the secret is missing, the job fails.
  5. Build and push the multi-arch image (`linux/amd64,linux/arm64`) with `VERSION=vX.Y.Z`. Its tags are `vX.Y.Z`, `X.Y.Z`, `X.Y`, `X`, `latest` and `sha-<short>`.
  6. **Last step:** create the tag and the GitHub Release (`gh release create vX.Y.Z --target <sha> --generate-notes`) with the three assets. Notes are grouped by `.github/release.yml`. Users are only told about a release once the image and the assets exist.
- `workflow_dispatch` takes a `bump` input (`patch|minor|major`) for a manual release from `main`.

## 3. Update check (server)

- The check runs only for admins and only from the server. The default API is `https://api.github.com/repos/jae-jae/genkan`. The env var `GENKAN_UPDATE_API` overrides it (for tests and mirrors).
- The server calls `GET {api}/releases?per_page=30` with `If-None-Match`, and `Authorization` when `GENKAN_GITHUB_TOKEN` is set. It keeps the non-draft, non-prerelease releases whose tag is semver and newer than the running version, sorted newest first. The result is persisted in `<data>/.update/check.json` so a restart does not have to re-fetch.
- The first check runs about 1 minute after start (with jitter), then every 6 hours.
- The check runs when the setting `update_check` is on (default **true**) and is skipped when:
  - the env var `GENKAN_UPDATE_CHECK=off` is set, or
  - the binary is a development build.
- A manual check (`POST /api/update/check`) works even when `update_check` is off, but not for development builds or when `GENKAN_UPDATE_CHECK=off` (both answer `400`). A failure to reach GitHub is not an HTTP error: the check answers `200` and the status carries `error`; the last good release list is kept.
- A restart does not re-fetch while `check.json` is younger than 6 hours: the first check then waits until it is 6 hours old.

## 4. Applying an update (server)

- Only an admin can apply, through `POST /api/update/apply`, and only a version that is in the checked list. Steps:
  1. Download `checksums.txt`, `checksums.txt.sig` and the tarball for `runtime.GOARCH` (`linux` only). Download URLs come from the release's `assets[].browser_download_url`. Redirects are allowed only to these hosts:
     - `github.com`
     - `objects.githubusercontent.com`
     - `release-assets.githubusercontent.com`
     - the host of `GENKAN_UPDATE_API` when that is overridden
  2. Verify the signature with the embedded public key (`internal/update`), then the tarball's SHA-256. Size limit: 100 MiB.
  3. Extract `genkan` to `<data>/.update/<version>/genkan` (0755), then run `<it> version` and require the output `genkan <version>`.
  4. Back up the store files to `<data>/.update/backup-<version>/`: `genkan.json` and the `plugins/` directory (plugin sources and `*.kv.json` storage). Uploaded images are not part of it. The backup is taken again after the graceful shutdown of step 6 (the store and plugin storage are flushed by then), so the rollback point is the state at the moment of the restart. If that second backup fails, the first one stays.
  5. Write `<data>/.update/state.json`:
     ```json
     {"version": "vX.Y.Z", "status": "pending", "attempts": 0, "from": "<running version>", "backup": "backup-vX.Y.Z", "at": <ms>}
     ```
  6. Shut down gracefully: stop HTTP, the plugin manager and the scheduler, and flush the store. Then `syscall.Exec` the current executable (`os.Executable()`) with the same args and the same env **minus `GENKAN_LAUNCHED`** (a process that was itself started by the launcher carries it), so the launcher (§5) takes over. `main` waits for the apply job's restart request in the same `select` as the signal handler, runs the normal shutdown path, and only then calls the exec; there is no `os.Exit` involved.
- The tarball must hold exactly one entry, a regular file named `genkan` (or `./genkan`); directories, links, extra files and a second `genkan` are errors. The unpacked size limit is 200 MiB. Download URLs must be on an allowed host as well, not only the redirects. A failed job removes `<data>/.update/<version>/`, its backup and the partial download, and can be retried.
- The env var `GENKAN_UPDATE_PUBKEY` (base64 public key) replaces the embedded key. It exists for tests: whoever can set the env can replace the binary anyway.
- `GENKAN_SELF_UPDATE=off`, a non-linux OS (or an arch other than amd64/arm64), or a data dir on a `noexec` mount (from `/proc/self/mountinfo`) turns applying off. The UI then shows the pull commands instead. Development builds are `dev` first; the other reasons follow in the order `disabled`, `platform`, `noexec`.
- If `GENKAN_UPDATE_PUBKEY` is set but is not a base64 ed25519 key, applying fails with an error (there is no silent fallback to the embedded key).

## 5. Launcher (start of `main`, before anything else)

Subcommands (`version`, `healthcheck`, …), development builds and processes started with `GENKAN_LAUNCHED=1` skip the launcher. Otherwise it reads `<data>/.update/state.json`:

- The state's version is not newer than the running binary (for example, a newer image was pulled): remove `<data>/.update/` except `check.json`, and run as normal.
- The binary is missing or not executable: drop the state and run as normal.
- `status: "pending"` and `attempts >= 1`: the last start of the new version never confirmed. **Roll back**:
  - restore the backup;
  - remove the failed version's dir (the dir of `from` is kept: other version dirs are only removed when the new version confirms, see below);
  - write `<data>/.update/rolled_back.json` with `{"version", "at"}`;
  - if `state.from` is a version newer than the running (image) binary and `<data>/.update/<from>/genkan` still exists (an earlier update that was confirmed before this one), rewrite `state.json` as `{"version": <from>, "status": "confirmed", "attempts": 0, "from": <running version>, ...}` and `syscall.Exec` that binary with `GENKAN_LAUNCHED=1`, so a failed v1.3 ends on v1.2 with its data and not on the older image binary; if that exec fails, drop the state;
  - otherwise (no earlier update, or its directory is gone) the state is removed and the image binary runs as normal.
- Otherwise: if pending, increment `attempts` and save. Then `syscall.Exec` the new binary with `GENKAN_LAUNCHED=1` added to the env. If the exec fails, a pending update is rolled back as above and a confirmed one is dropped; the process runs as normal.

The version in `state.json` is only used as a directory name after it parsed as `vX.Y.Z`; the backup directory is always `backup-<version>`.

The new version confirms itself once it has been up for 30 s and its own `/healthz` answers (it probes `127.0.0.1` on `GENKAN_ADDR`, retrying every 5 s until it does): it sets `status: "confirmed"`, then removes the backup and any other version dirs. A crash before that makes the container restart, and the launcher rolls back.

## 6. HTTP API (admin only, JSON, the usual CSRF rules)

`GET /api/update` returns `UpdateStatus`:

```json
{
  "current": "v1.0.0",
  "dev": false,
  "repo": "https://github.com/jae-jae/genkan",
  "check_enabled": true,
  "env_disabled": false,
  "checking": false,
  "checked_at": 1762300000000,
  "error": "",
  "latest": "v1.2.0",
  "available": true,
  "dismissed": "",
  "releases": [
    {"version": "v1.2.0", "name": "v1.2.0", "notes": "markdown", "published_at": 1762300000000, "url": "https://github.com/jae-jae/genkan/releases/tag/v1.2.0"}
  ],
  "apply": {"supported": true, "reason": ""},
  "job": {"phase": "idle", "version": "", "progress": 0, "error": ""},
  "rolled_back": ""
}
```

Field notes:

- `latest` is `""` and `available` is `false` when nothing is newer. `releases` is always an array.
- `apply.reason` is one of `""`, `"disabled"` (env), `"platform"`, `"noexec"`, `"dev"`.
- `job.phase` is one of `idle`, `downloading`, `verifying`, `installing`, `restarting`, `failed`. `progress` runs from 0 to 1 while downloading.
- `rolled_back` is the version that failed and was rolled back, or `""`. Showing it once is enough: `POST /api/update/dismiss` with `{"rolled_back": true}` clears it.
- `dismissed` is the version the admin chose to ignore. The topbar button is hidden while `latest == dismissed`.

Endpoints:

| Endpoint | Body | Result |
|---|---|---|
| `POST /api/update/check` | none | Checks now and returns `UpdateStatus`. `400` for development builds and `GENKAN_UPDATE_CHECK=off`. |
| `POST /api/update/apply` | `{"version": "vX.Y.Z"}` | `202` with `UpdateStatus`, then runs in the background. A wrong or unknown version → `400`; already running → `409`; unsupported → `400` with the reason. |
| `POST /api/update/dismiss` | `{"version": "vX.Y.Z"}` or `{"rolled_back": true}` | Stores `update_dismissed` in settings, or clears the rolled-back notice. Returns `UpdateStatus`. The version must look like `vX.Y.Z` (any, it need not be in the list); an empty body or neither field → `400`. |

Settings:

- `update_check` (bool, default true; a store without the field loads as true) is read and changed through the existing settings API.
- `update_dismissed` (string) is changed only through `/api/update/dismiss`; `PATCH /api/settings` ignores it.

MCP gets a read-only `update_status` tool. Applying an update is not exposed through MCP.

## 7. UI

- **Settings → About:**
  - version and revision;
  - a GitHub link (`repo`);
  - the "check for updates automatically" switch (`update_check`);
  - a "check now" button with the last-checked time and any error;
  - the latest version, with a button that opens the changelog panel;
  - the rolled-back notice;
  - for development builds, a note that updates are disabled.
- **Topbar (admin only):** when `available && latest != dismissed`, an update button sits left of the plugins button and opens the changelog panel. The UI fetches `GET /api/update` on load and then every hour.
- **Changelog panel:**
  - each newer release with its version, date and notes (a small, safe markdown subset), plus a link to GitHub;
  - "update now", which asks for confirmation first: data is backed up and a failed start rolls back automatically;
  - "skip this version".
  - When `apply.supported` is false, the panel shows the reason and copyable `docker compose pull && docker compose up -d` commands instead.
- **While updating:** the UI shows the phase and progress. Once it is restarting, it polls `GET /api/update` (and tolerates network errors) until `current` equals the target, then reloads the page. It gives up after 3 minutes and shows the error and a link to the logs hint.
