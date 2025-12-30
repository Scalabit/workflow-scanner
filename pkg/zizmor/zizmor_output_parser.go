package zizmor

import (
	"encoding/json"
	"fmt"
	"strings"
)

type Finding struct {
	Ident          string         `json:"ident"`
	Desc           string         `json:"desc"`
	URL            string         `json:"url"`
	Determinations Determinations `json:"determinations"`
	Locations      []Location     `json:"locations"`
	Ignored        bool           `json:"ignored"`
}

type Determinations struct {
	Confidence string `json:"confidence"`
	Severity   string `json:"severity"`
	Persona    string `json:"persona"`
}

type Location struct {
	Symbolic Symbolic `json:"symbolic"`
	Concrete Concrete `json:"concrete"`
}

type Symbolic struct {
	Key         Key             `json:"key"`
	Annotation  string          `json:"annotation"`
	Route       Route           `json:"route"`
	FeatureKind json.RawMessage `json:"feature_kind"`
	Kind        string          `json:"kind"`
}

type Key struct {
	Local *LocalKey `json:"Local,omitempty"`
}

type LocalKey struct {
	Prefix    string `json:"prefix"`
	GivenPath string `json:"given_path"`
}

type Route struct {
	Route []RouteElement `json:"route"`
}

type RouteElement struct {
	Key   *string `json:"Key,omitempty"`
	Index *int    `json:"Index,omitempty"`
}

type Concrete struct {
	Location LocationDetail `json:"location"`
	Feature  string         `json:"feature"`
	Comments []string       `json:"comments"`
}

type LocationDetail struct {
	StartPoint Point `json:"start_point"`
	EndPoint   Point `json:"end_point"`
	OffsetSpan Span  `json:"offset_span"`
}

type Point struct {
	Row    int `json:"row"`
	Column int `json:"column"`
}

type Span struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

func ParseZizmorOutput(input string) ([]Finding, string, error) {
	start := strings.Index(input, "[")
	if start == -1 {
		return nil, "", fmt.Errorf("no JSON array found")
	}

	bracketCount := 0
	end := -1

	for i := start; i < len(input); i++ {
		if input[i] == '[' {
			bracketCount++
		} else if input[i] == ']' {
			bracketCount--
			if bracketCount == 0 {
				end = i + 1
				break
			}
		}
	}

	if end == -1 {
		return nil, "", fmt.Errorf("unclosed JSON array")
	}

	jsonPart := input[start:end]

	var findings []Finding
	if err := json.Unmarshal([]byte(jsonPart), &findings); err != nil {
		return nil, "", fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	fixSummary := ""
	if end < len(input) {
		fixSummary = strings.TrimSpace(input[end:])
	}

	return findings, fixSummary, nil
}
