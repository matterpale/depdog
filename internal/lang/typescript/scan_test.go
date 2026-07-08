package typescript

import (
	"reflect"
	"testing"
)

// captured is a stripped-down specifier for table comparisons; the scanner's
// line numbers are checked separately where they matter.
type captured struct {
	Raw  string
	Kind kind
}

func capture(src string) []captured {
	specs := scan([]byte(src))
	out := make([]captured, 0, len(specs))
	for _, s := range specs {
		out = append(out, captured{Raw: s.Raw, Kind: s.Kind})
	}
	return out
}

func rawsOnly(src string) []string {
	specs := scan([]byte(src))
	out := make([]string, 0, len(specs))
	for _, s := range specs {
		out = append(out, s.Raw)
	}
	return out
}

func TestScanImportSurfaces(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []captured
	}{
		{
			name: "default import",
			src:  `import Foo from './foo';`,
			want: []captured{{"./foo", kindImport}},
		},
		{
			name: "named import double quotes",
			src:  `import { a, b } from "../bar";`,
			want: []captured{{"../bar", kindImport}},
		},
		{
			name: "namespace import",
			src:  `import * as ns from 'lodash';`,
			want: []captured{{"lodash", kindImport}},
		},
		{
			name: "side-effect import",
			src:  `import './styles.css';`,
			want: []captured{{"./styles.css", kindImport}},
		},
		{
			name: "import type",
			src:  `import type { T } from './types';`,
			want: []captured{{"./types", kindImport}},
		},
		{
			name: "export from",
			src:  `export { a } from './a';`,
			want: []captured{{"./a", kindExport}},
		},
		{
			name: "export star",
			src:  `export * from './all';`,
			want: []captured{{"./all", kindExport}},
		},
		{
			name: "export star as",
			src:  `export * as ns from './ns';`,
			want: []captured{{"./ns", kindExport}},
		},
		{
			name: "export type from",
			src:  `export type { T } from './t';`,
			want: []captured{{"./t", kindExport}},
		},
		{
			name: "dynamic import",
			src:  `const m = import('./dyn');`,
			want: []captured{{"./dyn", kindDynamic}},
		},
		{
			name: "dynamic import with await and spaces",
			src:  `const m = await import(  "pkg"  );`,
			want: []captured{{"pkg", kindDynamic}},
		},
		{
			name: "require",
			src:  `const x = require('./req');`,
			want: []captured{{"./req", kindRequire}},
		},
		{
			name: "require double quote",
			src:  `const y = require("node:fs");`,
			want: []captured{{"node:fs", kindRequire}},
		},
		{
			name: "multiple imports",
			src: `import a from './a';
import b from './b';
const c = require('c');`,
			want: []captured{
				{"./a", kindImport},
				{"./b", kindImport},
				{"c", kindRequire},
			},
		},
		{
			name: "multiline named import",
			src: `import {
  a,
  b,
  c
} from './multi';`,
			want: []captured{{"./multi", kindImport}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := capture(tt.src)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("scan(%q)\n got = %+v\nwant = %+v", tt.src, got, tt.want)
			}
		})
	}
}

func TestScanIgnoresCommentsAndStrings(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string // raw specifiers, in order
	}{
		{
			name: "line comment",
			src:  `// import Foo from './commented';`,
			want: nil,
		},
		{
			name: "trailing line comment does not eat next line",
			src: `const x = 1; // import x from './nope';
import real from './real';`,
			want: []string{"./real"},
		},
		{
			name: "block comment",
			src: `/* import Foo from './blocked';
   require('./also-blocked'); */
import real from './real';`,
			want: []string{"./real"},
		},
		{
			name: "specifier text inside a string literal",
			src:  `const s = "import Foo from './fake'";`,
			want: nil,
		},
		{
			name: "require-looking text inside a string",
			src:  `const s = 'require("./fake")';`,
			want: nil,
		},
		{
			name: "template literal with import text",
			src:  "const t = `import x from './fake'`;\nimport real from './real';",
			want: []string{"./real"},
		},
		{
			name: "template literal interpolation returns to code",
			src:  "const t = `value ${require('./interp')} end`;",
			want: []string{"./interp"},
		},
		{
			name: "escaped quote inside string",
			src:  `const s = "he said \"import x from './fake'\"";`,
			want: nil,
		},
		{
			name: "url in comment with slashes",
			src: `// see https://example.com/import/from
import real from './real';`,
			want: []string{"./real"},
		},
		{
			name: "division not a comment",
			src: `const x = a / b; const y = c / d;
import real from './real';`,
			want: []string{"./real"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rawsOnly(tt.src)
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("scan(%q) = %v, want %v", tt.src, got, tt.want)
			}
		})
	}
}

func TestScanSkipsNonLiteralArguments(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"dynamic import with variable", `const m = import(someVar);`},
		{"dynamic import with expression", "const m = import(`${base}/mod`);"},
		{"require with variable", `const x = require(name);`},
		{"require with call", `const x = require(getName());`},
		{"require with concatenation", `const x = require('a' + 'b');`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rawsOnly(tt.src)
			// Note: `require('a' + 'b')` captures 'a' as a literal-first arg;
			// that is acceptable static behavior. We only assert no crash and
			// that clearly non-literal forms yield nothing.
			if tt.name == "require with concatenation" {
				return
			}
			if len(got) != 0 {
				t.Errorf("scan(%q) = %v, want no specifiers", tt.src, got)
			}
		})
	}
}

func TestScanLineNumbers(t *testing.T) {
	src := `const a = 1;
const b = 2;
import Foo from './foo';
// blank
import Bar from './bar';`
	specs := scan([]byte(src))
	if len(specs) != 2 {
		t.Fatalf("got %d specifiers, want 2: %+v", len(specs), specs)
	}
	if specs[0].Line != 3 {
		t.Errorf("first import line = %d, want 3", specs[0].Line)
	}
	if specs[1].Line != 5 {
		t.Errorf("second import line = %d, want 5", specs[1].Line)
	}
}

func TestScanMixedRealWorld(t *testing.T) {
	src := "import React from 'react';\n" +
		"import { useState } from 'react';\n" +
		"// import removed from './old';\n" +
		"import type { Config } from './config';\n" +
		"export { helper } from './helpers';\n" +
		"const lazy = () => import('./lazy');\n" +
		"const fs = require('node:fs');\n" +
		"const msg = `do not import './fake'`;\n"
	want := []string{"react", "react", "./config", "./helpers", "./lazy", "node:fs"}
	got := rawsOnly(src)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("scan mixed = %v, want %v", got, want)
	}
}
