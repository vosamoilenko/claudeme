# All accounts already share one projects root

The "are we missing projects from other accounts?" question is **answered: no.**
Every account symlinks `projects` at the same shared directory, so
`usage.ProjectsRoot()` — `~/.config/claudeme/shared/projects` — is the complete
set. There is no per-account transcript store to merge.

```
~/.config/claudeme/accounts/jdselbach@gmail.com/projects            -> ../../shared/projects
~/.config/claudeme/accounts/samoilenkovolodymyr@gmail.com/projects  -> ../../shared/projects
~/.config/claudeme/accounts/volodymyr.samoilenko@sclable.com/projects -> ../../shared/projects
```

`~/.claude/projects` is empty (the live account's data lives in the shared dir
via the same symlink mechanism), so it is not a second source either.

Verify in one line:

```sh
ls -la ~/.config/claudeme/accounts/*/projects ~/.claude/projects
```

Caveat worth one check if the question comes back: this holds for accounts
`claudeme` manages. A transcript directory written before an account was
adopted into `claudeme`, or by a plain `claude` install pointing at
`~/.claude/projects`, would not be here. `~/.claude/projects` is currently
empty, so nothing is stranded today.
