package rtcbench

import "github.com/rtcbench/rtcbench/internal/core"

type (
	Client      = core.Client
	User        = core.User
	UserConfig  = core.UserConfig
	UserRole    = core.UserRole
	UserMetrics = core.UserMetrics

	Plugin         = core.Plugin
	PluginEnv      = core.PluginEnv
	PluginFactory  = core.PluginFactory
	PluginRegistry = core.PluginRegistry
	Participant    = core.Participant
	VideoPublisher = core.VideoPublisher
	StatsPipeline  = core.StatsPipeline

	Scenario         = core.Scenario
	ScenarioEnv      = core.ScenarioEnv
	ScenarioFactory  = core.ScenarioFactory
	ScenarioRegistry = core.ScenarioRegistry
	DefaultScenario  = core.DefaultScenario
	ChurnScenario    = core.ChurnScenario

	JoinRequest           = core.JoinRequest
	LeaveRequest          = core.LeaveRequest
	PublishVideoRequest   = core.PublishVideoRequest
	UnpublishVideoRequest = core.UnpublishVideoRequest
	ConnectionState       = core.ConnectionState
)

type (
	Config              = core.Config
	MetadataConfig      = core.MetadataConfig
	SpecConfig          = core.SpecConfig
	ConferenceConfig    = core.ConferenceConfig
	CameraConfig        = core.CameraConfig
	SVCCameraConfig     = core.SVCCameraConfig
	JoinPolicyConfig    = core.JoinPolicyConfig
	RecordingConfig     = core.RecordingConfig
	PacketCaptureConfig = core.PacketCaptureConfig
	NetworkConfig       = core.NetworkConfig
	LoggingConfig       = core.LoggingConfig
	LogStreamConfig     = core.LogStreamConfig
	MetricsConfig       = core.MetricsConfig
	RunThresholdConfig  = core.RunThresholdConfig

	ImpairmentProfile        = core.ImpairmentProfile
	ImpairmentConfig         = core.ImpairmentConfig
	ImpairmentAssignment     = core.ImpairmentAssignment
	AssignmentSelector       = core.AssignmentSelector
	ReceiverThresholdConfig  = core.ReceiverThresholdConfig
	ReceiverSummary          = core.ReceiverSummary
	ReceiverThresholdFailure = core.ReceiverThresholdFailure
)

type (
	YAMLConfig              = core.YAMLConfig
	YAMLMetadataConfig      = core.YAMLMetadataConfig
	YAMLSpecConfig          = core.YAMLSpecConfig
	YAMLConferenceConfig    = core.YAMLConferenceConfig
	YAMLCameraConfig        = core.YAMLCameraConfig
	YAMLSVCCamConfig        = core.YAMLSVCCamConfig
	YAMLJoinPolicyConfig    = core.YAMLJoinPolicyConfig
	YAMLRecordingConfig     = core.YAMLRecordingConfig
	YAMLPacketCaptureConfig = core.YAMLPacketCaptureConfig
	YAMLNetworkConfig       = core.YAMLNetworkConfig
	YAMLLoggingConfig       = core.YAMLLoggingConfig
	YAMLLogStreamConfig     = core.YAMLLogStreamConfig
	YAMLMetricsConfig       = core.YAMLMetricsConfig
	YAMLRunThresholdConfig  = core.YAMLRunThresholdConfig
)

type (
	MetricsCollector   = core.MetricsCollector
	Labels             = core.Labels
	RunMetricsSnapshot = core.RunMetricsSnapshot
	RunMetricsSummary  = core.RunMetricsSummary
	OperationSummary   = core.OperationSummary
	HistogramSummary   = core.HistogramSummary
	HistogramSnapshot  = core.HistogramSnapshot
	GaugeSummary       = core.GaugeSummary
	ThresholdFailure   = core.ThresholdFailure
)

const (
	APIVersionV1            = core.APIVersionV1
	KindVideoCallStressTest = core.KindVideoCallStressTest

	DefaultScenarioID = core.DefaultScenarioID
	ChurnScenarioID   = core.ChurnScenarioID

	Viewer = core.Viewer
	Sender = core.Sender

	StateNew    = core.StateNew
	StateJoined = core.StateJoined
	StateLeft   = core.StateLeft
	StateClosed = core.StateClosed
)

var (
	ErrUnsupportedRole       = core.ErrUnsupportedRole
	ErrUnsupportedCapability = core.ErrUnsupportedCapability
	ErrCannotJoinRoom        = core.ErrCannotJoinRoom
	ErrConnectionNotJoined   = core.ErrConnectionNotJoined
	ErrUnknownPlugin         = core.ErrUnknownPlugin
	ErrUnknownScenario       = core.ErrUnknownScenario
	ErrConnectionExists      = core.ErrConnectionExists
	ErrUserExists            = core.ErrUserExists
	ErrUserClosed            = core.ErrUserClosed
	ErrMissingRoomID         = core.ErrMissingRoomID
	ErrImpairmentUnsupported = core.ErrImpairmentUnsupported

	ErrMissingAPIVersion = core.ErrMissingAPIVersion
	ErrInvalidAPIVersion = core.ErrInvalidAPIVersion
	ErrMissingKind       = core.ErrMissingKind
	ErrInvalidKind       = core.ErrInvalidKind

	ErrMissingMetadata = core.ErrMissingMetadata
	ErrMissingName     = core.ErrMissingName
	ErrInvalidName     = core.ErrInvalidName

	ErrMissingSpec           = core.ErrMissingSpec
	ErrMissingPlugin         = core.ErrMissingPlugin
	ErrInvalidPlugin         = core.ErrInvalidPlugin
	ErrInvalidScenario       = core.ErrInvalidScenario
	ErrInvalidScenarioConfig = core.ErrInvalidScenarioConfig
	ErrMissingConference     = core.ErrMissingConference
	ErrMissingNetwork        = core.ErrMissingNetwork
	ErrMissingLogging        = core.ErrMissingLogging

	ErrMissingConferenceName   = core.ErrMissingConferenceName
	ErrInvalidConferenceName   = core.ErrInvalidConferenceName
	ErrMissingUsersPerRoom     = core.ErrMissingUsersPerRoom
	ErrNonPositiveUsersPerRoom = core.ErrNonPositiveUsersPerRoom
	ErrNonPositiveTotalRooms   = core.ErrNonPositiveTotalRooms
	ErrMissingJoinPolicy       = core.ErrMissingJoinPolicy

	ErrMissingCameraCountPerRoom   = core.ErrMissingCameraCountPerRoom
	ErrInvalidCameraCountPerRoom   = core.ErrInvalidCameraCountPerRoom
	ErrMissingCameraFileType       = core.ErrMissingCameraFileType
	ErrUnsupportedCameraFileType   = core.ErrUnsupportedCameraFileType
	ErrMissingCameraVideoCodec     = core.ErrMissingCameraVideoCodec
	ErrUnsupportedCameraVideoCodec = core.ErrUnsupportedCameraVideoCodec
	ErrMissingCameraDirectory      = core.ErrMissingCameraDirectory

	ErrMissingJoinPolicyConcurrency  = core.ErrMissingJoinPolicyConcurrency
	ErrNegativeJoinPolicyConcurrency = core.ErrNegativeJoinPolicyConcurrency
	ErrNegativeJoinStartSpacing      = core.ErrNegativeJoinStartSpacing

	ErrInvalidStatsBufferSize        = core.ErrInvalidStatsBufferSize
	ErrInvalidSVCMode                = core.ErrInvalidSVCMode
	ErrInvalidSVCSpatialLayers       = core.ErrInvalidSVCSpatialLayers
	ErrInvalidSVCTemporalLayers      = core.ErrInvalidSVCTemporalLayers
	ErrMissingRecordingDirectory     = core.ErrMissingRecordingDirectory
	ErrMissingPacketCaptureDirectory = core.ErrMissingPacketCaptureDirectory

	ErrMissingServerIP = core.ErrMissingServerIP
	ErrInvalidServerIP = core.ErrInvalidServerIP
	ErrInvalidClientIP = core.ErrInvalidClientIP

	ErrMissingLoggingDirectory = core.ErrMissingLoggingDirectory
	ErrInvalidConsoleLevel     = core.ErrInvalidConsoleLevel
	ErrInvalidFileLevel        = core.ErrInvalidFileLevel
	ErrInvalidMetricsThreshold = core.ErrInvalidMetricsThreshold

	ErrMissingLogStreamName  = core.ErrMissingLogStreamName
	ErrInvalidLogStreamName  = core.ErrInvalidLogStreamName
	ErrMissingLogStreamLevel = core.ErrMissingLogStreamLevel
	ErrInvalidLogStreamLevel = core.ErrInvalidLogStreamLevel
)

var (
	SupportedCameraVideoCodecs = core.SupportedCameraVideoCodecs
	SupportedCameraFileTypes   = core.SupportedCameraFileTypes
)

var (
	NewClient                  = core.NewClient
	NewStatsPipeline           = core.NewStatsPipeline
	EvaluateRunThresholds      = core.EvaluateRunThresholds
	EvaluateReceiverThresholds = core.EvaluateReceiverThresholds
	LoadCamerasFromConfig      = core.LoadCamerasFromConfig
	SetupPacketCaptureDir      = core.SetupPacketCaptureDir
)
