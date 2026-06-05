package rules

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Action string

const (
	ActionAlertNow     Action = "alert_now"
	ActionClassify     Action = "classify"
	ActionDailySummary Action = "daily_summary"
	ActionIgnore       Action = "ignore"
	ActionNeedsReview  Action = "needs_review"
)

var validActions = map[Action]bool{
	ActionAlertNow:     true,
	ActionClassify:     true,
	ActionDailySummary: true,
	ActionIgnore:       true,
	ActionNeedsReview:  true,
}

func IsValidAction(a Action) bool {
	return validActions[a]
}

type Rule struct {
	ID             string `yaml:"id"`
	Enabled        *bool  `yaml:"enabled"`
	Description    string `yaml:"description"`
	Match          Match  `yaml:"match"`
	Action         Action `yaml:"action"`
	ClassifierHint string `yaml:"classifier_hint,omitempty"`
	Source         string `yaml:"source"`
}

func (r *Rule) IsEnabled() bool {
	return r.Enabled == nil || *r.Enabled
}

type Match struct {
	FromEmail          string            `yaml:"from_email,omitempty" json:"from_email,omitempty"`
	FromDomain         string            `yaml:"from_domain,omitempty" json:"from_domain,omitempty"`
	ToContains         string            `yaml:"to_contains,omitempty" json:"to_contains,omitempty"`
	CcContains         string            `yaml:"cc_contains,omitempty" json:"cc_contains,omitempty"`
	ListID             string            `yaml:"list_id,omitempty" json:"list_id,omitempty"`
	SubjectContainsAll []string          `yaml:"subject_contains_all,omitempty" json:"subject_contains_all,omitempty"`
	SubjectContainsAny []string          `yaml:"subject_contains_any,omitempty" json:"subject_contains_any,omitempty"`
	HeaderEquals       map[string]string `yaml:"header_equals,omitempty" json:"header_equals,omitempty"`
	HeaderContains     map[string]string `yaml:"header_contains,omitempty" json:"header_contains,omitempty"`
}

func (m *Match) HasSubjectConstraints() bool {
	return len(m.SubjectContainsAll) > 0 || len(m.SubjectContainsAny) > 0
}

func (m *Match) IsEmpty() bool {
	return m.FromEmail == "" &&
		m.FromDomain == "" &&
		m.ToContains == "" &&
		m.CcContains == "" &&
		m.ListID == "" &&
		len(m.SubjectContainsAll) == 0 &&
		len(m.SubjectContainsAny) == 0 &&
		len(m.HeaderEquals) == 0 &&
		len(m.HeaderContains) == 0
}

type ruleFile struct {
	Rules []Rule `yaml:"rules"`
}

func LoadDir(dir string) ([]Rule, error) {
	pattern := filepath.Join(dir, "*.yaml")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("globbing rules: %w", err)
	}
	sort.Strings(files)

	var all []Rule
	for _, f := range files {
		rules, err := loadFile(f)
		if err != nil {
			return nil, fmt.Errorf("loading %s: %w", filepath.Base(f), err)
		}
		all = append(all, rules...)
	}

	return all, nil
}

func loadFile(path string) ([]Rule, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var rf ruleFile
	if err := yaml.Unmarshal(data, &rf); err != nil {
		return nil, fmt.Errorf("parsing: %w", err)
	}

	return rf.Rules, nil
}

func Validate(rules []Rule) error {
	seen := make(map[string]bool)
	for i, r := range rules {
		if r.ID == "" {
			return fmt.Errorf("rule %d: id is required", i)
		}
		if seen[r.ID] {
			return fmt.Errorf("rule %d: duplicate id %q", i, r.ID)
		}
		seen[r.ID] = true

		if !validActions[r.Action] {
			return fmt.Errorf("rule %q: unsupported action %q", r.ID, r.Action)
		}

		if r.Match.IsEmpty() {
			return fmt.Errorf("rule %q: match must have at least one condition", r.ID)
		}
	}
	return nil
}

type Decision struct {
	Action         Action `json:"action"`
	Source         string `json:"source"`
	RuleID         string `json:"rule_id,omitempty"`
	Reason         string `json:"reason"`
	ClassifierHint string `json:"classifier_hint,omitempty"`
}

func Evaluate(rules []Rule, msg MessageData) *Decision {
	for _, r := range rules {
		if !r.IsEnabled() {
			continue
		}
		if matchRule(r.Match, msg) {
			return &Decision{
				Action:         r.Action,
				Source:         "rule",
				RuleID:         r.ID,
				Reason:         "Matched active rule",
				ClassifierHint: r.ClassifierHint,
			}
		}
	}
	return nil
}

type MessageData struct {
	FromEmail string
	FromDomain string
	To        []string
	Cc        []string
	Subject   string
	ListID    string
	Headers   map[string]string
}

func matchRule(m Match, msg MessageData) bool {
	if m.FromEmail != "" {
		if !strings.EqualFold(m.FromEmail, msg.FromEmail) {
			return false
		}
	}

	if m.FromDomain != "" {
		if !strings.EqualFold(m.FromDomain, msg.FromDomain) {
			return false
		}
	}

	if m.ToContains != "" {
		if !containsFold(msg.To, m.ToContains) {
			return false
		}
	}

	if m.CcContains != "" {
		if !containsFold(msg.Cc, m.CcContains) {
			return false
		}
	}

	if m.ListID != "" {
		if !strings.EqualFold(m.ListID, msg.ListID) {
			return false
		}
	}

	if len(m.SubjectContainsAll) > 0 {
		subLower := strings.ToLower(msg.Subject)
		for _, s := range m.SubjectContainsAll {
			if !strings.Contains(subLower, strings.ToLower(s)) {
				return false
			}
		}
	}

	if len(m.SubjectContainsAny) > 0 {
		subLower := strings.ToLower(msg.Subject)
		found := false
		for _, s := range m.SubjectContainsAny {
			if strings.Contains(subLower, strings.ToLower(s)) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if len(m.HeaderEquals) > 0 {
		for k, v := range m.HeaderEquals {
			hv, ok := msg.Headers[strings.ToLower(k)]
			if !ok || !strings.EqualFold(hv, v) {
				return false
			}
		}
	}

	if len(m.HeaderContains) > 0 {
		for k, v := range m.HeaderContains {
			hv, ok := msg.Headers[strings.ToLower(k)]
			if !ok || !strings.Contains(strings.ToLower(hv), strings.ToLower(v)) {
				return false
			}
		}
	}

	return true
}

func containsFold(list []string, target string) bool {
	for _, s := range list {
		if strings.EqualFold(s, target) {
			return true
		}
	}
	return false
}

func HasSenderRule(ruleList []Rule, fromEmail, fromDomain string, action Action) bool {
	for _, r := range ruleList {
		if !r.IsEnabled() {
			continue
		}
		if r.Action != action && r.Action != ActionClassify {
			continue
		}
		if r.Match.HasSubjectConstraints() {
			continue
		}
		if fromEmail != "" && r.Match.FromEmail != "" &&
			strings.EqualFold(r.Match.FromEmail, fromEmail) {
			return true
		}
		if fromDomain != "" && r.Match.FromDomain != "" &&
			strings.EqualFold(r.Match.FromDomain, fromDomain) {
			return true
		}
		if fromEmail != "" && r.Match.FromDomain != "" {
			parts := strings.SplitN(fromEmail, "@", 2)
			if len(parts) == 2 && strings.EqualFold(r.Match.FromDomain, parts[1]) {
				return true
			}
		}
	}
	return false
}
