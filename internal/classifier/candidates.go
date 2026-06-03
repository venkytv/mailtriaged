package classifier

import (
	"fmt"
	"os"
	"time"

	"github.com/venky/mailtriaged/internal/rules"
	"gopkg.in/yaml.v3"
)

type Candidate struct {
	ID              string      `yaml:"id"`
	CreatedAt       string      `yaml:"created_at"`
	SourceMessageID string      `yaml:"source_message_id"`
	ProposedBy      string      `yaml:"proposed_by"`
	Action          rules.Action `yaml:"action"`
	Reason          string      `yaml:"reason"`
	Safety          string      `yaml:"safety"`
	Match           rules.Match `yaml:"match"`
}

type candidateFile struct {
	Candidates []Candidate `yaml:"candidates"`
}

func AppendCandidate(path string, suggested *SuggestedRule, messageID string, reason string) error {
	existing, err := loadCandidates(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("loading candidates: %w", err)
	}

	if hasDuplicateMatch(existing, suggested.Match) {
		return nil
	}

	candidate := Candidate{
		ID:              fmt.Sprintf("candidate_%s", time.Now().Format("20060102_150405")),
		CreatedAt:       time.Now().Format(time.RFC3339),
		SourceMessageID: messageID,
		ProposedBy:      "classifier",
		Action:          suggested.Action,
		Reason:          reason,
		Safety:          suggested.Safety,
		Match:           suggested.Match,
	}

	existing = append(existing, candidate)

	return writeCandidates(path, existing)
}

func loadCandidates(path string) ([]Candidate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cf candidateFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return nil, fmt.Errorf("parsing candidates: %w", err)
	}

	return cf.Candidates, nil
}

func writeCandidates(path string, candidates []Candidate) error {
	cf := candidateFile{Candidates: candidates}

	data, err := yaml.Marshal(&cf)
	if err != nil {
		return fmt.Errorf("marshaling candidates: %w", err)
	}

	return os.WriteFile(path, data, 0644)
}

func hasDuplicateMatch(existing []Candidate, match rules.Match) bool {
	return HasRejectedMatch(existing, match)
}

func HasRejectedMatch(candidates []Candidate, match rules.Match) bool {
	key := matchKey(match)
	for _, c := range candidates {
		if matchKey(c.Match) == key {
			return true
		}
	}
	return false
}

func matchKey(m rules.Match) string {
	data, _ := yaml.Marshal(m)
	return string(data)
}
