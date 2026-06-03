package consolidate

import (
	"fmt"
	"os"

	"github.com/venky/mailtriaged/internal/classifier"
	"github.com/venky/mailtriaged/internal/rules"
	"gopkg.in/yaml.v3"
)

type rejectedFile struct {
	Rejected []classifier.Candidate `yaml:"rejected"`
}

func LoadCandidates(path string) ([]classifier.Candidate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var cf struct {
		Candidates []classifier.Candidate `yaml:"candidates"`
	}
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return nil, fmt.Errorf("parsing candidates: %w", err)
	}
	return cf.Candidates, nil
}

func Promote(candidatesPath, activePath string, candidateID string, actionOverride rules.Action) error {
	candidates, err := LoadCandidates(candidatesPath)
	if err != nil {
		return fmt.Errorf("loading candidates: %w", err)
	}

	var target *classifier.Candidate
	var remaining []classifier.Candidate
	for i := range candidates {
		if candidates[i].ID == candidateID {
			target = &candidates[i]
		} else {
			remaining = append(remaining, candidates[i])
		}
	}
	if target == nil {
		return fmt.Errorf("candidate %q not found", candidateID)
	}

	action := target.Action
	if actionOverride != "" {
		action = actionOverride
	}

	issues := rules.CheckSafety(target.Match, action)
	if rules.HasRejectIssues(issues) {
		return fmt.Errorf("candidate %q failed safety check: %s", candidateID, issues[0].Message)
	}

	newRule := rules.Rule{
		ID:          target.ID,
		Description: target.Reason,
		Match:       target.Match,
		Action:      action,
		Source:      "classifier",
	}

	if err := appendActiveRule(activePath, newRule); err != nil {
		return fmt.Errorf("appending to active rules: %w", err)
	}

	return writeCandidates(candidatesPath, remaining)
}

func LoadRejected(path string) ([]classifier.Candidate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var rf rejectedFile
	if err := yaml.Unmarshal(data, &rf); err != nil {
		return nil, fmt.Errorf("parsing rejected: %w", err)
	}
	return rf.Rejected, nil
}

func Reject(candidatesPath, rejectedPath string, candidateID string) error {
	candidates, err := LoadCandidates(candidatesPath)
	if err != nil {
		return fmt.Errorf("loading candidates: %w", err)
	}

	var target *classifier.Candidate
	var remaining []classifier.Candidate
	for i := range candidates {
		if candidates[i].ID == candidateID {
			target = &candidates[i]
		} else {
			remaining = append(remaining, candidates[i])
		}
	}
	if target == nil {
		return fmt.Errorf("candidate %q not found", candidateID)
	}

	if err := appendRejected(rejectedPath, *target); err != nil {
		return fmt.Errorf("appending to rejected: %w", err)
	}

	return writeCandidates(candidatesPath, remaining)
}

func appendActiveRule(path string, rule rules.Rule) error {
	var rf struct {
		Rules []rules.Rule `yaml:"rules"`
	}

	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(data) > 0 {
		if err := yaml.Unmarshal(data, &rf); err != nil {
			return fmt.Errorf("parsing active rules: %w", err)
		}
	}

	for _, existing := range rf.Rules {
		if existing.ID == rule.ID {
			return fmt.Errorf("rule %q already exists in active rules", rule.ID)
		}
	}

	enabled := true
	rule.Enabled = &enabled
	rf.Rules = append(rf.Rules, rule)

	out, err := yaml.Marshal(&rf)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0644)
}

func appendRejected(path string, candidate classifier.Candidate) error {
	var rf rejectedFile

	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(data) > 0 {
		if err := yaml.Unmarshal(data, &rf); err != nil {
			return fmt.Errorf("parsing rejected: %w", err)
		}
	}

	rf.Rejected = append(rf.Rejected, candidate)

	out, err := yaml.Marshal(&rf)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0644)
}

func writeCandidates(path string, candidates []classifier.Candidate) error {
	if len(candidates) == 0 {
		return os.WriteFile(path, []byte("candidates: []\n"), 0644)
	}

	cf := struct {
		Candidates []classifier.Candidate `yaml:"candidates"`
	}{Candidates: candidates}

	out, err := yaml.Marshal(&cf)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0644)
}
