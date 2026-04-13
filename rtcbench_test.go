package rtcbench

import (
	"context"
	"errors"
	"testing"

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
	setupCalls          int
	shutdownCalls       int
	newParticipantCalls int
	participants        []*fakeParticipant
}

func (p *fakeParticipantPlugin) Setup(ctx context.Context, e PluginEnv) error {
	p.setupCalls++
	return nil
}

func (p *fakeParticipantPlugin) Shutdown(ctx context.Context) error {
	p.shutdownCalls++
	return nil
}

func (p *fakeParticipantPlugin) JoinRoom(ctx context.Context, role UserRole, roomID, userID string) error {
	return errors.New("legacy JoinRoom should not be used")
}

func (p *fakeParticipantPlugin) NewParticipant(ctx context.Context, cfg *UserConfig) (Participant, error) {
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
