package aoflog

import (
	"fmt"
	"log"
	"os"
	"sync"
)

type Client interface {
	Println(v ...any)
	Printf(format string, v ...any)

	LogPrintln(v ...any)
	LogPrintf(format string, v ...any)
	LogFatalln(v ...any)
	LogFatalf(format string, v ...any)
}

// StdClient prints to stdout/stderr. Log* methods can use a prefix and timestamps.
type StdClient struct {
	logger *log.Logger
}

// NewStdClient creates a StdClient with an optional prefix wrapped in <>
func NewStdClient(prefix string) *StdClient {
	if prefix != "" {
		prefix = "<" + prefix + "> "
	}
	return &StdClient{
		logger: log.New(os.Stderr, prefix, log.LstdFlags),
	}
}

func (c *StdClient) Println(v ...any) {
	fmt.Println(v...)
}

func (c *StdClient) Printf(format string, v ...any) {
	fmt.Printf(format, v...)
}

func (c *StdClient) LogPrintln(v ...any) {
	if c.logger != nil {
		c.logger.Println(v...)
	} else {
		log.Println(v...)
	}
}

func (c *StdClient) LogPrintf(format string, v ...any) {
	if c.logger != nil {
		c.logger.Printf(format, v...)
	} else {
		log.Printf(format, v...)
	}
}

func (c *StdClient) LogFatalln(v ...any) {
	if c.logger != nil {
		c.logger.Fatalln(v...)
	} else {
		log.Fatalln(v...)
	}
}

func (c *StdClient) LogFatalf(format string, v ...any) {
	if c.logger != nil {
		c.logger.Fatalf(format, v...)
	} else {
		log.Fatalf(format, v...)
	}
}

// FileClient logs to an append-only file with optional prefix for Log* methods.
type FileClient struct {
	logger *log.Logger
	file   *os.File
	mu     sync.Mutex
}

// NewFileClient creates a client to log to an append-only file with optional prefix wrapped in <>
func NewFileClient(prefix, path string) (*FileClient, error) {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	if prefix != "" {
		prefix = "<" + prefix + "> "
	}

	logger := log.New(file, prefix, log.LstdFlags)

	return &FileClient{
		logger: logger,
		file:   file,
	}, nil
}

func (f *FileClient) Close() error {
	return f.file.Close()
}

func (f *FileClient) Sync() error {
	return f.file.Sync()
}

// Println writes raw output (no timestamp), thread-safe
func (f *FileClient) Println(v ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fmt.Fprintln(f.file, v...)
}

// Printf writes raw output (no timestamp), thread-safe
func (f *FileClient) Printf(format string, v ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fmt.Fprintf(f.file, format, v...)
}

// Log* methods use timestamps and prefix, via thread-safe logger
func (f *FileClient) LogPrintln(v ...any) {
	f.logger.Println(v...)
}

func (f *FileClient) LogPrintf(format string, v ...any) {
	f.logger.Printf(format, v...)
}

func (f *FileClient) LogFatalln(v ...any) {
	f.logger.Fatalln(v...)
}

func (f *FileClient) LogFatalf(format string, v ...any) {
	f.logger.Fatalf(format, v...)
}

// NilClient is a no-op implementation, useful for testing or disabling logs.
type NilClient struct{}

func (NilClient) Println(...any)           {}
func (NilClient) Printf(string, ...any)    {}
func (NilClient) LogPrintln(...any)        {}
func (NilClient) LogPrintf(string, ...any) {}
func (NilClient) LogFatalln(...any)        {}
func (NilClient) LogFatalf(string, ...any) {}

// MultiClient logs to multiple underlying clients
type MultiClient struct {
	clients []Client
}

func NewMultiClient(clients ...Client) *MultiClient {
	return &MultiClient{clients: clients}
}

func (m *MultiClient) AddClient(client Client) {
	m.clients = append(m.clients, client)
}

func (m *MultiClient) Println(v ...any) {
	for _, c := range m.clients {
		c.Println(v...)
	}
}

func (m *MultiClient) Printf(format string, v ...any) {
	for _, c := range m.clients {
		c.Printf(format, v...)
	}
}

func (m *MultiClient) LogPrintln(v ...any) {
	for _, c := range m.clients {
		c.LogPrintln(v...)
	}
}

func (m *MultiClient) LogPrintf(format string, v ...any) {
	for _, c := range m.clients {
		c.LogPrintf(format, v...)
	}
}

func (m *MultiClient) LogFatalln(v ...any) {
	for _, c := range m.clients {
		c.LogFatalln(v...)
	}
}

func (m *MultiClient) LogFatalf(format string, v ...any) {
	for _, c := range m.clients {
		c.LogFatalf(format, v...)
	}
}
