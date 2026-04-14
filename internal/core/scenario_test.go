package core

import (
	"context"
	"testing"
	"time"
)

func TestJoinAllRoomsPublishesSenderParticipants(t *testing.T) {
	plugin := &fakeVideoParticipantPlugin{}
	client := newTestClient("fake", func() Plugin { return plugin })
	client.env.config.Spec.Conference.UsersPerRoom = 3
	client.env.config.Spec.Conference.Cameras.PerRoom = 1

	if err := client.JoinAllRooms(context.Background()); err != nil {
		t.Fatalf("JoinAllRooms() error = %v", err)
	}

	plugin.mu.Lock()
	defer plugin.mu.Unlock()
	if len(plugin.participants) != 3 {
		t.Fatalf("participants = %d, want 3", len(plugin.participants))
	}

	published := 0
	for _, p := range plugin.participants {
		if p.joinCalls != 1 {
			t.Fatalf("joinCalls = %d, want 1", p.joinCalls)
		}
		if p.publishCalls > 0 {
			published++
		}
	}
	if published != 1 {
		t.Fatalf("published participants = %d, want 1 sender", published)
	}
}

func TestJoinAllRoomsIgnoresUnsupportedSenderPublish(t *testing.T) {
	plugin := &fakeParticipantPlugin{}
	client := newTestClient("fake", func() Plugin { return plugin })
	client.env.config.Spec.Conference.UsersPerRoom = 2
	client.env.config.Spec.Conference.Cameras.PerRoom = 1

	if err := client.JoinAllRooms(context.Background()); err != nil {
		t.Fatalf("JoinAllRooms() error = %v", err)
	}

	plugin.mu.Lock()
	defer plugin.mu.Unlock()
	if len(plugin.participants) != 2 {
		t.Fatalf("participants = %d, want 2", len(plugin.participants))
	}
}

func TestJoinAllRoomsUsesDefaultScenario(t *testing.T) {
	client := newTestClient("fake", func() Plugin { return &fakeParticipantPlugin{} })
	scenario := &fakeScenario{}
	client.RegisterScenario(DefaultScenarioID, func() Scenario { return scenario })

	if err := client.JoinAllRooms(context.Background()); err != nil {
		t.Fatalf("JoinAllRooms() error = %v", err)
	}

	if scenario.runCalls != 1 {
		t.Fatalf("default scenario Run() calls = %d, want 1", scenario.runCalls)
	}
}

func TestLoadChurnConfigDefaults(t *testing.T) {
	cfg, err := loadChurnConfig(nil)
	if err != nil {
		t.Fatalf("loadChurnConfig() error = %v", err)
	}
	if cfg.mode != churnModeLeaveRejoin {
		t.Fatalf("mode = %q, want %q", cfg.mode, churnModeLeaveRejoin)
	}
	if cfg.sessionDuration != 30*time.Second {
		t.Fatalf("sessionDuration = %s, want 30s", cfg.sessionDuration)
	}
	if cfg.rejoinDelay != 0 {
		t.Fatalf("rejoinDelay = %s, want 0", cfg.rejoinDelay)
	}
}

func TestLoadChurnConfigInvalidDuration(t *testing.T) {
	_, err := loadChurnConfig(map[string]any{
		"sessionDuration": "nope",
	})
	if err == nil {
		t.Fatal("loadChurnConfig() error = nil, want error")
	}
}

func TestLoadChurnConfigInvalidMode(t *testing.T) {
	_, err := loadChurnConfig(map[string]any{
		"mode": "nope",
	})
	if err == nil {
		t.Fatal("loadChurnConfig() error = nil, want error")
	}
}

func TestChurnScenarioRejoinsUsers(t *testing.T) {
	plugin := &fakeParticipantPlugin{}
	client := newTestClient("fake", func() Plugin { return plugin })
	client.env.config.Spec.ScenarioConfig = map[string]any{
		"sessionDuration": "20ms",
		"rejoinDelay":     "5ms",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 85*time.Millisecond)
	defer cancel()

	if err := client.RunScenario(ctx, ChurnScenarioID); err != nil {
		t.Fatalf("RunScenario(churn) error = %v", err)
	}

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		plugin.mu.Lock()
		participantCount := len(plugin.participants)
		plugin.mu.Unlock()
		client.mu.Lock()
		userCount := len(client.users)
		client.mu.Unlock()
		if participantCount >= 2 && userCount == 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	plugin.mu.Lock()
	participantCount := len(plugin.participants)
	participants := append([]*fakeParticipant(nil), plugin.participants...)
	plugin.mu.Unlock()
	if participantCount < 2 {
		t.Fatalf("participants = %d, want at least 2 churned joins", participantCount)
	}
	client.mu.Lock()
	userCount := len(client.users)
	client.mu.Unlock()
	if userCount != 0 {
		t.Fatalf("tracked users = %d, want 0 after churn shutdown", userCount)
	}

	closed := 0
	for _, p := range participants {
		if p.closeCalls > 0 {
			closed++
		}
	}
	if closed == 0 {
		t.Fatal("no churn participants were closed")
	}
}

func TestChurnScenarioMediaToggleSenders(t *testing.T) {
	plugin := &fakeVideoParticipantPlugin{}
	client := newTestClient("fake", func() Plugin { return plugin })
	client.env.config.Spec.Conference.Cameras.PerRoom = 1
	client.env.config.Spec.ScenarioConfig = map[string]any{
		"mode":            string(churnModeMediaToggle),
		"sessionDuration": "20ms",
		"rejoinDelay":     "5ms",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 85*time.Millisecond)
	defer cancel()

	if err := client.RunScenario(ctx, ChurnScenarioID); err != nil {
		t.Fatalf("RunScenario(churn) error = %v", err)
	}

	client.mu.Lock()
	userCount := len(client.users)
	client.mu.Unlock()
	if userCount != 0 {
		t.Fatalf("tracked users = %d, want 0 after churn shutdown", userCount)
	}

	plugin.mu.Lock()
	defer plugin.mu.Unlock()
	if len(plugin.participants) != 1 {
		t.Fatalf("participants = %d, want 1 joined sender connection", len(plugin.participants))
	}
	p := plugin.participants[0]
	if p.joinCalls != 1 {
		t.Fatalf("joinCalls = %d, want 1", p.joinCalls)
	}
	if p.publishCalls == 0 {
		t.Fatal("publishCalls = 0, want at least one publish")
	}
	if p.unpublishCalls == 0 {
		t.Fatal("unpublishCalls = 0, want at least one unpublish")
	}
	if p.closeCalls != 1 {
		t.Fatalf("closeCalls = %d, want 1", p.closeCalls)
	}
}
