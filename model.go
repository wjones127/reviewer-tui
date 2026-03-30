package main

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const (
	tabNew           = 0
	tabReview        = 1
	tabAwaitingMerge = 2
)

type keyMap struct {
	Quit          key.Binding
	TabLeft       key.Binding
	TabRight      key.Binding
	Open          key.Binding
	Assign        key.Binding
	ApproveCI     key.Binding
	Snooze        key.Binding
	Dismiss       key.Binding
	ShowDismissed key.Binding
	Refresh       key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.TabLeft, k.TabRight, k.Open, k.Assign, k.ApproveCI, k.Snooze, k.Dismiss, k.Refresh, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{k.ShortHelp()}
}

var keys = keyMap{
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
	TabLeft: key.NewBinding(
		key.WithKeys("left"),
		key.WithHelp("←/→", "switch tab"),
	),
	TabRight: key.NewBinding(
		key.WithKeys("right"),
		key.WithHelp("", ""),
	),
	Open: key.NewBinding(
		key.WithKeys("enter", "o"),
		key.WithHelp("enter/o", "open in browser"),
	),
	Assign: key.NewBinding(
		key.WithKeys("a"),
		key.WithHelp("a", "assign self"),
	),
	ApproveCI: key.NewBinding(
		key.WithKeys("c"),
		key.WithHelp("c", "approve CI"),
	),
	Snooze: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "snooze 2d"),
	),
	Dismiss: key.NewBinding(
		key.WithKeys("d"),
		key.WithHelp("d", "dismiss"),
	),
	ShowDismissed: key.NewBinding(
		key.WithKeys("D"),
		key.WithHelp("D", "show dismissed"),
	),
	Refresh: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "refresh"),
	),
}

type model struct {
	cfg       Config
	db        *DB
	activeTab int
	newTab           tabModel
	reviewTab        tabModel
	awaitingMergeTab tabModel
	help      help.Model
	spinner   spinner.Model
	loading   bool
	err       error
	width     int
	height    int
	statusMsg string
}

// Messages for async operations.
type fetchDoneMsg struct{ err error }
type actionDoneMsg struct {
	msg string
	err error
}

func newModel(cfg Config, db *DB) model {
	s := spinner.New(spinner.WithSpinner(spinner.Dot))

	m := model{
		cfg:       cfg,
		db:        db,
		activeTab:        tabNew,
		newTab:           newTabModel(tabNew),
		reviewTab:        newTabModel(tabReview),
		awaitingMergeTab: newTabModel(tabAwaitingMerge),
		help:      help.New(),
		spinner:   s,
	}
	return m
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.loadFromDB(),
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.help.SetWidth(msg.Width)
		tableHeight := m.height - 6 // tab bar + help bar + padding
		m.newTab.setSize(m.width, tableHeight)
		m.reviewTab.setSize(m.width, tableHeight)
		m.awaitingMergeTab.setSize(m.width, tableHeight)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)

	case fetchDoneMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			m.statusMsg = fmt.Sprintf("Error: %v", msg.err)
		} else {
			m.statusMsg = "Refreshed"
			cmds = append(cmds, m.loadFromDB())
		}

	case actionDoneMsg:
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("Error: %v", msg.err)
		} else {
			m.statusMsg = msg.msg
			cmds = append(cmds, m.loadFromDB())
		}

	case prsLoadedMsg:
		m.newTab.setPRs(msg.newPRs, m.cfg.Tags, msg.updatesMap)
		m.reviewTab.setPRs(msg.reviewPRs, m.cfg.Tags, msg.updatesMap)
		m.awaitingMergeTab.setPRs(msg.awaitingMergePRs, m.cfg.Tags, msg.updatesMap)

	case tea.KeyPressMsg:
		if m.loading {
			if key.Matches(msg, keys.Quit) {
				return m, tea.Quit
			}
			break
		}

		switch {
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit

		case key.Matches(msg, keys.TabLeft):
			m.activeTab--
			if m.activeTab < 0 {
				m.activeTab = tabAwaitingMerge
			}

		case key.Matches(msg, keys.TabRight):
			m.activeTab++
			if m.activeTab > tabAwaitingMerge {
				m.activeTab = tabNew
			}

		case key.Matches(msg, keys.Refresh):
			m.loading = true
			m.statusMsg = "Refreshing..."
			cmds = append(cmds, m.fetchFromGitHub())

		case key.Matches(msg, keys.Open):
			if pr, ok := m.selectedPR(); ok {
				OpenInBrowser(pr.Repo, pr.Number)
			}

		case key.Matches(msg, keys.Assign):
			if pr, ok := m.selectedPR(); ok {
				m.statusMsg = fmt.Sprintf("Assigning to %s#%d...", pr.Repo, pr.Number)
				cmds = append(cmds, m.assignSelf(pr))
			}

		case key.Matches(msg, keys.ApproveCI):
			if pr, ok := m.selectedPR(); ok {
				m.statusMsg = fmt.Sprintf("Approving CI for %s#%d...", pr.Repo, pr.Number)
				cmds = append(cmds, m.approveCI(pr))
			}

		case key.Matches(msg, keys.Snooze):
			if pr, ok := m.selectedPR(); ok {
				until := time.Now().Add(48 * time.Hour)
				if err := m.db.SnoozePR(pr.Repo, pr.Number, until); err != nil {
					m.statusMsg = fmt.Sprintf("Error snoozing: %v", err)
				} else {
					m.statusMsg = fmt.Sprintf("Snoozed %s#%d for 2 days", pr.Repo, pr.Number)
					cmds = append(cmds, m.loadFromDB())
				}
			}

		case key.Matches(msg, keys.Dismiss):
			if pr, ok := m.selectedPR(); ok {
				if err := m.db.DismissPR(pr.Repo, pr.Number); err != nil {
					m.statusMsg = fmt.Sprintf("Error dismissing: %v", err)
				} else {
					m.statusMsg = fmt.Sprintf("Dismissed %s#%d", pr.Repo, pr.Number)
					cmds = append(cmds, m.loadFromDB())
				}
			}

		case key.Matches(msg, keys.ShowDismissed):
			m.activeTab = tabNew
			m.newTab.showDismissed = !m.newTab.showDismissed
			cmds = append(cmds, m.loadFromDB())

		default:
			// Forward to active tab for table navigation
			var cmd tea.Cmd
			switch m.activeTab {
			case tabNew:
				m.newTab, cmd = m.newTab.update(msg)
			case tabReview:
				m.reviewTab, cmd = m.reviewTab.update(msg)
			case tabAwaitingMerge:
				m.awaitingMergeTab, cmd = m.awaitingMergeTab.update(msg)
			}
			cmds = append(cmds, cmd)
		}
	}

	return m, tea.Batch(cmds...)
}

func (m model) View() tea.View {
	if m.width == 0 {
		v := tea.NewView("Initializing...")
		v.AltScreen = true
		return v
	}

	var b strings.Builder

	// Tab bar
	b.WriteString(m.renderTabBar())
	b.WriteString("\n")

	// Table content
	if m.loading {
		b.WriteString(fmt.Sprintf("\n  %s Loading...\n", m.spinner.View()))
	} else {
		switch m.activeTab {
		case tabNew:
			b.WriteString(m.newTab.view())
		case tabReview:
			b.WriteString(m.reviewTab.view())
		case tabAwaitingMerge:
			b.WriteString(m.awaitingMergeTab.view())
		}
	}

	// Fill remaining space
	contentHeight := strings.Count(b.String(), "\n")
	helpHeight := 2 // help bar + status line
	for i := contentHeight; i < m.height-helpHeight; i++ {
		b.WriteString("\n")
	}

	// Status message
	if m.statusMsg != "" {
		b.WriteString(statusStyle.Render(m.statusMsg))
	}
	b.WriteString("\n")

	// Help bar
	b.WriteString(m.help.View(keys))

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

func (m model) renderTabBar() string {
	tabs := []string{"New PRs", "Review PRs", "Awaiting Merge"}
	var rendered []string
	for i, t := range tabs {
		if i == m.activeTab {
			rendered = append(rendered, activeTabStyle.Render(t))
		} else {
			rendered = append(rendered, inactiveTabStyle.Render(t))
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
}

func (m model) selectedPR() (PR, bool) {
	switch m.activeTab {
	case tabNew:
		return m.newTab.selectedPR()
	case tabReview:
		return m.reviewTab.selectedPR()
	case tabAwaitingMerge:
		return m.awaitingMergeTab.selectedPR()
	}
	return PR{}, false
}

// Commands

type prsLoadedMsg struct {
	newPRs           []PR
	reviewPRs        []PR
	awaitingMergePRs []PR
	updatesMap       map[string]bool
}

func (m model) loadFromDB() tea.Cmd {
	return func() tea.Msg {
		newPRs, err := m.db.ListNewPRs(m.cfg.User, 10)
		if err != nil {
			return fetchDoneMsg{err: err}
		}

		reviewPRs, err := m.db.ListReviewPRs(m.cfg.User)
		if err != nil {
			return fetchDoneMsg{err: err}
		}

		awaitingMergePRs, err := m.db.ListAwaitingMergePRs(m.cfg.User)
		if err != nil {
			return fetchDoneMsg{err: err}
		}

		updatesMap := make(map[string]bool)
		for _, pr := range reviewPRs {
			has, err := m.db.HasUpdatesSinceReview(pr.Repo, pr.Number)
			if err == nil {
				updatesMap[fmt.Sprintf("%s#%d", pr.Repo, pr.Number)] = has
			}
		}

		return prsLoadedMsg{
			newPRs:           newPRs,
			reviewPRs:        reviewPRs,
			awaitingMergePRs: awaitingMergePRs,
			updatesMap:       updatesMap,
		}
	}
}

func (m model) fetchFromGitHub() tea.Cmd {
	return func() tea.Msg {
		for _, repo := range m.cfg.Repos {
			if err := FetchRepoPRs(m.db, repo, m.cfg.User); err != nil {
				return fetchDoneMsg{err: fmt.Errorf("%s: %w", repo, err)}
			}
		}
		return fetchDoneMsg{}
	}
}

func (m model) assignSelf(pr PR) tea.Cmd {
	return func() tea.Msg {
		if err := AssignReviewer(pr.Repo, pr.Number, m.cfg.User); err != nil {
			return actionDoneMsg{err: err}
		}
		return actionDoneMsg{msg: fmt.Sprintf("Assigned to %s#%d", pr.Repo, pr.Number)}
	}
}

func (m model) approveCI(pr PR) tea.Cmd {
	return func() tea.Msg {
		if err := ApproveWorkflowRuns(pr.Repo, pr.HeadSHA); err != nil {
			return actionDoneMsg{err: err}
		}
		return actionDoneMsg{msg: fmt.Sprintf("Approved CI for %s#%d", pr.Repo, pr.Number)}
	}
}

// Styles

var (
	activeTabStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205")).
			Padding(0, 2).
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(lipgloss.Color("205"))

	inactiveTabStyle = lipgloss.NewStyle().
				Padding(0, 2).
				Foreground(lipgloss.Color("240")).
				Border(lipgloss.NormalBorder(), false, false, true, false).
				BorderForeground(lipgloss.Color("240"))

	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Italic(true)
)
