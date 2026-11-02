<div align="center">

# Genkan 玄関

**The AI-native start page for your homelab.**<br>
Tell your AI assistant to organize your services and Genkan applies it instantly. Wallpaper, frosted-glass UI, ~11 MB.

[![CI](https://github.com/jae-jae/genkan/actions/workflows/ci.yml/badge.svg)](https://github.com/jae-jae/genkan/actions/workflows/ci.yml)
[![Docker image](https://img.shields.io/badge/ghcr.io-jae--jae%2Fgenkan-2496ED?logo=docker&logoColor=white)](https://github.com/jae-jae/genkan/pkgs/container/genkan)
[![License: MIT](https://img.shields.io/github/license/jae-jae/genkan)](LICENSE)
[![Go version](https://img.shields.io/github/go-mod/go-version/jae-jae/genkan?logo=go&logoColor=white)](go.mod)

English | [简体中文](README.zh-CN.md)

<img src="docs/images/hero.jpg" alt="Genkan on desktop and mobile" width="900">

</div>

## AI-first

Genkan is built to be driven by an AI assistant. Point Claude, Cursor or any MCP client at it and just say what you want:

> *"Group my services by type and pin the five I use most."*
>
> *"Add Jellyfin, Sonarr and Radarr at nas.lan to Media and enable health checks."*
>
> *"Find dead links and move them to an Archive category."*

Changes land immediately and the open page updates live. Under the hood: a built-in [MCP](https://modelcontextprotocol.io) server with 13 tools, plus a downloadable Agent Skill with your URL and token already filled in. Live updates use SSE, and everything stays local on your own server.

**Set it up in two steps** (Settings → AI shows both with your real values; one API token covers everything):

1. **Skill (recommended):** click *Download Skill*, then `unzip genkan-skill.zip -d ~/.claude/skills/`.
2. **MCP:** `claude mcp add --transport http genkan http://<host>:8080/mcp --header "Authorization: Bearer <token>"` (a generic JSON snippet is there too).

> [!WARNING]
> The token is full admin access and is embedded in the Skill and MCP snippets. Never share or commit them; regenerate the token if it leaks.

The REST API and MCP tools are described in [`docs/SPEC.md`](docs/SPEC.md).

## Features

- **AI-native**: organize via the built-in MCP server or a one-click Skill; changes apply live
- **Multiple workspaces**: categories, pinned links, Lucide icons for workspaces and links
- **Beautiful UI**: wallpapers and frosted-glass capsules, desktop and mobile
- **Smart sorting**: by usage frequency (default), recent, or custom drag order
- **Quick search**: `/` or `Ctrl/⌘+K`, across all workspaces, web fallback
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

## Configuration

| Variable | Default | Description |
|---|---|---|
| `GENKAN_ADDR` | `:8080` | Listen address |
| `GENKAN_DATA` | `./data` (`/data` in Docker) | Data directory: `genkan.json` and `uploads/` |
| `GENKAN_PASSWORD` | empty | Sets the admin password on first start (min. 6 chars); never overwrites an existing one |

- Everything lives in the data directory: back it up, or use Settings → Data to export JSON.
- Behind a reverse proxy (Nginx, Caddy, Traefik), forward `X-Forwarded-Proto: https` so cookies get the `Secure` flag.
- Subcommands: `genkan version`, `genkan healthcheck` (used by the Docker `HEALTHCHECK`).

## Screenshots

<table>
  <tr>
    <td><img src="docs/images/ai.jpg" alt="AI settings"><br><sub>Settings → AI: token (masked), Skill, MCP</sub></td>
    <td><img src="docs/images/search.jpg" alt="Quick search"><br><sub>Quick search across all workspaces</sub></td>
  </tr>
  <tr>
    <td><img src="docs/images/content.jpg" alt="Content manager"><br><sub>Content manager for workspaces, categories and links</sub></td>
    <td><img src="docs/images/context-menu.jpg" alt="Context menu"><br><sub>Right-click (long-press on mobile) menus</sub></td>
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
