package rust

import (
	"reflect"
	"testing"
)

// refStrings renders scanned refs to a comparable, source-order slice of the
// path so table cases stay readable. The synthetic modToken head produced by a
// `mod name;` declaration is rendered as "mod:<name>".
func refStrings(refs []importRef) []string {
	if len(refs) == 0 {
		return nil
	}
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		p := r.Path
		if len(p) >= len(modToken) && p[:len(modToken)] == modToken {
			p = "mod:" + p[len(modToken)+2:]
		}
		out = append(out, p)
	}
	return out
}

func TestScanUseForms(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"crate path", "use crate::domain::order;\n", []string{"crate::domain::order"}},
		{"self path", "use self::helper;\n", []string{"self::helper"}},
		{"super path", "use super::config;\n", []string{"super::config"}},
		{"external crate", "use serde::Deserialize;\n", []string{"serde::Deserialize"}},
		{"std path", "use std::collections::HashMap;\n", []string{"std::collections::HashMap"}},
		{"core path", "use core::mem;\n", []string{"core::mem"}},
		{"pub use", "pub use crate::domain::Order;\n", []string{"crate::domain::Order"}},
		{"pub(crate) use", "pub(crate) use crate::service::run;\n", []string{"crate::service::run"}},
		{"use as rename", "use crate::domain::Order as O;\n", []string{"crate::domain::Order"}},
		{"glob", "use crate::domain::*;\n", []string{"crate::domain"}},
		{
			name: "grouped",
			src:  "use crate::domain::{Order, Customer};\n",
			want: []string{"crate::domain::Order", "crate::domain::Customer"},
		},
		{
			name: "grouped nested",
			src:  "use crate::a::{b::c, d::{e, f}};\n",
			want: []string{"crate::a::b::c", "crate::a::d::e", "crate::a::d::f"},
		},
		{
			name: "grouped with self",
			src:  "use crate::domain::{self, order::Order};\n",
			want: []string{"crate::domain::self", "crate::domain::order::Order"},
		},
		{
			name: "multiline use",
			src:  "use crate::domain::{\n    Order,\n    Customer,\n};\n",
			want: []string{"crate::domain::Order", "crate::domain::Customer"},
		},
		{
			name: "several use statements",
			src:  "use std::io;\nuse crate::domain::Order;\nuse serde::Serialize;\n",
			want: []string{"std::io", "crate::domain::Order", "serde::Serialize"},
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

func TestScanModAndExtern(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"mod decl", "mod domain;\n", []string{"mod:domain"}},
		{"pub mod decl", "pub mod service;\n", []string{"mod:service"}},
		{"inline mod is not an edge", "mod inline {\n    use std::io;\n}\n", []string{"std::io"}},
		{"extern crate", "extern crate serde;\n", []string{"serde"}},
		{"extern C block is not an edge", "extern \"C\" {\n    fn f();\n}\n", nil},
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
		{"line comment", "// use fake::thing;\n", nil},
		{"trailing comment", "let x = 1; // use nope::x;\nuse real::thing;\n", []string{"real::thing"}},
		{"block comment", "/* use fake::a;\n use fake::b; */\nuse real::c;\n", []string{"real::c"}},
		{"nested block comment", "/* outer /* use fake::a; */ still */\nuse real::c;\n", []string{"real::c"}},
		{"import text inside string", "let s = \"use fake::thing;\";\n", nil},
		{"raw string with hashes", "let s = r#\"use fake::thing;\"#;\nuse real::x;\n", []string{"real::x"}},
		{"escaped quote in string", "let s = \"he said \\\"use x::y;\\\"\";\nuse real::z;\n", []string{"real::z"}},
		{"char literal brace", "let c = '}';\nuse real::a;\n", []string{"real::a"}},
		{"lifetime not a char", "fn f<'a>(x: &'a str) {}\nuse real::b;\n", []string{"real::b"}},
		{"use only at item start", "let use_count = call();\nuse real::c;\n", []string{"real::c"}},
		{"identifier prefixed with use", "let useful = 1;\n", nil},
		{"byte string", "let b = b\"use fake::x;\";\nuse real::d;\n", []string{"real::d"}},
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
	src := "use std::io;\n\nuse crate::domain::Order;\n/*\nblock\n*/\nuse serde::Serialize;\n"
	refs := scan([]byte(src))
	if len(refs) != 3 {
		t.Fatalf("want 3 refs, got %d: %v", len(refs), refStrings(refs))
	}
	wantLines := []int{1, 3, 7}
	for i, r := range refs {
		if r.Line != wantLines[i] {
			t.Errorf("ref %d (%q) line = %d, want %d", i, r.Path, r.Line, wantLines[i])
		}
	}
}
