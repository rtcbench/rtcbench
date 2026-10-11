package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestCreatePlugin(t *testing.T) {
	s, h := newServer(t)
	s.Plugins = fstest.MapFS{"builtin/plugin.yml": {Data: []byte("id: builtin\nname: Built-in\n")}}
	create := pluginFixture(t, "my-plugin")
	project := createTestPlugin(t, h, create)
	wantPath, err := filepath.Abs(filepath.Join(s.dir, "plugins", create.Name))
	if err != nil {
		t.Fatal(err)
	}
	if want := (pluginProject{Name: create.Name, Path: wantPath}); project != want {
		t.Fatalf("project = %+v, want %+v", project, want)
	}
	for file, want := range create.Files {
		t.Run(file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(project.Path, filepath.FromSlash(file)))
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != want {
				t.Error("saved file differs from submitted source")
			}
			if len(data) == 0 || strings.Contains(string(data), "__PLUGIN__") || strings.Contains(string(data), "__MODULE__") {
				t.Error("empty or unrendered template")
			}
		})
	}
	if res := do(t, h, "POST", "/api/v1/plugins", create); res.Code != 409 {
		t.Fatalf("duplicate creation: status = %d, want 409: %s", res.Code, res.Body)
	}
	saved, err := s.readPlugin(create.Name)
	if err != nil {
		t.Fatal(err)
	}
	if saved != project {
		t.Errorf("saved metadata = %+v, want %+v", saved, project)
	}
	list := do(t, h, "GET", "/api/v1/plugins", nil)
	if list.Code != 200 {
		t.Fatalf("listing: status = %d, want 200: %s", list.Code, list.Body)
	}
	var plugins []pluginManifest
	if err := json.Unmarshal(list.Body.Bytes(), &plugins); err != nil {
		t.Fatal(err)
	}
	if len(plugins) != 2 {
		t.Fatalf("got %d plugins, want built-in and project", len(plugins))
	}
	if plugins[0].ID != "builtin" || plugins[1].Project != create.Name {
		t.Errorf("unexpected manifest listing: %+v", plugins)
	}
}

func TestDownloadPlugin(t *testing.T) {
	_, h := newServer(t)
	create := pluginFixture(t, "my-plugin")
	project := createTestPlugin(t, h, create)
	want := maps.Clone(create.Files)
	want["plugin/plugin.go"] += "\nfunc custom() {}\n"
	want["plugin/extra.go"] = "package plugin\n"
	files := map[string]string{
		"plugin/plugin.go":           want["plugin/plugin.go"],
		"plugin/extra.go":            want["plugin/extra.go"],
		"web/node_modules/.bin/tool": "not source",
		"web/dist/main.js":           "not source",
		".git/config":                "not source",
		"rtcbench-data/logs/run.log": "not source",
		"rtcbench":                   "not source",
		"rtcbench.exe":               "not source",
	}
	for file, content := range files {
		name := filepath.Join(project.Path, filepath.FromSlash(file))
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("main.go", filepath.Join(project.Path, "source-link")); err != nil {
		t.Log("symlinks unavailable:", err)
	}
	res := do(t, h, "GET", "/api/v1/plugins/my-plugin/download", nil)
	if res.Code != 200 {
		t.Fatalf("download: status = %d, want 200: %s", res.Code, res.Body)
	}
	archive, err := zip.NewReader(bytes.NewReader(res.Body.Bytes()), int64(res.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range archive.File {
		if file.FileInfo().IsDir() {
			continue
		}
		expected, ok := want[file.Name]
		if !ok {
			t.Errorf("unexpected or duplicate archive file: %s", file.Name)
			continue
		}
		stream, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(stream)
		stream.Close()
		if err != nil {
			t.Fatal(err)
		}
		if string(content) != expected {
			t.Errorf("archive file %s differs from saved source", file.Name)
		}
		delete(want, file.Name)
	}
	for file := range want {
		t.Errorf("archive is missing %s", file)
	}
}

func TestPluginProjectValidation(t *testing.T) {
	_, h := newServer(t)
	for _, name := range []string{"../outside", "a/b", "A", "con", "new", "", "a;echo"} {
		t.Run("name="+name, func(t *testing.T) {
			create := pluginFixture(t, "valid")
			create.Name = name
			res := do(t, h, "POST", "/api/v1/plugins", create)
			if res.Code != 400 {
				t.Fatalf("status = %d, want 400: %s", res.Code, res.Body)
			}
		})
	}
	t.Run("invalid module", func(t *testing.T) {
		create := pluginFixture(t, "valid")
		create.Module = "example.com/\"\ninjected"
		res := do(t, h, "POST", "/api/v1/plugins", create)
		if res.Code != 400 {
			t.Fatalf("status = %d, want 400: %s", res.Code, res.Body)
		}
	})
}

func TestPluginProjectBoundaries(t *testing.T) {
	s, h := newServer(t)
	createTestPlugin(t, h, pluginFixture(t, "valid"))
	if res := do(t, h, "PUT", "/api/v1/plugins/valid", map[string]any{"files": map[string]string{"main.go": "bad"}}); res.Code != 405 {
		t.Fatalf("file editing: status = %d, want 405: %s", res.Code, res.Body)
	}
	if root, err := s.pluginRoot("../outside"); err == nil {
		root.Close()
		t.Fatal("accepted parent path")
	}
}

func TestPluginProjectSymlinkEscape(t *testing.T) {
	s, _ := newServer(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "plugin.yml"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(s.dir, "plugins", "escape")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	if _, err := s.readPlugin("escape"); err == nil {
		t.Fatal("followed project symlink outside plugins")
	}
}

func createTestPlugin(t *testing.T, h http.Handler, create pluginRequest) pluginProject {
	t.Helper()
	res := do(t, h, "POST", "/api/v1/plugins", create)
	if res.Code != 201 {
		t.Fatalf("create: status = %d, want 201: %s", res.Code, res.Body)
	}
	var project pluginProject
	if err := json.Unmarshal(res.Body.Bytes(), &project); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Body.String(), `"files"`) {
		t.Fatal("creation response includes source files")
	}
	return project
}

func pluginFixture(t *testing.T, name string) pluginRequest {
	t.Helper()
	request := pluginRequest{Name: name, Module: "example.com/" + name, Files: map[string]string{}}
	root := os.DirFS("../../packages/app/plugin-template")
	replace := strings.NewReplacer("__PLUGIN__", name, "__MODULE__", request.Module)
	err := fs.WalkDir(root, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := fs.ReadFile(root, path)
		request.Files[strings.TrimSuffix(path, ".tmpl")] = replace.Replace(string(data))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func TestPluginFileBoundaries(t *testing.T) {
	_, handler := newServer(t)
	for _, path := range []string{
		"../outside",
		"/absolute",
		"a/../../outside",
		`a\outside`,
		"file:stream",
		".",
	} {
		t.Run(path, func(t *testing.T) {
			request := pluginFixture(t, "my-plugin")
			request.Files[path] = "invalid"
			response := do(t, handler, "POST", "/api/v1/plugins", request)
			if response.Code != 400 {
				t.Fatal("accepted unsafe file path", response.Code)
			}
		})
	}
}
