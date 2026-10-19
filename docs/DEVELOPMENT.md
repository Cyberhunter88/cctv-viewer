# Local development on Windows and CachyOS

Use VS Code and a local Codex CLI from a normal checkout. Read `AGENTS.md` before work. Check `git status --short --branch` and the current branch, then work on a dedicated feature branch. Check GitHub for the current pull request state when needed. Keep local settings and credentials outside Git.

Install Node and Go, then run `npm ci`, `npm run typecheck`, `npm test`, `npm run build`, `go vet ./...`, and `go test -race ./...` for changes that affect both parts. Run relevant browser and workflow checks described in `AGENTS.md` when those areas change. Review `git diff --cached` before committing and run relevant tests before pushing.

## Install the pre-push hook

From the repository root, run the same Git command in PowerShell on Windows or a terminal on CachyOS:

```sh
git config --local core.hooksPath .githooks
```

The hook requires `node` and `git` on `PATH`. On CachyOS, ensure `.githooks/pre-push` is executable (`chmod +x .githooks/pre-push`). On Windows, Git for Windows runs it through its bundled shell. Verify with `git config --local --get core.hooksPath` and `node scripts/check-secrets.mjs --working-tree`. Git hooks are local configuration, so install on each clone. The hook reads Git's proposed ref updates and scans outgoing commits before the push is sent. A failed scan blocks the push; review the masked finding and remove the sensitive content. Never use `--no-verify` or redirect `core.hooksPath` to bypass it.

Run `node scripts/check-secrets.mjs --history` for a read-only audit of all locally available commits. The scanner reports path, commit, line, rule, risk, and masked value. Review findings manually; automated pattern checks cannot prove that a value is live. The older shared PowerShell scanner is outside this repository and is not required by this cross-platform hook.
