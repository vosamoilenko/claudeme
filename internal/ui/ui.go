package ui

import (
	"fmt"
	"io"
	"sort"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/vosamoilenko/claudeme/internal/config"
)

var (
	titleStyle        = lipgloss.NewStyle().MarginLeft(2).Bold(true)
	itemStyle         = lipgloss.NewStyle().PaddingLeft(4)
	selectedItemStyle = lipgloss.NewStyle().PaddingLeft(2).Foreground(lipgloss.Color("170"))
	activeStyle       = lipgloss.NewStyle().PaddingLeft(4).Foreground(lipgloss.Color("240"))
	helpStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).MarginLeft(2)
	aliasStyle        = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("170"))
	deleteStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
)

// Action represents what the user wants to do
type Action int

const (
	ActionNone Action = iota
	ActionSelect
	ActionDelete
)

// item wraps a profile for the list
type item struct {
	email    string
	profile  config.Profile
	alias    string
	isActive bool
}

func (i item) FilterValue() string {
	if i.alias != "" {
		return i.alias + " " + i.email
	}
	return i.email
}

type itemDelegate struct{}

func (d itemDelegate) Height() int                             { return 1 }
func (d itemDelegate) Spacing() int                            { return 0 }
func (d itemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d itemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(item)
	if !ok {
		return
	}

	str := i.email
	if i.alias != "" {
		str += " " + aliasStyle.Render(i.alias)
	}
	if i.profile.Org != "" {
		str += " [" + i.profile.Org + "]"
	}
	if i.isActive {
		str += " (active)"
	}

	fn := itemStyle.Render
	if index == m.Index() {
		fn = func(s ...string) string {
			return selectedItemStyle.Render("> " + s[0])
		}
	} else if i.isActive {
		fn = activeStyle.Render
	}

	fmt.Fprint(w, fn(str))
}

// Model is the main TUI model
type Model struct {
	list          list.Model
	choice        string
	action        Action
	quitting      bool
	confirmDelete bool
	deleteTarget  string
}

// New creates a new TUI model
func New(cfg *config.ProfilesConfig) Model {
	aliases, _ := config.LoadAliases()

	// Sort by email for stable ordering
	emails := make([]string, 0, len(cfg.Profiles))
	for email := range cfg.Profiles {
		emails = append(emails, email)
	}
	sort.Strings(emails)

	items := make([]list.Item, len(emails))
	for i, email := range emails {
		alias := ""
		if aliases != nil {
			alias = aliases.FindAlias(email)
		}
		items[i] = item{
			email:    email,
			profile:  cfg.Profiles[email],
			alias:    alias,
			isActive: email == cfg.Active,
		}
	}

	l := list.New(items, itemDelegate{}, 60, min(len(items)+6, 14))
	l.Title = "claudeme"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.Styles.Title = titleStyle
	l.SetShowHelp(false)

	return Model{
		list:   l,
		action: ActionNone,
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.list.SetWidth(msg.Width)
		return m, nil

	case tea.KeyMsg:
		// Handle delete confirmation
		if m.confirmDelete {
			switch msg.String() {
			case "y", "Y":
				m.action = ActionDelete
				return m, tea.Quit
			case "n", "N", "esc":
				m.confirmDelete = false
				m.deleteTarget = ""
				return m, nil
			}
			return m, nil
		}

		// Don't capture keys when filtering
		if m.list.FilterState() == list.Filtering {
			break
		}

		switch msg.String() {
		case "q", "ctrl+c", "esc":
			m.quitting = true
			return m, tea.Quit

		case "enter":
			if i, ok := m.list.SelectedItem().(item); ok {
				m.choice = i.email
				m.action = ActionSelect
			}
			return m, tea.Quit

		case "d", "x":
			if i, ok := m.list.SelectedItem().(item); ok {
				m.deleteTarget = i.email
				m.confirmDelete = true
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}

	if m.confirmDelete && m.deleteTarget != "" {
		return fmt.Sprintf("\n  %s\n\n    %s\n\n  %s\n",
			deleteStyle.Render("Delete profile?"),
			m.deleteTarget,
			helpStyle.Render("y: yes  n: no"),
		)
	}

	return "\n" + m.list.View() + "\n" + helpStyle.Render("  enter: select  d: delete  /: filter  q: quit") + "\n"
}

func (m Model) Choice() string    { return m.choice }
func (m Model) Action() Action    { return m.action }
func (m Model) DeleteTarget() string { return m.deleteTarget }

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
