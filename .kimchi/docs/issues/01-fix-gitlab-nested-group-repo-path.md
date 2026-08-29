# 01 — Fix GitLab nested-group repo path derivation in intake observer

**What to build:** The intake observer's repo-path derivation currently truncates GitLab project paths to the last two segments — a GitHub assumption that breaks GitLab nested groups (e.g. `group/subgroup/repo` becomes `subgroup/repo`, a non-existent project). Fix `cleanRepoPath` to branch on provider: GitLab preserves the full namespace path; GitHub keeps the existing last-two-segment behavior. This is a standalone bug fix that must land before the GraphQL adapter relies on the full path.

**Blocked by:** None — can start immediately.

**Status:** ready-for-agent

- [ ] `cleanRepoPath` preserves the full namespace path for GitLab (e.g. `group/subgroup/repo` stays `group/subgroup/repo`)
- [ ] `cleanRepoPath` keeps existing behavior for GitHub (last two segments, e.g. `owner/repo`)
- [ ] `parseRepoNative` passes the provider through to `cleanRepoPath` so it can branch
- [ ] Tests cover: GitLab nested group (full path preserved), GitLab simple (2-segment unchanged), GitHub simple (last-two-segment unchanged)
- [ ] `go test ./internal/observe/trackerintake/...` passes
