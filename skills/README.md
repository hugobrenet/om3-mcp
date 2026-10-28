# Skills

Each directory holds one [Agent Skill](https://agentskills.io/specification), a `SKILL.md` whose `metadata` key `opensvc/tools` lists, separated by spaces, the tools the skill uses: a client loading the skill adds them to the catalog of the run.
In the body, a tool to call is written between backticks and listed there; a tool only named, such as one never to use, is written without them.
`internal/tools/skills_test.go` checks both ways against the catalog of the server.
