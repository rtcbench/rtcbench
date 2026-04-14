package rtcbench

import (
	"context"
	"testing"
)

func TestClientMetricsSnapshotTracksRunMetrics(t *testing.T) {
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

	snapshot := client.MetricsSnapshot()
	labels := Labels{
		metricLabelScenario: manualScenarioLabel,
		metricLabelPlugin:   "fake",
		metricLabelRole:     "sender",
	}

	assertCounterValue(t, snapshot, labels, "join", metricOutcomeAttempt, 1)
	assertCounterValue(t, snapshot, labels, "join", metricOutcomeSuccess, 1)
	assertCounterValue(t, snapshot, labels, "publish_video", metricOutcomeAttempt, 1)
	assertCounterValue(t, snapshot, labels, "publish_video", metricOutcomeSuccess, 1)
	assertCounterValue(t, snapshot, labels, "unpublish_video", metricOutcomeAttempt, 1)
	assertCounterValue(t, snapshot, labels, "unpublish_video", metricOutcomeSuccess, 1)
	assertCounterValue(t, snapshot, labels, "leave", metricOutcomeAttempt, 1)
	assertCounterValue(t, snapshot, labels, "leave", metricOutcomeSuccess, 1)

	closeLabels := Labels{
		metricLabelScenario: manualScenarioLabel,
		metricLabelPlugin:   "fake",
		metricLabelRole:     "sender",
	}
	assertCounterValue(t, snapshot, closeLabels, "close", metricOutcomeAttempt, 1)
	assertCounterValue(t, snapshot, closeLabels, "close", metricOutcomeSuccess, 1)

	histogram := snapshot.Histogram(metricOperationDuration, withLabel(labels, metricLabelOp, "join"))
	if histogram.Count != 1 {
		t.Fatalf("join histogram count = %d, want 1", histogram.Count)
	}
	if histogram.Max <= 0 {
		t.Fatalf("join histogram max = %f, want > 0", histogram.Max)
	}

	sessionHistogram := snapshot.Histogram(metricSessionDuration, labels)
	if sessionHistogram.Count != 1 {
		t.Fatalf("session histogram count = %d, want 1", sessionHistogram.Count)
	}
	if sessionHistogram.Max <= 0 {
		t.Fatalf("session histogram max = %f, want > 0", sessionHistogram.Max)
	}
}

func TestClientMetricsSnapshotTracksActiveStateGauges(t *testing.T) {
	plugin := &fakeVideoParticipantPlugin{}
	client := newTestClient("fake", func() Plugin { return plugin })

	user := mustCreateUser(t, client, "alice", Sender)
	if err := user.JoinRoom(context.Background(), &JoinRequest{
		Plugin: "fake",
		RoomID: "room-1",
	}); err != nil {
		t.Fatalf("JoinRoom() error = %v", err)
	}

	snapshot := client.MetricsSnapshot()
	roleLabels := Labels{
		metricLabelScenario: manualScenarioLabel,
		metricLabelPlugin:   "fake",
		metricLabelRole:     "sender",
	}
	pluginLabels := Labels{
		metricLabelScenario: manualScenarioLabel,
		metricLabelPlugin:   "fake",
	}
	if got := snapshot.GaugeValue(metricUsersActive, roleLabels); got != 1 {
		t.Fatalf("users_active = %f, want 1", got)
	}
	if got := snapshot.GaugeValue(metricConnectionsJoined, roleLabels); got != 1 {
		t.Fatalf("connections_joined = %f, want 1", got)
	}
	if got := snapshot.GaugeValue(metricRoomsActive, pluginLabels); got != 1 {
		t.Fatalf("rooms_active = %f, want 1", got)
	}
	if got := snapshot.GaugeValue(metricPublishersActive, pluginLabels); got != 0 {
		t.Fatalf("publishers_active before publish = %f, want 0", got)
	}

	if err := user.PublishVideo(context.Background(), &PublishVideoRequest{
		Plugin: "fake",
		RoomID: "room-1",
	}); err != nil {
		t.Fatalf("PublishVideo() error = %v", err)
	}

	snapshot = client.MetricsSnapshot()
	if got := snapshot.GaugeValue(metricPublishersActive, pluginLabels); got != 1 {
		t.Fatalf("publishers_active after publish = %f, want 1", got)
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

	snapshot = client.MetricsSnapshot()
	if got := snapshot.GaugeValue(metricPublishersActive, pluginLabels); got != 0 {
		t.Fatalf("publishers_active after leave = %f, want 0", got)
	}
	if got := snapshot.GaugeValue(metricUsersActive, roleLabels); got != 0 {
		t.Fatalf("users_active after leave = %f, want 0", got)
	}
	if got := snapshot.GaugeValue(metricConnectionsJoined, roleLabels); got != 0 {
		t.Fatalf("connections_joined after leave = %f, want 0", got)
	}
	if got := snapshot.GaugeValue(metricRoomsActive, pluginLabels); got != 0 {
		t.Fatalf("rooms_active after leave = %f, want 0", got)
	}
	if got := snapshot.GaugePeakValue(metricUsersActive, roleLabels); got != 1 {
		t.Fatalf("users_active peak = %f, want 1", got)
	}
	if got := snapshot.GaugePeakValue(metricPublishersActive, pluginLabels); got != 1 {
		t.Fatalf("publishers_active peak = %f, want 1", got)
	}
}

func TestJoinAllRoomsMetricsUseDefaultScenarioLabel(t *testing.T) {
	plugin := &fakeParticipantPlugin{}
	client := newTestClient("fake", func() Plugin { return plugin })

	if err := client.JoinAllRooms(context.Background()); err != nil {
		t.Fatalf("JoinAllRooms() error = %v", err)
	}

	snapshot := client.MetricsSnapshot()
	assertCounterValue(t, snapshot, Labels{
		metricLabelScenario: DefaultScenarioID,
		metricLabelPlugin:   "fake",
		metricLabelRole:     "viewer",
	}, "join", metricOutcomeSuccess, 1)
}

func TestClientMetricsSummaryAggregatesOperationsAndGauges(t *testing.T) {
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
	if err := user.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	summary := client.MetricsSummary()
	if len(summary.Operations) == 0 {
		t.Fatal("summary.Operations = 0, want operation summaries")
	}
	if len(summary.Gauges) == 0 {
		t.Fatal("summary.Gauges = 0, want gauge summaries")
	}
	if len(summary.Sessions) == 0 {
		t.Fatal("summary.Sessions = 0, want session summaries")
	}

	var foundJoin bool
	for _, op := range summary.Operations {
		if op.Operation == "join" && op.Plugin == "fake" && op.Role == "sender" && op.Scenario == manualScenarioLabel {
			foundJoin = true
			if op.Attempts != 1 || op.Successes != 1 || op.Failures != 0 {
				t.Fatalf("join summary = %+v, want one successful join", op)
			}
			if op.P95Seconds <= 0 {
				t.Fatalf("join summary p95 = %f, want > 0", op.P95Seconds)
			}
		}
	}
	if !foundJoin {
		t.Fatal("did not find join operation summary")
	}

	var foundPublishersGauge bool
	for _, gauge := range summary.Gauges {
		if gauge.Name == metricPublishersActive && gauge.Plugin == "fake" && gauge.Scenario == manualScenarioLabel {
			foundPublishersGauge = true
			if gauge.Current != 0 {
				t.Fatalf("publishers gauge current = %f, want 0 after close", gauge.Current)
			}
			if gauge.Peak != 1 {
				t.Fatalf("publishers gauge peak = %f, want 1", gauge.Peak)
			}
		}
	}
	if !foundPublishersGauge {
		t.Fatal("did not find publishers_active gauge summary")
	}
}
