import { Device } from "mediasoup-client";

export async function join({ proxy, room, identity, video, remove, status }) {
  const address = new URL(proxy);
  address.protocol = address.protocol === "https:" ? "wss:" : "ws:";
  address.search = new URLSearchParams({ roomId: room, peerId: identity });
  const socket = new WebSocket(address, "protoo");
  const pending = new Map(),
    consumers = new Map();
  let next = 0,
    send,
    receive,
    producer,
    closed = false;
  function request(method, data = {}) {
    return new Promise((resolve, reject) => {
      const id = ++next;
      const timer = setTimeout(() => {
        pending.delete(id);
        reject(Error(`${method} timed out`));
      }, 15000);
      pending.set(id, { resolve, reject, timer });
      socket.send(JSON.stringify({ request: true, id, method, data }));
    });
  }
  function close() {
    if (closed) return;
    closed = true;
    send?.close();
    receive?.close();
    socket.close();
    for (const item of pending.values()) {
      clearTimeout(item.timer);
      item.reject(Error("Disconnected"));
    }
    pending.clear();
  }
  socket.onclose = () => {
    if (closed) return;
    close();
    status("Disconnected. Leave and join again to reconnect.");
  };
  socket.onmessage = async (event) => {
    const message = JSON.parse(event.data);
    const item = pending.get(message.id);
    if (message.response && item) {
      clearTimeout(item.timer);
      pending.delete(message.id);
      if (message.ok) item.resolve(message.data);
      else item.reject(Error(message.errorReason));
    } else if (message.request) {
      try {
        if (message.method !== "newConsumer")
          throw Error("Unsupported request");
        const consumer = await receive.consume({
          ...message.data,
          id: message.data.consumerId,
        });
        consumers.set(consumer.id, consumer);
        video(consumer.id, consumer.track, message.data.peerId);
        socket.send(
          JSON.stringify({
            response: true,
            id: message.id,
            ok: true,
            data: {},
          }),
        );
      } catch (error) {
        socket.send(
          JSON.stringify({
            response: true,
            id: message.id,
            ok: false,
            errorCode: 500,
            errorReason: error.message,
          }),
        );
      }
    } else if (message.notification && message.method === "consumerClosed") {
      consumers.get(message.data.consumerId)?.close();
      consumers.delete(message.data.consumerId);
      remove(message.data.consumerId);
    }
  };
  try {
    await new Promise((resolve, reject) => {
      socket.onopen = resolve;
      socket.onerror = () => reject(Error("Could not connect to deployment"));
    });
    const device = new Device();
    await device.load({
      routerRtpCapabilities: (await request("getRouterRtpCapabilities"))
        .routerRtpCapabilities,
    });
    async function transport(producing) {
      const options = await request("createWebRtcTransport", {
        forceTcp: false,
        appData: { direction: producing ? "producer" : "consumer" },
        sctpCapabilities: undefined,
      });
      const transport = producing
        ? device.createSendTransport({ ...options, id: options.transportId })
        : device.createRecvTransport({ ...options, id: options.transportId });
      transport.on("connect", ({ dtlsParameters }, success, failure) =>
        request("connectWebRtcTransport", {
          transportId: transport.id,
          dtlsParameters,
        }).then(success, failure),
      );
      return transport;
    }
    receive = await transport(false);
    send = await transport(true);
    send.on("produce", ({ kind, rtpParameters, appData }, success, failure) =>
      request("produce", {
        transportId: send.id,
        kind,
        rtpParameters,
        appData,
      }).then(({ producerId }) => success({ id: producerId }), failure),
    );
    await request("join", {
      displayName: identity,
      device: { name: "RTCBench", flag: "browser", version: "1" },
      rtpCapabilities: device.rtpCapabilities,
    });
    return {
      async camera(track) {
        if (producer) {
          socket.send(
            JSON.stringify({
              notification: true,
              method: "closeProducer",
              data: { producerId: producer.id },
            }),
          );
          producer.close();
          producer = undefined;
        }
        if (track)
          producer = await send.produce({
            track,
            appData: { source: "webcam" },
            codec: device.rtpCapabilities.codecs.find(
              (codec) => codec.mimeType.toLowerCase() === "video/vp9",
            ),
          });
      },
      close,
    };
  } catch (error) {
    close();
    throw error;
  }
}
