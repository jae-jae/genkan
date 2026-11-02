// Package skills holds the Genkan Agent Skill (skills/genkan/*.md). Users fetch it straight
// from GitHub, so there is no Go code here; these tests keep the Markdown honest.
package skills

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSkillFiles(t *testing.T) {
	skill, ref := read(t, "genkan/SKILL.md"), read(t, "genkan/reference.md")

	_, body, ok := strings.Cut(strings.TrimPrefix(skill, "---\n"), "\n---\n")
	if !ok || !strings.HasPrefix(skill, "---\nname: genkan\ndescription: ") {
		t.Fatal("frontmatter missing")
	}
	if w := strings.SplitN(strings.TrimSpace(body), "\n\n", 2)[0]; !strings.HasPrefix(w, "> **WARNING") || !strings.Contains(w, "~/.config/genkan/env") {
		t.Fatalf("token warning must directly follow the frontmatter: %.160q", body)
	}
	if !strings.Contains(skill, "(reference.md)") {
		t.Error("SKILL.md must link to reference.md")
	}
	secret := regexp.MustCompile(`gk_[a-z2-7]{32}`)
	for name, md := range map[string]string{"SKILL.md": skill, "reference.md": ref} {
		// the Skill is the REST-only alternative to MCP, generic and free of secrets
		if strings.Contains(strings.ToLower(md), "mcp") {
			t.Errorf("%s mentions MCP", name)
		}
		if strings.Contains(md, "{{") {
			t.Errorf("%s still contains template placeholders", name)
		}
		if secret.MatchString(md) {
			t.Errorf("%s contains something that looks like an API token", name)
		}
		for _, want := range []string{"$GENKAN_URL", "gk "} {
			if !strings.Contains(md, want) {
				t.Errorf("%s lacks %q", name, want)
			}
		}
	}
	if !strings.Contains(skill, "~/.config/genkan/env") || !strings.Contains(skill, "GENKAN_TOKEN") {
		t.Error("SKILL.md must explain where the credentials come from")
	}
}

// Every REST route an agent may use must be documented in reference.md, so the Skill cannot
// silently drift from the API on main.
func TestReferenceCoversRoutes(t *testing.T) {
	api := read(t, "../internal/server/api.go")
	ref := read(t, "genkan/reference.md")
	skip := regexp.MustCompile(`^(GET /healthz|POST /api/auth/|GET /api/token|POST /api/token/|GET /api/skill|GET /api/events|GET /uploads/|POST /api/links/\{id\}/click|/mcp|/api/|/$)`)
	n := 0
	for _, m := range regexp.MustCompile(`m\.HandleFunc\("([^"]+)"`).FindAllStringSubmatch(api, -1) {
		route := m[1]
		if skip.MatchString(route) {
			continue
		}
		n++
		if !strings.Contains(ref, "### "+route+"\n") {
			t.Errorf("reference.md has no section %q", "### "+route)
		}
	}
	if n < 20 {
		t.Fatalf("only %d routes checked; is the route regexp still right?", n)
	}
	// and never document what an agent must not call
	for _, bad := range []string{"### POST /api/auth", "### POST /api/token/regenerate", "### GET /api/events"} {
		if strings.Contains(ref, bad) {
			t.Errorf("reference.md must not document %q", bad)
		}
	}
}

func TestEmbeddedFilesMatchRepo(t *testing.T) {
	for _, f := range Files() {
		if got := read(t, f.Name); got != string(f.Content) {
			t.Errorf("%s: embedded copy differs from the file on disk", f.Name)
		}
	}
	if fs := Files(); len(fs) != 2 || fs[0].Name != "genkan/SKILL.md" || fs[1].Name != "genkan/reference.md" {
		t.Fatalf("unexpected embedded files: %v", fs)
	}
}
