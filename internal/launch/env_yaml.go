package launch

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
)

type LookupFunc func(string) (string, bool)

type MissingEnvError struct {
	Missing []string
}

func (e *MissingEnvError) Error() string {
	cp := append([]string(nil), e.Missing...)
	sort.Strings(cp)
	return "missing required environment variables:\n  - " + strings.Join(cp, "\n  - ")
}

// RenderEnvYAML performs $env substitution first, then interpolates any _-prefixed keys.
// Returns error if required env or interpolated config fields are missing.
func RenderEnvYAML(in []byte, lookup LookupFunc) ([]byte, error) {
	var v any
	if err := yaml.Unmarshal(in, &v); err != nil {
		return nil, err
	}

	var missing []string
	v = renderEnv(v, "", &missing, lookup)
	if len(missing) > 0 {
		return nil, &MissingEnvError{Missing: missing}
	}

	// Interpolate _-prefixed keys
	v, err := renderInterpolation(v, v, lookup)
	if err != nil {
		return nil, err
	}

	// Strip leading underscores from keys
	v = stripUnderscoreKeys(v)

	out, err := yaml.Marshal(v)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func renderEnv(v any, path string, missing *[]string, lookup LookupFunc) any {
	switch t := v.(type) {
	case yaml.MapSlice:
		for i := range t {
			keyStr := fmt.Sprint(t[i].Key)
			childPath := keyStr
			if path != "" {
				childPath = path + "." + keyStr
			}
			t[i].Value = renderEnv(t[i].Value, childPath, missing, lookup)
		}
		return t

	case map[string]any:
		for k, vv := range t {
			childPath := k
			if path != "" {
				childPath = path + "." + k
			}
			t[k] = renderEnv(vv, childPath, missing, lookup)
		}
		return t

	case map[any]any:
		for k, vv := range t {
			keyStr := fmt.Sprint(k)
			childPath := keyStr
			if path != "" {
				childPath = path + "." + keyStr
			}
			t[k] = renderEnv(vv, childPath, missing, lookup)
		}
		return t

	case []any:
		for i := range t {
			childPath := fmt.Sprintf("%s[%d]", path, i)
			t[i] = renderEnv(t[i], childPath, missing, lookup)
		}
		return t

	case string:
		ok, varName, def := parseEnvRef(t)
		if !ok {
			return t
		}
		val, found := lookup(varName)
		if !found {
			if def != nil {
				return coerceScalar(*def)
			}
			*missing = append(*missing, fmt.Sprintf("%s -> %s", path, varName))
			return t
		}
		return coerceScalar(val)

	default:
		return v
	}
}

func renderInterpolation(node, root any, lookup LookupFunc) (any, error) {
	switch t := node.(type) {
	case yaml.MapSlice:
		for i := range t {
			keyStr := fmt.Sprint(t[i].Key)
			if strings.HasPrefix(keyStr, "_") {
				if s, ok := t[i].Value.(string); ok {
					s2, err := interpolateString(s, root, lookup)
					if err != nil {
						return nil, fmt.Errorf("interpolation error for key %s: %w", keyStr, err)
					}
					t[i].Value = s2
				}
			}
			var err error
			t[i].Value, err = renderInterpolation(t[i].Value, root, lookup)
			if err != nil {
				return nil, err
			}
		}
		return t, nil
	case map[string]any:
		for k, v := range t {
			if strings.HasPrefix(k, "_") {
				if s, ok := v.(string); ok {
					s2, err := interpolateString(s, root, lookup)
					if err != nil {
						return nil, fmt.Errorf("interpolation error for key %s: %w", k, err)
					}
					t[k] = s2
				}
			}
			var err error
			t[k], err = renderInterpolation(t[k], root, lookup)
			if err != nil {
				return nil, err
			}
		}
		return t, nil
	case map[any]any:
		for k, v := range t {
			ks := fmt.Sprint(k)
			if strings.HasPrefix(ks, "_") {
				if s, ok := v.(string); ok {
					s2, err := interpolateString(s, root, lookup)
					if err != nil {
						return nil, fmt.Errorf("interpolation error for key %s: %w", ks, err)
					}
					t[k] = s2
				}
			}
			var err error
			t[k], err = renderInterpolation(t[k], root, lookup)
			if err != nil {
				return nil, err
			}
		}
		return t, nil
	case []any:
		for i := range t {
			var err error
			t[i], err = renderInterpolation(t[i], root, lookup)
			if err != nil {
				return nil, err
			}
		}
		return t, nil
	default:
		return node, nil
	}
}

var interpolationRegex = regexp.MustCompile(`\{\{\s*([^{}]+)\s*\}\}`)

func interpolateString(s string, root any, lookup LookupFunc) (string, error) {
	var errs []error
	replacedStr := interpolationRegex.ReplaceAllStringFunc(s, func(match string) string {
		content := interpolationRegex.FindStringSubmatch(match)[1]
		content = strings.TrimSpace(content)

		if strings.HasPrefix(content, "env:") {
			envVar := strings.TrimSpace(strings.TrimPrefix(content, "env:"))
			val, ok := lookup(envVar)
			if !ok || val == "" {
				errs = append(errs, fmt.Errorf("missing environment variable for interpolation: %s", envVar))
				return ""
			}
			return coerceToString(val)
		}

		val, err := lookupPath(root, content)
		if err != nil || val == "" {
			errs = append(errs, fmt.Errorf("missing config path for interpolation: %s", content))
			return ""
		}
		return coerceToString(val)
	})
	return replacedStr, errors.Join(errs...)
}

func stripUnderscoreKeys(node any) any {
	switch t := node.(type) {
	case yaml.MapSlice:
		for i := range t {
			keyStr := fmt.Sprint(t[i].Key)
			t[i].Value = stripUnderscoreKeys(t[i].Value)
			if strings.HasPrefix(keyStr, "_") {
				t[i].Key = keyStr[1:]
			}
		}
		return t
	case map[string]any:
		for k, v := range t {
			t[k] = stripUnderscoreKeys(v)
			if strings.HasPrefix(k, "_") {
				newK := k[1:]
				t[newK] = t[k]
				delete(t, k)
			}
		}
		return t
	case map[any]any:
		for k, v := range t {
			t[k] = stripUnderscoreKeys(v)
			ks := fmt.Sprint(k)
			if strings.HasPrefix(ks, "_") {
				newK := ks[1:]
				t[newK] = t[k]
				delete(t, k)
			}
		}
		return t
	case []any:
		for i := range t {
			t[i] = stripUnderscoreKeys(t[i])
		}
		return t
	default:
		return node
	}
}

func coerceToString(val any) string {
	if val == nil {
		return ""
	}
	switch v := val.(type) {
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	case int, int8, int16, int32, int64:
		return fmt.Sprintf("%d", v)
	case float32, float64:
		return fmt.Sprintf("%v", v)
	default:
		return fmt.Sprint(v)
	}
}

func lookupPath(node any, path string) (any, error) {
	parts := strings.Split(path, ".")
	curr := node

	for _, part := range parts {
		switch t := curr.(type) {
		case yaml.MapSlice:
			found := false
			for _, item := range t {
				if fmt.Sprint(item.Key) == part {
					curr = item.Value
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("path not found: %s", path)
			}
		case map[string]any:
			if v, ok := t[part]; ok {
				curr = v
			} else {
				return nil, fmt.Errorf("path not found: %s", path)
			}
		case map[any]any:
			found := false
			for k, v := range t {
				if fmt.Sprint(k) == part {
					curr = v
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("path not found: %s", path)
			}
		default:
			return nil, fmt.Errorf("path not found: %s", path)
		}
	}

	return curr, nil
}

func parseEnvRef(s string) (ok bool, varName string, def *string) {
	const prefix = "$env:"
	if !strings.HasPrefix(s, prefix) {
		return false, "", nil
	}

	rest := strings.TrimSpace(strings.TrimPrefix(s, prefix))
	if rest == "" {
		return false, "", nil
	}

	if i := strings.Index(rest, "="); i >= 0 {
		vn := strings.TrimSpace(rest[:i])
		dv := rest[i+1:]
		if vn == "" {
			return false, "", nil
		}
		dv = strings.TrimRight(dv, "\r\n")
		return true, vn, &dv
	}

	return true, rest, nil
}

func coerceScalar(raw string) any {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}

	switch strings.ToLower(s) {
	case "true":
		return true
	case "false":
		return false
	case "null", "~":
		return nil
	}

	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i
	}

	if strings.ContainsAny(s, ".eE") {
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f
		}
	}

	return raw
}
