# Third-party notices

Genkan itself is MIT licensed (see [LICENSE](LICENSE)). It includes or builds on the following.

## Shipped in the binary / web bundle

| Component | License | Notes |
|---|---|---|
| [Lucide](https://lucide.dev) icons (52 icons, from `lucide-static`) | ISC (parts derived from Feather, MIT) | SVG paths inlined in [`web/src/lucide.ts`](web/src/lucide.ts); the copyright and permission notice is kept in that file's header |
| [Preact](https://preactjs.com) and [@preact/signals](https://github.com/preactjs/signals) | MIT | frontend runtime |
| [golang.org/x/crypto](https://pkg.go.dev/golang.org/x/crypto), [golang.org/x/net](https://pkg.go.dev/golang.org/x/net) | BSD-3-Clause | bcrypt, HTML parsing |

## Test fixtures only (not shipped)

- `e2e/fixtures/icons/*.svg` are service logos from [homarr-labs/dashboard-icons](https://github.com/homarr-labs/dashboard-icons) (Apache-2.0). They are used only to seed demo data for the end-to-end tests and the README screenshots; they are not part of the binary or the Docker image. The logos remain trademarks of their respective owners. See [`e2e/fixtures/icons/README.md`](e2e/fixtures/icons/README.md).
