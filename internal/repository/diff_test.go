package repository

import "testing"

func TestParseDiff(t *testing.T) {
	document := ParseDiff([]byte("notice\n@@ -10,3 +10,3 @@ label\n same\n-old\n+new\n tail\n"))
	if len(document.Preamble) != 1 || len(document.Hunks) != 1 {
		t.Fatalf("unexpected structure: %#v", document)
	}
	if document.Summary != (DiffSummary{Additions: 1, Deletions: 1, Hunks: 1}) {
		t.Fatalf("unexpected summary: %#v", document.Summary)
	}
	lines := document.Hunks[0].Lines
	if lines[1].OldNo == nil || *lines[1].OldNo != 11 || lines[2].NewNo == nil || *lines[2].NewNo != 11 {
		t.Fatalf("unexpected line numbers: %#v", lines)
	}
}

func TestParseDiffHandlesZeroRangesAndMetadata(t *testing.T) {
	document := ParseDiff([]byte("@@ -0,0 +1,2 @@\n+first\n+second\n\\ No newline at end of file\n"))
	if document.Summary.Additions != 2 || document.Hunks[0].Lines[2].Type != "meta" {
		t.Fatalf("unexpected document: %#v", document)
	}
}
