package skills

import (
	"strings"
	"testing"
	"testing/fstest"
)

// A skill whose frontmatter breaks the Agent Skills specification stops the
// start of the server: it would be served with a name or a manifest a client
// refuses.
func TestLoadSkillsRefusesAnInvalidSkill(t *testing.T) {
	for name, tc := range map[string]struct {
		files map[string]string
		want  string
	}{
		"no frontmatter":     {map[string]string{"a/SKILL.md": "# A\n"}, "does not begin"},
		"unclosed":           {map[string]string{"a/SKILL.md": "---\nname: a\n"}, "not closed"},
		"name not the dir":   {map[string]string{"a/SKILL.md": "---\nname: b\ndescription: d\n---\n"}, "directory name"},
		"uppercase name":     {map[string]string{"A/SKILL.md": "---\nname: A\ndescription: d\n---\n"}, "directory name"},
		"no description":     {map[string]string{"a/SKILL.md": "---\nname: a\n---\n"}, "description"},
		"no skill":           {map[string]string{}, "no skill"},
		"non-string map key": {map[string]string{"a/SKILL.md": "---\nname: a\ndescription: d\nmetadata:\n  1: x\n---\n"}, "not a string"},
		"skill.md":           {map[string]string{"a/skill.md": "---\nname: a\ndescription: d\n---\n"}, "SKILL.md"},
		"file beside":        {map[string]string{"a/SKILL.md": "---\nname: a\ndescription: d\n---\n", "notes.md": "x\n"}, "not a skill directory"},
		"image":              {map[string]string{"a/SKILL.md": "---\nname: a\ndescription: d\n---\n", "a/assets/x.png": "\x89PNG"}, "text files only"},
		"long description":   {map[string]string{"a/SKILL.md": "---\nname: a\ndescription: " + strings.Repeat("é", 1025) + "\n---\n"}, "description"},
	} {
		t.Run(name, func(t *testing.T) {
			skillFS := fstest.MapFS{}
			for file, content := range tc.files {
				skillFS[file] = &fstest.MapFile{Data: []byte(content)}
			}
			if _, _, err := loadSkills(skillFS); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

// The manifest lists every file of the skill, SKILL.md and those beside it,
// and the frontmatter keeps a nested string as a string.
func TestLoadSkillsManifest(t *testing.T) {
	skillFS := fstest.MapFS{
		"a/SKILL.md":            {Data: []byte("---\nname: a\ndescription: d\nmetadata:\n  opensvc/tools: x y\n---\nbody\n")},
		"a/references/notes.md": {Data: []byte("notes\n")},
	}
	entries, files, err := loadSkills(skillFS)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].URI != "skill://a/SKILL.md" || len(entries[0].Resources) != 2 || len(files) != 2 {
		t.Fatalf("entries = %+v", entries)
	}
	if tools, ok := entries[0].Frontmatter["metadata"].(map[string]any)["opensvc/tools"].(string); !ok || tools != "x y" {
		t.Fatalf("frontmatter = %v", entries[0].Frontmatter)
	}
	for _, file := range entries[0].Resources {
		if !strings.HasPrefix(file.Digest, "sha256:") || len(file.Digest) != len("sha256:")+64 || file.Size == 0 {
			t.Errorf("file = %+v", file)
		}
	}
	if files[1].name != "references/notes.md" || files[1].mimeType != "text/markdown" {
		t.Errorf("supporting file = %+v", files[1])
	}
}

// The limit of a description counts characters, not bytes.
func TestLoadSkillsCountsDescriptionCharacters(t *testing.T) {
	skillFS := fstest.MapFS{"a/SKILL.md": {Data: []byte("---\nname: a\ndescription: " + strings.Repeat("é", 1024) + "\n---\n")}}
	if _, _, err := loadSkills(skillFS); err != nil {
		t.Fatal(err)
	}
}
