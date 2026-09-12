# `.claude/` — this repo's Claude Code layer

The global layer in `~/.claude/` (rules, skills, agents, hooks) applies everywhere. This folder holds only
what is specific to this repo.

| Layer | Here | Holds |
|---|---|---|
| memory | `memory/MEMORY.md` (index) · `memory/project_state.md` (resume point) · topic files | facts a new session needs that the code does not say |
| rules | `rules/*.md` with `paths:` frontmatter | conventions that only matter for some files here |
| skills | `skills/<name>/SKILL.md` | a repo-specific version of a global skill shadows it by delegation (the global skill reads this file) |
| agents | `agents/*.md` | bounded read-only tasks specific to this repo |
| scripts | `scripts/link-memory.sh` (auto-memory dir → this folder) · repo checks | plumbing |
| settings | `settings.json` (project permissions/env, committed) · `settings.local.json` (per machine, gitignored) | |
| guards | `protected-paths.txt` — globs the `git-add-bulk-guard` hook treats as never-edit-in-place | |

Conventions: memory files `memory/<type>_<slug>.md`, frontmatter `name:` = slug, `[[wikilinks]]` between
files, `[Title](file.md)` only in `MEMORY.md`. Check with `python ~/.claude/scripts/check-config.py --root .claude`.
