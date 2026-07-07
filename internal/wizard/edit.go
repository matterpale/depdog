package wizard

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/matterpale/depdog/internal/core"
)

// nameRE is the charset Marshal can render as a bare YAML key and rule ref
// without quoting; the edit prompt enforces it so generated files stay valid.
var nameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ValidateName reports whether name is usable as a component name in a
// generated depdog.yaml: non-empty, not one of the reserved rule keywords, and
// made of characters Marshal can emit unquoted. Errors tell the user the fix.
func ValidateName(name string) error {
	if name == "" {
		return errors.New(`component name is empty — type a name like "domain"`)
	}
	if isSpecial(name) {
		return fmt.Errorf("%q is reserved (std, external, unassigned and * have special meaning in rules) — pick another name", name)
	}
	if !nameRE.MatchString(name) {
		return fmt.Errorf("component name %q must start with a letter or digit and use only letters, digits, '.', '_' and '-'", name)
	}
	return nil
}

// ValidateRename reports whether newName is acceptable as the new name of
// component oldName: it must pass ValidateName and not collide with any other
// component. Renaming a component to its current name is fine (a no-op).
func (c Config) ValidateRename(oldName, newName string) error {
	if err := ValidateName(newName); err != nil {
		return err
	}
	for _, comp := range c.Components {
		if comp.Name == newName && comp.Name != oldName {
			return fmt.Errorf("a component named %q already exists — pick another name", newName)
		}
	}
	return nil
}

// Rename returns a copy of the Config with component oldName renamed to
// newName and every rule ref to it rewritten, so the result still round-trips
// through config.Parse. The receiver is left untouched.
func (c Config) Rename(oldName, newName string) (Config, error) {
	if _, ok := c.Component(oldName); !ok {
		return c, fmt.Errorf("no component named %q", oldName)
	}
	if err := c.ValidateRename(oldName, newName); err != nil {
		return c, err
	}
	if oldName == newName {
		return c, nil
	}
	out := c
	out.Components = make([]Component, len(c.Components))
	for i, comp := range c.Components {
		if comp.Name == oldName {
			comp.Name = newName
		}
		comp.Allow = renameRef(comp.Allow, oldName, newName)
		comp.Deny = renameRef(comp.Deny, oldName, newName)
		out.Components[i] = comp
	}
	return out, nil
}

// SetPatterns returns a copy of the Config with the component's patterns
// replaced. Every pattern must pass the engine's validation
// (core.ValidatePattern) so the result still round-trips through config.Parse.
func (c Config) SetPatterns(name string, patterns []string) (Config, error) {
	if len(patterns) == 0 {
		return c, fmt.Errorf(`component %q needs at least one pattern, e.g. "internal/%s/**"`, name, name)
	}
	for _, p := range patterns {
		if err := core.ValidatePattern(p); err != nil {
			return c, err
		}
	}
	out := c
	out.Components = make([]Component, len(c.Components))
	copy(out.Components, c.Components)
	for i := range out.Components {
		if out.Components[i].Name == name {
			out.Components[i].Patterns = append([]string(nil), patterns...)
			return out, nil
		}
	}
	return c, fmt.Errorf("no component named %q", name)
}

// ParsePatterns parses a comma-separated pattern list as typed into the edit
// prompt, trimming whitespace and validating each pattern. Errors are
// human-actionable so huh can show them inline.
func ParsePatterns(s string) ([]string, error) {
	var out []string
	for _, part := range strings.Split(s, ",") {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		if err := core.ValidatePattern(p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, errors.New(`enter at least one pattern, e.g. "internal/api/**" (comma-separated)`)
	}
	return out, nil
}

// Component looks up a component by name.
func (c Config) Component(name string) (Component, bool) {
	for _, comp := range c.Components {
		if comp.Name == name {
			return comp, true
		}
	}
	return Component{}, false
}

// renameRef rewrites refs equal to oldName to newName, copying only on change.
func renameRef(refs []string, oldName, newName string) []string {
	for i, r := range refs {
		if r != oldName {
			continue
		}
		out := append([]string(nil), refs...)
		for j := i; j < len(out); j++ {
			if out[j] == oldName {
				out[j] = newName
			}
		}
		return out
	}
	return refs
}
