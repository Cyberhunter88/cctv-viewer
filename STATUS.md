# Development status

Updated: 2026-10-18. This file records public, project-level information only.

## Current state

- Go backend and TypeScript frontend are maintained in this repository. `package.json` currently declares version 0.9.5.
- The frontend build creates `web/dist/app.js`; HTML, CSS, and theme JavaScript in `web/dist/` are maintained sources.
- The main branch contains the current project baseline; this workflow setup is being prepared on a feature branch.

## Open tasks

- Review the repository workflow changes in a pull request before merging.
- Validate the pre-push hook on CachyOS as well as Windows; the current setup was made on Windows.
- Keep checking outgoing commits and review future scanner findings manually. Do not rewrite published history without approval.

## Technical decisions

- Recordings stay read-only. Generated data, local configuration, and reports stay outside Git.
- Feature work goes through branches and pull requests; the owner merges manually.
- The repository uses a Node-based, versioned secret preflight so Windows and Linux can run the same check. The earlier shared PowerShell scanner is not present in this checkout.

## Next steps

1. Install and verify the Git hook on the CachyOS checkout.
2. Open a pull request after explicit push authorization.
