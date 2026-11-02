// Package skills embeds the Agent Skill template. go:embed cannot reach parent
// directories, so the template lives next to this file and is exported here.
package skills

import _ "embed"

// SkillTemplate is genkan/SKILL.md with {{GENKAN_URL}} and {{GENKAN_TOKEN}} placeholders.
//
//go:embed genkan/SKILL.md
var SkillTemplate string
