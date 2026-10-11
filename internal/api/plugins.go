package api

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/goccy/go-yaml"
	"golang.org/x/mod/module"
)

var pluginName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`)

type pluginProject struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type pluginManifest struct {
	ID           string   `yaml:"id" json:"id"`
	Name         string   `yaml:"name" json:"name"`
	Description  string   `yaml:"description" json:"description"`
	Dockerfile   string   `yaml:"dockerfile,omitempty" json:"dockerfile,omitempty"`
	BuildContext []string `yaml:"build_context,omitempty" json:"-"`
	Project      string   `yaml:"-" json:"project,omitempty"`
	Error        string   `yaml:"-" json:"error,omitempty"`
}

func readPluginManifest(files fs.FS, file, project string) pluginManifest {
	manifest := pluginManifest{Project: project}
	data, err := fs.ReadFile(files, file)
	if err == nil {
		err = yaml.UnmarshalWithOptions(data, &manifest, yaml.Strict())
	}
	if err == nil && (manifest.ID == "" || manifest.Name == "") {
		err = errors.New("plugin.yml needs id and name")
	}
	if err != nil {
		manifest.Error = err.Error()
		manifest.Name = project
	}
	return manifest
}

func (s *Server) pluginRoot(name string) (*os.Root, error) {
	if name != "" && (!pluginName.MatchString(name) || !validName(name) || name == "new") {
		return nil, errors.New("use a project name with lowercase letters, numbers, and hyphens")
	}
	root, err := os.OpenRoot(filepath.Join(s.dir, "plugins"))
	if err != nil || name == "" {
		return root, err
	}
	defer root.Close()
	return root.OpenRoot(name)
}

func (s *Server) listPlugins(w http.ResponseWriter, r *http.Request) {
	root, err := s.pluginRoot("")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer root.Close()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	plugins := []pluginManifest{}
	if s.Plugins != nil {
		_ = fs.WalkDir(s.Plugins, ".", func(file string, entry fs.DirEntry, err error) error {
			if err == nil && !entry.IsDir() && path.Base(file) == "plugin.yml" {
				plugins = append(plugins, readPluginManifest(s.Plugins, file, ""))
			}
			return err
		})
	}
	for _, entry := range entries {
		if entry.IsDir() && pluginName.MatchString(entry.Name()) && validName(entry.Name()) && entry.Name() != "new" {
			plugins = append(plugins, readPluginManifest(root.FS(), entry.Name()+"/plugin.yml", entry.Name()))
		}
	}
	writeJSON(w, http.StatusOK, plugins)
}

func (s *Server) createPlugin(w http.ResponseWriter, r *http.Request) {
	var req pluginRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := req.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	root, err := s.pluginRoot("")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer root.Close()
	if err := root.Mkdir(req.Name, 0o755); err != nil {
		if errors.Is(err, fs.ErrExist) {
			writeError(w, http.StatusConflict, errors.New("a project with this name already exists"))
		} else {
			writeError(w, http.StatusInternalServerError, err)
		}
		return
	}
	project, err := root.OpenRoot(req.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer project.Close()
	for name, data := range req.Files {
		if err = project.MkdirAll(path.Dir(name), 0o755); err == nil {
			err = project.WriteFile(name, []byte(data), 0o644)
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}
	info, err := s.readPlugin(req.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, info)
}

type pluginRequest struct {
	Name   string            `json:"name"`
	Module string            `json:"module"`
	Files  map[string]string `json:"files"`
}

func (req pluginRequest) validate() error {
	if !pluginName.MatchString(req.Name) || !validName(req.Name) || req.Name == "new" {
		return errors.New("enter a lowercase project name with letters, numbers, and hyphens")
	}
	if module.CheckImportPath(req.Module) != nil || !strings.Contains(strings.Split(req.Module, "/")[0], ".") {
		return errors.New("enter a Go module path such as example.com/my-plugin")
	}
	if len(req.Files) > 64 || req.Files["plugin.yml"] == "" {
		return errors.New("include plugin.yml and at most 64 project files")
	}
	for name := range req.Files {
		if !fs.ValidPath(name) || name == "." || strings.ContainsAny(name, `\:`) {
			return errors.New("project files must have relative paths inside the project")
		}
	}
	return nil
}

func (s *Server) readPlugin(name string) (pluginProject, error) {
	project := pluginProject{Name: name}
	root, err := s.pluginRoot(name)
	if err != nil {
		return project, err
	}
	defer root.Close()
	if _, err := root.Stat("plugin.yml"); err != nil {
		return project, err
	}
	project.Path, err = filepath.Abs(filepath.Join(s.dir, "plugins", name))
	return project, err
}

func (s *Server) getPlugin(w http.ResponseWriter, r *http.Request) {
	project, err := s.readPlugin(r.PathValue("name"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, project)
}

func (s *Server) downloadPlugin(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	root, err := s.pluginRoot(name)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	defer root.Close()
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`.zip"`)
	archive := zip.NewWriter(w)
	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "node_modules", "dist", "rtcbench-data":
				return fs.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() || name == "rtcbench" || name == "rtcbench.exe" {
			return nil
		}
		file, err := root.Open(name)
		if err != nil {
			return err
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name, header.Method = name, zip.Deflate
		out, err := archive.CreateHeader(header)
		if err == nil {
			_, err = io.Copy(out, file)
		}
		return err
	})
	if err == nil {
		err = archive.Close()
	}
	if err != nil {
		s.Logf("download plugin %s: %v", name, err)
		panic(http.ErrAbortHandler)
	}
}
