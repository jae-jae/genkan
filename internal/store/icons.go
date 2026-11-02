package store

import "strings"

// WorkspaceIconPrefix prefixes every workspace icon value ("lucide:house").
const WorkspaceIconPrefix = "lucide:"

// DefaultWorkspaceIcon is used when a workspace is created without an icon and
// as the migration fallback for unknown legacy emoji.
const DefaultWorkspaceIcon = "lucide:layout-grid"

// firstWorkspaceIcon is the icon of the workspace created on first run.
const firstWorkspaceIcon = "lucide:house"

// BuiltinIcons are the Lucide icon names a workspace may use (52 names).
// The frontend ships the matching SVG paths.
var BuiltinIcons = []string{
	"house", "layout-grid", "folder", "briefcase", "code", "terminal", "server", "database",
	"hard-drive", "cloud", "network", "router", "wifi", "globe", "shield", "lock",
	"key-round", "film", "clapperboard", "tv", "music", "headphones", "image", "camera",
	"gamepad-2", "book-open", "graduation-cap", "newspaper", "shopping-cart", "wallet", "chart-line", "activity",
	"gauge", "cpu", "wrench", "settings", "bot", "sparkles", "heart", "star",
	"bookmark", "users", "message-circle", "mail", "calendar", "download", "flask-conical", "leaf",
	"coffee", "plane", "lightbulb", "rocket",
}

var builtinIconSet = func() map[string]bool {
	m := make(map[string]bool, len(BuiltinIcons))
	for _, n := range BuiltinIcons {
		m[n] = true
	}
	return m
}()

// legacyEmojiIcons maps the emoji workspace icons of earlier versions to Lucide names.
var legacyEmojiIcons = map[string]string{
	"🏠": "house", "🏡": "house",
	"🎬": "clapperboard", "🎞": "clapperboard", "🎞️": "clapperboard", "📺": "clapperboard",
	"🛠": "wrench", "🛠️": "wrench", "🔧": "wrench", "⚙️": "wrench", "⚙": "wrench",
	"🌐": "globe", "🌍": "globe", "🌎": "globe",
	"💼": "briefcase",
	"🎮": "gamepad-2",
	"🎵": "music", "🎶": "music",
	"📚": "book-open", "📖": "book-open",
	"💻": "code", "🖥": "code", "🖥️": "code",
	"☁️": "cloud", "☁": "cloud",
	"🔒": "lock", "🔐": "lock",
	"⭐": "star", "⭐️": "star",
	"❤️": "heart", "❤": "heart",
}

// migrateWorkspaceIcon converts a legacy (non-lucide) icon to "lucide:<name>".
// Valid lucide icons are returned unchanged; anything unrecognised becomes the default.
func migrateWorkspaceIcon(icon string) string {
	icon = strings.TrimSpace(icon)
	if strings.HasPrefix(icon, WorkspaceIconPrefix) {
		if builtinIconSet[icon[len(WorkspaceIconPrefix):]] {
			return icon
		}
		return DefaultWorkspaceIcon
	}
	if n, ok := legacyEmojiIcons[icon]; ok {
		return WorkspaceIconPrefix + n
	}
	return DefaultWorkspaceIcon
}

// cleanWorkspaceIcon validates a workspace icon: empty means the default,
// otherwise it must be lucide:<builtin name>.
func cleanWorkspaceIcon(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return DefaultWorkspaceIcon, nil
	}
	if !strings.HasPrefix(s, WorkspaceIconPrefix) || !builtinIconSet[s[len(WorkspaceIconPrefix):]] {
		return "", invalid("workspace icon must be lucide:<name> with a built-in icon name (e.g. lucide:house)")
	}
	return s, nil
}
