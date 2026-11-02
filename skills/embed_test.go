package skills

import (
	"strings"
	"testing"
)

func TestTemplate(t *testing.T) {
	_, body, ok := strings.Cut(strings.TrimPrefix(SkillTemplate, "---\n"), "\n---\n")
	if !ok || !strings.HasPrefix(SkillTemplate, "---\nname: genkan\n") {
		t.Fatal("frontmatter missing")
	}
	if !strings.HasPrefix(strings.TrimSpace(body), "> **WARNING") || !strings.Contains(strings.SplitN(strings.TrimSpace(body), "\n\n", 2)[0], "secret API token") {
		t.Fatalf("token warning must directly follow the frontmatter: %.120q", body)
	}
	for _, want := range []string{"{{GENKAN_URL}}", "{{GENKAN_TOKEN}}",
		`claude mcp add --transport http genkan {{GENKAN_URL}}/mcp --header "Authorization: Bearer {{GENKAN_TOKEN}}"`} {
		if !strings.Contains(SkillTemplate, want) {
			t.Errorf("template lacks %s", want)
		}
	}
	if strings.Contains(SkillTemplate, "$GENKAN_") {
		t.Error("env-var indirection should be gone")
	}
}
