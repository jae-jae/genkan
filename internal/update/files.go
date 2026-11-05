package update

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// File layout under <data>/.update (docs/UPDATES.md §4, §5):
//
//	check.json            persisted release check
//	state.json            pending/confirmed update
//	rolled_back.json      notice that an update was rolled back
//	<version>/genkan      the unpacked new binary
//	backup-<version>/     genkan.json + plugins/ before the update

const (
	dirName        = ".update"
	checkFile      = "check.json"
	stateFile      = "state.json"
	rolledBackFile = "rolled_back.json"
	storeFile      = "genkan.json"
	pluginsDir     = "plugins"
)

func updateDir(dataDir string) string { return filepath.Join(dataDir, dirName) }

// State is <data>/.update/state.json.
type State struct {
	Version  string `json:"version"`
	Status   string `json:"status"` // "pending" | "confirmed"
	Attempts int    `json:"attempts"`
	From     string `json:"from"`
	Backup   string `json:"backup"`
	At       int64  `json:"at"`
}

const (
	statusPending   = "pending"
	statusConfirmed = "confirmed"
)

func backupName(version string) string { return "backup-" + version }

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func writeJSONFile(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return atomicWrite(path, b, 0o600)
}

// atomicWrite writes via a temp file in the same directory, fsync and rename.
func atomicWrite(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	fail := func(err error) error {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, perm); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

func readState(dataDir string) (State, error) {
	var st State
	err := readJSON(filepath.Join(updateDir(dataDir), stateFile), &st)
	return st, err
}

func writeState(dataDir string, st State) error {
	return writeJSONFile(filepath.Join(updateDir(dataDir), stateFile), st)
}

// cleanup removes everything under <data>/.update except check.json.
func cleanup(dataDir string) {
	dir := updateDir(dataDir)
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		if e.Name() == checkFile {
			continue
		}
		_ = os.RemoveAll(filepath.Join(dir, e.Name()))
	}
}

// Backup ------------------------------------------------------------------------------

// backupStore copies the store state (genkan.json and the plugins directory:
// plugin sources and key/value files) to <data>/.update/backup-<version>/.
// Uploaded images are not part of it: an update never rewrites them.
func backupStore(dataDir, version string) error {
	final := filepath.Join(updateDir(dataDir), backupName(version))
	tmp := final + ".tmp"
	if err := os.RemoveAll(tmp); err != nil {
		return err
	}
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return err
	}
	if err := copyFile(filepath.Join(dataDir, storeFile), filepath.Join(tmp, storeFile), 0o600); err != nil {
		os.RemoveAll(tmp)
		return fmt.Errorf("back up %s: %w", storeFile, err)
	}
	if err := copyTree(filepath.Join(dataDir, pluginsDir), filepath.Join(tmp, pluginsDir)); err != nil {
		os.RemoveAll(tmp)
		return fmt.Errorf("back up %s: %w", pluginsDir, err)
	}
	if err := os.RemoveAll(final); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	return os.Rename(tmp, final)
}

// restoreStore puts a backup made by backupStore back in place.
func restoreStore(dataDir, backupDir string) error {
	if _, err := os.Stat(filepath.Join(backupDir, storeFile)); err != nil {
		return fmt.Errorf("backup is incomplete: %w", err)
	}
	// plugins first: if it fails halfway genkan.json still describes the new state
	live := filepath.Join(dataDir, pluginsDir)
	if err := os.RemoveAll(live); err != nil {
		return err
	}
	if err := copyTree(filepath.Join(backupDir, pluginsDir), live); err != nil {
		return err
	}
	return copyFile(filepath.Join(backupDir, storeFile), filepath.Join(dataDir, storeFile), 0o600)
}

// copyTree copies regular files and directories; a missing src is not an error.
func copyTree(src, dst string) error {
	if _, err := os.Stat(src); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case d.Type().IsRegular():
			if strings.HasPrefix(d.Name(), ".tmp-") { // in-flight atomic writes
				return nil
			}
			return copyFile(p, target, 0o600)
		}
		return nil
	})
}

// copyFile copies src to dst atomically.
func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, perm); err != nil {
		os.Remove(name)
		return err
	}
	return os.Rename(name, dst)
}
