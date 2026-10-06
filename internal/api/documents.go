package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

type Document struct {
	Name      string    `json:"name"`
	YAML      string    `json:"yaml"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type DocumentRequest struct {
	Name string `json:"name"`
	YAML string `json:"yaml"`
}

type Examples struct {
	Pattern string   `json:"pattern"`
	Files   []string `json:"files"`
}

type documents struct {
	kind   string
	dir    string
	redact func(data []byte) ([]byte, error)
	merge  func(next, previous []byte) ([]byte, error)
}

func (d documents) file(name string) (string, error) {
	name = strings.Trim(strings.TrimSpace(name), "/")
	if strings.HasSuffix(name, ".yml") || !validName(name) {
		return "", fmt.Errorf("invalid %s name %q", d.kind, name)
	}
	return name + ".yml", nil
}

func (d documents) read(root *os.Root, file string) (Document, error) {
	data, err := root.ReadFile(file)
	if err == nil && d.redact != nil {
		data, err = d.redact(data)
	}
	if err != nil {
		return Document{}, err
	}
	info, err := root.Stat(file)
	if err != nil {
		return Document{}, err
	}
	return Document{Name: strings.TrimSuffix(file, ".yml"), YAML: string(data), UpdatedAt: info.ModTime()}, nil
}

func removeDocument(root *os.Root, file string) error {
	if err := root.Remove(file); err != nil {
		return err
	}
	if dir := path.Dir(file); dir != "." {
		_ = root.Remove(dir)
	}
	return nil
}

func (d documents) root(w http.ResponseWriter) (*os.Root, bool) {
	root, err := os.OpenRoot(d.dir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return nil, false
	}
	return root, true
}

func (d documents) list(w http.ResponseWriter, _ *http.Request) {
	root, ok := d.root(w)
	if !ok {
		return
	}
	defer root.Close()
	docs := []Document{}
	err := fs.WalkDir(root.FS(), ".", func(file string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(file, ".yml") {
			return err
		}
		doc, err := d.read(root, file)
		if err == nil {
			docs = append(docs, doc)
		}
		return nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, docs)
}

func (d documents) get(w http.ResponseWriter, r *http.Request) {
	file, err := d.file(r.PathValue("name"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	root, ok := d.root(w)
	if !ok {
		return
	}
	defer root.Close()
	doc, err := d.read(root, file)
	if err != nil {
		writeError(w, http.StatusNotFound, errors.New(d.kind+" not found"))
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (d documents) post(w http.ResponseWriter, r *http.Request) {
	d.save(w, r, "", http.StatusCreated)
}

func (d documents) put(w http.ResponseWriter, r *http.Request) {
	d.save(w, r, r.PathValue("name"), http.StatusOK)
}

func (d documents) save(w http.ResponseWriter, r *http.Request, current string, status int) {
	var req DocumentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	file, err := d.file(req.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	root, ok := d.root(w)
	if !ok {
		return
	}
	defer root.Close()
	old, _ := d.file(current)
	if _, err := root.Stat(file); err == nil && old != file {
		writeError(w, http.StatusConflict, fmt.Errorf("%s %q already exists", d.kind, strings.TrimSuffix(file, ".yml")))
		return
	}
	data := []byte(req.YAML)
	if d.merge != nil {
		previous, _ := root.ReadFile(old)
		if data, err = d.merge(data, previous); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}
	if err := root.MkdirAll(path.Dir(file), 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := root.WriteFile(file, data, 0o644); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if old != "" && old != file {
		_ = removeDocument(root, old)
	}
	doc, err := d.read(root, file)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, status, doc)
}

func (d documents) delete(w http.ResponseWriter, r *http.Request) {
	file, err := d.file(r.PathValue("name"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	root, ok := d.root(w)
	if !ok {
		return
	}
	defer root.Close()
	if err := removeDocument(root, file); err != nil {
		writeError(w, http.StatusNotFound, errors.New(d.kind+" not found"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) page(d documents) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name, raw := strings.CutSuffix(r.PathValue("name"), ".yml")
		if !raw {
			s.serveWeb(w, r)
			return
		}
		file, err := d.file(name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		root, ok := d.root(w)
		if !ok {
			return
		}
		defer root.Close()
		doc, err := d.read(root, file)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(doc.YAML))
	}
}

func (s *Server) exampleFiles() []string {
	files := []string{}
	_ = fs.WalkDir(os.DirFS(s.examples), ".", func(file string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(file, ".yml") {
			files = append(files, file)
		}
		return nil
	})
	return files
}

func (s *Server) getExamples(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, Examples{
		Pattern: "." + string(filepath.Separator) + filepath.Join("config-examples", "**", "*.yml"),
		Files:   s.exampleFiles(),
	})
}

func (s *Server) copyExamples(w http.ResponseWriter, _ *http.Request) {
	root, ok := s.configs.root(w)
	if !ok {
		return
	}
	defer root.Close()
	for _, file := range s.exampleFiles() {
		if _, err := root.Stat(file); err == nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.examples, file))
		if err == nil {
			err = root.MkdirAll(path.Dir(file), 0o755)
		}
		if err == nil {
			err = root.WriteFile(file, data, 0o644)
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
