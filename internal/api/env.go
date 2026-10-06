package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"

	"github.com/goccy/go-yaml"
)

type EnvVar struct {
	Name  string  `yaml:"name"`
	Type  string  `yaml:"type"`
	Value *string `yaml:"value,omitempty"`
}

var envVarName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func parseEnv(data []byte) ([]EnvVar, error) {
	var vars []EnvVar
	err := yaml.Unmarshal(data, &vars)
	return vars, err
}

func redactEnv(data []byte) ([]byte, error) {
	vars, err := parseEnv(data)
	if err != nil {
		return nil, err
	}
	for i := range vars {
		if vars[i].Type != "variable" {
			vars[i].Value = nil
		}
	}
	return yaml.Marshal(vars)
}

func mergeEnv(system bool) func(next, previous []byte) ([]byte, error) {
	return func(next, previous []byte) ([]byte, error) {
		vars, err := parseEnv(next)
		if err != nil {
			return nil, err
		}
		stored, _ := parseEnv(previous)
		seen := map[string]bool{}
		for i, v := range vars {
			if !envVarName.MatchString(v.Name) || seen[v.Name] {
				return nil, fmt.Errorf("invalid variable name %q", v.Name)
			}
			seen[v.Name] = true
			switch {
			case v.Type == "secret" && v.Value == nil:
				j := slices.IndexFunc(stored, func(s EnvVar) bool { return s.Name == v.Name && s.Type == "secret" })
				if j < 0 {
					return nil, fmt.Errorf("secret %s needs a value", v.Name)
				}
				vars[i].Value = stored[j].Value
			case v.Type == "system" && system:
				vars[i].Value = nil
			case v.Type != "variable" && v.Type != "secret", v.Value == nil:
				return nil, fmt.Errorf("invalid variable %s", v.Name)
			}
		}
		return yaml.Marshal(vars)
	}
}

func (s *Server) environment(name string) (env, variables map[string]string, err error) {
	env, variables = map[string]string{}, map[string]string{}
	apply := func(data []byte, record bool) error {
		vars, err := parseEnv(data)
		for _, v := range vars {
			if v.Type == "system" || v.Value == nil {
				continue
			}
			env[v.Name] = *v.Value
			if record && v.Type == "variable" {
				variables[v.Name] = *v.Value
			}
		}
		return err
	}
	overrides, _ := os.ReadFile(s.overrides)
	if err := apply(overrides, false); err != nil {
		return nil, nil, err
	}
	if name == "" {
		return env, variables, nil
	}
	file, err := s.envs.file(name)
	if err != nil {
		return nil, nil, err
	}
	data, err := os.ReadFile(filepath.Join(s.envs.dir, file))
	if err != nil {
		return nil, nil, fmt.Errorf("environment %q not found", name)
	}
	return env, variables, apply(data, true)
}

func (s *Server) getOverrides(w http.ResponseWriter, _ *http.Request) {
	data, err := os.ReadFile(s.overrides)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	redacted, err := redactEnv(data)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	doc := Document{Name: "system-overrides", YAML: string(redacted)}
	if info, err := os.Stat(s.overrides); err == nil {
		doc.UpdatedAt = info.ModTime()
	}
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) putOverrides(w http.ResponseWriter, r *http.Request) {
	var req DocumentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	previous, _ := os.ReadFile(s.overrides)
	data, err := mergeEnv(false)([]byte(req.YAML), previous)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := os.WriteFile(s.overrides, data, 0o644); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.getOverrides(w, r)
}
