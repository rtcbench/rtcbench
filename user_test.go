package rtcbench

import (
	"context"
	"errors"
	"testing"

	pkglog "github.com/rtcbench/rtcbench/pkg/log"
)

func TestUserJoinRoomUsesParticipantFactory(t *testing.T) {
	plugin := &fakeParticipantPlugin{}
	client := newTestClient("fake", func() Plugin { return plugin })

	user := mustCreateUser(t, client, "alice", Viewer)

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

	user := mustCreateUser(t, client, "alice", Viewer)

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

	user := mustCreateUser(t, client, "alice", Viewer)

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

func TestUserCannotJoinSameConnectionTwice(t *testing.T) {
	plugin := &fakeParticipantPlugin{}
	client := newTestClient("fake", func() Plugin { return plugin })

	user := mustCreateUser(t, client, "alice", Viewer)

	joinReq := &JoinRequest{
		Plugin: "fake",
		RoomID: "room-1",
	}
	if err := user.JoinRoom(context.Background(), joinReq); err != nil {
		t.Fatalf("first JoinRoom() error = %v", err)
	}

	err := user.JoinRoom(context.Background(), joinReq)
	if !errors.Is(err, ErrConnectionExists) {
		t.Fatalf("second JoinRoom() error = %v, want ErrConnectionExists", err)
	}

	if plugin.newParticipantCalls != 1 {
		t.Fatalf("NewParticipant() calls = %d, want 1", plugin.newParticipantCalls)
	}
}

func TestUserCloseRemovesUserFromClient(t *testing.T) {
	plugin := &fakeParticipantPlugin{}
	client := newTestClient("fake", func() Plugin { return plugin })

	user := mustCreateUser(t, client, "alice", Viewer)
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

func TestUserPublishVideoTargetsVideoPublisher(t *testing.T) {
	plugin := &fakeVideoParticipantPlugin{}
	client := newTestClient("fake", func() Plugin { return plugin })

	user := mustCreateUser(t, client, "alice", Sender)
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

	user := mustCreateUser(t, client, "alice", Viewer)
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

func TestUserPublishVideoBeforeJoinReturnsConnectionNotJoined(t *testing.T) {
	plugin := &fakeVideoParticipantPlugin{}
	client := newTestClient("fake", func() Plugin { return plugin })

	user := mustCreateUser(t, client, "alice", Sender)

	err := user.PublishVideo(context.Background(), &PublishVideoRequest{
		Plugin: "fake",
		RoomID: "room-1",
	})
	if !errors.Is(err, ErrConnectionNotJoined) {
		t.Fatalf("PublishVideo() error = %v, want ErrConnectionNotJoined", err)
	}
}

func TestUserPublishVideoAfterLeaveReturnsConnectionNotJoined(t *testing.T) {
	plugin := &fakeVideoParticipantPlugin{}
	client := newTestClient("fake", func() Plugin { return plugin })

	user := mustCreateUser(t, client, "alice", Sender)
	if err := user.JoinRoom(context.Background(), &JoinRequest{
		Plugin: "fake",
		RoomID: "room-1",
	}); err != nil {
		t.Fatalf("JoinRoom() error = %v", err)
	}
	if err := user.LeaveRoom(context.Background(), &LeaveRequest{
		Plugin: "fake",
		RoomID: "room-1",
	}); err != nil {
		t.Fatalf("LeaveRoom() error = %v", err)
	}

	err := user.PublishVideo(context.Background(), &PublishVideoRequest{
		Plugin: "fake",
		RoomID: "room-1",
	})
	if !errors.Is(err, ErrConnectionNotJoined) {
		t.Fatalf("PublishVideo() error = %v, want ErrConnectionNotJoined", err)
	}
}

func TestUserPublishVideoAfterCloseReturnsUserClosed(t *testing.T) {
	plugin := &fakeVideoParticipantPlugin{}
	client := newTestClient("fake", func() Plugin { return plugin })

	user := mustCreateUser(t, client, "alice", Sender)
	if err := user.JoinRoom(context.Background(), &JoinRequest{
		Plugin: "fake",
		RoomID: "room-1",
	}); err != nil {
		t.Fatalf("JoinRoom() error = %v", err)
	}
	if err := user.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	err := user.PublishVideo(context.Background(), &PublishVideoRequest{
		Plugin: "fake",
		RoomID: "room-1",
	})
	if !errors.Is(err, ErrUserClosed) {
		t.Fatalf("PublishVideo() error = %v, want ErrUserClosed", err)
	}
}

func TestUserMetricsSnapshotTracksLifecycle(t *testing.T) {
	plugin := &fakeVideoParticipantPlugin{}
	client := newTestClient("fake", func() Plugin { return plugin })

	user := mustCreateUser(t, client, "alice", Sender)
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
	if err := user.LeaveRoom(context.Background(), &LeaveRequest{
		Plugin: "fake",
		RoomID: "room-1",
	}); err != nil {
		t.Fatalf("LeaveRoom() error = %v", err)
	}
	if err := user.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	metrics := user.MetricsSnapshot()
	if metrics.JoinAttempts != 1 || metrics.JoinSuccesses != 1 || metrics.JoinFailures != 0 {
		t.Fatalf("join metrics = %+v, want 1 success and 0 failures", metrics)
	}
	if metrics.PublishVideoAttempts != 1 || metrics.PublishVideoSuccesses != 1 || metrics.PublishVideoFailures != 0 {
		t.Fatalf("publish metrics = %+v, want 1 success and 0 failures", metrics)
	}
	if metrics.UnpublishVideoAttempts != 1 || metrics.UnpublishVideoSuccesses != 1 || metrics.UnpublishVideoFailures != 0 {
		t.Fatalf("unpublish metrics = %+v, want 1 success and 0 failures", metrics)
	}
	if metrics.LeaveAttempts != 1 || metrics.LeaveSuccesses != 1 || metrics.LeaveFailures != 0 {
		t.Fatalf("leave metrics = %+v, want 1 success and 0 failures", metrics)
	}
	if metrics.CloseAttempts != 1 || metrics.CloseSuccesses != 1 || metrics.CloseFailures != 0 {
		t.Fatalf("close metrics = %+v, want 1 success and 0 failures", metrics)
	}
	if metrics.JoinLatencyTotal <= 0 || metrics.PublishVideoLatencyTotal <= 0 || metrics.CloseLatencyTotal <= 0 {
		t.Fatalf("latency totals = %+v, want positive durations", metrics)
	}
}

func TestUserLifecycleLoggingIncludesPluginAndRoom(t *testing.T) {
	capture := pkglog.NewCaptureHandler()
	reg := pkglog.NewRegistry([]pkglog.Handler{capture}, map[string]pkglog.Level{
		"general": pkglog.LevelInfo,
	})
	client := NewClient(newTestConfig("fake"), reg)
	client.RegisterPlugin("fake", func() Plugin { return &fakeVideoParticipantPlugin{} })

	user := mustCreateUser(t, client, "alice", Sender)
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

	records := capture.Filter("general", pkglog.LevelInfo)
	if len(records) == 0 {
		t.Fatal("captured records = 0, want lifecycle logs")
	}

	foundJoin := false
	foundPublish := false
	for _, record := range records {
		if record.Prefix != "[alice]" {
			continue
		}
		if containsAll(record.Msg, "[user] join ok", "plugin=fake", "room=room-1") {
			foundJoin = true
		}
		if containsAll(record.Msg, "[user] publish-video ok", "plugin=fake", "room=room-1") {
			foundPublish = true
		}
	}
	if !foundJoin {
		t.Fatal("did not find join lifecycle log with plugin and room")
	}
	if !foundPublish {
		t.Fatal("did not find publish lifecycle log with plugin and room")
	}
}
