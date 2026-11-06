# Genkan 插件机制 — 需求与接口契约 (PLUGINS)

本文档是插件功能的**唯一契约**：后端、前端、示例插件三方都以此为准。实现中如发现契约不合理，可以小幅调整，但必须同步修改本文件并保持三方一致。

## 0. 目标与原则

- 用 JavaScript 插件扩展导航页：**壁纸源、网页小组件(Widget)、搜索提供者、定时任务、小组件按钮动作**。
- 插件在**服务端**运行，引擎为纯 Go 的 [moejs](https://github.com/MoeForge/moejs)（`github.com/MoeForge/moejs`，使用 `v0.1.0-alpha.11` 或更新版本；`eventloop` + `webapi` 子包提供定时器和 `fetch`）。保持 `CGO_ENABLED=0`、单二进制。
- 浏览器**永不执行插件代码**。插件输出的是**声明式 JSON**（小组件视图、搜索结果、壁纸元数据），由前端用固定组件渲染，不使用 `innerHTML`。
- 遵守 SPEC 的第一原则：轻量。没有启用插件时不创建任何 JS 运行时、不增加常驻内存；首屏 JS gzip 仍需 ≤ 60 KB（插件相关 UI 尽量精简）。
- AI-native：AI 可以通过 REST / MCP **编写、安装、测试**插件（"帮我写一个显示 Pi-hole 拦截数的小组件"）。
- 安全：HTTP 只能访问 manifest 声明的主机；SSH 资源采集需独立声明 `permissions.ssh:true`，可连接调用参数指定的主机（包含内网），可选验证指纹；有超时、内存上限、存储上限；无文件系统访问。

## 1. 插件文件格式

一个插件 = **一个 ES module 源文件**（`.js`；也接受 TypeScript，用 `moejs.CompileTS` 编译，由安装请求的 `lang: "ts"` 指定）。插件 ID 取自 manifest。

```js
// 界面文字放进 messages，manifest 里用 t("key") 引用，钩子里用 ctx.t("key", params)（详见 §12）
export const messages = {
  en: { name: "Weather", desc: "Open-Meteo weather, no API key." },
  zh: { name: "天气",    desc: "Open-Meteo 天气，无需 API key。" },
}

export const manifest = {
  id: "weather",                 // 必填，^[a-z0-9][a-z0-9-]{1,39}$，全局唯一
  name: t("name"),               // 必填，≤ 40 字符。可见文字都是 LocalizedString：普通字符串、{ en, zh } 对象或 t("key")（§12）
  version: "1.0.0",              // 必填，≤ 20 字符
  description: t("desc"),        // 可选，≤ 300（LocalizedString）
  author: "genkan",              // 可选，≤ 60
  homepage: "https://...",       // 可选 http(s)
  icon: "cloud",                 // 可选，Lucide 名（前端 LUCIDE 表中有的才显示，否则回退为 puzzle/默认图标）

  permissions: {
    fetch: ["api.open-meteo.com", "*.bing.net"], // 允许 fetch 的主机（语义同 webapi.AllowHosts；"*" 表示任意主机，UI 必须以警告样式显示）
    links: "read",               // 可选："read" | "write"；授权 genkan.links.* 宿主 API
  },

  // 插件级配置（在 设置→插件 中填写），也作为 ctx.config 传入
  config: [ /* ConfigField[] 见下 */ ],

  // 提供的能力，全部可选，至少一项
  wallpapers: [{ id: "daily", name: "每日壁纸", refresh: 3600 }],               // name ≤ 40（LocalizedString）；refresh 秒，≥ 300，默认 3600
  widgets:    [{ id: "current", name: "当前天气", size: "s", refresh: 600,     // name ≤ 40（LocalizedString）；size: "s"|"m"|"l"；refresh 秒 ≥ 30，默认 300
                 config: [ /* 实例级 ConfigField[]，每个小组件实例单独填写 */ ] }],
  search:     { name: "计算器", prefix: "" },                                  // name ≤ 40（LocalizedString）；prefix 非空时仅当查询以 "<prefix> " 开头才调用，并把去掉前缀后的文本作为 ctx.query
  schedules:  [{ id: "sync", name: "同步", every: 3600 }],                      // name 可选（LocalizedString，≤ 40）；every 秒 ≥ 60
}

export async function wallpaper(ctx) { /* → WallpaperResult */ }
export async function widget(ctx)    { /* → WidgetView */ }
export async function search(ctx)    { /* → SearchResult[] */ }
export async function schedule(ctx)  { /* → 任意，忽略 */ }
export async function action(ctx)    { /* → { toast?: string, refresh?: boolean, view?: WidgetView }（view 见 §15.3） */ }
```

声明了某能力却没有导出对应函数 → 安装时校验失败。

### ConfigField

```ts
{
  key: string            // ^[a-zA-Z][a-zA-Z0-9_]{0,39}$
  type: "string" | "text" | "number" | "boolean" | "select" | "secret" | "url"
  label: LocalizedString // ≤ 60（每种语言各自 ≤ 60，见 §12）
  help?: LocalizedString // ≤ 200
  default?: any
  required?: boolean
  options?: { value: string, label: LocalizedString }[]   // select 必填，label ≤ 100
  min?: number, max?: number                       // number
  multiline?: boolean    // 仅 secret：多行私钥，最多 5000 字符；保密、保留和清除语义不变
  allowFetch?: boolean   // 仅 type:"url"：把此 URL 的主机（含端口）加入 fetch 白名单。用于 homelab 内网服务（如 Pi-hole 地址由用户填写）
}
```

- `secret` 值**永不**返回给浏览器：管理员读取时返回 `""` 并附带 `secrets_set: ["key", ...]`；PATCH 时传 `""` 或省略表示保持原值，传 `null` 表示清除。
- 校验：类型、required、select 选项、number 范围、url 必须 http(s)。未知 key 丢弃。

## 2. 执行环境（后端）

- 每个插件编译一次（`moejs.Compile` / `CompileTS`），按需创建运行时并放入小池（每插件最多 2 个空闲运行时），**插件被禁用/删除/更新时丢弃池**。
- 每个运行时：`eventloop` + `webapi.Install`，`Options.MemoryLimit = 32 MiB`；`http.Client` 超时 15 s；`Allow` = manifest `permissions.fetch` ∪ 插件配置里 `allowFetch` 的 url 字段主机。默认不允许任何主机。
- 每次调用的超时：`search` 2 s，`widget`/`wallpaper`/`action` 10 s，`schedule` 60 s；超时用 `Interrupt` 终止。
- 同一插件同一钩子的并发调用通过 singleflight 合并（如多个浏览器同时请求同一小组件实例）。
- 插件错误不能影响主程序：捕获 panic、记录到插件日志，API 返回错误信息。

### 2.1 全局 `genkan` 宿主 API

```ts
genkan.log(...args)                 // 写插件日志（环形缓冲，每插件最近 200 条，内存中）
genkan.storage.get(key): any        // 每插件持久 KV（JSON 值），存 data/plugins/<id>.kv.json
genkan.storage.set(key, value)      // 总大小上限 256 KiB，超出抛错；写入防抖落盘
genkan.storage.delete(key)
genkan.storage.keys(): string[]
genkan.widgets.list(): { id, widget, workspace_id, config }[]  // 本插件自己的小组件实例（config 含 secret 明文），见 §13.2
genkan.links.list(): Link[]         // 需 permissions.links >= "read"；字段同 REST 的 Link
genkan.links.create(input): Link    // 需 "write"；input 同 POST /api/links
genkan.links.update(id, patch): Link// 需 "write"
genkan.workspaces.list() / genkan.categories.list()   // 需 "read"
genkan.version                      // 服务器版本字符串
```

全局 `t(key)` 仅在模块求值期间（即写 `manifest` 时）可用，返回一个占位标记，安装时展开为 LocalizedString（§12）；在钩子里调用会抛错并提示改用 `ctx.t`。

此外有标准 `fetch`/`URL`/`URLSearchParams`/`AbortController`/`TextDecoder` 等（来自 webapi），以及 `setTimeout` 等定时器。没有 `require`、`import`（禁止导入其它模块）、文件系统。

### 2.2 ctx 对象

```ts
{
  plugin: { id, version },
  config: { ...插件级配置（含 secret 明文） },
  instance?: { id, config, storage },   // widget/action：当前小组件实例；storage = 实例级存储（§13.1）
  source?: string,             // wallpaper：壁纸源 id
  widget?: string,             // widget/action：小组件 id
  query?: string,              // search：去掉前缀后的查询
  action?: string, params?: any, // action
  schedule?: string,           // schedule：任务 id
  lang: string,                // 请求方语言的基础语言（"zh"、"en"…；Accept-Language/前端传参），定时任务为 "en"
  locale: string,              // 给 Intl / toLocaleString 用的 BCP-47 标签：zh → "zh-CN"，en → "en-US"
  t: (key: string, params?: object) => string,   // 翻译（§12）
  now: number                  // ms 时间戳
}
```

## 3. 返回值格式

### 3.1 WallpaperResult

```ts
{ url: string,          // 图片 http(s) URL（服务端下载）或 data:image/...;base64
  title?: string, credit?: string, link?: string }
// 也可返回数组，服务端随机挑一张
```

服务端用插件的 fetch 白名单下载图片（≤ 15 MiB，`media.Sniff` 校验，仅 jpg/png/webp/avif），存入 `data/uploads/plugin-wallpapers/`（内容哈希文件名），每个壁纸源保留最近 5 张，旧的删除。浏览器只从本站加载图片（离线友好、保护隐私）。

### 3.2 WidgetView（声明式 UI）

```ts
{
  title?: string, icon?: string, link?: string,   // 标题栏；link 为 http(s)
  body: Block[],                                  // ≤ 50 个块（嵌套计入），超出截断
  error?: string                                  // 插件可主动返回错误信息
}

type Block =
  | { type: "text",     text: string, size?: "s"|"m"|"l"|"xl", muted?: boolean, align?: "left"|"center"|"right" }
  | { type: "stat",     label: string, value: string|number, unit?: string, icon?: string, trend?: "up"|"down"|"flat" }
  | { type: "stats",    items: StatBlock[] }                        // 网格排列，≤ 6
  | { type: "list",     items: { title: string, subtitle?: string, url?: string, icon?: string, image?: string, right?: string, badge?: string }[] }  // ≤ 20
  | { type: "kv",       items: { k: string, v: string }[] }         // ≤ 20
  | { type: "progress", label?: string, value: number /*0..1*/, text?: string }
  | { type: "image",    src: string, alt?: string, href?: string, ratio?: number }
  | { type: "chart",    kind: "spark"|"bar", values: number[], labels?: string[] }  // ≤ 60 点，前端内联 SVG 绘制
  | { type: "chart",    kind: "line", points: [tsMs, value][], format?, color? }       // 时间序列折线图，≤ 500 点，见 §13.5
  | { type: "row",      children: Block[] }                         // 水平排列
  | { type: "button",   label: string, action: string, params?: any, icon?: string }  // 仅管理员可点；POST action 钩子
  | { type: "divider" }
```

- 后端负责**规范化与清洗**：丢弃未知类型和字段、截断字符串（单个字符串 ≤ 500）、过滤非 http(s)/非 `data:image/` 的 URL、限制数量。前端仍需防御性渲染。
- 字符串一律作为纯文本渲染（唯一例外是经白名单清洗的 `html` 块）。
- 更多块类型 `iframe`、`html`、`media` 和图片轮播见 §14。

### 3.3 SearchResult

```ts
{ title: string, subtitle?: string, url?: string, icon?: string /*lucide 名*/, copy?: string }[]   // ≤ 8
// 有 url → 回车打开；无 url 有 copy → 回车复制到剪贴板（计算器等）
```

## 4. 数据模型（store）

`genkan.json` 新增（`SchemaVersion` 视需要提升并做迁移）：

```jsonc
"plugins": [
  { "id": "weather", "enabled": true, "config": { "unit": "c" }, "installed_at": "...", "updated_at": "...",
    "source_url": "" /* 从 URL 安装时记录，用于"检查更新" */ }
],
"widgets": [
  { "id": "<随机id>", "plugin": "weather", "widget": "current", "workspace_id": "" /* ""=所有工作区 */,
    "config": { "city": "Tokyo" }, "sort": 0 }
]
```

- 插件源码存 `data/plugins/<id>.js`（或 `.ts`），不放进 genkan.json。
- `Workspace.wallpaper` 新增合法形式 `plugin:<pluginId>/<sourceId>`。插件被删除时，引用它的工作区壁纸重置为 `DefaultWallpaper`；被禁用时保留值、前端回退显示默认壁纸。删除插件同时删除其小组件实例和 KV 文件。
- 导出（`GET /api/export`）包含 `plugins`（含 `source` 源码，**不含 secret 值**）与 `widgets`；导入时一并恢复。
- 所有改动走现有 `OnChange` → SSE `change` 事件，前端照常重新拉 bootstrap。

## 5. REST API

所有响应 JSON；错误 `{error}`。权限：admin = 管理员；read = 管理员或访客。

| 方法 & 路径 | 权限 | 说明 |
|---|---|---|
| `GET /api/bootstrap` | public | 新增 `plugins: PluginInfo[]`（访客也可见，但不含 config）、`widgets: WidgetInstance[]` |
| `GET /api/plugins` | admin | `PluginInfo[]`，含 `config`（secret 置空）、`secrets_set`、`source_url`、最近错误 |
| `POST /api/plugins` | admin | 安装或更新。body：`{ source: string, lang?: "js"\|"ts", enable?: bool }` 或 `{ url: string, enable?: bool }`（服务端下载，≤ 512 KiB）或 `{ catalog: "<id>" }`（从内置示例安装）。同 ID 已存在 → 更新源码（保留配置/实例）。返回 `PluginInfo` |
| `GET /api/plugins/catalog` | admin | 内置示例插件列表：`{ id, name, description, icon, version, installed: bool, widgets: { id, name, size }[] }[]`（`name`/`description` 与 `widgets[].name` 为 LocalizedString：字符串或 `{ lang: text }`；`widgets` 永不为 null，见 §15.5） |
| `GET /api/plugins/{id}/source` | admin | `{ source, lang }` |
| `PATCH /api/plugins/{id}` | admin | `{ enabled?: bool, config?: object }` |
| `DELETE /api/plugins/{id}` | admin | 卸载 |
| `GET /api/plugins/{id}/logs` | admin | `{ logs: { ts, level, msg }[] }` |
| `GET`/`DELETE /api/plugins/{id}/storage` | admin | 查看 / 清除插件存储，见 §13.3 |
| `POST /api/plugins/{id}/test` | admin | 试运行，供 AI/开发者调试：`{ hook: "wallpaper"\|"widget"\|"search"\|"schedule"\|"action", target?: string /* source/widget/schedule id */, query?, config?: object /* 实例配置 */, action?, params? }` → `{ ok, result, error?, logs, ms }`。不写缓存 |
| `POST /api/widgets` | admin | 新建实例 `{ plugin, widget, workspace_id?, config? }` |
| `PATCH /api/widgets/{id}` | admin | `{ workspace_id?, config?, sort? }` |
| `DELETE /api/widgets/{id}` | admin | |
| `POST /api/widgets/reorder` | admin | `{ ids: string[] }` |
| `GET /api/widgets/{id}/data` | read | `{ view: WidgetView, updated_at, next_at }`。服务端按 refresh 缓存；`?force=1` 仅 admin 生效，绕过缓存 |
| `POST /api/widgets/{id}/action` | admin | `{ action, params }` → `{ toast?, refresh?, view? }`；`view` 为 `{ view: WidgetView, updated_at, next_at }`（与 `/data` 同形），见 §15.3、§15.8。请求体 ≤ 64 KiB |
| `GET /api/plugins/wallpaper/{plugin}/{source}` | read | `{ url: "/uploads/plugin-wallpapers/<file>", title?, credit?, link?, updated_at }`，按 refresh 缓存 |
| `GET /api/plugins/search?q=` | read | 并发调用所有已启用的搜索插件（无前缀的仅当 q ≥ 2 字符），总时长 ≤ 2 s，返回 `{ groups: { plugin, name, icon, results: SearchResult[] }[] }`；超时/出错的插件被忽略 |
| `GET /uploads/plugin-wallpapers/{name}` | read | 复用现有 `/uploads/{kind}/{name}` |

`PluginInfo`（所有 LocalizedString 字段——`name`、`description`、`label`、`help`、选项 `label`、`widgets[].name`、`wallpapers[].name`、`schedules[].name`、`search.name`——原样返回字符串或 `{ lang: text }` 对象，由前端用 `loc()` 解析；`search` 接口的分组 `name` 是例外，服务端已按请求语言解析为字符串）：

```ts
{ id, name, version, description, author, homepage, icon, enabled: boolean,
  permissions: { fetch: string[], links: "" | "read" | "write" },
  config_schema: ConfigField[], wallpapers: [...], widgets: [...], search: {...} | null, schedules: [...],
  // 仅 admin：
  config?: object, secrets_set?: string[], source_url?: string, last_error?: string, installed_at?, updated_at? }
```

`WidgetInstance`：`{ id, plugin, widget, workspace_id, config, sort }`（访客看到的 config 中去掉实例 schema 里 secret 字段）。

## 6. MCP 工具与 Skill

新增 MCP 工具（风格与现有工具一致）：`list_plugins`、`install_plugin`（source 或 url 或 catalog）、`update_plugin`（enabled/config）、`uninstall_plugin`、`test_plugin`、`plugin_logs`、`plugin_storage`、`clear_plugin_storage`（§13.3）、`list_widgets`、`add_widget`、`update_widget`、`remove_widget`。
`skills/genkan/reference.md` 增加插件 API 一节，并说明插件编写要点（指向 `docs/PLUGINS.md`）。

## 7. 前端

1. **设置 → 插件 标签页**（admin）：
   - 已安装列表：图标、名称、版本、能力徽章（壁纸/小组件/搜索/定时）、启用开关、配置按钮、日志、删除。
   - 配置表单由 `config_schema` 自动生成（string/text/number/boolean/select/secret/url）。
   - "添加插件"：内置示例目录（一键安装）/ 从 URL 安装 / 粘贴源码安装。**安装前确认框列出权限**（fetch 主机、links 权限；`*` 用警告色）。
   - 显示最近错误与日志查看（简单 `<pre>` 列表即可）。
2. **小组件区**：位于 Hero（时钟+搜索）和 Board 之间，响应式网格，size s/m/l 分别占 1/2/3 列（手机上全部占满一行或 s 两列）。显示当前工作区的实例（`workspace_id` 为空或等于当前工作区）。毛玻璃卡片风格与现有一致。
   - 每个实例按 `refresh` 轮询 `GET /api/widgets/{id}/data`（页面不可见时暂停）；加载骨架、错误态。
   - 管理员编辑模式下：添加小组件（选择插件的 widget → 填实例配置 → 选择工作区）、配置、删除、拖拽或上下移动排序。
   - `button` 块调用 action 接口，显示 toast；`refresh: true` 时强制刷新。
3. **壁纸**：壁纸图库（Gallery）增加"插件壁纸"分组，列出已启用插件的 wallpaper 源；选中后壁纸值为 `plugin:<id>/<source>`。`Wallpaper.tsx` 遇到此值时请求 `/api/plugins/wallpaper/...` 获取图片 URL，按 refresh 定时刷新，平滑过渡沿用现有图层动画；有 `credit` 时在左下角显示小字署名（可点击 link）。插件未启用/出错 → 回退默认壁纸。
4. **搜索**：搜索框输入时（250 ms 防抖）请求 `/api/plugins/search`，结果作为分组插入在链接结果与"网页搜索"之间，参与键盘上下选择；`copy` 结果回车复制并 toast。没有启用搜索插件时不发请求（从 bootstrap 判断）。
5. i18n：所有新文案提供 zh/en；新增 `data-testid` 记录到 `web/TESTIDS.md`；`web/mock/server.mjs` 增加插件相关接口的模拟（含一个模拟天气小组件、一个模拟壁纸源、一个模拟计算器搜索），方便无后端开发。

## 8. 内置示例插件（`plugins/catalog/*.js`，embed 进二进制）

| ID | 能力 | 说明 |
|---|---|---|
| `bing-wallpaper` | 壁纸 | Bing 每日图片（`www.bing.com` HPImageArchive），配置：市场 mkt |
| `nasa-apod` | 壁纸 | NASA 每日天文图，配置：api_key（secret，默认 DEMO_KEY） |
| `picsum` | 壁纸 | picsum.photos 随机风景图 |
| `weather` | 小组件 | Open-Meteo（无需 key），实例配置：城市名（用 geocoding API 解析）、单位；显示当前温度、天气、未来几天 spark 图 |
| `hacker-news` | 小组件 | HN 热门 Top N 列表 |
| `rss` | 小组件 | 实例配置：feed url（allowFetch），解析 RSS/Atom（插件内手写简易 XML 解析），列表显示 |
| `world-clock` | 小组件 | 无网络；实例配置时区列表，用 `Intl.DateTimeFormat` |
| `countdown` | 小组件 | 无网络；倒计时到某日期 |
| `calculator` | 搜索 | 无网络；安全的四则运算/括号/幂/百分号解析器（不用 eval），结果 copy |
| `github-search` | 搜索 | prefix `gh`，调用 GitHub search repositories API |
| `pihole` | 小组件+动作 | 实例配置 Pi-hole 地址（url, allowFetch）与 API token（secret）；显示今日查询/拦截数/拦截率；按钮"暂停拦截 5 分钟"（action） |
| `uptime-summary` | 小组件 | `links: "read"`；汇总开启健康检查的链接数量（演示宿主 API） |
| `link-importer` | 定时任务 | `links: "write"`；配置一个 JSON URL（allowFetch），定时把其中 `{title,url,category}` 导入为链接（演示定时任务），已存在的 URL 跳过 |
| `github-stars` | 小组件+定时任务 | GitHub 星标增长折线图（`chart` 的 `line`、实例级配置、`genkan.widgets.list()`、插件存储），见 §13.6 |
| `media` | 小组件 | 网页 / 网页片段 / 图片轮播 / 视频 / 音频，见 §14.5 |
| `todo` | 小组件+动作 | 待办清单（`input`、可勾选/编辑/排序的 `list`、实例存储），首次启动自动安装，见 §15.6 |
| `note` | 小组件+动作 | Markdown 便签（`markdown` 块就地编辑、`tone` 底色），首次启动自动安装，见 §15.6 |

每个示例都要短小、注释清晰，作为插件开发者的范例。

## 9. 测试与验收

- Go：`go vet ./... && go test -race ./...` 通过。覆盖：manifest 校验、各钩子调用、超时中断、内存超限、fetch 白名单拒绝（用 httptest 服务器）、storage 上限、secret 不外泄、WidgetView 清洗、壁纸下载缓存与清理、REST 权限（访客/未登录）、导出导入、删除插件的级联清理、**所有内置示例插件均能编译且 manifest 校验通过**、无网络的示例（calculator/world-clock/countdown）实际运行结果正确。
- 前端：`npm run build` 通过（tsc + vite），gzip 体积不超标。
- e2e：新增 `e2e/specs/20-plugins.spec.ts`：安装内置示例（calculator、world-clock、countdown，均无外网依赖）、添加小组件并看到渲染、搜索得到计算结果、禁用后消失、访客可见但无管理按钮。
- 文档：README（中英）功能列表加一行；`docs/SPEC.md` 加指向本文的一节；`web/TESTIDS.md`、`skills/genkan/reference.md` 同步。

## 10. 后端实现补充（对契约的澄清与小幅调整）

以下是后端实现时确定的细节；JSON 形状与上文一致，仅有加粗标注的新增字段。

**时间与语言**
- 运行时产生的时间戳一律为 **Unix 毫秒整数**：`/data` 的 `updated_at`、`next_at`，壁纸的 `updated_at`，日志的 `ts`。（`PluginInfo.installed_at/updated_at` 仍是 RFC3339 字符串。）
- `ctx.lang`：优先 `?lang=zh|en`，否则 `Accept-Language`（`zh*` → `zh`，其余 `en`）；定时任务与未带语言的 test 为 `en`。

**错误状态码**
- 插件钩子执行失败（异常、超时、内存超限、结果无效）：`502 {error}`，超时 `504`；`GET /api/widgets/{id}/data` 与壁纸接口失败后 15 秒内复用该错误（不重复执行），`?force=1`（仅管理员）除外。
- 插件被禁用 → `409`；插件/小组件/壁纸源不存在 → `404`；校验失败 → `400`。

**安装**
- `POST /api/plugins`：`enable` 省略时新装插件默认**启用**，更新已存在插件则保持原状态；`enable:false` 会把已存在的插件也置为禁用。校验失败返回 400，message 指出具体字段。
- 同一时间最多 50 个插件、200 个小组件实例；源码 ≤ 512 KiB；插件文件不得 `import`。
- manifest 未知的顶层字段被忽略；`permissions.fetch` 可以为空数组（主机仅来自 `allowFetch`）。`permissions.fetch` 项可写 `host`、`host:port`、`*.suffix`、`*`。

**配置**
- REST `PATCH /api/plugins/{id}`、`PATCH /api/widgets/{id}` 的 `config` 是**整体替换**（省略的非 secret 字段回到默认值，空字符串等同于清除）；secret：`""`/省略 = 保持，`null` = 清除。MCP 的 `update_plugin` / `update_widget` 的 `config` 则是**合并**（null 删除该键）。
- `WidgetInstance` 新增可选字段 **`secrets_set: string[]`**（仅管理员；实例 schema 里有值的 secret 字段名）。管理员看到的实例 `config` 中 secret 值为 `""`，访客的 `config` 中没有 secret 键。
- `allowFetch` 的 url 字段把 `host:port` 加入白名单（URL 未写端口时为 80/443）。仅这些主机在 HTTPS 下**不校验证书**（自签证书的内网服务）；manifest 声明的主机始终校验证书。
- secret 值会从插件日志、`last_error`、test 结果与错误信息中以 `***` 替换；`default` 对 secret 字段允许（manifest 内容是公开的）。

**其它**
- 宿主 API 另提供 `console.log/info/debug/warn/error`（写入插件日志）。
- test 请求可选 **`instance`**（已保存的小组件实例 id，作为配置基底，保留其 secret）和 `lang`。`result` 是清洗后的值；壁纸 test 不下载图片。
- 壁纸文件名为 `<插件前缀8位>-<源前缀6位>-<内容哈希16位>.<ext>`（前缀由插件 id/源 id 的 sha256 得到，用于按源保留最近 5 张并在卸载时清理），仍由 `/uploads/plugin-wallpapers/{name}` 提供。
- 搜索：结果为空的分组不返回；无前缀插件需 `q` ≥ 2 个字符；带前缀的需 `q` 以 `<prefix> ` 开头且去前缀后非空。
- 小组件按钮 `action` 名需匹配 `^[A-Za-z0-9_.:-]{1,40}$`；`params` 序列化后 ≤ 2 KiB（action 请求的 `params` ≤ 8 KiB，`params.value` 另算，见 §15.3）。
- 导出 `schema` 升为 **2**（旧文件无需迁移，可直接读取；旧版本程序会拒绝读取 schema 2 的数据文件）。导出中仅在存在插件/小组件时才出现 `plugins`、`widgets` 键；`plugins[]` 为 `PluginRecord + source + lang`，secret 值被去除（无法解析 manifest 的插件导出时不含 config）。导入前每个插件源码都会被编译校验，manifest id 必须与记录一致；`merge` 跳过已存在的插件 id；`POST /api/import` 响应新增 `plugins`、`widgets` 计数。
- 数据文件里的 `plugins[]` 记录多一个 `lang`（`js`|`ts`）字段。
- 运行时池：每次调用独占一个运行时，成功结束的运行时放回池（每插件最多 2 个空闲，5 分钟无调用释放）；超时、出错或钩子完成后仍有残留定时器的运行时直接丢弃。插件不应依赖 JS 全局变量保存状态（请用 `genkan.storage`）。
- Docker 镜像是 `FROM scratch`：二进制内置了 `time/tzdata`（约 450 KB），world-clock 才能使用 IANA 时区。

## 11. 第二轮需求（v2）

### 11.1 小组件布局（拖拽、自由尺寸、紧凑排列）

- `WidgetInstance` 新增 **`w`**（列宽，整数 0–6；0 = 按 manifest `size` 取默认：s→2、m→4、l→6）与 **`h`**（高度，像素整数；0 = 自动按内容高度；否则 80–1200）。`POST /api/widgets`、`PATCH /api/widgets/{id}`、MCP `add_widget`/`update_widget`、导出导入都支持这两个字段；越界值钳制到范围内。
- 前端网格：桌面 6 列；≤ 1100px 4 列；≤ 700px 2 列；≤ 480px 1 列。窄屏下 `w` 按比例换算并钳制（`ceil(w * cols / 6)`）。采用"瀑布流"紧凑排列：`grid-auto-flow: row dense` + 细粒度行单位（例如 8px 行 + 行间距），每张卡片 row-span 由 `h`（固定高度）或 ResizeObserver 测得的内容高度（`h=0`）计算，消除卡片下方空洞。固定高度时内容超出可在卡片内滚动。
- 编辑模式：
  - "添加小组件"卡片放在**最前面**。
  - 拖拽卡片改变顺序（Pointer Events，参考 `dnd.ts` 现有实现，支持触屏），松手后 `POST /api/widgets/reorder`。
  - 卡片右下角有缩放手柄：横向按列吸附、纵向按行单位吸附，拖动时实时预览，松手 `PATCH {w,h}`。双击手柄 = 恢复自动高度（`h:0`）。
- 小组件**右键菜单**（复用 `ContextMenu.tsx`；触屏长按）：刷新、打开链接（view 有 link 时）、配置、尺寸（小/中/大/整行 + 自动高度）、显示范围（当前工作区 / 所有工作区）、删除。访客只有"刷新""打开链接"。
- **页面右键菜单**新增"添加小组件"（管理员）。
- 顶栏右上角：用**插件图标**（`puzzle`）替换 AI 图标，点击打开 设置→插件；AI 入口移入设置按钮的下拉菜单（MainMenu，位于"设置"上方）。

### 11.2 源码查看/编辑与安装前预览

- `PUT /api/plugins/{id}/source`（admin）：`{ source, lang? }` 更新已安装插件源码；manifest.id 必须等于 `{id}`；保留启用状态、配置与实例；返回 `PluginInfo`；校验失败 400，错误信息尽量带行列号。
- `POST /api/plugins/preview`（admin）：body 与安装相同（`{source,lang}` | `{url}` | `{catalog}`），**不安装**，返回
  `{ plugin: PluginInfo | null /* 仅 manifest 派生字段 */, source, lang, installed: bool, installed_version?: string, error?: string }`。用于安装前查看源码与权限。
- 前端：
  - 已安装插件行增加"源码"按钮 → 源码编辑器弹窗（textarea + 行号栏，等宽字体，Tab 插入两个空格，Ctrl/⌘+S 保存；无新依赖，懒加载）。保存调用 `PUT .../source`，显示错误；"试运行"面板（选择钩子/目标，调用 `/test`，展示 JSON 结果、错误、日志、耗时）**始终显示在源码上方**，布局紧凑，源码编辑器仍占据大部分高度。
  - **语法高亮**：可编辑编辑器与所有只读源码视图都做 JS/TS 高亮。实现是自带的小型分词器（`web/src/highlight.ts`，无依赖，仅在懒加载块中）：关键字、字符串、模板字符串（`${}` 内按代码处理）、注释、数字、正则字面量（按前一个有效 token 判断）、`true/false/null/undefined`、属性名/函数名、标点。可编辑模式使用"透明 textarea 叠在高亮 `<pre>` 上"的技术（字体、字号、行高、内边距、tab 宽度、不换行与滚动位置严格同步）；token 以转义文本节点/`<span>` 渲染，插件源码从不作为 HTML 注入。
  - 安装流程（URL、粘贴、第三方）提供"预览源码"按钮（默认折叠不展示源码），权限确认弹窗中也有该按钮。内置目录的每一项在名称/版本号之后有一个小的 `code` 图标按钮（`data-testid="plugin-catalog-source"`），点击打开对话框：插件信息（名称、版本、描述、作者、能力、权限、配置项）+ 高亮的只读源码（`POST /api/plugins/preview {catalog}`）。
  - 添加插件的标签顺序：内置目录、第三方、URL、粘贴。

### 11.3 AI 开发插件

- 插件开发指南 **`skills/genkan/plugin-dev.md`**（英文为主，面向 AI 编码助手；属于 Skill 的一部分，见 §15.7）：插件开发完整指南——文件格式、manifest、ctx、宿主 API、返回值、限制、通过 REST 的"编写 → 安装(`enable:false`) → test → 看日志 → 修改 → 启用 → 添加小组件"闭环、调试技巧、常见错误、完整示例、发布到 GitHub 的约定（11.4）。GitHub 地址：`https://github.com/jae-jae/genkan/blob/main/skills/genkan/plugin-dev.md`，原始文件：`https://raw.githubusercontent.com/jae-jae/genkan/main/skills/genkan/plugin-dev.md`。Skill 的 `SKILL.md` 与 `reference.md` 指向它（§15.7）。
- 插件页"添加插件"旁边新增"用 AI 开发"按钮 → 弹窗两步：① 先到 设置→AI 安装或更新 Genkan Skill（带跳转按钮）；② 复制一句短提示词（中/英随界面语言）并在占位处写上需求，发给 AI。指南和完整流程都在 Skill 里（§15.7）。

### 11.4 第三方插件（GitHub）

- 约定：仓库加 topic **`genkan-plugin`**。插件文件为仓库内任意 `.js`/`.ts`（≤ 512 KiB，含 `export const manifest`），扫描时跳过 `node_modules/`、`.github/`、`test(s)/`、`dist/` 以外的隐藏目录；若仓库根目录有 `genkan-plugins.json`（`{ "plugins": ["path/a.js", ...] }`）则只用其中列出的文件。最多 40 个候选文件。
- `GET /api/plugins/market?sort=stars|updated&q=&page=`（admin）：服务端调用 GitHub Search API（`topic:genkan-plugin` + 可选关键字），缓存 10 分钟。返回 `{ items: { repo, description, stars, updated_at, html_url, owner_avatar, default_branch }[], total, cached_at, error? }`。
- `GET /api/plugins/market/repo?repo=owner/name`（admin）：扫描仓库（git trees API + raw.githubusercontent.com，并发 ≤ 4），对每个候选文件编译并校验 manifest，缓存 10 分钟。返回 `{ repo, default_branch, plugins: { path, url /* raw URL，作为安装用的 source_url */, plugin: PluginInfo | null, error?, installed: bool, installed_version?: string }[] }`。
- 安装沿用 `POST /api/plugins {url}`（因此记录 `source_url`，可"检查更新"）。前端支持勾选多个/全部安装，逐个安装并汇总结果；安装前同样走权限确认（可合并为一次确认，列出所有插件的权限）。
- 可选环境变量 `GENKAN_GITHUB_TOKEN` 提高 GitHub API 限额。只有打开"第三方"标签时服务端才访问 GitHub；GitHub 不可达时返回 `error` 而非 5xx 页面。
- 前端：添加插件弹窗新增"第三方"标签：搜索框、排序（Star / 最近更新）、仓库卡片（头像、名称、描述、star、更新时间、GitHub 链接）、"查看插件"→ 展开扫描结果列表（复选框、名称、能力徽章、版本、已安装状态、预览源码）→"安装选中 / 全部安装"。显著标注"第三方插件未经审核，请检查权限和源码"。

### 11.5 后端实现补充（v2）

JSON 形状与 11.1–11.4 一致，以下为实现时确定的细节。

**小组件布局**：`w`、`h` 始终出现在 `WidgetInstance` JSON 中（不省略 0）。`w` 钳制到 0–6；`h` ≤ 0 → 0（自动），1–79 → 80，> 1200 → 1200。`PATCH` 只改传入的字段（省略的保持原值）；旧数据文件/导入文件中缺失时视为 0/0，导入时同样钳制。MCP `add_widget`/`update_widget` 接受 `w`、`h`，`list_widgets` 返回它们。

**`PUT /api/plugins/{id}/source`**：`lang` 省略时沿用该插件当前语言（可切换 js↔ts，旧文件被删除）；保留启用状态、配置、实例与 `source_url`；失败时磁盘与内存状态都不变。语法错误信息形如 `invalid plugin: compile error: plugin.js:3:8: SyntaxError: ...`（TS 为 `plugin.ts`）。manifest.id 不一致 → 400；插件不存在 → 404。即使已安装的插件源码损坏（`last_error`），也可以通过它修复。

**`POST /api/plugins/preview`**：请求形状错误（不是恰好一个来源、`lang` 非法、源码 > 512 KiB、URL 非 http(s)）→ 400，内置目录项不存在 → 404；URL 下载失败 → 200，`error` 为下载错误，`plugin: null`，`source: ""`；源码无法通过校验 → 200，`plugin: null`，`error` 为 `invalid plugin: ...`（带行列号），仍返回 `source`/`lang`。`plugin` 是 `PluginInfo` 的 manifest 派生部分（`enabled` 恒为 `false`，没有 `config`/`secrets_set`/`source_url`/`last_error`/`installed_at`/`updated_at`）。`installed_version` 仅在 `installed: true` 且已安装版本可读时出现。不写文件、不写 store、不运行任何钩子（与安装一样，会在无网络/无存储的探测运行时里执行模块顶层以读取 manifest）。

**第三方市场**：
- 时间戳 `cached_at` 为 Unix 毫秒（与 §10 一致）；`items[].updated_at` 为 GitHub 返回的 RFC3339 字符串。
- `GET /api/plugins/market`：`sort` 只接受 `stars`（默认）/`updated`，否则 400；`page` 非数字 → 400，范围钳制到 1–30；每页 20；`q` 去除控制字符、合并空白、截断到 100 字节。GitHub 失败（限流、网络、非 200）→ **200**，`items: []`、`total: 0`、`error` 为可读信息（限流时提示设置 `GENKAN_GITHUB_TOKEN`）。
- `GET /api/plugins/market/repo`：`repo` 不符合 `^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`（或某段为 `.`/`..`）→ 400。GitHub 失败 → 200，`plugins: []`、`error` 非空。响应额外含 `cached_at` 和（失败时）`error`。`installed`/`installed_version` 每次请求按当前已安装插件实时计算（扫描结果本身才缓存）。
- 扫描：默认分支取自 `GET /repos/{o}/{r}`；先读根目录 `genkan-plugins.json`（存在则只用其中列出的文件，路径必须是安全的相对路径、扩展名 `.js`/`.ts`，列出的文件即使无法通过校验也会以 `error` 条目返回）；否则用 git trees API（`recursive=1`）列出 `.js`/`.ts` 文件，跳过 `.d.ts`、`.min.js`、> 512 KiB、任一目录段是 `node_modules`/`test`/`tests`/`dist`/`__tests__` 或以 `.` 开头的路径，按目录深度再按路径排序，最多取 40 个。自动扫描模式下内容里不同时含 `manifest` 与 `export` 的文件被静默忽略（不是插件，如普通脚本）；含有的文件如编译/校验失败则返回 `plugin: null` 与 `error`。下载并发 ≤ 4。
- 缓存 10 分钟（成功），失败结果缓存 30 秒；缓存最多 128 项；并发相同请求合并为一次。令牌只发送给 API 主机，不发送给 raw 主机，并从错误信息中抹除。
- `POST /api/plugins/market/cache/clear`（admin，与其他写操作一样，Cookie 会话需要 `X-Requested-With: genkan` 头）：清空搜索与仓库扫描两类缓存（含失败结果的短缓存），返回 `{ "ok": true }`；清空时已在进行中的 GitHub 请求即使稍后完成也不会再写回缓存。随后的 `GET` 会重新访问 GitHub。前端"第三方"标签的"清除缓存"按钮调用它，然后重新拉取当前搜索以及已展开的仓库扫描，并弹出提示。
- 配置：环境变量 `GENKAN_GITHUB_TOKEN`；`plugin.Config`/`server.Config` 的 `GitHubToken`、`GitHubAPI`、`GitHubRaw`、`GitHubClient`（后三者供测试覆盖）。

**MCP**：新增 `preview_plugin`、`get_plugin_source`、`update_plugin_source`（共 26 个工具）。`preview_plugin` 在 `error` 非空时以 `isError` 返回结构化结果。MCP 不提供市场工具（第三方浏览只在 UI/REST 中）。

## 12. 本地化（i18n）

插件的可见文字分两类，各有一个机制：**运行时文字**（钩子返回的内容）用 `messages` 表和 `ctx.t`；**清单文字**（名称、描述、配置项标签……）用 LocalizedString。两者可以共用同一张 `messages` 表：清单里写 `t("key")`。

### 12.1 `messages` 表与 `ctx.t`

```js
export const messages = {
  en: { feels: "Feels like", next7: "Next {n} days high" },
  zh: { feels: "体感",       next7: "未来 {n} 天最高温" },
}
export async function widget(ctx) {
  const { t } = ctx
  return { body: [{ type: "text", text: t("next7", { n: 7 }) }] }
}
```

- `messages` 可选。存在时必须是 `{ 语言标签: { key: 文本 } }`。校验（失败则安装被拒绝，错误信息带路径）：
  - 语言标签形如 BCP-47（`en`、`zh-CN`、`zh-Hant-TW`、`es-419`，正则 `^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8}){0,3}$`，≤ 35 字符），大小写不敏感且不可重复（`zh-CN` 与 `zh-cn` 冲突）。
  - 每个值必须是字符串。
  - 上限：**最多 32 种语言**；**每种语言最多 500 个 key**；key 长 1–64 字符，不含控制字符；文本 ≤ 2000 字符，不含除 `\n`、`\t` 以外的控制字符；整张表序列化后 ≤ 128 KiB。
- `ctx.t(key, params?)`：
  - **插值**：`{name}` 用 `params.name` 替换（数字、布尔、字符串按 JavaScript `String()` 的方式转文本，对象/数组为 JSON）。参数缺失时占位符原样保留。参数值只替换一次，不会被再次展开。
  - **转义**：`{{` 得到字面量 `{`，`}}` 得到 `}`。
  - **回退链**（按 key 逐级查找）：完全匹配的语言 → 基础语言（`zh-TW` → `zh`；`zh-Hant-TW` → `zh-Hant` → `zh`）→ `en` → 按字母序第一张表 → key 本身。语言标签大小写不敏感。找不到 key 绝不抛错，返回 key 原文（不插值）。
  - `key` 不是字符串时按空字符串处理；`params` 不是对象时忽略。
  - `ctx.t` 在 `wallpaper`、`widget`、`search`、`action`、`schedule` 以及 `/test` 中都可用，语言即 `ctx.lang`（定时任务为 `en`）。
- `ctx.lang`：请求方语言的基础语言。服务端不把语言限制在 zh/en：`Accept-Language`/`?lang=` 的第一项取其基础语言子标签（`fr-CA` → `fr`，无法识别则 `en`）。当前界面只有 zh/en，但插件应按“任意语言”来写（让 `t` 的回退链处理）。
- `ctx.locale`：给 `Intl`、`toLocaleString` 的 BCP-47 标签。带地区的语言原样使用；`zh` → `zh-CN`，`en` → `en-US`，另有 `ja-JP`、`ko-KR`、`de-DE`、`fr-FR`、`es-ES`、`it-IT`、`pt-BR`、`ru-RU`；其它语言用语言码本身。

### 12.2 清单里的 LocalizedString

下列字段都可以是**普通字符串**、**`{ 语言: 文本 }` 对象**，或 **`t("key")`**：`name`、`description`、配置项（插件级与小组件实例级）的 `label`/`help`、`select` 选项的 `label`、`widgets[].name`、`wallpapers[].name`、`schedules[].name`、`search.name`。

```js
name: "Weather"                          // 普通字符串（兼容旧插件）
name: { en: "Weather", zh: "天气" }       // 内联对象
name: t("name")                          // 引用 messages 表
```

校验：对象至少有一种语言，最多 32 种；语言标签同上；每个值是非空字符串（去首尾空白后），长度不超过该字段原有的限制（name 40、description 300、label 60、help 200、选项 label 100），无控制字符。`en` 推荐但不是必须。普通字符串的规则不变。

**清单里的 `t(key)`**：

- 全局 `t` 只在模块求值期间可用（也就是写 `manifest` 的时候）。它返回一个惰性标记 `{ "$t": key }`；安装/预览时，服务端用 `messages` 把每个标记展开为 `{ 语言: messages[语言][key] }`，只包含有该 key 的语言。之后走与内联对象完全相同的 LocalizedString 校验。**前端和 API 永远看不到标记**。
- 清单里的 `t` 不带参数；多传的参数被忽略（`t("w", { n: 1 })` 得到的仍是模板 `Widget {n}`）。
- key 在所有语言里都不存在，或没有导出 `messages` → 校验失败，错误信息给出字段路径和 key，例如 ``manifest.widgets[0].name: t("zzz") is not defined in any language of `messages` ``。
- 在钩子里调用全局 `t` 会**抛出错误**（`t() only works while the module is evaluated … use ctx.t(key, params)`），而不是悄悄返回标记。钩子里要用 `ctx.t`；在函数里写 `const { t } = ctx` 会遮蔽全局 `t`，这是推荐写法。

### 12.3 线路格式与解析

- API 把 LocalizedString 原样返回：字符串或对象（PluginInfo、目录、预览、市场扫描、bootstrap、MCP 工具结果都一样）。键的顺序按字母序。
- 前端用同一个小函数 `loc(v)`（`web/src/i18n.ts`）按当前界面语言解析，回退链与 `ctx.t` 相同：完全匹配 → 基础语言 → `en` → 字母序第一种语言。因此在设置里切换语言，插件名称、描述、配置表单、小组件选择器、壁纸库立即更新；小组件的文字由服务端按请求语言生成，切换语言后会重新拉取。
- 需要单个字符串的服务端位置按请求语言解析：搜索接口分组的 `name`、配置校验错误里的字段名（`城市 is required`）。日志和插件错误信息不翻译。
- 小组件数据缓存按语言分别保存；搜索结果同样按语言区分。壁纸结果按来源缓存、与语言无关（避免不同语言的访问者得到不同的图片），所以壁纸的 `title`/`credit` 使用第一次请求时的语言。

## 13. 存储增强、折线图与 `github-stars`

### 13.1 实例级存储 `ctx.instance.storage`

`widget` 与 `action` 钩子的 `ctx.instance` 多一个 `storage`，API 与 `genkan.storage` 完全相同（`get`/`set`/`delete`/`keys`），但只属于**当前小组件实例**：两个实例互不可见，`keys()` 只列出本实例的键。

- 与插件级存储共用同一个文件 `data/plugins/<id>.kv.json` 和同一个 **256 KiB** 上限（键名与值的总和，含内部前缀）。
- 内部命名空间是键前缀 `@i/<实例 id>/`：插件级 `genkan.storage` 看不到、读不到、也不能写以 `@i/` 开头的键（`set` 抛错；`keys()` 不列出）。
- 删除小组件实例会清除它的存储：REST `DELETE /api/widgets/{id}`、MCP `remove_widget`、UI 删除、**级联**（删除工作空间、卸载插件）和"替换导入"里不再存在的实例，全部走同一个 store 回调（`Store.SetWidgetsGone`，在锁外调用 `Manager.clearInstances`）。
- `/test` 试运行未保存的实例（`instance` 为 `"test"`）时，`ctx.instance.storage` 是一次性的内存存储，不落盘；带 `instance` 参数试运行已保存的实例则读写真实数据。
- `schedule`、`search`、`wallpaper` 没有 `ctx.instance`；要在定时任务里按实例处理，用 §13.2。

### 13.2 `genkan.widgets.list()`

返回**本插件自己的**小组件实例：`[{ id, widget, workspace_id, config }]`。`config` 已套用清单里的默认值，**包含 secret 明文**（它们属于这个插件）。同步调用，无需任何 `permissions`；永远不会返回其它插件的实例（门控就是"只看自己"）。探测运行时（只读清单）调用会失败，与其它宿主 API 相同。因为它把所有实例的 secret 交给了插件，这些值同样会从日志和错误信息里被脱敏。

典型用途：`schedule` 钩子要为所有实例准备数据（`github-stars` 的每小时采样）。

### 13.3 存储的查看与清除

| 方法 & 路径 | 权限 | 说明 |
|---|---|---|
| `GET /api/plugins/{id}/storage` | admin | `{ size, limit, entries: [{ key, size, value, instance? }] }`。`size`/`limit` 单位字节（`limit` = 262144）；`entries[].size` = 内部键 + JSON 值的长度（各项之和即 `size`）；`instance` 仅实例级存储的键有。排序：插件级键在前，然后按实例 id、键名 |
| `DELETE /api/plugins/{id}/storage` | admin | 清空全部（含各实例的存储） |
| `DELETE /api/plugins/{id}/storage?key=K` | admin | 删除插件级键 K；加 `&instance=<实例 id>` 删除该实例的键 K。只给 `instance` 不给 `key` → 400。键不存在不是错误 |

清除后会丢弃该插件各小组件的缓存视图。MCP 工具：`plugin_storage {id}`（同 GET）与 `clear_plugin_storage {id, key?, instance?}`（同 DELETE；破坏性）。

UI：设置 → 插件，每个已安装插件有一个"数据 / Data"按钮（数据库图标，`data-testid="plugin-data-btn"`），打开懒加载对话框（`PluginData.tsx`，独立分块）：用量条（`plugin-data-usage`）、键列表（`plugin-data-row`，带大小、实例徽标）、可展开的格式化 JSON 值、逐键删除，以及"全部清除"（对话框内二次确认）。

### 13.4 导出/导入插件数据（可选）

- `GET /api/export?plugin_data=1`：在导出 JSON 里多一个 `plugin_data: { "<pluginId>": { "<key>": value, … } }`，含各实例的命名空间（键名就是内部名 `@i/<实例 id>/<key>`）。只包含有数据的插件。**默认导出不变**（没有 `plugin_data` 键）。
- `POST /api/import`：文件里有 `plugin_data`（键存在，哪怕是 `{}`）时才还原；没有则完全不动存储。
  - **replace**：被导入的每个插件的存储都被替换为 `plugin_data` 里的内容（没有条目的插件被清空）。
  - **merge**：只还原这次**新增**的插件；已安装的插件不动。merge 给小组件分配新 id，实例级的键随之改名；指向未导入实例（或属于别的插件）的键被丢弃。
  - 导入前校验：插件 id、键长、每个插件总量 ≤ 256 KiB，不合格返回 400 且不导入任何内容。
- UI：设置 → 数据 有一个默认关闭的复选框"包含插件数据 / Include plugin data"，并提示插件数据可能含缓存的敏感数据；勾选后导出链接带 `?plugin_data=1`。

### 13.5 `chart` 的 `line` 类型（时间序列折线图）

```ts
{ type: "chart", kind: "line",
  points: [number /* 时间戳 ms */, number /* 值 */][],     // 至多 500 个点
  format?: "int" | "compact" | "percent" | "decimal",     // 默认 "int"
  color?:  "accent" | "green" | "red" | "blue" | "orange" // 默认 "accent" }
```

- **清洗**：非 `[有限数, 有限数]` 的点、时间戳 ≤ 0 的点被丢弃；只取前 500 个有效点；**按时间升序排序**（时间戳取整，相同时间戳的点在前端只保留最后一个）；没有有效点则丢弃整个块；未知的 `format`/`color` 被忽略。`spark`/`bar` 不变（`values` ≤ 60）。
- **渲染**（`web/src/LineChart.tsx`，内联 SVG，无依赖，在懒加载的 Widgets 分块里）：随容器宽度自适应（ResizeObserver）；单调三次曲线 + 渐变面积；y 轴 3–4 个"整齐"刻度，一律是紧凑写法（`1.2k`，中文界面 `1.2万`），`percent` 带 `%`；x 轴按跨度选刻度：≤ 2 天按小时、≤ 75 天按天、更长按月（≥ 14 个月带两位年份），日期用界面语言本地化；鼠标悬停/触摸出现十字线、圆点和提示（日期 + 值，跨度 ≤ 14 天时带时间）；没有数据时显示"暂无数据"。
- `format` 决定**提示里的值**：`int` 整数加千分位（`12,345`），`compact` 紧凑（`12.35k`），`decimal` 固定两位小数，`percent` 的值本身就是百分数（`42.5` → `42.5%`，不是 0..1）。

### 13.6 内置插件 `github-stars`

GitHub 星标增长折线图（`plugins/catalog/github-stars.js`）。GitHub 没有星标历史接口，所以采用混合方案：

- **配置**：插件级 `token`（secret，可选；**回填历史需要它**，并把速率限制从 60 提高到 5000 次/小时）；小组件 `trend` 的实例配置 `repo`（`owner/name`，必填；也接受 `github.com/o/r`、`https://github.com/o/r.git`，无效时返回本地化 `error`）和 `range`（`30d` / `90d` / `1y` / `all`，默认 `90d`）。`permissions.fetch: ["api.github.com"]`。
- **自采样**：定时任务 `sample`（每 3600 s）用 `genkan.widgets.list()` 收集不同的仓库，请求 `GET /repos/{o}/{r}` 取 `stargazers_count`，追加 `[now, count]` 到插件存储 `series:{owner/name}`（键小写）。小组件钩子在最新点超过 1 小时时也会自己取一次，所以新建的小组件不用等第一次定时任务。
- **一次性回填**（**需要 token**：GitHub 对未登录的 stargazers 请求返回 401 `Requires authentication`，实测如此；没有 token 时插件不会发这些请求，只做自采样，图表从现在开始积累，小组件里提示"添加 GitHub 令牌即可回填更早的历史"，之后配置了 token 就会自动开始回填）：第一次见到某个仓库（小组件钩子或定时任务）时，用 `GET /repos/{o}/{r}/stargazers?per_page=100&page=N`（`Accept: application/vnd.github.star+json`）在 `ceil(stars/100)` 页（上限 400 页，即 GitHub 的 4 万星限制）里均匀取约 10 页；每页得到一个点 `(第一条的 starred_at, (页-1)*100+1)`，再加当前总数作为最后一点。超过 4 万星的仓库，小组件里提示"在开始自采样之前的历史是近似值"。遇到 403/429（或剩余额度 ≤ 2）就停止，进度（`meta:{repo}.todo` 剩余页）已保存，下一次运行继续。小组件钩子里的回填有 5 秒预算。
- **紧凑存储**：`series:{repo}` = `{ t, c, d }`，`t`/`c` 是第一个点的时间（Unix 分钟）与星数，`d` 是 `[[Δ分钟, Δ星数], …]`。压缩规则：最近 7 天保留所有点（每小时一个）、之前每天一个、满一年之后每周一个。2 年以上历史的一个仓库约 600 个点、约 5 KB；**20 个仓库约 105 KB**（Go 测试 `TestStarsTwentyReposFitInStorage` 断言 < 128 KiB），所以大约 40 个仓库以内都在 256 KiB 之内。没有任何实例使用的仓库，其 `series:`/`meta:` 在最后一次被使用 30 天后删除。
- **小组件**：标题是仓库名（链接到 GitHub）；`stat` 当前星数（趋势 = 近 7 天）；`stats` 近 7 天、近 30 天增量；`line` 图表（所选范围，起点按插值补在范围起点上，超过 480 点时均匀抽稀）；少于 2 个点显示"正在收集数据…"；刷新失败但有旧数据时仍显示并附"刷新失败：原因"。错误（仓库不存在、速率限制、令牌无效、网络）返回本地化的 `error`。
- 并发：定时任务与小组件钩子可能同时读写同一个系列；插件在没有 `await` 的同步段里"读-改-写"，最坏丢一个点。

## 14. 媒体与嵌入：`iframe` / `html` / `media` 块、图片轮播、`genkan.html.select`

本节扩展 §3.2 的块类型与 §2.1 的宿主 API，并给出内置插件 `media`（`plugins/catalog/media.js`）。所有块仍然先经服务端清洗、前端再防御性清洗；未知字段静默丢弃。新增的渲染器都在懒加载的 Widgets 分块里，不增加首屏 JS。

### 14.1 新增与扩展的块

```ts
type Block =
  | { type: "iframe", src: string,              // 浏览器直接加载；仅 http(s)
      height?: number,                          // px，默认 360，夹在 120–1600
      ratio?: number,                           // 宽/高（0.2–5）；设置后用宽高比代替固定高度，适合视频
      crop?: { x?: number, y?: number, width: number, height: number },  // 只显示页面的这块矩形（页面像素）
      zoom?: number,                            // 0.25–2，默认 1
      refresh?: number }                        // 秒，≥ 5；0/省略 = 不自动刷新
  | { type: "html", html: string, base?: string }          // 经白名单清洗的 HTML 片段
  | { type: "media", kind: "video"|"audio", src: string, poster?: string, title?: string,
      autoplay?: boolean, muted?: boolean, loop?: boolean, controls?: boolean /* 默认 true */ }
  | { type: "image", images: { src: string, alt?: string, href?: string }[],   // 轮播，≤ 20 张
      interval?: number /* 秒，≥ 3，默认 6 */, fit?: "cover"|"contain", height?: number /* px，60–1000，默认 16:9 */ }
  // 原有的单图写法 { type: "image", src, alt?, href?, ratio? } 保持不变，也可加 fit / height
```

**`iframe`**
- `src` 只能是 http(s) URL（≤ 2048）。服务端无法知道 Genkan 页面自己的来源，所以**同源检查在前端渲染时做**：`src` 的 origin 与 `location.origin` 相同就拒绝渲染，改显示一条错误（`data-testid="widget-iframe-blocked"`）。
- 渲染为 `<iframe sandbox="allow-scripts allow-same-origin allow-forms allow-popups allow-popups-to-escape-sandbox" referrerpolicy="no-referrer" loading="lazy" allow="fullscreen; autoplay; picture-in-picture">`。`allow-scripts` 与 `allow-same-origin` 同时给出时，**同源页面可以拆掉自己的沙箱**，只有因为强制了跨源才是安全的（见 14.4）。没有 `allow-top-navigation`：被嵌入的页面不能跳转 Genkan。
- 被嵌入的网站可能用 `X-Frame-Options` / CSP `frame-ancestors` 拒绝嵌入，而这在浏览器里**无法检测**，所以框下方始终有一行低调的提示和“在新标签页打开”链接。
- `crop` 与 `zoom`：框外面是 `overflow:hidden` 的定长容器，iframe 按 `-x/-y` 偏移并用 CSS `transform: scale(zoom)` 缩放，只露出该区域。有 `crop` 时容器高度取自区域（`crop.height × 缩放`），`height` 被忽略；卡片比区域窄时缩放自动变小以显示完整宽度。没有 `crop` 时 `zoom < 1` 表示显示页面更多内容。
- `refresh`：每隔 N 秒重新加载 iframe（不触发插件钩子）。
- 编辑模式下 iframe 不接收指针事件，以免妨碍拖拽。

**`html`**
- `html` 经 `golang.org/x/net/html` 解析、用**白名单重建**（见 14.3）后输出，≤ **64 KiB**，嵌套 ≤ **20** 层；`base` 是 http(s) 绝对 URL，用来解析相对链接。清洗后没有可见内容（无文字、无图片）的块被丢弃。输出里没有 `base` 字段。
- 前端在插入前**再清洗一次**：用 `DOMParser`（惰性文档，不执行脚本、不加载资源）解析，只把白名单节点和属性重建进一个新的 `DocumentFragment`；**从不**对插件的原始 HTML 使用 `innerHTML`。链接由前端加上 `target="_blank" rel="noopener noreferrer"`，图片加上 `loading="lazy" referrerpolicy="no-referrer"`。
- 样式是内置的、限定在卡片内的排版（标题、列表、表格、代码、引用……）。插件不能指定 `style`/`class`。

**`media`**
- `kind: "video"` 渲染 `<video playsinline preload="metadata">`；`kind: "audio"` 渲染一个与 Genkan 风格一致的紧凑播放条（播放/暂停、进度条、时间、标题、静音）。直播流没有时长：进度条换成 `LIVE`。`controls: false` 的音频只保留播放按钮和标题。
- `src`/`poster` 只能是 http(s)（`poster` 只对视频有效）。
- **`autoplay` 只在同时 `muted: true` 时生效**：浏览器不允许带声音的自动播放，服务端与前端都会丢弃未静音的 `autoplay`。
- `title` ≤ 200 字符。加载失败时显示“无法播放该媒体”。

**`image` 轮播**
- `images` 非空时是轮播：每张 `{ src, alt?, href? }`（`src` 规则同 `image.src`，`href` 为 http(s)），`interval`（秒，3–3600，默认 6）后淡入下一张，下方有圆点可跳转；单张图时没有圆点和定时器。`fit` 为 `cover`（默认，填满裁剪）或 `contain`（完整显示）。`height` 省略时容器是 16:9。有效图片为 0 张时块被丢弃。
- 单图写法 `src` 与之前完全一致；单图加 `fit`/`height` 时按只有一张的轮播渲染。

### 14.2 宿主 API：`genkan.html.select(html, selector, opts?)`

同步、纯计算（不需要任何权限，也不联网）。用 CSS 选择器（[cascadia](https://github.com/andybalholm/cascadia)，BSD-2-Clause）在**完整 HTML 文档**里选元素：

```js
const items = genkan.html.select(pageHtml, ".titleline > a", { base: "https://news.ycombinator.com/", limit: 5 });
// -> [{ html: '<a href="https://news.ycombinator.com/item?id=1">Title</a>', text: "Title" }, ...]
```

- 返回 `[{ html, text }]`，按文档顺序。`html` 是该元素**清洗后的外层 HTML**（与 `html` 块同一套白名单，URL 已绝对化；元素本身不在白名单时只保留其子内容），可直接放进 `{ type: "html", html }`；`text` 是折叠空白后的文本内容（≤ 4000 字符）。清洗后为空的匹配被跳过。
- `opts.base`：http(s) 绝对 URL，用来把相对链接/图片地址绝对化；省略则相对 URL 被丢弃。`opts.limit`：默认 **10**，最大 100。嵌套在较早匹配里的匹配会被跳过（`div` 不会返回每一层）。
- 输入 HTML **≤ 2 MiB**，选择器 ≤ 500 字符。无效选择器、非字符串参数、超限输入都会**抛出**带说明的错误，例如 `genkan.html.select: invalid selector "a[[": expected identifier, found [ instead`。没有匹配返回 `[]`，不是错误。
- `<tr>`、`<li>`、`<td>` 单独放进 `html` 块会被 HTML 解析器丢掉标签（片段解析没有表格/列表上下文），要自己包上 `<table><tbody>…</tbody></table>` / `<ul>…</ul>`（`media` 插件的 `snippet` 就是这么做的）。
- 抓取本身仍然走 `fetch` 与允许列表（§2.1）：不渲染 JavaScript，也不带登录状态。

### 14.3 `html` 白名单

| 项 | 内容 |
|---|---|
| 保留的元素 | `p div span br hr h1–h6 strong b em i u s small sub sup code pre blockquote ul ol li dl dt dd table thead tbody tr th td a img figure figcaption time` |
| 整体删除（含内容） | `script style iframe frame frameset object embed applet svg math template noscript noembed noframes head title meta link base audio video source track canvas map area dialog plaintext xmp textarea select option optgroup datalist button input slot portal`，以及所有 SVG/MathML 命名空间的元素 |
| 其余元素 | 去掉标签、**保留子内容**（`section`、`article`、`font`、`form` 等） |
| 保留的属性 | `a[href]`、`img[src alt width height]`、`td/th[colspan rowspan]`、任意白名单元素的 `title`、`time[datetime]`。**没有** `style`、`class`、`id`、`srcset`、`on*`、`target`、`rel` |
| URL | 只接受绝对 http(s)（相对 URL 用 `base` 解析，没有 `base` 就丢弃；`//host/x` 用 `base` 的协议）。解析发生在实体解码**之后**，所以 `&#106;avascript:`、`java&#x09;script:` 都看得到真实协议；URL 里含控制字符（含 tab/换行）一律拒绝；`javascript:`、`vbscript:`、`data:` 等全部丢弃。输出是规范化后的 `url.String()`。没有可用 `href` 的 `<a>` 退化为纯内容；没有可用 `src` 的 `<img>` 被删除 |
| 限制 | 输出 ≤ 64 KiB（超出时按文档顺序截断，保持标签闭合）、嵌套 ≤ 20 层（更深的元素只保留文字）、`html` 块输入 ≤ 512 KiB。解析器自身在 512 层打开元素处拒绝 |

### 14.4 安全模型

1. **不信任插件**：插件只能返回数据。`html` 在服务端被**重建**而不是“过滤”：输出树里只有白名单元素和属性，文本由序列化器转义，所以未知构造没有办法漏过去；前端用同一份白名单再做一次。
2. **iframe 的信任边界是“跨源”**：`allow-scripts` + `allow-same-origin` 让被嵌入页面像普通网站一样工作（登录态、本地存储、脚本）。这只有在它**不与 Genkan 同源**时才安全，否则页面里的脚本能访问管理员会话。服务端不知道 Genkan 的来源，因此前端在渲染时比较 `new URL(src).origin` 与 `location.origin`，相同则拒绝。`localhost` 与 `127.0.0.1` 是不同的 origin，属于跨源。被嵌入页面无法导航 Genkan（无 `allow-top-navigation`），也拿不到 Referer。
3. **没有新的服务端网络能力**：`iframe`/`media`/图片由**浏览器**加载，不需要 `permissions.fetch`，也不经过允许列表；服务端抓取（`snippet`）仍然受 §2.1 约束。代价是这些 URL 由访问者的浏览器请求，插件作者与被嵌入站点能看到访问者的 IP。
4. **只接受 http(s)**：`iframe.src`、`media.src/poster`、`html` 里的链接和图片、轮播的 `href` 都拒绝 `javascript:`、`data:`、`blob:`、`file:`。（轮播与单图的 `src` 仍允许 §3.2 已有的 `data:image/`。）
5. **自动播放**：只有静音的视频/音频才会自动播放。

### 14.5 内置插件 `media`

一个插件、五个小组件，全部需要每个实例自己的 URL，所以清单里没有固定的 fetch 主机。

| 小组件 | 默认大小 | 配置 | 输出 |
|---|---|---|---|
| `page` 网页 | l | `url`、`height`、`cropX/cropY/cropW/cropH`、`zoom`、`refresh`（0 = 关） | `iframe`；刷新由块自己的 `refresh` 完成，小组件轮询间隔 3600 秒 |
| `snippet` 网页片段 | m | `url`（`allowFetch`）、`selector`（必填）、`title`、`max`（默认 5，≤ 20） | 服务端抓取页面（浏览器式 User-Agent），`genkan.html.select` 后每个匹配一个 `html` 块（全是 `<tr>` 或 `<li>` 时合并成一个 `table`/`ul`）；刷新 900 秒。错误：无匹配（显示选择器）、选择器无效、抓取失败、HTTP 错误、非 HTML 内容类型。不支持 JavaScript 渲染的内容和需要登录的页面 |
| `image` 图片 | m | `urls`（每行一个，≤ 20）、`interval`、`fit`、`height`、`link` | 轮播 `image`；刷新 3600 秒 |
| `video` 视频 | m | `url`、`poster`、`autoplay`（静音）、`loop`、`controls` | YouTube（`watch?v=`、`youtu.be/`、`shorts/`、`live/`、`embed/`）→ `https://www.youtube-nocookie.com/embed/ID` 的 `iframe`（`ratio: 16/9`）；哔哩哔哩 `bilibili.com/video/BV…` → `https://player.bilibili.com/player.html?bvid=…&autoplay=0` 的 `iframe`；其它 → `media` 视频块。主机必须正好是 youtube / youtu.be / bilibili 域名（`youtube.com.evil.example` 之类不会被识别） |
| `audio` 音频 | s | `url`、`title`、`loop` | `media` 音频块（文件和电台流都可以） |

## 15. 协议 v3：可交互小组件、预装插件、小组件选择器

目标：插件能做可交互的小组件（输入、勾选、就地编辑、排序、Markdown 编辑）。内置 `todo`（待办）与 `note`（便签）两个插件，首次启动时自动安装。小组件选择器改为网格，加搜索。所有改动向后兼容，旧插件不受影响。

### 15.1 交互模型

所有交互都走现有的 `action(ctx)` 钩子（`POST /api/widgets/{id}/action`，仅管理员）。

1. 前端把用户的输入放进 `params.value`（或 `params.checked`、`params.ids`），与块里声明的 `params` 合并，块里的同名键会被覆盖，然后调用 action。
2. 插件写 `ctx.instance.storage`，返回 `{ view }`、`{ refresh: true }` 或 `{ toast }`。

访客（非管理员）看到的是只读视图：`input` 不渲染；勾选框只显示、不能点；条目按钮、就地编辑、拖动排序、Markdown 编辑入口都不出现。

### 15.2 新块与扩展字段（`SanitizeView`）

**`input`**：单行输入框，按 Enter 或点提交按钮触发。

```js
{ type: "input", action: "add", params: { list: "a" }, placeholder: "Add a task", submit: "Add", icon: "plus", maxLength: 500, value: "" }
```

- `action` 必填，规则同 `button`。`params` ≤ 2 KiB，规则同 `button`。
- `placeholder`、`submit` ≤ 200；`icon` 同 Lucide 规则；`value` 是初始值，≤ `maxLength`。
- `maxLength` 取 1..20000，默认 500，超出范围时夹到边界。
- 前端行为：
  - 空白输入（trim 后为空）不提交；
  - 提交成功后清空输入框并保持焦点；
  - 提交期间禁用输入框，防止重复提交。

**`list` 条目的新字段**：

- `id`：字符串，≤ 64，匹配 `^[A-Za-z0-9_.:-]+$`，否则丢弃该字段。用作排序和 React key。
- `check: { checked: bool, action, params? }`：标题前显示勾选框，点击时调用 action，`params.checked` 是新状态。`checked: true` 的条目，标题显示删除线并变暗。
- `edit: { action, params? }`：双击标题（或在编辑模式下点标题）就地编辑，Enter 保存、Esc 取消。值放在 `params.value`，≤ 500，空值不提交。
- `actions: [{ icon, label, action, params? }]`：条目右侧的小图标按钮，悬停或聚焦时显示，触屏上始终显示。最多 3 个；`icon` 与 `label`（用作 aria-label 和 title）都必填，否则丢弃该按钮。
- 所有 action 名都要匹配 `actionRe`，`params` ≤ 2 KiB；不合规的对象整体丢弃，条目本身保留。

**`list` 的新字段**：

- `sortAction`：action 名。只有当**所有**条目都有 `id` 时才保留。前端允许拖动排序，触屏用长按；键盘支持 Alt+↑/↓。放下后调用 action，`params.ids` 是新顺序的全部 id。
- `empty`：≤ 200，列表为空时显示的文字。有 `empty` 的空列表会保留，不再被丢弃。
- 条目上限：普通列表仍是 20；**含交互字段（任一条目有 `check`、`edit`、`actions`，或列表有 `sortAction`）的列表上限 200**。

**`markdown`**：

```js
{ type: "markdown", text: "# Hi\n- [ ] task", editAction: "save", params: {}, placeholder: "Write something…" }
```

- `text` ≤ 20000 字符。保留换行和制表符，去掉其它控制字符，不折叠空白。
- 设了 `editAction`（规则同 `actionRe`）就是可编辑便签：
  - 管理员双击正文或点卡片上的"编辑"按钮，切换为 textarea（自动高度）。
  - Ctrl/⌘+Enter 或失焦时保存；Esc 取消。
  - 值放在 `params.value`，可以为空字符串，表示清空。
- 任务列表 `- [ ]` / `- [x]` 在显示状态下可以直接点：前端翻转该行，再用整段新文本调用 `editAction`。
- `placeholder` ≤ 200：文本为空时显示（管理员看到"双击编辑"之类的提示）。
- 前端用安全渲染器（`web/src/md.tsx`，扩展后）生成 VNode，**不使用 innerHTML**。
  - 支持：标题 h1-h6、段落、粗体、斜体、删除线、行内代码、围栏代码块、引用、有序和无序列表（含嵌套一层）、任务列表、链接、自动链接、分隔线、GFM 表格。
  - 链接只允许 http(s) 和 mailto，加 `target=_blank rel="noopener noreferrer"`；图片语法渲染成链接，不加载图片。

**WidgetView 的新字段**：

- `tone`：取 `"yellow" | "pink" | "blue" | "green" | "purple"` 之一，其它值丢弃。卡片会染上对应的柔和底色，由前端定义，深浅主题下文字都可读。

### 15.3 action 请求与结果

- 请求：`{ action, params }`。`params.value` 若是字符串，单独限制为 ≤ 20000 字符；其余部分（去掉 `value` 后）仍 ≤ 8 KiB。
  - 请求体整体上限放宽到 64 KiB，所以服务端的 `readJSON` 对这个端点用单独的上限。
  - 超限返回 400。
- 结果新增 `view`：action 可以返回 `{ view: <WidgetView>, toast? }`。
  - 服务端按 `SanitizeView` 清洗后，**写回该实例的视图缓存**，并在响应里原样返回 `view`。前端直接用它替换卡片内容，不用再请求一次 `/data`。
  - 返回的视图带上和 `/data` 一样的元信息（例如 `updated_at`），具体格式与 `WidgetData` 对齐，实现时定义为 `ActionResult.View *WidgetData`。
  - 没有 `view` 时行为不变：清掉视图缓存；`refresh: true` 时前端重新拉取。
- **同一个实例的 action 串行执行**：按 widget id 加锁，用 `sync.Map` 里存 `*sync.Mutex`，或类似方案。快速连点不会丢失修改。不同实例之间仍可并发。
- `/test` 的 action 试运行同样支持返回 `view`。

### 15.4 预装插件（自动安装一次）

- `store.Settings` 新增 `PluginsSeeded []string`（json `plugins_seeded`），不通过 `PATCH /api/settings` 暴露给用户编辑。
- 启动时（`plugin.Manager` 初始化之后），对 `autoInstall = ["todo", "note"]` 里每个不在 `plugins_seeded` 中的 id：
  - 若未安装，就从 catalog 安装并启用（Install 走现有逻辑）；
  - 无论是否已安装，都把 id 记入 `plugins_seeded`。
  - 所以用户卸载之后不会被装回来；安装失败只记日志，不记入，下次启动再试。
- 只安装插件，不自动添加小组件。
- 备份导出、导入时 `plugins_seeded` 跟随 settings 一起处理（加性字段）。

### 15.5 catalog 返回小组件列表；小组件选择器

- `CatalogItem` 新增 `widgets: [{ id, name, size }]`（按请求语言本地化，与 `Info.widgets` 同格式），没有小组件时为空数组或省略。`GET /api/plugins/catalog` 原样返回。
- 前端选择器（`WidgetModal` 的选择步骤）：
  - **顶部搜索框**（`data-testid="widget-search"`），自动聚焦。
    - 按小组件名、插件名、插件描述、id 过滤，中英文名都参与匹配，不区分大小写。
    - 无结果时显示空状态（`widget-search-empty`）。
  - **网格卡片**：宽度足够时 3 列，≤ 520px 时 2 列。卡片内容：图标、小组件名、插件名、默认尺寸。卡片的 testid 不变（`widget-pick-<plugin>-<widget>`）。
  - **分组**："已安装"（已启用插件的小组件，按插件名、小组件名排序）、"已禁用的插件"、最后是**未安装的 catalog 插件**"可安装"，卡片上有"未安装"徽标。卡片本身显示插件名，所以不再按插件逐个分组（实现中调整：单个小组件的插件会产生大量只有一张卡的组）。
    - 点未安装的卡片：调用 `POST /api/plugins {catalog: id, enable: true}`，成功后刷新插件列表，进入该小组件的配置表单。安装中显示加载状态，失败时 toast。
    - 已安装但被禁用的插件也显示，徽标为"已禁用"。点击后先 `PATCH` 启用，再进入配置表单；SSH 插件先展示权限确认，取消保持禁用。
  - 弹窗内容区高度固定（最大约 70vh），只有列表区域滚动，搜索框固定在顶部。
- 编辑模式下的"添加小组件"块独占一整行：`grid-column: 1 / -1`，高度约 52px，位于小组件区最前。

### 15.6 内置插件 `todo` 与 `note`（`plugins/catalog/`）

两个插件都要做中英文本地化（`messages`），不需要任何 `permissions.fetch`。

**`todo`**，小组件 `list`，size m，图标 `list-todo`，无配置项：
- 实例存储键 `items`：`[{ id, text, done, at }]`，最多 200 条，`text` ≤ 500。
- 视图：
  - `title` 为"待办"，或"待办 · N"（N 为未完成数）；
  - 先是一个 `input`（action `add`）；
  - 然后是一个 `list`：未完成的在前，已完成的在后，保持各自的相对顺序。每条有 `id`、`check`（action `toggle`）、`edit`（action `rename`）、`actions: [{ icon: "trash-2", action: "remove" }]`，列表带 `sortAction: "reorder"` 和 `empty` 文案；
  - 有已完成项时，末尾一个 `button`（action `clear`，图标 `eraser`，标签"清除已完成 (N)"）。
- 每个 action 都返回 `{ view: await widget(ctx) }` 形式的新视图。

**`note`**，小组件 `note`，size m，图标 `sticky-note`：
- 配置项 `tone`：select，取值 default / yellow / pink / blue / green / purple，默认 `yellow`。
- 实例存储键 `text`：≤ 20000 字符。
- 视图：
  - 无 `title`，即不显示标题栏；
  - `tone` 不是 default 时设置 `tone`；
  - `body` 是一个 `markdown` 块，`editAction: "save"`，并设置 `placeholder`。
- action `save` 写入 `text`，返回新视图。

### 15.7 文档与 Skill

- 插件开发指南移到 **`skills/genkan/plugin-dev.md`**，embed 进 Skill 包和 `/api/skill` 的 zip；删除 `docs/PLUGIN_DEV.md`，所有引用随之更新。
- 指南新增"可交互小组件"一节，用 todo 作完整示例。
- `SKILL.md`：
  - description 加上插件开发的触发词，例如"develop/write/debug a Genkan plugin or widget"、"开发 Genkan 插件"；
  - 新增"Developing plugins"一节，写清完整闭环，并指向 `plugin-dev.md`。
- 设置 → AI 的 Skill 安装提示词，下载文件列表加上 `plugin-dev.md`。
- 插件页"用 AI 开发"弹窗：
  - 步骤 1 是安装或更新 Skill；
  - 步骤 2 复制一句短提示词："用 Genkan Skill 帮我开发一个 Genkan 插件：【需求】"，中英文随界面语言。
  - 不再内嵌指南链接和长流程。

### 15.8 后端实现补充

以下是后端落地时对 §15.1–15.7 的澄清；与前文冲突处以本节为准。

**action 响应的 JSON 形状**（`POST /api/widgets/{id}/action`；`view` 的类型就是 §5 里 `GET /data` 的 `WidgetData`，前端可以把 `res.view` 直接当作 `/data` 的响应使用）：

```jsonc
{
  "toast": "…",          // 可选
  "refresh": true,       // 可选
  "view": {              // 可选：插件返回了 view 时才有，永远是清洗后的
    "view": { "title": "…", "icon": "…", "tone": "yellow", "body": [ /* 清洗后的块 */ ] },
    "updated_at": 1730000000000,   // ms，= 服务端生成该视图的时刻
    "next_at":    1730000060000    // ms，= updated_at + 小组件清单里的 refresh × 1000
  }
}
```

- 插件 `action` 返回值里的 `view` 若不是对象则被忽略（当作没有 `view`，缓存被清掉）。
- 有 `view`：服务端把它写回该实例的视图缓存，**只写请求语言**（`?lang=` / `Accept-Language`）的那一份；其它语言的缓存被清掉，下次请求时重新调用 `widget()`。没有 `view`（或 action 抛错）：该实例所有语言的缓存都被清掉。
- 如果有一次 `widget()` 在 action 完成**之前**就开始了、之后才结束，它的结果不会写入缓存（用每实例的代数计数判断），因此不会用旧视图覆盖 action 刚写入的新视图。
- `/test`（`hook: "action"`）的 `result` 是同样形状 `{ toast?, refresh?, view? }`，`view` 带 `updated_at`/`next_at`；它不写任何缓存（未保存的实例用一次性的内存存储）。

**限制与错误码**

| 项 | 限制 | 超限 |
|---|---|---|
| action 请求体 | 64 KiB（只对 `POST /api/widgets/{id}/action`，其它端点仍是 1 MiB） | 413 `request body too large` |
| `params.value`（字符串） | ≤ 20000 个字符（按字符不是字节） | 400 `params.value is too long (max 20000 characters)` |
| `params` 去掉 `value` 之后 | JSON ≤ 8 KiB（`value` 不是字符串时整体算在内） | 400 `params are too large` |
| `params` 不是对象 | 整体仍 ≤ 8 KiB | 400 |
| action 名 | `^[A-Za-z0-9_.:-]{1,40}$` | 400 `invalid action name` |

`/test` 的 action 试运行使用同样的 `params` 限制（请求体仍是 1 MiB）。

**串行化**：每个 widget id 一个容量为 1 的 channel（存在 `sync.Map` 里），按到达顺序排队；等待锁的请求在 HTTP 请求被取消时放弃（不会执行）。持锁期间包括 `ctx.instance.storage` 的读-改-写和写回缓存。不同实例互不影响。删除小组件时锁被丢弃。

**清洗细则**（补充 §15.2）

- 声明 action 的新结构里，`params` 必须是 JSON 对象且序列化 ≤ 2 KiB（浏览器会把 `value`/`checked`/`ids` 合并进这个对象）：
  - `input`、`markdown`：`params` 不合格（不是对象或太大）时只丢弃 `params`，块保留（与 `button` 一致）。`markdown` 没有合法的 `editAction` 时 `params` 也被丢弃。
  - `check`、`edit`、`actions[]`：不合格的 `params` 使**整个对象**被丢弃（条目保留）。
- `input`：输出一定带 `maxLength`（整数，夹到 1..20000，缺省或非数字为 500）；`placeholder`、`submit` 按单行清洗（合并空白，≤ 200）；`value` 按单行清洗并截到 `maxLength`，为空则省略。
- `list` 条目：
  - `id` 不合法（空、含非法字符、> 64、不是字符串）或与前面某条重复时，该条目的 `id` 被丢弃（条目保留）；
  - `check.checked` 输出里总是布尔值（缺省或非布尔为 `false`）；
  - `actions` 里不合格的按钮被跳过，不占 3 个的名额；全部不合格则没有 `actions` 字段。
- `list.sortAction`：对**清洗后留下的**条目判断：全部有（合法且唯一的）`id` 才保留；空列表也保留。
- 条目上限：先按 200 清洗；清洗后如果这个列表没有任何交互字段（没有 `check`/`edit`/`actions`，也没有保留下来的 `sortAction`），再截到 20。也就是说，不合格的交互字段不会解锁 200 条。
- `list.empty`（≤ 200，按单行清洗）有则输出。空列表（没有 `empty` 也一样）一直都会保留，这一点没有变化。
- `markdown`：`text` 必须是字符串（空字符串合法，表示空便签），否则丢弃整个块；`\r\n` 先变成 `\n`，再去掉除 `\n`、`\t` 外的控制字符（含 `\r`、`\u0085`），不 trim、不折叠空白，截到 20000 个字符。
- `WidgetView.tone` 区分大小写，只认小写的五个值；`"default"` 或其它值被丢弃（输出里没有 `tone`）。

**预装**（补充 §15.4）

- 入口：`plugin.Manager.SeedDefaults(ctx)`，`cmd/genkan/main.go` 在 `server.New` 之后、开始监听之前同步调用一次；`server.New` 本身不预装（测试和嵌入使用不受影响）。
- `Settings.PluginsSeeded` 的 JSON 永远是数组（`"plugins_seeded": []`），旧数据文件缺省时加载为空数组并在下次保存时写出；不改 `SchemaVersion`。只能通过 `Store.MarkPluginsSeeded` 追加，`PATCH /api/settings` 与 MCP `update_settings` 会忽略该字段。
- 导入：`replace` 时 `plugins_seeded` 取**导入文件与当前值的并集**（旧备份没有该字段也不会让已卸载的预装插件回来）；`merge` 不动设置。
- 全新安装的启动日志里会有 `plugins: installed built-in plugin todo` 之类的行。

**catalog**（补充 §15.5）

- `widgets[].name` 是与 `Info.widgets` 相同的 LocalizedString（字符串或 `{ lang: text }` 对象），由前端按界面语言解析；不是后端按请求语言解析后的字符串。`widgets` 永远是数组，没有小组件的插件是 `[]`。MCP `list_plugins` 的 `catalog` 同样带上它。

**`todo` / `note` 细节**（补充 §15.6）

- 两个插件的小组件都是 `refresh: 60`（只影响多设备间的同步；本机的修改靠 action 返回的 `view` 立即生效）。`todo` 的小组件 id 是 `list`，`note` 的是 `note`。
- `todo`：标题只在有未完成项时带 `· N`；文字单行化（合并空白、去控制字符）并截到 500 字符；`toggle` 的 `params.checked` 缺省时翻转；`reorder` 的 `params.ids` 里未知 id 被忽略、没提到的条目保持原有顺序排在最后；空文本、未知 id、第 201 条都**抛出本地化错误**（API 返回 502，`error` 为该文案，前端以 toast 显示）。
- `note`：`tone` 选项为 `default`（不染色）或五种颜色，默认 `yellow`；`save` 的 `params.value` 必须是字符串，否则抛错；空字符串会删除存储键。

## 17. 原生 SSH 资源采集

插件声明 `permissions: { ssh: true }` 后，可调用 `await genkan.ssh.stats(options)`。默认无 SSH 权限，安装预览、权限列表与更新权限比较均包含此权限。该权限允许使用插件提供的凭据连接任意指定主机（包括内网），不复用 HTTP fetch 白名单。界面以警告样式提示，只启用可信插件。小组件选择器中选择已禁用或新安装的 SSH 插件，也必须确认权限后才启用；取消则保持禁用。

`options` 为 `{host, port?, username, fingerprint?, password?, privateKey?, passphrase?}`。`host` 是不含协议/端口的主机名或 IP，IPv6 不带方括号；`port` 默认为 22。`fingerprint` 可选：省略或空字符串时直接连接，不确认也不校验服务器身份；提供时必须是从可信服务器控制台核实的 `SHA256:` 主机公钥指纹，格式错误或不匹配仍拒绝连接。密码和 PEM/OpenSSH 私钥二选一；加密私钥可提供口令；OpenSSH bcrypt 参数上限为 64 轮/64 字节 salt，超出返回 `SSH_KEY_COST`，避免解密耗时突破 hook 预算。凭据使用 `secret` 配置，私钥声明 `multiline:true`（最多 5000 字符，允许换行、回车和制表符）；所有 secret 仍采用原有遮蔽、保持、清除和存储规则，**不是额外的静态加密存储**。普通 secret 的 2000 字符限制不变。

实现使用现有 `golang.org/x/crypto/ssh`，没有本机 shell/SSH 进程、文件访问、SSH agent、端口转发或任意远程命令 API。仅执行内置只读 Linux 采集脚本；远端需要 `/proc`、`/bin/sh`、awk、cat、df、uname、tail、sleep，无需 root/sudo 或远端 agent。仅采集可见的系统资源，不保证是容器 cgroup 限额。

- 每次 hook 最多调用一次；全服务最多 8 个同时执行的采集，排队计入超时。
- 每次最多 7 秒，也受 hook 剩余时间限制；取消会关闭连接。采样窗口约 1 秒。
- stdout 最多 64 KiB，stderr 最多 8 KiB；不把原始错误、banner 或 stderr 返回给插件。
- 安装探测和模块顶层不能联网。连接不跨调用复用。

返回对象：

```js
{
  hostname, kernel, arch, cores,
  collectedAt,       // Genkan 时钟，Unix 毫秒
  cpuPercent,        // 0..100，总 CPU 两次计数之差，排除 idle/iowait；不重复计算 guest
  uptimeSeconds,
  load: [load1, load5, load15],
  memory: { totalBytes, usedBytes, percent }, // used = MemTotal - MemAvailable
  disk: { mount: "/", totalBytes, usedBytes, percent }, // df -Pk /；percent = used/(used+available)，含保留空间语义
  network: { rxBytesPerSecond, txBytesPerSecond } // 非 lo 接口合计，计数重置/新增接口不产生虚假速率
}
```

网络合计可能重复计算 bridge/veth/物理接口中的同一流量；首版不支持网卡筛选、其他挂载点或非 Linux 系统。计数缺失/不合法会报错，不伪造为零。

错误为固定代码：`SSH_CONFIG`、`SSH_FINGERPRINT`、`SSH_CREDENTIALS`、`SSH_KEY`、`SSH_KEY_COST`、`SSH_HOST_KEY`、`SSH_HANDSHAKE`、`SSH_CONNECT`、`SSH_TIMEOUT`、`SSH_SESSION`、`SSH_COLLECT`、`SSH_DATA`、`SSH_OUTPUT`、`SSH_LIMIT`。无权限和安装期调用有明确错误。插件应自行用 `ctx.t` 翻译，且不可把凭据写入视图或缓存。
