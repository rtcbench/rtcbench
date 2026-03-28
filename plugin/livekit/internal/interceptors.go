package internal

import (
	"sync/atomic"

	"call.zip/pkg/arrival"
	"call.zip/pkg/gcc"
	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/cc"
	piongcc "github.com/pion/interceptor/pkg/gcc"
	"github.com/pion/interceptor/pkg/nack"
	"github.com/pion/interceptor/pkg/report"
	"github.com/pion/interceptor/pkg/twcc"

	sdkinterceptor "github.com/livekit/server-sdk-go/v2/pkg/interceptor"
)

// SenderInterceptors builds the LiveKit SDK default interceptor chain plus
// a GCC send-side bandwidth estimator. Returns the factories and a function
// that returns the latest GCC estimate in bps.
func SenderInterceptors(initialBitrateBps int) ([]interceptor.Factory, func() int, error) {
	var targetBitrate atomic.Int64
	targetBitrate.Store(int64(initialBitrateBps))

	nackGen := &sdkinterceptor.NackGeneratorInterceptorFactory{}
	nackResp, err := nack.NewResponderInterceptor()
	if err != nil {
		return nil, nil, err
	}

	rtcpReceiver, err := report.NewReceiverInterceptor()
	if err != nil {
		return nil, nil, err
	}
	rtcpSender, err := report.NewSenderInterceptor()
	if err != nil {
		return nil, nil, err
	}

	twccSender, err := twcc.NewSenderInterceptor()
	if err != nil {
		return nil, nil, err
	}

	limitSize := sdkinterceptor.NewLimitSizeInterceptorFactory()

	gccFactory, err := cc.NewInterceptor(func() (cc.BandwidthEstimator, error) {
		return piongcc.NewSendSideBWE(
			piongcc.SendSideBWEInitialBitrate(initialBitrateBps),
			// Cap below the S2T2 selection threshold (1.08 Mbps for a 1.2 Mbps
			// SVC target) so GCC stays in the S2T1 regime (15 fps @ 1080p)
			// during unconstrained phases. Without this cap GCC climbs to 50 Mbps
			// and selects S2T2 (30 fps), which diverges from Chromium's native
			// behavior (~15 fps) and also causes slower adaptation when tc
			// constrains the path (more AIMD steps to shed the inflated estimate).
			piongcc.SendSideBWEMaxBitrate(1_000_000),
			piongcc.SendSideBWEMinBitrate(100_000),
			piongcc.SendSideBWEPacer(piongcc.NewNoOpPacer()),
		)
	})
	if err != nil {
		return nil, nil, err
	}
	gccFactory.OnNewPeerConnection(func(_ string, estimator cc.BandwidthEstimator) {
		estimator.OnTargetBitrateChange(func(bitrate int) {
			targetBitrate.Store(int64(bitrate))
		})
	})

	// twccExt must be outer to gcc so transport-cc headers are set before
	// GCC's feedback adapter reads them.
	factories := []interceptor.Factory{
		nackGen,
		nackResp,
		rtcpReceiver,
		rtcpSender,
		gccFactory,
		twccSender,
		gcc.TWCCExtFactory{},
		limitSize,
	}

	getBitrate := func() int {
		return int(targetBitrate.Load())
	}
	return factories, getBitrate, nil
}

// ViewerInterceptors builds the LiveKit SDK default interceptor chain for a
// receive-only participant, plus an arrival-time interceptor that timestamps
// incoming RTP packets before pion's internal buffer.
func ViewerInterceptors() ([]interceptor.Factory, error) {
	nackGen := &sdkinterceptor.NackGeneratorInterceptorFactory{}
	nackResp, err := nack.NewResponderInterceptor()
	if err != nil {
		return nil, err
	}

	rtcpReceiver, err := report.NewReceiverInterceptor()
	if err != nil {
		return nil, err
	}
	rtcpSender, err := report.NewSenderInterceptor()
	if err != nil {
		return nil, err
	}

	twccSender, err := twcc.NewSenderInterceptor()
	if err != nil {
		return nil, err
	}

	limitSize := sdkinterceptor.NewLimitSizeInterceptorFactory()

	// arrival.Factory must be outermost so it timestamps packets before any
	// other interceptor processing.
	factories := []interceptor.Factory{
		arrival.Factory{},
		nackGen,
		nackResp,
		rtcpReceiver,
		rtcpSender,
		twccSender,
		limitSize,
	}
	return factories, nil
}
