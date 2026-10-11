export async function join({ proxy, room, identity, video, remove, status }) {
  const roomID = Number(room.match(/\d+$/)?.[0]);
  if (!Number.isSafeInteger(roomID) || roomID <= 0)
    throw Error(
      "Janus room names must end in a positive room number, such as room-1234",
    );
  const endpoint = new URL("janus", proxy);
  const pending = new Map(),
    subscribers = new Map(),
    pcs = new Set();
  const abort = new AbortController();
  let sid,
    publisher,
    pubPC,
    closed = false;
  const post = async (path, body) => {
    const response = await fetch(endpoint + path, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ transaction: crypto.randomUUID(), ...body }),
      signal: abort.signal,
    });
    if (!response.ok) throw Error("Janus signaling failed");
    const value = await response.json();
    if (value.error) throw Error(value.error.reason);
    return value;
  };
  function deliver(message) {
    const item = pending.get(message.transaction);
    if (item) {
      clearTimeout(item.timer);
      pending.delete(message.transaction);
      const error = message.plugindata?.data?.error || message.error?.reason;
      if (error) item.reject(Error(error));
      else item.resolve(message);
    }
    const data = message.plugindata?.data;
    for (const feed of data?.publishers ?? [])
      void subscribe(feed).catch((error) => status(error.message));
    const leaving = data?.leaving ?? data?.unpublished;
    if (leaving && leaving !== "ok") {
      subscribers.get(leaving)?.close();
      subscribers.delete(leaving);
      remove(String(leaving));
    }
  }
  function message(handle, body, jsep) {
    return new Promise((resolve, reject) => {
      const transaction = crypto.randomUUID();
      const timer = setTimeout(() => {
        pending.delete(transaction);
        reject(Error("Janus request timed out"));
      }, 15000);
      pending.set(transaction, { resolve, reject, timer });
      post(`/${sid}/${handle}`, { janus: "message", transaction, body, jsep })
        .then((reply) => {
          if (reply.janus !== "ack") deliver(reply);
        })
        .catch((error) => {
          clearTimeout(timer);
          pending.delete(transaction);
          reject(error);
        });
    });
  }
  async function attach() {
    return (
      await post(`/${sid}`, {
        janus: "attach",
        plugin: "janus.plugin.videoroom",
      })
    ).data.id;
  }
  function peer(handle) {
    const pc = new RTCPeerConnection();
    pcs.add(pc);
    pc.onicecandidate = (event) => {
      void post(`/${sid}/${handle}`, {
        janus: "trickle",
        candidate: event.candidate?.toJSON() ?? { completed: true },
      }).catch(() => {});
    };
    return pc;
  }
  async function subscribe(feed) {
    if (subscribers.has(feed.id)) return;
    subscribers.set(feed.id, null);
    const handle = await attach(),
      pc = peer(handle);
    subscribers.set(feed.id, pc);
    pc.ontrack = (event) => {
      if (event.track.kind === "video")
        video(String(feed.id), event.track, feed.display || String(feed.id));
    };
    const offer = await message(handle, {
      request: "join",
      ptype: "subscriber",
      room: roomID,
      streams: [{ feed: feed.id }],
    });
    await pc.setRemoteDescription(offer.jsep);
    await pc.setLocalDescription(await pc.createAnswer());
    await message(
      handle,
      { request: "start", room: roomID },
      { type: "answer", sdp: pc.localDescription.sdp },
    );
  }
  async function close() {
    if (closed) return;
    closed = true;
    for (const pc of pcs) pc.close();
    for (const item of pending.values()) {
      clearTimeout(item.timer);
      item.reject(Error("Disconnected"));
    }
    pending.clear();
    if (sid) await post(`/${sid}`, { janus: "destroy" }).catch(() => {});
    abort.abort();
  }
  try {
    sid = (await post("", { janus: "create" })).data.id;
    void (async () => {
      while (!closed) {
        const response = await fetch(`${endpoint}/${sid}?maxev=10`, {
          signal: abort.signal,
        });
        if (!response.ok) throw Error("Janus disconnected");
        const events = await response.json();
        for (const event of Array.isArray(events) ? events : [events])
          deliver(event);
      }
    })().catch((error) => {
      if (!closed) {
        status(error.message);
        void close();
      }
    });
    publisher = await attach();
    const exists = await message(publisher, {
      request: "exists",
      room: roomID,
    });
    if (!exists.plugindata.data.exists)
      await message(publisher, {
        request: "create",
        room: roomID,
        publishers: 100,
        videocodec: "vp9",
        bitrate: 0,
      });
    await message(publisher, {
      request: "join",
      ptype: "publisher",
      room: roomID,
      display: identity,
    });
    return {
      async camera(track) {
        if (pubPC) {
          await message(publisher, { request: "unpublish" });
          pubPC.close();
          pubPC = undefined;
        }
        if (!track) return;
        pubPC = peer(publisher);
        const transceiver = pubPC.addTransceiver(track, {
          direction: "sendonly",
          streams: [new MediaStream([track])],
        });
        const codecs = RTCRtpSender.getCapabilities("video").codecs.filter(
          (codec) => codec.mimeType.toLowerCase() === "video/vp9",
        );
        if (codecs.length) transceiver.setCodecPreferences(codecs);
        await pubPC.setLocalDescription(await pubPC.createOffer());
        const answer = await message(
          publisher,
          {
            request: "configure",
            video: true,
            audio: false,
            videocodec: "vp9",
          },
          { type: "offer", sdp: pubPC.localDescription.sdp },
        );
        await pubPC.setRemoteDescription(answer.jsep);
      },
      close,
    };
  } catch (error) {
    await close();
    throw error;
  }
}
