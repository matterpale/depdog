package config

import (
	"bytes"
	"strings"
	"testing"

	"github.com/matterpale/depdog/internal/core"
)

func TestBaselineRoundTrip(t *testing.T) {
	b := &core.Baseline{Entries: []core.BaselineEntry{
		{FromPackage: "m/z", Import: "m/a"},
		{FromPackage: "m/a", Import: "m/b"},
	}}
	b.Sort()

	var buf bytes.Buffer
	if err := WriteBaseline(&buf, b); err != nil {
		t.Fatalf("WriteBaseline: %v", err)
	}
	got, err := ParseBaseline(buf.Bytes())
	if err != nil {
		t.Fatalf("ParseBaseline: %v", err)
	}
	if len(got.Entries) != 2 || got.Entries[0].FromPackage != "m/a" || got.Entries[1].Import != "m/a" {
		t.Errorf("round-trip mismatch: %+v", got.Entries)
	}
}

func TestWriteBaselineEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteBaseline(&buf, &core.Baseline{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "violations: []") {
		t.Errorf("empty baseline should render an empty list:\n%s", buf.String())
	}
	if b, err := ParseBaseline(buf.Bytes()); err != nil || len(b.Entries) != 0 {
		t.Errorf("empty round-trip: %v %+v", err, b)
	}
}

func TestParseBaselineErrors(t *testing.T) {
	if _, err := ParseBaseline([]byte("version: 2\n")); err == nil || !strings.Contains(err.Error(), "version 2") {
		t.Errorf("want version error, got %v", err)
	}
	if _, err := ParseBaseline([]byte("version: 1\nviolations:\n  - from: x\n")); err == nil || !strings.Contains(err.Error(), "import") {
		t.Errorf("want missing-import error, got %v", err)
	}
	if b, err := ParseBaseline([]byte("")); err != nil || len(b.Entries) != 0 {
		t.Errorf("empty input should be an empty baseline: %v %+v", err, b)
	}
}
