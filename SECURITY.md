# Security policy

## Reporting a vulnerability

Please **do not open a public issue** for security problems. Use GitHub's private reporting instead:
**Security tab → "Report a vulnerability"** on <https://github.com/jae-jae/genkan>.

Include what you found, how to reproduce it and the version (`genkan version`, or Settings → About). I aim to reply within a few days.

Only the latest release is supported.

## Things worth knowing

- The **API token is full admin access** (REST API and `/mcp`). It is embedded in the setup prompt, the downloadable Skill package and the MCP snippets shown in Settings → AI. Never share, commit or paste them publicly. If one leaks, regenerate the token in Settings → AI (and send the prompt to your AI assistant again); the old one stops working immediately.
- Genkan serves plain HTTP. Put it behind a reverse proxy with TLS if it is reachable beyond your LAN, and forward `X-Forwarded-Proto: https` so session cookies get the `Secure` flag.
- Health checks and favicon fetching make server-side requests to the URLs you configure (including private addresses). Only admins can add links.
