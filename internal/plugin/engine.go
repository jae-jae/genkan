package plugin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MoeForge/moejs"
	"github.com/MoeForge/moejs/eventloop"
	"github.com/MoeForge/moejs/webapi"
)

// Execution limits (docs/PLUGINS.md §2).
const (
	memoryLimit    = 32 << 20
	maxIdle        = 2 // idle runtimes kept per plugin
	maxConcurrent  = 4 // runtimes per plugin running at once
	idleTTL        = 5 * time.Minute
	loadTimeout    = 2 * time.Second
	settleGrace    = 100 * time.Millisecond // how long leftovers may run after the hook settled
	maxResultBytes = 1 << 20
)

// Per-hook timeouts (variables so tests can shorten them).
var (
	timeoutSearch  = 2 * time.Second
	timeoutDefault = 10 * time.Second // widget, wallpaper, action
	timeoutSched   = 60 * time.Second
)

var (
	errTimeout = errors.New("timed out")
	errClosed  = errors.New("plugin manager is shutting down")
)

// HookError is a failure inside a plugin hook (exception, timeout, memory
// limit, invalid result). Its message is safe to show to the admin.
type HookError struct {
	Msg     string
	Timeout bool
}

func (e *HookError) Error() string { return e.Msg }

func hookErr(format string, a ...any) error { return &HookError{Msg: fmt.Sprintf(format, a...)} }

// entry is a compiled plugin: module, manifest, runtime pool, log, storage.
type entry struct {
	m    *Manager
	id   string
	rev  uint64
	lang string

	mod  *moejs.Module
	man  *Manifest
	err  error // build error: the plugin cannot run
	hook map[string]moejs.Hook

	logs *logRing
	kv   *kvStore
	pool *pool

	lastErr atomic.Pointer[string]
}

func (e *entry) setLastError(msg string) {
	if msg == "" {
		e.lastErr.Store(nil)
		return
	}
	e.lastErr.Store(&msg)
}

func (e *entry) lastError() string {
	if p := e.lastErr.Load(); p != nil {
		return *p
	}
	if e.err != nil {
		return e.err.Error()
	}
	return ""
}

func (e *entry) close() {
	if e.pool != nil {
		e.pool.close()
	}
	if e.kv != nil {
		_ = e.kv.close()
	}
}

// instance is one JS runtime with its event loop and Web APIs.
type instance struct {
	e    *entry
	rt   *moejs.Runtime
	loop *eventloop.Loop
	api  *webapi.API
	tr   *hostTransport

	loading atomic.Bool // true while the module's top level runs: the global t() is usable only then
	track   moejs.Value // (promise, ok, fail) => Promise.resolve(promise).then(ok, fail)
	allow   atomic.Pointer[allowList]
	redact  atomic.Pointer[[]string]
}

var (
	trackOnce   sync.Once
	trackScript *moejs.Script
)

func trackScr() *moejs.Script {
	trackOnce.Do(func() {
		s, err := moejs.CompileScript("genkan-track.js", `(function (p, ok, fail) { Promise.resolve(p).then(ok, fail); })`)
		if err != nil {
			panic(err)
		}
		trackScript = s
	})
	return trackScript
}

// newInstance creates a runtime with the genkan host API, loads the module
// and (unless probe) installs fetch and the timers. A probe runtime is used
// only to read the manifest: no network, no storage, no links.
func newInstance(m *Manager, e *entry, name string, mod *moejs.Module, probe bool) (*instance, error) {
	rt := moejs.NewRuntime(moejs.Options{MemoryLimit: memoryLimit, DisableDynamicCode: true})
	loop, err := eventloop.New(rt, eventloop.Options{})
	if err != nil {
		return nil, err
	}
	in := &instance{e: e, rt: rt, loop: loop}
	if !probe {
		in.tr = newHostTransport()
		client := newClient(in.tr, in.checkFetch)
		in.api, err = webapi.Install(loop, webapi.Options{Client: client, Allow: in.checkFetch})
		if err != nil {
			return nil, err
		}
	}
	if err := installHost(in, m, probe); err != nil {
		in.close()
		return nil, err
	}
	// load the module (top level) with a deadline
	in.loading.Store(true)
	defer in.loading.Store(false)
	timer := time.AfterFunc(loadTimeout, func() { loop.Interrupt(errTimeout) })
	err = loop.Run(func(rt *moejs.Runtime) error {
		if err := rt.Load(mod); err != nil && !errors.Is(err, moejs.ErrModulePending) {
			return err
		}
		tr, err := rt.RunScript(trackScr())
		in.track = tr
		return err
	})
	timer.Stop()
	if err != nil {
		in.close()
		return nil, describeErr(err, 0, in.redactList())
	}
	rt.ClearInterrupt()
	rt.ReleaseCallData()
	return in, nil
}

func (in *instance) redactList() []string {
	if p := in.redact.Load(); p != nil {
		return *p
	}
	return nil
}

func (in *instance) close() {
	if in.api != nil {
		in.api.Close()
	}
	in.loop.Terminate()
	if in.tr != nil {
		in.tr.close()
	}
}

// describeErr turns an engine error into a HookError with a readable message.
func describeErr(err error, timeout time.Duration, redact []string) error {
	var he *HookError
	if errors.As(err, &he) {
		return he
	}
	var exc *moejs.Exception
	var intr *moejs.InterruptedError
	var syn *moejs.SyntaxError
	var internal *moejs.InternalError
	switch {
	case errors.Is(err, moejs.ErrMemoryLimit):
		return hookErr("exceeded the %d MiB memory limit", memoryLimit>>20)
	case errors.As(err, &intr):
		if errors.Is(err, errTimeout) {
			return &HookError{Msg: fmt.Sprintf("timed out after %s", timeout), Timeout: true}
		}
		if errors.Is(err, errClosed) {
			return hookErr("interrupted: server is shutting down")
		}
		if errors.Is(err, context.Canceled) {
			return hookErr("canceled")
		}
		return hookErr("interrupted")
	case errors.As(err, &exc):
		return hookErr("%s", redactSecrets(excText(exc), redact))
	case errors.As(err, &syn):
		return hookErr("%s", syn.Error())
	case errors.As(err, &internal):
		return hookErr("internal error: %v", internal.Value)
	}
	return hookErr("%s", redactSecrets(err.Error(), redact))
}

func excText(exc *moejs.Exception) string {
	name, msg := exc.Name(), exc.Message()
	switch {
	case name != "" && msg != "":
		return name + ": " + msg
	case msg != "":
		return msg
	case name != "":
		return name
	}
	return "uncaught exception"
}

// pool ---------------------------------------------------------------------------

type pool struct {
	e      *entry
	sem    chan struct{}
	mu     sync.Mutex
	idle   []*instance
	closed bool
	timer  *time.Timer
}

func newPool(e *entry) *pool { return &pool{e: e, sem: make(chan struct{}, maxConcurrent)} }

func (p *pool) acquire(ctx context.Context) (*instance, error) {
	select {
	case p.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, context.Cause(ctx)
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		<-p.sem
		return nil, hookErr("plugin was updated or removed")
	}
	if n := len(p.idle); n > 0 {
		in := p.idle[n-1]
		p.idle = p.idle[:n-1]
		p.mu.Unlock()
		return in, nil
	}
	p.mu.Unlock()
	in, err := newInstance(p.e.m, p.e, p.e.id, p.e.mod, false)
	if err != nil {
		<-p.sem
		return nil, err
	}
	return in, nil
}

func (p *pool) release(in *instance, reuse bool) {
	p.mu.Lock()
	keep := reuse && !p.closed && len(p.idle) < maxIdle
	if keep {
		p.idle = append(p.idle, in)
		if p.timer == nil {
			p.timer = time.AfterFunc(idleTTL, p.purge)
		} else {
			p.timer.Reset(idleTTL)
		}
	}
	p.mu.Unlock()
	if !keep {
		in.close()
	}
	<-p.sem
}

// purge drops idle runtimes so an unused plugin costs no memory.
func (p *pool) purge() {
	p.mu.Lock()
	idle := p.idle
	p.idle = nil
	p.mu.Unlock()
	for _, in := range idle {
		in.close()
	}
}

func (p *pool) close() {
	p.mu.Lock()
	p.closed = true
	if p.timer != nil {
		p.timer.Stop()
	}
	idle := p.idle
	p.idle = nil
	p.mu.Unlock()
	for _, in := range idle {
		in.close()
	}
}

func (p *pool) idleCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.idle)
}

// invoke ----------------------------------------------------------------------------

type callOpts struct {
	hook       string
	timeout    time.Duration
	ctx        map[string]any
	pluginHost []string // allowFetch hosts from the plugin config
	extraHosts []string // allowFetch hosts from the widget instance config
	secrets    []string // values to redact from logs and errors
	lang       string   // request language, for ctx.t
	msgs       *Messages
	instID     string   // widget instance of the call ("" = none)
	instKV     *kvStore // storage behind ctx.instance.storage
}

// invoke runs one hook of the plugin and returns its result converted to Go
// values (maps, slices, strings, numbers, bools).
func (e *entry) invoke(parent context.Context, o callOpts) (res any, err error) {
	if e.err != nil {
		return nil, hookErr("plugin is broken: %v", e.err)
	}
	h, ok := e.hook[o.hook]
	if !ok {
		return nil, hookErr("plugin does not export %s()", o.hook)
	}
	defer func() {
		if r := recover(); r != nil {
			err = hookErr("internal error: %v", r)
		}
		if err != nil {
			e.logs.add("error", fmt.Sprintf("%s: %v", o.hook, err), o.secrets)
			e.setLastError(redactSecrets(fmt.Sprintf("%s: %v", o.hook, err), o.secrets))
		} else {
			e.setLastError("")
		}
	}()

	callCtx, cancel := context.WithTimeoutCause(parent, o.timeout, errTimeout)
	defer cancel()
	in, err := e.pool.acquire(callCtx)
	if err != nil {
		return nil, describeErr(err, o.timeout, o.secrets)
	}
	reuse := false
	defer func() { e.pool.release(in, reuse) }()

	in.allow.Store(newAllowList(e.man.Permissions.Fetch, o.pluginHost, o.extraHosts))
	in.tr.lenients.Store(newAllowList(o.pluginHost, o.extraHosts))
	in.redact.Store(&o.secrets)
	in.api.SetContext(callCtx)

	interrupted := make(chan struct{})
	stop := context.AfterFunc(callCtx, func() {
		in.loop.Interrupt(context.Cause(callCtx))
		close(interrupted)
	})

	var (
		out     any
		settled bool
		jsErr   error
		grace   *time.Timer
		fired   atomic.Bool
	)
	runErr := in.loop.Run(func(rt *moejs.Runtime) error {
		hctx := make(map[string]any, len(o.ctx)+1)
		for k, v := range o.ctx {
			hctx[k] = v
		}
		hctx["t"] = moejs.NativeFunc(in.translateFunc(o.msgs, o.lang))
		if inst, ok := hctx["instance"].(map[string]any); ok && o.instKV != nil {
			withStorage := make(map[string]any, len(inst)+1)
			for k, v := range inst {
				withStorage[k] = v
			}
			withStorage["storage"] = in.instanceStorage(o.instKV, o.instID)
			hctx["instance"] = withStorage
		}
		arg, err := rt.FromGo(hctx)
		if err != nil {
			return err
		}
		v, err := rt.Call(h, arg)
		if err != nil {
			return err
		}
		finish := func() {
			settled = true
			grace = time.AfterFunc(settleGrace, func() {
				fired.Store(true)
				in.loop.StopNoWait()
			})
		}
		ok := rt.Function("ok", 1, func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
			g, err := rt.ToGoWith(moejs.Arg(args, 0), moejs.DecodeOptions{MaxBytes: maxResultBytes})
			if err != nil {
				jsErr = hookErr("result is not usable: %v", err)
			} else {
				out = g
			}
			finish()
			return moejs.Undefined(), nil
		})
		fail := rt.Function("fail", 1, func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
			jsErr = hookErr("%s", rejectionText(moejs.Arg(args, 0)))
			finish()
			return moejs.Undefined(), nil
		})
		_, err = rt.CallFunction(in.track, moejs.Undefined(), v, ok, fail)
		return err
	})
	if !stop() {
		<-interrupted
	}
	graceFired := false
	if grace != nil {
		graceFired = !grace.Stop()
	}
	callDone := callCtx.Err() != nil

	switch {
	case settled && jsErr == nil && (runErr == nil || graceFired):
		reuse = runErr == nil && !graceFired && !callDone
		return out, nil
	case settled && jsErr == nil:
		// settled, but something after it failed (timer callback, interrupt)
		if runErr != nil && !isInterrupt(runErr) {
			e.logs.add("warn", "after "+o.hook+"(): "+describeErr(runErr, o.timeout, o.secrets).Error(), o.secrets)
		}
		return out, nil
	case jsErr != nil:
		return nil, jsErr
	case runErr != nil:
		return nil, describeErr(runErr, o.timeout, o.secrets)
	}
	return nil, hookErr("%s() never settled: it returned a promise that does not resolve", o.hook)
}

func isInterrupt(err error) bool {
	var intr *moejs.InterruptedError
	return errors.As(err, &intr)
}

// rejectionText renders the reason of a rejected promise.
func rejectionText(v moejs.Value) string {
	text := excText(&moejs.Exception{Value: v})
	return strings.TrimSpace(text)
}
