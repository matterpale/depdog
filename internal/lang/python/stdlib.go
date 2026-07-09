package python

// stdlibModules is a curated set of Python standard-library top-level module
// names. Classification is keyed on the first dotted segment: an absolute
// import whose head is in this set is std (e.g. `import os.path` -> os -> std).
// It intentionally covers the common CPython 3.x standard library plus a few
// always-available builtins; anything not in it and not resolvable under the
// project root is external (a third-party / site-packages dependency). The set
// is fixed rather than probed from a live interpreter so the scanner stays a
// pure-Go, runtime-free static analysis.
var stdlibModules = map[string]bool{
	"__future__": true, "_thread": true, "abc": true, "aifc": true,
	"argparse": true, "array": true, "ast": true, "asyncio": true,
	"atexit": true, "base64": true, "bdb": true, "binascii": true,
	"bisect": true, "builtins": true, "bz2": true, "calendar": true,
	"cgi": true, "cgitb": true, "chunk": true, "cmath": true,
	"cmd": true, "code": true, "codecs": true, "codeop": true,
	"collections": true, "colorsys": true, "compileall": true,
	"concurrent": true, "configparser": true, "contextlib": true,
	"contextvars": true, "copy": true, "copyreg": true, "cProfile": true,
	"crypt": true, "csv": true, "ctypes": true, "curses": true,
	"dataclasses": true, "datetime": true, "dbm": true, "decimal": true,
	"difflib": true, "dis": true, "doctest": true, "email": true,
	"encodings": true, "ensurepip": true, "enum": true, "errno": true,
	"faulthandler": true, "fcntl": true, "filecmp": true, "fileinput": true,
	"fnmatch": true, "fractions": true, "ftplib": true, "functools": true,
	"gc": true, "getopt": true, "getpass": true, "gettext": true,
	"glob": true, "graphlib": true, "grp": true, "gzip": true,
	"hashlib": true, "heapq": true, "hmac": true, "html": true,
	"http": true, "imaplib": true, "imghdr": true, "importlib": true,
	"inspect": true, "io": true, "ipaddress": true, "itertools": true,
	"json": true, "keyword": true, "lib2to3": true, "linecache": true,
	"locale": true, "logging": true, "lzma": true, "mailbox": true,
	"mailcap": true, "marshal": true, "math": true, "mimetypes": true,
	"mmap": true, "modulefinder": true, "msilib": true, "msvcrt": true,
	"multiprocessing": true, "netrc": true, "nntplib": true, "numbers": true,
	"operator": true, "os": true, "ossaudiodev": true, "pathlib": true,
	"pdb": true, "pickle": true, "pickletools": true, "pipes": true,
	"pkgutil": true, "platform": true, "plistlib": true, "poplib": true,
	"posix": true, "posixpath": true, "pprint": true, "profile": true,
	"pstats": true, "pty": true, "pwd": true, "py_compile": true,
	"pyclbr": true, "pydoc": true, "queue": true, "quopri": true,
	"random": true, "re": true, "readline": true, "reprlib": true,
	"resource": true, "rlcompleter": true, "runpy": true, "sched": true,
	"secrets": true, "select": true, "selectors": true, "shelve": true,
	"shlex": true, "shutil": true, "signal": true, "site": true,
	"smtplib": true, "sndhdr": true, "socket": true, "socketserver": true,
	"spwd": true, "sqlite3": true, "ssl": true, "stat": true,
	"statistics": true, "string": true, "stringprep": true, "struct": true,
	"subprocess": true, "sunau": true, "symtable": true, "sys": true,
	"sysconfig": true, "syslog": true, "tabnanny": true, "tarfile": true,
	"telnetlib": true, "tempfile": true, "termios": true, "test": true,
	"textwrap": true, "threading": true, "time": true, "timeit": true,
	"tkinter": true, "token": true, "tokenize": true, "tomllib": true,
	"trace": true, "traceback": true, "tracemalloc": true, "tty": true,
	"turtle": true, "turtledemo": true, "types": true, "typing": true,
	"unicodedata": true, "unittest": true, "urllib": true, "uu": true,
	"uuid": true, "venv": true, "warnings": true, "wave": true,
	"weakref": true, "webbrowser": true, "winreg": true, "winsound": true,
	"wsgiref": true, "xdrlib": true, "xml": true, "xmlrpc": true,
	"zipapp": true, "zipfile": true, "zipimport": true, "zlib": true,
	"zoneinfo": true,
}

// isStdlib reports whether the head segment of a dotted module name refers to
// the Python standard library.
func isStdlib(module string) bool {
	head := module
	if i := indexByte(module, '.'); i >= 0 {
		head = module[:i]
	}
	return stdlibModules[head]
}

// indexByte is a tiny local helper to avoid importing strings for one call in
// a file that otherwise needs no imports.
func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
