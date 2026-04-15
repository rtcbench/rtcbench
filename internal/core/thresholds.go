package core

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type ThresholdFailure struct {
	Threshold RunThresholdConfig
	Actual    OperationSummary
	Reason    string
}

func (f ThresholdFailure) Error() string {
	scope := []string{
		"op=" + f.Actual.Operation,
		"scenario=" + f.Actual.Scenario,
		"plugin=" + f.Actual.Plugin,
		"role=" + f.Actual.Role,
	}
	return fmt.Sprintf("%s: %s", strings.Join(scope, " "), f.Reason)
}

func EvaluateRunThresholds(summary RunMetricsSummary, thresholds []RunThresholdConfig) error {
	if len(thresholds) == 0 {
		return nil
	}

	var errs []error
	for _, threshold := range thresholds {
		matched := 0
		for _, actual := range summary.Operations {
			if !threshold.matches(actual) {
				continue
			}
			matched++
			if failure := threshold.evaluate(actual); failure != nil {
				errs = append(errs, failure)
			}
		}
		if matched == 0 {
			errs = append(errs, fmt.Errorf(
				"op=%s scenario=%s plugin=%s role=%s: no matching run metric series",
				threshold.Op,
				fallbackThresholdValue(threshold.Scenario),
				fallbackThresholdValue(threshold.Plugin),
				fallbackThresholdValue(threshold.Role),
			))
		}
	}
	return errors.Join(errs...)
}

func (t RunThresholdConfig) matches(actual OperationSummary) bool {
	if t.Op != "" && t.Op != actual.Operation {
		return false
	}
	if t.Scenario != "" && t.Scenario != actual.Scenario {
		return false
	}
	if t.Plugin != "" && t.Plugin != actual.Plugin {
		return false
	}
	if t.Role != "" && t.Role != actual.Role {
		return false
	}
	return true
}

func (t RunThresholdConfig) evaluate(actual OperationSummary) error {
	if t.MaxFailureRate != nil && actual.FailureRate > *t.MaxFailureRate {
		return ThresholdFailure{
			Threshold: t,
			Actual:    actual,
			Reason:    fmt.Sprintf("failure_rate %.4f exceeds %.4f", actual.FailureRate, *t.MaxFailureRate),
		}
	}
	if t.MaxMean != nil && time.Duration(actual.MeanSeconds*float64(time.Second)) > *t.MaxMean {
		return ThresholdFailure{
			Threshold: t,
			Actual:    actual,
			Reason:    fmt.Sprintf("mean %s exceeds %s", durationString(actual.MeanSeconds), t.MaxMean.String()),
		}
	}
	if t.MaxP95 != nil && time.Duration(actual.P95Seconds*float64(time.Second)) > *t.MaxP95 {
		return ThresholdFailure{
			Threshold: t,
			Actual:    actual,
			Reason:    fmt.Sprintf("p95 %s exceeds %s", durationString(actual.P95Seconds), t.MaxP95.String()),
		}
	}
	if t.MaxP99 != nil && time.Duration(actual.P99Seconds*float64(time.Second)) > *t.MaxP99 {
		return ThresholdFailure{
			Threshold: t,
			Actual:    actual,
			Reason:    fmt.Sprintf("p99 %s exceeds %s", durationString(actual.P99Seconds), t.MaxP99.String()),
		}
	}
	return nil
}

func fallbackThresholdValue(v string) string {
	if v == "" {
		return metricLabelAll
	}
	return v
}

func durationString(seconds float64) string {
	return (time.Duration(seconds * float64(time.Second))).String()
}

type ReceiverThresholdFailure struct {
	Threshold ReceiverThresholdConfig
	Actual    ReceiverSummary
	Reason    string
}

func (f ReceiverThresholdFailure) Error() string {
	scope := []string{
		"user=" + f.Actual.UserID,
		"scenario=" + f.Actual.Scenario,
		"plugin=" + f.Actual.Plugin,
		"profile=" + f.Actual.Profile,
	}
	return fmt.Sprintf("%s: %s", strings.Join(scope, " "), f.Reason)
}

func EvaluateReceiverThresholds(summaries []ReceiverSummary, thresholds []ReceiverThresholdConfig) error {
	if len(thresholds) == 0 || len(summaries) == 0 {
		return nil
	}

	var errs []error
	for _, threshold := range thresholds {
		matched := 0
		for _, actual := range summaries {
			if !receiverThresholdMatches(threshold, actual) {
				continue
			}
			matched++
			if failure := evaluateReceiverThreshold(threshold, actual); failure != nil {
				errs = append(errs, failure)
			}
		}
		if matched == 0 {
			errs = append(errs, fmt.Errorf(
				"scenario=%s plugin=%s profile=%s: no matching receiver series",
				fallbackThresholdValue(threshold.Scenario),
				fallbackThresholdValue(threshold.Plugin),
				fallbackThresholdValue(threshold.Profile),
			))
		}
	}
	return errors.Join(errs...)
}

func receiverThresholdMatches(t ReceiverThresholdConfig, actual ReceiverSummary) bool {
	if t.Scenario != "" && t.Scenario != actual.Scenario {
		return false
	}
	if t.Plugin != "" && t.Plugin != actual.Plugin {
		return false
	}
	if t.Profile != "" && t.Profile != actual.Profile {
		return false
	}
	if t.UserID != "" && t.UserID != actual.UserID {
		return false
	}
	return true
}

func evaluateReceiverThreshold(t ReceiverThresholdConfig, actual ReceiverSummary) error {
	if t.MaxFreezeCount != nil && actual.FreezeCount > *t.MaxFreezeCount {
		return ReceiverThresholdFailure{
			Threshold: t,
			Actual:    actual,
			Reason:    fmt.Sprintf("freeze_count %d exceeds %d", actual.FreezeCount, *t.MaxFreezeCount),
		}
	}
	if t.MaxFreezeDuration != nil && actual.FreezeDurationTotal > *t.MaxFreezeDuration {
		return ReceiverThresholdFailure{
			Threshold: t,
			Actual:    actual,
			Reason:    fmt.Sprintf("freeze_duration %.2f exceeds %.2f", actual.FreezeDurationTotal, *t.MaxFreezeDuration),
		}
	}
	if t.MaxFrameLossRatio != nil && actual.FrameLossRatio > *t.MaxFrameLossRatio {
		return ReceiverThresholdFailure{
			Threshold: t,
			Actual:    actual,
			Reason:    fmt.Sprintf("frame_loss_ratio %.4f exceeds %.4f", actual.FrameLossRatio, *t.MaxFrameLossRatio),
		}
	}
	if t.MinBitrateBps != nil && actual.MeanBitrateBps < *t.MinBitrateBps {
		return ReceiverThresholdFailure{
			Threshold: t,
			Actual:    actual,
			Reason:    fmt.Sprintf("mean_bitrate_bps %.0f below %.0f", actual.MeanBitrateBps, *t.MinBitrateBps),
		}
	}
	if t.MinFPS != nil && actual.MeanFPS < *t.MinFPS {
		return ReceiverThresholdFailure{
			Threshold: t,
			Actual:    actual,
			Reason:    fmt.Sprintf("mean_fps %.1f below %.1f", actual.MeanFPS, *t.MinFPS),
		}
	}
	if t.MaxJitterUS != nil && actual.MaxJitterUS > *t.MaxJitterUS {
		return ReceiverThresholdFailure{
			Threshold: t,
			Actual:    actual,
			Reason:    fmt.Sprintf("max_jitter_us %.0f exceeds %.0f", actual.MaxJitterUS, *t.MaxJitterUS),
		}
	}
	if t.MaxMeanRTTMS != nil && actual.MeanRTTMS > *t.MaxMeanRTTMS {
		return ReceiverThresholdFailure{
			Threshold: t,
			Actual:    actual,
			Reason:    fmt.Sprintf("mean_rtt_ms %.1f exceeds %.1f", actual.MeanRTTMS, *t.MaxMeanRTTMS),
		}
	}
	if t.MaxPLICount != nil && actual.PliCount > *t.MaxPLICount {
		return ReceiverThresholdFailure{
			Threshold: t,
			Actual:    actual,
			Reason:    fmt.Sprintf("pli_count %d exceeds %d", actual.PliCount, *t.MaxPLICount),
		}
	}
	return nil
}
