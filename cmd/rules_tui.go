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
	showHelp   bool
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
		if m.showHelp {
			m.showHelp = false
			return m, nil
		}

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
		case "r":
			return m.doReject()
		case "s":
			return m.doSkip()
		case "?":
			m.showHelp = true
			return m, nil
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
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "4", Dark: "12"})
	labelStyle  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "8", Dark: "7"})
	actionStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "3", Dark: "11"})
	safeStyle   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "2", Dark: "10"})
	warnStyle   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "1", Dark: "9"})
	helpStyle   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "8", Dark: "7"})
)

func (m tuiModel) View() string {
	if m.quitted || m.index >= len(m.candidates) {
		return ""
	}

	if m.showHelp {
		return m.viewHelp()
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
	sb.WriteString(helpStyle.Render("  p promote  i/a/d promote as...  r defer to classifier  s skip  q quit  ? help"))
	sb.WriteString("\n")

	return sb.String()
}

func (m tuiModel) viewHelp() string {
	title := headerStyle.Render("Keybindings")
	body := helpStyle.Render(strings.Join([]string{
		"",
		"  Promote (add as a permanent rule):",
		"    p   promote with the suggested action",
		"    i   promote as ignore",
		"    a   promote as alert_now",
		"    d   promote as daily_summary",
		"",
		"  Review:",
		"    r   defer to classifier — no rule is created; the classifier",
		"        will evaluate this pattern each time based on content.",
		"        Future suggestions for this pattern are suppressed.",
		"    s   skip — leave in candidates for later review",
		"",
		"  Navigation:",
		"    q   quit",
		"    ?   toggle this help",
		"",
		"  Press any key to return.",
		"",
	}, "\n"))
	return title + "\n" + body
}
