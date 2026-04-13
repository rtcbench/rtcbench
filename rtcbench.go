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

	"github.com/google/uuid"
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
	ErrUnsupportedRole  = errors.New("unsupported role")
	ErrCannotJoinRoom   = errors.New("cannot join room")
	ErrUnknownPlugin    = errors.New("unknown plugin")
	ErrConnectionExists = errors.New("connection already exists")
	ErrUserClosed       = errors.New("user is closed")
	ErrMissingRoomID    = errors.New("missing room id")
)

type Plugin interface {
	Setup(ctx context.Context, e PluginEnv) error
	Shutdown(ctx context.Context) error
	JoinRoom(ctx context.Context, role UserRole, roomID, userID string) error
}

type ParticipantPlugin interface {
	Plugin
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

type joinRoomConfig struct {
	roomName     string
	usersPerRoom int

	signaling signalingConfig
}

type signalingConfig struct {
	concurrency int
}

type userConfig struct {
	roomID string
	userID string
	role   UserRole
}

type wrappedSignalingError struct {
	error error

	// other metadata for retrying
	roomName string
	nickname string
}

type Client struct {
	mu       sync.Mutex
	env      *pluginEnv
	registry PluginRegistry
	plugins  map[string]Plugin
	users    map[string]*User
	log      *log.Logger
}

func NewClient(config *Config, logRegistry *log.Registry) *Client {
	return &Client{
		env: &pluginEnv{
			config:      config,
			logRegistry: logRegistry,
		},
		registry: make(PluginRegistry),
		plugins:  make(map[string]Plugin),
		users:    make(map[string]*User),
		log:      logRegistry.NewLogger("general", ""),
	}
}

func (c *Client) AddStatsConsumer(fn func(vp9_stats.Period, vp9_stats.VideoQualitySample)) {
	c.env.statsConsumers = append(c.env.statsConsumers, fn)
}

func (c *Client) RegisterPlugin(pluginID string, factory PluginFactory) {
	c.registry[pluginID] = factory
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

func (c *Client) CreateUser(_ context.Context, cfg *UserConfig) *User {
	user := &User{
		client:        c,
		id:            cfg.UserID,
		role:          cfg.Role,
		connections:   make(map[connectionKey]*connection),
		log:           c.env.logRegistry.NewLogger("general", "["+cfg.UserID+"]"),
		defaultPlugin: c.env.config.Spec.Plugin,
	}

	c.mu.Lock()
	c.users[cfg.UserID] = user
	c.mu.Unlock()

	return user
}

func (c *Client) GetUser(userID string) *User {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.users[userID]
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
		if err := user.Close(); err != nil {
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
	if err := c.SetupPlugin(ctx, c.env.config.Spec.Plugin); err != nil {
		return err
	}
	wg := sync.WaitGroup{}
	for i := 0; i < c.env.config.Spec.Conference.TotalRooms; i++ {
		wg.Add(1)
		fmtRoomName := expandRoomName(c.env.config.Spec.Conference.Name, i)
		go func() {
			c.log.Infof("joining room %q (%d users)", fmtRoomName, c.env.config.Spec.Conference.UsersPerRoom)
			c.joinRoomByName(ctx, fmtRoomName)
			c.log.Infof("finished joining room %q", fmtRoomName)
			wg.Done()
		}()
	}
	wg.Wait()
	return nil
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

func (c *Client) joinRoomByName(ctx context.Context, roomName string) {
	if c.env.config.Spec.Conference.JoinPolicy.AlwaysRetryFailedJoins {
		nUsersRemaining := c.env.config.Spec.Conference.UsersPerRoom
		var errs []wrappedSignalingError
		for {
			errs = c.joinRoom(ctx, joinRoomConfig{
				roomName:     roomName,
				usersPerRoom: nUsersRemaining,
				signaling: signalingConfig{
					concurrency: c.env.config.Spec.Conference.JoinPolicy.Concurrency,
				},
			})
			if len(errs) == 0 {
				c.log.Infof("[AlwaysRetryFailedJoins] finished joining room %s", roomName)
				break
			}

			retryDelay := c.env.config.Spec.Conference.JoinPolicy.JoinStartSpacing
			if retryDelay < 1*time.Second {
				retryDelay = 1 * time.Second
			}

			c.log.Errorf("[AlwaysRetryFailedJoins] %d errors occurred, will retry in %s", len(errs), retryDelay.String())
			for _, e := range errs {
				c.log.Errorf("[AlwaysRetryFailedJoins] error: %v", e.error)
			}
			nUsersRemaining = len(errs)

			time.Sleep(retryDelay)
		}
	} else {
		if errs := c.joinRoom(ctx, joinRoomConfig{
			roomName:     roomName,
			usersPerRoom: c.env.config.Spec.Conference.UsersPerRoom,
			signaling: signalingConfig{
				concurrency: c.env.config.Spec.Conference.JoinPolicy.Concurrency,
			},
		}); errs != nil {
			c.log.Errorf("%d errors from JoinRoom: %v", len(errs), errs)
			for _, e := range errs {
				c.log.Errorf("error: %v", e.error)
			}
		}
	}
}

func (c *Client) joinRoom(ctx context.Context, cfg joinRoomConfig) []wrappedSignalingError {
	size := cfg.usersPerRoom
	if size < 1 {
		return nil
	}

	cfgCh := make(chan userConfig)
	go func() {
		defer close(cfgCh)

		spacing := c.env.config.Spec.Conference.JoinPolicy.JoinStartSpacing
		senders := c.env.config.Spec.Conference.Cameras.PerRoom

		for i := 0; i < cfg.usersPerRoom; i++ {
			if i > 0 && spacing > 0 {
				time.Sleep(spacing)
			}

			role := Viewer
			if i < senders { // senders have join priority over viewers
				role = Sender
			}

			cfgCh <- userConfig{
				roomID: cfg.roomName,
				userID: fmt.Sprintf("%s-%s", string(role), uuid.NewString()),
				role:   role,
			}
		}
	}()

	errCh := make(chan wrappedSignalingError, size)

	concurrency := size
	if cfg.signaling.concurrency > 0 && cfg.signaling.concurrency < size {
		concurrency = cfg.signaling.concurrency
	} else if cfg.signaling.concurrency <= 0 {
		concurrency = 1 // default to serial
	}

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for vc := range cfgCh {
				user := c.CreateUser(ctx, &UserConfig{
					UserID: vc.userID,
					Role:   vc.role,
				})
				if err := user.JoinRoom(ctx, &JoinRequest{
					Plugin: c.env.config.Spec.Plugin,
					RoomID: vc.roomID,
				}); err != nil {
					errCh <- wrappedSignalingError{
						error:    fmt.Errorf("cannot join %q to room %q: %w", vc.userID, cfg.roomName, err),
						roomName: cfg.roomName,
						nickname: vc.userID,
					}
				}
			}
			c.log.Infof("[JoinRoom] worker #%d done (room=%s)", workerID, cfg.roomName)
		}(i)
	}

	wg.Wait()
	close(errCh)

	var errs []wrappedSignalingError

	for err := range errCh {
		errs = append(errs, err)
	}

	return errs
}

func (c *Client) getPlugin(pluginID string) Plugin {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.plugins[pluginID]
}
