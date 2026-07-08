package typescript

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// tsconfig holds the subset of tsconfig.json that affects module resolution:
// baseUrl and the single-level path aliases. It is intentionally minimal — v1
// supports baseUrl + paths + a single level of `extends`.
type tsconfig struct {
	BaseURL string
	Paths   map[string][]string
}

// rawTSConfig mirrors the on-disk shape we read.
type rawTSConfig struct {
	Extends         string `json:"extends"`
	CompilerOptions struct {
		BaseURL string              `json:"baseUrl"`
		Paths   map[string][]string `json:"paths"`
	} `json:"compilerOptions"`
}

// loadTSConfig reads tsconfig.json at root, following one level of `extends`.
// A missing tsconfig.json yields an empty (non-nil) config, not an error. The
// reader is JSONC-tolerant: it strips // and /* */ comments and trailing
// commas before decoding.
func loadTSConfig(root string) (*tsconfig, error) {
	cfg := &tsconfig{Paths: map[string][]string{}}

	main, err := readRawTSConfig(filepath.Join(root, "tsconfig.json"))
	if err != nil {
		return nil, err
	}
	if main == nil {
		return cfg, nil // no tsconfig.json: empty config
	}

	// Follow a single level of extends first, so the child's values win.
	if main.Extends != "" {
		baseDir := root
		extPath := main.Extends
		if !strings.HasSuffix(extPath, ".json") {
			extPath += ".json"
		}
		if !filepath.IsAbs(extPath) {
			extPath = filepath.Join(baseDir, filepath.FromSlash(extPath))
		}
		if base, err := readRawTSConfig(extPath); err == nil && base != nil {
			applyRaw(cfg, base)
		}
	}
	applyRaw(cfg, main)
	return cfg, nil
}

// applyRaw merges a raw config into cfg. Non-empty baseUrl overrides; path
// aliases merge (later calls win on key collision).
func applyRaw(cfg *tsconfig, raw *rawTSConfig) {
	if raw.CompilerOptions.BaseURL != "" {
		cfg.BaseURL = raw.CompilerOptions.BaseURL
	}
	for k, v := range raw.CompilerOptions.Paths {
		cfg.Paths[k] = v
	}
}

// readRawTSConfig reads and JSONC-decodes a tsconfig file. Returns (nil, nil)
// when the file does not exist.
func readRawTSConfig(path string) (*rawTSConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	clean := stripJSONC(data)
	var raw rawTSConfig
	if err := json.Unmarshal(clean, &raw); err != nil {
		// Tolerate an unparseable config rather than crash the whole load;
		// degrade to no aliases. The caller treats missing config as empty.
		return &rawTSConfig{}, nil
	}
	return &raw, nil
}

// stripJSONC removes // line comments, /* */ block comments, and trailing
// commas from JSON-with-comments, while respecting string literals so a "//"
// inside a string is preserved.
func stripJSONC(data []byte) []byte {
	var out []byte
	inString := false
	for i := 0; i < len(data); i++ {
		c := data[i]
		if inString {
			out = append(out, c)
			if c == '\\' && i+1 < len(data) {
				out = append(out, data[i+1])
				i++
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		switch {
		case c == '"':
			inString = true
			out = append(out, c)
		case c == '/' && i+1 < len(data) && data[i+1] == '/':
			for i < len(data) && data[i] != '\n' {
				i++
			}
			if i < len(data) {
				out = append(out, '\n')
			}
		case c == '/' && i+1 < len(data) && data[i+1] == '*':
			i += 2
			for i+1 < len(data) && !(data[i] == '*' && data[i+1] == '/') {
				i++
			}
			i++ // land on the '/'
		default:
			out = append(out, c)
		}
	}
	return removeTrailingCommas(out)
}

// removeTrailingCommas deletes commas that immediately precede a closing } or
// ] (ignoring whitespace), which JSON forbids but JSONC allows.
func removeTrailingCommas(data []byte) []byte {
	out := make([]byte, 0, len(data))
	inString := false
	for i := 0; i < len(data); i++ {
		c := data[i]
		if inString {
			out = append(out, c)
			if c == '\\' && i+1 < len(data) {
				out = append(out, data[i+1])
				i++
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			out = append(out, c)
			continue
		}
		if c == ',' {
			j := i + 1
			for j < len(data) && (data[j] == ' ' || data[j] == '\t' || data[j] == '\r' || data[j] == '\n') {
				j++
			}
			if j < len(data) && (data[j] == '}' || data[j] == ']') {
				continue // drop the trailing comma
			}
		}
		out = append(out, c)
	}
	return out
}

// packageName returns the package.json "name" at root, falling back to the
// directory basename when there is no package.json or it has no name.
func packageName(root string) string {
	fallback := filepath.Base(root)
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return fallback
	}
	var pkg struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(stripJSONC(data), &pkg); err != nil || pkg.Name == "" {
		return fallback
	}
	return pkg.Name
}
