# Docs

## Tree

| Path | Holds |
| --- | --- |
| `terminology/` | the settled word for every concept — owned by `Skill(smk-skills:terminology)` |
| `architecture/` | system structure: modules, boundaries, data flow |
| `commands/` | runnable operational docs — build, run, seed, deploy |
| `commands/scratch/` | ad-hoc commands kept from sessions, each with its outcome |
| `contracts/` | external interfaces: API contracts, schemas, event formats |
| `adr/` | decisions, with rationale and rejected options |
| `learnings/` | gotchas and unexpected behaviour |
| `todos/` | deferred work, each with its trigger (closed → `todos/done/`) |
| `issues/` | known problems with a live cost (closed → `issues/resolved/`) |
| `plans/` | implementation plans, live while the work runs (finished → `plans/done/`) |
| `.trash/` | quarantine — never read, never linked |

A directory exists only when it has content. In a multi-deployable repo,
`architecture/` and `commands/` get one subfolder per component; everything else stays
flat.

## Routing

| The thing is… | Goes in |
| --- | --- |
| a choice between options, with consequences | `adr/` |
| a surprise about how something behaves | `learnings/` |
| work consciously deferred, with a trigger | `todos/` |
| something currently broken | `issues/` |
| a structural fact about where code lives | `architecture/` |
| a command runnable again on purpose | `commands/` |
| a one-off command worth keeping, with its outcome | `commands/scratch/` |
| an interface something outside this repo consumes | `contracts/` |
| multi-step work about to be done | `plans/` |
| a name | `terminology/` — never defined inline anywhere else |

Filenames are `YYYY-MM-DD_slug.md`. The date is the creation date and never changes.

## Staleness

- **Agent-editable** (`architecture/`, `commands/`, `contracts/`, `plans/`, `todos/`,
  `issues/`): must match the code. A mismatch is a bug. Substantive changes get a line
  in `## Amendments`.
- **Read-only** (`adr/`, `learnings/`): never edited. A changed decision is a new file
  that supersedes the old one.
- **Working state** (`todos/`, `issues/`, `plans/`): closed entries move to `done/` /
  `resolved/` and stay in the project.
