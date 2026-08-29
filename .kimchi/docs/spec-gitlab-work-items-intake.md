## Problem Statement

As a project maintainer using GitLab for issue tracking, I configure tracker intake to automatically pick up issues assigned to me — just like it works for GitHub. But AO never spawns workers for my GitLab board items. The intake loop polls the GitLab Issues API (`GET /projects/:id/issues`), which only returns work items of type "issue." My board contains Work Items of various types (tasks, objectives, etc.) at URLs like `/-/work_items`, and those are invisible to the Issues endpoint. So intake silently returns zero results, no workers spawn, and there is no error or log explaining why.

## Solution

Switch the GitLab tracker adapter from the REST Issues API to the GraphQL Work Items API. Work Items is the superset — Issues are work items of type "issue" — so the adapter sees all open work items on the board, regardless of type. The GraphQL API supports `assigneeUsernames` filtering (which the REST Work Items endpoint lacks), so the assignee-based intake filter continues to work server-side. From the user's perspective: GitLab intake now picks up all assigned work items (tasks, issues, etc.) the same way GitHub intake picks up assigned issues.

## User Stories

1. As a project maintainer, I want GitLab tracker intake to pick up work items (not just issues) from my project board, so that all assigned items get workers automatically.
2. As a project maintainer, I want work items of type "task" to spawn workers just like issues do, so that I don't have to convert tasks to issues for AO to pick them up.
3. As a project maintainer, I want the assignee filter to work against GitLab usernames, so that only work items assigned to me are picked up.
4. As a project maintainer, I want existing spawned workers for GitLab issues to keep working after this change, so that the IID-based issue ID format (`gitlab:group/project#iid`) remains stable.
5. As a project maintainer with a nested-group GitLab project (e.g. `group/subgroup/repo`), I want intake to derive the correct full project path from my git origin URL, so that the adapter queries the right project.
6. As a project maintainer using a self-managed GitLab instance, I want intake to route GraphQL queries to my instance's host, so that self-managed deployments work the same as gitlab.com.
7. As a project maintainer, I want the adapter to handle pagination across multiple pages of work items, so that large boards with more than 100 items are fully swept.
8. As a project maintainer, I want the adapter to respect the `Limit` filter, so that intake sweeps don't pull unbounded data.
9. As a project maintainer, I want the adapter to respect the state filter (open/closed/all), so that only open work items are picked up by default intake.
10. As a project maintainer, I want the adapter to respect the labels filter, so that label-based intake narrowing works on GitLab.
11. As a project maintainer, I want token validation (Preflight) to keep working against GitLab, so that the daemon reports auth failures at startup rather than silently failing intake.
12. As a project maintainer, I want authentication failures to surface as clear errors, so that misconfigured tokens are obvious.
13. As a project maintainer, I want rate-limit responses to be classified correctly, so that the observer's backoff behavior kicks in.
14. As a project maintainer, I want the spawned worker to receive the work item's title, body, labels, assignees, and URL as context, so that the agent has enough information to implement the task.
15. As a project maintainer, I want work items without a description to be handled gracefully, so that tasks with only a title still spawn workers.
16. As a project maintainer, I want work items without labels to be handled gracefully, so that unlabeled tasks still spawn workers.
17. As a project maintainer, I want work items without assignees to be handled gracefully, so that the adapter doesn't crash on edge-case items.
18. As a project maintainer, I want the GitHub tracker adapter to remain unchanged, so that existing GitHub intake has no regression risk.
19. As a project maintainer, I want the tracker port interface (`List`, `Get`, `Preflight`) to remain unchanged, so that the service layer and observer don't need modification.
20. As a project maintainer, I want the canonical issue ID format to remain `gitlab:group/project#iid`, so that existing sessions, deduplication, and issue-context lookups keep working.
21. As a project maintainer, I want the observer's `issueMatchesConfig` assignee check to keep working, so that client-side assignee validation matches server-side filtering.
22. As a project maintainer, I want the fix for nested-group repo path derivation to be included, so that projects under `group/subgroup/repo` don't get a truncated (non-existent) project path.
23. As a future contributor, I want an ADR explaining why GitLab uses GraphQL when all other tracker integrations use REST, so that I don't "fix" the divergence by reverting to REST.

## Implementation Decisions

- **GitLab tracker adapter `List` and `Get` switch from REST Issues API to GraphQL Work Items API.** The REST Issues endpoint only returns work items of type "issue"; the GraphQL Work Items query returns all types. The query targets `POST /api/v4/graphql` against the same per-host base URL the adapter already resolves via `configForHost`. Work Items is the superset — Issues are work items of type "issue" — so the Issues API is dropped entirely; there is no need to query both.

- **GraphQL query uses `project(fullPath:)` scope with `workItems` connection.** The query filters by `state`, `assigneeUsernames`, and `labelName`. Pagination is cursor-based (`first`/`after` with `pageInfo.endCursor`/`hasNextPage`), replacing the REST Link-header pagination. The `Limit` filter caps total results, same as before.

- **Field mapping from GraphQL widgets to `domain.Issue`.** Work item details (description, labels, assignees) are exposed as typed widgets in the GraphQL response: `WorkItemWidgetAssignees` (assignees→`assignees.nodes[].username`), `WorkItemWidgetLabels` (labels→`labels.nodes[].title`), and the top-level `description` field maps to `Issue.Body`. Top-level `iid`, `title`, `state`, and `webUrl` map directly to `Issue.ID.Native`, `Issue.Title`, `Issue.State`, and `Issue.URL`. Missing widgets produce nil slices (already the domain zero value).

- **State mapping: GraphQL `opened`→`IssueOpen`, `closed`→`IssueDone`.** Same mapping as the REST adapter. The normalized state vocabulary is unchanged.

- **Error classification for GraphQL responses.** GraphQL returns HTTP 200 with an `errors` array for query-level failures. The adapter inspects `errors[0].extensions.code` to classify: `"NOT_FOUND"` → `ErrNotFound`. HTTP-level errors are classified by status code as before: 401/403 → `ErrAuthFailed`, 429 → `ErrRateLimited`. HTTP 200 with `data.project == null` → `ErrNotFound` (project not visible or doesn't exist). All existing sentinel errors are preserved.

- **`Preflight` stays on REST `GET /user`.** It validates token validity, not Work Items access. REST `/user` is simpler and sufficient. The GraphQL transport is only for `List` and `Get`.

- **Host routing is unchanged.** The `configForHost` method returns a `hostEntry` with `baseURL` and `tokens`. GraphQL `POST /api/v4/graphql` goes to the same `baseURL` as REST. Self-managed hosts continue to require `AllowedHosts` and use per-host tokens.

- **`cleanRepoPath` fix for GitLab nested groups.** The observer's `parseRepoNative` → `cleanRepoPath` currently truncates to the last two path segments (a GitHub assumption). For GitLab, the full namespace path (`group/subgroup/repo`) must be preserved because GitLab's `fullPath` parameter requires it. The fix branches on provider: GitHub keeps last-2-segment behavior; GitLab preserves the full path.

- **No work item type filtering in v1.** The GraphQL query does not pass a `types` filter — all open work items for the assignee are returned. If epics or incidents cause problems in practice, type filtering can be added later without breaking the query shape.

- **No type field added to `domain.Issue`.** Work items are handled identically to issues — same prompt, same worker behavior. The work item type is fetched in the GraphQL query (for future use) but not surfaced to the domain or the agent.

- **Canonical issue ID format unchanged.** Work item IIDs are project-scoped and unique across types within a project, so `gitlab:group/project#iid` remains a valid, collision-free identifier. Existing sessions, deduplication (`seenIssueIDs`), and issue-context lookups (`trackerIDForIssue`) all keep working without change.

- **GitHub adapter is untouched.** No changes to the GitHub tracker, the `ports.Tracker` interface, `domain.Issue`, `domain.TrackerID`, `domain.TrackerRepo`, or `domain.ListFilter`.

- **ADR documenting the GraphQL decision.** An ADR under `.kimchi/docs/` explains why GitLab diverges to GraphQL when every other tracker integration uses REST. It covers the three criteria: hard to reverse, surprising without context, and the result of a real trade-off (REST Work Items API lacks assignee filtering).

## Testing Decisions

**What makes a good test:** Tests assert external behavior at the tracker port boundary (`ports.Tracker`), not implementation details. A test should be able to swap the underlying transport (REST → GraphQL) without changing test assertions beyond the request shape the fake server expects. Tests use `httptest.Server` fakes that record inbound requests and return canned responses — the same pattern already established in the GitLab and GitHub tracker test suites.

**Modules tested:**

1. **GitLab tracker adapter** (`backend/internal/adapters/tracker/gitlab/`) — all `List` and `Get` tests are rewritten to assert against GraphQL requests. The fake test server (`fakeGL`) is updated to match on `POST /api/v4/graphql` instead of `GET /projects/:path/issues`. Tests verify:
   - Happy path: single-page query, fields mapped correctly from GraphQL widgets.
   - Multi-page pagination via `endCursor`/`hasNextPage`.
   - `Limit` stops early.
   - Nested group paths preserved (full namespace in `fullPath` variable).
   - Assignee filter sent as `assigneeUsernames`.
   - State filter mapped (open→opened, closed→closed, all→all).
   - Labels filter sent as `labelName`.
   - Provider/repo validation (unchanged — pre-network).
   - Error classification: not-found (GraphQL errors array + null project), auth failed (401/403), rate limited (429).
   - `Get` happy path, not-found, self-managed host routing, wrong provider.
   - `Preflight` unchanged (REST `/user`).
   - Edge cases: work item with no labels, no assignees, no description.

2. **Tracker intake observer** (`backend/internal/observe/trackerintake/`) — new tests for `parseRepoNative`/`cleanRepoPath` with GitLab nested groups. Existing observer tests that use a fake tracker are unaffected (they don't hit the real adapter).

**Prior art:** The existing `tracker_test.go` files in both `github/` and `gitlab/` packages use the same `httptest.Server` + recorded request pattern. The observer test file (`observer_test.go`) uses a `fakeTracker` that implements `ports.Tracker` directly — no HTTP.

**Test seams:** The single seam is `ports.Tracker` — the adapter's `List`, `Get`, and `Preflight` methods. The observer tests use a fake tracker and are unaffected by the transport change. No new test seams are introduced.

## Out of Scope

- Work item type filtering (e.g., only pick up tasks, not epics) — can be added later without breaking the query shape.
- Surfacing work item type to the agent (e.g., a "type" field in the issue context) — not needed since all types are handled identically.
- Runtime fallback to the Issues API for old GitLab versions (<15.x) — the adapter targets GitLab 16+; older instances will get a clear query failure.
- Changes to the `ports.Tracker` interface, `domain.Issue`, or any other domain type.
- Changes to the GitHub tracker adapter.
- Changes to the observer's polling logic, backoff, or spawn behavior.
- Changes to the session manager's issue-context enrichment (`withIssueContext`).
- The REST Work Items API (`/projects/:id/-/work_items`) — considered and rejected due to missing assignee filtering.

## Further Notes

- The GraphQL query shape is based on GitLab's own frontend query (`get_work_items_full.query.graphql`), confirmed via the Fossies mirror and GitLab's architecture design doc for the Work Item REST API.
- Work item IIDs share a single per-project sequence across all types — an issue with iid=5 and a task with iid=5 cannot coexist. This means the canonical ID format is safe without modification.
- The `cleanRepoPath` truncation bug was pre-existing (not introduced by this change) but directly affects GitLab intake for nested groups. Fixing it here avoids shipping a feature that only works for 2-segment project paths.
- The ADR lives under `.kimchi/docs/` per user direction, not the repo's `docs/adr/` directory.
