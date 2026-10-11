const $ = (id) => document.getElementById(id);
const params = new URL(location.href).searchParams;
const deployment = params.get("deployment");
$("room").value = params.get("room") || $("room").value;
const api = new URL(
  `api/v1/deployments/${encodeURIComponent(deployment)}/`,
  location.href,
);
let session,
  cameraStream,
  cameraOn = false;
const tiles = new Map();
const status = (text) => {
  $("status").textContent = text;
};
function video(id, track, label = id, local = false) {
  remove(id);
  const figure = document.createElement("figure");
  const element = document.createElement("video");
  element.autoplay = element.playsInline = element.muted = true;
  element.dataset.local = String(local);
  element.srcObject = new MediaStream([track]);
  const caption = document.createElement("figcaption");
  caption.textContent = label;
  figure.append(element, caption);
  $("videos").append(figure);
  tiles.set(id, figure);
  track.addEventListener("ended", () => remove(id), { once: true });
  element.play().catch(() => {});
}
function remove(id) {
  tiles.get(id)?.remove();
  tiles.delete(id);
}
async function leave() {
  cameraStream?.getTracks().forEach((track) => track.stop());
  cameraStream = undefined;
  cameraOn = false;
  const previous = session;
  session = undefined;
  for (const id of tiles.keys()) remove(id);
  $("camera").hidden = $("leave").hidden = true;
  $("join").disabled = $("room").disabled = false;
  $("camera").textContent = "Enable camera";
  try {
    await previous?.close();
  } catch (error) {
    status(error.message);
  }
}
document.querySelector("form").onsubmit = async (event) => {
  event.preventDefault();
  $("join").disabled = $("room").disabled = true;
  status("Joining…");
  try {
    const response = await fetch(new URL("join", api), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ room: $("room").value }),
    });
    const config = await response.json();
    if (!response.ok) throw Error(config.error);
    if (!/^[a-z][a-z0-9-]*$/.test(config.plugin)) throw Error("Invalid plugin");
    const adapter = `./deployment-${config.plugin}.js`;
    const { join } = await import(adapter);
    session = await join({
      ...config,
      proxy: new URL("proxy/", api),
      video,
      remove,
      status,
    });
    $("camera").hidden = $("leave").hidden = false;
    document.querySelector("h1").textContent = config.name;
    document.title = `${config.room} | ${config.name}`;
    status(`Connected to ${config.room}`);
  } catch (error) {
    await leave();
    status(error.message);
  }
};
$("camera").onclick = async () => {
  $("camera").disabled = $("leave").disabled = true;
  try {
    if (cameraOn) {
      await session.camera(null);
      cameraStream?.getTracks().forEach((track) => track.stop());
      cameraStream = undefined;
      remove("self");
    } else {
      cameraStream = await navigator.mediaDevices.getUserMedia({
        video: { width: { ideal: 1920 }, height: { ideal: 1080 } },
        audio: false,
      });
      const track = cameraStream.getVideoTracks()[0];
      await session.camera(track);
      video("self", track, "You", true);
    }
    cameraOn = !cameraOn;
    $("camera").textContent = cameraOn ? "Disable camera" : "Enable camera";
  } catch (error) {
    cameraStream?.getTracks().forEach((track) => track.stop());
    cameraOn = false;
    $("camera").textContent = "Enable camera";
    status(error.message);
  } finally {
    $("camera").disabled = $("leave").disabled = false;
  }
};
$("leave").onclick = async () => {
  await leave();
  status("Disconnected");
};
addEventListener("pagehide", () => {
  void leave();
});
