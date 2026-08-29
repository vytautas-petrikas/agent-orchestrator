# 02 — Switch GitLab tracker adapter List to GraphQL Work Items

**What to build:** Replace the GitLab tracker adapter's REST Issues API `List` with a GraphQL Work Items query. The query targets `POST /api/v4/graphql` against the per-host base URL, scoped to `project(fullPath:)` with a `workItems` connection filtered by `state`, `assigneeUsernames`, and `labelName`. Pagination is cursor-based (`first`/`after` with `pageInfo.endCursor`/`hasNextPage`). Results are mapped from GraphQL widgets (assignees from `WorkItemWidgetAssignees`, labels from `WorkItemWidgetLabels`, description from the top-level field) to `domain.Issue`. All `ListFilter` dimensions work (state, assignee, labels, limit). GraphQL error responses are classified: HTTP 200 with `errors` array → inspect `extensions.code` for `NOT_FOUND` → `ErrNotFound`; HTTP 200 with `data.project == null` → `ErrNotFound`; 401/403 → `ErrAuthFailed`; 429 → `ErrRateLimited`. The REST Issues API `List` path is removed. `Preflight` stays on REST `GET /user` — unchanged.

**Blocked by:** 01 — Fix GitLab nested-group repo path derivation in intake observer (the adapter needs correct full project paths to query `fullPath`)

**Status:** ready-for-agent

- [ ] `List` sends a GraphQL query to `POST /api/v4/graphql` against the correct per-host base URL
- [ ] Query filters by `state` (open→opened, closed→closed, all→all), `assigneeUsernames`, and `labelName`
- [ ] Cursor pagination works across multiple pages via `endCursor`/`hasNextPage`
- [ ] `Limit` caps total results, stopping early
- [ ] Fields mapped correctly: iid→Native, title→Title, description→Body, state→State, webUrl→URL, assignees from widget, labels from widget
- [ ] Work items with no labels, no assignees, or no description are handled gracefully (nil slices)
- [ ] Nested group paths work (full namespace in `fullPath` variable)
- [ ] Error classification: not-found (GraphQL errors array + null project), auth failed (401/403), rate limited (429)
- [ ] All existing sentinel errors preserved (`ErrNotFound`, `ErrAuthFailed`, `ErrRateLimited`, `ErrWrongProvider`, `ErrBadID`, `ErrHostNotAllowed`)
- [ ] `Preflight` unchanged (REST `/user`)
- [ ] Host routing unchanged — self-managed hosts use per-host base URL and tokens
- [ ] `go test ./internal/adapters/tracker/gitlab/...` passes
