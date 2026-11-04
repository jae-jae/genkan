package plugin

import (
	"sync"
	"time"
)

const (
	firstRunAfterStart = 15 * time.Second
	firstRunAfterEdit  = 5 * time.Second
	maxScheduled       = 2 // scheduled jobs running at once
)

// scheduler runs plugin schedules. Its goroutine only exists while an
// enabled plugin is installed; it exits when no schedule is left.
type scheduler struct {
	m *Manager

	mu      sync.Mutex
	running bool
	wake    chan struct{}
	quit    chan struct{}
	done    chan struct{}
	started bool // false until the first run of the goroutine (startup delay)
	sem     chan struct{}
	next    map[string]time.Time
}

func newScheduler(m *Manager) *scheduler {
	return &scheduler{m: m, wake: make(chan struct{}, 1), quit: make(chan struct{}), sem: make(chan struct{}, maxScheduled), next: map[string]time.Time{}}
}

// kick tells the scheduler that plugins changed. It never runs JavaScript.
func (s *scheduler) kick() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		select {
		case s.wake <- struct{}{}:
		default:
		}
		return
	}
	select {
	case <-s.quit:
		return
	default:
	}
	for _, p := range s.m.st.Plugins() {
		if p.Enabled {
			s.running = true
			s.done = make(chan struct{})
			go s.run(s.done)
			return
		}
	}
}

func (s *scheduler) stop() {
	s.mu.Lock()
	select {
	case <-s.quit:
	default:
		close(s.quit)
	}
	done := s.done
	s.mu.Unlock()
	if done != nil {
		<-done
	}
}

type job struct {
	plugin, schedule string
	every            time.Duration
}

// plan returns the schedules of all enabled plugins and keeps s.next in sync.
func (s *scheduler) plan() map[string]job {
	jobs := map[string]job{}
	delay := firstRunAfterEdit
	if !s.started {
		delay, s.started = firstRunAfterStart, true
	}
	for _, rec := range s.m.st.Plugins() {
		if !rec.Enabled {
			continue
		}
		e := s.m.entryRec(rec)
		if e.man == nil {
			continue
		}
		for _, sc := range e.man.Schedules {
			key := rec.ID + "|" + sc.ID
			jobs[key] = job{rec.ID, sc.ID, time.Duration(sc.Every) * time.Second}
			if _, ok := s.next[key]; !ok {
				s.next[key] = time.Now().Add(delay)
			}
		}
	}
	for k := range s.next {
		if _, ok := jobs[k]; !ok {
			delete(s.next, k)
		}
	}
	return jobs
}

func (s *scheduler) run(done chan struct{}) {
	defer close(done)
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	for {
		jobs := s.plan()
		if len(jobs) == 0 {
			s.mu.Lock()
			select {
			case <-s.wake: // a change arrived while planning: plan again
				s.mu.Unlock()
				continue
			default:
			}
			s.running = false
			s.next = map[string]time.Time{}
			s.mu.Unlock()
			return
		}
		var earliest time.Time
		for k := range jobs {
			if t := s.next[k]; earliest.IsZero() || t.Before(earliest) {
				earliest = t
			}
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(time.Until(earliest))
		select {
		case <-s.quit:
			return
		case <-s.m.ctx.Done():
			return
		case <-s.wake:
			continue
		case <-timer.C:
		}
		now := time.Now()
		for k, j := range jobs {
			if !s.next[k].After(now) {
				s.next[k] = now.Add(j.every)
				s.m.wg.Add(1)
				go s.runOne(j)
			}
		}
	}
}

func (s *scheduler) runOne(j job) {
	defer s.m.wg.Done()
	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	case <-s.m.ctx.Done():
		return
	}
	e, rec, err := s.m.ready(j.plugin)
	if err != nil {
		return
	}
	_, _, _ = s.m.fl.do("sch|"+j.plugin+"|"+j.schedule, func() (any, error) {
		return s.m.call(s.m.ctx, e, rec, hookReq{
			hook: hookSchedule, lang: "en", timeout: timeoutSched,
			fields: map[string]any{"schedule": j.schedule},
		})
	})
}
