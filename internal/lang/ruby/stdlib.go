package ruby

// stdlibModules is a curated set of Ruby standard-library / always-available
// feature names as they appear in a `require` argument. Classification is keyed
// on the first slash-separated segment, so a nested feature like `net/http`
// classifies on its head (`net`) while flat features like `json` match
// directly; the full nested names are also listed so a lone `net` and a
// `net/http` both resolve to std. A `require` whose argument is in this set (or
// whose head segment is) is std; anything else that is neither a std feature
// nor an on-disk file under the project root is external (a gem).
//
// The set is fixed rather than probed from a live Ruby so the scanner stays a
// pure-Go, runtime-free static analysis.
var stdlibModules = map[string]bool{
	// Flat, always-shipped features.
	"abbrev": true, "base64": true, "benchmark": true, "bigdecimal": true,
	"cgi": true, "coverage": true, "csv": true, "date": true, "delegate": true,
	"digest": true, "drb": true, "english": true, "erb": true, "etc": true,
	"expect": true, "fcntl": true, "fiddle": true, "fileutils": true,
	"find": true, "forwardable": true, "getoptlong": true, "io": true,
	"ipaddr": true, "irb": true, "json": true, "logger": true, "matrix": true,
	"mkmf": true, "monitor": true, "mutex_m": true, "objspace": true,
	"observer": true, "open3": true, "openssl": true, "optparse": true,
	"ostruct": true, "pathname": true, "pp": true, "prettyprint": true,
	"prime": true, "pstore": true, "psych": true, "pty": true, "racc": true,
	"random": true, "readline": true, "resolv": true, "ripper": true,
	"rss": true, "securerandom": true, "set": true, "shellwords": true,
	"singleton": true, "socket": true, "stringio": true, "strscan": true,
	"syslog": true, "tempfile": true, "time": true, "timeout": true,
	"tmpdir": true, "tracer": true, "tsort": true, "un": true, "uri": true,
	"weakref": true, "yaml": true, "zlib": true,

	// Nested features whose head is not itself a standalone requirable name;
	// listing both the head and the full path keeps `require "net"` and
	// `require "net/http"` classified as std.
	"net": true, "net/http": true, "net/https": true, "net/ftp": true,
	"net/imap": true, "net/pop": true, "net/smtp": true, "net/telnet": true,
	"rexml": true, "rexml/document": true,
	"rinda": true, "rinda/tuplespace": true,
	"rubygems": true,
}

// isStdlib reports whether a require argument (a feature name like "json" or
// "net/http") refers to the Ruby standard library. It matches the full feature
// name first, then falls back to the head segment before the first slash so a
// deep feature under a std namespace still classifies as std.
func isStdlib(feature string) bool {
	if stdlibModules[feature] {
		return true
	}
	head := feature
	if i := indexByte(feature, '/'); i >= 0 {
		head = feature[:i]
	}
	return stdlibModules[head]
}

// indexByte is a tiny local helper to avoid importing strings for one call in a
// file that otherwise needs no imports.
func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
