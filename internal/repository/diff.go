package repository

import (
	"regexp"
	"strconv"
	"strings"
)

type DiffDocument struct {
	Preamble []string    `json:"preamble"`
	Hunks    []DiffHunk  `json:"hunks"`
	Summary  DiffSummary `json:"summary"`
}

type DiffSummary struct {
	Additions int `json:"additions"`
	Deletions int `json:"deletions"`
	Hunks     int `json:"hunks"`
}

type DiffHunk struct {
	Index  int        `json:"index"`
	Header string     `json:"header"`
	Lines  []DiffLine `json:"lines"`
}

type DiffLine struct {
	Type    string `json:"type"`
	Raw     string `json:"raw"`
	Text    string `json:"text"`
	OldNo   *int   `json:"oldNo,omitempty"`
	NewNo   *int   `json:"newNo,omitempty"`
	Ordinal *int   `json:"ordinal,omitempty"`
}

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@(?:.*)?$`)

// ParseDiff converts Git's transport format to the API's frontend-neutral model.
func ParseDiff(text []byte) DiffDocument {
	document := DiffDocument{Preamble: []string{}, Hunks: []DiffHunk{}}
	lines := strings.Split(strings.TrimSuffix(string(text), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}
	var hunk *DiffHunk
	oldLine, newLine, ordinal, oldRemaining, newRemaining := 0, 0, 0, 0, 0
	for _, raw := range lines {
		match := hunkHeader.FindStringSubmatch(raw)
		if match != nil {
			oldLine, _ = strconv.Atoi(match[1])
			newLine, _ = strconv.Atoi(match[3])
			oldRemaining = count(match[2])
			newRemaining = count(match[4])
			ordinal = 0
			document.Hunks = append(document.Hunks, DiffHunk{Index: len(document.Hunks), Header: raw, Lines: []DiffLine{}})
			hunk = &document.Hunks[len(document.Hunks)-1]
			continue
		}
		if strings.HasPrefix(raw, "diff --git ") {
			hunk = nil
			continue
		}
		if hunk == nil {
			if raw != "" && !strings.HasPrefix(raw, "index ") && !strings.HasPrefix(raw, "--- ") && !strings.HasPrefix(raw, "+++ ") {
				document.Preamble = append(document.Preamble, raw)
			}
			continue
		}
		line := DiffLine{Type: "meta", Raw: raw, Text: raw}
		switch {
		case strings.HasPrefix(raw, "+") && newRemaining > 0:
			line.Type, line.Text, line.NewNo, line.Ordinal = "add", raw[1:], integer(newLine), integer(ordinal)
			newLine, newRemaining, ordinal = newLine+1, newRemaining-1, ordinal+1
			document.Summary.Additions++
		case strings.HasPrefix(raw, "-") && oldRemaining > 0:
			line.Type, line.Text, line.OldNo, line.Ordinal = "del", raw[1:], integer(oldLine), integer(ordinal)
			oldLine, oldRemaining, ordinal = oldLine+1, oldRemaining-1, ordinal+1
			document.Summary.Deletions++
		case strings.HasPrefix(raw, " ") && oldRemaining > 0 && newRemaining > 0:
			line.Type, line.Text, line.OldNo, line.NewNo = "context", raw[1:], integer(oldLine), integer(newLine)
			oldLine, newLine, oldRemaining, newRemaining = oldLine+1, newLine+1, oldRemaining-1, newRemaining-1
		}
		hunk.Lines = append(hunk.Lines, line)
	}
	document.Summary.Hunks = len(document.Hunks)
	return document
}

func count(value string) int {
	if value == "" {
		return 1
	}
	result, _ := strconv.Atoi(value)
	return result
}

func integer(value int) *int { return &value }
