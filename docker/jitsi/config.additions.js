// VP9 overrides for call.zip CI load testing.
// Appended to the base jitsi/web config.js at image build time.
// Affects browser clients only - call.zip negotiates VP9 via its own Jingle/SDP path.

config.resolution = 1080;
config.disableSimulcast = true;

config.videoQuality = Object.assign(config.videoQuality || {}, {
    codecPreferenceOrder: ['VP9', 'VP8', 'H264', 'AV1'],
    mobileCodecPreferenceOrder: ['VP8', 'VP9', 'H264'],
    maxBitratesVideo: {
        VP9: {
            low:      100000,
            standard: 1000000,
            high:     2000000,
            fullHd:   3500000,
            ultraHd:  3500000,
            ssHigh:   3500000,
        },
    },
    minHeightForQualityLvl: { 720: 'standard', 1080: 'high' },
    VP9: {
        scalabilityModeEnabled: false,
        useSimulcast: false,
        useKSVC: false,
    },
});
