package rtcbench

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	pkglog "github.com/rtcbench/rtcbench/pkg/log"
)

func newTestClient(pluginID string, factory PluginFactory) *Client {
	cfg := newTestConfig(pluginID)
	reg := pkglog.NewRegistry(nil, nil)
	client := NewClient(cfg, reg)
	client.RegisterPlugin(pluginID, factory)
	return client
}

func mustCreateUser(t *testing.T, client *Client, userID string, role UserRole) *User {
	t.Helper()
	user, err := client.CreateUser(context.Background(), &UserConfig{
		UserID: userID,
		Role:   role,
	})
	if err != nil {
		t.Fatalf("CreateUser(%q) error = %v", userID, err)
	}
	return user
}

func newTestConfig(pluginID string) *Config {
	return &Config{
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
}

func containsAll(s string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(s, needle) {
			return false
		}
	}
	return true
}

func assertCounterValue(t *testing.T, snapshot RunMetricsSnapshot, labels Labels, op, outcome string, want int64) {
	t.Helper()
	got := snapshot.CounterValue(metricOperationTotal, withLabel(withLabel(labels, metricLabelOp, op), metricLabelOutcome, outcome))
	if got != want {
		t.Fatalf("counter %s/%s labels=%v = %d, want %d", op, outcome, labels, got, want)
	}
}

func float64Ptr(v float64) *float64 {
	return &v
}

func durationPtr(v time.Duration) *time.Duration {
	return &v
}

func stringPtr(v string) *string {
	return &v
}

// fakeParticipantPlugin is a plugin whose participants do not support video.
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

// fakeVideoParticipantPlugin is a plugin whose participants support video publish/unpublish.
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

// blockingLeaveParticipantPlugin produces participants whose LeaveRoom blocks until ctx is done.
type blockingLeaveParticipantPlugin struct {
	mu           sync.Mutex
	participants []*blockingLeaveParticipant
}

func (p *blockingLeaveParticipantPlugin) Setup(ctx context.Context, e PluginEnv) error {
	return nil
}

func (p *blockingLeaveParticipantPlugin) Shutdown(ctx context.Context) error {
	return nil
}

func (p *blockingLeaveParticipantPlugin) NewParticipant(ctx context.Context, cfg *UserConfig) (Participant, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	part := &blockingLeaveParticipant{userID: cfg.UserID}
	p.participants = append(p.participants, part)
	return part, nil
}

type blockingLeaveParticipant struct {
	userID     string
	joinCalls  int
	leaveCalls int
	closeCalls int
}

func (p *blockingLeaveParticipant) JoinRoom(ctx context.Context, req *JoinRequest) error {
	p.joinCalls++
	return nil
}

func (p *blockingLeaveParticipant) LeaveRoom(ctx context.Context, req *LeaveRequest) error {
	p.leaveCalls++
	<-ctx.Done()
	return ctx.Err()
}

func (p *blockingLeaveParticipant) Close() error {
	p.closeCalls++
	return nil
}

// fakeScenario records Run calls and captures the ScenarioEnv.
type fakeScenario struct {
	runCalls int
	env      ScenarioEnv
}

func (s *fakeScenario) Run(ctx context.Context, env ScenarioEnv) error {
	s.runCalls++
	s.env = env
	return nil
}
