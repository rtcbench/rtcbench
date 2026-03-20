// Minimal mediasoup signaling server for call.zip e2e testing.
//
// Implements the subset of the mediasoup-demo protoo protocol needed by the
// call.zip mediasoup plugin: room join, WebRtcTransport creation, produce,
// consume, and consumer resume. One worker, one WebRtcServer (single port),
// rooms created on demand.

'use strict';

const mediasoup = require('mediasoup');
const protoo = require('protoo-server');
const http = require('http');

// ---------------------------------------------------------------------------
// Configuration (from environment)
// ---------------------------------------------------------------------------

const config = {
  listenIp:    process.env.LISTEN_IP    || '0.0.0.0',
  announcedIp: process.env.ANNOUNCED_IP || undefined,
  httpPort:    parseInt(process.env.HTTP_PORT    || '4443'),
  webRtcPort:  parseInt(process.env.WEBRTC_PORT  || '44444'),
  mediasoup: {
    worker: {
      logLevel: 'warn',
      logTags:  ['rtp', 'rtcp', 'ice', 'dtls'],
      rtcMinPort: 40000,
      rtcMaxPort: 40099,
    },
    router: {
      mediaCodecs: [
        {
          kind:       'video',
          mimeType:   'video/VP9',
          clockRate:  90000,
          parameters: { 'profile-id': 0 },
        },
      ],
    },
  },
};

// ---------------------------------------------------------------------------
// Global state
// ---------------------------------------------------------------------------

let worker;
let webRtcServer;
const rooms = new Map(); // roomId -> Room

// ---------------------------------------------------------------------------
// Room
// ---------------------------------------------------------------------------

class Room {
  constructor(id, router) {
    this._id          = id;
    this._router      = router;
    this._protooRoom  = new protoo.Room();
    this._peers       = new Map(); // peerId -> PeerData
  }

  static async create(id) {
    const router = await worker.createRouter({
      mediaCodecs: config.mediasoup.router.mediaCodecs,
    });
    const room = new Room(id, router);
    rooms.set(id, room);
    console.log(`room created [id:${id}]`);
    return room;
  }

  close() {
    this._protooRoom.close();
    this._router.close();
    rooms.delete(this._id);
    console.log(`room closed [id:${this._id}]`);
  }

  // -- peer lifecycle -------------------------------------------------------

  async handlePeerConnect(peerId, protooTransport) {
    // Kick duplicate peer.
    if (this._protooRoom.hasPeer(peerId)) {
      console.log(`closing duplicate peer [peerId:${peerId}]`);
      this._protooRoom.getPeer(peerId).close();
    }

    let peer;
    try {
      peer = await this._protooRoom.createPeer(peerId, protooTransport);
    } catch (err) {
      console.error(`createPeer failed [peerId:${peerId}]:`, err);
      return;
    }

    const peerData = {
      joined:          false,
      rtpCapabilities: null,
      transports:      new Map(), // transportId -> { transport, consuming }
      producers:       new Map(), // producerId -> producer
      consumers:       new Map(), // consumerId -> consumer
    };
    this._peers.set(peerId, peerData);

    peer.on('close', () => {
      if (peerData.joined) {
        for (const other of this._joinedPeersExcept(peerId)) {
          other.notify('peerClosed', { peerId }).catch(() => {});
        }
      }
      for (const { transport } of peerData.transports.values()) {
        transport.close();
      }
      this._peers.delete(peerId);

      // Auto-close empty room after a grace period.
      if (this._protooRoom.peers.length === 0) {
        setTimeout(() => {
          if (this._protooRoom.peers.length === 0 && rooms.has(this._id)) {
            this.close();
          }
        }, 10_000);
      }
    });

    peer.on('request', async (request, accept, reject) => {
      try {
        await this._handleRequest(peer, peerData, request, accept);
      } catch (err) {
        console.error(`request "${request.method}" failed:`, err);
        reject(500, String(err));
      }
    });

    peer.on('notification', async (notification) => {
      try {
        await this._handleNotification(peerData, notification);
      } catch (err) {
        console.error(`notification "${notification.method}" failed:`, err);
      }
    });
  }

  // -- request handler ------------------------------------------------------

  async _handleRequest(peer, peerData, request, accept) {
    switch (request.method) {

      case 'getRouterRtpCapabilities': {
        accept(this._router.rtpCapabilities);
        break;
      }

      case 'createWebRtcTransport': {
        const { producing, consuming } = request.data || {};
        const transport = await this._router.createWebRtcTransport({
          listenInfos: [
            { protocol: 'udp', ip: config.listenIp, announcedAddress: config.announcedIp },
            { protocol: 'tcp', ip: config.listenIp, announcedAddress: config.announcedIp },
          ],
          enableUdp: true,
          enableTcp: true,
          initialAvailableOutgoingBitrate: 1_000_000,
        });
        transport.on('icestatechange', (state) => {
          console.log(`transport ${transport.id} ICE state: ${state}`);
        });
        transport.on('dtlsstatechange', (state) => {
          console.log(`transport ${transport.id} DTLS state: ${state}`);
        });
        peerData.transports.set(transport.id, {
          transport,
          consuming: !!consuming,
        });
        console.log(`transport created [id:${transport.id}, candidates:${JSON.stringify(transport.iceCandidates)}]`);
        accept({
          id:             transport.id,
          iceParameters:  transport.iceParameters,
          iceCandidates:  transport.iceCandidates,
          dtlsParameters: transport.dtlsParameters,
        });
        break;
      }

      case 'connectWebRtcTransport': {
        const { transportId, dtlsParameters } = request.data;
        const td = peerData.transports.get(transportId);
        if (!td) throw new Error(`transport ${transportId} not found`);
        await td.transport.connect({ dtlsParameters });
        accept();
        break;
      }

      case 'join': {
        const { rtpCapabilities, displayName } = request.data || {};
        peerData.rtpCapabilities = rtpCapabilities;
        peerData.joined = true;

        // Existing peers list.
        const peers = [];
        for (const [id, data] of this._peers) {
          if (id !== peer.id && data.joined) {
            peers.push({ id, displayName: id });
          }
        }
        accept({ peers });

        // Notify others.
        for (const other of this._joinedPeersExcept(peer.id)) {
          other.notify('newPeer', {
            id: peer.id,
            displayName: displayName || peer.id,
          }).catch(() => {});
        }

        // Create consumers for every existing producer.
        for (const [otherPeerId, otherData] of this._peers) {
          if (otherPeerId === peer.id || !otherData.joined) continue;
          for (const producer of otherData.producers.values()) {
            await this._createConsumer(peer, peerData, otherPeerId, producer);
          }
        }
        break;
      }

      case 'produce': {
        const { transportId, kind, rtpParameters, appData } = request.data;
        const td = peerData.transports.get(transportId);
        if (!td) throw new Error(`transport ${transportId} not found`);

        const producer = await td.transport.produce({
          kind,
          rtpParameters,
          appData: appData || {},
        });
        peerData.producers.set(producer.id, producer);
        producer.on('transportclose', () => peerData.producers.delete(producer.id));

        accept({ id: producer.id });

        // Push consumer to every other joined peer.
        for (const [otherPeerId, otherData] of this._peers) {
          if (otherPeerId === peer.id || !otherData.joined) continue;
          const otherPeer = this._protooRoom.getPeer(otherPeerId);
          if (otherPeer) {
            await this._createConsumer(otherPeer, otherData, peer.id, producer);
          }
        }
        break;
      }

      default:
        console.warn(`unknown request "${request.method}"`);
        throw new Error(`unknown method "${request.method}"`);
    }
  }

  // -- notification handler -------------------------------------------------

  async _handleNotification(peerData, notification) {
    switch (notification.method) {
      case 'resumeConsumer': {
        const { consumerId } = notification.data;
        const consumer = peerData.consumers.get(consumerId);
        if (consumer) await consumer.resume();
        break;
      }
      case 'closeProducer': {
        const { producerId } = notification.data;
        const producer = peerData.producers.get(producerId);
        if (producer) {
          producer.close();
          peerData.producers.delete(producerId);
        }
        break;
      }
      default:
        console.warn(`unknown notification "${notification.method}"`);
    }
  }

  // -- consumer creation ----------------------------------------------------

  async _createConsumer(consumerPeer, consumerPeerData, producerPeerId, producer) {
    if (!consumerPeerData.rtpCapabilities) return;
    if (!this._router.canConsume({
      producerId:      producer.id,
      rtpCapabilities: consumerPeerData.rtpCapabilities,
    })) return;

    // Find a consuming transport.
    let consumerTransport;
    for (const td of consumerPeerData.transports.values()) {
      if (td.consuming) { consumerTransport = td.transport; break; }
    }
    if (!consumerTransport) return;

    let consumer;
    try {
      consumer = await consumerTransport.consume({
        producerId:      producer.id,
        rtpCapabilities: consumerPeerData.rtpCapabilities,
        paused:          true,
      });
    } catch (err) {
      console.error('consume() failed:', err);
      return;
    }

    consumerPeerData.consumers.set(consumer.id, consumer);
    consumer.on('transportclose', () => consumerPeerData.consumers.delete(consumer.id));
    consumer.on('producerclose', () => {
      consumerPeerData.consumers.delete(consumer.id);
      consumerPeer.notify('consumerClosed', { consumerId: consumer.id }).catch(() => {});
    });

    try {
      await consumerPeer.request('newConsumer', {
        peerId:         producerPeerId,
        producerId:     producer.id,
        id:             consumer.id,
        kind:           consumer.kind,
        rtpParameters:  consumer.rtpParameters,
        type:           consumer.type,
        producerPaused: consumer.producerPaused,
        appData:        producer.appData,
      });
    } catch (err) {
      console.error('newConsumer request to peer failed:', err);
      consumer.close();
      consumerPeerData.consumers.delete(consumer.id);
    }
  }

  // -- helpers --------------------------------------------------------------

  _joinedPeersExcept(excludeId) {
    return this._protooRoom.peers.filter((p) => p.id !== excludeId);
  }
}

// ---------------------------------------------------------------------------
// Startup
// ---------------------------------------------------------------------------

async function main() {
  // Single worker.
  worker = await mediasoup.createWorker({
    logLevel:   config.mediasoup.worker.logLevel,
    logTags:    config.mediasoup.worker.logTags,
    rtcMinPort: config.mediasoup.worker.rtcMinPort,
    rtcMaxPort: config.mediasoup.worker.rtcMaxPort,
  });
  console.log(`mediasoup worker created [pid:${worker.pid}]`);

  worker.on('died', () => {
    console.error('mediasoup worker died, exiting');
    process.exit(1);
  });

  // WebRtcServer — all transports share one port.
  webRtcServer = await worker.createWebRtcServer({
    listenInfos: [
      { protocol: 'udp', ip: config.listenIp, announcedAddress: config.announcedIp, port: config.webRtcPort },
      { protocol: 'tcp', ip: config.listenIp, announcedAddress: config.announcedIp, port: config.webRtcPort },
    ],
  });
  console.log(`WebRtcServer created [port:${config.webRtcPort}]`);

  // HTTP — health check endpoint.
  const httpServer = http.createServer((req, res) => {
    if (req.url === '/health') {
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ status: 'ok', rooms: rooms.size }));
      return;
    }
    res.writeHead(404);
    res.end('Not Found');
  });

  // Protoo WebSocket server.
  const wsServer = new protoo.WebSocketServer(httpServer, {
    maxReceivedFrameSize:   960000,
    maxReceivedMessageSize: 960000,
  });

  wsServer.on('connectionrequest', async (info, accept, reject) => {
    const u = new URL(info.request.url, `http://${info.request.headers.host}`);
    const roomId = u.searchParams.get('roomId');
    const peerId = u.searchParams.get('peerId');

    if (!roomId || !peerId) {
      reject(400, 'Missing roomId or peerId query parameters');
      return;
    }

    console.log(`protoo connection [roomId:${roomId}, peerId:${peerId}]`);

    let room = rooms.get(roomId);
    if (!room) {
      room = await Room.create(roomId);
    }

    const protooTransport = accept();
    await room.handlePeerConnect(peerId, protooTransport);
  });

  httpServer.listen(config.httpPort, () => {
    console.log(`mediasoup server listening on port ${config.httpPort}`);
  });
}

main().catch((err) => {
  console.error('startup failed:', err);
  process.exit(1);
});
