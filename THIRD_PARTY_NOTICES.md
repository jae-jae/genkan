# Third-party notices

Genkan itself is MIT licensed (see [LICENSE](LICENSE)). It includes or builds on the following.

## Shipped in the binary / web bundle

| Component | License | Notes |
|---|---|---|
| [Lucide](https://lucide.dev) icons (52 icons, from `lucide-static`) | ISC (parts derived from Feather, MIT) | SVG paths inlined in [`web/src/lucide.ts`](web/src/lucide.ts) (plus two speaker glyphs inline in `web/src/WidgetMedia.tsx`); the copyright and permission notice is kept in that file's header |
| [Preact](https://preactjs.com) and [@preact/signals](https://github.com/preactjs/signals) | MIT | frontend runtime |
| [moejs](https://github.com/MoeForge/moejs) (`github.com/MoeForge/moejs`) | Apache-2.0 | pure-Go JavaScript engine that runs plugins on the server. Its compiled-in Unicode CLDR / UCD / ICU data and ported V8 code carry their own notices (Unicode License v3, ICU License, BSD-3-Clause), listed in the module's [`THIRD_PARTY_NOTICES`](https://github.com/MoeForge/moejs/blob/main/THIRD_PARTY_NOTICES) |
| [cascadia](https://github.com/andybalholm/cascadia) (`github.com/andybalholm/cascadia` v1.3.5) | BSD-2-Clause | CSS selector engine behind `genkan.html.select` in plugins |
| [golang.org/x/crypto](https://pkg.go.dev/golang.org/x/crypto), [golang.org/x/net](https://pkg.go.dev/golang.org/x/net) | BSD-3-Clause | bcrypt, HTML parsing and sanitizing |

## Test fixtures only (not shipped)

- `e2e/fixtures/icons/*.svg` are service logos from [homarr-labs/dashboard-icons](https://github.com/homarr-labs/dashboard-icons) (Apache-2.0). They are used only to seed demo data for the end-to-end tests and the README screenshots; they are not part of the binary or the Docker image. The logos remain trademarks of their respective owners. See [`e2e/fixtures/icons/README.md`](e2e/fixtures/icons/README.md).
