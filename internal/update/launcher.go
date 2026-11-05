package update

import (
	"log"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// LaunchedEnv is set to "1" in the environment of a binary started by the
// launcher; such a process skips the launcher.
const LaunchedEnv = "GENKAN_LAUNCHED"

// Outcome is what the launcher decided (for tests and logging).
type Outcome string

const (
	OutcomeSkip       Outcome = "skip"        // nothing to do
	OutcomeCleanup    Outcome = "cleanup"     // state was obsolete and removed
	OutcomeDropped    Outcome = "dropped"     // binary missing: state removed
	OutcomeRolledBack Outcome = "rolled_back" // restored the backup
	OutcomeExec       Outcome = "exec"        // exec'd the new binary (only with an injected Exec)
	// OutcomeRolledBackExec: rolled back, then exec'd the previously confirmed
	// update (state.from) instead of running the image binary.
	OutcomeRolledBackExec Outcome = "rolled_back_exec"
)

// Launcher decides at start-up whether to hand over to an installed update
// (docs/UPDATES.md §5). The zero value is not usable; see Launch.
type Launcher struct {
	DataDir string
	Version string // the running binary's version
	Args    []string
	Environ []string
	// Exec replaces the process (syscall.Exec). It returns only on failure,
	// or, for an injected fake, to report that it was called.
	Exec func(path string, argv, env []string) error
	Now  func() time.Time
	Logf func(format string, args ...any)
}

// Launch is called first in main for the server path. If an installed update
// should run it replaces the process and does not return; otherwise it
// returns (possibly after rolling a failed update back) and the caller runs
// as normal.
func Launch(dataDir, version string) {
	l := Launcher{DataDir: dataDir, Version: version, Args: os.Args, Environ: os.Environ(), Exec: syscall.Exec}
	l.Run()
}

// Run executes the decision table.
func (l *Launcher) Run() Outcome {
	logf := l.Logf
	if logf == nil {
		logf = log.Printf
	}
	now := l.Now
	if now == nil {
		now = time.Now
	}
	for _, kv := range l.Environ {
		if kv == LaunchedEnv+"=1" {
			return OutcomeSkip
		}
	}
	if IsDev(l.Version) {
		return OutcomeSkip // development builds never take part in updates
	}
	st, err := readState(l.DataDir)
	if err != nil {
		if !os.IsNotExist(err) {
			logf("update: dropping unreadable %s: %v", stateFile, err)
			_ = os.Remove(filepath.Join(updateDir(l.DataDir), stateFile))
		}
		return OutcomeSkip
	}
	if !Newer(st.Version, l.Version) {
		logf("update: %s is not newer than %s, removing %s", st.Version, l.Version, dirName)
		cleanup(l.DataDir)
		return OutcomeCleanup
	}
	dir := updateDir(l.DataDir)
	bin := filepath.Join(dir, st.Version, "genkan")
	if fi, err := os.Stat(bin); err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm()&0o111 == 0 {
		logf("update: %s is missing or not executable, dropping the update state", bin)
		_ = os.Remove(filepath.Join(dir, stateFile))
		_ = os.RemoveAll(filepath.Join(dir, st.Version))
		_ = os.RemoveAll(filepath.Join(dir, backupName(st.Version)))
		return OutcomeDropped
	}
	if st.Status == statusPending && st.Attempts >= 1 {
		logf("update: %s did not confirm its last start, rolling back", st.Version)
		return l.rollbackAndResume(st, now())
	}
	if st.Status == statusPending {
		st.Attempts++
		if err := writeState(l.DataDir, st); err != nil {
			logf("update: could not record the attempt: %v", err)
		}
	}
	argv := append([]string{bin}, l.argsTail()...)
	env := append(append([]string(nil), l.Environ...), LaunchedEnv+"=1")
	logf("update: starting %s", bin)
	if err := l.Exec(bin, argv, env); err != nil {
		logf("update: exec %s: %v", bin, err)
		if st.Status == statusPending {
			return l.rollbackAndResume(st, now())
		}
		_ = os.Remove(filepath.Join(dir, stateFile))
		return OutcomeDropped
	}
	return OutcomeExec
}

func (l *Launcher) argsTail() []string {
	if len(l.Args) > 1 {
		return l.Args[1:]
	}
	return nil
}

// rollbackAndResume rolls the failed pending update back. If the update was
// applied from an earlier update (st.From is newer than the running image
// binary and its binary is still installed), that earlier version is made the
// confirmed state and exec'd, so a failed v1.3 ends on v1.2 and not on the
// image's older binary and the data (restored to the v1.2 era) match it.
func (l *Launcher) rollbackAndResume(st State, at time.Time) Outcome {
	logf := l.Logf
	if logf == nil {
		logf = log.Printf
	}
	l.rollback(st, at)
	if !Newer(st.From, l.Version) || st.From == st.Version {
		return OutcomeRolledBack
	}
	dir := updateDir(l.DataDir)
	bin := filepath.Join(dir, st.From, "genkan") // st.From parsed as vX.Y.Z: a safe name
	if fi, err := os.Stat(bin); err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm()&0o111 == 0 {
		return OutcomeRolledBack
	}
	resumed := State{Version: st.From, Status: statusConfirmed, From: l.Version, At: at.UnixMilli()}
	if err := writeState(l.DataDir, resumed); err != nil {
		logf("update: could not record the resumed state: %v", err)
		return OutcomeRolledBack
	}
	argv := append([]string{bin}, l.argsTail()...)
	env := append(append([]string(nil), l.Environ...), LaunchedEnv+"=1")
	logf("update: resuming %s after the rollback", st.From)
	if err := l.Exec(bin, argv, env); err != nil {
		logf("update: exec %s: %v", bin, err)
		_ = os.Remove(filepath.Join(dir, stateFile))
		return OutcomeRolledBack
	}
	return OutcomeRolledBackExec
}

func (l *Launcher) rollback(st State, at time.Time) {
	logf := l.Logf
	if logf == nil {
		logf = log.Printf
	}
	dir := updateDir(l.DataDir)
	backup := filepath.Join(dir, backupName(st.Version)) // st.Version parsed as vX.Y.Z: a safe name
	if err := restoreStore(l.DataDir, backup); err != nil {
		logf("update: restoring the backup failed: %v", err)
	}
	_ = os.RemoveAll(filepath.Join(dir, st.Version))
	_ = os.RemoveAll(backup)
	_ = os.Remove(filepath.Join(dir, stateFile))
	if err := writeJSONFile(filepath.Join(dir, rolledBackFile), rolledBack{Version: st.Version, At: at.UnixMilli()}); err != nil {
		logf("update: could not write %s: %v", rolledBackFile, err)
	}
}

type rolledBack struct {
	Version string `json:"version"`
	At      int64  `json:"at"`
}
