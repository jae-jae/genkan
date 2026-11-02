<div align="center">

# Genkan 玄関

**轻量、可自托管的 homelab 导航页。**<br>
壁纸 + 毛玻璃胶囊界面，所有服务一键直达。

[![CI](https://github.com/jae-jae/genkan/actions/workflows/ci.yml/badge.svg)](https://github.com/jae-jae/genkan/actions/workflows/ci.yml)
[![Docker image](https://img.shields.io/badge/ghcr.io-jae--jae%2Fgenkan-2496ED?logo=docker&logoColor=white)](https://github.com/jae-jae/genkan/pkgs/container/genkan)
[![License: MIT](https://img.shields.io/github/license/jae-jae/genkan)](LICENSE)
[![Go version](https://img.shields.io/github/go-mod/go-version/jae-jae/genkan?logo=go&logoColor=white)](go.mod)

[English](README.md) | 简体中文

<img src="docs/images/hero.jpg" alt="Genkan 桌面与手机界面" width="900">

</div>

## 为什么选 Genkan

- **毛玻璃界面 + 自选壁纸。** 内置预设，也可上传多张壁纸组成图库；桌面与手机都好用（可添加到主屏，PWA）。
- **工作空间、分类、置顶。** 置顶链接显示在顶部，同时保留在所属分类中；工作空间可选 52 个内置 Lucide 图标。
- **按你的习惯排序。** 默认按使用频率，也可按最近使用，或拖拽自定义顺序。
- **快捷搜索。** 按 `/` 或 `Ctrl/⌘+K`，跨工作空间模糊匹配，找不到时可回退到网页搜索。
- **服务状态一目了然。** 可选的在线检测（绿/红点）和自动抓取站点图标。
- **让 AI 帮你整理。** 内置 [MCP](https://modelcontextprotocol.io) 服务和可下载的 Agent Skill，改动即时生效，界面实时刷新。
- **默认私有。** 管理员登录、可选的访客只读模式，无遥测、运行时零外网依赖。
- **足够小。** 单个静态 Go 二进制，`FROM scratch` 镜像约 11 MB，空闲内存约 6 MB，JS 约 42 KB（gzip）。

## 快速开始

```bash
docker run -d --name genkan --restart unless-stopped \
  -p 8080:8080 -v genkan-data:/data \
  -e GENKAN_PASSWORD=change-me-please \
  ghcr.io/jae-jae/genkan:latest
```

打开 <http://localhost:8080>。不设置 `GENKAN_PASSWORD` 时，首次访问会让你设置管理员密码。

或使用 Docker Compose（[`docker-compose.yml`](docker-compose.yml) 里有带安全加固的完整示例）：

```yaml
services:
  genkan:
    image: ghcr.io/jae-jae/genkan:latest
    restart: unless-stopped
    ports: ["8080:8080"]
    volumes: ["genkan-data:/data"]
    environment:
      GENKAN_PASSWORD: "change-me-please"   # 可选；不设置则首次访问时设置
volumes:
  genkan-data:
```

镜像提供 `linux/amd64` 与 `linux/arm64`（树莓派、Apple Silicon、NAS 均可）。

## 配置

| 环境变量 | 默认值 | 说明 |
|---|---|---|
| `GENKAN_ADDR` | `:8080` | 监听地址 |
| `GENKAN_DATA` | `./data`（Docker 内为 `/data`） | 数据目录：`genkan.json` 与 `uploads/` |
| `GENKAN_PASSWORD` | 空 | 首次启动时初始化管理员密码（至少 6 位）；不会覆盖已有密码 |

- 所有数据都在数据目录里：备份该目录即可，或在 设置 → 数据 中导出 JSON。
- 放在反向代理（Nginx、Caddy、Traefik）后面时，请转发 `X-Forwarded-Proto: https`，会话 Cookie 才会带 `Secure` 标记。
- 子命令：`genkan version`、`genkan healthcheck`（Docker `HEALTHCHECK` 使用）。

## AI 整理

整个实例只有一个 API Token（设置 → AI）。把它交给 AI 助手，然后说「帮我把服务按类型整理成分类」即可。

1. **Skill（推荐）：** 点击「下载 Skill」，然后执行 `unzip genkan-skill.zip -d ~/.claude/skills/`。Skill 中已经填好了你实例的地址和 Token。
2. **MCP：** `claude mcp add --transport http genkan http://<host>:8080/mcp --header "Authorization: Bearer <token>"`（设置 → AI 页面会直接给出带真实值的命令，以及通用 JSON 配置）。

> [!WARNING]
> Token 等同于管理员权限，且包含在 Skill 与 MCP 配置中。请勿分享或提交到代码仓库；一旦泄露请立即重新生成。

REST API 与 MCP 工具说明见 [`docs/SPEC.md`](docs/SPEC.md)（中文）。

## 截图

<table>
  <tr>
    <td><img src="docs/images/search.jpg" alt="快捷搜索"><br><sub>跨工作空间的快捷搜索</sub></td>
    <td><img src="docs/images/context-menu.jpg" alt="右键菜单"><br><sub>右键菜单（手机上为长按）</sub></td>
  </tr>
  <tr>
    <td><img src="docs/images/ai.jpg" alt="AI 设置"><br><sub>设置 → AI：Token（已遮罩）、Skill、MCP</sub></td>
    <td><img src="docs/images/content.jpg" alt="内容管理器"><br><sub>内容管理器：集中管理工作空间、分类与链接</sub></td>
  </tr>
</table>

## 开发

需要 Go 1.27+ 与 Node 22+。

```bash
make web      # 构建前端（会被打进二进制）
make build    # 静态二进制 -> bin/genkan
make run      # 本地运行，数据在 ./data
make test     # go vet + go test -race
make e2e      # Playwright 端到端测试（针对一次性的 Docker 容器）
```

mock 服务、截图生成与 PR 约定见 [CONTRIBUTING.md](CONTRIBUTING.md)，设计文档见 [`docs/SPEC.md`](docs/SPEC.md)。

## 许可证

[MIT](LICENSE) © 2026 jae-jae

致谢：工作空间图标来自 [Lucide](https://lucide.dev)（ISC）。截图中的示例服务图标来自 [homarr-labs/dashboard-icons](https://github.com/homarr-labs/dashboard-icons)（Apache-2.0），商标归各自所有者。详见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。
