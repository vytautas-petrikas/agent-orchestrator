# 1. GitLab tracker adapter uses GraphQL for Work Items `List` and `Get`

Date: 2026-08-24
Status: Accepted

## Context

The GitLab tracker adapter (`backend/internal/adapters/tracker/gitlab/tracker.go`)
originally used the REST Issues API (`GET /projects/:id/issues`) for `List` and
`Get`. This only returns GitLab Issues — work items of type "issue". GitLab issue
boards and the broader Work Items surface (tasks, objectives, etc.) are invisible
to this endpoint.

Users report that GitLab intake "doesn't auto pick from tasks from Issue board/work
items in GitLab for given GitLab user ID, automatically like it does for GitHub
issues." The root cause is that the board items are Work Items, not Issues.

Two API options exist for Work Items:

1. **REST Work Items API** (`GET /projects/:id/-/work_items`) — behind a feature
   flag, and critically **lacks assignee filtering**. The endpoint accepts
   `state` and `labels` query parameters but has no `assignee_username` or
   equivalent. The adapter would have to fetch all open work items for the
   project and filter client-side by assignee — a wasteful and fragile approach
   that also risks exceeding the `Limit` budget before the right items are found.

2. **GraphQL Work Items API** (`project(fullPath:) { workItems(assigneeUsernames:) }`)
   — GA since GitLab 15.x, has full `assigneeUsernames` filter parity with GitLab's
   own board UI, supports cursor pagination, and returns exactly the fields needed
   in one query. This is the API GitLab's frontend uses for the board view.

Every other tracker adapter (GitHub) uses REST. Introducing GraphQL in one adapter
diverges from the established pattern.

This decision meets all three criteria for an ADR:

- **Hard to reverse.** Once the adapter is on GraphQL — with cursor pagination,
  widget-based field mapping, and GraphQL error-envelope classification — reverting
  to REST is not a one-line config flip. It requires rewriting `List` and `Get`
  from scratch (different request shape, different response parsing, different
  pagination model, different error classification). Any downstream code or tests
  that have come to depend on the GraphQL response shape would also break.

- **Surprising without context.** A contributor opening the GitLab tracker
  adapter will see `POST /api/v4/graphql` where the GitHub adapter uses REST
  endpoints. Without this ADR, that looks like an accidental inconsistency or a
  mistake to "fix" by reverting to REST — which would silently break Work Items
  intake again.

- **Result of a real trade-off.** The REST Work Items API was the natural choice
  (consistent with the GitHub adapter), but it lacks server-side assignee
  filtering. GraphQL was chosen specifically because it is the only GitLab API
  that returns all work item types *and* supports assignee filtering
  server-side. This is not a preference; it is a functional requirement.

## Decision

Use **GraphQL** for the GitLab tracker adapter's `List` and `Get` methods. Keep
`Preflight` on REST `GET /user` (token validation doesn't need Work Items).

This replaces the REST Issues API calls entirely. Work Items is the superset —
Issues are work items of type "issue" — so there is no need to query both.

### Transport summary

| Method     | Transport | Endpoint                      | Purpose                          |
|------------|-----------|-------------------------------|----------------------------------|
| `Preflight`| REST      | `GET /user`                   | Token validation (unchanged)    |
| `List`     | GraphQL   | `POST /api/v4/graphql`        | Fetch filtered work items        |
| `Get`      | GraphQL   | `POST /api/v4/graphql`        | Fetch a single work item by IID  |

### GraphQL query shape

The query targets `project(fullPath:)` and uses the `workItems` connection with
`state`, `assigneeUsernames`, and `labelName` filters. Pagination is cursor-based
(`first`/`after` with `pageInfo.endCursor`/`hasNextPage`), replacing the REST
Link-header model. Work item details (description, labels, assignees) are exposed
as typed widgets in the response: `WorkItemWidgetAssignees` and
`WorkItemWidgetLabels`. Top-level `iid`, `title`, `state`, and `webUrl` map
directly to `domain.Issue` fields.

### Error classification

GraphQL returns HTTP 200 with a top-level `errors` array for query-level failures.
The adapter inspects `errors[0].extensions.code` to classify: `"NOT_FOUND"` →
`ErrNotFound`. HTTP-level errors are classified by status code as before:
401/403 → `ErrAuthFailed`, 429 → `ErrRateLimited`. HTTP 200 with
`data.project == null` → `ErrNotFound` (project not visible or doesn't exist).

## Consequences

- **Mixed transports.** The GitLab adapter now has two HTTP transports: REST for
  `Preflight`, GraphQL for `List`/`Get`. This is a deliberate, documented
  divergence from the GitHub adapter's all-REST pattern. Contributors should not
  "normalize" the adapter by reverting `List` or `Get` to REST — doing so would
  break Work Items intake (REST Issues API only returns issues, not all work item
  types; REST Work Items API lacks assignee filtering).

- **GraphQL error handling differs from REST.** The adapter's `classifyError`
  must handle GraphQL error envelopes (top-level `errors` array with
  `extensions.code`), which have a different shape than REST's JSON error
  responses. The existing sentinel errors (`ErrNotFound`, `ErrAuthFailed`,
  `ErrRateLimited`) are preserved and continue to be the contract with the
  service layer.

- **Cursor pagination replaces Link-header.** The adapter implements cursor
  following (`after`/`first` with `pageInfo.endCursor`/`hasNextPage`) instead of
  parsing REST `Link` headers. The `Limit` filter caps total results as before.

- **Schema dependency.** If GitLab deprecates or changes the Work Items GraphQL
  schema, the adapter breaks. This is the same risk as any API dependency, but
  GraphQL schemas tend to be more stable than REST endpoints in GitLab's
  experience.

- **Minimum version.** Self-managed GitLab instances older than 15.x do not have
  Work Items GraphQL support. The adapter targets GitLab 16+; older instances will
  get a clear query failure rather than silently returning zero results.

- **Canonical ID format unchanged.** Work item IIDs are project-scoped and unique
  across all types within a project, so `gitlab:group/project#iid` remains a valid,
  collision-free identifier. Existing sessions, deduplication, and issue-context
  lookups all keep working without change.

- **No type filtering in v1.** The GraphQL query does not pass a `types` filter —
  all open work items for the assignee are returned. If epics or incidents cause
  problems in practice, type filtering can be added later without breaking the
  query shape.
