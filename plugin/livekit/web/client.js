import { Room, RoomEvent } from "livekit-client";

export async function join({ proxy, data, video, remove, status }) {
  const room = new Room({ adaptiveStream: false, dynacast: false });
  room.on(RoomEvent.TrackSubscribed, (track, publication, participant) => {
    if (track.kind === "video")
      video(publication.trackSid, track.mediaStreamTrack, participant.identity);
  });
  room.on(RoomEvent.TrackUnsubscribed, (_track, publication) =>
    remove(publication.trackSid),
  );
  room.on(RoomEvent.Disconnected, () =>
    status("Disconnected. Leave and join again to reconnect."),
  );
  const address = new URL(proxy);
  address.protocol = address.protocol === "https:" ? "wss:" : "ws:";
  try {
    await room.connect(address.href, data.token);
  } catch (error) {
    await room.disconnect();
    throw error;
  }
  let publication;
  return {
    async camera(track) {
      if (publication)
        await room.localParticipant.unpublishTrack(publication.track);
      publication = track
        ? await room.localParticipant.publishTrack(track, {
            videoCodec: "vp9",
            simulcast: false,
          })
        : undefined;
    },
    close: () => room.disconnect(),
  };
}
