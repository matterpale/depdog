package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// dcWriteFile writes body to path, creating parent dirs. Named to avoid
// colliding with wsWriteFile in workspace_test.go.
func dcWriteFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// dcConfig drops a depdog.yaml in dir (relative to root).
func dcConfig(t *testing.T, root, dir string) {
	t.Helper()
	dcWriteFile(t, filepath.Join(root, filepath.FromSlash(dir), DefaultName), "rules: []\n")
}

// dcMarker drops a marker file in dir (relative to root).
func dcMarker(t *testing.T, root, dir, marker string) {
	t.Helper()
	dcWriteFile(t, filepath.Join(root, filepath.FromSlash(dir), marker), "")
}

// unitRels extracts the sorted Rel slice from a discovery result.
func unitRels(units []Unit) []string {
	rels := make([]string, len(units))
	for i, u := range units {
		rels[i] = u.Rel
	}
	return rels
}

// TestDiscoverUnitsMonorepoFixture is the integration-style assertion over the
// real P0 fixture: units, decoy pruning, and the disjoint-advisory rule all at
// once. testdata is itself skip-listed, so discovery is rooted AT the fixture
// dir (the skip list applies below root; root is always entered).
func TestDiscoverUnitsMonorepoFixture(t *testing.T) {
	root, err := filepath.Abs(filepath.FromSlash("../../testdata/fixtures/monorepo"))
	if err != nil {
		t.Fatal(err)
	}
	// Markers spanning several adapters; Gemfile catches legacy/, go.mod catches
	// the governed Go unit (which must NOT advisory), package.json catches root
	// scaffolding + web (both governed).
	markers := []string{"go.mod", "package.json", "Gemfile", "pyproject.toml"}

	units, ungoverned, err := DiscoverUnits(root, markers)
	if err != nil {
		t.Fatalf("DiscoverUnits: %v", err)
	}

	wantUnits := []string{"ml", "services/api", "web"}
	if got := unitRels(units); !reflect.DeepEqual(got, wantUnits) {
		t.Errorf("units = %v, want %v", got, wantUnits)
	}

	// legacy/ has a Gemfile and no unit anywhere near it -> advisory. Root
	// package.json has units below (no advisory). services/api has go.mod but is
	// itself a unit (no advisory). web/package.json sits under the web unit (no
	// advisory). ml/pyproject.toml is in the ml unit (no advisory).
	wantUngoverned := []string{"legacy"}
	if !reflect.DeepEqual(ungoverned, wantUngoverned) {
		t.Errorf("ungoverned = %v, want %v", ungoverned, wantUngoverned)
	}

	// Decoys must be invisible: node_modules pruned, dot-prefixed pruned.
	for _, u := range units {
		if u.Rel == "web/node_modules/x" {
			t.Errorf("decoy web/node_modules/x was discovered")
		}
		if u.Rel == ".hidden" {
			t.Errorf("decoy .hidden was discovered")
		}
	}
}

// TestDiscoverUnitsNested verifies a config at root and a config in a subdir are
// both discovered with correct Rel.
func TestDiscoverUnitsNested(t *testing.T) {
	root := t.TempDir()
	dcConfig(t, root, ".")     // root itself is a unit
	dcConfig(t, root, "inner") // nested unit below it

	units, _, err := DiscoverUnits(root, nil)
	if err != nil {
		t.Fatalf("DiscoverUnits: %v", err)
	}
	want := []string{".", "inner"}
	if got := unitRels(units); !reflect.DeepEqual(got, want) {
		t.Errorf("nested units = %v, want %v", got, want)
	}
	// Dir must be absolute.
	for _, u := range units {
		if !filepath.IsAbs(u.Dir) {
			t.Errorf("unit %q Dir %q is not absolute", u.Rel, u.Dir)
		}
	}
}

// TestDiscoverUnitsRelRootShape verifies Rel is "." when root itself holds a
// depdog.yaml.
func TestDiscoverUnitsRelRootShape(t *testing.T) {
	root := t.TempDir()
	dcConfig(t, root, ".")

	units, _, err := DiscoverUnits(root, nil)
	if err != nil {
		t.Fatalf("DiscoverUnits: %v", err)
	}
	if got := unitRels(units); !reflect.DeepEqual(got, []string{"."}) {
		t.Errorf("root-unit Rel = %v, want [.]", got)
	}
}

// TestDiscoverUnitsAdvisoryContainment covers all four containment cases of the
// disjoint-advisory rule (D5).
func TestDiscoverUnitsAdvisoryContainment(t *testing.T) {
	const marker = "go.mod"

	tests := []struct {
		name           string
		build          func(t *testing.T, root string)
		wantUngoverned []string
	}{
		{
			name: "disjoint marker dir -> advisory",
			build: func(t *testing.T, root string) {
				dcConfig(t, root, "svc") // a unit somewhere else
				dcMarker(t, root, "legacy", marker)
			},
			wantUngoverned: []string{"legacy"},
		},
		{
			name: "unit below marker dir -> no advisory",
			build: func(t *testing.T, root string) {
				dcMarker(t, root, "app", marker)
				dcConfig(t, root, "app/inner") // unit BELOW the marker dir
			},
			wantUngoverned: nil,
		},
		{
			name: "unit above marker dir -> no advisory",
			build: func(t *testing.T, root string) {
				dcConfig(t, root, "app") // unit ABOVE the marker dir
				dcMarker(t, root, "app/pkg", marker)
			},
			wantUngoverned: nil,
		},
		{
			name: "marker dir is also a unit -> no advisory",
			build: func(t *testing.T, root string) {
				dcConfig(t, root, "app")
				dcMarker(t, root, "app", marker) // same dir carries config + marker
			},
			wantUngoverned: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			tt.build(t, root)
			_, ungoverned, err := DiscoverUnits(root, []string{marker})
			if err != nil {
				t.Fatalf("DiscoverUnits: %v", err)
			}
			if !reflect.DeepEqual(ungoverned, tt.wantUngoverned) {
				t.Errorf("ungoverned = %v, want %v", ungoverned, tt.wantUngoverned)
			}
		})
	}
}

// TestDiscoverUnitsDeterminism asserts discovered order is lexicographic by
// Rel even when the tree is created in non-sorted order.
func TestDiscoverUnitsDeterminism(t *testing.T) {
	root := t.TempDir()
	// Create in deliberately non-sorted order.
	for _, dir := range []string{"zeta", "alpha", "mid/beta", "mid/alpha", "beta"} {
		dcConfig(t, root, dir)
	}

	units, _, err := DiscoverUnits(root, nil)
	if err != nil {
		t.Fatalf("DiscoverUnits: %v", err)
	}
	want := []string{"alpha", "beta", "mid/alpha", "mid/beta", "zeta"}
	if got := unitRels(units); !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want lexicographic %v", got, want)
	}
}

// TestDiscoverUnitsSkipList verifies each skip-listed dir name (and a
// dot-prefixed dir) prunes a depdog.yaml buried inside its subtree.
func TestDiscoverUnitsSkipList(t *testing.T) {
	skipped := []string{
		"node_modules", "vendor", "testdata", "target",
		"dist", "build", "out", "__pycache__", ".hidden", ".git",
	}
	for _, name := range skipped {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			dcConfig(t, root, "real")                      // a legit unit
			dcConfig(t, root, filepath.ToSlash(name)+"/x") // buried decoy under the skipped dir

			units, _, err := DiscoverUnits(root, nil)
			if err != nil {
				t.Fatalf("DiscoverUnits: %v", err)
			}
			if got := unitRels(units); !reflect.DeepEqual(got, []string{"real"}) {
				t.Errorf("with skipped %q: units = %v, want [real]", name, got)
			}
		})
	}
}
