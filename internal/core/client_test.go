package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestExpandRoomName(t *testing.T) {
	tests := []struct {
		base   string
		offset int
		want   string
	}{
		// offset 0 always returns base unchanged
		{"room-1234", 0, "room-1234"},
		{"nodigits", 0, "nodigits"},

		// trailing digits are incremented
		{"room-1234", 1, "room-1235"},
		{"room-1234", 2, "room-1236"},
		{"room-1234", 100, "room-1334"},

		// single digit
		{"test0", 1, "test1"},
		{"test9", 1, "test10"},

		// all digits
		{"42", 0, "42"},
		{"42", 1, "43"},
		{"42", 8, "50"},

		// no trailing digits falls back to _N suffix
		{"nodigits", 1, "nodigits_1"},
		{"nodigits", 5, "nodigits_5"},
		{"room-abc", 2, "room-abc_2"},

		// empty string
		{"", 0, ""},
		{"", 1, "_1"},
	}

	for _, tt := range tests {
		t.Run(tt.base+"_"+string(rune('0'+tt.offset)), func(t *testing.T) {
			got := expandRoomName(tt.base, tt.offset)
			if got != tt.want {
				t.Errorf("expandRoomName(%q, %d) = %q, want %q",
					tt.base, tt.offset, got, tt.want)
			}
		})
	}
}

func TestClientShutdownAllClosesUsersAndPlugins(t *testing.T) {
	plugin := &fakeParticipantPlugin{}
	client := newTestClient("fake", func() Plugin { return plugin })

	user := mustCreateUser(t, client, "alice", Viewer)

	if err := user.JoinRoom(context.Background(), &JoinRequest{
		Plugin: "fake",
		RoomID: "room-1",
	}); err != nil {
		t.Fatalf("JoinRoom() error = %v", err)
	}

	if err := client.ShutdownAll(context.Background()); err != nil {
		t.Fatalf("ShutdownAll() error = %v", err)
	}

	p := plugin.participants[0]
	if p.closeCalls != 1 {
		t.Fatalf("participant Close() calls = %d, want 1", p.closeCalls)
	}
	if plugin.shutdownCalls != 1 {
		t.Fatalf("plugin Shutdown() calls = %d, want 1", plugin.shutdownCalls)
	}
}

func TestClientCreateUserRejectsDuplicateUserID(t *testing.T) {
	client := newTestClient("fake", func() Plugin { return &fakeParticipantPlugin{} })

	first := mustCreateUser(t, client, "alice", Viewer)
	second, err := client.CreateUser(context.Background(), &UserConfig{
		UserID: "alice",
		Role:   Viewer,
	})
	if !errors.Is(err, ErrUserExists) {
		t.Fatalf("second CreateUser() error = %v, want ErrUserExists", err)
	}
	if second != nil {
		t.Fatalf("second CreateUser() = %#v, want nil on duplicate", second)
	}
	if got := client.GetUser("alice"); got != first {
		t.Fatalf("GetUser() = %#v, want original user %#v", got, first)
	}
}

func TestClientShutdownAllRespectsContextDuringUserClose(t *testing.T) {
	plugin := &blockingLeaveParticipantPlugin{}
	client := newTestClient("fake", func() Plugin { return plugin })

	user := mustCreateUser(t, client, "alice", Viewer)
	if err := user.JoinRoom(context.Background(), &JoinRequest{
		Plugin: "fake",
		RoomID: "room-1",
	}); err != nil {
		t.Fatalf("JoinRoom() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- client.ShutdownAll(ctx)
	}()

	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("ShutdownAll() error = %v, want context deadline exceeded", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("ShutdownAll() blocked past context deadline")
	}

	plugin.mu.Lock()
	defer plugin.mu.Unlock()
	if len(plugin.participants) != 1 {
		t.Fatalf("participants = %d, want 1", len(plugin.participants))
	}
	if plugin.participants[0].closeCalls != 1 {
		t.Fatalf("participant Close() calls = %d, want 1", plugin.participants[0].closeCalls)
	}
}

func TestClientRunScenarioUsesRegisteredScenario(t *testing.T) {
	client := newTestClient("fake", func() Plugin { return &fakeParticipantPlugin{} })
	scenario := &fakeScenario{}
	client.RegisterScenario("custom", func() Scenario { return scenario })

	if err := client.RunScenario(context.Background(), "custom"); err != nil {
		t.Fatalf("RunScenario() error = %v", err)
	}

	if scenario.runCalls != 1 {
		t.Fatalf("scenario Run() calls = %d, want 1", scenario.runCalls)
	}
	if scenario.env == nil {
		t.Fatal("scenario env is nil")
	}
	if scenario.env.Client() != client {
		t.Fatal("scenario env client mismatch")
	}
	if scenario.env.Config() != client.env.config {
		t.Fatal("scenario env config mismatch")
	}
}

func TestClientRunScenarioUnknownScenario(t *testing.T) {
	client := newTestClient("fake", func() Plugin { return &fakeParticipantPlugin{} })

	err := client.RunScenario(context.Background(), "missing")
	if !errors.Is(err, ErrUnknownScenario) {
		t.Fatalf("RunScenario() error = %v, want ErrUnknownScenario", err)
	}
}
