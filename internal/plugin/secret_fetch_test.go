package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestSecretFetchValidationAndPolicy(t *testing.T) {
	e := newEnv(t)
	for _, strict := range []bool{false, true} {
		src := plug(fmt.Sprintf(`{id:"private-fetch",name:"Private",version:"1",widgets:[{id:"main",name:"Main",config:[{key:"url",label:"URL",type:"secret",allowFetch:true,verifyTLS:%t}]}]}`, strict), `export function widget(){return {body:[]}}`)
		e.install(src)
	}
	f := &ConfigField{Type: "secret", AllowFetch: true}
	bad := []string{"fixture-private", "//example.test/fixture-private", "ftp://example.test/fixture-private", "https://user:fixture-private@example.test/", "https://*.example.test/fixture-private", "https://example.test/#fixture-private", "https://example.test/%zz?key=fixture-private", "https://example.test:bad/?key=fixture-private"}
	for _, raw := range bad {
		if _, err := checkValue(f, raw); err == nil || strings.Contains(err.Error(), "fixture-private") {
			t.Fatalf("invalid URL not safely rejected: %v", err)
		}
		hosts, verified := fetchPolicyFromConfig([]ConfigField{{Key: "url", Type: "secret", AllowFetch: true, VerifyTLS: true}}, map[string]any{"url": raw})
		if len(hosts) != 0 || len(verified) != 0 {
			t.Fatal("invalid saved secret granted network access")
		}
	}
	for _, tc := range []struct{ url, host string }{
		{"https://EXAMPLE.test/private/path?token=fixture-private", "example.test:443"},
		{"http://example.test/private?token=fixture-private", "example.test:80"},
		{"https://[::1]:8443/private?token=fixture-private", "[::1]:8443"},
	} {
		if _, err := checkValue(f, tc.url); err != nil {
			t.Fatal(err)
		}
		hosts, verified := fetchPolicyFromConfig([]ConfigField{{Key: "url", Type: "secret", AllowFetch: true, VerifyTLS: true}, {Key: "ordinary", Type: "secret"}}, map[string]any{"url": tc.url, "ordinary": "https://other.test/private"})
		if len(hosts) != 1 || hosts[0] != tc.host || len(verified) != 1 || verified[0] != tc.host {
			t.Fatal("policy must contain only normalized host and port")
		}
	}
	if _, err := checkValue(&ConfigField{Type: "secret"}, "ordinary-token"); err != nil {
		t.Fatal(err)
	}
}

const secretFetchSource = `export const manifest={id:"private-fetch",name:"Private",version:"1",
 config:[{key:"target",label:"Target",type:"secret"}],
 widgets:[{id:"main",name:"Main",config:[
 {key:"url",label:"Subscription",type:"secret",allowFetch:true},
 {key:"verified",label:"Verified",type:"url",allowFetch:true,verifyTLS:true}]}],
 schedules:[{id:"sample",every:60}]};
 async function check(ctx){
  if (!genkan.capabilities.secretFetch) throw new Error("missing capability");
  const target=ctx.instance?.config.url || ctx.config.target;
  genkan.log(target);
  try{const r=await fetch(target);await r.text();return "accepted"}catch(_){return "rejected"}
 }
 export async function widget(ctx){return {body:[{type:"text",text:await check(ctx)}]}}
 export async function schedule(ctx){return await check(ctx)}
 export async function action(ctx){genkan.log(ctx.instance.config.url);throw new Error(ctx.instance.config.url)}
`

func TestSecretFetchHooksMaskingAndRevocation(t *testing.T) {
	s := newTLSStub(t) // intentionally untrusted: no forced TLS verification
	e := newEnv(t)
	e.install(secretFetchSource)
	raw := s.URL + "/private-path?token=fixture-private-token"
	info, err := e.m.Patch("private-fetch", PatchRequest{Config: map[string]any{"target": raw}})
	if err != nil {
		t.Fatal(err)
	}
	checkMasked := func(v any) {
		t.Helper()
		b, _ := json.Marshal(v)
		for _, secret := range []string{raw, "fixture-private-token", "private-path"} {
			if strings.Contains(string(b), secret) {
				t.Fatal("private URL leaked")
			}
		}
	}
	checkMasked(info)
	run := func(id, hook string, cfg map[string]any, want string) {
		t.Helper()
		res, err := e.m.Test(context.Background(), id, TestRequest{Hook: hook, Config: cfg})
		if err != nil || !res.OK || !strings.Contains(fmt.Sprint(res.Result), want) {
			t.Fatalf("%s/%s: %+v %v", id, hook, res, err)
		}
		checkMasked(res)
	}
	ent, _, _ := e.m.entryFor("private-fetch")
	for _, strict := range []bool{false, true, false, true} {
		cfg := map[string]any{"url": raw}
		want := "accepted"
		if strict {
			cfg["verified"] = s.URL
			want = "rejected"
		}
		run("private-fetch", "widget", cfg, want)
		current, _, _ := e.m.entryFor("private-fetch")
		if current != ent || ent.pool.idleCount() != 1 {
			t.Fatal("expected same pooled runtime")
		}
	}
	result, err := e.m.Test(context.Background(), "private-fetch", TestRequest{Hook: "action", Action: "probe", Config: map[string]any{"url": raw}})
	if err != nil || result.OK {
		t.Fatal("expected sanitized action error")
	}
	checkMasked(result)
	w := addWidget(t, e, "private-fetch", "main", map[string]any{"url": raw})
	checkMasked(w)
	if w.Config["url"] != "" || len(w.SecretsSet) != 1 {
		t.Fatal("secret must be masked in admin response")
	}
	checkMasked(e.m.Present(e.st.Widgets(), false))
	run("private-fetch", "schedule", nil, "accepted")
	run("private-fetch", "widget", nil, "rejected") // other instances do not inherit this grant
	e.install(strings.ReplaceAll(secretFetchSource, `"private-fetch"`, `"other-private-fetch"`))
	if _, err = e.m.Patch("other-private-fetch", PatchRequest{Config: map[string]any{"target": raw}}); err != nil {
		t.Fatal(err)
	}
	run("other-private-fetch", "schedule", nil, "rejected")
	if _, err = e.m.UpdateWidget(w.ID, WidgetPatch{Config: map[string]any{"verified": s.URL}, Merge: true}); err != nil {
		t.Fatal(err)
	}
	run("private-fetch", "schedule", nil, "rejected")
	if _, err = e.m.UpdateWidget(w.ID, WidgetPatch{Config: map[string]any{"verified": nil}, Merge: true}); err != nil {
		t.Fatal(err)
	}
	run("private-fetch", "schedule", nil, "accepted")
	if _, err = e.m.UpdateWidget(w.ID, WidgetPatch{Config: map[string]any{"url": "https://other.example.test/private?token=fixture-private-token"}}); err != nil {
		t.Fatal(err)
	}
	run("private-fetch", "schedule", nil, "rejected")
	if _, err = e.m.UpdateWidget(w.ID, WidgetPatch{Config: map[string]any{"url": raw}}); err != nil {
		t.Fatal(err)
	}
	run("private-fetch", "schedule", nil, "accepted")
	if err = e.m.DeleteWidget(w.ID); err != nil {
		t.Fatal(err)
	}
	run("private-fetch", "schedule", nil, "rejected")
	logs, err := e.m.Logs("private-fetch")
	if err != nil {
		t.Fatal(err)
	}
	checkMasked(logs)
}
