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
- 小组件按钮 `action` 名需匹配 `^[A-Za-z0-9_.:-]{1,40}$`；`params` 序列化后 ≤ 2 KiB（action 请求 ≤ 8 KiB）。
- 导出 `schema` 升为 **2**（旧文件无需迁移，可直接读取；旧版本程序会拒绝读取 schema 2 的数据文件）。导出中仅在存在插件/小组件时才出现 `plugins`、`widgets` 键；`plugins[]` 为 `PluginRecord + source + lang`，secret 值被去除（无法解析 manifest 的插件导出时不含 config）。导入前每个插件源码都会被编译校验，manifest id 必须与记录一致；`merge` 跳过已存在的插件 id；`POST /api/import` 响应新增 `plugins`、`widgets` 计数。
- 数据文件里的 `plugins[]` 记录多一个 `lang`（`js`|`ts`）字段。
- 运行时池：每次调用独占一个运行时，成功结束的运行时放回池（每插件最多 2 个空闲，5 分钟无调用释放）；超时、出错或钩子完成后仍有残留定时器的运行时直接丢弃。插件不应依赖 JS 全局变量保存状态（请用 `genkan.storage`）。
- Docker 镜像是 `FROM scratch`：二进制内置了 `time/tzdata`（约 450 KB），world-clock 才能使用 IANA 时区。

## 11. 第二轮需求（v2）

### 11.1 小组件布局（拖拽、自由尺寸、紧凑排列）

- `WidgetInstance` 新增 **`w`**（列宽，整数 0–6；0 = 按 manifest `size` 取默认：s→2、m→3、l→6）与 **`h`**（高度，像素整数；0 = 自动按内容高度；否则 80–1200）。`POST /api/widgets`、`PATCH /api/widgets/{id}`、MCP `add_widget`/`update_widget`、导出导入都支持这两个字段；越界值钳制到范围内。
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
  - 已安装插件行增加"源码"按钮 → 源码编辑器弹窗（纯 textarea + 行号栏，等宽字体，Tab 插入两个空格，Ctrl/⌘+S 保存；无新依赖，懒加载）。保存调用 `PUT .../source`，显示错误；附"试运行"面板（选择钩子/目标，调用 `/test`，展示 JSON 结果、错误、日志、耗时）。
  - 安装流程（内置目录、URL、粘贴、第三方）都提供"预览源码"按钮（默认折叠不展示源码），权限确认弹窗中也有该按钮。

### 11.3 AI 开发插件

- 新文档 **`docs/PLUGIN_DEV.md`**（英文为主，面向 AI 编码助手）：插件开发完整指南——文件格式、manifest、ctx、宿主 API、返回值、限制、通过 REST 的"编写 → 安装(`enable:false`) → test → 看日志 → 修改 → 启用 → 添加小组件"闭环、调试技巧、常见错误、完整示例、发布到 GitHub 的约定（11.4）。GitHub 地址：`https://github.com/jae-jae/genkan/blob/main/docs/PLUGIN_DEV.md`，原始文件：`https://raw.githubusercontent.com/jae-jae/genkan/main/docs/PLUGIN_DEV.md`。Skill（`skills/genkan/SKILL.md`、`reference.md`）中加入指向它的说明。
- 插件页"添加插件"旁边新增"用 AI 开发"按钮 → 弹窗三步：① 先到 设置→AI 按提示安装 Genkan Skill（带跳转按钮）；② 复制提示词（中/英随界面语言，包含上面的文档地址、要求先读文档、用 Skill 的 REST API 安装为禁用状态并 test 通过后再启用、完成后告诉我插件 id）；③ 在提示词末尾的占位处写上你的需求，发给 AI。

### 11.4 第三方插件（GitHub）

- 约定：仓库加 topic **`genkan-plugin`**。插件文件为仓库内任意 `.js`/`.ts`（≤ 512 KiB，含 `export const manifest`），扫描时跳过 `node_modules/`、`.github/`、`test(s)/`、`dist/` 以外的隐藏目录；若仓库根目录有 `genkan-plugins.json`（`{ "plugins": ["path/a.js", ...] }`）则只用其中列出的文件。最多 40 个候选文件。
- `GET /api/plugins/market?sort=stars|updated&q=&page=`（admin）：服务端调用 GitHub Search API（`topic:genkan-plugin` + 可选关键字），缓存 10 分钟。返回 `{ items: { repo, description, stars, updated_at, html_url, owner_avatar, default_branch }[], total, cached_at, error? }`。
- `GET /api/plugins/market/repo?repo=owner/name`（admin）：扫描仓库（git trees API + raw.githubusercontent.com，并发 ≤ 4），对每个候选文件编译并校验 manifest，缓存 10 分钟。返回 `{ repo, default_branch, plugins: { path, url /* raw URL，作为安装用的 source_url */, plugin: PluginInfo | null, error?, installed: bool, installed_version?: string }[] }`。
- 安装沿用 `POST /api/plugins {url}`（因此记录 `source_url`，可"检查更新"）。前端支持勾选多个/全部安装，逐个安装并汇总结果；安装前同样走权限确认（可合并为一次确认，列出所有插件的权限）。
- 可选环境变量 `GENKAN_GITHUB_TOKEN` 提高 GitHub API 限额。只有打开"第三方"标签时服务端才访问 GitHub；GitHub 不可达时返回 `error` 而非 5xx 页面。
- 前端：添加插件弹窗新增"第三方"标签：搜索框、排序（Star / 最近更新）、仓库卡片（头像、名称、描述、star、更新时间、GitHub 链接）、"查看插件"→ 展开扫描结果列表（复选框、名称、能力徽章、版本、已安装状态、预览源码）→"安装选中 / 全部安装"。显著标注"第三方插件未经审核，请检查权限和源码"。
