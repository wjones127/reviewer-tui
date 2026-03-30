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
	tabType       int
	showDismissed bool
}

func newTabModel(tabType int) tabModel {
	cols := commonColumns()
	switch tabType {
	case tabNew:
		cols = append(cols, table.Column{Title: "New?", Width: 4})
	case tabReview:
		cols = append(cols, table.Column{Title: "Upd", Width: 3})
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
	}
}

func (t *tabModel) setSize(width, height int) {
	t.table.SetWidth(width)
	t.table.SetHeight(height)
}

func (t *tabModel) setPRs(prs []PR, tags []string, updatesMap map[string]bool) {
	t.prs = prs
	SortPRsByScore(prs, tags, updatesMap)

	rows := make([]table.Row, len(prs))
	for i, pr := range prs {
		row := table.Row{
			shortRepo(pr.Repo),
			fmt.Sprintf("#%d", pr.Number),
			truncate(pr.Title, 38),
			pr.Author,
			ciStatusLabel(pr.CIStatus),
			reviewDecisionLabel(pr.ReviewDecision),
			mergeableLabel(pr.Mergeable),
			fmt.Sprintf("+%d/-%d", pr.Additions, pr.Deletions),
			formatAge(pr.CreatedAt),
		}
		switch t.tabType {
		case tabNew:
			if pr.IsNewContributor() {
				row = append(row, " !")
			} else {
				row = append(row, "")
			}
		case tabReview:
			k := fmt.Sprintf("%s#%d", pr.Repo, pr.Number)
			if updatesMap[k] {
				row = append(row, " *")
			} else {
				row = append(row, "")
			}
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
	ciPassStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))  // green
	ciFailStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))  // red
	ciNeedsApprovalStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("3")) // yellow
)

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
