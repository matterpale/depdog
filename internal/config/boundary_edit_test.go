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

func TestSetBoundarySealed(t *testing.T) {
	// Shorthand → sealed: the member text is kept verbatim inside the expanded
	// single-line form, and the trailing comment survives.
	out, err := SetBoundarySealed([]byte(boundaryFixture), "peers", true)
	if err != nil {
		t.Fatal(err)
	}
	pl := lineFor(t, out, "peers:")
	if !strings.Contains(pl, "{ members: [ a, b ], sealed: true }") || !strings.Contains(pl, "# shorthand") {
		t.Errorf("peers should expand in place and keep its comment:\n%s", pl)
	}
	rs, err := Parse(out)
	if err != nil {
		t.Fatalf("Parse after seal: %v", err)
	}
	for _, b := range rs.Boundaries {
		if b.Name == "peers" && !b.Sealed {
			t.Error("peers should compile sealed")
		}
	}

	// Unsealing a shorthand boundary is a no-op (absent means false).
	same, err := SetBoundarySealed([]byte(boundaryFixture), "peers", false)
	if err != nil {
		t.Fatal(err)
	}
	if string(same) != boundaryFixture {
		t.Error("unsealing an unsealed shorthand boundary should be a no-op")
	}

	// Existing sealed scalar rewritten in place (block mapping).
	out, err = SetBoundarySealed([]byte(boundaryFixture), "sealed-set", false)
	if err != nil {
		t.Fatal(err)
	}
	if sl := lineFor(t, out, "sealed:"); !strings.Contains(sl, "sealed: false") {
		t.Errorf("sealed-set should rewrite its scalar:\n%s", sl)
	}
	if _, err := Parse(out); err != nil {
		t.Fatalf("Parse after unseal: %v", err)
	}

	// Sealing it again with the flag it already has is a no-op.
	same, err = SetBoundarySealed([]byte(boundaryFixture), "sealed-set", true)
	if err != nil {
		t.Fatal(err)
	}
	if string(same) != boundaryFixture {
		t.Error("sealing an already-sealed boundary should be a no-op")
	}
}

func TestSetBoundarySealedShapes(t *testing.T) {
	// Single-line flow mapping without a sealed key gains one before the brace.
	flow := `version: 2
components:
  a: { path: "a/**" }
  b: { path: "b/**" }
boundaries:
  flowy: { members: [a, b] } # flow form
`
	out, err := SetBoundarySealed([]byte(flow), "flowy", true)
	if err != nil {
		t.Fatal(err)
	}
	fl := lineFor(t, out, "flowy:")
	if !strings.Contains(fl, "{ members: [a, b], sealed: true }") || !strings.Contains(fl, "# flow form") {
		t.Errorf("flow mapping should gain sealed inline and keep its comment:\n%s", fl)
	}
	if _, err := Parse(out); err != nil {
		t.Fatalf("Parse after flow seal: %v", err)
	}

	// Block mapping without a sealed key gains an aligned sealed line.
	block := `version: 2
components:
  a: { path: "a/**" }
  b: { path: "b/**" }
boundaries:
  blocky:
    members: [a, b]

default: allow
`
	out, err = SetBoundarySealed([]byte(block), "blocky", true)
	if err != nil {
		t.Fatal(err)
	}
	txt := string(out)
	if !strings.Contains(txt, "    members: [a, b]\n    sealed: true\n") {
		t.Errorf("block mapping should gain an aligned sealed line:\n%s", txt)
	}
	if !strings.Contains(txt, "default: allow") {
		t.Error("the rest of the file must survive")
	}
	if _, err := Parse(out); err != nil {
		t.Fatalf("Parse after block seal: %v", err)
	}

	// Unknown boundary errors.
	if _, err := SetBoundarySealed([]byte(flow), "ghost", true); err == nil ||
		!strings.Contains(err.Error(), `boundary "ghost" is not in the config`) {
		t.Errorf("unknown boundary error = %v", err)
	}
}

func TestSetBoundarySealedCRLF(t *testing.T) {
	// A block mapping in a CRLF file: the inserted sealed line must adopt the
	// file's CRLF ending rather than introducing a lone LF.
	block := "version: 2\r\ncomponents:\r\n  a: { path: \"a/**\" }\r\n  b: { path: \"b/**\" }\r\nboundaries:\r\n  blocky:\r\n    members: [a, b]\r\n"
	out, err := SetBoundarySealed([]byte(block), "blocky", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "    sealed: true\r\n") {
		t.Errorf("the inserted sealed line should carry the file's CRLF ending:\n%q", string(out))
	}
	// No lone LF should survive: stripping every CRLF pair leaves no bare "\n".
	if strings.Contains(strings.ReplaceAll(string(out), "\r\n", ""), "\n") {
		t.Errorf("a CRLF edit must not introduce a bare LF:\n%q", string(out))
	}
	if _, err := Parse(out); err != nil {
		t.Fatalf("Parse after CRLF block seal: %v", err)
	}
}
