package update

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

// confirmLoop confirms a freshly launched version once it has been up for
// ConfirmAfter and (when a Probe is set) serves /healthz (docs/UPDATES.md §5).
// It does nothing in a process that the launcher did not start.
func (s *Service) confirmLoop(ctx context.Context) {
	if s.cfg.Getenv(LaunchedEnv) != "1" {
		return
	}
	st, err := readState(s.cfg.DataDir)
	if err != nil || st.Status != statusPending || st.Version != s.cfg.Version {
		return
	}
	t := time.NewTimer(s.cfg.ConfirmAfter)
	defer t.Stop()
	retry := 5 * time.Second
	if s.cfg.ConfirmAfter < retry {
		retry = s.cfg.ConfirmAfter
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if s.cfg.Probe != nil {
			pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := s.cfg.Probe(pctx)
			cancel()
			if err != nil {
				s.cfg.Logf("update: waiting to confirm %s: %v", st.Version, err)
				t.Reset(retry)
				continue
			}
		}
		if err := s.confirm(st); err != nil {
			s.cfg.Logf("update: could not confirm %s: %v", st.Version, err)
		} else {
			s.cfg.Logf("update: %s confirmed", st.Version)
		}
		return
	}
}

// confirm marks the update confirmed and removes the backup and the other version dirs.
func (s *Service) confirm(st State) error {
	st.Status = statusConfirmed
	if err := writeState(s.cfg.DataDir, st); err != nil {
		return err
	}
	dir := updateDir(s.cfg.DataDir)
	ents, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range ents {
		if !e.IsDir() || e.Name() == st.Version {
			continue
		}
		_ = os.RemoveAll(filepath.Join(dir, e.Name()))
	}
	return nil
}
