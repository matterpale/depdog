package python

import (
	"reflect"
	"testing"
)

// refStrings renders scanned refs to a comparable, source-order slice of
// "<dots><module>@<line>" so table cases stay readable.
func refStrings(refs []importRef) []string {
	if len(refs) == 0 {
		return nil
	}
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		dots := ""
		for i := 0; i < r.Level; i++ {
			dots += "."
		}
		out = append(out, dots+r.Module)
	}
	return out
}

func TestScanImportForms(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "plain import",
			src:  "import os\n",
			want: []string{"os"},
		},
		{
			name: "dotted import",
			src:  "import a.b.c\n",
			want: []string{"a.b.c"},
		},
		{
			name: "import as alias",
			src:  "import numpy as np\n",
			want: []string{"numpy"},
		},
		{
			name: "import dotted as alias",
			src:  "import a.b as c\n",
			want: []string{"a.b"},
		},
		{
			name: "comma-separated imports",
			src:  "import os, sys, a.b\n",
			want: []string{"os", "sys", "a.b"},
		},
		{
			name: "from absolute import",
			src:  "from a.b import x\n",
			want: []string{"a.b"},
		},
		{
			name: "from absolute import multiple",
			src:  "from a.b import x, y, z\n",
			want: []string{"a.b"},
		},
		{
			name: "from relative single dot",
			src:  "from . import x\n",
			want: []string{"."},
		},
		{
			name: "from relative dotted",
			src:  "from ..pkg import y\n",
			want: []string{"..pkg"},
		},
		{
			name: "from relative deep",
			src:  "from ...a.b import z\n",
			want: []string{"...a.b"},
		},
		{
			name: "from with parenthesised list",
			src:  "from a.b import (\n    x,\n    y,\n)\n",
			want: []string{"a.b"},
		},
		{
			name: "from with backslash continuation",
			src:  "from a.b import x, \\\n    y\n",
			want: []string{"a.b"},
		},
		{
			name: "import with trailing comment",
			src:  "import os  # the platform module\n",
			want: []string{"os"},
		},
		{
			name: "star import",
			src:  "from a.b import *\n",
			want: []string{"a.b"},
		},
		{
			name: "several statements",
			src:  "import os\nfrom a import b\nimport sys\n",
			want: []string{"os", "a", "sys"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := refStrings(scan([]byte(tc.src)))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("scan(%q) = %v, want %v", tc.src, got, tc.want)
			}
		})
	}
}

func TestScanIgnoresCommentsAndStrings(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "line comment",
			src:  "# import fake\n",
			want: nil,
		},
		{
			name: "trailing comment does not eat next line",
			src:  "x = 1  # import nope\nimport real\n",
			want: []string{"real"},
		},
		{
			name: "import text inside a single-quoted string",
			src:  "s = 'import fake'\n",
			want: nil,
		},
		{
			name: "import text inside a double-quoted string",
			src:  "s = \"from fake import x\"\n",
			want: nil,
		},
		{
			name: "triple-quoted docstring with import text",
			src:  "\"\"\"\nimport fake\nfrom fake import x\n\"\"\"\nimport real\n",
			want: []string{"real"},
		},
		{
			name: "single-quote triple docstring",
			src:  "'''from a import b'''\nimport real\n",
			want: []string{"real"},
		},
		{
			name: "import keyword only recognised at line start",
			src:  "result = do_import()\nimport real\n",
			want: []string{"real"},
		},
		{
			name: "identifier prefixed with import is not a keyword",
			src:  "importlib_metadata = 1\n",
			want: nil,
		},
		{
			name: "escaped quote inside string",
			src:  "s = \"he said \\\"import x\\\"\"\n",
			want: nil,
		},
		{
			name: "indented import inside a block",
			src:  "def f():\n    import os\n    return os\n",
			want: []string{"os"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := refStrings(scan([]byte(tc.src)))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("scan(%q) = %v, want %v", tc.src, got, tc.want)
			}
		})
	}
}

func TestScanLineNumbers(t *testing.T) {
	src := "import os\n\nfrom a.b import x\n\"\"\"\nmulti\nline\n\"\"\"\nimport sys\n"
	refs := scan([]byte(src))
	if len(refs) != 3 {
		t.Fatalf("want 3 refs, got %d: %v", len(refs), refStrings(refs))
	}
	wantLines := []int{1, 3, 8}
	for i, r := range refs {
		if r.Line != wantLines[i] {
			t.Errorf("ref %d (%q) line = %d, want %d", i, r.Module, r.Line, wantLines[i])
		}
	}
}
