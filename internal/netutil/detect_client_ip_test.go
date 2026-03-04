package netutil

import (
	"errors"
	"testing"
)

func TestDetectClientIP_IPv4Localhost(t *testing.T) {
	ip, err := DetectClientIP("127.0.0.1")
	if err != nil {
		t.Fatalf("DetectClientIP error: %v", err)
	}
	if ip != "127.0.0.1" {
		t.Fatalf("expected 127.0.0.1, got %q", ip)
	}
}

func TestDetectClientIP_InvalidServerIP_Wraps(t *testing.T) {
	_, err := DetectClientIP("not-an-ip")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, ErrDetectClientIP) {
		t.Fatalf("expected errors.Is(err, ErrDetectClientIP) true; err=%v", err)
	}
}

func TestDetectClientIP_IPv6Localhost(t *testing.T) {
	ip, err := DetectClientIP("::1")
	if err != nil {
		t.Fatalf("DetectClientIP error: %v", err)
	}
	if ip != "::1" {
		t.Fatalf("expected ::1, got %q", ip)
	}
}
