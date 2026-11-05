package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// RestartRequested yields once when the apply job has
// installed an update and wants the process to shut down gracefully. The
// caller (main) must then stop the HTTP server, the plugin manager and the
// scheduler, flush the store, and call Restart.
func (s *Service) RestartRequested() <-chan struct{} { return s.restartCh }

// Apply starts the background job that installs version, which must be one of
// the checked releases. It returns ErrBusy while a job runs, ErrUnknownVersion
// for a version that is not in the list and *UnsupportedError when applying is
// turned off here.
func (s *Service) Apply(version string) error {
	if sup := s.applySupport(); !sup.Supported {
		return &UnsupportedError{Reason: sup.Reason}
	}
	if s.keyErr != nil {
		return fmt.Errorf("%w", s.keyErr)
	}
	rel := s.findRelease(version)
	if rel == nil {
		return ErrUnknownVersion
	}
	s.mu.Lock()
	switch s.job.Phase {
	case PhaseDownloading, PhaseVerifying, PhaseInstalling, PhaseRestarting:
		s.mu.Unlock()
		return ErrBusy
	}
	s.job = Job{Phase: PhaseDownloading, Version: version}
	s.mu.Unlock()
	go s.runApply(*rel)
	return nil
}

func (s *Service) setJob(phase string, progress float64) {
	s.mu.Lock()
	s.job.Phase, s.job.Progress = phase, progress
	s.mu.Unlock()
}

func (s *Service) failJob(err error) {
	s.cfg.Logf("update: applying failed: %v", err)
	s.mu.Lock()
	s.job.Phase, s.job.Error = PhaseFailed, err.Error()
	s.mu.Unlock()
}

func (s *Service) runApply(rel storedRelease) {
	ctx, cancel := context.WithTimeout(s.ctx, applyTimeout)
	defer cancel()
	dir := updateDir(s.cfg.DataDir)
	if err := s.install(ctx, rel); err != nil {
		_ = os.RemoveAll(filepath.Join(dir, rel.Version))
		_ = os.RemoveAll(filepath.Join(dir, backupName(rel.Version)))
		_ = os.Remove(filepath.Join(dir, "download-"+rel.Version+".tar.gz"))
		if st, err2 := readState(s.cfg.DataDir); err2 == nil && st.Version == rel.Version && st.Status == statusPending {
			_ = os.Remove(filepath.Join(dir, stateFile))
		}
		s.failJob(err)
		return
	}
	s.mu.Lock()
	s.job.Phase, s.job.Progress = PhaseRestarting, 1
	s.mu.Unlock()
	select {
	case s.restartCh <- struct{}{}:
	default:
	}
}

// install runs download, verification and installation (docs/UPDATES.md §4, 1-5).
func (s *Service) install(ctx context.Context, rel storedRelease) error {
	dir := updateDir(s.cfg.DataDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("cannot write to the data directory: %w", err)
	}
	find := func(name string) (asset, error) {
		for _, a := range rel.Assets {
			if a.Name == name {
				return a, nil
			}
		}
		return asset{}, fmt.Errorf("release %s has no %s asset", rel.Version, name)
	}
	tarName := tarballName(rel.Version, s.cfg.GOARCH)
	aSums, err := find("checksums.txt")
	if err != nil {
		return err
	}
	aSig, err := find("checksums.txt.sig")
	if err != nil {
		return err
	}
	aTar, err := find(tarName)
	if err != nil {
		return err
	}

	// 1. download
	s.setJob(PhaseDownloading, 0)
	sums, err := s.fetchBytes(ctx, aSums.URL, maxChecksums)
	if err != nil {
		return fmt.Errorf("checksums.txt: %w", err)
	}
	sig, err := s.fetchBytes(ctx, aSig.URL, maxSig)
	if err != nil {
		return fmt.Errorf("checksums.txt.sig: %w", err)
	}
	tmpTar := filepath.Join(dir, "download-"+rel.Version+".tar.gz")
	f, err := os.OpenFile(tmpTar, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer os.Remove(tmpTar)
	h := sha256.New()
	err = s.fetch(ctx, aTar.URL, maxTarball, io.MultiWriter(f, h), func(done, total int64) {
		if total <= 0 {
			total = aTar.Size
		}
		if total > 0 {
			p := float64(done) / float64(total)
			if p > 1 {
				p = 1
			}
			s.setJob(PhaseDownloading, p)
		}
	})
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("%s: %w", tarName, err)
	}

	// 2. verify: signature first, then the digest
	s.setJob(PhaseVerifying, 1)
	if s.keyErr != nil {
		return s.keyErr
	}
	if err := verifyRelease(s.key, sums, sig, tarName, hex.EncodeToString(h.Sum(nil))); err != nil {
		return err
	}

	// 3. install
	s.setJob(PhaseInstalling, 1)
	verDir := filepath.Join(dir, rel.Version)
	if err := os.RemoveAll(verDir); err != nil {
		return err
	}
	bin := filepath.Join(verDir, "genkan")
	in, err := os.Open(tmpTar)
	if err != nil {
		return err
	}
	err = extractBinary(in, bin)
	in.Close()
	if err != nil {
		return err
	}
	out, err := s.cfg.RunVersion(ctx, bin)
	if err != nil {
		return fmt.Errorf("the new binary does not run: %w", err)
	}
	if want := "genkan " + rel.Version; strings.TrimSpace(out) != want {
		return fmt.Errorf("the new binary reports %q, expected %q", strings.TrimSpace(out), want)
	}

	// 4. back up the store, 5. write the state
	if err := backupStore(s.cfg.DataDir, rel.Version); err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}
	st := State{Version: rel.Version, Status: statusPending, From: s.cfg.Version, Backup: backupName(rel.Version), At: s.cfg.Now().UnixMilli()}
	if err := writeState(s.cfg.DataDir, st); err != nil {
		return fmt.Errorf("could not write %s: %w", stateFile, err)
	}
	_ = os.Remove(tmpTar)
	return nil
}

// Restart is step 6 of §4, to be called by main after the graceful shutdown
// (HTTP server, plugin manager, scheduler stopped, store flushed). It
// refreshes the backup, which was taken before in-flight writes settled, and
// exec's the current executable so the launcher takes over. It returns only
// if the exec fails.
func (s *Service) Restart() error {
	st, err := readState(s.cfg.DataDir)
	if err != nil {
		return fmt.Errorf("update state: %w", err)
	}
	if err := backupStore(s.cfg.DataDir, st.Version); err != nil {
		s.cfg.Logf("update: could not refresh the backup, keeping the earlier one: %v", err) // the first backup stays in place
	}
	exe, err := s.cfg.Executable()
	if err != nil {
		return err
	}
	// The launcher must run again: a process that was itself started by the
	// launcher carries GENKAN_LAUNCHED=1.
	var env []string
	for _, kv := range s.cfg.Environ() {
		if !strings.HasPrefix(kv, LaunchedEnv+"=") {
			env = append(env, kv)
		}
	}
	args := s.cfg.Args
	if len(args) == 0 {
		args = []string{exe}
	}
	s.cfg.Logf("update: restarting into %s", st.Version)
	return s.cfg.Exec(exe, args, env)
}
