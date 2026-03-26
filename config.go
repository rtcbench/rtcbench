package call

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"slices"
	"strings"
	"time"

	"call.zip/pkg/vp9_stats"
)

const (
	APIVersionV1            = "call.zip/v1"
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

	ErrMissingSpec       = errors.New("missing spec section")
	ErrMissingPlugin     = errors.New("missing spec.plugin string")
	ErrInvalidPlugin     = errors.New("invalid spec.plugin string")
	ErrMissingConference = errors.New("missing spec.conference section")
	ErrMissingNetwork    = errors.New("missing spec.network section")
	ErrMissingLogging    = errors.New("missing spec.logging section")

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

	ErrMissingServerIP = errors.New("missing spec.network.serverIP address")
	ErrInvalidServerIP = errors.New("invalid spec.network.serverIP address")
	ErrInvalidClientIP = errors.New("invalid spec.network.clientIP address")

	ErrMissingLoggingDirectory = errors.New("missing spec.logging.directory pathname")
	ErrInvalidConsoleLevel     = errors.New("invalid spec.logging.consoleLevel string, must match " + logLevelRegex.String())
	ErrInvalidFileLevel        = errors.New("invalid spec.logging.fileLevel string, must match " + logLevelRegex.String())

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
	Plugin       string
	Conference   ConferenceConfig
	Network      NetworkConfig
	PluginConfig map[string]any
	Logging      LoggingConfig
	Metrics      MetricsConfig
}

type YAMLSpecConfig struct {
	Plugin       *string               `yaml:"plugin,omitempty"`
	Conference   *YAMLConferenceConfig `yaml:"conference,omitempty"`
	Network      *YAMLNetworkConfig    `yaml:"network,omitempty"`
	PluginConfig map[string]any        `yaml:"pluginConfig,omitempty"`
	Logging      *YAMLLoggingConfig    `yaml:"logging,omitempty"`
	Metrics      *YAMLMetricsConfig    `yaml:"metrics,omitempty"`
}

type MetricsConfig struct {
	Port           int
	StatsJSONLPath string
}

type YAMLMetricsConfig struct {
	Port           *int    `yaml:"port,omitempty"`
	StatsJSONLPath *string `yaml:"statsJSONLPath,omitempty"`
}

func (yc *YAMLSpecConfig) validate() error {
	var (
		errs          []error
		conferenceErr error
		networkErr    error
		loggingErr    error
	)
	if yc.Plugin == nil {
		// return early for crucial mistake
		return ErrMissingPlugin
	}
	if !nameRegex.MatchString(*yc.Plugin) {
		// return early for invalid plugin name string
		return ErrInvalidPlugin
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
	return errors.Join(errs...)
}

func (yc *YAMLSpecConfig) mustConvert() SpecConfig {
	var c SpecConfig
	c.Plugin = *yc.Plugin
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
	}
	return c
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
	ServerIP string
	ClientIP string
}

type YAMLNetworkConfig struct {
	ServerIP *string `yaml:"serverIP,omitempty"`
	ClientIP *string `yaml:"clientIP,omitempty"`
}

func (yc *YAMLNetworkConfig) validate() error {
	var errs []error
	if yc.ServerIP == nil {
		errs = append(errs, ErrMissingServerIP)
	} else if net.ParseIP(*yc.ServerIP) == nil {
		errs = append(errs, ErrInvalidServerIP)
	}
	if yc.ClientIP != nil && *yc.ClientIP != "" && net.ParseIP(*yc.ClientIP) == nil {
		errs = append(errs, ErrInvalidClientIP)
	}
	return errors.Join(errs...)
}

func (yc *YAMLNetworkConfig) mustConvert() NetworkConfig {
	var c NetworkConfig
	c.ServerIP = *yc.ServerIP
	if yc.ClientIP != nil {
		c.ClientIP = *yc.ClientIP
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
