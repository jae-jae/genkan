package mcp

import (
	"encoding/json"
	"testing"

	"github.com/jae-jae/genkan/internal/store"
	"github.com/jae-jae/genkan/internal/update"
)

func TestUpdateStatusTool(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := New(st, "v1.0.0")

	res := call(t, s, "update_status", nil)
	if res["isError"] != true {
		t.Fatalf("without a Service: %v", res)
	}

	s.Updates = update.New(update.Config{DataDir: st.Dir(), Version: "v1.0.0", Getenv: func(string) string { return "" }})
	defer s.Updates.Close()
	res = call(t, s, "update_status", nil)
	if res["isError"] != nil {
		t.Fatalf("%v", res)
	}
	var got update.Status
	b, _ := json.Marshal(res["structuredContent"])
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Current != "v1.0.0" || got.Releases == nil || got.Available {
		t.Fatalf("%+v", got)
	}

	// read-only annotation
	m, _ := rpc(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	for _, tl := range m["result"].(map[string]any)["tools"].([]any) {
		tm := tl.(map[string]any)
		if tm["name"] == "update_status" {
			ann := tm["annotations"].(map[string]any)
			if ann["readOnlyHint"] != true || ann["destructiveHint"] != false {
				t.Fatalf("annotations %v", ann)
			}
			return
		}
	}
	t.Fatal("tool not listed")
}
