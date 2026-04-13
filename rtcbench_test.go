package rtcbench

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	pkglog "github.com/rtcbench/rtcbench/pkg/log"
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

func TestUserJoinRoomUsesParticipantFactory(t *testing.T) {
	plugin := &fakeParticipantPlugin{}
	client := newTestClient("fake", func() Plugin { return plugin })

	user := client.CreateUser(context.Background(), &UserConfig{
		UserID: "alice",
		Role:   Viewer,
	})

	err := user.JoinRoom(context.Background(), &JoinRequest{
		Plugin: "fake",
		RoomID: "room-1",
	})
	if err != nil {
		t.Fatalf("JoinRoom() error = %v", err)
	}

	if plugin.setupCalls != 1 {
		t.Fatalf("Setup() calls = %d, want 1", plugin.setupCalls)
	}
	if plugin.newParticipantCalls != 1 {
		t.Fatalf("NewParticipant() calls = %d, want 1", plugin.newParticipantCalls)
	}
	if len(plugin.participants) != 1 {
		t.Fatalf("participants = %d, want 1", len(plugin.participants))
	}

	p := plugin.participants[0]
	if p.joinCalls != 1 {
		t.Fatalf("participant JoinRoom() calls = %d, want 1", p.joinCalls)
	}
	if p.joinReq == nil || p.joinReq.RoomID != "room-1" || p.joinReq.Plugin != "fake" {
		t.Fatalf("participant JoinRoom() request = %+v, want room-1/fake", p.joinReq)
	}

	if got := len(user.connections); got != 1 {
		t.Fatalf("user connections = %d, want 1", got)
	}
	for _, conn := range user.connections {
		if conn.state != StateJoined {
			t.Fatalf("connection state = %v, want StateJoined", conn.state)
		}
	}
}

func TestUserLeaveRoomAndCloseAreIdempotent(t *testing.T) {
	plugin := &fakeParticipantPlugin{}
	client := newTestClient("fake", func() Plugin { return plugin })

	user := client.CreateUser(context.Background(), &UserConfig{
		UserID: "alice",
		Role:   Viewer,
	})

	err := user.JoinRoom(context.Background(), &JoinRequest{
		Plugin: "fake",
		RoomID: "room-1",
	})
	if err != nil {
		t.Fatalf("JoinRoom() error = %v", err)
	}

	err = user.LeaveRoom(context.Background(), &LeaveRequest{
		Plugin: "fake",
		RoomID: "room-1",
	})
	if err != nil {
		t.Fatalf("LeaveRoom() error = %v", err)
	}

	err = user.LeaveRoom(context.Background(), &LeaveRequest{
		Plugin: "fake",
		RoomID: "room-1",
	})
	if err != nil {
		t.Fatalf("second LeaveRoom() error = %v", err)
	}

	p := plugin.participants[0]
	if p.leaveCalls != 1 {
		t.Fatalf("participant LeaveRoom() calls = %d, want 1", p.leaveCalls)
	}

	if err := user.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := user.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}

	if p.closeCalls != 1 {
		t.Fatalf("participant Close() calls = %d, want 1", p.closeCalls)
	}
}

func TestUserCanRejoinAfterLeave(t *testing.T) {
	plugin := &fakeParticipantPlugin{}
	client := newTestClient("fake", func() Plugin { return plugin })

	user := client.CreateUser(context.Background(), &UserConfig{
		UserID: "alice",
		Role:   Viewer,
	})

	joinReq := &JoinRequest{
		Plugin: "fake",
		RoomID: "room-1",
	}
	if err := user.JoinRoom(context.Background(), joinReq); err != nil {
		t.Fatalf("first JoinRoom() error = %v", err)
	}
	if err := user.LeaveRoom(context.Background(), &LeaveRequest{
		Plugin: "fake",
		RoomID: "room-1",
	}); err != nil {
		t.Fatalf("LeaveRoom() error = %v", err)
	}
	if err := user.JoinRoom(context.Background(), joinReq); err != nil {
		t.Fatalf("second JoinRoom() error = %v", err)
	}

	if plugin.newParticipantCalls != 2 {
		t.Fatalf("NewParticipant() calls = %d, want 2", plugin.newParticipantCalls)
	}
	if len(plugin.participants) != 2 {
		t.Fatalf("participants = %d, want 2", len(plugin.participants))
	}

	first := plugin.participants[0]
	second := plugin.participants[1]
	if first.leaveCalls != 1 {
		t.Fatalf("first participant LeaveRoom() calls = %d, want 1", first.leaveCalls)
	}
	if first.closeCalls != 1 {
		t.Fatalf("first participant Close() calls = %d, want 1", first.closeCalls)
	}
	if second.joinCalls != 1 {
		t.Fatalf("second participant JoinRoom() calls = %d, want 1", second.joinCalls)
	}
	if got := len(user.connections); got != 1 {
		t.Fatalf("user connections = %d, want 1", got)
	}
	for _, conn := range user.connections {
		if conn.dp != second {
			t.Fatal("active connection did not swap to rejoined participant")
		}
		if conn.state != StateJoined {
			t.Fatalf("active connection state = %v, want StateJoined", conn.state)
		}
	}
}

func TestClientShutdownAllClosesUsersAndPlugins(t *testing.T) {
	plugin := &fakeParticipantPlugin{}
	client := newTestClient("fake", func() Plugin { return plugin })

	user := client.CreateUser(context.Background(), &UserConfig{
		UserID: "alice",
		Role:   Viewer,
	})

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

func TestJoinAllRoomsSupportsLegacyPlugins(t *testing.T) {
	plugin := &fakeLegacyPlugin{}
	client := newTestClient("legacy", func() Plugin { return plugin })
	client.env.config.Spec.Conference.UsersPerRoom = 3

	err := client.JoinAllRooms(context.Background())
	if err != nil {
		t.Fatalf("JoinAllRooms() error = %v", err)
	}

	if plugin.setupCalls != 1 {
		t.Fatalf("Setup() calls = %d, want 1", plugin.setupCalls)
	}
	if plugin.joinCalls != 3 {
		t.Fatalf("JoinRoom() calls = %d, want 3", plugin.joinCalls)
	}
}

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

func TestUserCloseRemovesUserFromClient(t *testing.T) {
	plugin := &fakeParticipantPlugin{}
	client := newTestClient("fake", func() Plugin { return plugin })

	user := client.CreateUser(context.Background(), &UserConfig{
		UserID: "alice",
		Role:   Viewer,
	})
	if client.GetUser("alice") == nil {
		t.Fatal("GetUser() = nil, want tracked user")
	}

	if err := user.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if got := client.GetUser("alice"); got != nil {
		t.Fatalf("GetUser() = %#v, want nil after close", got)
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

func TestUserPublishVideoTargetsVideoPublisher(t *testing.T) {
	plugin := &fakeVideoParticipantPlugin{}
	client := newTestClient("fake", func() Plugin { return plugin })

	user := client.CreateUser(context.Background(), &UserConfig{
		UserID: "alice",
		Role:   Sender,
	})
	if err := user.JoinRoom(context.Background(), &JoinRequest{
		Plugin: "fake",
		RoomID: "room-1",
	}); err != nil {
		t.Fatalf("JoinRoom() error = %v", err)
	}

	if err := user.PublishVideo(context.Background(), &PublishVideoRequest{
		Plugin: "fake",
		RoomID: "room-1",
	}); err != nil {
		t.Fatalf("PublishVideo() error = %v", err)
	}
	if err := user.UnpublishVideo(context.Background(), &UnpublishVideoRequest{
		Plugin: "fake",
		RoomID: "room-1",
	}); err != nil {
		t.Fatalf("UnpublishVideo() error = %v", err)
	}

	plugin.mu.Lock()
	defer plugin.mu.Unlock()
	if len(plugin.participants) != 1 {
		t.Fatalf("participants = %d, want 1", len(plugin.participants))
	}
	p := plugin.participants[0]
	if p.publishCalls != 1 {
		t.Fatalf("publishCalls = %d, want 1", p.publishCalls)
	}
	if p.unpublishCalls != 1 {
		t.Fatalf("unpublishCalls = %d, want 1", p.unpublishCalls)
	}
}

func TestUserPublishVideoUnsupportedCapability(t *testing.T) {
	plugin := &fakeParticipantPlugin{}
	client := newTestClient("fake", func() Plugin { return plugin })

	user := client.CreateUser(context.Background(), &UserConfig{
		UserID: "alice",
		Role:   Viewer,
	})
	if err := user.JoinRoom(context.Background(), &JoinRequest{
		Plugin: "fake",
		RoomID: "room-1",
	}); err != nil {
		t.Fatalf("JoinRoom() error = %v", err)
	}

	err := user.PublishVideo(context.Background(), &PublishVideoRequest{
		Plugin: "fake",
		RoomID: "room-1",
	})
	if !errors.Is(err, ErrUnsupportedCapability) {
		t.Fatalf("PublishVideo() error = %v, want ErrUnsupportedCapability", err)
	}
}

func newTestClient(pluginID string, factory PluginFactory) *Client {
	cfg := &Config{
		Spec: SpecConfig{
			Plugin: pluginID,
			Conference: ConferenceConfig{
				Name:         "room-1000",
				UsersPerRoom: 1,
				TotalRooms:   1,
				Cameras: CameraConfig{
					PerRoom: 0,
				},
				JoinPolicy: JoinPolicyConfig{
					Concurrency: 1,
				},
			},
			Network: NetworkConfig{
				ServerIP: "127.0.0.1",
				ClientIP: "127.0.0.1",
			},
		},
	}
	reg := pkglog.NewRegistry(nil, nil)
	client := NewClient(cfg, reg)
	client.RegisterPlugin(pluginID, factory)
	return client
}

type fakeParticipantPlugin struct {
	mu                  sync.Mutex
	setupCalls          int
	shutdownCalls       int
	newParticipantCalls int
	participants        []*fakeParticipant
}

func (p *fakeParticipantPlugin) Setup(ctx context.Context, e PluginEnv) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.setupCalls++
	return nil
}

func (p *fakeParticipantPlugin) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.shutdownCalls++
	return nil
}

func (p *fakeParticipantPlugin) JoinRoom(ctx context.Context, role UserRole, roomID, userID string) error {
	return errors.New("legacy JoinRoom should not be used")
}

func (p *fakeParticipantPlugin) NewParticipant(ctx context.Context, cfg *UserConfig) (Participant, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.newParticipantCalls++
	part := &fakeParticipant{userID: cfg.UserID}
	p.participants = append(p.participants, part)
	return part, nil
}

type fakeParticipant struct {
	userID     string
	joinCalls  int
	leaveCalls int
	closeCalls int
	joinReq    *JoinRequest
	leaveReq   *LeaveRequest
}

func (p *fakeParticipant) JoinRoom(ctx context.Context, req *JoinRequest) error {
	p.joinCalls++
	cp := *req
	p.joinReq = &cp
	return nil
}

func (p *fakeParticipant) LeaveRoom(ctx context.Context, req *LeaveRequest) error {
	p.leaveCalls++
	if req != nil {
		cp := *req
		p.leaveReq = &cp
	}
	return nil
}

func (p *fakeParticipant) Close() error {
	p.closeCalls++
	return nil
}

type fakeLegacyPlugin struct {
	setupCalls    int
	shutdownCalls int
	joinCalls     int
}

func (p *fakeLegacyPlugin) Setup(ctx context.Context, e PluginEnv) error {
	p.setupCalls++
	return nil
}

func (p *fakeLegacyPlugin) Shutdown(ctx context.Context) error {
	p.shutdownCalls++
	return nil
}

func (p *fakeLegacyPlugin) JoinRoom(ctx context.Context, role UserRole, roomID, userID string) error {
	p.joinCalls++
	return nil
}

type fakeVideoParticipantPlugin struct {
	mu                  sync.Mutex
	setupCalls          int
	shutdownCalls       int
	newParticipantCalls int
	participants        []*fakeVideoParticipant
}

func (p *fakeVideoParticipantPlugin) Setup(ctx context.Context, e PluginEnv) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.setupCalls++
	return nil
}

func (p *fakeVideoParticipantPlugin) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.shutdownCalls++
	return nil
}

func (p *fakeVideoParticipantPlugin) JoinRoom(ctx context.Context, role UserRole, roomID, userID string) error {
	return errors.New("legacy JoinRoom should not be used")
}

func (p *fakeVideoParticipantPlugin) NewParticipant(ctx context.Context, cfg *UserConfig) (Participant, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.newParticipantCalls++
	part := &fakeVideoParticipant{userID: cfg.UserID}
	p.participants = append(p.participants, part)
	return part, nil
}

type fakeVideoParticipant struct {
	userID         string
	joinCalls      int
	leaveCalls     int
	closeCalls     int
	publishCalls   int
	unpublishCalls int
}

func (p *fakeVideoParticipant) JoinRoom(ctx context.Context, req *JoinRequest) error {
	p.joinCalls++
	return nil
}

func (p *fakeVideoParticipant) LeaveRoom(ctx context.Context, req *LeaveRequest) error {
	p.leaveCalls++
	return nil
}

func (p *fakeVideoParticipant) Close() error {
	p.closeCalls++
	return nil
}

func (p *fakeVideoParticipant) PublishVideo(ctx context.Context, req *PublishVideoRequest) error {
	p.publishCalls++
	return nil
}

func (p *fakeVideoParticipant) UnpublishVideo(ctx context.Context, req *UnpublishVideoRequest) error {
	p.unpublishCalls++
	return nil
}

type fakeScenario struct {
	runCalls int
	env      ScenarioEnv
}

func (s *fakeScenario) Run(ctx context.Context, env ScenarioEnv) error {
	s.runCalls++
	s.env = env
	return nil
}
