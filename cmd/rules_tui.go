package cmd

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/venky/mailtriaged/internal/classifier"
	"github.com/venky/mailtriaged/internal/consolidate"
	"github.com/venky/mailtriaged/internal/rules"
)

var rulesTUICmd = &cobra.Command{
	Use:   "tui",
	Short: "Interactively review candidate rules",
	RunE:  runRulesTUI,
}

func init() {
	rulesCmd.AddCommand(rulesTUICmd)
}

func runRulesTUI(cmd *cobra.Command, args []string) error {
	candidates, err := consolidate.LoadCandidates(candidatesPath())
	if err != nil {
		return fmt.Errorf("loading candidates: %w", err)
	}
	if len(candidates) == 0 {
		fmt.Println("no candidates to review")
		return nil
	}

	m := newTUIModel(candidates)
	p := tea.NewProgram(m)
	final, err := p.Run()
	if err != nil {
		return err
	}

	fm := final.(tuiModel)
	if fm.err != nil {
		return fm.err
	}

	for _, a := range fm.actions {
		fmt.Println(a)
	}
	return nil
}

type tuiModel struct {
	candidates []classifier.Candidate
	index      int
	actions    []string
	err        error
	quitted    bool
	width      int
}

func newTUIModel(candidates []classifier.Candidate) tuiModel {
	return tuiModel{
		candidates: candidates,
		width:      80,
	}
}

func (m tuiModel) Init() tea.Cmd {
	return nil
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitted = true
			return m, tea.Quit

		case "p":
			return m.doPromote("")
		case "i":
			return m.doPromote(rules.ActionIgnore)
		case "a":
			return m.doPromote(rules.ActionAlertNow)
		case "d":
			return m.doPromote(rules.ActionDailySummary)
		case "n":
			return m.doPromote(rules.ActionNeedsReview)
		case "r":
			return m.doReject()
		case "s":
			return m.doSkip()
		}
	}
	return m, nil
}

func (m tuiModel) doPromote(actionOverride rules.Action) (tea.Model, tea.Cmd) {
	c := m.candidates[m.index]
	err := consolidate.Promote(candidatesPath(), activePath(), c.ID, actionOverride)
	if err != nil {
		m.err = err
		return m, tea.Quit
	}

	action := c.Action
	if actionOverride != "" {
		action = actionOverride
	}
	m.actions = append(m.actions, fmt.Sprintf("promoted  %-40s → %s", c.ID, action))
	return m.advance()
}

func (m tuiModel) doReject() (tea.Model, tea.Cmd) {
	c := m.candidates[m.index]
	err := consolidate.Reject(candidatesPath(), rejectedPath(), c.ID)
	if err != nil {
		m.err = err
		return m, tea.Quit
	}
	m.actions = append(m.actions, fmt.Sprintf("rejected  %s", c.ID))
	return m.advance()
}

func (m tuiModel) doSkip() (tea.Model, tea.Cmd) {
	c := m.candidates[m.index]
	m.actions = append(m.actions, fmt.Sprintf("skipped   %s", c.ID))
	return m.advance()
}

func (m tuiModel) advance() (tea.Model, tea.Cmd) {
	m.index++
	if m.index >= len(m.candidates) {
		return m, tea.Quit
	}
	return m, nil
}

var (
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	labelStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	actionStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11"))
	safeStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	warnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	helpStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

func (m tuiModel) View() string {
	if m.quitted || m.index >= len(m.candidates) {
		return ""
	}

	c := m.candidates[m.index]
	issues := rules.CheckSafety(c.Match, c.Action)
	safety := "OK"
	safetyStyled := safeStyle.Render("OK")
	if rules.HasRejectIssues(issues) {
		safety = "REJECT"
		safetyStyled = warnStyle.Render(safety)
	} else if len(issues) > 0 {
		safety = "WARN"
		safetyStyled = warnStyle.Render(safety)
	}
	_ = safety

	var sb strings.Builder

	sb.WriteString(headerStyle.Render(fmt.Sprintf("Candidate %d/%d", m.index+1, len(m.candidates))))
	sb.WriteString("\n\n")

	sb.WriteString(fmt.Sprintf("  %s  %s\n", labelStyle.Render("id:"), c.ID))
	sb.WriteString(fmt.Sprintf("  %s  %s\n", labelStyle.Render("action:"), actionStyle.Render(string(c.Action))))
	sb.WriteString(fmt.Sprintf("  %s  %s\n", labelStyle.Render("safety:"), safetyStyled))
	sb.WriteString(fmt.Sprintf("  %s  %s\n", labelStyle.Render("reason:"), c.Reason))

	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("  %s\n", labelStyle.Render("match:")))
	if c.Match.FromEmail != "" {
		sb.WriteString(fmt.Sprintf("    from_email:           %s\n", c.Match.FromEmail))
	}
	if c.Match.FromDomain != "" {
		sb.WriteString(fmt.Sprintf("    from_domain:          %s\n", c.Match.FromDomain))
	}
	if c.Match.ToContains != "" {
		sb.WriteString(fmt.Sprintf("    to_contains:          %s\n", c.Match.ToContains))
	}
	if c.Match.CcContains != "" {
		sb.WriteString(fmt.Sprintf("    cc_contains:          %s\n", c.Match.CcContains))
	}
	if c.Match.ListID != "" {
		sb.WriteString(fmt.Sprintf("    list_id:              %s\n", c.Match.ListID))
	}
	if len(c.Match.SubjectContainsAll) > 0 {
		sb.WriteString(fmt.Sprintf("    subject_contains_all: %v\n", c.Match.SubjectContainsAll))
	}
	if len(c.Match.SubjectContainsAny) > 0 {
		sb.WriteString(fmt.Sprintf("    subject_contains_any: %v\n", c.Match.SubjectContainsAny))
	}

	for _, issue := range issues {
		sb.WriteString(fmt.Sprintf("  %s\n", warnStyle.Render(fmt.Sprintf("[%s] %s", issue.Severity, issue.Message))))
	}

	sb.WriteString("\n")
	sb.WriteString(helpStyle.Render("  p promote  i ignore  a alert  d daily  n needs_review  r reject  s skip  q quit"))
	sb.WriteString("\n")

	return sb.String()
}
