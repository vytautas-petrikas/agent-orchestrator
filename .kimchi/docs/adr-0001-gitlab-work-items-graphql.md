# ADR 1: GitLab tracker adapter uses GraphQL for Work Items

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
   flag, and critically **lacks assignee filtering**. The adapter would have to
   fetch all open work items and filter client-side, which is wasteful and fragile.

2. **GraphQL Work Items API** (`project(fullPath:) { workItems(assigneeUsernames:) }`)
   — GA since GitLab 15.x, has full assignee filter parity with GitLab's own board
   UI, supports cursor pagination, and returns exactly the fields needed in one
   query. This is the API GitLab's frontend uses for the board view.

Every other tracker adapter (GitHub) uses REST. Introducing GraphQL in one adapter
diverges from the established pattern.

## Decision

Use **GraphQL** for the GitLab tracker adapter's `List` and `Get` methods. Keep
`Preflight` on REST `GET /user` (token validation doesn't need Work Items).

This replaces the REST Issues API calls entirely. Work Items is the superset —
Issues are work items of type "issue" — so there is no need to query both.

## Consequences

- The GitLab adapter now has two HTTP transports: REST for `Preflight`, GraphQL
  for `List`/`Get`. This is a deliberate, documented divergence from the
  GitHub adapter's all-REST pattern.
- A future contributor seeing GraphQL in one adapter may be tempted to "fix" it
  by reverting to REST. This ADR exists to prevent that.
- GraphQL error responses have a different shape than REST errors. The adapter's
  `classifyError` must handle GraphQL error envelopes (top-level `errors` array
  with `extensions.code`).
- Cursor-based pagination replaces Link-header pagination. The adapter
  implements cursor following (`after`/`first`) instead of parsing `Link` headers.
- If GitLab deprecates or changes the Work Items GraphQL schema, the adapter
  breaks. This is the same risk as any API dependency, but GraphQL schemas tend
  to be more stable than REST endpoints in GitLab's experience.
- Self-managed GitLab instances older than 15.x do not have Work Items GraphQL
  support. The adapter targets GitLab 16+; older instances will get a clear
  query failure.
