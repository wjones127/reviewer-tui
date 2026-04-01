package main

import (
	"fmt"
	"math"
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type tabModel struct {
	table         table.Model
	prs           []PR
	triageMap     map[string]TriageResult
	userReviewMap map[string]UserReview
	tabType       int
	showDismissed bool
}

func newTabModel(tabType int) tabModel {
	cols := commonColumns()
	switch tabType {
	case tabReview:
		cols = append(cols, table.Column{Title: "Last action", Width: 20})
	}

	t := table.New(
		table.WithColumns(cols),
		table.WithRows([]table.Row{}),
		table.WithFocused(true),
		table.WithHeight(10),
	)
	t.SetStyles(tableStyles())

	return tabModel{
		table:   t,
		tabType: tabType,
	}
}

func commonColumns() []table.Column {
	return []table.Column{
		{Title: "Repo", Width: 20},
		{Title: "PR#", Width: 6},
		{Title: "Title", Width: 40},
		{Title: "Author", Width: 15},
		{Title: "CI", Width: 6},
		{Title: "Review", Width: 8},
		{Title: "Merge", Width: 5},
		{Title: "+/-", Width: 10},
		{Title: "Age", Width: 8},
		{Title: "Est.", Width: 5},
	}
}

func (t *tabModel) setSize(width, height int) {
	t.table.SetWidth(width)
	t.table.SetHeight(height)
}

func (t *tabModel) setPRs(prs []PR, tags []string, updatesMap map[string]bool, triageMap map[string]TriageResult, userReviewMap map[string]UserReview) {
	t.prs = prs
	t.triageMap = triageMap
	t.userReviewMap = userReviewMap
	SortPRsByScore(prs, tags, updatesMap)

	rows := make([]table.Row, len(prs))
	for i, pr := range prs {
		k := fmt.Sprintf("%s#%d", pr.Repo, pr.Number)

		effortStr := "..."
		if tr, ok := triageMap[k]; ok && tr.HeadSHA == pr.HeadSHA {
			effortStr = tr.Effort
		}

		row := table.Row{
			shortRepo(pr.Repo),
			fmt.Sprintf("#%d", pr.Number),
			truncate(pr.Title, 38),
			authorLabel(pr),
			ciStatusLabel(pr.CIStatus),
			reviewDecisionLabel(pr.ReviewDecision),
			mergeableLabel(pr.Mergeable),
			fmt.Sprintf("+%d/-%d", pr.Additions, pr.Deletions),
			formatAge(pr.CreatedAt),
			effortStr,
		}
		if t.tabType == tabReview {
			ur := userReviewMap[k]
			label, actionNeeded, since := lastActionLabel(pr, ur)
			row = append(row, lastActionStyle(label, actionNeeded, since))
		}
		rows[i] = row
	}
	t.table.SetRows(rows)
}

func (t tabModel) selectedPR() (PR, bool) {
	idx := t.table.Cursor()
	if idx < 0 || idx >= len(t.prs) {
		return PR{}, false
	}
	return t.prs[idx], true
}

func (t tabModel) update(msg tea.Msg) (tabModel, tea.Cmd) {
	var cmd tea.Cmd
	t.table, cmd = t.table.Update(msg)
	return t, cmd
}

func (t tabModel) view() string {
	if len(t.prs) == 0 {
		return "\n  No PRs to show.\n"
	}
	return t.table.View() + "\n"
}

// lastActionLabel returns a display label, whether action is needed, and the
// reference timestamp for the event (used for age-based color escalation).
func lastActionLabel(pr PR, ur UserReview) (label string, actionNeeded bool, since time.Time) {
	reviewed := !ur.ReviewedAt.IsZero()

	if !reviewed {
		// No review yet — show PR age, action needed.
		if pr.IsReviewer {
			return "Requested", true, pr.CreatedAt
		}
		if pr.IsAssignee {
			return "Assigned", true, pr.CreatedAt
		}
		return "New", true, pr.CreatedAt
	}

	// Has a review — check what happened since.
	if ur.State == "APPROVED" {
		return "Approved", false, ur.ReviewedAt
	}

	if pr.LastCommitAt.After(ur.ReviewedAt) {
		return "Author updated", true, pr.LastCommitAt
	}
	if pr.LastAuthorCommentAt.After(ur.ReviewedAt) {
		return "Author replied", true, pr.LastAuthorCommentAt
	}

	if ur.State == "CHANGES_REQUESTED" {
		return "Changes req'd", false, ur.ReviewedAt
	}
	return "Reviewed", false, ur.ReviewedAt
}

var (
	lastActionActionStyle = lipgloss.NewStyle()                                   // default — age escalation applied below
	lastActionWaitStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("240")) // dim gray
	lastActionYellow      = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	lastActionRed         = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
)

func lastActionStyle(label string, actionNeeded bool, since time.Time) string {
	age := time.Since(since)
	text := label + " " + formatAge(since)
	if !actionNeeded {
		return lastActionWaitStyle.Render(text)
	}
	switch {
	case age >= 3*24*time.Hour:
		return lastActionRed.Render(text)
	case age >= 24*time.Hour:
		return lastActionYellow.Render(text)
	default:
		return text
	}
}

// Helpers

func shortRepo(repo string) string {
	// "lancedb/lancedb" -> "lancedb"
	parts := splitRepoParts(repo)
	if parts[0] == parts[1] {
		return parts[1]
	}
	return repo
}

func splitRepoParts(repo string) [2]string {
	for i, c := range repo {
		if c == '/' {
			return [2]string{repo[:i], repo[i+1:]}
		}
	}
	return [2]string{repo, repo}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

var (
	ciPassStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("2")) // green
	ciFailStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("1")) // red
	ciNeedsApprovalStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("3")) // yellow

	authorInternalStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))   // cyan — org member/owner
	authorNewStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))   // yellow — first-time/unknown
	authorBotStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("240")) // dim gray — bot
)

func authorLabel(pr PR) string {
	name := truncate(pr.Author, 13)
	if pr.IsBot {
		return authorBotStyle.Render(name)
	}
	switch pr.AuthorAssociation {
	case "OWNER", "MEMBER":
		return authorInternalStyle.Render(name)
	case "COLLABORATOR", "CONTRIBUTOR":
		return name
	default: // FIRST_TIME_CONTRIBUTOR, FIRST_TIMER, NONE
		return authorNewStyle.Render(name)
	}
}

func ciStatusLabel(status CIStatus) string {
	switch status {
	case CIStatusPass:
		return ciPassStyle.Render("  ✓")
	case CIStatusFail:
		return ciFailStyle.Render("  ✗")
	case CIStatusPending:
		return "  …"
	case CIStatusNeedsApproval:
		return ciNeedsApprovalStyle.Render(" !CI")
	default:
		return "  -"
	}
}

var (
	reviewApprovedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	reviewChangesStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	mergeConflictStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
)

func reviewDecisionLabel(decision string) string {
	switch decision {
	case "APPROVED":
		return reviewApprovedStyle.Render("  ✓")
	case "CHANGES_REQUESTED":
		return reviewChangesStyle.Render("  ✗")
	case "REVIEW_REQUIRED":
		return "  …"
	default:
		return "  -"
	}
}

func mergeableLabel(mergeable string) string {
	switch mergeable {
	case "MERGEABLE":
		return "  ✓"
	case "CONFLICTING":
		return mergeConflictStyle.Render("  !")
	default:
		return "  ?"
	}
}

func formatAge(created time.Time) string {
	hours := time.Since(created).Hours()
	if hours < 1 {
		return "<1h"
	}
	if hours < 24 {
		return fmt.Sprintf("%dh", int(hours))
	}
	days := int(math.Floor(hours / 24))
	return fmt.Sprintf("%dd", days)
}

func tableStyles() table.Styles {
	s := table.DefaultStyles()
	s.Header = s.Header.
		Bold(true).
		Foreground(lipgloss.Color("205")).
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderForeground(lipgloss.Color("240"))
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	return s
}
