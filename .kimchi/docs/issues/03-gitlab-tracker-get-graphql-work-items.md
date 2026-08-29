# 03 — Switch GitLab tracker adapter Get to GraphQL Work Items

**What to build:** Replace the GitLab tracker adapter's REST Issues API `Get` with a GraphQL Work Items query. The query fetches a single work item by project path + iid via the same GraphQL transport and widget-mapping code introduced in the `List` change. The response is mapped to `domain.Issue` with identical field mapping. Not-found returns `ErrNotFound` (both via GraphQL errors array and null project/data). Host routing for self-managed instances works. The REST Issues API `Get` path is removed, completing the migration from REST Issues to GraphQL Work Items.

**Blocked by:** 02 — Switch GitLab tracker adapter List to GraphQL Work Items (shares the same GraphQL transport and response-mapping code)

**Status:** ready-for-agent

- [ ] `Get` sends a GraphQL query fetching a single work item by project path + iid
- [ ] Fields mapped correctly: iid→Native, title→Title, description→Body, state→State, webUrl→URL, assignees from widget, labels from widget
- [ ] Work items with missing widgets handled gracefully (nil slices)
- [ ] Not-found returns `ErrNotFound` (GraphQL errors array with `NOT_FOUND` code, and null data)
- [ ] Self-managed host routing works (query goes to per-host base URL)
- [ ] Wrong provider rejected before any network call
- [ ] `go test ./internal/adapters/tracker/gitlab/...` passes
