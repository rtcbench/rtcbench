package aoflog

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// Interface satisfaction (compile-time checks)
// ---------------------------------------------------------------------------

func TestNilClient_InterfaceSatisfaction(t *testing.T) {
	var _ Client = NilClient{}
}

func TestStdClient_InterfaceSatisfaction(t *testing.T) {
	var _ Client = &StdClient{}
}

func TestFileClient_InterfaceSatisfaction(t *testing.T) {
	var _ Client = &FileClient{}
}

func TestMultiClient_InterfaceSatisfaction(t *testing.T) {
	var _ Client = &MultiClient{}
}

// ---------------------------------------------------------------------------
// NilClient
// ---------------------------------------------------------------------------

func TestNilClient_NoPanic(t *testing.T) {
	var c NilClient
	c.Println("a", "b", "c")
	c.Printf("hello %s %d", "world", 42)
	c.LogPrintln("log", "message")
	c.LogPrintf("log %s", "formatted")
}

// ---------------------------------------------------------------------------
// StdClient
// ---------------------------------------------------------------------------

func TestNewStdClient(t *testing.T) {
	c := NewStdClient("test")
	if c == nil {
		t.Fatalf("NewStdClient returned nil")
	}
}

func TestNewStdClient_PrefixWrapping(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
	}{
		{name: "empty prefix", prefix: ""},
		{name: "non-empty prefix", prefix: "test"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewStdClient(tt.prefix)
			if c == nil {
				t.Fatalf("NewStdClient(%q) returned nil", tt.prefix)
			}
			if c.logger == nil {
				t.Fatalf("NewStdClient(%q) has nil logger", tt.prefix)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// FileClient
// ---------------------------------------------------------------------------

func newTestFileClient(t *testing.T, prefix string) (*FileClient, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	fc, err := NewFileClient(prefix, path)
	if err != nil {
		t.Fatalf("NewFileClient(%q, %q) error: %v", prefix, path, err)
	}
	return fc, path
}

func readFileContent(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error: %v", path, err)
	}
	return string(data)
}

func TestNewFileClient_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	fc, err := NewFileClient("", path)
	if err != nil {
		t.Fatalf("NewFileClient error: %v", err)
	}
	defer fc.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("file does not exist after NewFileClient: %v", err)
	}
	if info.IsDir() {
		t.Fatalf("expected file, got directory")
	}
}

func TestFileClient_Println(t *testing.T) {
	fc, path := newTestFileClient(t, "")
	defer fc.Close()

	fc.Println("a", "b")

	content := readFileContent(t, path)
	// fmt.Fprintln joins with spaces and appends newline
	expected := fmt.Sprintln("a", "b")
	if !strings.Contains(content, strings.TrimRight(expected, "\n")) {
		t.Fatalf("expected file to contain %q, got %q", expected, content)
	}
}

func TestFileClient_Printf(t *testing.T) {
	fc, path := newTestFileClient(t, "")
	defer fc.Close()

	fc.Printf("count=%d", 42)

	content := readFileContent(t, path)
	if !strings.Contains(content, "count=42") {
		t.Fatalf("expected file to contain %q, got %q", "count=42", content)
	}
}

func TestFileClient_LogPrintln(t *testing.T) {
	fc, path := newTestFileClient(t, "")
	defer fc.Close()

	fc.LogPrintln("hello world")

	content := readFileContent(t, path)
	if !strings.Contains(content, "hello world") {
		t.Fatalf("expected file to contain %q, got %q", "hello world", content)
	}

	matched, err := regexp.MatchString(`\d{4}/\d{2}/\d{2}`, content)
	if err != nil {
		t.Fatalf("regexp error: %v", err)
	}
	if !matched {
		t.Fatalf("expected timestamp pattern in output, got %q", content)
	}
}

func TestFileClient_LogPrintf(t *testing.T) {
	fc, path := newTestFileClient(t, "")
	defer fc.Close()

	fc.LogPrintf("value=%d", 99)

	content := readFileContent(t, path)
	if !strings.Contains(content, "value=99") {
		t.Fatalf("expected file to contain %q, got %q", "value=99", content)
	}

	matched, err := regexp.MatchString(`\d{4}/\d{2}/\d{2}`, content)
	if err != nil {
		t.Fatalf("regexp error: %v", err)
	}
	if !matched {
		t.Fatalf("expected timestamp pattern in output, got %q", content)
	}
}

func TestFileClient_WithPrefix(t *testing.T) {
	fc, path := newTestFileClient(t, "myprefix")
	defer fc.Close()

	fc.LogPrintln("hello")

	content := readFileContent(t, path)
	if !strings.Contains(content, "<myprefix>") {
		t.Fatalf("expected file to contain %q, got %q", "<myprefix>", content)
	}
	if !strings.Contains(content, "hello") {
		t.Fatalf("expected file to contain %q, got %q", "hello", content)
	}
}

func TestFileClient_WithoutPrefix(t *testing.T) {
	fc, path := newTestFileClient(t, "")
	defer fc.Close()

	fc.LogPrintln("hello")

	content := readFileContent(t, path)
	if strings.Contains(content, "<") {
		t.Fatalf("expected no angle bracket wrapping in output, got %q", content)
	}
	if !strings.Contains(content, "hello") {
		t.Fatalf("expected file to contain %q, got %q", "hello", content)
	}
}

func TestFileClient_Close(t *testing.T) {
	fc, path := newTestFileClient(t, "")

	fc.Println("before close")
	err := fc.Close()
	if err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	content := readFileContent(t, path)
	if !strings.Contains(content, "before close") {
		t.Fatalf("expected file to contain %q after close, got %q", "before close", content)
	}
}

func TestFileClient_Sync(t *testing.T) {
	fc, path := newTestFileClient(t, "")
	defer fc.Close()
	_ = path

	fc.Println("sync test")
	err := fc.Sync()
	if err != nil {
		t.Fatalf("Sync() returned error: %v", err)
	}
}

func TestFileClient_ConcurrentWrites(t *testing.T) {
	fc, path := newTestFileClient(t, "")

	const goroutines = 10
	const writesPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(id int) {
			defer wg.Done()
			for j := range writesPerGoroutine {
				fc.Println("goroutine", id, "write", j)
			}
		}(i)
	}
	wg.Wait()

	err := fc.Close()
	if err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	content := readFileContent(t, path)
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	expected := goroutines * writesPerGoroutine
	if len(lines) != expected {
		t.Fatalf("expected %d lines, got %d", expected, len(lines))
	}
}

// ---------------------------------------------------------------------------
// MultiClient
// ---------------------------------------------------------------------------

func TestMultiClient_FanOut(t *testing.T) {
	fc1, path1 := newTestFileClient(t, "")
	defer fc1.Close()
	fc2, path2 := newTestFileClient(t, "")
	defer fc2.Close()

	mc := NewMultiClient(fc1, fc2)
	mc.Println("hello")

	content1 := readFileContent(t, path1)
	content2 := readFileContent(t, path2)

	if !strings.Contains(content1, "hello") {
		t.Fatalf("expected file1 to contain %q, got %q", "hello", content1)
	}
	if !strings.Contains(content2, "hello") {
		t.Fatalf("expected file2 to contain %q, got %q", "hello", content2)
	}
}

func TestMultiClient_LogPrintf_FanOut(t *testing.T) {
	fc1, path1 := newTestFileClient(t, "")
	defer fc1.Close()
	fc2, path2 := newTestFileClient(t, "")
	defer fc2.Close()

	mc := NewMultiClient(fc1, fc2)
	mc.LogPrintf("n=%d", 7)

	content1 := readFileContent(t, path1)
	content2 := readFileContent(t, path2)

	if !strings.Contains(content1, "n=7") {
		t.Fatalf("expected file1 to contain %q, got %q", "n=7", content1)
	}
	if !strings.Contains(content2, "n=7") {
		t.Fatalf("expected file2 to contain %q, got %q", "n=7", content2)
	}
}

func TestMultiClient_AddClient(t *testing.T) {
	fc1, path1 := newTestFileClient(t, "")
	defer fc1.Close()
	fc2, path2 := newTestFileClient(t, "")
	defer fc2.Close()

	mc := NewMultiClient(fc1)
	mc.Println("first")
	mc.AddClient(fc2)
	mc.Println("second")

	content1 := readFileContent(t, path1)
	content2 := readFileContent(t, path2)

	// file1 should have both messages
	if !strings.Contains(content1, "first") {
		t.Fatalf("expected file1 to contain %q, got %q", "first", content1)
	}
	if !strings.Contains(content1, "second") {
		t.Fatalf("expected file1 to contain %q, got %q", "second", content1)
	}

	// file2 should only have the message written after AddClient
	if strings.Contains(content2, "first") {
		t.Fatalf("expected file2 to NOT contain %q, got %q", "first", content2)
	}
	if !strings.Contains(content2, "second") {
		t.Fatalf("expected file2 to contain %q, got %q", "second", content2)
	}
}
