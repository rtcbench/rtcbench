package core

import (
	"testing"
	"time"
)

func TestEvaluateRunThresholdsPasses(t *testing.T) {
	thresholds := []RunThresholdConfig{
		{
			Op:             "join",
			Scenario:       manualScenarioLabel,
			Plugin:         "fake",
			Role:           "sender",
			MaxFailureRate: float64Ptr(0.01),
			MaxP95:         durationPtr(5 * time.Second),
		},
	}

	summary := RunMetricsSummary{
		Operations: []OperationSummary{
			{
				Scenario:    manualScenarioLabel,
				Plugin:      "fake",
				Role:        "sender",
				Operation:   "join",
				Attempts:    10,
				Successes:   10,
				Failures:    0,
				FailureRate: 0,
				P95Seconds:  1.5,
			},
		},
	}

	if err := EvaluateRunThresholds(summary, thresholds); err != nil {
		t.Fatalf("EvaluateRunThresholds() error = %v, want nil", err)
	}
}

func TestEvaluateRunThresholdsFails(t *testing.T) {
	thresholds := []RunThresholdConfig{
		{
			Op:             "join",
			Plugin:         "fake",
			MaxFailureRate: float64Ptr(0.10),
			MaxP95:         durationPtr(2 * time.Second),
		},
	}

	summary := RunMetricsSummary{
		Operations: []OperationSummary{
			{
				Scenario:    manualScenarioLabel,
				Plugin:      "fake",
				Role:        "viewer",
				Operation:   "join",
				Attempts:    10,
				Successes:   8,
				Failures:    2,
				FailureRate: 0.20,
				P95Seconds:  3.0,
			},
		},
	}

	err := EvaluateRunThresholds(summary, thresholds)
	if err == nil {
		t.Fatal("EvaluateRunThresholds() error = nil, want failure")
	}
	if !containsAll(err.Error(), "failure_rate", "exceeds") {
		t.Fatalf("EvaluateRunThresholds() error = %v, want failure_rate message", err)
	}
}

func TestYAMLMetricsThresholdsValidateAndConvert(t *testing.T) {
	role := string(Sender)
	op := "join"
	maxFailureRate := 0.05
	maxP95 := "2s"
	metrics := &YAMLMetricsConfig{
		Thresholds: []YAMLRunThresholdConfig{
			{
				Op:             &op,
				Plugin:         stringPtr("fake"),
				Role:           &role,
				MaxFailureRate: &maxFailureRate,
				MaxP95:         &maxP95,
			},
		},
	}

	if err := metrics.validate(); err != nil {
		t.Fatalf("validate() error = %v", err)
	}

	converted := metrics.Thresholds[0].mustConvert()
	if converted.Op != "join" || converted.Plugin != "fake" || converted.Role != string(Sender) {
		t.Fatalf("converted threshold = %+v, want join/fake/sender", converted)
	}
	if converted.MaxFailureRate == nil || *converted.MaxFailureRate != 0.05 {
		t.Fatalf("converted MaxFailureRate = %v, want 0.05", converted.MaxFailureRate)
	}
	if converted.MaxP95 == nil || *converted.MaxP95 != 2*time.Second {
		t.Fatalf("converted MaxP95 = %v, want 2s", converted.MaxP95)
	}
}

func TestYAMLReceiverThresholdsValidateAndConvert(t *testing.T) {
	metrics := &YAMLMetricsConfig{
		ReceiverThresholds: []YAMLReceiverThresholdConfig{
			{
				Plugin:       stringPtr("janus"),
				Profile:      stringPtr("lossy-wifi"),
				MaxMeanRTTMS: float64Ptr(120.0),
			},
		},
	}

	if err := metrics.validate(); err != nil {
		t.Fatalf("validate() error = %v", err)
	}

	converted := metrics.ReceiverThresholds[0].mustConvert()
	if converted.Plugin != "janus" || converted.Profile != "lossy-wifi" {
		t.Fatalf("converted threshold = %+v, want janus/lossy-wifi", converted)
	}
	if converted.MaxMeanRTTMS == nil || *converted.MaxMeanRTTMS != 120.0 {
		t.Fatalf("converted MaxMeanRTTMS = %v, want 120", converted.MaxMeanRTTMS)
	}
}
