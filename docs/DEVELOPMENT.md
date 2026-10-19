# Local development on Windows and CachyOS

Use VS Code and a local Codex CLI from a normal checkout. Read `AGENTS.md` before work. Check `git status --short --branch` and the current branch before editing. Keep local settings and credentials outside Git. The commands below work in PowerShell and a POSIX shell.

## First setup on a new computer

1. Install Git (Git for Windows on Windows), VS Code, Node.js in the version recorded in `.node-version`, and the Go version in `go.mod`. Install Python 3, FFmpeg, and the matching Playwright browsers when running the corresponding tests. Check `node --version`, `go version`, and `git --version` against the project and CI requirements. If PowerShell blocks `npm.ps1` under the local execution policy, run `npm.cmd` with the same arguments.
2. Clone the GitHub repository and open the clone in VS Code. Configure Git authentication on this computer without storing credentials in the repository.
3. From the repository root, run `git config --local core.hooksPath .githooks`, `npm ci`, and `node scripts/check-secrets.mjs --working-tree`. On CachyOS, confirm that `.githooks/pre-push` is executable. Repeat the hook setup for every clone because local Git configuration is not shared through GitHub.
4. Run the checks relevant to the task. For changes affecting both application parts, run `npm run typecheck`, `npm test`, `npm run build`, `go vet ./...`, and `go test -race ./...`. See `AGENTS.md` for browser and workflow checks.

## Start a task

Check `git status --short --branch` and the GitHub PR state. If the checkout has local changes, keep working on its branch or save the changes in a separate commit before switching; never discard or overwrite them. With a clean checkout, start each new task from the current remote main:

```sh
git fetch origin
git switch --no-track -c codex/mein-thema origin/main
```

Choose a unique topic. `--no-track` prevents the new feature branch from using `origin/main` as its push target. If the task already has a branch, use that branch instead of creating another one. Do not commit directly to `main`. Stage only related files, inspect `git diff --cached` for secrets and local artifacts, run relevant tests, then commit. For the first authorized push, use `git push -u origin codex/mein-thema`; later pushes use the feature branch as their upstream. Create a PR against `main` and let the owner merge it manually.

## Continue the same feature on another computer

On the first computer, inspect `git status --short --branch`. Finish or explicitly set aside every local change: uncommitted changes do not travel through GitHub. Review the staged diff and commit the intended work. Push the feature branch normally after authorization; do not force-push or bypass the pre-push hook.

On the second computer, first inspect `git status --short --branch`. Stop if it contains unsaved changes that would conflict with switching or pulling. Fetch without changing working files, then switch to the same branch:

```sh
git fetch origin
git switch codex/mein-thema
```

If that branch does not exist locally yet, use `git switch --track origin/codex/mein-thema` instead. Confirm the branch with `git status --short --branch`. If the local branch already exists and the working tree is clean, update it with `git pull --ff-only`. If Git cannot fast-forward, inspect the differing commits and resolve the divergence deliberately; do not reset, force-push, or rebase published commits. Repeat this process when changing back to the first computer. Use the same branch and PR throughout the task.

## After the PR is merged

Check that the PR is merged on GitHub. Preserve any uncommitted changes before switching. Refresh main without overwriting local work:

```sh
git fetch origin
git switch main
git pull --ff-only origin main
```

If the local `main` has diverged, inspect it instead of resetting it. Start the next task with a new name, for example `git switch -c codex/naechstes-thema origin/main`. Branch deletion is optional and must be a separate deliberate action.

## Install the pre-push hook

From the repository root, run the same Git command in PowerShell on Windows or a terminal on CachyOS:

```sh
git config --local core.hooksPath .githooks
```

The hook requires `node` and `git` on `PATH`. On CachyOS, ensure `.githooks/pre-push` is executable (`chmod +x .githooks/pre-push`). On Windows, Git for Windows runs it through its bundled shell. Verify with `git config --local --get core.hooksPath` and `node scripts/check-secrets.mjs --working-tree`. Git hooks are local configuration, so install on each clone. The hook reads Git's proposed ref updates and scans outgoing commits before the push is sent. A failed scan blocks the push; review the masked finding and remove the sensitive content. Never use `--no-verify` or redirect `core.hooksPath` to bypass it.

Run `node scripts/check-secrets.mjs --history` for a read-only audit of all locally available commits. The scanner reports path, commit, line, rule, risk, and masked value. Review findings manually; automated pattern checks cannot prove that a value is live. The older shared PowerShell scanner is outside this repository and is not required by this cross-platform hook.
