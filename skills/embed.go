// Package skills embeds the Agent Skills of the server, one directory with a
// SKILL.md each under catalog/; internal/skills serves them.
package skills

import "embed"

// FS holds catalog/ and every file under it.
//
//go:embed catalog
var FS embed.FS
