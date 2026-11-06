package plugin

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVerifyTLSValidation(t *testing.T) {
	e := newEnv(t)
	for _, field := range []string{
		`type:"url",allowFetch:true,verifyTLS:"true"`,
		`type:"url",allowFetch:true,verifyTLS:null`,
		`type:"url",verifyTLS:true`,
		`type:"string",verifyTLS:false`,
	} {
		err := e.installErr(plug(`{id:"invalid-tls",name:"TLS",version:"1",config:[{key:"origin",label:"Origin",`+field+`}],widgets:[{id:"main",name:"Main"}]}`, `export function widget(){return {body:[]}}`))
		if !strings.Contains(err.Error(), "verifyTLS") {
			t.Fatal(err)
		}
	}
}

const tlsTestSource = `export const manifest = {
 id:"tls-test",name:"TLS",version:"1",
 config:[
  {key:"target",type:"url",label:"Target"},
  {key:"legacy",type:"url",label:"Legacy",allowFetch:true},
  {key:"strict",type:"url",label:"Strict",allowFetch:true,verifyTLS:true}
 ],
 widgets:[{id:"main",name:"Main",config:[
  {key:"legacy",type:"url",label:"Legacy",allowFetch:true,verifyTLS:false},
  {key:"strict",type:"url",label:"Strict",allowFetch:true,verifyTLS:true}
 ]}],
 schedules:[{id:"sample",every:3600}]
};
async function check(ctx) {
 if (!genkan.capabilities.verifyTLS) throw new Error("missing capability");
 try { const r=await fetch(ctx.config.target); await r.text(); return "accepted"; }
 catch (_) { return "rejected"; }
}
export async function widget(ctx) {return {body:[{type:"text",text:await check(ctx)}]};}
export async function schedule(ctx) {return await check(ctx);}
`

func newTLSStub(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Subscription-Userinfo", "upload=1; download=2; total=100; expire=2000000000")
		fmt.Fprint(w, "test")
	}))
	s.Config.ErrorLog = log.New(io.Discard, "", 0)
	s.StartTLS()
	t.Cleanup(s.Close)
	return s
}

func TestVerifyTLSHooksAndPool(t *testing.T) {
	s := newTLSStub(t)
	e := newEnv(t)
	e.install(tlsTestSource)
	run := func(pluginCfg, widgetCfg map[string]any, hook, want string) {
		t.Helper()
		pluginCfg["target"] = s.URL
		if _, err := e.m.Patch("tls-test", PatchRequest{Config: pluginCfg}); err != nil {
			t.Fatal(err)
		}
		res, err := e.m.Test(context.Background(), "tls-test", TestRequest{Hook: hook, Config: widgetCfg})
		if err != nil || !res.OK || !strings.Contains(fmt.Sprint(res.Result), want) {
			t.Fatalf("%s: %+v %v", hook, res, err)
		}
	}
	// Repeated calls reuse pooled runtimes; strict policy must not inherit a
	// lenient connection, and a previous strict declaration must not linger.
	run(map[string]any{"legacy": s.URL}, nil, "widget", "accepted")
	run(map[string]any{"legacy": s.URL, "strict": s.URL}, nil, "widget", "rejected")
	run(map[string]any{"legacy": s.URL}, nil, "widget", "accepted")
	run(map[string]any{}, map[string]any{"legacy": s.URL}, "widget", "accepted")
	run(map[string]any{}, map[string]any{"strict": s.URL}, "widget", "rejected")
	run(map[string]any{"strict": s.URL}, map[string]any{"legacy": s.URL}, "widget", "rejected")
	run(map[string]any{"legacy": s.URL}, map[string]any{"strict": s.URL}, "widget", "rejected")
	run(map[string]any{}, map[string]any{"legacy": s.URL}, "schedule", "rejected")
	run(map[string]any{"legacy": s.URL}, nil, "schedule", "accepted")
	run(map[string]any{"strict": s.URL}, nil, "schedule", "rejected")
}

func TestVerifyTLSTrustedCertificateAndRedirect(t *testing.T) {
	trusted := newTLSStub(t)
	untrusted := newTLSStub(t)
	// httptest reuses a certificate, so use a distinct private CA rejection by
	// trusting neither first, then installing the test CA in the strict client.
	e := newEnv(t)
	e.install(tlsTestSource)
	if _, err := e.m.Patch("tls-test", PatchRequest{Config: map[string]any{"target": trusted.URL, "strict": trusted.URL}}); err != nil {
		t.Fatal(err)
	}
	res, err := e.m.Test(context.Background(), "tls-test", TestRequest{Hook: "schedule"})
	if err != nil || !res.OK || res.Result != "rejected" {
		t.Fatalf("untrusted: %+v %v", res, err)
	}

	roots := x509.NewCertPool()
	roots.AddCert(trusted.Certificate())
	old := http.DefaultTransport
	transport := old.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = old; transport.CloseIdleConnections() })
	// A fresh manager builds transports with the trusted test CA.
	fresh := newEnv(t)
	fresh.install(tlsTestSource)
	if _, err := fresh.m.Patch("tls-test", PatchRequest{Config: map[string]any{"target": trusted.URL, "strict": trusted.URL}}); err != nil {
		t.Fatal(err)
	}
	res, err = fresh.m.Test(context.Background(), "tls-test", TestRequest{Hook: "schedule"})
	if err != nil || !res.OK || res.Result != "accepted" {
		t.Fatalf("trusted: %+v %v", res, err)
	}

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, untrusted.URL, http.StatusFound) }))
	t.Cleanup(redirect.Close)
	// The original manager's strict transport still does not trust this CA.
	if _, err := e.m.Patch("tls-test", PatchRequest{Config: map[string]any{"target": redirect.URL, "legacy": redirect.URL, "strict": untrusted.URL}}); err != nil {
		t.Fatal(err)
	}
	res, err = e.m.Test(context.Background(), "tls-test", TestRequest{Hook: "schedule"})
	if err != nil || !res.OK || res.Result != "rejected" {
		t.Fatalf("redirect: %+v %v", res, err)
	}
}

func TestVerifyTLSPolicyNormalization(t *testing.T) {
	schema := []ConfigField{
		{Key: "a", AllowFetch: true}, {Key: "b", AllowFetch: true, VerifyTLS: true},
		{Key: "c", AllowFetch: true}, {Key: "d", AllowFetch: true, VerifyTLS: true},
	}
	hosts, verified := fetchPolicyFromConfig(schema, map[string]any{
		"a": "https://EXAMPLE.test/path", "b": "https://example.test:443/", "c": "https://[::1]:8443/", "d": "https://[::1]:8443/",
	})
	if len(hosts) != 4 || len(verified) != 2 || len(lenientHosts(hosts, verified)) != 0 {
		t.Fatalf("%v %v", hosts, verified)
	}
}

func TestVerifyTLSWallpaperDownload(t *testing.T) {
	s := newTLSStub(t)
	e := newEnv(t)
	e.install(plug(`{id:"tls-wallpaper",name:"TLS",version:"1",config:[{key:"url",type:"url",label:"URL",allowFetch:true,verifyTLS:true}],wallpapers:[{id:"main",name:"Main"}]}`, `export function wallpaper(ctx){return {url:ctx.config.url}}`))
	if _, err := e.m.Patch("tls-wallpaper", PatchRequest{Config: map[string]any{"url": s.URL}}); err != nil {
		t.Fatal(err)
	}
	_, err := e.m.Wallpaper(context.Background(), "tls-wallpaper", "main", "en")
	if err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("expected TLS rejection before reading image: %v", err)
	}
}

func TestVerifyTLSPooledInstanceSwitch(t *testing.T) {
	s := newTLSStub(t)
	e := newEnv(t)
	e.install(tlsTestSource)
	if _, err := e.m.Patch("tls-test", PatchRequest{Config: map[string]any{"target": s.URL}}); err != nil {
		t.Fatal(err)
	}
	ent, _, _ := e.m.entryFor("tls-test")
	for _, tc := range []struct{ field, want string }{
		{"legacy", "accepted"}, {"strict", "rejected"}, {"legacy", "accepted"}, {"strict", "rejected"},
	} {
		res, err := e.m.Test(context.Background(), "tls-test", TestRequest{Hook: "widget", Config: map[string]any{tc.field: s.URL}})
		if err != nil || !res.OK || !strings.Contains(fmt.Sprint(res.Result), tc.want) {
			t.Fatalf("%s: %+v %v", tc.field, res, err)
		}
		current, _, _ := e.m.entryFor("tls-test")
		if current != ent || ent.pool.idleCount() != 1 {
			t.Fatal("test must reuse the same pooled runtime")
		}
	}
}

func TestScheduleWidgetFetchScopeAndRevocation(t *testing.T) {
	s := newTLSStub(t)
	e := newEnv(t)
	e.install(tlsTestSource)
	e.install(strings.ReplaceAll(tlsTestSource, `"tls-test"`, `"foreign-tls"`))
	for _, id := range []string{"tls-test", "foreign-tls"} {
		if _, err := e.m.Patch(id, PatchRequest{Config: map[string]any{"target": s.URL}}); err != nil {
			t.Fatal(err)
		}
	}
	run := func(id, hook, want string) {
		t.Helper()
		res, err := e.m.Test(context.Background(), id, TestRequest{Hook: hook})
		if err != nil || !res.OK || !strings.Contains(fmt.Sprint(res.Result), want) {
			t.Fatalf("%s/%s: %+v %v", id, hook, res, err)
		}
	}
	w := addWidget(t, e, "tls-test", "main", map[string]any{"legacy": s.URL})
	run("tls-test", "schedule", "accepted")
	run("foreign-tls", "schedule", "rejected")
	run("tls-test", "widget", "rejected")
	strict := addWidget(t, e, "tls-test", "main", map[string]any{"strict": s.URL})
	run("tls-test", "schedule", "rejected") // strict wins across saved instances
	if err := e.m.DeleteWidget(strict.ID); err != nil {
		t.Fatal(err)
	}
	run("tls-test", "schedule", "accepted")
	if _, err := e.m.UpdateWidget(w.ID, WidgetPatch{Config: map[string]any{"legacy": "https://other.example.test"}}); err != nil {
		t.Fatal(err)
	}
	run("tls-test", "schedule", "rejected")
	if _, err := e.m.UpdateWidget(w.ID, WidgetPatch{Config: map[string]any{"legacy": s.URL}}); err != nil {
		t.Fatal(err)
	}
	run("tls-test", "schedule", "accepted")
	if err := e.m.DeleteWidget(w.ID); err != nil {
		t.Fatal(err)
	}
	run("tls-test", "schedule", "rejected")
}
