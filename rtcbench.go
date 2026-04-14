package rtcbench

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/rtcbench/rtcbench/pkg/ivf"
	"github.com/rtcbench/rtcbench/pkg/log"
	"github.com/rtcbench/rtcbench/pkg/viewer"
	"github.com/rtcbench/rtcbench/pkg/vp9_stats"
)

type UserRole string

const (
	Viewer UserRole = "viewer"
	Sender UserRole = "sender"
)

var (
	ErrUnsupportedRole       = errors.New("unsupported role")
	ErrUnsupportedCapability = errors.New("unsupported capability")
	ErrCannotJoinRoom        = errors.New("cannot join room")
	ErrConnectionNotJoined   = errors.New("connection not joined")
	ErrUnknownPlugin         = errors.New("unknown plugin")
	ErrUnknownScenario       = errors.New("unknown scenario")
	ErrConnectionExists      = errors.New("connection already exists")
	ErrUserExists            = errors.New("user already exists")
	ErrUserClosed            = errors.New("user is closed")
	ErrMissingRoomID         = errors.New("missing room id")
)

type Plugin interface {
	Setup(ctx context.Context, e PluginEnv) error
	Shutdown(ctx context.Context) error
	NewParticipant(ctx context.Context, cfg *UserConfig) (Participant, error)
}

type PluginEnv interface {
	Config() *Config
	LogRegistry() *log.Registry
	StatsConsumers() []func(vp9_stats.Period, vp9_stats.VideoQualitySample)
}

// StatsPipeline encapsulates the viewer manager and stats publisher that every
// plugin creates identically in Setup(). Use NewStatsPipeline in Setup and
// call Stop in Shutdown.
type StatsPipeline struct {
	ViewerManager *viewer.Manager
	Publisher     *vp9_stats.Publisher
}

// NewStatsPipeline builds the stats channel, viewer manager, publisher, default
// log subscriber, and external consumers. It starts the publisher goroutine.
func NewStatsPipeline(e PluginEnv) *StatsPipeline {
	input := make(chan vp9_stats.VideoQualitySample, e.Config().Spec.Conference.StatsInputChanSize)
	statsLog := e.LogRegistry().NewLogger("video_stats", "")
	vm := viewer.NewManager(input, statsLog)
	pub := vp9_stats.NewPublisher(input)
	pub.AddSubscriber(func(period vp9_stats.Period, sample vp9_stats.VideoQualitySample) {
		statsLog.Infof("bitrate=%s,period=%s,sample=%s", sample.Mbps(), period.String(), sample.String())
	})
	for _, consumer := range e.StatsConsumers() {
		pub.AddSubscriber(consumer)
	}
	go pub.Run()
	return &StatsPipeline{ViewerManager: vm, Publisher: pub}
}

// Stop shuts down the viewer manager and publisher in the correct order.
func (sp *StatsPipeline) Stop() {
	if sp.ViewerManager != nil {
		sp.ViewerManager.StopAll()
	}
	if sp.Publisher != nil {
		sp.Publisher.Stop()
	}
}

// SetupPacketCaptureDir creates a timestamped directory for packet captures
// under the configured base directory. Returns the path, or empty string if
// packet capture is disabled.
func SetupPacketCaptureDir(cfg PacketCaptureConfig) (string, error) {
	if !cfg.Enabled {
		return "", nil
	}
	ts := time.Now().UTC().Format("2006-01-02T15-04-05Z")
	dir := filepath.Join(cfg.Directory, ts)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("packet capture directory: %w", err)
	}
	return dir, nil
}

// LoadCamerasFromConfig loads IVF camera files when senders are configured.
// Returns nil when perRoom is 0.
func LoadCamerasFromConfig(cfg CameraConfig) (*ivf.Cameras, error) {
	if cfg.PerRoom <= 0 {
		return nil, nil
	}
	return ivf.NewCameras(cfg.Directory, cfg.InMemory)
}

type PluginFactory func() Plugin

type PluginRegistry map[string]PluginFactory

type ScenarioFactory func() Scenario

type ScenarioRegistry map[string]ScenarioFactory

type pluginEnv struct {
	config         *Config
	logRegistry    *log.Registry
	statsConsumers []func(vp9_stats.Period, vp9_stats.VideoQualitySample)
}

func (e *pluginEnv) Config() *Config {
	return e.config
}

func (e *pluginEnv) LogRegistry() *log.Registry {
	return e.logRegistry
}

func (e *pluginEnv) StatsConsumers() []func(vp9_stats.Period, vp9_stats.VideoQualitySample) {
	return e.statsConsumers
}

type Client struct {
	mu             sync.Mutex
	env            *pluginEnv
	registry       PluginRegistry
	scenarios      ScenarioRegistry
	plugins        map[string]Plugin
	users          map[string]*User
	metrics        *runMetricsCollector
	activeScenario string
	log            *log.Logger
}

func NewClient(config *Config, logRegistry *log.Registry) *Client {
	client := &Client{
		env: &pluginEnv{
			config:      config,
			logRegistry: logRegistry,
		},
		registry:  make(PluginRegistry),
		scenarios: make(ScenarioRegistry),
		plugins:   make(map[string]Plugin),
		users:     make(map[string]*User),
		metrics:   newRunMetricsCollector(),
		log:       logRegistry.NewLogger("general", ""),
	}
	client.RegisterScenario(DefaultScenarioID, func() Scenario { return DefaultScenario{} })
	client.RegisterScenario(ChurnScenarioID, func() Scenario { return ChurnScenario{} })
	return client
}

func (c *Client) AddStatsConsumer(fn func(vp9_stats.Period, vp9_stats.VideoQualitySample)) {
	c.env.statsConsumers = append(c.env.statsConsumers, fn)
}

func (c *Client) RegisterPlugin(pluginID string, factory PluginFactory) {
	c.registry[pluginID] = factory
}

func (c *Client) RegisterScenario(scenarioID string, factory ScenarioFactory) {
	c.scenarios[scenarioID] = factory
}

func (c *Client) RunScenario(ctx context.Context, scenarioID string) error {
	factory, exists := c.scenarios[scenarioID]
	if !exists {
		return ErrUnknownScenario
	}
	previousScenario := c.setActiveScenario(scenarioID)
	defer c.setActiveScenario(previousScenario)
	return factory().Run(ctx, &scenarioEnv{
		config: c.env.config,
		client: c,
		log:    c.log,
	})
}

func (c *Client) SetupPlugin(ctx context.Context, pluginID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.plugins[pluginID]; exists {
		return nil
	}

	factory, exists := c.registry[pluginID]
	if !exists {
		return ErrUnknownPlugin
	}

	plugin := factory()
	if err := plugin.Setup(ctx, c.env); err != nil {
		return err
	}
	c.plugins[pluginID] = plugin
	return nil
}

func (c *Client) CreateUser(_ context.Context, cfg *UserConfig) (*User, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.users[cfg.UserID]; exists {
		return nil, ErrUserExists
	}

	user := &User{
		client:        c,
		id:            cfg.UserID,
		role:          cfg.Role,
		connections:   make(map[connectionKey]*connection),
		log:           c.env.logRegistry.NewLogger("general", "["+cfg.UserID+"]"),
		defaultPlugin: c.env.config.Spec.Plugin,
	}
	c.users[cfg.UserID] = user

	return user, nil
}

func (c *Client) GetUser(userID string) *User {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.users[userID]
}

func (c *Client) MetricsSnapshot() RunMetricsSnapshot {
	return c.metrics.Snapshot()
}

func (c *Client) MetricsSummary() RunMetricsSummary {
	return c.MetricsSnapshot().Summary()
}

func (c *Client) removeUser(userID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.users, userID)
}

func (c *Client) currentScenarioLabel() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.activeScenario != "" {
		return c.activeScenario
	}
	if c.env != nil && c.env.config != nil && c.env.config.Spec.Scenario != "" {
		return c.env.config.Spec.Scenario
	}
	return manualScenarioLabel
}

func (c *Client) setActiveScenario(scenarioID string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	previous := c.activeScenario
	c.activeScenario = scenarioID
	return previous
}

func (c *Client) Shutdown(ctx context.Context) error {
	return c.ShutdownAll(ctx)
}

func (c *Client) ShutdownAll(ctx context.Context) error {
	c.mu.Lock()
	users := make([]*User, 0, len(c.users))
	for _, user := range c.users {
		users = append(users, user)
	}
	plugins := make([]Plugin, 0, len(c.plugins))
	for _, plugin := range c.plugins {
		plugins = append(plugins, plugin)
	}
	c.mu.Unlock()

	var errs []error
	for _, user := range users {
		if err := user.closeWithContext(ctx); err != nil {
			errs = append(errs, err)
		}
	}

	for _, plugin := range plugins {
		done := make(chan error, 1)
		go func(p Plugin) {
			done <- p.Shutdown(ctx)
		}(plugin)

		select {
		case err := <-done:
			if err != nil {
				errs = append(errs, err)
			}
		case <-ctx.Done():
			errs = append(errs, ctx.Err())
		}
	}

	return errors.Join(errs...)
}

func (c *Client) JoinAllRooms(ctx context.Context) error {
	return c.RunScenario(ctx, DefaultScenarioID)
}

// expandRoomName generates the room name for a given index in a multi-room
// setup. If the base name ends with digits, those digits are parsed as a
// number and incremented by offset. For index 0 the original name is returned.
//
//	expandRoomName("room-1234", 0) → "room-1234"
//	expandRoomName("room-1234", 1) → "room-1235"
//	expandRoomName("room-1234", 2) → "room-1236"
//	expandRoomName("nodigits", 1)  → "nodigits_1"
func expandRoomName(base string, offset int) string {
	if offset == 0 {
		return base
	}
	end := len(base)
	if end == 0 || base[end-1] < '0' || base[end-1] > '9' {
		return base + "_" + strconv.Itoa(offset)
	}
	start := end - 1
	for start > 0 && base[start-1] >= '0' && base[start-1] <= '9' {
		start--
	}
	n, err := strconv.ParseInt(base[start:end], 10, 64)
	if err != nil {
		return base + "_" + strconv.Itoa(offset)
	}
	return base[:start] + strconv.FormatInt(n+int64(offset), 10)
}

func (c *Client) getPlugin(pluginID string) Plugin {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.plugins[pluginID]
}
