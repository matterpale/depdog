package config

import (
	"strings"
	"testing"
)

const boundaryFixture = `version: 2

components:
  a: { path: "a/**" }
  b: { path: "b/**" }
  c: { path: "c/**" }

boundaries:
  peers: [ a, b ]                 # shorthand
  sealed-set:
    members: [ a, c ]
    sealed: true

default: allow
`

func TestAddBoundaryMember(t *testing.T) {
	// Shorthand list.
	out, err := AddBoundaryMember([]byte(boundaryFixture), "peers", "c")
	if err != nil {
		t.Fatal(err)
	}
	pl := lineFor(t, out, "peers:")
	if !strings.Contains(pl, "c") || !strings.Contains(pl, "# shorthand") {
		t.Errorf("peers should gain c and keep its comment:\n%s", pl)
	}
	if _, err := Parse(out); err != nil {
		t.Fatalf("Parse after add: %v", err)
	}

	// Expanded members list; the sealed line and everything else survive.
	out, err = AddBoundaryMember([]byte(boundaryFixture), "sealed-set", "b")
	if err != nil {
		t.Fatal(err)
	}
	ml := lineFor(t, out, "members:")
	if !strings.Contains(ml, "b") {
		t.Errorf("sealed-set members should gain b:\n%s", ml)
	}
	if !strings.Contains(string(out), "sealed: true") {
		t.Error("the sealed line should be untouched")
	}

	// Adding an existing member is a no-op.
	same, err := AddBoundaryMember([]byte(boundaryFixture), "peers", "a")
	if err != nil {
		t.Fatal(err)
	}
	if string(same) != boundaryFixture {
		t.Error("adding an existing member should be a no-op")
	}
}

func TestRemoveBoundaryMember(t *testing.T) {
	out, err := RemoveBoundaryMember([]byte(boundaryFixture), "sealed-set", "c")
	if err != nil {
		t.Fatal(err)
	}
	ml := lineFor(t, out, "members:")
	if strings.Contains(ml, "c") {
		t.Errorf("c should be gone from sealed-set members:\n%s", ml)
	}
	if !strings.Contains(ml, "a") {
		t.Errorf("a should remain:\n%s", ml)
	}
	if _, err := Parse(out); err != nil {
		t.Fatalf("Parse after remove: %v", err)
	}

	// Removing a non-member is a no-op.
	same, err := RemoveBoundaryMember([]byte(boundaryFixture), "peers", "zzz")
	if err != nil {
		t.Fatal(err)
	}
	if string(same) != boundaryFixture {
		t.Error("removing a non-member should be a no-op")
	}
}

func TestBoundaryMemberRefusals(t *testing.T) {
	if _, err := AddBoundaryMember([]byte(boundaryFixture), "nope", "a"); err == nil {
		t.Error("an unknown boundary should be refused")
	}
}
