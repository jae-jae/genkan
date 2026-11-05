package update

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type launchFix struct {
	t     *testing.T
	data  string
	calls []execCall
	fail  error
	l     Launcher
}

// newLaunchFix prepares a data dir with a store, a plugin, and an installed v1.2.0.
func newLaunchFix(t *testing.T, version string, st *State) *launchFix {
	t.Helper()
	f := &launchFix{t: t, data: t.TempDir()}
	writeFile(t, filepath.Join(f.data, "genkan.json"), `{"v":"old"}`)
	writeFile(t, filepath.Join(f.data, "plugins", "p.js"), "old plugin")
	f.l = Launcher{
		DataDir: f.data, Version: version, Args: []string{"/genkan", "-x"}, Environ: []string{"A=1"},
		Exec: func(path string, argv, env []string) error {
			f.calls = append(f.calls, execCall{path, argv, env})
			return f.fail
		},
		Now:  func() time.Time { return time.UnixMilli(1762300000000) },
		Logf: func(string, ...any) {},
	}
	if st != nil {
		f.install(*st)
	}
	return f
}

// install writes the binary, a backup of the old data, then changes the live data (as the new version would).
func (f *launchFix) install(st State) {
	dir := filepath.Join(f.data, ".update")
	writeFile(f.t, filepath.Join(dir, st.Version, "genkan"), "bin")
	os.Chmod(filepath.Join(dir, st.Version, "genkan"), 0o755)
	if err := backupStore(f.data, st.Version); err != nil {
		f.t.Fatal(err)
	}
	writeFile(f.t, filepath.Join(f.data, "genkan.json"), `{"v":"new"}`)
	writeFile(f.t, filepath.Join(f.data, "plugins", "p.js"), "new plugin")
	writeFile(f.t, filepath.Join(f.data, "plugins", "extra.js"), "added by the new version")
	writeFile(f.t, filepath.Join(dir, "check.json"), `{"api":"x"}`)
	if st.Backup == "" {
		st.Backup = backupName(st.Version)
	}
	if err := writeState(f.data, st); err != nil {
		f.t.Fatal(err)
	}
}

func (f *launchFix) state() (State, bool) {
	st, err := readState(f.data)
	return st, err == nil
}

func (f *launchFix) exists(rel string) bool {
	_, err := os.Stat(filepath.Join(f.data, ".update", rel))
	return err == nil
}

func pending(attempts int) *State {
	return &State{Version: "v1.2.0", Status: "pending", Attempts: attempts, From: "v1.0.0"}
}

func TestLauncherNewerPendingExecs(t *testing.T) {
	f := newLaunchFix(t, "v1.0.0", pending(0))
	if got := f.l.Run(); got != OutcomeExec {
		t.Fatalf("outcome %s", got)
	}
	if len(f.calls) != 1 {
		t.Fatalf("calls %v", f.calls)
	}
	c := f.calls[0]
	if c.path != filepath.Join(f.data, ".update", "v1.2.0", "genkan") || c.argv[0] != c.path || strings.Join(c.argv[1:], " ") != "-x" {
		t.Fatalf("exec %+v", c)
	}
	if strings.Join(c.envs, ",") != "A=1,GENKAN_LAUNCHED=1" {
		t.Fatalf("env %v", c.envs)
	}
	if st, _ := f.state(); st.Attempts != 1 || st.Status != "pending" {
		t.Fatalf("state %+v", st)
	}
	if readFile(t, filepath.Join(f.data, "genkan.json")) != `{"v":"new"}` {
		t.Fatal("data touched")
	}
}

func TestLauncherConfirmedExecsWithoutCounting(t *testing.T) {
	f := newLaunchFix(t, "v1.0.0", &State{Version: "v1.2.0", Status: "confirmed", From: "v1.0.0"})
	for i := 0; i < 3; i++ {
		if got := f.l.Run(); got != OutcomeExec {
			t.Fatalf("outcome %s", got)
		}
	}
	if st, _ := f.state(); st.Attempts != 0 || st.Status != "confirmed" {
		t.Fatalf("state %+v", st)
	}
}

func TestLauncherSkips(t *testing.T) {
	// no state
	f := newLaunchFix(t, "v1.0.0", nil)
	if f.l.Run() != OutcomeSkip || len(f.calls) != 0 {
		t.Fatal("no state")
	}
	// already launched
	f = newLaunchFix(t, "v1.0.0", pending(0))
	f.l.Environ = []string{"GENKAN_LAUNCHED=1"}
	if f.l.Run() != OutcomeSkip || len(f.calls) != 0 {
		t.Fatal("launched")
	}
	if st, _ := f.state(); st.Attempts != 0 {
		t.Fatal("attempt counted for a launched process")
	}
	// dev build never launches
	f = newLaunchFix(t, "dev", pending(0))
	if f.l.Run() != OutcomeSkip || len(f.calls) != 0 || !f.exists("state.json") {
		t.Fatal("dev")
	}
}

func TestLauncherOlderOrEqualCleansUp(t *testing.T) {
	for _, running := range []string{"v1.2.0", "v1.3.0"} {
		f := newLaunchFix(t, running, pending(0))
		writeFile(t, filepath.Join(f.data, ".update", "v1.1.0", "genkan"), "stale")
		if got := f.l.Run(); got != OutcomeCleanup {
			t.Fatalf("%s: outcome %s", running, got)
		}
		if len(f.calls) != 0 {
			t.Fatal("exec")
		}
		ents, _ := os.ReadDir(filepath.Join(f.data, ".update"))
		if len(ents) != 1 || ents[0].Name() != "check.json" {
			t.Fatalf("%s: left over %v", running, ents)
		}
		// data stays as is: no rollback happens in this case
		if readFile(t, filepath.Join(f.data, "genkan.json")) != `{"v":"new"}` {
			t.Fatal("data restored")
		}
	}
}

func TestLauncherBinaryMissingOrNotExecutable(t *testing.T) {
	f := newLaunchFix(t, "v1.0.0", pending(0))
	os.Remove(filepath.Join(f.data, ".update", "v1.2.0", "genkan"))
	if got := f.l.Run(); got != OutcomeDropped {
		t.Fatalf("outcome %s", got)
	}
	if _, ok := f.state(); ok || len(f.calls) != 0 {
		t.Fatal("state kept or exec")
	}
	if !f.exists("check.json") {
		t.Fatal("check.json removed")
	}
	if f.l.Run() != OutcomeSkip {
		t.Fatal("second start runs as normal")
	}

	f = newLaunchFix(t, "v1.0.0", pending(0))
	os.Chmod(filepath.Join(f.data, ".update", "v1.2.0", "genkan"), 0o644)
	if got := f.l.Run(); got != OutcomeDropped || len(f.calls) != 0 {
		t.Fatalf("not executable: %s", got)
	}
	// a directory in place of the binary
	f = newLaunchFix(t, "v1.0.0", pending(0))
	os.Remove(filepath.Join(f.data, ".update", "v1.2.0", "genkan"))
	os.MkdirAll(filepath.Join(f.data, ".update", "v1.2.0", "genkan"), 0o755)
	if got := f.l.Run(); got != OutcomeDropped {
		t.Fatalf("dir: %s", got)
	}
}

func TestLauncherRollback(t *testing.T) {
	f := newLaunchFix(t, "v1.0.0", pending(1))
	if got := f.l.Run(); got != OutcomeRolledBack {
		t.Fatalf("outcome %s", got)
	}
	if len(f.calls) != 0 {
		t.Fatal("exec after a failed attempt")
	}
	if readFile(t, filepath.Join(f.data, "genkan.json")) != `{"v":"old"}` || readFile(t, filepath.Join(f.data, "plugins", "p.js")) != "old plugin" {
		t.Fatal("backup not restored")
	}
	if _, err := os.Stat(filepath.Join(f.data, "plugins", "extra.js")); err == nil {
		t.Fatal("file added by the new version survived the rollback")
	}
	if f.exists("v1.2.0") || f.exists("backup-v1.2.0") || f.exists("state.json") {
		t.Fatal("version dir/backup/state left")
	}
	var rb struct {
		Version string `json:"version"`
		At      int64  `json:"at"`
	}
	b, _ := os.ReadFile(filepath.Join(f.data, ".update", "rolled_back.json"))
	if json.Unmarshal(b, &rb) != nil || rb.Version != "v1.2.0" || rb.At != 1762300000000 {
		t.Fatalf("rolled_back.json = %s", b)
	}
	if !f.exists("check.json") {
		t.Fatal("check.json removed")
	}
	// the status reports it once, then dismiss clears it
	s := New(Config{DataDir: f.data, Version: "v1.0.0", Getenv: func(string) string { return "" }, GOOS: "linux", GOARCH: "amd64", NoExec: func(string) bool { return false }})
	if s.Status().RolledBack != "v1.2.0" {
		t.Fatal("status.rolled_back")
	}
	s.ClearRolledBack()
	if s.Status().RolledBack != "" {
		t.Fatal("not cleared")
	}
	// next start runs as normal
	if f.l.Run() != OutcomeSkip {
		t.Fatal("start after rollback")
	}
}

func TestLauncherRollbackWithoutPluginsDirInBackup(t *testing.T) {
	f := newLaunchFix(t, "v1.0.0", nil)
	os.RemoveAll(filepath.Join(f.data, "plugins"))
	f.install(*pending(2)) // creates plugins/ afterwards, as the new version might have
	os.RemoveAll(filepath.Join(f.data, ".update", "backup-v1.2.0", "plugins"))
	if f.l.Run() != OutcomeRolledBack {
		t.Fatal("rollback")
	}
	if _, err := os.Stat(filepath.Join(f.data, "plugins", "extra.js")); err == nil {
		t.Fatal("plugins dir not reset")
	}
}

func TestLauncherExecFailureRollsBack(t *testing.T) {
	f := newLaunchFix(t, "v1.0.0", pending(0))
	f.fail = os.ErrPermission
	if got := f.l.Run(); got != OutcomeRolledBack {
		t.Fatalf("outcome %s", got)
	}
	if readFile(t, filepath.Join(f.data, "genkan.json")) != `{"v":"old"}` || f.exists("state.json") || !f.exists("rolled_back.json") {
		t.Fatal("not rolled back")
	}
	// a confirmed version that cannot be exec'd is dropped without touching data
	f = newLaunchFix(t, "v1.0.0", &State{Version: "v1.2.0", Status: "confirmed"})
	f.fail = os.ErrPermission
	if got := f.l.Run(); got != OutcomeDropped {
		t.Fatalf("confirmed: %s", got)
	}
	if readFile(t, filepath.Join(f.data, "genkan.json")) != `{"v":"new"}` || f.exists("state.json") {
		t.Fatal("confirmed handling")
	}
}

func TestLauncherCorruptState(t *testing.T) {
	f := newLaunchFix(t, "v1.0.0", nil)
	writeFile(t, filepath.Join(f.data, ".update", "state.json"), "{not json")
	if f.l.Run() != OutcomeSkip || f.exists("state.json") {
		t.Fatal("corrupt state should be dropped")
	}
	// a state with a hostile version string is "not newer" and cleaned, never joined into a path
	f = newLaunchFix(t, "v1.0.0", nil)
	writeFile(t, filepath.Join(f.data, ".update", "state.json"), `{"version":"../../etc","status":"pending","attempts":5}`)
	if f.l.Run() != OutcomeCleanup {
		t.Fatal("hostile version")
	}
}

func TestBackupStore(t *testing.T) {
	d := t.TempDir()
	writeFile(t, filepath.Join(d, "genkan.json"), "J")
	writeFile(t, filepath.Join(d, "plugins", "a.js"), "A")
	writeFile(t, filepath.Join(d, "plugins", "a.kv.json"), "K")
	writeFile(t, filepath.Join(d, "plugins", ".tmp-123"), "partial")
	writeFile(t, filepath.Join(d, "uploads", "icons", "i.png"), "IMG")
	if err := backupStore(d, "v1.2.0"); err != nil {
		t.Fatal(err)
	}
	b := filepath.Join(d, ".update", "backup-v1.2.0")
	if readFile(t, filepath.Join(b, "genkan.json")) != "J" || readFile(t, filepath.Join(b, "plugins", "a.js")) != "A" || readFile(t, filepath.Join(b, "plugins", "a.kv.json")) != "K" {
		t.Fatal("backup content")
	}
	for _, p := range []string{filepath.Join(b, "plugins", ".tmp-123"), filepath.Join(b, "uploads")} {
		if _, err := os.Stat(p); err == nil {
			t.Fatalf("%s should not be backed up", p)
		}
	}
	// a second backup replaces the first
	writeFile(t, filepath.Join(d, "genkan.json"), "J2")
	if err := backupStore(d, "v1.2.0"); err != nil || readFile(t, filepath.Join(b, "genkan.json")) != "J2" {
		t.Fatal(err)
	}
	// no store file: error and no half-written backup
	os.Remove(filepath.Join(d, "genkan.json"))
	if err := backupStore(d, "v1.3.0"); err == nil {
		t.Fatal("missing genkan.json")
	}
	if _, err := os.Stat(filepath.Join(d, ".update", "backup-v1.3.0")); err == nil {
		t.Fatal("partial backup")
	}
}

// chained: image v1.0.0 -> v1.2.0 confirmed -> v1.3.0 pending that never confirms.
func chainedFix(t *testing.T, keepFromDir bool) *launchFix {
	f := newLaunchFix(t, "v1.0.0", nil)
	dir := filepath.Join(f.data, ".update")
	// v1.2.0 is installed and confirmed; its data era is "v12"
	writeFile(t, filepath.Join(dir, "v1.2.0", "genkan"), "bin12")
	os.Chmod(filepath.Join(dir, "v1.2.0", "genkan"), 0o755)
	writeFile(t, filepath.Join(f.data, "genkan.json"), `{"v":"v12"}`)
	writeFile(t, filepath.Join(f.data, "plugins", "p.js"), "plugin v12")
	os.Remove(filepath.Join(f.data, "plugins", "extra.js"))
	// apply v1.3.0 from the running v1.2.0: backup, state, then the new version migrates the data
	writeFile(t, filepath.Join(dir, "v1.3.0", "genkan"), "bin13")
	os.Chmod(filepath.Join(dir, "v1.3.0", "genkan"), 0o755)
	if err := backupStore(f.data, "v1.3.0"); err != nil {
		t.Fatal(err)
	}
	if err := writeState(f.data, State{Version: "v1.3.0", Status: "pending", From: "v1.2.0", Backup: "backup-v1.3.0"}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(f.data, "genkan.json"), `{"v":"v13"}`)
	writeFile(t, filepath.Join(f.data, "plugins", "p.js"), "plugin v13")
	if !keepFromDir {
		os.RemoveAll(filepath.Join(dir, "v1.2.0"))
	}
	return f
}

func TestLauncherChainedRollbackResumesPreviousUpdate(t *testing.T) {
	f := chainedFix(t, true)
	// start 1 (image binary): v1.3.0 is launched, attempts=1; it crashes
	if f.l.Run() != OutcomeExec {
		t.Fatal("first launch")
	}
	// start 2: roll back, then go on with v1.2.0 and not with the image binary
	f.calls = nil
	if got := f.l.Run(); got != OutcomeRolledBackExec {
		t.Fatalf("outcome %s", got)
	}
	if len(f.calls) != 1 {
		t.Fatalf("calls %v", f.calls)
	}
	c := f.calls[0]
	want := filepath.Join(f.data, ".update", "v1.2.0", "genkan")
	if c.path != want || c.argv[0] != want || strings.Join(c.envs, ",") != "A=1,GENKAN_LAUNCHED=1" {
		t.Fatalf("exec %+v", c)
	}
	if readFile(t, filepath.Join(f.data, "genkan.json")) != `{"v":"v12"}` || readFile(t, filepath.Join(f.data, "plugins", "p.js")) != "plugin v12" {
		t.Fatal("data not restored to the v1.2.0 era")
	}
	st, ok := f.state()
	if !ok || st.Version != "v1.2.0" || st.Status != "confirmed" || st.Attempts != 0 {
		t.Fatalf("state %+v", st)
	}
	if f.exists("v1.3.0") || f.exists("backup-v1.3.0") || !f.exists("v1.2.0/genkan") {
		t.Fatal("dirs")
	}
	var rb struct{ Version string }
	b, _ := os.ReadFile(filepath.Join(f.data, ".update", "rolled_back.json"))
	if json.Unmarshal(b, &rb) != nil || rb.Version != "v1.3.0" {
		t.Fatalf("rolled_back.json = %s", b)
	}
	// later starts keep launching v1.2.0
	f.calls = nil
	if f.l.Run() != OutcomeExec || f.calls[0].path != want {
		t.Fatal("next start")
	}
}

func TestLauncherChainedRollbackFromDirMissing(t *testing.T) {
	f := chainedFix(t, false)
	f.l.Run() // attempts=1
	f.calls = nil
	if got := f.l.Run(); got != OutcomeRolledBack {
		t.Fatalf("outcome %s", got)
	}
	if len(f.calls) != 0 {
		t.Fatal("exec of a missing binary")
	}
	if _, ok := f.state(); ok || readFile(t, filepath.Join(f.data, "genkan.json")) != `{"v":"v12"}` || !f.exists("rolled_back.json") {
		t.Fatal("fallback to the image binary")
	}
}

func TestLauncherRollbackIgnoresFromNotNewerThanImage(t *testing.T) {
	// from == the image's own version (no earlier update): plain rollback, as before
	f := newLaunchFix(t, "v1.0.0", pending(1))
	writeFile(t, filepath.Join(f.data, ".update", "v1.0.0", "genkan"), "x")
	os.Chmod(filepath.Join(f.data, ".update", "v1.0.0", "genkan"), 0o755)
	if f.l.Run() != OutcomeRolledBack || len(f.calls) != 0 {
		t.Fatal("expected a plain rollback")
	}
}

func TestLauncherChainedExecFailureFallsBack(t *testing.T) {
	f := chainedFix(t, true)
	f.l.Run()
	f.fail = os.ErrPermission
	f.calls = nil
	if got := f.l.Run(); got != OutcomeRolledBack {
		t.Fatalf("outcome %s", got)
	}
	if _, ok := f.state(); ok {
		t.Fatal("state kept after a failed resume")
	}
}
