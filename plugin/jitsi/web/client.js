export async function join({
  proxy,
  room,
  identity,
  data,
  video,
  remove,
  status,
}) {
  await new Promise((resolve, reject) => {
    const script = document.createElement("script");
    script.src = new URL("libs/lib-jitsi-meet.min.js", proxy);
    script.onload = resolve;
    script.onerror = () => reject(Error("Could not load Jitsi"));
    document.head.append(script);
  });
  const Jitsi = window.JitsiMeetJS;
  Jitsi.init({ disableAudioLevels: true });
  const connection = new Jitsi.JitsiConnection(null, null, {
    hosts: { domain: data.domain, muc: `conference.${data.domain}` },
    serviceUrl: new URL("http-bind", proxy).href,
  });
  let conference, local;
  const close = async () => {
    await conference?.leave();
    connection.disconnect();
    local?.dispose();
  };
  try {
    await new Promise((resolve, reject) => {
      connection.addEventListener(
        Jitsi.events.connection.CONNECTION_ESTABLISHED,
        resolve,
      );
      connection.addEventListener(
        Jitsi.events.connection.CONNECTION_FAILED,
        (error) => reject(Error(String(error))),
      );
      connection.connect();
    });
    conference = connection.initJitsiConference(room.toLowerCase(), {
      p2p: { enabled: false },
      videoQuality: { preferredCodec: "VP9", enforcePreferredCodec: true },
    });
    conference.setDisplayName(identity);
    conference.on(Jitsi.events.conference.TRACK_ADDED, (track) => {
      if (track.isLocal() || track.getType() !== "video") return;
      const render = () =>
        track.isMuted()
          ? remove(track.getId())
          : video(track.getId(), track.getTrack(), track.getParticipantId());
      track.on(Jitsi.events.track.TRACK_MUTE_CHANGED, render);
      render();
    });
    conference.on(Jitsi.events.conference.TRACK_REMOVED, (track) =>
      remove(track.getId()),
    );
    conference.on(Jitsi.events.conference.CONFERENCE_LEFT, () =>
      status("Disconnected"),
    );
    await new Promise((resolve, reject) => {
      conference.on(Jitsi.events.conference.CONFERENCE_JOINED, resolve);
      conference.on(Jitsi.events.conference.CONFERENCE_FAILED, (error) =>
        reject(Error(String(error))),
      );
      conference.join();
    });
    conference.setReceiverConstraints({
      lastN: -1,
      selectedSources: [],
      onStageSources: [],
      defaultConstraints: { maxHeight: 1080 },
    });
    return {
      async camera(track) {
        if (local) {
          await local.mute();
          await conference.removeTrack(local);
          local.dispose();
          local = undefined;
        }
        if (track) {
          const tracks = Jitsi.createLocalTracksFromMediaStreams([
            {
              stream: new MediaStream([track]),
              track,
              mediaType: "video",
              constraints: track.getConstraints(),
              videoType: "camera",
            },
          ]);
          local = tracks[0];
          await conference.addTrack(local);
        }
      },
      close,
    };
  } catch (error) {
    await close();
    throw error;
  }
}
