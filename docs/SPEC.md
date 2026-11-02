# Genkan — 规格说明 (SPEC)

Genkan（玄関）是一款可自托管的极简导航页，面向 homelab 玩家，通过 Docker 部署在 NAS 等设备上。
以壁纸 + 毛玻璃胶囊界面集中呈现服务与常用链接；支持多工作空间（Lucide 图标）、分类、置顶、右键菜单、壁纸图库、快捷搜索；
默认按使用频率排序，可切换最近使用 / 自定义顺序；适配桌面与手机；AI 可通过 Skill（REST API + curl）或 MCP 两种互相独立的方式直接整理导航（直接生效）。

**设计第一原则：轻量、高性能、低资源占用、完全离线可用。**

## 1. 目标指标

| 项 | 目标 |
|---|---|
| Docker 镜像 | ≤ 20 MB（`FROM scratch`，静态二进制） |
| 空闲内存 RSS | ≤ 25 MB |
| 首屏 JS (gzip) | ≤ 60 KB；CSS ≤ 15 KB |
| 首屏请求 | `index.html` + 1 JS + 1 CSS + `GET /api/bootstrap`（一次拿全数据） |
| 架构 | linux/amd64 + linux/arm64 |
| 外部依赖 | 运行时零外网依赖（图标抓取、健康检查访问的是用户自己的服务） |

## 2. 技术栈与目录

- 后端：Go 1.2x，`CGO_ENABLED=0`，尽量只用标准库；允许 `golang.org/x/crypto`（bcrypt）、`golang.org/x/net/html`（解析 favicon）。不引入 web 框架，用 `net/http`（Go 1.22+ 路由模式）。
- 存储：**单个 JSON 文件 `data/genkan.json`**，全量常驻内存，`sync.RWMutex` 保护；写操作后原子落盘（写临时文件 + fsync + rename）。点击计数等高频写入做 2 秒防抖合并落盘。进程退出（SIGTERM）时 flush。
- 前端：Preact + TypeScript + Vite，原生 CSS（无 UI 库、无 CSS 框架），拖拽排序自己用 Pointer Events 实现。
- 前端产物 `web/dist` 通过 `//go:embed` 打进二进制。

```
.
├── docs/SPEC.md           本文件
├── docs/images/           README 用的优化后截图（由 e2e/tools/docs-images.mjs 生成）
├── go.mod                 module github.com/jae-jae/genkan
├── cmd/genkan/main.go     入口；子命令 `genkan healthcheck`（Docker HEALTHCHECK 用）、`genkan version`；`var version = "dev"` 由 -ldflags "-X main.version=..." 注入
├── internal/
│   ├── store/             数据模型、JSON 持久化、CRUD、排序、导入导出
│   ├── server/            HTTP 路由、REST API、静态资源、SSE、鉴权中间件
│   ├── auth/              密码哈希、会话 cookie、API token 生成、登录限速
│   ├── mcp/               MCP (Streamable HTTP, JSON-RPC 2.0) 实现
│   ├── health/            服务在线检测
│   └── favicon/           图标抓取
├── web/
│   ├── embed.go           package web; //go:embed all:dist  (后端负责)
│   ├── dist/              构建产物（前端负责生成；后端放占位 index.html 保证可编译）
│   ├── package.json, vite.config.ts, tsconfig.json, index.html
│   └── src/               前端源码（前端负责）
├── skills/genkan/         AI Skill 源码（仅 REST，不含 MCP，不含任何密钥/占位符；用户通过 raw.githubusercontent.com 直接从仓库获取，另可从设置页下载安装包）：SKILL.md（入口：凭据、`gk` 助手、数据模型、工作流、最佳实践、常用配方）+ reference.md（完整 REST 参考，每个接口一条可运行 curl）
├── skills/embed.go        package skills；`//go:embed genkan/SKILL.md genkan/reference.md`（go:embed 不能引用父目录），`Files()` 返回与仓库字节一致的文件
├── e2e/                   Playwright 端到端测试
├── Dockerfile, docker-compose.yml, Makefile, README.md, LICENSE, CONTRIBUTING.md, SECURITY.md
├── .github/               CI（测试 + 多架构镜像推送到 GHCR）、dependabot、Issue/PR 模板
```

## 3. 配置（环境变量）

| 变量 | 默认 | 说明 |
|---|---|---|
| `GENKAN_ADDR` | `:8080` | 监听地址 |
| `GENKAN_DATA` | `./data`（Docker 内 `/data`） | 数据目录：`genkan.json`、`uploads/wallpapers/`、`uploads/icons/` |
| `GENKAN_PASSWORD` | 空 | 若设置且尚无管理员密码，启动时用它初始化密码（免去首次设置页） |

## 4. 数据模型

ID 统一为 12 位随机 base32 小写字符串。时间为 RFC3339 字符串，空值为 `null`。

```jsonc
// genkan.json
{
  "schema": 1,
  "auth": { "password_hash": "", "session_secret": "<random hex>", "api_token": "gk_<32 位随机 base32>" },
  "settings": { ... Settings ... },
  "workspaces": [ Workspace ],
  "categories": [ Category ],
  "links": [ Link ]
}
```

**Settings**
```jsonc
{
  "title": "Genkan",
  "sort_mode": "frequency",          // "frequency" | "recent" | "custom"
  "guest_access": false,             // 未登录访客能否只读查看
  "search_engine": "https://www.google.com/search?q=%s",
  "open_in_new_tab": true,
  "health_interval": 60,             // 秒，最小 15
  "show_clock": true
}
```

**Workspace** `{ "id", "name", "icon": "lucide:house", "wallpaper": "preset:dusk", "sort": 0 }`
- `icon`：`lucide:<name>`，`<name>` 必须属于内置的 52 个 Lucide 图标（Go 切片 `store.BuiltinIcons`，前端内置对应 SVG 路径）：`house, layout-grid, folder, briefcase, code, terminal, server, database, hard-drive, cloud, network, router, wifi, globe, shield, lock, key-round, film, clapperboard, tv, music, headphones, image, camera, gamepad-2, book-open, graduation-cap, newspaper, shopping-cart, wallet, chart-line, activity, gauge, cpu, wrench, settings, bot, sparkles, heart, star, bookmark, users, message-circle, mail, calendar, download, flask-conical, leaf, coffee, plane, lightbulb, rocket`。创建/更新时校验，未知名称 → 400；空串 = `lucide:layout-grid`。首次启动创建的默认工作空间为 `{name:"Home", icon:"lucide:house"}`。
- **迁移**：加载数据文件时，任何不以 `lucide:` 开头的旧 emoji 图标自动映射（🏠🏡→house；🎬🎞📺→clapperboard；🛠🔧⚙️→wrench；🌐🌍🌎→globe；💼→briefcase；🎮→gamepad-2；🎵🎶→music；📚📖→book-open；💻🖥→code；☁️→cloud；🔒🔐→lock；⭐→star；❤️→heart；其余→layout-grid）并立即写回文件。`POST /api/import` 同样接受旧导出中的 emoji 图标并做同样映射。
- `wallpaper`：`preset:<id>`（前端内置，id ∈ `dusk, aurora, ocean, forest, sakura, graphite`）或 `upload:<filename>`（URL 为 `/uploads/wallpapers/<filename>`）。**默认壁纸为黄昏（`preset:dusk`）**：首次启动自动创建的 Home、未指定壁纸的新工作空间（REST / MCP / UI 新建表单的默认选中）、空串、登录 / 首次设置页（尚无工作空间）都用它；已存储的显式壁纸不迁移、不改变。

**Category** `{ "id", "workspace_id", "name", "sort": 0 }`

**Link**
```jsonc
{
  "id": "", "workspace_id": "", "category_id": "",   // category_id 为空 = 未分类
  "title": "Jellyfin", "url": "http://nas.lan:8096", "description": "",
  "icon": "",            // 见下
  "pinned": false, "sort": 0, "pin_sort": 0,   // sort = 分类内顺序；pin_sort = 顶部置顶行内顺序
  "clicks": 0, "last_used_at": null,
  "health_check": false,
  "created_at": "", "updated_at": ""
}
```
`icon` 字符串格式：
- `""` → 前端显示标题首字母徽章（颜色由标题哈希决定）
- `favicon:<file>` / `upload:<file>` → `/uploads/icons/<file>`
- `emoji:🚀`
- `lucide:<name>` → 内置 Lucide 图标，`<name>` 必须属于与工作空间相同的 52 个 `store.BuiltinIcons`（未知名称：REST 400、MCP/导入逐条报错）。前端在圆形图标槽内渲染该图标：白色线条字形（约占槽 58%），底色为由标题哈希得到的柔和渐变（与首字母徽章同色系），与真实 favicon 并排协调。无合适 favicon 的服务可用此格式
- `url:https://...` → 直接作为图片 src

**排序语义**（前端负责呈现，后端 `sort` 字段负责自定义顺序）
- `frequency`：clicks 降序，相同则 `sort` 升序
- `recent`：last_used_at 降序（null 最后），相同则 `sort` 升序
- `custom`：`sort` 升序
- 置顶（pinned）的链接**同时**出现在工作空间顶部的置顶行（无标题文字）和它所属的分类（或未分类）中——置顶不改变分类归属，置顶链接仍是分类的普通成员。置顶行排序规则：frequency / recent 与其它区域相同；custom 按 `pin_sort` 升序（分类内仍按 `sort`）。
- `pin_sort`：把链接从非置顶改为置顶（创建时 `pinned:true`、`PATCH`、MCP、`reorder __pinned`）时，`pin_sort = 该工作空间内所有置顶链接 pin_sort 的最大值 + 1`（没有则为 0），即排到置顶行末尾。跨工作空间移动已置顶的链接时同样重新排到新工作空间置顶行末尾。导入/导出携带 `pin_sort`；旧数据（缺少该字段）在加载/导入时 `pin_sort = sort`（迁移并写回）。
- 分类、工作空间始终按各自 `sort` 升序。
- 新建对象 `sort` = 同级最大值 + 1。

## 5. 鉴权

- 单管理员。密码 bcrypt 存储。最少 6 位。
- 首次运行若无密码且未设置 `GENKAN_PASSWORD` → `setup_required=true`，前端显示"设置管理员密码"页。
- 会话：登录成功后下发 `genkan_session` cookie（HttpOnly, SameSite=Lax, Path=/, 30 天；请求经 TLS 或 `X-Forwarded-Proto: https` 时加 Secure）。值为 `过期时间戳.HMAC-SHA256(session_secret)`，无状态。修改密码时轮换 `session_secret`（使所有会话失效，当前会话重新签发）。
- API Token：**全站只有一个**，存于 `genkan.json` 的 `auth.api_token`（明文，仅 admin 可读；格式 `gk_` + 32 位随机 base32 小写字符）。首次启动缺失时随机生成；旧数据文件中的 `tokens` 数组在加载时丢弃并生成新 Token。**绝不**出现在 `/api/export` 或 `/api/bootstrap` 中。`Authorization: Bearer gk_...` 可访问 REST API 与 `/mcp`，权限等同管理员；比较使用 `crypto/subtle` 常量时间。用户可在界面重新生成（`POST /api/token/regenerate`），旧 Token 立即失效。
- 角色：`admin`（cookie 或 token）、`guest`（未登录且 `guest_access=true`，只读）、`none`。
- CSRF：使用 cookie 鉴权的非 GET 请求必须带请求头 `X-Requested-With: genkan`，否则 403。Bearer token 请求不需要。
- 登录限速：同一 IP 1 分钟内失败 5 次 → 429。
- 访客点击不计数（`POST /click` 对 guest 返回 204 但不记录）。

## 6. REST API

全部 JSON。错误统一 `{"error": "message"}` + 合适状态码（400/401/403/404/409/413/429）。
权限标注：**P** = 公开，**R** = admin 或 guest 可读，**A** = 仅 admin。

| 方法 路径 | 权限 | 请求 | 响应 |
|---|---|---|---|
| `GET /api/bootstrap` | P | – | `Bootstrap`（见下） |
| `POST /api/auth/setup` | P（仅 setup_required 时） | `{password}` | `{ok:true}` + 设置 cookie |
| `POST /api/auth/login` | P | `{password}` | `{ok:true}` + cookie；错误 401 |
| `POST /api/auth/logout` | P | – | `{ok:true}` 清 cookie |
| `POST /api/auth/password` | A | `{old_password,new_password}` | `{ok:true}` |
| `PATCH /api/settings` | A | Settings 部分字段 | 完整 Settings |
| `POST /api/workspaces` | A | `{name, icon?, wallpaper?}` | Workspace |
| `PATCH /api/workspaces/{id}` | A | 部分字段 | Workspace |
| `DELETE /api/workspaces/{id}` | A | – | `{ok:true}`（级联删除其分类、链接；不允许删最后一个 → 409） |
| `POST /api/workspaces/reorder` | A | `{ids:[...]}` | `{ok:true}` |
| `POST /api/categories` | A | `{workspace_id, name}` | Category |
| `PATCH /api/categories/{id}` | A | `{name?}` | Category |
| `DELETE /api/categories/{id}` | A | `?delete_links=1` 可选 | `{ok:true}`（默认其链接变为未分类） |
| `POST /api/categories/reorder` | A | `{workspace_id, ids}` | `{ok:true}` |
| `POST /api/links` | A | `{workspace_id, category_id?, title, url, description?, icon?, pinned?, health_check?, fetch_icon?}` | Link |
| `PATCH /api/links/{id}` | A | 部分字段（含 workspace_id/category_id 移动） | Link |
| `DELETE /api/links/{id}` | A | – | `{ok:true}` |
| `POST /api/links/reorder` | A | `{workspace_id, category_id, ids}` | `{ok:true}` 按 ids 顺序重写 `sort`，并把这些链接的 category_id 设为该分类（支持跨分类拖拽；置顶链接是分类的普通成员，同样参与重排）。`category_id:"__pinned"` 表示对置顶行排序：只重写 `pin_sort`（并把列出的链接设为 pinned），**绝不**改动 `sort` 与 `category_id`；未列出的置顶链接保持相对顺序排在其后 |
| `POST /api/links/{id}/click` | R | – | 204 |
| `POST /api/links/{id}/refresh-icon` | A | – | Link（同步抓取，超时 8s） |
| `GET /api/health` | R | – | `{ "<link_id>": {status:"up"\|"down"\|"unknown", latency_ms, checked_at, code} }` |
| `POST /api/uploads/wallpaper` | A | multipart `file`（jpg/png/webp/avif，≤ 15MB） | `{value:"upload:<file>", url}`（文件名由内容哈希决定，重复上传同一张图是幂等的；前端多选上传时逐张调用） |
| `GET /api/wallpapers` | A | – | 壁纸图库：`[{value:"upload:<file>", url:"/uploads/wallpapers/<file>", size, created_at}]`（`created_at` 为文件 mtime，RFC3339），按时间倒序，读取自 `uploads/wallpapers/` |
| `DELETE /api/wallpapers/{file}` | A | – | `{ok:true}`。`file` 必须是安全文件名（`[A-Za-z0-9._-]`，不以 `.` 开头，否则 400）；文件不存在 → 404。删除文件，并把所有 `wallpaper == "upload:<file>"` 的工作空间重置为默认壁纸 `preset:dusk`，递增版本并广播 SSE change |
| `POST /api/uploads/icon` | A | multipart `file`（png/svg/ico/webp/jpg，≤ 1MB） | `{value:"upload:<file>", url}` |
| `GET /api/export` | A | – | 导出 JSON（`{schema, settings, workspaces, categories, links}`，不含 auth / API Token），`Content-Disposition: attachment` |
| `POST /api/import` | A | `{mode:"replace"\|"merge", data:<export json>}` | `{ok:true, workspaces:n, links:n}` merge 模式：新 ID 导入，不覆盖现有 |
| `GET /api/skill` | A | – | `application/zip`，文件名 `genkan-skill.zip`，内含 `genkan/SKILL.md`、`genkan/reference.md`（与仓库/内嵌文件字节一致，通用、不含密钥、不含 MCP）和服务端生成的 `genkan/.env`（zip 头 unix 权限 0600；内容 `GENKAN_URL=<实例 origin>` 与 `GENKAN_TOKEN=<当前 Token>` 两行）。origin 由请求推导：scheme 取 `X-Forwarded-Proto`（否则 TLS/http），host 取 `X-Forwarded-Host`（否则 `Host`） |
| `GET /api/token` | A | – | `{"token":"gk_..."}`（`Cache-Control: no-store`） |
| `POST /api/token/regenerate` | A | – | `{"token":"gk_新..."}`；旧 Token 立即失效；Cookie 鉴权需 CSRF 头 |
| `GET /api/events` | R | – | SSE 流 |
| `GET /healthz` | P | – | `ok` |
| `POST /mcp` | Bearer token | JSON-RPC | 见第 8 节 |

**Bootstrap**
```jsonc
{
  "auth": { "setup_required": false, "role": "admin" },   // role: admin|guest|none
  "settings": { ...Settings },        // role=none 时只返回 {title}
  "workspaces": [], "categories": [], "links": [],   // role=none 时为空数组
  "health": { },                      // 同 /api/health
  "version": 42,                      // 数据版本号，每次写操作 +1（点击计数不 +1）
  "server_version": "v0.1.0"          // 服务端构建版本（-ldflags "-X main.version=..."，默认 "dev"）；role=none 时省略；也由 `genkan version` 输出，显示在 设置 → 关于
}
```

**SSE `/api/events`**
- `event: change` / `data: {"version": 43}` —— 任意数据变更（含 MCP 修改、图标抓取完成）。前端收到后重新拉 `/api/bootstrap`（防抖 300ms）。
- `event: health` / `data: { ...health map }` —— 每轮检测结束推送。
- 每 25 秒发送 `: ping` 注释保活。连接数上限 64。

**静态资源**
- 嵌入的 `web/dist`：`/assets/*` 带 hash，`Cache-Control: public, max-age=31536000, immutable`；`index.html` `no-cache`；未知非 `/api` 路径回退 `index.html`。
- 启动时对嵌入的文本资源（js/css/html/svg/json）预先 gzip 并缓存于内存，按 `Accept-Encoding` 返回。
- `/uploads/*` 从数据目录读取，`Cache-Control: public, max-age=86400`；需 R 权限（访客模式关闭时未登录不可读）。上传文件名使用内容 sha256 前 16 位 + 扩展名。

## 7. 后台功能

**健康检查**：对 `health_check=true` 的链接，每 `health_interval` 秒检测一次；并发上限 8；超时 5s；先 HEAD，若 405/501 则 GET；**允许自签证书**（InsecureSkipVerify，homelab 常见）；任何 HTTP 响应且状态码 < 500 视为 `up`，否则或网络错误为 `down`。结果仅存内存。新开启检测的链接应尽快（几秒内）首次检测。

**图标抓取**：创建链接时 `icon` 为空且 `fetch_icon` 不为 false → 异步抓取；完成后更新 `icon=favicon:<file>` 并广播 change。流程：GET 页面（≤ 1MB，5s 超时，允许自签证书）→ 解析 `<link rel="icon"|"shortcut icon"|"apple-touch-icon"|"mask-icon">`，优先 apple-touch-icon / 最大 sizes / svg → 失败回退 `/favicon.ico` → 校验为图片（≤ 512KB）→ 存 `uploads/icons/`。失败则保持空（显示首字母）。

## 8. MCP（Streamable HTTP）

MCP 与 Skill 是**两条互相独立、二选一**的 AI 接入路径：安装 Skill 的用户不需要配置 MCP（Skill 直接用 REST API，见第 6 节），配置了 MCP 的用户也不需要 Skill。两者共用同一个 API Token，权限相同。

- 端点 `POST /mcp`，需 `Authorization: Bearer gk_...`（即唯一的 API Token，常量时间比较；Token 重新生成后旧值立即 401）。无状态实现：每个 POST 一个 JSON-RPC 请求（也支持数组批量），响应 `Content-Type: application/json`。`GET /mcp` → 405。通知（无 id）返回 202。
- 支持方法：`initialize`（protocolVersion 回显客户端版本，若不认识则 `2025-06-18`；capabilities `{tools:{}}`；serverInfo `{name:"genkan",version}`；instructions 简述用途）、`notifications/initialized`、`ping`、`tools/list`、`tools/call`。
- 工具结果：`{content:[{type:"text",text:<JSON 字符串>}], structuredContent:<对象>}`；业务错误 `isError:true`。
- 所有修改**直接生效**并通过 SSE 广播，前端实时刷新。

工具清单（inputSchema 需完整 JSON Schema，带中文/英文 description）：
| 工具 | 参数 | 说明 |
|---|---|---|
| `get_overview` | – | 返回全部工作空间→分类→链接的树形结构（含 id、clicks、pinned、`pin_sort` 等；置顶链接同时出现在其分类中），以及 settings |
| `search_links` | `query`, `workspace_id?` | 模糊匹配 title/url/description |
| `create_workspace` | `name`, `icon?`, `wallpaper?` | `icon` 的 schema 是 enum（52 个 `lucide:<name>`，默认 `lucide:layout-grid`） |
| `update_workspace` | `id`, `name?`, `icon?`, `wallpaper?` | `icon` 同上 enum |
| `delete_workspace` | `id` | |
| `create_category` | `workspace_id`, `name` | |
| `update_category` | `id`, `name` | |
| `delete_category` | `id`, `delete_links?` | |
| `create_links` | `links: [{workspace_id, category_id?, title, url, description?, icon?, pinned?, health_check?}]` | 批量创建（自动抓图标）；`icon` 可为 `lucide:<name>`（52 个内置名之一，适合没有合适 favicon 的服务）；`pinned:true` 的链接置顶（仍保留在分类中，排到置顶行末尾） |
| `update_link` | `id`, 任意可改字段（含移动 `workspace_id`/`category_id`） | |
| `delete_links` | `ids: []` | 批量删除 |
| `reorder` | `kind: workspaces\|categories\|links`, `ids`, `workspace_id?`, `category_id?` | 同 REST reorder 语义：`category_id:"__pinned"` 仅调整置顶行（`pin_sort`），其余按分类重写 `sort` |
| `update_settings` | Settings 部分字段 | |

## 9. 前端 UI/UX

**视觉**：全屏壁纸（内置预设为多层 radial-gradient 组成的"网格渐变"柔和色块，6 套，要好看；或上传图片 `object-fit: cover`），上覆轻微暗化层保证文字可读。所有交互元素是**毛玻璃胶囊**：`backdrop-filter: blur(18px) saturate(160%)`、半透明白底、1px 半透明描边、`border-radius: 999px`、柔和阴影；hover 提亮 + 轻微上浮；文字白色。弹窗/面板为毛玻璃圆角卡片。系统字体栈。尊重 `prefers-reduced-motion`。不支持 backdrop-filter 时回退为更不透明的底色。

**布局（桌面）**
- 顶栏**左上角**：一个工作空间下拉胶囊按钮（当前工作空间的 Lucide 图标 + 名称 + 箭头；不使用 emoji）。点击展开毛玻璃下拉菜单，列出全部工作空间（Lucide 图标、名称、`1`–`9` 快捷键提示、当前项打勾），选择即切换并关闭。`aria-haspopup="menu"` / `aria-expanded`，菜单 `role=menu`，条目 `menuitemradio`；Esc、点击外部、选择后关闭，关闭后焦点回到按钮；↑↓/Home/End 移动，Enter 选择。编辑模式（admin）下每项出现 上移/下移/编辑 小按钮（工作空间排序用上下移动按钮而非拖拽，键盘与触屏友好），编辑与删除走工作空间编辑弹窗（含 52 个内置图标的选择网格，可按名称搜索，无 emoji 输入）。**admin 的菜单始终**以分隔线 + “新建工作空间” + “管理工作空间…”（打开设置 → 内容）结尾，不依赖编辑模式。访客只看到列表。
- 顶栏**右上角**（从左到右）：编辑模式开关（铅笔 / “完成”，仅 admin）、**AI 按钮**（Lucide `bot` 机器人图标，`aria-label="AI"`，仅 admin，直接打开设置的 AI 标签）、**主菜单**胶囊按钮（sliders 图标）。主菜单（admin）：“排序”分组（频率 / 最近 / 自定义，radio 风格打勾）、分隔线、设置、退出登录；访客只有“登录”。关闭规则同上。看板不再有排序控件；编辑模式下看板工具栏只剩“新建分类”。
- 中部：可选时钟（大号时间 + 日期，按语言格式化）；宽搜索胶囊。
- 置顶行：位于顶部，**没有**“收藏/Pinned”标题文字，仅显示 pinned 链接胶囊；这些链接同时仍在各自分类中显示。置顶行排序：frequency/recent 同全局，custom 按 `pin_sort`。
- 分类区：**一行一个分类**（整行宽度，不再两列），分类标题（小号半透明文字）+ 链接胶囊自动换行流式排列。未分类放最后。
- 链接胶囊：图标(20–24px 圆形) + 标题；有 description 时变为两行（标题 + 下方 ~12px、约 70% 不透明度、单行省略的描述，限制最大宽度），无描述保持单行；开启健康检查时右侧小圆点（绿 up / 红 down / 灰 unknown）；hover 显示 description 与 url 的 tooltip（title 属性即可）。手机两列网格同样显示（截断的）描述。
- 空状态：引导用户添加第一个链接或使用 MCP。

**布局（手机 ≤ 640px）**：顶栏同样是左上工作空间下拉 + 右上编辑/主菜单（同一种下拉，不做底部抽屉，宽度受视口限制）；搜索置顶；链接胶囊两列网格；弹窗变为底部抽屉；安全区 `env(safe-area-inset-*)`；可左右滑动切换工作空间（可选）。

**交互**
- 点击链接：`navigator.sendBeacon('/api/links/{id}/click')`（beacon 无法带自定义头 → 后端对 `/click` 免除 CSRF 头检查，它是幂等计数且仅影响统计），然后按设置在新标签/当前页打开。
- 搜索：`/` 或 `Ctrl/⌘+K` 聚焦；跨所有工作空间模糊匹配（title、url、description、分类名，子序列匹配并打分）；下拉结果列表最多 8 条，↑↓ 选择，Enter 打开，Esc 关闭；无结果时 Enter 用 `search_engine` 搜索网页（显示"在网络上搜索 xxx"项）。数字键 `1-9`（搜索框未聚焦时）切换工作空间。
- 排序模式切换：设置中，或右上角主菜单的“排序”分组（频率 / 最近 / 自定义）。
- **编辑模式**（仅 admin）：链接胶囊出现编辑/删除小按钮；每个分类末尾有"+"胶囊；"新建分类"按钮；分类标题可重命名/删除/拖动排序；链接可拖拽排序（含跨分类；把分类中的链接拖入置顶行 = 置顶：`PATCH pinned:true` 再 reorder `__pinned`；置顶行内拖拽只 reorder `__pinned`；从置顶行拖入分类无效果，取消置顶请用右键菜单/编辑弹窗）；拖拽时若排序模式不是 custom，自动切换为 custom 并提示。工作空间在下拉菜单里用上移/下移按钮排序，右键菜单项或点铅笔编辑。
- 链接编辑弹窗字段：标题、URL、描述、工作空间、分类、图标（自动抓取 / emoji / 图标库(Lucide) / 上传 / 图片 URL / 首字母；“图标库”模式复用工作空间编辑器的可搜索 52 图标网格，手机底部弹窗中网格限高可滚动）、置顶、健康检查。粘贴 URL 时若标题为空则自动用主机名填充。
- **右键菜单**（毛玻璃，`role=menu`，方向键导航，Esc / 点击外部 / 滚动 / 缩放关闭，位置限制在视口内）：
  - 页面背景右键（不含胶囊/输入框；输入框与文本选区保留原生菜单）：admin：新建链接、新建分类、更换壁纸（打开设置 → 外观）、编辑模式（带勾选状态）、AI、设置；访客仅“登录”。
  - 链接胶囊右键：打开、在新标签页打开、复制链接 —— 分隔线 —— 编辑、置顶 / 取消置顶、删除（红色，需确认）；访客只有前三项。
  - 触屏：长按（约 500ms，移动 > 8px 取消）非编辑模式下的胶囊打开胶囊菜单（≤ 640px 为底部抽屉）；编辑模式下长按仍是拖拽；长按不触发跳转。
- 设置面板（标签页，顺序）：常规、外观、内容、AI、安全、数据、关于；可直接打开到指定标签（AI 按钮、右键菜单、工作空间菜单使用）。
  - 常规：站点标题、语言、时钟、新标签打开、搜索引擎、排序模式。
  - 外观（当前工作空间壁纸）：6 个预设缩略图，其后是“我的图库”网格（`GET /api/wallpapers`）：当前选中项高亮，点击应用到当前工作空间；悬停/长按显示删除（×，需确认，调用 `DELETE /api/wallpapers/{file}`）；“上传”磁贴支持多选与拖放文件，逐张顺序上传并显示进度，新图片出现在图库；可选“应用到所有工作空间”。
  - 内容（内容管理器）：左侧（手机在上方）工作空间列表（Lucide 图标 + 名称 + 链接数；新增、编辑、删除（确认）、上移/下移）；右侧所选工作空间的分类（含未分类）折叠分组，分组头有 重命名/删除/上移下移/“+ 添加链接”，每个链接行显示图标、标题、描述（弱化）、URL（弱化、截断）及 置顶（星标）/编辑/删除（确认）；“新建分类”按钮；顶部搜索框跨所有工作空间过滤链接并显示 工作空间 › 分类 路径。全部通过现有 REST 接口，改动实时同步。
  - AI（原“AI / MCP”，全站不再出现旧名称）：一句话简介；“API Token”卡片（遮罩 `gk_••••••••…末4位`，图标按钮：显示/隐藏、复制、重新生成（行内二次确认，说明旧 Token 立即失效、需把 Skill 提示词重新发给 AI 并更新 MCP 配置）），其下仅一行紧凑琥珀色警告（Token 等同管理员权限，提示词与 MCP 配置中都包含它，请勿分享；泄露后请重新生成）；“接入方式”分段控件 **[Skill（推荐）| MCP]**（二选一：Skill 直接调用 REST API、无需 MCP 配置，MCP 是替代方案），只显示选中面板：
    - **Skill**（`pane-skill`）：一句提示（“复制下面这段提示词发给你的 AI 助手（Claude Code、Codex 等），它会自动安装 Skill 并配置 Token。重新生成 Token 后再发一次即可更新。”，并说明无需 MCP），其下是一个自动换行的提示词代码块（`skill-prompt`，右上角图标复制按钮 `copy-skill-prompt`）。其下是次要的、低调的一行“或手动安装：[下载 Skill 安装包]（`skill-download`，调用 `/api/skill`）解压到 skills 目录（Claude Code：`~/.claude/skills/`）。安装包内含 Token，请勿分享。”。提示词（英文，两种界面语言相同，约 6–10 行）要求 AI：① 把 `SKILL.md` 与 `reference.md` 从 `https://raw.githubusercontent.com/jae-jae/genkan/<ref>/skills/genkan/` 下载到它的 skills 目录下名为 `genkan/` 的文件夹（Claude Code：`~/.claude/skills/genkan/`；其他 Agent：各自的 skills 目录），覆盖已有文件；② 创建 `~/.config/genkan/env`（必要时建目录），写入 `GENKAN_URL=<当前页面 origin>` 与 `GENKAN_TOKEN=<真实 Token>` 两行并 `chmod 600`；③ 用 `GET $GENKAN_URL/api/bootstrap`（Bearer Token）验证，报告工作空间与链接数，且不得打印 Token。`<ref>`：`server_version`（去掉前导 `v`）是 `x.y.z` 形式的发布版本时为 `v<x.y.z>`，否则（`dev`、`git describe` 的脏/带提交数版本等）为 `main`。Skill 读取凭据的顺序（`gk` 助手实现，SKILL.md 描述）：环境变量 `GENKAN_URL` / `GENKAN_TOKEN` → `~/.config/genkan/env` → Skill 自身目录里的 `.env`（即 SKILL.md 所在目录；下载包自带）；都缺失时让 AI 请用户重新复制提示词或重新下载安装包。
    - **MCP**（`pane-mcp`）：简短说明（Skill 的替代方案），其下两个带短标签的代码块：Claude Code（已填入真实 Token 的 `claude mcp add --transport http genkan <origin>/mcp --header "Authorization: Bearer <token>"`，`mcp-command` / `copy-mcp-command`）与其他 MCP 客户端（`mcpServers` JSON 片段，`mcp-json` / `copy-mcp-json`）。
    - 代码块右上角仅图标的复制按钮（复制后 1.5s 显示对勾）；MCP 代码块不换行（横向滚动）；Token 隐藏时提示词与片段也遮罩（DOM 中不含真实值），但复制始终复制真实值。
  - 安全（访客只读开关、修改密码）、数据（导出 JSON、导入 JSON 替换/合并）、关于（版本，及 Lucide 图标的 ISC 许可声明）。
- 登录页 / 首次设置页：壁纸背景 + 居中毛玻璃卡片。访客模式下右上角显示"登录"。
- 实时更新：连接 `/api/events`，断线指数退避重连；页面重新可见时也刷新。
- 乐观更新 + 失败 toast 提示。

**i18n**：中文 / English 两套字典；设置 → 常规 → 语言有三个选项：自动（跟随浏览器，**默认**）/ 简体中文 / English，选择以 `auto` | `zh` | `en` 存 localStorage `genkan.lang`，缺失或非法值视为 `auto`（之前已显式存了 `zh`/`en` 的用户保持不变）。自动模式按 `navigator.languages`（回退 `navigator.language`）取第一个 zh* / en* 项决定（zh* → 中文，其余 → English），并监听 `languagechange` 事件实时切换；`<html lang>` 随之设为 `zh-CN` / `en`。所有可见文案走字典。

**可访问性**：按钮有 aria-label；焦点可见；对比度足够；键盘可完成主要操作。

**PWA**：`manifest.webmanifest` + 图标（SVG + 192/512 PNG），`theme-color`，可添加到手机主屏。不需要 service worker。

## 10. Docker

- 多阶段：`node:22-alpine` 构建前端 → `golang:1.2x-alpine` 构建（`-trimpath -ldflags "-s -w"`，支持 `TARGETOS/TARGETARCH` 交叉编译）→ `FROM scratch`，复制 ca-certificates、二进制；`ENV GENKAN_DATA=/data`；`VOLUME /data`；`EXPOSE 8080`；`HEALTHCHECK CMD ["/genkan","healthcheck"]`。
- `docker-compose.yml` 示例（端口、卷、`GENKAN_PASSWORD`、`restart: unless-stopped`、可选 `user: "1000:1000"`）。
- Makefile：`make web`、`make build`、`make docker`、`make test`、`make e2e`、`make screenshots`；版本号取自 `git describe --tags --always --dirty`，通过 `-ldflags "-X main.version=..."` 注入；Dockerfile 使用 `ARG VERSION=dev`，前端与 Go 编译阶段均运行在 `$BUILDPLATFORM` 上（Go 按 TARGETOS/TARGETARCH 交叉编译，无需 QEMU）。
- CI（`.github/workflows/ci.yml`）：`test` 任务（构建前端、`go vet`、`go test -race`）；push 到 main / `v*` 标签时 `docker` 任务构建 linux/amd64 + linux/arm64 镜像并推送到 `ghcr.io/jae-jae/genkan`（标签：`latest`、`x.y.z`、`x.y`、`sha-<short>`）。

## 11. 测试

- Go 单元测试：store（CRUD、排序、reorder、导入导出、持久化原子性）、auth（会话签名、token 生成、限速）、mcp（initialize、tools/list、tools/call）、server（权限矩阵、CSRF）。
- 端到端（Playwright，Chromium）：对**Docker 容器**运行。覆盖：首次设置密码、登录/登出、创建工作空间/分类/链接、编辑删除、置顶（同时出现在顶部与分类中）、右键菜单、内容管理器、壁纸图库（多选上传/删除）、搜索与键盘导航、点击计数与排序模式、拖拽排序、访客只读、壁纸切换与上传、导入导出、单一 API Token（默认生成、重新生成后旧 Token 失效、Skill 提示词含真实 Token / origin / 正确的 raw.githubusercontent.com 版本引用，遮罩时复制仍为真实值；下载包 `.env` 含当前 Token 且重新生成后新下载随之更新）+ MCP 调用后 UI 实时刷新、顶栏两个下拉菜单、手机视口布局、中英文切换。
- 截图（`npm run screenshots`）输出到被 git 忽略的 `.tmp/screenshots/`（默认英文界面，`GENKAN_SHOTS_LANG=zh` 为中文），再由 `npm run docs-images` 合成并压缩为 `docs/images/`（hero + 图库，JPEG）。覆盖：桌面（主页、编辑模式、搜索、链接编辑、工作空间下拉、主菜单、设置-AI、设置-内容、设置-外观、登录页）与手机（主页、搜索、链接编辑、媒体工作空间），使用一套逼真的 homelab 示例数据。
