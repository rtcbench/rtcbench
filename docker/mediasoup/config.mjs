import * as os from 'node:os';

const numWorkers = os.cpus().length;
const signalingPort = Number(process.env['HTTP_PORT'] || 4443);
const mediaPort = Number(process.env['MEDIASOUP_MEDIA_PORT'] || 44444);
const lastMediaPort = mediaPort + numWorkers - 1;
if (!Number.isInteger(mediaPort) || mediaPort < 1 || lastMediaPort > 65535 ||
	(signalingPort >= mediaPort && signalingPort <= lastMediaPort)) {
	throw new Error(`Invalid media ports ${mediaPort}-${lastMediaPort} for ${numWorkers} workers; ports must fit within 1-65535 and not overlap signaling`);
}

export const config = {
	domain: process.env['DOMAIN'] || 'localhost',
	http: {
		listenIp: '0.0.0.0',
		listenPort: signalingPort,
		tls: {
			cert: '/app/tls.crt',
			key: '/app/tls.key',
		},
	},
	mediasoup: {
		numWorkers,
		workerSettings: {
			dtlsCertificateFile: undefined,
			dtlsPrivateKeyFile: undefined,
			logLevel: 'warn',
			logTags: ['rtp', 'rtcp', 'ice', 'dtls'],
			disableLiburing: false,
		},
		routerOptions: {
			mediaCodecs: [
				{
					kind: 'video',
					mimeType: 'video/VP9',
					clockRate: 90000,
					parameters: { 'profile-id': 0 },
				},
			],
		},
		webRtcServerOptions: {
			listenInfos: [
				{
					protocol: 'udp',
					ip: process.env['MEDIASOUP_LISTEN_IP'] || '0.0.0.0',
					announcedAddress: process.env['MEDIASOUP_ANNOUNCED_ADDRESS'],
					port: mediaPort,
				},
				{
					protocol: 'tcp',
					ip: process.env['MEDIASOUP_LISTEN_IP'] || '0.0.0.0',
					announcedAddress: process.env['MEDIASOUP_ANNOUNCED_ADDRESS'],
					port: mediaPort,
				},
			],
		},
		webRtcTransportOptions: {
			initialAvailableOutgoingBitrate: Number(process.env['INITIAL_OUTGOING_BITRATE'] || 1000000),
			minimumAvailableOutgoingBitrate: Number(process.env['MIN_OUTGOING_BITRATE'] || 600000),
			maxSctpMessageSize: 262144,
		},
		plainTransportOptions: {
			listenInfo: {
				protocol: 'udp',
				ip: process.env['MEDIASOUP_LISTEN_IP'] || '0.0.0.0',
				announcedAddress: process.env['MEDIASOUP_ANNOUNCED_ADDRESS'],
				portRange: { min: 40000, max: 40999 },
			},
			maxSctpMessageSize: 262144,
		},
	},
};
