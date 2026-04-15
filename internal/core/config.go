package core

import (
	"errors"
	"fmt"
	"net"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/rtcbench/rtcbench/pkg/vp9_stats"
)

const (
	APIVersionV1            = "rtcbench/v1"
	KindVideoCallStressTest = "VideoCallStressTest"
)

var (
	ErrMissingAPIVersion = errors.New("missing apiVersion, expected " + APIVersionV1)
	ErrInvalidAPIVersion = errors.New("invalid apiVersion, expected " + APIVersionV1)
	ErrMissingKind       = errors.New("missing kind, expected " + KindVideoCallStressTest)
	ErrInvalidKind       = errors.New("invalid kind, expected " + KindVideoCallStressTest)

	ErrMissingMetadata = errors.New("missing metadata section")
	ErrMissingName     = errors.New("missing metadata.name string")
	ErrInvalidName     = errors.New("invalid metadata.name string")

	ErrMissingSpec           = errors.New("missing spec section")
	ErrMissingPlugin         = errors.New("missing spec.plugin string")
	ErrInvalidPlugin         = errors.New("invalid spec.plugin string")
	ErrInvalidScenario       = errors.New("invalid spec.scenario string")
	ErrInvalidScenarioConfig = errors.New("invalid spec.scenarioConfig section")
	ErrMissingConference     = errors.New("missing spec.conference section")
	ErrMissingNetwork        = errors.New("missing spec.network section")
	ErrMissingLogging        = errors.New("missing spec.logging section")

	ErrMissingConferenceName   = errors.New("missing spec.conference.name string")
	ErrInvalidConferenceName   = errors.New("invalid spec.conference.name string")
	ErrMissingUsersPerRoom     = errors.New("missing spec.conference.usersPerRoom integer")
	ErrNonPositiveUsersPerRoom = errors.New("invalid spec.conference.usersPerRoom integer, must be >= 1")
	ErrNonPositiveTotalRooms   = errors.New("invalid spec.conference.totalRooms integer, must be >= 1")
	ErrMissingJoinPolicy       = errors.New("missing spec.conference.joinPolicy section")

	ErrMissingCameraCountPerRoom   = errors.New("missing spec.conference.cameras.perRoom integer")
	ErrInvalidCameraCountPerRoom   = errors.New("invalid spec.conference.cameras.perRoom integer, must be >= 0")
	ErrMissingCameraFileType       = errors.New("missing spec.conference.cameras.fileType string")
	ErrUnsupportedCameraFileType   = errors.New("unsupported spec.conference.cameras.fileType string")
	ErrMissingCameraVideoCodec     = errors.New("missing spec.conference.cameras.videoCodec string")
	ErrUnsupportedCameraVideoCodec = errors.New("unsupported spec.conference.cameras.videoCodec string")
	ErrMissingCameraDirectory      = errors.New("missing spec.conference.cameras.directory string")

	ErrMissingJoinPolicyConcurrency  = errors.New("missing spec.conference.joinPolicy.concurrency integer")
	ErrNegativeJoinPolicyConcurrency = errors.New("invalid spec.conference.joinPolicy.concurrency, must be >= 0")
	ErrNegativeJoinStartSpacing      = errors.New("invalid spec.conference.joinPolicy.joinStartSpacing, must be >= 0")

	ErrInvalidStatsBufferSize        = errors.New("invalid spec.conference.statsBufferSize integer, must be in range [64..4096]")
	ErrInvalidSVCMode                = errors.New("invalid spec.conference.cameras.svc.mode, must be auto|on|off")
	ErrInvalidSVCSpatialLayers       = errors.New("invalid spec.conference.cameras.svc.spatialLayers, must be 1-3")
	ErrInvalidSVCTemporalLayers      = errors.New("invalid spec.conference.cameras.svc.temporalLayers, must be 1-3")
	ErrMissingRecordingDirectory     = errors.New("missing spec.conference.recording.directory pathname")
	ErrMissingPacketCaptureDirectory = errors.New("missing spec.conference.packetCapture.directory pathname")

	ErrMissingServerIP                = errors.New("missing spec.network.serverIP address")
	ErrInvalidServerIP                = errors.New("invalid spec.network.serverIP address")
	ErrInvalidServerPort              = errors.New("spec.network.serverPort must be in [1,65535]")
	ErrMissingServerPortForImpairment = errors.New("spec.network.serverPort is required when spec.network.impairment is set")
	ErrInvalidClientIP                = errors.New("invalid spec.network.clientIP address")

	ErrImpairmentProfilesEmpty            = errors.New("spec.network.impairment.profiles is empty")
	ErrImpairmentProfileNameEmpty         = errors.New("spec.network.impairment.profiles has an empty key")
	ErrImpairmentProfileBandwidthNeg      = errors.New("spec.network.impairment.profiles.*.bandwidthKbps must be >= 0")
	ErrImpairmentProfileLatencyNeg        = errors.New("spec.network.impairment.profiles.*.latency must be >= 0")
	ErrImpairmentProfileJitterNeg         = errors.New("spec.network.impairment.profiles.*.jitter must be >= 0")
	ErrImpairmentProfileJitterUnsupported = errors.New("spec.network.impairment.profiles.*.jitter is not supported yet (pion vnet.DelayFilter is constant-only and vnet.NIC cannot be extended externally)")
	ErrImpairmentProfileLossRange         = errors.New("spec.network.impairment.profiles.*.lossPercent must be in [0,100]")
	ErrImpairmentDefaultMissing           = errors.New("spec.network.impairment.default is required")
	ErrImpairmentDefaultUnknown           = errors.New("spec.network.impairment.default references an unknown profile")
	ErrImpairmentAssignmentProfileEmpty   = errors.New("spec.network.impairment.assignments[*].profile is required")
	ErrImpairmentAssignmentProfileUnknown = errors.New("spec.network.impairment.assignments[*].profile references an unknown profile")
	ErrImpairmentAssignmentMatchEmpty     = errors.New("spec.network.impairment.assignments[*].match must set at least one selector")
	ErrImpairmentAssignmentRoleInvalid    = errors.New("spec.network.impairment.assignments[*].match.role must be 'sender' or 'viewer'")
	ErrImpairmentAssignmentPatternInvalid = errors.New("spec.network.impairment.assignments[*].match.userIDPattern must be a valid glob")
	ErrImpairmentAssignmentIndexRange     = errors.New("spec.network.impairment.assignments[*].match.userIndexRange must be [min,max] with min<=max or max=-1")

	ErrMissingLoggingDirectory = errors.New("missing spec.logging.directory pathname")
	ErrInvalidConsoleLevel     = errors.New("invalid spec.logging.consoleLevel string, must match " + logLevelRegex.String())
	ErrInvalidFileLevel        = errors.New("invalid spec.logging.fileLevel string, must match " + logLevelRegex.String())
	ErrInvalidMetricsThreshold = errors.New("invalid spec.metrics.thresholds section")

	ErrMissingLogStreamName  = errors.New("missing spec.logging.streams[#].name string")
	ErrInvalidLogStreamName  = errors.New("invalid spec.logging.streams[#].name string")
	ErrMissingLogStreamLevel = errors.New("missing spec.logging.streams[#].level string")
	ErrInvalidLogStreamLevel = errors.New("invalid spec.logging.streams[#].level string, must match " + logLevelRegex.String())

	// nameRegex starts with an alphanumeric, then has letters numbers _'s and -'s, and ends with an alphanumeric
	nameRegex = regexp.MustCompile("^[a-zA-Z0-9][a-zA-Z0-9_-]+[a-zA-Z0-9]$")

	// logLevelRegex must be one of the following lowercase levels
	logLevelRegex = regexp.MustCompile("^(info|debug|error)$")

	SupportedCameraVideoCodecs = []string{"vp9"}
	SupportedCameraFileTypes   = []string{"ivf"}
)

type Config struct {
	APIVersion string
	Kind       string
	Metadata   MetadataConfig
	Spec       SpecConfig
}

type YAMLConfig struct {
	APIVersion *string             `yaml:"apiVersion,omitempty"`
	Kind       *string             `yaml:"kind,omitempty"`
	Metadata   *YAMLMetadataConfig `yaml:"metadata,omitempty"`
	Spec       *YAMLSpecConfig     `yaml:"spec,omitempty"`
}

func (yc *YAMLConfig) IntoConfig() (*Config, error) {
	err := yc.validate()
	if err != nil {
		return nil, err
	}
	return yc.mustConvert(), nil
}

func (yc *YAMLConfig) validate() error {
	var (
		errs        []error
		metadataErr error
		specErr     error
	)
	if yc.APIVersion == nil {
		errs = append(errs, ErrMissingAPIVersion)
	} else if *yc.APIVersion != APIVersionV1 {
		errs = append(errs, ErrInvalidAPIVersion)
	}
	if yc.Kind == nil {
		errs = append(errs, ErrMissingKind)
	} else if *yc.Kind != KindVideoCallStressTest {
		errs = append(errs, ErrInvalidKind)
	}
	if errs != nil {
		// return early as these 2 fields may change validation for spec and metadata in the future
		return errors.Join(errs...)
	}
	if yc.Metadata == nil {
		metadataErr = ErrMissingMetadata
	} else {
		metadataErr = yc.Metadata.validate()
	}
	if metadataErr != nil {
		errs = append(errs, metadataErr)
	}
	if yc.Spec == nil {
		specErr = ErrMissingSpec
	} else {
		specErr = yc.Spec.validate()
	}
	if specErr != nil {
		errs = append(errs, specErr)
	}
	return errors.Join(errs...)
}

func (yc *YAMLConfig) mustConvert() *Config {
	var c Config
	c.APIVersion = *yc.APIVersion
	c.Kind = *yc.Kind
	c.Metadata = yc.Metadata.mustConvert()
	c.Spec = yc.Spec.mustConvert()
	return &c
}

type MetadataConfig struct {
	Name string
}

type YAMLMetadataConfig struct {
	Name *string `yaml:"name,omitempty"`
}

func (yc *YAMLMetadataConfig) validate() error {
	if yc.Name == nil {
		return ErrMissingName
	}
	if !nameRegex.MatchString(*yc.Name) {
		return ErrInvalidName
	}
	return nil
}

func (yc *YAMLMetadataConfig) mustConvert() MetadataConfig {
	var c MetadataConfig
	c.Name = *yc.Name
	return c
}

type SpecConfig struct {
	Plugin         string
	Scenario       string
	ScenarioConfig map[string]any
	Conference     ConferenceConfig
	Network        NetworkConfig
	PluginConfig   map[string]any
	Logging        LoggingConfig
	Metrics        MetricsConfig
}

type YAMLSpecConfig struct {
	Plugin         *string               `yaml:"plugin,omitempty"`
	Scenario       *string               `yaml:"scenario,omitempty"`
	ScenarioConfig map[string]any        `yaml:"scenarioConfig,omitempty"`
	Conference     *YAMLConferenceConfig `yaml:"conference,omitempty"`
	Network        *YAMLNetworkConfig    `yaml:"network,omitempty"`
	PluginConfig   map[string]any        `yaml:"pluginConfig,omitempty"`
	Logging        *YAMLLoggingConfig    `yaml:"logging,omitempty"`
	Metrics        *YAMLMetricsConfig    `yaml:"metrics,omitempty"`
}

type MetricsConfig struct {
	Port               int
	StatsJSONLPath     string
	Thresholds         []RunThresholdConfig
	ReceiverThresholds []ReceiverThresholdConfig
}

type YAMLMetricsConfig struct {
	Port               *int                          `yaml:"port,omitempty"`
	StatsJSONLPath     *string                       `yaml:"statsJSONLPath,omitempty"`
	Thresholds         []YAMLRunThresholdConfig      `yaml:"thresholds,omitempty"`
	ReceiverThresholds []YAMLReceiverThresholdConfig `yaml:"receiverThresholds,omitempty"`
}

type RunThresholdConfig struct {
	Op             string
	Scenario       string
	Plugin         string
	Role           string
	MaxFailureRate *float64
	MaxMean        *time.Duration
	MaxP95         *time.Duration
	MaxP99         *time.Duration
}

type YAMLRunThresholdConfig struct {
	Op             *string  `yaml:"op,omitempty"`
	Scenario       *string  `yaml:"scenario,omitempty"`
	Plugin         *string  `yaml:"plugin,omitempty"`
	Role           *string  `yaml:"role,omitempty"`
	MaxFailureRate *float64 `yaml:"maxFailureRate,omitempty"`
	MaxMean        *string  `yaml:"maxMean,omitempty"`
	MaxP95         *string  `yaml:"maxP95,omitempty"`
	MaxP99         *string  `yaml:"maxP99,omitempty"`
}

type ReceiverThresholdConfig struct {
	Scenario          string
	Plugin            string
	Profile           string
	UserID            string
	MaxFreezeCount    *int64
	MaxFreezeDuration *float64
	MaxFrameLossRatio *float64
	MinBitrateBps     *float64
	MinFPS            *float64
	MaxJitterUS       *float64
	MaxMeanRTTMS      *float64
	MaxPLICount       *int64
}

type YAMLReceiverThresholdConfig struct {
	Scenario          *string  `yaml:"scenario,omitempty"`
	Plugin            *string  `yaml:"plugin,omitempty"`
	Profile           *string  `yaml:"profile,omitempty"`
	UserID            *string  `yaml:"userID,omitempty"`
	MaxFreezeCount    *int64   `yaml:"maxFreezeCount,omitempty"`
	MaxFreezeDuration *float64 `yaml:"maxFreezeDuration,omitempty"`
	MaxFrameLossRatio *float64 `yaml:"maxFrameLossRatio,omitempty"`
	MinBitrateBps     *float64 `yaml:"minBitrateBps,omitempty"`
	MinFPS            *float64 `yaml:"minFPS,omitempty"`
	MaxJitterUS       *float64 `yaml:"maxJitterUS,omitempty"`
	MaxMeanRTTMS      *float64 `yaml:"maxMeanRTTMS,omitempty"`
	MaxPLICount       *int64   `yaml:"maxPLICount,omitempty"`
}

func (yc *YAMLSpecConfig) validate() error {
	var (
		errs          []error
		conferenceErr error
		networkErr    error
		loggingErr    error
		metricsErr    error
	)
	if yc.Plugin == nil {
		// return early for crucial mistake
		return ErrMissingPlugin
	}
	if !nameRegex.MatchString(*yc.Plugin) {
		// return early for invalid plugin name string
		return ErrInvalidPlugin
	}
	if yc.Scenario != nil && !nameRegex.MatchString(*yc.Scenario) {
		return ErrInvalidScenario
	}
	if yc.ScenarioConfig != nil && (yc.Scenario == nil || *yc.Scenario == "") {
		errs = append(errs, ErrInvalidScenarioConfig)
	}
	if yc.Conference == nil {
		errs = append(errs, ErrMissingConference)
	} else {
		conferenceErr = yc.Conference.validate()
	}
	if conferenceErr != nil {
		errs = append(errs, conferenceErr)
	}
	if yc.Network == nil {
		errs = append(errs, ErrMissingNetwork)
	} else {
		networkErr = yc.Network.validate()
	}
	if networkErr != nil {
		errs = append(errs, networkErr)
	}
	if yc.Logging == nil {
		errs = append(errs, ErrMissingLogging)
	} else {
		loggingErr = yc.Logging.validate()
	}
	if loggingErr != nil {
		errs = append(errs, loggingErr)
	}
	if yc.Metrics != nil {
		metricsErr = yc.Metrics.validate()
	}
	if metricsErr != nil {
		errs = append(errs, metricsErr)
	}
	return errors.Join(errs...)
}

func (yc *YAMLSpecConfig) mustConvert() SpecConfig {
	var c SpecConfig
	c.Plugin = *yc.Plugin
	if yc.Scenario != nil {
		c.Scenario = *yc.Scenario
	}
	if yc.ScenarioConfig != nil {
		c.ScenarioConfig = make(map[string]any, len(yc.ScenarioConfig))
		for k, v := range yc.ScenarioConfig {
			c.ScenarioConfig[k] = v
		}
	}
	c.Conference = yc.Conference.mustConvert()
	c.Network = yc.Network.mustConvert()
	if yc.PluginConfig != nil {
		c.PluginConfig = make(map[string]any, len(yc.PluginConfig))
		for k, v := range yc.PluginConfig {
			c.PluginConfig[k] = v
		}
	}
	c.Logging = yc.Logging.mustConvert()
	if yc.Metrics != nil {
		if yc.Metrics.Port != nil {
			c.Metrics.Port = *yc.Metrics.Port
		}
		if yc.Metrics.StatsJSONLPath != nil {
			c.Metrics.StatsJSONLPath = *yc.Metrics.StatsJSONLPath
		}
		if len(yc.Metrics.Thresholds) > 0 {
			c.Metrics.Thresholds = make([]RunThresholdConfig, 0, len(yc.Metrics.Thresholds))
			for _, threshold := range yc.Metrics.Thresholds {
				c.Metrics.Thresholds = append(c.Metrics.Thresholds, threshold.mustConvert())
			}
		}
		if len(yc.Metrics.ReceiverThresholds) > 0 {
			c.Metrics.ReceiverThresholds = make([]ReceiverThresholdConfig, 0, len(yc.Metrics.ReceiverThresholds))
			for _, threshold := range yc.Metrics.ReceiverThresholds {
				c.Metrics.ReceiverThresholds = append(c.Metrics.ReceiverThresholds, threshold.mustConvert())
			}
		}
	}
	return c
}

func (ym *YAMLMetricsConfig) validate() error {
	var errs []error
	for i, threshold := range ym.Thresholds {
		if err := threshold.validate(i); err != nil {
			errs = append(errs, err)
		}
	}
	for i, threshold := range ym.ReceiverThresholds {
		if err := threshold.validate(i); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (yt *YAMLRunThresholdConfig) validate(index int) error {
	if yt.Op == nil || *yt.Op == "" {
		return fmt.Errorf("spec.metrics.thresholds[%d].op: required", index)
	}
	switch *yt.Op {
	case "join", "leave", "publish_video", "unpublish_video", "close":
	default:
		return fmt.Errorf("spec.metrics.thresholds[%d].op: unsupported operation %q", index, *yt.Op)
	}
	if yt.Scenario != nil && !nameRegex.MatchString(*yt.Scenario) {
		return fmt.Errorf("spec.metrics.thresholds[%d].scenario: invalid name %q", index, *yt.Scenario)
	}
	if yt.Plugin != nil && !nameRegex.MatchString(*yt.Plugin) {
		return fmt.Errorf("spec.metrics.thresholds[%d].plugin: invalid name %q", index, *yt.Plugin)
	}
	if yt.Role != nil {
		switch *yt.Role {
		case string(Sender), string(Viewer):
		default:
			return fmt.Errorf("spec.metrics.thresholds[%d].role: must be %q or %q", index, Sender, Viewer)
		}
	}
	if yt.MaxFailureRate != nil && (*yt.MaxFailureRate < 0 || *yt.MaxFailureRate > 1) {
		return fmt.Errorf("spec.metrics.thresholds[%d].maxFailureRate: must be in [0,1]", index)
	}
	if yt.MaxMean == nil && yt.MaxP95 == nil && yt.MaxP99 == nil && yt.MaxFailureRate == nil {
		return fmt.Errorf("spec.metrics.thresholds[%d]: at least one threshold must be set", index)
	}
	for field, raw := range map[string]*string{
		"maxMean": yt.MaxMean,
		"maxP95":  yt.MaxP95,
		"maxP99":  yt.MaxP99,
	} {
		if raw == nil {
			continue
		}
		duration, err := time.ParseDuration(*raw)
		if err != nil {
			return fmt.Errorf("spec.metrics.thresholds[%d].%s: %w", index, field, err)
		}
		if duration <= 0 {
			return fmt.Errorf("spec.metrics.thresholds[%d].%s: must be > 0", index, field)
		}
	}
	return nil
}

func (yt *YAMLRunThresholdConfig) mustConvert() RunThresholdConfig {
	var cfg RunThresholdConfig
	if yt.Op != nil {
		cfg.Op = *yt.Op
	}
	if yt.Scenario != nil {
		cfg.Scenario = *yt.Scenario
	}
	if yt.Plugin != nil {
		cfg.Plugin = *yt.Plugin
	}
	if yt.Role != nil {
		cfg.Role = *yt.Role
	}
	cfg.MaxFailureRate = yt.MaxFailureRate
	if yt.MaxMean != nil {
		duration := mustParseDuration(*yt.MaxMean)
		cfg.MaxMean = &duration
	}
	if yt.MaxP95 != nil {
		duration := mustParseDuration(*yt.MaxP95)
		cfg.MaxP95 = &duration
	}
	if yt.MaxP99 != nil {
		duration := mustParseDuration(*yt.MaxP99)
		cfg.MaxP99 = &duration
	}
	return cfg
}

func (yt *YAMLReceiverThresholdConfig) validate(index int) error {
	if yt.Scenario != nil && !nameRegex.MatchString(*yt.Scenario) {
		return fmt.Errorf("spec.metrics.receiverThresholds[%d].scenario: invalid name %q", index, *yt.Scenario)
	}
	if yt.Plugin != nil && !nameRegex.MatchString(*yt.Plugin) {
		return fmt.Errorf("spec.metrics.receiverThresholds[%d].plugin: invalid name %q", index, *yt.Plugin)
	}
	if yt.Profile != nil && !nameRegex.MatchString(*yt.Profile) {
		return fmt.Errorf("spec.metrics.receiverThresholds[%d].profile: invalid name %q", index, *yt.Profile)
	}
	if yt.MaxFreezeCount != nil && *yt.MaxFreezeCount < 0 {
		return fmt.Errorf("spec.metrics.receiverThresholds[%d].maxFreezeCount: must be >= 0", index)
	}
	if yt.MaxFreezeDuration != nil && *yt.MaxFreezeDuration < 0 {
		return fmt.Errorf("spec.metrics.receiverThresholds[%d].maxFreezeDuration: must be >= 0", index)
	}
	if yt.MaxFrameLossRatio != nil && (*yt.MaxFrameLossRatio < 0 || *yt.MaxFrameLossRatio > 1) {
		return fmt.Errorf("spec.metrics.receiverThresholds[%d].maxFrameLossRatio: must be in [0,1]", index)
	}
	if yt.MinBitrateBps != nil && *yt.MinBitrateBps < 0 {
		return fmt.Errorf("spec.metrics.receiverThresholds[%d].minBitrateBps: must be >= 0", index)
	}
	if yt.MinFPS != nil && *yt.MinFPS < 0 {
		return fmt.Errorf("spec.metrics.receiverThresholds[%d].minFPS: must be >= 0", index)
	}
	if yt.MaxJitterUS != nil && *yt.MaxJitterUS < 0 {
		return fmt.Errorf("spec.metrics.receiverThresholds[%d].maxJitterUS: must be >= 0", index)
	}
	if yt.MaxMeanRTTMS != nil && *yt.MaxMeanRTTMS < 0 {
		return fmt.Errorf("spec.metrics.receiverThresholds[%d].maxMeanRTTMS: must be >= 0", index)
	}
	if yt.MaxPLICount != nil && *yt.MaxPLICount < 0 {
		return fmt.Errorf("spec.metrics.receiverThresholds[%d].maxPLICount: must be >= 0", index)
	}
	if yt.MaxFreezeCount == nil &&
		yt.MaxFreezeDuration == nil &&
		yt.MaxFrameLossRatio == nil &&
		yt.MinBitrateBps == nil &&
		yt.MinFPS == nil &&
		yt.MaxJitterUS == nil &&
		yt.MaxMeanRTTMS == nil &&
		yt.MaxPLICount == nil {
		return fmt.Errorf("spec.metrics.receiverThresholds[%d]: at least one threshold must be set", index)
	}
	return nil
}

func (yt *YAMLReceiverThresholdConfig) mustConvert() ReceiverThresholdConfig {
	var cfg ReceiverThresholdConfig
	if yt.Scenario != nil {
		cfg.Scenario = *yt.Scenario
	}
	if yt.Plugin != nil {
		cfg.Plugin = *yt.Plugin
	}
	if yt.Profile != nil {
		cfg.Profile = *yt.Profile
	}
	if yt.UserID != nil {
		cfg.UserID = *yt.UserID
	}
	cfg.MaxFreezeCount = yt.MaxFreezeCount
	cfg.MaxFreezeDuration = yt.MaxFreezeDuration
	cfg.MaxFrameLossRatio = yt.MaxFrameLossRatio
	cfg.MinBitrateBps = yt.MinBitrateBps
	cfg.MinFPS = yt.MinFPS
	cfg.MaxJitterUS = yt.MaxJitterUS
	cfg.MaxMeanRTTMS = yt.MaxMeanRTTMS
	cfg.MaxPLICount = yt.MaxPLICount
	return cfg
}

func mustParseDuration(raw string) time.Duration {
	duration, err := time.ParseDuration(raw)
	if err != nil {
		panic(err)
	}
	return duration
}

type ConferenceConfig struct {
	Name               string
	UsersPerRoom       int
	TotalRooms         int
	StatsBufferSize    int
	StatsInputChanSize int
	Cameras            CameraConfig
	JoinPolicy         JoinPolicyConfig
	Recording          RecordingConfig
	PacketCapture      PacketCaptureConfig
}

type YAMLConferenceConfig struct {
	// Name conference room identifier
	Name *string `yaml:"name,omitempty"`
	// UsersPerRoom number of users per conference room
	UsersPerRoom *int `yaml:"usersPerRoom,omitempty"`
	// TotalRooms defaults to 1
	TotalRooms *int `yaml:"totalRooms,omitempty"`
	// StatsBufferSize optional ring buffer size for video quality stats (default 768)
	StatsBufferSize *int `yaml:"statsBufferSize,omitempty"`
	// StatsInputChanSize optional channel buffer size for stats input (default 65536)
	StatsInputChanSize *int `yaml:"statsInputChanSize,omitempty"`
	// Cameras optional config for some (or all) users to turn on their cameras
	Cameras    *YAMLCameraConfig     `yaml:"cameras,omitempty"`
	JoinPolicy *YAMLJoinPolicyConfig `yaml:"joinPolicy,omitempty"`
	// Recording optional section, defaults to disabled
	Recording *YAMLRecordingConfig `yaml:"recording,omitempty"`
	// PacketCapture optional section, defaults to disabled
	PacketCapture *YAMLPacketCaptureConfig `yaml:"packetCapture,omitempty"`
}

func (yc *YAMLConferenceConfig) validate() error {
	var (
		errs          []error
		joinPolicyErr error
		recordingErr  error
		camerasErr    error
	)
	if yc.Name == nil {
		// return early as every conference is identified by a name
		return ErrMissingConferenceName
	}
	if !nameRegex.MatchString(*yc.Name) {
		// return early cannot create a conference with invalid name
		return ErrInvalidConferenceName
	}
	if yc.UsersPerRoom == nil {
		errs = append(errs, ErrMissingUsersPerRoom)
	} else if *yc.UsersPerRoom < 1 {
		errs = append(errs, ErrNonPositiveUsersPerRoom)
	}
	if yc.TotalRooms != nil && *yc.TotalRooms < 1 {
		errs = append(errs, ErrNonPositiveTotalRooms)
	}
	if yc.StatsBufferSize != nil && (*yc.StatsBufferSize < 64 || *yc.StatsBufferSize > 4096) {
		errs = append(errs, ErrInvalidStatsBufferSize)
	}
	if yc.Cameras != nil {
		camerasErr = yc.Cameras.validate()
	}
	if camerasErr != nil {
		errs = append(errs, camerasErr)
	}
	if yc.JoinPolicy == nil {
		errs = append(errs, ErrMissingJoinPolicy)
	} else {
		joinPolicyErr = yc.JoinPolicy.validate()
	}
	if joinPolicyErr != nil {
		errs = append(errs, joinPolicyErr)
	}
	if yc.Recording != nil {
		recordingErr = yc.Recording.validate()
	}
	if recordingErr != nil {
		errs = append(errs, recordingErr)
	}
	if yc.PacketCapture != nil {
		if pcErr := yc.PacketCapture.validate(); pcErr != nil {
			errs = append(errs, pcErr)
		}
	}
	return errors.Join(errs...)
}

func (yc *YAMLConferenceConfig) mustConvert() ConferenceConfig {
	var c ConferenceConfig
	c.Name = *yc.Name
	c.UsersPerRoom = *yc.UsersPerRoom
	c.TotalRooms = 1
	if yc.TotalRooms != nil {
		c.TotalRooms = *yc.TotalRooms
	}
	c.StatsBufferSize = vp9_stats.DefaultStatsBufferSize
	if yc.StatsBufferSize != nil {
		c.StatsBufferSize = *yc.StatsBufferSize
	}
	c.StatsInputChanSize = 1 << 16 // 65536
	if yc.StatsInputChanSize != nil {
		c.StatsInputChanSize = *yc.StatsInputChanSize
	}
	c.JoinPolicy = yc.JoinPolicy.mustConvert()
	if yc.Recording != nil {
		c.Recording = yc.Recording.mustConvert()
	} else {
		c.Recording = RecordingConfig{
			Enabled:   false,
			Directory: "",
		}
	}
	if yc.Cameras == nil {
		c.Cameras = CameraConfig{
			PerRoom:    0,
			FileType:   "",
			VideoCodec: "",
			Directory:  "",
		}
	} else {
		c.Cameras = yc.Cameras.mustConvert()
	}
	if yc.PacketCapture != nil {
		c.PacketCapture = yc.PacketCapture.mustConvert()
	}
	return c
}

type CameraConfig struct {
	PerRoom    int
	FileType   string
	VideoCodec string
	Directory  string
	InMemory   bool
	SVC        SVCCameraConfig
}

// SVCCameraConfig controls VP9 SVC (Scalable Video Coding) behavior for senders.
//
//   - Mode "auto": detect from IVF file. Non-SVC files use simple path, SVC files use SVC path.
//   - Mode "on": force SVC path. SpatialLayers/TemporalLayers override auto-detection (0 = auto).
//   - Mode "off": force simple path regardless of IVF file content.
type SVCCameraConfig struct {
	Mode           string // "auto" (default), "on", "off"
	SpatialLayers  int    // 1-3 when Mode="on", 0 = auto-detect from IVF
	TemporalLayers int    // 1-3 when Mode="on", 0 = auto-detect from IVF
}

type YAMLCameraConfig struct {
	PerRoom    *int              `yaml:"perRoom,omitempty"`
	FileType   *string           `yaml:"fileType,omitempty"`
	VideoCodec *string           `yaml:"videoCodec,omitempty"`
	Directory  *string           `yaml:"directory,omitempty"`
	InMemory   *bool             `yaml:"inMemory,omitempty"`
	SVC        *YAMLSVCCamConfig `yaml:"svc,omitempty"`
}

type YAMLSVCCamConfig struct {
	Mode           *string `yaml:"mode,omitempty"`
	SpatialLayers  *int    `yaml:"spatialLayers,omitempty"`
	TemporalLayers *int    `yaml:"temporalLayers,omitempty"`
}

var validSVCModes = []string{"auto", "on", "off"}

func (yc *YAMLSVCCamConfig) validate() error {
	var errs []error
	if yc.Mode != nil && !slices.Contains(validSVCModes, *yc.Mode) {
		errs = append(errs, ErrInvalidSVCMode)
	}
	if yc.SpatialLayers != nil && (*yc.SpatialLayers < 1 || *yc.SpatialLayers > 3) {
		errs = append(errs, ErrInvalidSVCSpatialLayers)
	}
	if yc.TemporalLayers != nil && (*yc.TemporalLayers < 1 || *yc.TemporalLayers > 3) {
		errs = append(errs, ErrInvalidSVCTemporalLayers)
	}
	return errors.Join(errs...)
}

func (yc *YAMLSVCCamConfig) mustConvert() SVCCameraConfig {
	c := SVCCameraConfig{Mode: "auto"}
	if yc.Mode != nil {
		c.Mode = *yc.Mode
	}
	if yc.SpatialLayers != nil {
		c.SpatialLayers = *yc.SpatialLayers
	}
	if yc.TemporalLayers != nil {
		c.TemporalLayers = *yc.TemporalLayers
	}
	return c
}

func (yc *YAMLCameraConfig) validate() error {
	var errs []error
	if yc.PerRoom == nil {
		errs = append(errs, ErrMissingCameraCountPerRoom)
	} else if *yc.PerRoom == 0 {
		return nil
	} else if *yc.PerRoom < 0 {
		errs = append(errs, ErrInvalidCameraCountPerRoom)
	}
	if yc.FileType == nil {
		errs = append(errs, ErrMissingCameraFileType)
	} else if !slices.Contains(SupportedCameraFileTypes, strings.ToLower(*yc.FileType)) {
		errs = append(errs, ErrUnsupportedCameraFileType)
	}
	if yc.VideoCodec == nil {
		errs = append(errs, ErrMissingCameraVideoCodec)
	} else if !slices.Contains(SupportedCameraVideoCodecs, strings.ToLower(*yc.VideoCodec)) {
		errs = append(errs, ErrUnsupportedCameraVideoCodec)
	}
	if yc.Directory == nil || strings.TrimSpace(*yc.Directory) == "" {
		errs = append(errs, ErrMissingCameraDirectory)
	}
	if yc.SVC != nil {
		if svcErr := yc.SVC.validate(); svcErr != nil {
			errs = append(errs, svcErr)
		}
	}
	return errors.Join(errs...)
}

func (yc *YAMLCameraConfig) mustConvert() CameraConfig {
	var c CameraConfig
	c.PerRoom = *yc.PerRoom
	if c.PerRoom == 0 {
		return c
	}
	c.FileType = *yc.FileType
	c.VideoCodec = *yc.VideoCodec
	c.Directory = *yc.Directory
	if yc.InMemory != nil {
		c.InMemory = *yc.InMemory
	}
	c.SVC = SVCCameraConfig{Mode: "auto"}
	if yc.SVC != nil {
		c.SVC = yc.SVC.mustConvert()
	}
	return c
}

type JoinPolicyConfig struct {
	Concurrency            int
	AlwaysRetryFailedJoins bool
	JoinStartSpacing       time.Duration
}

type YAMLJoinPolicyConfig struct {
	Concurrency            *int `yaml:"concurrency,omitempty"`
	AlwaysRetryFailedJoins bool `yaml:"alwaysRetryFailedJoins,omitempty"`
	// JoinStartSpacing optional duration parsed with time.ParseDuration (e.g. "2s")
	JoinStartSpacing *string `yaml:"joinStartSpacing,omitempty"`
}

func (yc *YAMLJoinPolicyConfig) validate() error {
	var errs []error
	if yc.Concurrency == nil {
		errs = append(errs, ErrMissingJoinPolicyConcurrency)
	} else if *yc.Concurrency < 0 {
		errs = append(errs, ErrNegativeJoinPolicyConcurrency)
	}
	if yc.JoinStartSpacing != nil {
		dur, durErr := time.ParseDuration(*yc.JoinStartSpacing)
		if durErr != nil {
			errs = append(errs, durErr)
		} else if dur < 0 {
			errs = append(errs, ErrNegativeJoinStartSpacing)
		}
	}
	return errors.Join(errs...)
}

func (yc *YAMLJoinPolicyConfig) mustConvert() JoinPolicyConfig {
	var c JoinPolicyConfig
	c.Concurrency = *yc.Concurrency
	c.AlwaysRetryFailedJoins = yc.AlwaysRetryFailedJoins
	if yc.JoinStartSpacing != nil {
		dur, durErr := time.ParseDuration(*yc.JoinStartSpacing)
		if durErr != nil {
			panic(durErr)
		}
		c.JoinStartSpacing = dur
	}
	return c
}

type RecordingConfig struct {
	Enabled   bool
	Directory string
}

type YAMLRecordingConfig struct {
	Enabled   *bool   `yaml:"enabled,omitempty"`
	Directory *string `yaml:"directory,omitempty"`
}

func (yc *YAMLRecordingConfig) validate() error {
	if yc.Enabled == nil || *yc.Enabled == false {
		return nil
	}
	if yc.Directory == nil || strings.TrimSpace(*yc.Directory) == "" {
		return ErrMissingRecordingDirectory
	}
	return nil
}

func (yc *YAMLRecordingConfig) mustConvert() RecordingConfig {
	var c RecordingConfig
	c.Enabled = false
	c.Directory = ""
	if yc.Enabled != nil && *yc.Enabled {
		c.Enabled = true
		c.Directory = *yc.Directory
	}
	return c
}

type PacketCaptureConfig struct {
	Enabled   bool
	Directory string
}

type YAMLPacketCaptureConfig struct {
	Enabled   *bool   `yaml:"enabled,omitempty"`
	Directory *string `yaml:"directory,omitempty"`
}

func (yc *YAMLPacketCaptureConfig) validate() error {
	if yc.Enabled == nil || !*yc.Enabled {
		return nil
	}
	if yc.Directory == nil || strings.TrimSpace(*yc.Directory) == "" {
		return ErrMissingPacketCaptureDirectory
	}
	return nil
}

func (yc *YAMLPacketCaptureConfig) mustConvert() PacketCaptureConfig {
	var c PacketCaptureConfig
	if yc.Enabled != nil && *yc.Enabled {
		c.Enabled = true
		c.Directory = *yc.Directory
	}
	return c
}

type NetworkConfig struct {
	ServerIP   string
	ServerPort int
	ClientIP   string
	Impairment *ImpairmentConfig
}

type ImpairmentConfig struct {
	Profiles    map[string]*ImpairmentProfile
	Assignments []ImpairmentAssignment
	Default     string
}

type ImpairmentProfile struct {
	Name         string
	BandwidthBps int
	BaseLatency  time.Duration
	JitterStddev time.Duration
	LossPercent  int
	Seed         int64
}

type ImpairmentAssignment struct {
	Match   AssignmentSelector
	Profile string
}

type AssignmentSelector struct {
	Role          string
	UserIDPattern string
	UserIndexMin  *int
	UserIndexMax  *int
}

type YAMLNetworkConfig struct {
	ServerIP   *string               `yaml:"serverIP,omitempty"`
	ServerPort *int                  `yaml:"serverPort,omitempty"`
	ClientIP   *string               `yaml:"clientIP,omitempty"`
	Impairment *YAMLImpairmentConfig `yaml:"impairment,omitempty"`
}

type YAMLImpairmentConfig struct {
	Profiles    map[string]*YAMLImpairmentProfile `yaml:"profiles"`
	Assignments []YAMLImpairmentAssignment        `yaml:"assignments,omitempty"`
	Default     string                            `yaml:"default"`
}

type YAMLImpairmentProfile struct {
	BandwidthKbps *int    `yaml:"bandwidthKbps,omitempty"`
	Latency       *string `yaml:"latency,omitempty"`
	Jitter        *string `yaml:"jitter,omitempty"`
	LossPercent   *int    `yaml:"lossPercent,omitempty"`
	Seed          *int64  `yaml:"seed,omitempty"`
}

type YAMLImpairmentAssignment struct {
	Match   YAMLAssignmentSelector `yaml:"match"`
	Profile string                 `yaml:"profile"`
}

type YAMLAssignmentSelector struct {
	Role           *string `yaml:"role,omitempty"`
	UserIDPattern  *string `yaml:"userIDPattern,omitempty"`
	UserIndexRange *[2]int `yaml:"userIndexRange,omitempty"`
}

func (yc *YAMLNetworkConfig) validate() error {
	var errs []error
	if yc.ServerIP == nil {
		errs = append(errs, ErrMissingServerIP)
	} else if net.ParseIP(*yc.ServerIP) == nil {
		errs = append(errs, ErrInvalidServerIP)
	}
	if yc.ServerPort != nil && (*yc.ServerPort < 0 || *yc.ServerPort > 65535) {
		errs = append(errs, ErrInvalidServerPort)
	}
	if yc.ClientIP != nil && *yc.ClientIP != "" && net.ParseIP(*yc.ClientIP) == nil {
		errs = append(errs, ErrInvalidClientIP)
	}
	if yc.Impairment != nil {
		if yc.ServerPort == nil || *yc.ServerPort <= 0 {
			errs = append(errs, ErrMissingServerPortForImpairment)
		}
		errs = append(errs, validateImpairment(yc.Impairment)...)
	}
	return errors.Join(errs...)
}

func validateImpairment(c *YAMLImpairmentConfig) []error {
	var errs []error

	if len(c.Profiles) == 0 {
		errs = append(errs, ErrImpairmentProfilesEmpty)
		return errs
	}

	for name, p := range c.Profiles {
		if name == "" {
			errs = append(errs, ErrImpairmentProfileNameEmpty)
		}
		if p.BandwidthKbps != nil && *p.BandwidthKbps < 0 {
			errs = append(errs, ErrImpairmentProfileBandwidthNeg)
		}
		if p.Latency != nil {
			d, err := time.ParseDuration(*p.Latency)
			if err != nil {
				errs = append(errs, fmt.Errorf("%w: %v", ErrImpairmentProfileLatencyNeg, err))
			} else if d < 0 {
				errs = append(errs, ErrImpairmentProfileLatencyNeg)
			}
		}
		if p.Jitter != nil {
			d, err := time.ParseDuration(*p.Jitter)
			if err != nil {
				errs = append(errs, fmt.Errorf("%w: %v", ErrImpairmentProfileJitterNeg, err))
			} else if d < 0 {
				errs = append(errs, ErrImpairmentProfileJitterNeg)
			} else if d > 0 {
				errs = append(errs, fmt.Errorf("profile %q: %w", name, ErrImpairmentProfileJitterUnsupported))
			}
		}
		if p.LossPercent != nil && (*p.LossPercent < 0 || *p.LossPercent > 100) {
			errs = append(errs, ErrImpairmentProfileLossRange)
		}
	}

	if c.Default == "" {
		errs = append(errs, ErrImpairmentDefaultMissing)
	} else if _, ok := c.Profiles[c.Default]; !ok {
		errs = append(errs, ErrImpairmentDefaultUnknown)
	}

	for i, a := range c.Assignments {
		if a.Profile == "" {
			errs = append(errs, fmt.Errorf("(@%d): %w", i, ErrImpairmentAssignmentProfileEmpty))
		} else if _, ok := c.Profiles[a.Profile]; !ok {
			errs = append(errs, fmt.Errorf("(@%d): %w", i, ErrImpairmentAssignmentProfileUnknown))
		}

		hasSelector := a.Match.Role != nil || a.Match.UserIDPattern != nil || a.Match.UserIndexRange != nil
		if !hasSelector {
			errs = append(errs, fmt.Errorf("(@%d): %w", i, ErrImpairmentAssignmentMatchEmpty))
		}

		if a.Match.Role != nil && *a.Match.Role != "sender" && *a.Match.Role != "viewer" {
			errs = append(errs, fmt.Errorf("(@%d): %w", i, ErrImpairmentAssignmentRoleInvalid))
		}

		if a.Match.UserIDPattern != nil {
			if _, err := path.Match(*a.Match.UserIDPattern, ""); err != nil {
				errs = append(errs, fmt.Errorf("(@%d): %w", i, ErrImpairmentAssignmentPatternInvalid))
			}
		}

		if a.Match.UserIndexRange != nil {
			r := *a.Match.UserIndexRange
			if r[0] >= 0 && r[1] >= 0 && r[0] > r[1] {
				errs = append(errs, fmt.Errorf("(@%d): %w", i, ErrImpairmentAssignmentIndexRange))
			}
		}
	}

	return errs
}

func (yc *YAMLNetworkConfig) mustConvert() NetworkConfig {
	var c NetworkConfig
	c.ServerIP = *yc.ServerIP
	if yc.ServerPort != nil {
		c.ServerPort = *yc.ServerPort
	}
	if yc.ClientIP != nil {
		c.ClientIP = *yc.ClientIP
	}
	if yc.Impairment != nil {
		c.Impairment = mustConvertImpairment(yc.Impairment)
	}
	return c
}

func mustConvertImpairment(yc *YAMLImpairmentConfig) *ImpairmentConfig {
	c := &ImpairmentConfig{
		Profiles:    make(map[string]*ImpairmentProfile),
		Assignments: make([]ImpairmentAssignment, len(yc.Assignments)),
		Default:     yc.Default,
	}

	for name, yp := range yc.Profiles {
		p := &ImpairmentProfile{Name: name}
		if yp.BandwidthKbps != nil {
			p.BandwidthBps = *yp.BandwidthKbps * 1000
		}
		if yp.Latency != nil {
			d, _ := time.ParseDuration(*yp.Latency)
			p.BaseLatency = d
		}
		if yp.Jitter != nil {
			d, _ := time.ParseDuration(*yp.Jitter)
			p.JitterStddev = d
		}
		if yp.LossPercent != nil {
			p.LossPercent = *yp.LossPercent
		}
		if yp.Seed != nil {
			p.Seed = *yp.Seed
		}
		c.Profiles[name] = p
	}

	for i, ya := range yc.Assignments {
		a := ImpairmentAssignment{Profile: ya.Profile}
		if ya.Match.Role != nil {
			a.Match.Role = *ya.Match.Role
		}
		if ya.Match.UserIDPattern != nil {
			a.Match.UserIDPattern = *ya.Match.UserIDPattern
		}
		if ya.Match.UserIndexRange != nil {
			minV := (*ya.Match.UserIndexRange)[0]
			maxV := (*ya.Match.UserIndexRange)[1]
			if minV >= 0 {
				a.Match.UserIndexMin = &minV
			}
			if maxV >= 0 {
				a.Match.UserIndexMax = &maxV
			}
		}
		c.Assignments[i] = a
	}

	return c
}

type LoggingConfig struct {
	Console      bool
	ConsoleLevel string
	Color        bool
	Directory    string
	FileLevel    string
	Combined     bool
	Streams      []LogStreamConfig
}

type YAMLLoggingConfig struct {
	Console      bool                  `yaml:"console"`
	ConsoleLevel *string               `yaml:"consoleLevel,omitempty"`
	Color        *bool                 `yaml:"color,omitempty"`
	Directory    *string               `yaml:"directory,omitempty"`
	FileLevel    *string               `yaml:"fileLevel,omitempty"`
	Combined     *bool                 `yaml:"combined,omitempty"`
	Streams      []YAMLLogStreamConfig `yaml:"streams,omitempty"`
}

func (yc *YAMLLoggingConfig) validate() error {
	var (
		errs       []error
		streamErrs []error
	)
	if yc.Directory == nil || strings.TrimSpace(*yc.Directory) == "" {
		errs = append(errs, ErrMissingLoggingDirectory)
	}
	if yc.ConsoleLevel != nil && !logLevelRegex.MatchString(*yc.ConsoleLevel) {
		errs = append(errs, ErrInvalidConsoleLevel)
	}
	if yc.FileLevel != nil && !logLevelRegex.MatchString(*yc.FileLevel) {
		errs = append(errs, ErrInvalidFileLevel)
	}
	for i, stream := range yc.Streams {
		streamErr := stream.validate()
		if streamErr != nil {
			streamErrs = append(streamErrs, fmt.Errorf("(@%d)%w", i, streamErr))
		}
	}
	if streamErrs != nil {
		errs = append(errs, streamErrs...)
	}
	return errors.Join(errs...)
}

func (yc *YAMLLoggingConfig) mustConvert() LoggingConfig {
	var c LoggingConfig
	c.Console = yc.Console
	c.ConsoleLevel = "info"
	if yc.ConsoleLevel != nil {
		c.ConsoleLevel = *yc.ConsoleLevel
	}
	if yc.Color != nil {
		c.Color = *yc.Color
	}
	c.Directory = *yc.Directory
	c.FileLevel = "debug"
	if yc.FileLevel != nil {
		c.FileLevel = *yc.FileLevel
	}
	if yc.Combined != nil {
		c.Combined = *yc.Combined
	}
	for _, stream := range yc.Streams {
		c.Streams = append(c.Streams, stream.mustConvert())
	}
	return c
}

type LogStreamConfig struct {
	Name  string
	Level string
}

type YAMLLogStreamConfig struct {
	Name  *string `yaml:"name,omitempty"`
	Level *string `yaml:"level,omitempty"`
}

func (yc *YAMLLogStreamConfig) validate() error {
	var errs []error
	if yc.Name == nil {
		errs = append(errs, ErrMissingLogStreamName)
	} else if !nameRegex.MatchString(*yc.Name) {
		errs = append(errs, ErrInvalidLogStreamName)
	}
	if yc.Level == nil {
		errs = append(errs, ErrMissingLogStreamLevel)
	} else if !logLevelRegex.MatchString(*yc.Level) {
		errs = append(errs, ErrInvalidLogStreamLevel)
	}
	return errors.Join(errs...)
}

func (yc *YAMLLogStreamConfig) mustConvert() LogStreamConfig {
	var c LogStreamConfig
	c.Name = *yc.Name
	c.Level = *yc.Level
	return c
}
