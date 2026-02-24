package main

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
)

type MissingEnvError struct {
	Missing []string
}

func (e *MissingEnvError) Error() string {
	cp := append([]string(nil), e.Missing...)
	sort.Strings(cp)
	return "missing required environment variables:\n  - " + strings.Join(cp, "\n  - ")
}

func RenderEnvYAML(in []byte) ([]byte, error) {
	var v any
	if err := yaml.Unmarshal(in, &v); err != nil {
		return nil, err
	}

	var missing []string
	v = renderEnv(v, "", &missing)

	if len(missing) > 0 {
		return nil, &MissingEnvError{Missing: missing}
	}

	out, err := yaml.Marshal(v)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func renderEnv(v any, path string, missing *[]string) any {
	switch t := v.(type) {
	case yaml.MapSlice:
		for i := range t {
			keyStr := fmt.Sprint(t[i].Key)
			childPath := keyStr
			if path != "" {
				childPath = path + "." + keyStr
			}

			t[i].Key = renderKeyIfNeeded(t[i].Key, childPath, missing)
			t[i].Value = renderEnv(t[i].Value, childPath, missing)
		}
		return t

	case map[string]any:
		for k, vv := range t {
			childPath := k
			if path != "" {
				childPath = path + "." + k
			}
			t[k] = renderEnv(vv, childPath, missing)
		}
		return t

	case map[any]any:
		type keyChange struct {
			old any
			new any
			val any
		}
		var changes []keyChange

		for k, vv := range t {
			keyStr := fmt.Sprint(k)
			childPath := keyStr
			if path != "" {
				childPath = path + "." + keyStr
			}

			newV := renderEnv(vv, childPath, missing)
			t[k] = newV

			newK, changed := renderKeyIfNeededWithChanged(k, childPath, missing)
			if changed {
				changes = append(changes, keyChange{old: k, new: newK, val: newV})
			}
		}

		for _, c := range changes {
			delete(t, c.old)
			t[c.new] = c.val
		}
		return t

	case []any:
		for i := range t {
			childPath := fmt.Sprintf("%s[%d]", path, i)
			t[i] = renderEnv(t[i], childPath, missing)
		}
		return t

	case string:
		ok, varName, def := parseEnvRef(t)
		if !ok {
			return t
		}
		val, found := os.LookupEnv(varName)
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

func renderKeyIfNeeded(k any, path string, missing *[]string) any {
	newK, _ := renderKeyIfNeededWithChanged(k, path, missing)
	return newK
}

func renderKeyIfNeededWithChanged(k any, path string, missing *[]string) (any, bool) {
	ks, ok := k.(string)
	if !ok {
		return k, false
	}

	ok2, varName, def := parseEnvRef(ks)
	if !ok2 {
		return k, false
	}

	val, found := os.LookupEnv(varName)
	if !found {
		if def != nil {
			return coerceScalar(*def), true
		}
		*missing = append(*missing, fmt.Sprintf("%s(key) -> %s", path, varName))
		return k, false
	}

	return coerceScalar(val), true
}

// Syntax:
//
//	$env:VAR_NAME
//	$env:VAR_NAME=default value
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
		dv := rest[i+1:] // preserve spaces in default; coerceScalar trims as needed
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
