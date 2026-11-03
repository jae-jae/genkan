# Genkan 插件机制 — 需求与接口契约 (PLUGINS)

本文档是插件功能的**唯一契约**：后端、前端、示例插件三方都以此为准。实现中如发现契约不合理，可以小幅调整，但必须同步修改本文件并保持三方一致。

## 0. 目标与原则

- 用 JavaScript 插件扩展导航页：**壁纸源、网页小组件(Widget)、搜索提供者、定时任务、小组件按钮动作**。
- 插件在**服务端**运行，引擎为纯 Go 的 [moejs](https://github.com/MoeForge/moejs)（`github.com/MoeForge/moejs`，使用 `v0.1.0-alpha.11` 或更新版本；`eventloop` + `webapi` 子包提供定时器和 `fetch`）。保持 `CGO_ENABLED=0`、单二进制。
- 浏览器**永不执行插件代码**。插件输出的是**声明式 JSON**（小组件视图、搜索结果、壁纸元数据），由前端用固定组件渲染，不使用 `innerHTML`。
- 遵守 SPEC 的第一原则：轻量。没有启用插件时不创建任何 JS 运行时、不增加常驻内存；首屏 JS gzip 仍需 ≤ 60 KB（插件相关 UI 尽量精简）。
- AI-native：AI 可以通过 REST / MCP **编写、安装、测试**插件（"帮我写一个显示 Pi-hole 拦截数的小组件"）。
- 安全：插件只能访问 manifest 声明的主机；有超时、内存上限、存储上限；无文件系统访问。

## 1. 插件文件格式

一个插件 = **一个 ES module 源文件**（`.js`；也接受 TypeScript，用 `moejs.CompileTS` 编译，由安装请求的 `lang: "ts"` 指定）。插件 ID 取自 manifest。

```js
export const manifest = {
  id: "weather",                 // 必填，^[a-z0-9][a-z0-9-]{1,39}$，全局唯一
  name: "天气",                  // 必填，≤ 40 字符
  version: "1.0.0",              // 必填，≤ 20 字符
  description: "Open-Meteo 天气", // 可选，≤ 300
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
  wallpapers: [{ id: "daily", name: "每日壁纸", refresh: 3600 }],               // refresh 秒，≥ 300，默认 3600
  widgets:    [{ id: "current", name: "当前天气", size: "s", refresh: 600,     // size: "s"|"m"|"l"；refresh 秒 ≥ 30，默认 300
                 config: [ /* 实例级 ConfigField[]，每个小组件实例单独填写 */ ] }],
  search:     { name: "计算器", prefix: "" },                                  // prefix 非空时仅当查询以 "<prefix> " 开头才调用，并把去掉前缀后的文本作为 ctx.query
  schedules:  [{ id: "sync", every: 3600 }],                                    // every 秒 ≥ 60
}

export async function wallpaper(ctx) { /* → WallpaperResult */ }
export async function widget(ctx)    { /* → WidgetView */ }
export async function search(ctx)    { /* → SearchResult[] */ }
export async function schedule(ctx)  { /* → 任意，忽略 */ }
export async function action(ctx)    { /* → { toast?: string, refresh?: boolean } */ }
```

声明了某能力却没有导出对应函数 → 安装时校验失败。

### ConfigField

```ts
{
  key: string            // ^[a-zA-Z][a-zA-Z0-9_]{0,39}$
  type: "string" | "text" | "number" | "boolean" | "select" | "secret" | "url"
  label: string          // ≤ 60
  help?: string          // ≤ 200
  default?: any
  required?: boolean
  options?: { value: string, label: string }[]   // select 必填
  min?: number, max?: number                       // number
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
genkan.links.list(): Link[]         // 需 permissions.links >= "read"；字段同 REST 的 Link
genkan.links.create(input): Link    // 需 "write"；input 同 POST /api/links
genkan.links.update(id, patch): Link// 需 "write"
genkan.workspaces.list() / genkan.categories.list()   // 需 "read"
genkan.version                      // 服务器版本字符串
```

此外有标准 `fetch`/`URL`/`URLSearchParams`/`AbortController`/`TextDecoder` 等（来自 webapi），以及 `setTimeout` 等定时器。没有 `require`、`import`（禁止导入其它模块）、文件系统。

### 2.2 ctx 对象

```ts
{
  plugin: { id, version },
  config: { ...插件级配置（含 secret 明文） },
  instance?: { id, config },   // widget/action：当前小组件实例
  source?: string,             // wallpaper：壁纸源 id
  widget?: string,             // widget/action：小组件 id
  query?: string,              // search：去掉前缀后的查询
  action?: string, params?: any, // action
  schedule?: string,           // schedule：任务 id
  lang: "zh" | "en",           // 请求方语言（Accept-Language/前端传参），定时任务为 "en"
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
  | { type: "row",      children: Block[] }                         // 水平排列
  | { type: "button",   label: string, action: string, params?: any, icon?: string }  // 仅管理员可点；POST action 钩子
  | { type: "divider" }
```

- 后端负责**规范化与清洗**：丢弃未知类型和字段、截断字符串（单个字符串 ≤ 500）、过滤非 http(s)/非 `data:image/` 的 URL、限制数量。前端仍需防御性渲染。
- 字符串一律作为纯文本渲染。

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
| `GET /api/plugins/catalog` | admin | 内置示例插件列表：`{ id, name, description, icon, version, installed: bool }[]` |
| `GET /api/plugins/{id}/source` | admin | `{ source, lang }` |
| `PATCH /api/plugins/{id}` | admin | `{ enabled?: bool, config?: object }` |
| `DELETE /api/plugins/{id}` | admin | 卸载 |
| `GET /api/plugins/{id}/logs` | admin | `{ logs: { ts, level, msg }[] }` |
| `POST /api/plugins/{id}/test` | admin | 试运行，供 AI/开发者调试：`{ hook: "wallpaper"\|"widget"\|"search"\|"schedule"\|"action", target?: string /* source/widget/schedule id */, query?, config?: object /* 实例配置 */, action?, params? }` → `{ ok, result, error?, logs, ms }`。不写缓存 |
| `POST /api/widgets` | admin | 新建实例 `{ plugin, widget, workspace_id?, config? }` |
| `PATCH /api/widgets/{id}` | admin | `{ workspace_id?, config?, sort? }` |
| `DELETE /api/widgets/{id}` | admin | |
| `POST /api/widgets/reorder` | admin | `{ ids: string[] }` |
| `GET /api/widgets/{id}/data` | read | `{ view: WidgetView, updated_at, next_at }`。服务端按 refresh 缓存；`?force=1` 仅 admin 生效，绕过缓存 |
| `POST /api/widgets/{id}/action` | admin | `{ action, params }` → `{ toast?, refresh? }` |
| `GET /api/plugins/wallpaper/{plugin}/{source}` | read | `{ url: "/uploads/plugin-wallpapers/<file>", title?, credit?, link?, updated_at }`，按 refresh 缓存 |
| `GET /api/plugins/search?q=` | read | 并发调用所有已启用的搜索插件（无前缀的仅当 q ≥ 2 字符），总时长 ≤ 2 s，返回 `{ groups: { plugin, name, icon, results: SearchResult[] }[] }`；超时/出错的插件被忽略 |
| `GET /uploads/plugin-wallpapers/{name}` | read | 复用现有 `/uploads/{kind}/{name}` |

`PluginInfo`：

```ts
{ id, name, version, description, author, homepage, icon, enabled: boolean,
  permissions: { fetch: string[], links: "" | "read" | "write" },
  config_schema: ConfigField[], wallpapers: [...], widgets: [...], search: {...} | null, schedules: [...],
  // 仅 admin：
  config?: object, secrets_set?: string[], source_url?: string, last_error?: string, installed_at?, updated_at? }
```

`WidgetInstance`：`{ id, plugin, widget, workspace_id, config, sort }`（访客看到的 config 中去掉实例 schema 里 secret 字段）。

## 6. MCP 工具与 Skill

新增 MCP 工具（风格与现有工具一致）：`list_plugins`、`install_plugin`（source 或 url 或 catalog）、`update_plugin`（enabled/config）、`uninstall_plugin`、`test_plugin`、`plugin_logs`、`list_widgets`、`add_widget`、`update_widget`、`remove_widget`。
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

每个示例都要短小、注释清晰，作为插件开发者的范例。

## 9. 测试与验收

- Go：`go vet ./... && go test -race ./...` 通过。覆盖：manifest 校验、各钩子调用、超时中断、内存超限、fetch 白名单拒绝（用 httptest 服务器）、storage 上限、secret 不外泄、WidgetView 清洗、壁纸下载缓存与清理、REST 权限（访客/未登录）、导出导入、删除插件的级联清理、**所有内置示例插件均能编译且 manifest 校验通过**、无网络的示例（calculator/world-clock/countdown）实际运行结果正确。
- 前端：`npm run build` 通过（tsc + vite），gzip 体积不超标。
- e2e：新增 `e2e/specs/20-plugins.spec.ts`：安装内置示例（calculator、world-clock、countdown，均无外网依赖）、添加小组件并看到渲染、搜索得到计算结果、禁用后消失、访客可见但无管理按钮。
- 文档：README（中英）功能列表加一行；`docs/SPEC.md` 加指向本文的一节；`web/TESTIDS.md`、`skills/genkan/reference.md` 同步。
