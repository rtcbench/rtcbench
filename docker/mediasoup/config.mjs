import * as os from 'node:os';

export const config = {
	domain: process.env['DOMAIN'] || 'localhost',
	http: {
		listenIp: '0.0.0.0',
		listenPort: Number(process.env['HTTP_PORT'] || 4443),
		tls: {
			cert: '/app/tls.crt',
			key: '/app/tls.key',
		},
	},
	mediasoup: {
		numWorkers: Object.keys(os.cpus()).length,
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
					port: 44444,
				},
				{
					protocol: 'tcp',
					ip: process.env['MEDIASOUP_LISTEN_IP'] || '0.0.0.0',
					announcedAddress: process.env['MEDIASOUP_ANNOUNCED_ADDRESS'],
					port: 44444,
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
