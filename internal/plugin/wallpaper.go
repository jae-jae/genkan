package plugin

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jae-jae/genkan/internal/media"
	"github.com/jae-jae/genkan/internal/store"
)

const (
	maxWallpaperBytes = 15 << 20
	keepWallpapers    = 5 // most recent images kept per wallpaper source
	downloadTimeout   = 30 * time.Second
)

// WallpaperInfo is the response of GET /api/plugins/wallpaper/{plugin}/{source}.
type WallpaperInfo struct {
	URL       string `json:"url"`
	Title     string `json:"title,omitempty"`
	Credit    string `json:"credit,omitempty"`
	Link      string `json:"link,omitempty"`
	UpdatedAt int64  `json:"updated_at"`
}

func (m *Manager) wallpaperDir() string {
	return filepath.Join(m.cfg.DataDir, "uploads", store.PluginWallpaperDir)
}

// Wallpaper returns the current wallpaper of a source. The hook result is
// cached for the source's refresh interval; the image is downloaded once,
// verified and served from this server.
func (m *Manager) Wallpaper(ctx context.Context, plugin, source, lang string) (WallpaperInfo, error) {
	e, rec, err := m.ready(plugin)
	if err != nil {
		return WallpaperInfo{}, err
	}
	d := e.man.wallpaper(source)
	if d == nil {
		return WallpaperInfo{}, notFoundf("plugin %s has no wallpaper source %q", plugin, source)
	}
	key := "wp|" + plugin + "|" + source
	stamp := hashJSON(rec.Rev, rec.Config)
	if it := m.cached(key, stamp); it != nil {
		if it.err != nil {
			return WallpaperInfo{}, it.err
		}
		return it.data.(WallpaperInfo), nil
	}
	v, err, _ := m.fl.do(key+"|"+stamp, func() (any, error) {
		info, err := m.fetchWallpaper(e, rec, source, NormLang(lang))
		if err != nil {
			m.putCache(key, &cacheItem{stamp: stamp, err: err, errTill: time.Now().Add(errorCacheTTL)})
			return nil, err
		}
		info.UpdatedAt = nowMs()
		m.putCache(key, &cacheItem{stamp: stamp, data: info, updated: info.UpdatedAt, next: info.UpdatedAt + int64(d.Refresh)*1000})
		return info, nil
	})
	if err != nil {
		return WallpaperInfo{}, waitErr(ctx, err)
	}
	return v.(WallpaperInfo), nil
}

func (m *Manager) fetchWallpaper(e *entry, rec store.Plugin, source, lang string) (WallpaperInfo, error) {
	raw, err := m.call(m.ctx, e, rec, hookReq{
		hook: hookWallpaper, lang: lang, timeout: timeoutDefault,
		fields: map[string]any{"source": source},
	})
	if err != nil {
		return WallpaperInfo{}, err
	}
	wr, ok := SanitizeWallpaper(raw, rand.IntN)
	if !ok {
		err := hookErr("wallpaper() must return { url } (http(s) or data:image) or an array of those")
		e.logs.add("error", err.Error(), nil)
		return WallpaperInfo{}, err
	}
	cfg := withDefaults(e.man.ConfigSchema, rec.Config)
	secrets := secretValues(e.man.ConfigSchema, rec.Config)
	allow := newAllowList(e.man.Permissions.Fetch, fetchHostsFromConfig(e.man.ConfigSchema, cfg))
	file, err := m.storeWallpaper(m.ctx, wr.URL, allow, fetchHostsFromConfig(e.man.ConfigSchema, cfg), store.PluginSourcePrefix(rec.ID, source))
	if err != nil {
		err = hookErr("could not download the wallpaper: %s", redactSecrets(err.Error(), secrets))
		e.logs.add("error", err.Error(), secrets)
		e.setLastError(err.Error())
		return WallpaperInfo{}, err
	}
	e.setLastError("")
	return WallpaperInfo{URL: "/uploads/" + store.PluginWallpaperDir + "/" + file, Title: wr.Title, Credit: wr.Credit, Link: wr.Link}, nil
}

// storeWallpaper obtains the image (data: URL or an http(s) download through
// the plugin's fetch allow list), sniffs it, saves it under the source's
// prefix and prunes older images. Returns the file name.
func (m *Manager) storeWallpaper(ctx context.Context, src string, allow *allowList, lenient []string, prefix string) (string, error) {
	var data []byte
	if strings.HasPrefix(src, "data:") {
		_, b64, _ := strings.Cut(src, ",")
		b, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return "", fmt.Errorf("invalid data URL")
		}
		data = b
	} else {
		tr := newHostTransport()
		defer tr.close()
		tr.lenients.Store(newAllowList(lenient))
		client := newClient(tr, allow.check)
		dctx, cancel := context.WithTimeout(ctx, downloadTimeout)
		defer cancel()
		req, err := http.NewRequestWithContext(dctx, http.MethodGet, src, nil)
		if err != nil {
			return "", err
		}
		if err := allow.check(req); err != nil {
			return "", err
		}
		req.Header.Set("Accept", "image/avif,image/webp,image/png,image/jpeg,*/*;q=0.5")
		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("%s", trimErr(err))
		}
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			return "", fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		data, err = io.ReadAll(io.LimitReader(resp.Body, maxWallpaperBytes+1))
		if err != nil {
			return "", err
		}
	}
	if len(data) > maxWallpaperBytes {
		return "", fmt.Errorf("image is larger than %d MiB", maxWallpaperBytes>>20)
	}
	ext, _, ok := media.Sniff(data)
	if !ok || (ext != "jpg" && ext != "png" && ext != "webp" && ext != "avif") {
		return "", fmt.Errorf("not a jpg, png, webp or avif image")
	}
	dir := m.wallpaperDir()
	name, err := media.SaveNamed(dir, prefix, data, ext)
	if err != nil {
		return "", err
	}
	now := time.Now()
	_ = os.Chtimes(filepath.Join(dir, name), now, now) // the current image is always the newest
	pruneWallpapers(dir, prefix, keepWallpapers)
	return name, nil
}

// pruneWallpapers keeps the newest keep files with the given prefix.
func pruneWallpapers(dir, prefix string, keep int) {
	files, err := media.List(dir) // newest first
	if err != nil {
		return
	}
	n := 0
	for _, f := range files {
		if !strings.HasPrefix(f.Name, prefix) {
			continue
		}
		if n++; n > keep {
			_ = os.Remove(filepath.Join(dir, f.Name))
		}
	}
}
