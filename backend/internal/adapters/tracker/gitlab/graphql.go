package gitlab

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// graphqlRequest is the JSON body sent to POST /api/v4/graphql.
type graphqlRequest struct {
	Query         string         `json:"query"`
	Variables     map[string]any `json:"variables"`
	OperationName string         `json:"operationName,omitempty"`
}

// graphqlError represents one entry in a GraphQL response's top-level
// "errors" array. GitLab populates extensions.code for classification.
type graphqlError struct {
	Message    string         `json:"message"`
	Extensions map[string]any `json:"extensions"`
}

// graphqlResponse is the envelope returned by the GraphQL endpoint. The
// Data field is decoded into the specific query result type by each
// caller.
type graphqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []graphqlError  `json:"errors"`
}

// ---------------------------------------------------------------------------
// Work item query types
// ---------------------------------------------------------------------------

// wiQueryData is the decoded GraphQL result for a work-items query (both
// List and Get). The structure mirrors the query selection set:
//
//	data { project { workItems { nodes [...] pageInfo { endCursor hasNextPage } } } }
//	data { project { workItem { ... } } }
type wiQueryData struct {
	Project *struct {
		WorkItems *wiConnection `json:"workItems"`
		WorkItem  *workItemNode `json:"workItem"`
	} `json:"project"`
}

// wiConnection is the paginated work-items connection.
type wiConnection struct {
	Nodes    []workItemNode `json:"nodes"`
	PageInfo struct {
		EndCursor   string `json:"endCursor"`
		HasNextPage bool   `json:"hasNextPage"`
	} `json:"pageInfo"`
}

// workItemNode is the selection set from the GraphQL workItems query.
// Details (description, labels, assignees) are exposed as typed widgets.
type workItemNode struct {
	IID         string           `json:"iid"`
	Title       string           `json:"title"`
	Description *string          `json:"description"`
	State       string           `json:"state"`
	WebURL      string           `json:"webUrl"`
	Widgets     []workItemWidget `json:"widgets"`
}

// workItemWidget is a tagged-union: each widget has a Type field and the
// relevant payload. Only Assignees and Labels widgets are decoded; others
// are ignored.
type workItemWidget struct {
	Type string `json:"type"`

	// Assignees widget payload.
	Assignees *struct {
		Nodes []struct {
			Username string `json:"username"`
		} `json:"nodes"`
	} `json:"assignees,omitempty"`

	// Labels widget payload.
	Labels *struct {
		Nodes []struct {
			Title string `json:"title"`
		} `json:"nodes"`
	} `json:"labels,omitempty"`
}

// ---------------------------------------------------------------------------
// Query strings
// ---------------------------------------------------------------------------

// The fragment shared by both the List and Get queries. Widgets are fetched
// unconditionally — the response mapping handles missing widgets gracefully.
const workItemFragment = `
fragment WorkItemFields on WorkItem {
	iid
	title
	description
	state
	webUrl
	widgets {
		type
		... on WorkItemWidgetAssignees {
			assignees: assignees {
				nodes { username }
			}
		}
		... on WorkItemWidgetLabels {
			labels: labels {
				nodes { title }
			}
		}
	}
}`

// listWorkItemsQuery fetches work items via the project.workItems connection,
// supporting cursor-based pagination.
const listWorkItemsQuery = `query ListWorkItems($fullPath: ID!, $after: String, $first: Int, $state: WorkItemState, $assigneeUsernames: [String!], $labelName: [String!]) {
	project(fullPath: $fullPath) {
		workItems(after: $after, first: $first, state: $state, assigneeUsernames: $assigneeUsernames, labelName: $labelName) {
			nodes { ...WorkItemFields }
			pageInfo { endCursor hasNextPage }
		}
	}
}` + workItemFragment

// getWorkItemQuery fetches a single work item by project path + iid via the
// project.workItem lookup.
const getWorkItemQuery = `query GetWorkItem($fullPath: ID!, $iid: ID!) {
	project(fullPath: $fullPath) {
		workItem(iid: $iid) { ...WorkItemFields }
	}
}` + workItemFragment

// ---------------------------------------------------------------------------
// GraphQL transport
// ---------------------------------------------------------------------------

// graphqlDo sends a GraphQL query to the per-host base URL and returns the
// decoded response. HTTP-level errors are classified by status code (same as
// REST). GraphQL-level errors (HTTP 200 with errors array) are classified by
// extensions.code: "NOT_FOUND" → ErrNotFound.
func (t *Tracker) graphqlDo(ctx context.Context, he hostEntry, query string, vars map[string]any) (*graphqlResponse, error) {
	gqlReq := graphqlRequest{Query: query, Variables: vars}
	body, err := json.Marshal(gqlReq)
	if err != nil {
		return nil, fmt.Errorf("gitlab tracker: encode graphql request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, he.baseURL+"/api/v4/graphql", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("gitlab tracker: build graphql request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", t.userAgent)
	tok, err := he.tokens.Token(ctx)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)

	resp, err := t.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gitlab tracker: graphql request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, fmt.Errorf("gitlab tracker: read graphql response: %w", readErr)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, classifyError(resp, respBody)
	}

	var gqlResp graphqlResponse
	if err := json.Unmarshal(respBody, &gqlResp); err != nil {
		return nil, fmt.Errorf("gitlab tracker: decode graphql response: %w", err)
	}

	// GraphQL returns HTTP 200 with an errors array for query-level failures.
	if len(gqlResp.Errors) > 0 {
		return &gqlResp, classifyGraphQLError(gqlResp.Errors)
	}

	return &gqlResp, nil
}

// classifyGraphQLError inspects the first GraphQL error's extensions.code
// to map to an adapter sentinel error. "NOT_FOUND" → ErrNotFound; other
// codes fall through to a generic error wrapping the message.
func classifyGraphQLError(errs []graphqlError) error {
	if len(errs) == 0 {
		return nil
	}
	first := errs[0]
	code, _ := first.Extensions["code"].(string)
	if code == "NOT_FOUND" {
		return fmt.Errorf("%w: %s", ErrNotFound, first.Message)
	}
	return fmt.Errorf("gitlab tracker: graphql error: %s", first.Message)
}

// ---------------------------------------------------------------------------
// Work item → domain.Issue mapping
// ---------------------------------------------------------------------------

// issueFromWorkItem maps a GraphQL work item node to a domain.Issue.
// projectPath is passed in so the returned ID round-trips through the same
// adapter (Native = "path/to/project#iid"). The caller sets Host after
// this function returns.
func issueFromWorkItem(projectPath string, wi workItemNode) domain.Issue {
	var description string
	if wi.Description != nil {
		description = *wi.Description
	}

	labels := extractLabels(wi)
	assignees := extractAssignees(wi)

	out := domain.Issue{
		ID: domain.TrackerID{
			Provider: domain.TrackerProviderGitLab,
			Native:   fmt.Sprintf("%s#%s", projectPath, wi.IID),
		},
		Title:     wi.Title,
		Body:      description,
		State:     mapStateFromGitLab(wi.State),
		URL:       wi.WebURL,
		Labels:    labels,
		Assignees: assignees,
	}
	if len(out.Labels) == 0 {
		out.Labels = nil
	}
	if len(out.Assignees) == 0 {
		out.Assignees = nil
	}
	return out
}

// extractAssignees walks the work item's widgets looking for an Assignees
// widget. Returns nil if the widget is absent (already the domain zero
// value).
func extractAssignees(wi workItemNode) []string {
	for _, w := range wi.Widgets {
		if w.Type == "ASSIGNEES" && w.Assignees != nil {
			out := make([]string, 0, len(w.Assignees.Nodes))
			for _, n := range w.Assignees.Nodes {
				if n.Username != "" {
					out = append(out, n.Username)
				}
			}
			return out
		}
	}
	return nil
}

// extractLabels walks the work item's widgets looking for a Labels widget.
// Returns nil if the widget is absent.
func extractLabels(wi workItemNode) []string {
	for _, w := range wi.Widgets {
		if w.Type == "LABELS" && w.Labels != nil {
			out := make([]string, 0, len(w.Labels.Nodes))
			for _, n := range w.Labels.Nodes {
				if n.Title != "" {
					out = append(out, n.Title)
				}
			}
			return out
		}
	}
	return nil
}

// mapStateFilter translates the domain.ListStateFilter to GitLab's GraphQL
// WorkItemState enum value.
func mapStateFilter(s domain.ListStateFilter) string {
	switch s {
	case domain.ListOpen:
		return "OPENED"
	case domain.ListClosed:
		return "CLOSED"
	default:
		return "ALL"
	}
}

// classifyNullProject returns ErrNotFound when the GraphQL response's
// data.project is null — meaning the project doesn't exist or isn't visible
// to the token.
func classifyNullProject(data *wiQueryData) error {
	if data == nil || data.Project == nil {
		return fmt.Errorf("%w: project not found or not accessible", ErrNotFound)
	}
	return nil
}
