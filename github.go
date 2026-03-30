package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ghAPI calls `gh api` and returns the raw JSON output.
func ghAPI(method, endpoint string, extraArgs ...string) ([]byte, error) {
	args := []string{"api",
		"-H", "Accept: application/vnd.github+json",
		"-H", "X-GitHub-Api-Version: 2022-11-28",
	}
	if method != "" && method != "GET" {
		args = append(args, "-X", method)
	}
	args = append(args, extraArgs...)
	args = append(args, endpoint)

	cmd := exec.Command("gh", args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("gh api %s: %s", endpoint, string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("gh api %s: %w", endpoint, err)
	}
	return out, nil
}

func ghGraphQL(query string, vars ...string) ([]byte, error) {
	args := []string{"api", "graphql", "-f", "query=" + query}
	args = append(args, vars...)

	cmd := exec.Command("gh", args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("gh graphql: %s", string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("gh graphql: %w", err)
	}
	return out, nil
}

const prQuery = `
query($owner: String!, $name: String!) {
  repository(owner: $owner, name: $name) {
    pullRequests(first: 30, states: OPEN, orderBy: {field: UPDATED_AT, direction: DESC}) {
      nodes {
        number
        title
        createdAt
        updatedAt
        additions
        deletions
        headRefOid
        authorAssociation
        isDraft
        reviewDecision
        mergeable

        author {
          login
        }

        labels(first: 20) {
          nodes { name }
        }

        assignees(first: 10) {
          nodes { login }
        }

        reviewRequests(first: 20) {
          nodes {
            requestedReviewer {
              ... on User { login }
            }
          }
        }

        reviews(first: 10) {
          nodes {
            author { login }
            state
            submittedAt
          }
        }

        commits(last: 1) {
          nodes {
            commit {
              statusCheckRollup {
                state
              }
            }
          }
        }
      }
    }
  }
}
`

// GraphQL response types

type gqlResponse struct {
	Data struct {
		Repository struct {
			PullRequests struct {
				Nodes []gqlPR `json:"nodes"`
			} `json:"pullRequests"`
		} `json:"repository"`
	} `json:"data"`
}

type gqlPR struct {
	Number            int       `json:"number"`
	Title             string    `json:"title"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
	Additions         int       `json:"additions"`
	Deletions         int       `json:"deletions"`
	HeadRefOid        string    `json:"headRefOid"`
	AuthorAssociation string    `json:"authorAssociation"`
	IsDraft           bool      `json:"isDraft"`
	ReviewDecision    string    `json:"reviewDecision"`    // APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED, or ""
	Mergeable         string    `json:"mergeable"`         // MERGEABLE, CONFLICTING, UNKNOWN

	Author *struct {
		Login string `json:"login"`
	} `json:"author"`

	Labels struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"labels"`

	Assignees struct {
		Nodes []struct {
			Login string `json:"login"`
		} `json:"nodes"`
	} `json:"assignees"`

	ReviewRequests struct {
		Nodes []struct {
			RequestedReviewer *struct {
				Login string `json:"login"`
			} `json:"requestedReviewer"`
		} `json:"nodes"`
	} `json:"reviewRequests"`

	Reviews struct {
		Nodes []gqlReview `json:"nodes"`
	} `json:"reviews"`

	Commits struct {
		Nodes []struct {
			Commit struct {
				StatusCheckRollup *struct {
					State string `json:"state"`
				} `json:"statusCheckRollup"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"commits"`
}

type gqlReview struct {
	Author *struct {
		Login string `json:"login"`
	} `json:"author"`
	State       string    `json:"state"`
	SubmittedAt time.Time `json:"submittedAt"`
}

// FetchRepoPRs fetches all open PRs for a repo using a single GraphQL query,
// then upserts them into the database.
func FetchRepoPRs(db *DB, repo, user string) error {
	owner, repoName, err := splitRepo(repo)
	if err != nil {
		return err
	}

	out, err := ghGraphQL(prQuery,
		"-f", "owner="+owner,
		"-f", "name="+repoName,
	)
	if err != nil {
		return fmt.Errorf("fetching PRs for %s: %w", repo, err)
	}

	var resp gqlResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return fmt.Errorf("parsing PRs for %s: %w", repo, err)
	}

	now := time.Now()
	gqlPRs := resp.Data.Repository.PullRequests.Nodes
	openNumbers := make([]int, 0, len(gqlPRs))

	// Fetch SHAs with workflows awaiting approval. This is one REST call
	// per repo. We always make this call because the GraphQL response may
	// not include all PRs (limited to 100), and the ones needing approval
	// are often from first-time contributors further down the list.
	needsApproval := fetchSHAsNeedingApproval(owner, repoName)

	for _, gpr := range gqlPRs {
		openNumbers = append(openNumbers, gpr.Number)

		authorLogin := ""
		if gpr.Author != nil {
			authorLogin = gpr.Author.Login
		}

		labels := make([]string, len(gpr.Labels.Nodes))
		for i, l := range gpr.Labels.Nodes {
			labels[i] = l.Name
		}

		isReviewer := false
		for _, rr := range gpr.ReviewRequests.Nodes {
			if rr.RequestedReviewer != nil && strings.EqualFold(rr.RequestedReviewer.Login, user) {
				isReviewer = true
				break
			}
		}

		isAssignee := false
		for _, a := range gpr.Assignees.Nodes {
			if strings.EqualFold(a.Login, user) {
				isAssignee = true
				break
			}
		}

		ciStatus := parseCIStatus(gpr, needsApproval)

		pr := PR{
			Repo:              repo,
			Number:            gpr.Number,
			Title:             gpr.Title,
			Author:            authorLogin,
			AuthorAssociation: gpr.AuthorAssociation,
			Labels:            labels,
			CreatedAt:         gpr.CreatedAt,
			UpdatedAt:         gpr.UpdatedAt,
			Additions:         gpr.Additions,
			Deletions:         gpr.Deletions,
			HeadSHA:           gpr.HeadRefOid,
			CIStatus:          ciStatus,
			IsReviewer:        isReviewer,
			IsAssignee:        isAssignee,
			IsAuthor:          strings.EqualFold(authorLogin, user),
			IsDraft:           gpr.IsDraft,
			ReviewDecision:    gpr.ReviewDecision,
			Mergeable:         gpr.Mergeable,
			FetchedAt:         now,
		}
		if err := db.UpsertPR(pr); err != nil {
			return fmt.Errorf("upserting PR %s#%d: %w", repo, gpr.Number, err)
		}

		// Store the user's latest review timestamp
		for _, r := range gpr.Reviews.Nodes {
			if r.Author != nil && strings.EqualFold(r.Author.Login, user) {
				if err := db.UpsertUserReview(repo, gpr.Number, r.SubmittedAt, r.State); err != nil {
					return fmt.Errorf("storing review for %s#%d: %w", repo, gpr.Number, err)
				}
			}
		}
	}

	if err := db.DeleteClosedPRs(repo, openNumbers); err != nil {
		return fmt.Errorf("cleaning closed PRs for %s: %w", repo, err)
	}

	// Update CI status for cached PRs whose SHAs need approval but weren't
	// in the GraphQL response (e.g. older PRs beyond the first 100).
	if err := db.MarkNeedsApproval(repo, needsApproval); err != nil {
		return fmt.Errorf("marking needs-approval for %s: %w", repo, err)
	}

	return nil
}

func parseCIStatus(gpr gqlPR, needsApproval map[string]bool) CIStatus {
	// Workflow runs awaiting approval don't appear as check runs, so the
	// rollup can show SUCCESS even when CI hasn't actually run. Check the
	// REST-derived set first.
	if needsApproval[gpr.HeadRefOid] {
		return CIStatusNeedsApproval
	}

	if len(gpr.Commits.Nodes) == 0 {
		return CIStatusUnknown
	}
	rollup := gpr.Commits.Nodes[0].Commit.StatusCheckRollup
	if rollup == nil {
		return CIStatusUnknown
	}

	switch rollup.State {
	case "SUCCESS":
		return CIStatusPass
	case "FAILURE", "ERROR":
		return CIStatusFail
	case "PENDING", "EXPECTED":
		return CIStatusPending
	default:
		return CIStatusUnknown
	}
}

// fetchSHAsNeedingApproval returns a set of head SHAs that have workflow runs
// waiting for approval. This uses the REST API because the GraphQL
// statusCheckRollup doesn't surface unapproved workflow runs.
func fetchSHAsNeedingApproval(owner, repoName string) map[string]bool {
	out, err := ghAPI("GET", fmt.Sprintf("/repos/%s/%s/actions/runs?status=action_required", owner, repoName))
	if err != nil {
		return nil
	}
	var resp struct {
		WorkflowRuns []struct {
			HeadSHA string `json:"head_sha"`
		} `json:"workflow_runs"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil
	}
	result := make(map[string]bool, len(resp.WorkflowRuns))
	for _, run := range resp.WorkflowRuns {
		result[run.HeadSHA] = true
	}
	return result
}

// ApproveWorkflowRuns approves pending workflow runs for a specific PR
// (matched by head SHA).
func ApproveWorkflowRuns(repo string, prHeadSHA string) error {
	owner, repoName, err := splitRepo(repo)
	if err != nil {
		return err
	}

	out, err := ghAPI("GET", fmt.Sprintf("/repos/%s/%s/actions/runs?status=action_required", owner, repoName))
	if err != nil {
		return err
	}
	var resp struct {
		WorkflowRuns []struct {
			ID      int    `json:"id"`
			HeadSHA string `json:"head_sha"`
		} `json:"workflow_runs"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return err
	}

	approved := 0
	for _, run := range resp.WorkflowRuns {
		if run.HeadSHA != prHeadSHA {
			continue
		}
		_, err := ghAPI("POST", fmt.Sprintf("/repos/%s/%s/actions/runs/%d/approve", owner, repoName, run.ID))
		if err != nil {
			return fmt.Errorf("approving run %d: %w", run.ID, err)
		}
		approved++
	}
	if approved == 0 {
		return fmt.Errorf("no pending workflow runs found for this PR")
	}
	return nil
}

// AssignReviewer adds the user as a requested reviewer on the PR.
func AssignReviewer(repo string, number int, user string) error {
	owner, repoName, err := splitRepo(repo)
	if err != nil {
		return err
	}
	_, err = ghAPI("POST",
		fmt.Sprintf("/repos/%s/%s/pulls/%d/requested_reviewers", owner, repoName, number),
		"-f", fmt.Sprintf("reviewers[]=%s", user),
	)
	return err
}

// OpenInBrowser opens the PR in the default browser.
func OpenInBrowser(repo string, number int) error {
	url := fmt.Sprintf("https://github.com/%s/pull/%d", repo, number)
	return exec.Command("open", url).Start()
}

func splitRepo(repo string) (owner, name string, err error) {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid repo format %q, expected owner/name", repo)
	}
	return parts[0], parts[1], nil
}
