// webrtcperf customUrlHandler: disable VP9 simulcast in jitsi-meet by
// monkey-patching WebRTC APIs to force single-encoding at high bitrate.
module.exports = async function(page) {
  await page.evaluateOnNewDocument(() => {
    // Force addTransceiver to use only 1 encoding (no simulcast).
    const origAddTransceiver = RTCPeerConnection.prototype.addTransceiver;
    RTCPeerConnection.prototype.addTransceiver = function(trackOrKind, init) {
      if (init && init.sendEncodings && init.sendEncodings.length > 1) {
        console.log('[bench] stripped simulcast: ' + init.sendEncodings.length + ' -> 1 encoding');
        init.sendEncodings = [{maxBitrate: 3500000}];
      }
      return origAddTransceiver.call(this, trackOrKind, init);
    };

    // Force setParameters to use only 1 encoding at 3.5 Mbps.
    const origSetParams = RTCRtpSender.prototype.setParameters;
    RTCRtpSender.prototype.setParameters = function(params) {
      if (params && params.encodings && params.encodings.length > 1) {
        console.log('[bench] setParameters: forcing single encoding');
        params.encodings = [{
          ...params.encodings[0],
          maxBitrate: 3500000,
          scaleResolutionDownBy: undefined,
        }];
      } else if (params && params.encodings && params.encodings.length === 1) {
        params.encodings[0].maxBitrate = Math.max(params.encodings[0].maxBitrate || 0, 3500000);
      }
      return origSetParams.call(this, params);
    };

    console.log('[bench] monkey-patched addTransceiver+setParameters to disable simulcast');
  });
};
