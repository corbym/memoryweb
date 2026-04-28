package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestDrawProgressBar_Format(t *testing.T) {
	var buf bytes.Buffer
	drawProgressBar(&buf, 5, 10)
	got := buf.String()

	if !strings.HasPrefix(got, "\r") {
		t.Errorf("progress bar should start with \\r; got %q", got)
	}
	if !strings.Contains(got, "5/10") {
		t.Errorf("progress bar should contain '5/10'; got %q", got)
	}
	if !strings.Contains(got, "50%") {
		t.Errorf("progress bar should contain '50%%'; got %q", got)
	}
	if !strings.Contains(got, "[") || !strings.Contains(got, "]") {
		t.Errorf("progress bar should contain '[' and ']'; got %q", got)
	}
}

func TestDrawProgressBar_Complete(t *testing.T) {
	var buf bytes.Buffer
	drawProgressBar(&buf, 10, 10)
	got := buf.String()

	if !strings.Contains(got, "10/10") {
		t.Errorf("complete bar should show '10/10'; got %q", got)
	}
	if !strings.Contains(got, "100%") {
		t.Errorf("complete bar should show '100%%'; got %q", got)
	}
	// At 100% the bar should be all '=' with no '>'
	if strings.Contains(got, ">") {
		t.Errorf("complete bar should not contain '>'; got %q", got)
	}
}

func TestDrawProgressBar_First(t *testing.T) {
	var buf bytes.Buffer
	drawProgressBar(&buf, 1, 100)
	got := buf.String()

	if !strings.Contains(got, "1/100") {
		t.Errorf("first step should show '1/100'; got %q", got)
	}
	// Should contain the '>' cursor marker
	if !strings.Contains(got, ">") {
		t.Errorf("in-progress bar should contain '>'; got %q", got)
	}
}

