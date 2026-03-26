package gcc

import (
	"sync/atomic"

	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/cc"
	piongcc "github.com/pion/interceptor/pkg/gcc"
	"github.com/pion/interceptor/pkg/nack"
	"github.com/pion/interceptor/pkg/report"
	"github.com/pion/interceptor/pkg/twcc"
)

// SenderFactories builds interceptor factories for a sender PeerConnection
// with GCC bandwidth estimation. Returns the factories and a function that
// returns the current GCC estimate in bps.
//
// The chain includes NACK, RTCP reports, TWCC, GCC, and transport-cc
// header extension injection. Suitable for raw pion PeerConnections.
func SenderFactories(initialBitrateBps int) ([]interceptor.Factory, func() int, error) {
	var targetBitrate atomic.Int64
	targetBitrate.Store(int64(initialBitrateBps))

	nackGen, err := nack.NewGeneratorInterceptor()
	if err != nil {
		return nil, nil, err
	}
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

	gccFactory, err := cc.NewInterceptor(func() (cc.BandwidthEstimator, error) {
		return piongcc.NewSendSideBWE(
			piongcc.SendSideBWEInitialBitrate(initialBitrateBps),
			piongcc.SendSideBWEMaxBitrate(50_000_000),
			piongcc.SendSideBWEMinBitrate(100_000),
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
		TWCCExtFactory{},
	}

	getBitrate := func() int {
		return int(targetBitrate.Load())
	}
	return factories, getBitrate, nil
}

// BuildRegistry wraps factories into an interceptor.Registry for pion API.
func BuildRegistry(factories []interceptor.Factory) *interceptor.Registry {
	r := &interceptor.Registry{}
	for _, f := range factories {
		r.Add(f)
	}
	return r
}
