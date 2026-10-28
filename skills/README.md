# Skills

Each directory of `catalog/` holds one [Agent Skill](https://agentskills.io/specification), a `SKILL.md` whose `metadata` key `opensvc/tools` lists, separated by spaces, the tools the skill uses: a client loading the skill adds them to the catalog of the run.
In the body, a tool to call is written between backticks and listed there; a tool only named, such as one never to use, is written without them.
`internal/tools/skills_test.go` checks both ways against the catalog of the server.

The binary embeds `catalog/` (`embed.go`), files beginning with `.` or `_` apart, and `internal/skills` serves it on both listeners, as the MCP Skills extension, `io.modelcontextprotocol/skills` ([SEP-2640](https://modelcontextprotocol.io/extensions/skills/overview)), describes:

- the extension is declared with the `resources` capability, `directoryRead` false: no `resources/directory/read`;
- every file of a skill is a resource, `skill://<name>/<path>`, read by `resources/read`; the resource of a `SKILL.md` takes the name and description of its frontmatter, and `text/markdown`;
- `skills/list` gives every skill: the URI of its `SKILL.md`, its frontmatter as written, string values staying strings, and the manifest of its files, `SKILL.md` included, each with its URI, `sha256:<hex>` digest and size, computed once at start from the embedded bytes;
- `skills/get` gives the entry of the `SKILL.md` URI it is passed, and refuses any other URI with `-32602`;
- both results carry `resultType` `complete`, `ttlMs` and `cacheScope` `public`.

The go-sdk does not serve the extension yet: `skills/list` and `skills/get` are custom methods, to replace by those of the SDK when it does.
A skill whose frontmatter breaks the specification, a `name` other than its directory or no `description`, or holds a value other than a string or a map of strings on one level, the form the OpenSVC agent reads (a long description may be a `|` or `>` block), stops the start of the server; so does anything the server would not serve: a file beside the directories, a directory without `SKILL.md`, a file that is no UTF-8 text.
The MCP serves the skills only; activating one, and keeping its URIs out of a generic resource reading, is the client's.
