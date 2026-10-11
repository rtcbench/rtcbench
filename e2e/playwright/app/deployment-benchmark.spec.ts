import { test, expect } from "@playwright/test";

const runs = JSON.parse(process.env.RTCBENCH_WEB_BENCHMARKS ?? "{}");

for (const plugin of ["livekit", "janus", "mediasoup", "jitsi"]) {
  test(`${plugin} browser receives live benchmark cameras`, async ({
    page,
    context,
    request,
  }, info) => {
    test.skip(
      !runs[plugin],
      "Set RTCBENCH_WEB_BENCHMARKS to active run details",
    );
    test.setTimeout(180_000);
    const run = runs[plugin];
    const errors: string[] = [];
    const response = await request.get(`api/v1/runs/${run.id}`);
    expect(response.ok()).toBe(true);
    expect((await response.json()).status).toBe("running");
    await page.goto(`run/${run.id}`);
    const opened = context.waitForEvent("page");
    await page.getByRole("link", { name: "Join room", exact: true }).click();
    const client = await opened;
    client.on("pageerror", (error) => errors.push(error.message));
    await expect(client.getByLabel("Room", { exact: true })).toHaveValue(
      run.room,
    );
    expect(new URL(client.url()).searchParams.get("room")).toBe(run.room);
    await client
      .getByRole("button", { name: "Join room", exact: true })
      .click();
    await expect(client.getByRole("status")).toHaveText(
      `Connected to ${run.room}`,
      {
        timeout: 45_000,
      },
    );
    const videos = client.locator('video[data-local="false"]');
    await expect(videos).toHaveCount(4, { timeout: 60_000 });
    const frames = () =>
      videos.evaluateAll((elements: HTMLVideoElement[]) =>
        elements.map((video) => ({
          width: video.videoWidth,
          height: video.videoHeight,
          frames: video.getVideoPlaybackQuality().totalVideoFrames,
        })),
      );
    await expect
      .poll(
        async () =>
          (await frames()).every(
            (video) =>
              video.width === 1920 &&
              video.height === 1080 &&
              video.frames > 25,
          ),
        { timeout: 60_000 },
      )
      .toBe(true);
    const before = await frames();
    await expect
      .poll(
        async () =>
          (await frames()).every(
            (video, index) => video.frames > before[index]!.frames + 20,
          ),
        { timeout: 30_000 },
      )
      .toBe(true);
    await info.attach(`${plugin}-benchmark-streams`, {
      body: await client.screenshot(),
      contentType: "image/png",
    });
    await client.getByRole("button", { name: "Leave room" }).click();
    await expect(videos).toHaveCount(0);
    expect(errors).toEqual([]);
    expect(
      (await (await request.get(`api/v1/runs/${run.id}`)).json()).status,
    ).toBe("running");
  });
}

test("run room link uses the saved config and hides when its room is missing", async ({
  page,
  context,
}) => {
  const run = Object.values(runs)[0] as
    { id: string; deployment: string } | undefined;
  test.skip(!run, "Set RTCBENCH_WEB_BENCHMARKS to active run details");
  let roomName: string | undefined;
  await page.route(`**/api/v1/runs/${run!.id}`, async (route) => {
    const response = await route.fetch();
    await route.fulfill({
      response,
      json: { ...(await response.json()), roomName },
    });
  });
  let config = "spec:\n  conference:\n    name: research-room_42\n";
  await page.route(`**/api/v1/runs/${run!.id}/files/config.yml`, (route) =>
    route.fulfill({ contentType: "text/yaml", body: config }),
  );
  await page.goto(`run/${run!.id}`);
  const opened = context.waitForEvent("page");
  await page.getByRole("link", { name: "Join room", exact: true }).click();
  const client = await opened;
  await expect(client.getByLabel("Room", { exact: true })).toHaveValue(
    "research-room_42",
  );
  expect(new URL(client.url()).searchParams.get("deployment")).toBe(
    run!.deployment,
  );
  await client.close();
  roomName = "resolved-room_67";
  config = "spec:\n  conference:\n    name: $env:ROOM_NAME\n";
  await page.reload();
  const resolved = context.waitForEvent("page");
  await page.getByRole("link", { name: "Join room", exact: true }).click();
  const resolvedClient = await resolved;
  await expect(resolvedClient.getByLabel("Room", { exact: true })).toHaveValue(
    roomName,
  );
  await resolvedClient.close();
  roomName = undefined;
  config = "spec: {}";
  await page.reload();
  await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Join room", exact: true }),
  ).toHaveCount(0);
});
