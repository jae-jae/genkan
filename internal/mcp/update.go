package mcp

import (
	"encoding/json"
	"errors"
)

// updateTools is the read-only view of the self-update status (docs/UPDATES.md §6).
// Applying an update is deliberately not exposed through MCP.
func (s *Server) updateTools() []tool {
	return []tool{
		{
			name: "update_status", title: "Update status", readOnly: true, idempotent: true,
			desc: "Report the running Genkan version and whether a newer release is available: current, latest, available, the list of newer releases (version, name, release notes, date, url), when GitHub was last checked, the last check error, the dismissed version, whether the server can apply an update itself (apply.supported / apply.reason), the state of a running update job and a rolled_back version when an update failed and was undone. Read-only: it does not check GitHub and cannot install anything; updates are applied by the admin in the web UI. " +
				"返回当前 Genkan 版本及是否有新版本（含更新日志）。只读，无法通过 MCP 安装更新，需管理员在网页中操作。",
			schema: obj(M{}),
			fn: func(json.RawMessage) (any, error) {
				if s.Updates == nil {
					return nil, errors.New("updates are not available")
				}
				return s.Updates.Status(), nil
			},
		},
	}
}
