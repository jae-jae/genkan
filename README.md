<div align="center">

# Genkan 玄関

**The AI-native start page for your homelab.**<br>
Organize your services, widgets and plugins by talking to your AI assistant. Wallpaper, frosted-glass UI, ~11 MB.

[![CI](https://github.com/jae-jae/genkan/actions/workflows/ci.yml/badge.svg)](https://github.com/jae-jae/genkan/actions/workflows/ci.yml)
[![Docker image](https://img.shields.io/badge/ghcr.io-jae--jae%2Fgenkan-2496ED?logo=docker&logoColor=white)](https://github.com/jae-jae/genkan/pkgs/container/genkan)
[![License: MIT](https://img.shields.io/github/license/jae-jae/genkan)](LICENSE)
[![Go version](https://img.shields.io/github/go-mod/go-version/jae-jae/genkan?logo=go&logoColor=white)](go.mod)

English | [简体中文](README.zh-CN.md)

<img src="docs/images/hero.jpg" alt="Genkan on desktop and mobile" width="900">

</div>

## AI-first

Genkan is built to be driven by an AI assistant. Connect Claude, Cursor or any AI agent in **Settings → AI**, then just say what you want:

> *"Group my services by type and pin the five I use most."*
>
> *"Add Jellyfin, Sonarr and Radarr at nas.lan to Media and enable health checks."*
>
> *"Find dead links and move them to an Archive category."*
>
> *"Build a widget plugin for my Proxmox server and add it to the Home page."*

Changes apply immediately and open pages update live.

## Features

- **AI-native**: talk to your AI assistant via the Agent Skill or built-in MCP server to manage links, widgets and plugins live
- **Multiple workspaces**: categories, pinned links, Lucide icons for workspaces and links
- **Beautiful UI**: wallpapers and frosted-glass capsules & widget cards, desktop and mobile
- **Smart sorting**: by usage frequency (default), recent, or custom drag order
- **Quick search**: `/` or `Ctrl/⌘+K`, across all workspaces, web fallback and search plugins
- **Plugins & widgets**: JavaScript / TypeScript plugins run in a sandboxed pure-Go engine ([moejs](https://github.com/MoeForge/moejs)) to add interactive dashboard widgets, wallpaper sources, search providers and scheduled jobs
- **Health checks** and automatic favicons
- **Fast and tiny**: single Go binary, ~11 MB image, ~6 MB RAM
- **Private**: self-hosted, login plus read-only guest mode, no telemetry
- **Easy deploy**: Docker, amd64 / arm64

## Quick start

```bash
docker run -d --name genkan --restart unless-stopped \
  -p 8080:8080 -v genkan-data:/data \
  -e GENKAN_PASSWORD=change-me-please \
  ghcr.io/jae-jae/genkan:latest
```

Open <http://localhost:8080>. Without `GENKAN_PASSWORD` you are asked to set an admin password on first visit.

Or with Docker Compose (see [`docker-compose.yml`](docker-compose.yml) for a hardened example):

```yaml
services:
  genkan:
    image: ghcr.io/jae-jae/genkan:latest
    restart: unless-stopped
    ports: ["8080:8080"]
    volumes: ["genkan-data:/data"]
    environment:
      GENKAN_PASSWORD: "change-me-please"   # optional; otherwise set it on first visit
volumes:
  genkan-data:
```

Images are published for `linux/amd64` and `linux/arm64` (Raspberry Pi, Apple Silicon, NAS).

### Configuration

| Variable | Default | Description |
|---|---|---|
| `GENKAN_ADDR` | `:8080` | Listen address |
| `GENKAN_DATA` | `./data` (`/data` in Docker) | Data directory: `genkan.json` and `uploads/` |
| `GENKAN_PASSWORD` | empty | Sets the admin password on first start (min. 6 chars); never overwrites an existing one |
| `GENKAN_UPDATE_CHECK` | on | `off` disables update checks entirely |
| `GENKAN_SELF_UPDATE` | on | `off` disables in-place updates (the panel shows the `docker compose pull` commands instead) |
| `GENKAN_GITHUB_TOKEN` | empty | Optional GitHub token for the update check and the plugin market (higher rate limit) |

- Everything lives in the data directory: back it up, or use Settings → Data to export JSON.
- Behind a reverse proxy (Nginx, Caddy, Traefik), forward `X-Forwarded-Proto: https` so cookies get the `Secure` flag.
- Subcommands: `genkan version`, `genkan healthcheck` (used by the Docker `HEALTHCHECK`).

## Plugins

Plugins are single JavaScript or TypeScript files that run server-side in a sandboxed pure-Go engine ([moejs](https://github.com/MoeForge/moejs)) with memory and time limits, no filesystem access, and deny-by-default host, link and SSH permissions reviewed by the admin. They return declarative UI blocks, so the browser never executes plugin code.

- **Dashboard widgets:** resizable, drag-sortable masonry cards with stats, progress bars, charts (`line`, `bar`, `spark`), interactive checklists, Markdown notes, and embedded media. **To-do** and **Note** are pre-installed on first start.
- **Wallpaper sources:** Bing Daily, NASA APOD, Picsum, or custom image feeds.
- **Search providers:** inline calculator, GitHub repository search, or custom instant results right in the search box.
- **Scheduled jobs:** background sampling for trend history and quota tracking.

**Built-in & third-party plugins:**
- **Built-in catalog (17 plugins):** To-do, Note, Calculator, Weather, World Clock, Countdown, Pi-hole, Uptime Summary, GitHub Stars, GitHub Search, Hacker News, RSS, Media, Link Importer, Bing Wallpaper, NASA APOD, and Picsum — install from **Settings → Plugins** or directly from the widget picker.
- **Third-party market & [`jae-jae/genkan-plugins`](https://github.com/jae-jae/genkan-plugins):** Genkan automatically discovers GitHub repositories tagged with the `genkan-plugin` topic in **Settings → Plugins → Add → Third-party**, allowing you to inspect permissions and preview source code before enabling (or install from any URL or pasted code). For an example of how third-party plugins are built and organized, see [`jae-jae/genkan-plugins`](https://github.com/jae-jae/genkan-plugins), which includes **SSH Server** (Linux CPU, memory, disk & network over native SSH), **Airport Subscription** (proxy quota & 7D/30D/trend charts), **Codex Usage**, **Stock Trend**, and **Bitcoin Price**.

**Develop plugins with AI:** The [Agent Skill](skills/genkan) includes the full plugin authoring guide ([`skills/genkan/plugin-dev.md`](skills/genkan/plugin-dev.md)) and live test workflow. Click **Settings → Plugins → Develop with AI** to copy the one-line prompt and have your assistant write, test and install custom plugins on your instance. Plugin contract: [`docs/PLUGINS.md`](docs/PLUGINS.md).

<div align="center">
  <img src="docs/images/widgets.jpg" alt="Dashboard widgets from built-in and third-party plugins" width="900"><br>
  <sub>Dashboard widgets from built-in plugins and the third-party example repository <a href="https://github.com/jae-jae/genkan-plugins">genkan-plugins</a> (SSH Server, Airport Subscription, Stock Trend, Codex Usage, Pi-hole, Bitcoin Price)</sub>
</div>

## Screenshots

<table>
  <tr>
    <td><img src="docs/images/plugins.jpg" alt="Plugin manager"><br><sub>Settings → Plugins: manage installed plugins, configs, logs and storage</sub></td>
    <td><img src="docs/images/plugin-market.jpg" alt="Third-party plugin market"><br><sub>Third-party plugin market: discover <code>genkan-plugin</code> repositories (e.g. <code>jae-jae/genkan-plugins</code>)</sub></td>
  </tr>
  <tr>
    <td><img src="docs/images/widget-picker.jpg" alt="Widget picker"><br><sub>Searchable widget picker: add widgets or install built-ins in one click</sub></td>
    <td><img src="docs/images/ai.jpg" alt="AI settings"><br><sub>Settings → AI: masked API token, one-click Skill prompt and MCP</sub></td>
  </tr>
  <tr>
    <td><img src="docs/images/search.jpg" alt="Quick search"><br><sub>Quick search across all workspaces (plus search plugins like Calculator)</sub></td>
    <td><img src="docs/images/content.jpg" alt="Content manager"><br><sub>Content manager for workspaces, categories and links</sub></td>
  </tr>
</table>

## Development

Requires Go 1.27+ and Node 22+.

```bash
make web      # build the frontend (embedded into the binary)
make build    # static binary -> bin/genkan
make run      # run locally with ./data
make test     # go vet + go test -race
make e2e      # Playwright suite against a throw-away Docker container
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for the mock server, screenshots and PR guidelines, and [`docs/SPEC.md`](docs/SPEC.md) for the design.

## License

[MIT](LICENSE) © 2026 jae-jae

Acknowledgements: workspace and link icons are from [Lucide](https://lucide.dev) (ISC). Demo service logos in the screenshots come from [homarr-labs/dashboard-icons](https://github.com/homarr-labs/dashboard-icons) (Apache-2.0) and belong to their respective owners. Details in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

This project proudly supports and recognizes the [LINUX DO community](https://linux.do).
