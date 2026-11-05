package update

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	// DefaultAPI is the GitHub API base of the project; GENKAN_UPDATE_API overrides it.
	DefaultAPI = "https://api.github.com/repos/jae-jae/genkan"
	// RepoURL is the project's home, reported in the status.
	RepoURL = "https://github.com/jae-jae/genkan"

	DefaultInterval     = 6 * time.Hour
	DefaultConfirmAfter = 30 * time.Second
	versionCheckTimeout = 10 * time.Second
	applyTimeout        = 15 * time.Minute

	// Environment variables (docs/UPDATES.md).
	EnvAPI        = "GENKAN_UPDATE_API"
	EnvCheck      = "GENKAN_UPDATE_CHECK"
	EnvSelfUpdate = "GENKAN_SELF_UPDATE"
	EnvPubKey     = "GENKAN_UPDATE_PUBKEY"
	EnvToken      = "GENKAN_GITHUB_TOKEN"
)

// Job phases.
const (
	PhaseIdle        = "idle"
	PhaseDownloading = "downloading"
	PhaseVerifying   = "verifying"
	PhaseInstalling  = "installing"
	PhaseRestarting  = "restarting"
	PhaseFailed      = "failed"
)

// Errors that the API maps to HTTP statuses.
var (
	ErrBusy           = errors.New("an update is already in progress")
	ErrUnknownVersion = errors.New("that version is not an available update")
	ErrCheckDisabled  = errors.New("update checks are disabled")
)

// UnsupportedError reports why applying is turned off.
type UnsupportedError struct{ Reason string }

func (e *UnsupportedError) Error() string {
	switch e.Reason {
	case "dev":
		return "development builds cannot be updated"
	case "disabled":
		return "self-update is turned off (GENKAN_SELF_UPDATE=off)"
	case "platform":
		return "self-update is only available on linux/amd64 and linux/arm64"
	case "noexec":
		return "self-update is unavailable: the data directory is on a noexec mount"
	}
	return "self-update is not supported"
}

// Config holds the Service's dependencies. Only DataDir and Version are required.
type Config struct {
	DataDir string
	Version string
	// API overrides the GitHub API base (GENKAN_UPDATE_API; default DefaultAPI).
	API string
	// Token is sent to the API (GENKAN_GITHUB_TOKEN), never to download hosts.
	Token string
	// HTTPClient is the base client for the API and the downloads (default: a plain client).
	HTTPClient *http.Client
	// CheckEnabled reports the update_check setting (nil: true); Dismissed the update_dismissed setting.
	CheckEnabled func() bool
	Dismissed    func() string

	// Everything below exists for tests; the zero values are the real thing.
	Getenv     func(string) string
	GOOS       string
	GOARCH     string
	NoExec     func(dir string) bool
	PubKey     ed25519.PublicKey // overrides GENKAN_UPDATE_PUBKEY and the embedded key
	Exec       func(path string, argv, env []string) error
	Executable func() (string, error)
	Args       []string
	Environ    func() []string
	RunVersion func(ctx context.Context, bin string) (string, error)
	// ConfirmAfter is how long a freshly launched version must stay up (default 30 s).
	ConfirmAfter time.Duration
	// Probe checks that this process serves /healthz; nil skips the probe.
	Probe func(ctx context.Context) error
	// FirstCheckDelay replaces the ~1 minute (jittered) delay before the first check.
	FirstCheckDelay time.Duration
	Interval        time.Duration
	Now             func() time.Time
	Logf            func(format string, args ...any)
}

// Release is one entry of the status' releases list.
type Release struct {
	Version     string `json:"version"`
	Name        string `json:"name"`
	Notes       string `json:"notes"`
	PublishedAt int64  `json:"published_at"` // Unix ms
	URL         string `json:"url"`
}

// Apply describes whether applying updates works here.
type ApplyInfo struct {
	Supported bool   `json:"supported"`
	Reason    string `json:"reason"` // "", "disabled", "platform", "noexec", "dev"
}

// Job is the state of the apply job.
type Job struct {
	Phase    string  `json:"phase"`
	Version  string  `json:"version"`
	Progress float64 `json:"progress"`
	Error    string  `json:"error"`
}

// Status is the response of GET /api/update (docs/UPDATES.md §6).
type Status struct {
	Current      string    `json:"current"`
	Dev          bool      `json:"dev"`
	Repo         string    `json:"repo"`
	CheckEnabled bool      `json:"check_enabled"`
	EnvDisabled  bool      `json:"env_disabled"`
	Checking     bool      `json:"checking"`
	CheckedAt    int64     `json:"checked_at"` // Unix ms; 0 before the first check
	Error        string    `json:"error"`
	Latest       string    `json:"latest"`
	Available    bool      `json:"available"`
	Dismissed    string    `json:"dismissed"`
	Releases     []Release `json:"releases"`
	Apply        ApplyInfo `json:"apply"`
	Job          Job       `json:"job"`
	RolledBack   string    `json:"rolled_back"`
}

// Service checks for releases and applies updates.
type Service struct {
	cfg    Config
	api    string
	apiURL *url.URL

	key    ed25519.PublicKey
	keyErr error

	ctx    context.Context
	cancel context.CancelFunc

	noexecOnce sync.Once
	noexec     bool

	checkMu sync.Mutex // serializes checks

	mu          sync.Mutex
	cache       checkCache
	loaded      bool
	checking    bool
	checkedAt   int64 // last attempt, Unix ms
	lastErr     string
	job         Job
	restartCh   chan struct{}
	restartOnce sync.Once
}

// New builds a Service. It does nothing until Run, Check or Apply is called.
func New(cfg Config) *Service {
	if cfg.Getenv == nil {
		cfg.Getenv = os.Getenv
	}
	if cfg.GOOS == "" {
		cfg.GOOS = runtime.GOOS
	}
	if cfg.GOARCH == "" {
		cfg.GOARCH = runtime.GOARCH
	}
	if cfg.NoExec == nil {
		cfg.NoExec = dataDirNoexec
	}
	if cfg.Exec == nil {
		cfg.Exec = syscall.Exec
	}
	if cfg.Executable == nil {
		cfg.Executable = os.Executable
	}
	if cfg.Args == nil {
		cfg.Args = os.Args
	}
	if cfg.Environ == nil {
		cfg.Environ = os.Environ
	}
	if cfg.RunVersion == nil {
		cfg.RunVersion = runVersion
	}
	if cfg.ConfirmAfter <= 0 {
		cfg.ConfirmAfter = DefaultConfirmAfter
	}
	if cfg.Interval <= 0 {
		cfg.Interval = DefaultInterval
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Logf == nil {
		cfg.Logf = func(f string, a ...any) { logPrintf(f, a...) }
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 20 * time.Second}
	}
	s := &Service{cfg: cfg, job: Job{Phase: PhaseIdle}, restartCh: make(chan struct{}, 1)}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.api = strings.TrimRight(strings.TrimSpace(cfg.API), "/")
	if s.api == "" {
		s.api = strings.TrimRight(strings.TrimSpace(cfg.Getenv(EnvAPI)), "/")
	}
	if s.api == "" {
		s.api = DefaultAPI
	}
	s.apiURL, _ = url.Parse(s.api)
	switch {
	case cfg.PubKey != nil:
		s.key = cfg.PubKey
	case strings.TrimSpace(cfg.Getenv(EnvPubKey)) != "":
		if s.key, s.keyErr = ParsePublicKey(cfg.Getenv(EnvPubKey)); s.keyErr != nil {
			s.keyErr = fmt.Errorf("%s: %w", EnvPubKey, s.keyErr)
		}
	default:
		s.key, _ = ParsePublicKey(EmbeddedPublicKey)
	}
	return s
}

// SetProbe sets the health probe the confirmation of a new version waits for.
// Call before Run.
func (s *Service) SetProbe(p func(ctx context.Context) error) { s.cfg.Probe = p }

// Close stops background work started by the Service.
func (s *Service) Close() { s.cancel() }

func (s *Service) apiOverridden() bool { return s.api != DefaultAPI }

func isOff(v string) bool { return strings.EqualFold(strings.TrimSpace(v), "off") }

// Dev reports whether the running binary is a development build.
func (s *Service) Dev() bool { return IsDev(s.cfg.Version) }

func (s *Service) envDisabled() bool { return isOff(s.cfg.Getenv(EnvCheck)) }

func (s *Service) settingOn() bool { return s.cfg.CheckEnabled == nil || s.cfg.CheckEnabled() }

// applySupport reports whether applying updates works in this process.
func (s *Service) applySupport() ApplyInfo {
	reason := ""
	switch {
	case s.Dev():
		reason = "dev"
	case isOff(s.cfg.Getenv(EnvSelfUpdate)):
		reason = "disabled"
	case s.cfg.GOOS != "linux" || (s.cfg.GOARCH != "amd64" && s.cfg.GOARCH != "arm64"):
		reason = "platform"
	default:
		s.noexecOnce.Do(func() { s.noexec = s.cfg.NoExec(s.cfg.DataDir) })
		if s.noexec {
			reason = "noexec"
		}
	}
	return ApplyInfo{Supported: reason == "", Reason: reason}
}

// Status returns the current UpdateStatus.
func (s *Service) Status() Status {
	s.mu.Lock()
	s.loadLocked()
	st := Status{
		Current:      s.cfg.Version,
		Dev:          s.Dev(),
		Repo:         RepoURL,
		CheckEnabled: s.settingOn(),
		EnvDisabled:  s.envDisabled(),
		Checking:     s.checking,
		CheckedAt:    s.checkedAt,
		Error:        s.lastErr,
		Releases:     []Release{},
		Job:          s.job,
	}
	if !st.Dev {
		st.Releases = s.newerLocked()
	}
	s.mu.Unlock()
	if len(st.Releases) > 0 {
		st.Latest = st.Releases[0].Version
		st.Available = true
	}
	if s.cfg.Dismissed != nil {
		st.Dismissed = s.cfg.Dismissed()
	}
	st.Apply = s.applySupport()
	var rb rolledBack
	if readJSON(filepath.Join(updateDir(s.cfg.DataDir), rolledBackFile), &rb) == nil {
		st.RolledBack = rb.Version
	}
	return st
}

// ClearRolledBack removes the rolled-back notice.
func (s *Service) ClearRolledBack() {
	_ = os.Remove(filepath.Join(updateDir(s.cfg.DataDir), rolledBackFile))
}

func runVersion(ctx context.Context, bin string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, versionCheckTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "version")
	cmd.Env = append(os.Environ(), LaunchedEnv+"=1")
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("timed out")
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
