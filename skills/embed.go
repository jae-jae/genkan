// Package skills embeds the Genkan Agent Skill (genkan/*.md). go:embed cannot reach
// parent directories, so the files live next to this file and are exported here.
// The files are generic (no secrets, no placeholders); the server only adds a
// generated .env with the instance URL and token when it builds the download.
package skills

import "embed"

//go:embed genkan/SKILL.md genkan/reference.md genkan/plugin-dev.md
var files embed.FS

// File is one file of the Skill package, named relative to the zip root.
type File struct {
	Name    string
	Content []byte
}

// Files returns the embedded Skill files, byte-identical to the repository,
// SKILL.md first.
func Files() []File {
	out := make([]File, 0, 3)
	for _, n := range []string{"genkan/SKILL.md", "genkan/reference.md", "genkan/plugin-dev.md"} {
		b, err := files.ReadFile(n)
		if err != nil { // embedded at build time: cannot happen
			panic(err)
		}
		out = append(out, File{Name: n, Content: b})
	}
	return out
}
