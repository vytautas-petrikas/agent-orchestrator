# 04 — ADR for GitLab GraphQL decision

**What to build:** An ADR under `.kimchi/docs/` documenting why the GitLab tracker adapter uses GraphQL for `List` and `Get` when all other tracker integrations use REST. Covers the three criteria for an ADR: hard to reverse (once on GraphQL, going back is a rewrite), surprising without context (why GraphQL when everything else is REST?), and the result of a real trade-off (REST Work Items API lacks assignee filtering; GraphQL has full `assigneeUsernames` filter parity with GitLab's board UI). The ADR follows the existing ADR format (Context, Decision, Consequences) used in `docs/adr/`.

**Blocked by:** None — can start immediately.

**Status:** ready-for-agent

- [ ] ADR documents the context (REST Issues API only returns issues, not work items; REST Work Items API lacks assignee filtering)
- [ ] ADR documents the decision (use GraphQL Work Items API for `List` and `Get`; keep `Preflight` on REST)
- [ ] ADR documents consequences (GitLab adapter has mixed transports; future contributors should not revert to REST; GraphQL error handling differs from REST; cursor pagination replaces Link-header)
- [ ] ADR follows the existing ADR format (Context, Decision, Consequences)
