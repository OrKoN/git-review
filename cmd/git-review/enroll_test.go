package main

import (
	"strings"
	"testing"
)

func TestReadEnrollmentStopsAtNewline(t *testing.T) {
	bundle, err := readEnrollment(strings.NewReader("gr1:bundle\ninput that must not be read"))
	if err != nil {
		t.Fatal(err)
	}
	if bundle != "gr1:bundle" {
		t.Fatalf("readEnrollment() = %q, want gr1:bundle", bundle)
	}
}

func TestReadEnrollmentRejectsEmptyInput(t *testing.T) {
	if _, err := readEnrollment(strings.NewReader("")); err == nil {
		t.Fatal("readEnrollment accepted empty input")
	}
}
