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

	ErrMissingRecordingDirectory = errors.New("missing spec.conference.recording.directory pathname")

	ErrMissingServerIP = errors.New("missing spec.network.serverIP address")
	ErrInvalidServerIP = errors.New("invalid spec.network.serverIP address")
	ErrInvalidClientIP = errors.New("invalid spec.network.clientIP address")

	ErrMissingLoggingDirectory = errors.New("missing spec.logging.directory pathname")

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
	APIVersion     string
	Kind           string
	Metadata       MetadataConfig
	Spec           SpecConfig
	StatsConsumers []func(vp9_stats.Period, vp9_stats.VideoQualitySample)
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
	Port int
}

type YAMLMetricsConfig struct {
	Port *int `yaml:"port,omitempty"`
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
	if yc.Metrics != nil && yc.Metrics.Port != nil {
		c.Metrics.Port = *yc.Metrics.Port
	}
	return c
}

type ConferenceConfig struct {
	Name         string
	UsersPerRoom int
	TotalRooms   int
	Cameras      CameraConfig
	JoinPolicy   JoinPolicyConfig
	Recording    RecordingConfig
}

type YAMLConferenceConfig struct {
	// Name conference room identifier
	Name *string `yaml:"name,omitempty"`
	// UsersPerRoom number of users per conference room
	UsersPerRoom *int `yaml:"usersPerRoom,omitempty"`
	// TotalRooms defaults to 1
	TotalRooms *int `yaml:"totalRooms,omitempty"`
	// Cameras optional config for some (or all) users to turn on their cameras
	Cameras    *YAMLCameraConfig     `yaml:"cameras,omitempty"`
	JoinPolicy *YAMLJoinPolicyConfig `yaml:"joinPolicy,omitempty"`
	// Recording optional section, defaults to disabled
	Recording *YAMLRecordingConfig `yaml:"recording,omitempty"`
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
	return c
}

type CameraConfig struct {
	PerRoom    int
	FileType   string
	VideoCodec string
	Directory  string
}

type YAMLCameraConfig struct {
	PerRoom    *int    `yaml:"perRoom,omitempty"`
	FileType   *string `yaml:"fileType,omitempty"`
	VideoCodec *string `yaml:"videoCodec,omitempty"`
	Directory  *string `yaml:"directory,omitempty"`
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
	return errors.Join(errs...)
}

func (yc *YAMLCameraConfig) mustConvert() CameraConfig {
	var c CameraConfig
	c.PerRoom = *yc.PerRoom
	c.FileType = *yc.FileType
	c.VideoCodec = *yc.VideoCodec
	c.Directory = *yc.Directory
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
	Console   bool
	Directory string
	Streams   []LogStreamConfig
}

type YAMLLoggingConfig struct {
	Console   bool                  `yaml:"console"`
	Directory *string               `yaml:"directory,omitempty"`
	Streams   []YAMLLogStreamConfig `yaml:"streams,omitempty"`
}

func (yc *YAMLLoggingConfig) validate() error {
	var (
		errs       []error
		streamErrs []error
	)
	if yc.Directory == nil || strings.TrimSpace(*yc.Directory) == "" {
		errs = append(errs, ErrMissingLoggingDirectory)
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
	c.Directory = *yc.Directory
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
