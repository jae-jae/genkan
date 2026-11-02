package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jae-jae/genkan/internal/store"
)

type tool struct {
	name        string
	title       string
	desc        string
	schema      map[string]any
	readOnly    bool
	destructive bool
	idempotent  bool
	fn          func(args json.RawMessage) (any, error)
}

type M = map[string]any

// JSON Schema helpers -------------------------------------------------------

func str(desc string) M     { return M{"type": "string", "description": desc} }
func boolean(desc string) M { return M{"type": "boolean", "description": desc} }
func strArr(desc string) M {
	return M{"type": "array", "items": M{"type": "string"}, "description": desc}
}
func enumStr(desc string, values []string) M {
	return M{"type": "string", "enum": values, "description": desc}
}

// workspaceIconSchema is the icon parameter shared by create_workspace/update_workspace:
// an enum of the built-in Lucide icons, in the lucide:<name> form stored by Genkan.
func workspaceIconSchema(desc string) M {
	vals := make([]string, len(store.BuiltinIcons))
	for i, n := range store.BuiltinIcons {
		vals[i] = store.WorkspaceIconPrefix + n
	}
	return enumStr(desc+" One of the built-in Lucide icons (lucide:<name>); default lucide:layout-grid. 内置 Lucide 图标之一。", vals)
}

func obj(props M, required ...string) M {
	m := M{"type": "object", "properties": props}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

func linkProps() M {
	return M{
		"title":        str("Display name, e.g. \"Jellyfin\". 显示名称。"),
		"url":          str("Full URL incl. scheme, e.g. http://nas.lan:8096. 完整地址，需含协议。"),
		"description":  str("Short description shown in tooltip (optional). 简短描述（可选）。"),
		"icon":         str("Empty = auto-fetch favicon (recommended). Or \"emoji:🎬\", \"url:https://…/icon.png\". 留空则自动抓取图标（推荐）。"),
		"pinned":       boolean("Pin the link: it is shown in the pinned row at the top of the workspace AND stays in its category (pinning never moves it). New pins go to the end of the pinned row. 是否置顶：显示在工作空间顶部的置顶行，同时仍保留在所属分类中。"),
		"health_check": boolean("Enable online/offline status dot for this service. 是否开启在线检测。"),
	}
}

func merge(a, b M) M {
	out := M{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func decode(args json.RawMessage, v any) error {
	if err := json.Unmarshal(args, v); err != nil {
		return fmt.Errorf("invalid arguments: %v", err)
	}
	return nil
}

// Overview types -------------------------------------------------------------

type ovLink struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	URL         string  `json:"url"`
	Description string  `json:"description"`
	Icon        string  `json:"icon"`
	Pinned      bool    `json:"pinned"`
	PinSort     int     `json:"pin_sort"`
	Clicks      int     `json:"clicks"`
	LastUsedAt  *string `json:"last_used_at"`
	HealthCheck bool    `json:"health_check"`
}

type ovCategory struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Links []ovLink `json:"links"`
}

type ovWorkspace struct {
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	Icon          string       `json:"icon"`
	Wallpaper     string       `json:"wallpaper"`
	Categories    []ovCategory `json:"categories"`
	Uncategorized []ovLink     `json:"uncategorized"`
}

func toOv(l store.Link) ovLink {
	return ovLink{l.ID, l.Title, l.URL, l.Description, l.Icon, l.Pinned, l.PinSort, l.Clicks, l.LastUsedAt, l.HealthCheck}
}

func (s *Server) buildTools() []tool {
	st := s.Store

	okResult := func(m M) (any, error) { return m, nil }

	return []tool{
		{
			name: "get_overview", title: "Get overview", readOnly: true, idempotent: true,
			desc: "Return the whole navigation tree: every workspace → categories → links (with ids, clicks, pinned, pin_sort, health_check; links are listed in category order, pinned links appear in their category and are also shown in the pinned row ordered by pin_sort) plus settings. Call this first before organizing. " +
				"返回完整的导航结构（工作空间→分类→链接）及设置，整理前请先调用。",
			schema: obj(M{}),
			fn: func(json.RawMessage) (any, error) {
				snap := st.Snapshot()
				catIdx := map[string]*ovCategory{}
				var wss []ovWorkspace
				wsIdx := map[string]int{}
				for _, w := range snap.Workspaces {
					wsIdx[w.ID] = len(wss)
					wss = append(wss, ovWorkspace{ID: w.ID, Name: w.Name, Icon: w.Icon, Wallpaper: w.Wallpaper, Categories: []ovCategory{}, Uncategorized: []ovLink{}})
				}
				for _, c := range snap.Categories {
					i, ok := wsIdx[c.WorkspaceID]
					if !ok {
						continue
					}
					wss[i].Categories = append(wss[i].Categories, ovCategory{ID: c.ID, Name: c.Name, Links: []ovLink{}})
				}
				for wi := range wss {
					for ci := range wss[wi].Categories {
						catIdx[wss[wi].Categories[ci].ID] = &wss[wi].Categories[ci]
					}
				}
				total := 0
				for _, l := range snap.Links {
					i, ok := wsIdx[l.WorkspaceID]
					if !ok {
						continue
					}
					total++
					if c, ok := catIdx[l.CategoryID]; ok && l.CategoryID != "" {
						c.Links = append(c.Links, toOv(l))
					} else {
						wss[i].Uncategorized = append(wss[i].Uncategorized, toOv(l))
					}
				}
				return M{"settings": snap.Settings, "workspaces": wss, "total_links": total}, nil
			},
		},
		{
			name: "search_links", title: "Search links", readOnly: true, idempotent: true,
			desc: "Fuzzy search links by title, URL, description, category or workspace name (all words must match). " +
				"按标题/地址/描述/分类/工作空间名称模糊搜索链接。",
			schema: obj(M{
				"query":        str("Search words, space separated. 搜索关键词，空格分隔。"),
				"workspace_id": str("Limit to one workspace (optional). 限定工作空间（可选）。"),
			}, "query"),
			fn: func(args json.RawMessage) (any, error) {
				var a struct {
					Query       string `json:"query"`
					WorkspaceID string `json:"workspace_id"`
				}
				if err := decode(args, &a); err != nil {
					return nil, err
				}
				words := strings.Fields(strings.ToLower(a.Query))
				if len(words) == 0 {
					return nil, errors.New("query is required")
				}
				snap := st.Snapshot()
				wsName := map[string]string{}
				for _, w := range snap.Workspaces {
					wsName[w.ID] = w.Name
				}
				catName := map[string]string{}
				for _, c := range snap.Categories {
					catName[c.ID] = c.Name
				}
				type hit struct {
					score int
					m     M
				}
				var hits []hit
				for _, l := range snap.Links {
					if a.WorkspaceID != "" && l.WorkspaceID != a.WorkspaceID {
						continue
					}
					title := strings.ToLower(l.Title)
					hay := title + "\n" + strings.ToLower(l.URL) + "\n" + strings.ToLower(l.Description) + "\n" +
						strings.ToLower(catName[l.CategoryID]) + "\n" + strings.ToLower(wsName[l.WorkspaceID])
					score := 0
					ok := true
					for _, w := range words {
						if !strings.Contains(hay, w) {
							ok = false
							break
						}
						score++
						if strings.Contains(title, w) {
							score += 3
						}
					}
					if !ok {
						continue
					}
					hits = append(hits, hit{score, M{
						"id": l.ID, "title": l.Title, "url": l.URL, "description": l.Description,
						"icon": l.Icon, "pinned": l.Pinned, "pin_sort": l.PinSort, "clicks": l.Clicks, "health_check": l.HealthCheck,
						"workspace_id": l.WorkspaceID, "workspace_name": wsName[l.WorkspaceID],
						"category_id": l.CategoryID, "category_name": catName[l.CategoryID],
					}})
				}
				sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
				if len(hits) > 50 {
					hits = hits[:50]
				}
				out := make([]M, len(hits))
				for i, h := range hits {
					out[i] = h.m
				}
				return M{"count": len(out), "links": out}, nil
			},
		},
		{
			name: "create_workspace", title: "Create workspace",
			desc: "Create a workspace (a top-level page/tab, e.g. \"Home\", \"Work\") with an optional built-in Lucide icon. 创建工作空间（顶层标签页），可选内置 Lucide 图标。",
			schema: obj(M{
				"name":      str("Workspace name, max 40 chars. 名称。"),
				"icon":      workspaceIconSchema("Icon shown next to the name (optional)."),
				"wallpaper": str("preset:aurora|dusk|ocean|forest|sakura|graphite (optional). 壁纸预设（可选）。"),
			}, "name"),
			fn: func(args json.RawMessage) (any, error) {
				var in store.WorkspaceInput
				if err := decode(args, &in); err != nil {
					return nil, err
				}
				w, err := st.CreateWorkspace(in)
				if err != nil {
					return nil, err
				}
				return M{"workspace": w}, nil
			},
		},
		{
			name: "update_workspace", title: "Update workspace", idempotent: true,
			desc:   "Rename a workspace or change its icon (a built-in Lucide icon, lucide:<name>) or wallpaper. Only given fields change. 修改工作空间名称/图标（内置 Lucide 图标）/壁纸，仅修改传入字段。",
			schema: obj(M{"id": str("Workspace id."), "name": str("New name."), "icon": workspaceIconSchema("New icon."), "wallpaper": str("preset:<id> (aurora, dusk, ocean, forest, sakura, graphite).")}, "id"),
			fn: func(args json.RawMessage) (any, error) {
				var a struct {
					ID string `json:"id"`
					store.WorkspacePatch
				}
				if err := decode(args, &a); err != nil {
					return nil, err
				}
				w, err := st.UpdateWorkspace(a.ID, a.WorkspacePatch)
				if err != nil {
					return nil, err
				}
				return M{"workspace": w}, nil
			},
		},
		{
			name: "delete_workspace", title: "Delete workspace", destructive: true,
			desc:   "Delete a workspace together with all its categories and links. The last workspace cannot be deleted. Only do this when the user asked. 删除工作空间及其全部分类和链接（不可删除最后一个）；仅在用户明确要求时使用。",
			schema: obj(M{"id": str("Workspace id.")}, "id"),
			fn: func(args json.RawMessage) (any, error) {
				var a struct {
					ID string `json:"id"`
				}
				if err := decode(args, &a); err != nil {
					return nil, err
				}
				if err := st.DeleteWorkspace(a.ID); err != nil {
					return nil, err
				}
				return okResult(M{"ok": true, "deleted": a.ID})
			},
		},
		{
			name: "create_category", title: "Create category",
			desc:   "Create a category (a titled group of links) inside a workspace, e.g. \"Media\", \"Monitoring\". 在工作空间内创建分类（链接分组）。",
			schema: obj(M{"workspace_id": str("Workspace id."), "name": str("Category name, max 60 chars.")}, "workspace_id", "name"),
			fn: func(args json.RawMessage) (any, error) {
				var a struct {
					WorkspaceID string `json:"workspace_id"`
					Name        string `json:"name"`
				}
				if err := decode(args, &a); err != nil {
					return nil, err
				}
				c, err := st.CreateCategory(a.WorkspaceID, a.Name)
				if err != nil {
					return nil, err
				}
				return M{"category": c}, nil
			},
		},
		{
			name: "update_category", title: "Rename category", idempotent: true,
			desc:   "Rename a category. 重命名分类。",
			schema: obj(M{"id": str("Category id."), "name": str("New name.")}, "id", "name"),
			fn: func(args json.RawMessage) (any, error) {
				var a struct {
					ID   string  `json:"id"`
					Name *string `json:"name"`
				}
				if err := decode(args, &a); err != nil {
					return nil, err
				}
				if a.Name == nil {
					return nil, errors.New("name is required")
				}
				c, err := st.UpdateCategory(a.ID, a.Name)
				if err != nil {
					return nil, err
				}
				return M{"category": c}, nil
			},
		},
		{
			name: "delete_category", title: "Delete category", destructive: true,
			desc: "Delete a category. By default its links are kept and become uncategorized; set delete_links=true to delete them too. Only do this when the user asked. " +
				"删除分类；默认其链接变为未分类，delete_links=true 则一并删除；仅在用户明确要求时使用。",
			schema: obj(M{"id": str("Category id."), "delete_links": boolean("Also delete the links in this category (default false).")}, "id"),
			fn: func(args json.RawMessage) (any, error) {
				var a struct {
					ID          string `json:"id"`
					DeleteLinks bool   `json:"delete_links"`
				}
				if err := decode(args, &a); err != nil {
					return nil, err
				}
				if err := st.DeleteCategory(a.ID, a.DeleteLinks); err != nil {
					return nil, err
				}
				return okResult(M{"ok": true, "deleted": a.ID, "links_deleted": a.DeleteLinks})
			},
		},
		{
			name: "create_links", title: "Create links (batch)",
			desc: "Create many links at once (max 200). Icons are fetched automatically in the background when icon is empty. Items whose URL already exists in the same workspace are skipped as duplicates. Each item is reported individually. " +
				"批量创建链接（最多 200 个），icon 为空时后台自动抓取图标；同一工作空间内 URL 重复的条目会被跳过；逐条返回结果。",
			schema: obj(M{
				"links": M{
					"type": "array", "minItems": 1, "maxItems": 200,
					"description": "Links to create. 要创建的链接列表。",
					"items": obj(merge(M{
						"workspace_id": str("Target workspace id (required). 目标工作空间 id。"),
						"category_id":  str("Target category id in that workspace; omit for uncategorized. 目标分类 id，省略为未分类。"),
					}, linkProps()), "workspace_id", "title", "url"),
				},
			}, "links"),
			fn: func(args json.RawMessage) (any, error) {
				var a struct {
					Links []store.LinkInput `json:"links"`
				}
				if err := decode(args, &a); err != nil {
					return nil, err
				}
				if len(a.Links) == 0 {
					return nil, errors.New("links must not be empty")
				}
				if len(a.Links) > 200 {
					return nil, errors.New("too many links (max 200 per call)")
				}
				snap := st.Snapshot()
				seen := map[string]string{} // ws|url -> existing link id
				for _, l := range snap.Links {
					seen[l.WorkspaceID+"\x00"+normURL(l.URL)] = l.ID
				}
				type errItem struct {
					Index int    `json:"index"`
					Title string `json:"title"`
					Error string `json:"error"`
				}
				var errs []errItem
				var todo []store.LinkInput
				var todoIdx []int
				for i, in := range a.Links {
					if u, err := store.CleanURL(in.URL); err == nil {
						k := in.WorkspaceID + "\x00" + normURL(u)
						if id, dup := seen[k]; dup {
							errs = append(errs, errItem{i, in.Title, "duplicate: a link with this URL already exists in the workspace (id " + id + ")"})
							continue
						}
						seen[k] = "(this batch)"
					}
					todo = append(todo, in)
					todoIdx = append(todoIdx, i)
				}
				var created []store.Link
				if len(todo) > 0 {
					res, err := st.CreateLinks(todo)
					for k, r := range res {
						if r.Err != nil {
							errs = append(errs, errItem{todoIdx[k], todo[k].Title, r.Err.Error()})
						} else {
							created = append(created, r.Link)
						}
					}
					if err != nil && len(created) == 0 {
						return nil, err
					}
				}
				if len(created) > 0 && s.OnLinksCreated != nil {
					s.OnLinksCreated(created)
				}
				sort.Slice(errs, func(i, j int) bool { return errs[i].Index < errs[j].Index })
				if created == nil {
					created = []store.Link{}
				}
				if errs == nil {
					errs = []errItem{}
				}
				out := M{"created": created, "created_count": len(created), "errors": errs}
				if len(created) == 0 {
					return isErr{out}, nil
				}
				return out, nil
			},
		},
		{
			name: "update_link", title: "Update link", idempotent: true,
			desc: "Edit a link. Only given fields change. Move it by setting workspace_id and/or category_id (category_id \"\" = uncategorized). " +
				"修改链接，仅更新传入字段；通过 workspace_id / category_id 移动（category_id 为空串表示未分类）。",
			schema: obj(merge(M{
				"id":           str("Link id."),
				"workspace_id": str("Move to this workspace (category is then cleared unless category_id is also given)."),
				"category_id":  str("Move to this category; \"\" for uncategorized."),
			}, linkProps()), "id"),
			fn: func(args json.RawMessage) (any, error) {
				var a struct {
					ID string `json:"id"`
					store.LinkPatch
				}
				if err := decode(args, &a); err != nil {
					return nil, err
				}
				l, err := st.UpdateLink(a.ID, a.LinkPatch)
				if err != nil {
					return nil, err
				}
				return M{"link": l}, nil
			},
		},
		{
			name: "delete_links", title: "Delete links", destructive: true,
			desc:   "Delete links by id (batch). Only do this when the user asked. 批量删除链接；仅在用户明确要求时使用。",
			schema: obj(M{"ids": strArr("Link ids to delete. 要删除的链接 id 列表。")}, "ids"),
			fn: func(args json.RawMessage) (any, error) {
				var a struct {
					IDs []string `json:"ids"`
				}
				if err := decode(args, &a); err != nil {
					return nil, err
				}
				if len(a.IDs) == 0 {
					return nil, errors.New("ids must not be empty")
				}
				n, missing, err := st.DeleteLinks(a.IDs)
				if err != nil {
					return nil, err
				}
				if missing == nil {
					missing = []string{}
				}
				return M{"deleted": n, "not_found": missing}, nil
			},
		},
		{
			name: "reorder", title: "Reorder", idempotent: true,
			desc: "Set the custom order (and for links, the placement) of items. kind=workspaces: ids = workspace ids. kind=categories: workspace_id + ids = category ids. " +
				"kind=links: workspace_id + category_id (\"\" = uncategorized) + ids = link ids in desired order; listed links are moved into that category (pinned links are normal members of their category) and their `sort` is rewritten. Unlisted siblings keep their relative order after the listed ones. " +
				"With category_id \"__pinned\" only the pinned row order (pin_sort) is rewritten and listed links become pinned; their category and category order are untouched. " +
				"设置自定义顺序；kind=links 时被列出的链接会移动到指定分类（置顶链接同样属于其分类）；category_id 为 \"__pinned\" 时仅调整置顶行顺序（pin_sort）。",
			schema: obj(M{
				"kind":         M{"type": "string", "enum": []string{"workspaces", "categories", "links"}, "description": "What to reorder. 排序对象。"},
				"ids":          strArr("Ids in the desired order. 目标顺序的 id 列表。"),
				"workspace_id": str("Required for categories and links."),
				"category_id":  str("For links: category id, \"\" for uncategorized, \"__pinned\" to reorder only the pinned row (pin_sort)."),
			}, "kind", "ids"),
			fn: func(args json.RawMessage) (any, error) {
				var a struct {
					Kind        string   `json:"kind"`
					IDs         []string `json:"ids"`
					WorkspaceID string   `json:"workspace_id"`
					CategoryID  string   `json:"category_id"`
				}
				if err := decode(args, &a); err != nil {
					return nil, err
				}
				var err error
				switch a.Kind {
				case "workspaces":
					err = st.ReorderWorkspaces(a.IDs)
				case "categories":
					if a.WorkspaceID == "" {
						return nil, errors.New("workspace_id is required for kind=categories")
					}
					err = st.ReorderCategories(a.WorkspaceID, a.IDs)
				case "links":
					if a.WorkspaceID == "" {
						return nil, errors.New("workspace_id is required for kind=links")
					}
					err = st.ReorderLinks(a.WorkspaceID, a.CategoryID, a.IDs)
				default:
					return nil, errors.New(`kind must be "workspaces", "categories" or "links"`)
				}
				if err != nil {
					return nil, err
				}
				return okResult(M{"ok": true})
			},
		},
		{
			name: "update_settings", title: "Update settings", idempotent: true,
			desc: "Change site settings. Only given fields change. 修改站点设置，仅修改传入字段。",
			schema: obj(M{
				"title":           str("Site title."),
				"sort_mode":       M{"type": "string", "enum": []string{"frequency", "recent", "custom"}, "description": "Link ordering: by click count, by last use, or custom order."},
				"guest_access":    boolean("Allow read-only viewing without login."),
				"search_engine":   str("Web search URL template containing %s, e.g. https://www.google.com/search?q=%s"),
				"open_in_new_tab": boolean("Open links in a new tab."),
				"health_interval": M{"type": "integer", "minimum": 15, "description": "Health check interval in seconds (min 15)."},
				"show_clock":      boolean("Show the clock on the home page."),
			}),
			fn: func(args json.RawMessage) (any, error) {
				var p store.SettingsPatch
				if err := decode(args, &p); err != nil {
					return nil, err
				}
				ns, err := st.UpdateSettings(p)
				if err != nil {
					return nil, err
				}
				return M{"settings": ns}, nil
			},
		},
	}
}

func normURL(u string) string {
	u = strings.ToLower(strings.TrimSpace(u))
	if !strings.Contains(u, "://") {
		u = "http://" + u
	}
	return strings.TrimRight(u, "/")
}
