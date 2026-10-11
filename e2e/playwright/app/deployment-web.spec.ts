import { test, expect } from "@playwright/test";

const deployments = JSON.parse(process.env.RTCBENCH_WEB_DEPLOYMENTS ?? "{}");
test.use({
  permissions: ["camera"],
  launchOptions: {
    args: [
      "--use-fake-ui-for-media-stream",
      "--use-fake-device-for-media-stream",
    ],
  },
});

for (const plugin of ["livekit", "janus", "mediasoup", "jitsi"]) {
  test(`${plugin} deployment web UI sends and receives camera video`, async ({
    page,
    context,
  }, info) => {
    test.skip(
      !deployments[plugin],
      "Set RTCBENCH_WEB_DEPLOYMENTS to managed deployment IDs",
    );
    test.setTimeout(180_000);
    const errors: string[] = [];
    const room = `room-${Date.now()}`;
    await page.goto(`systems/deployments/${deployments[plugin]}`);
    const opened = context.waitForEvent("page");
    await page.getByRole("link", { name: "Open web UI" }).click();
    const first = await opened;
    const second = await context.newPage();
    await first.waitForURL(/deployment\.html/);
    await second.goto(first.url());
    for (const client of [first, second]) {
      client.on("pageerror", (error) => errors.push(error.message));
      await client.getByLabel("Room", { exact: true }).fill(room);
      await client
        .getByRole("button", { name: "Join room", exact: true })
        .click();
      await expect(client.getByRole("status")).toHaveText(
        `Connected to ${room}`,
        { timeout: 45_000 },
      );
      await expect(client.locator("video")).toHaveCount(0);
    }
    const remote = (client: typeof page) =>
      client.locator('video[data-local="false"]');
    for (const client of [first, second]) {
      await client.bringToFront();
      await client.getByRole("button", { name: "Enable camera" }).click();
      await expect(
        client.getByRole("button", { name: "Disable camera" }),
      ).toBeVisible();
    }
    for (const client of [first, second]) {
      await expect(remote(client)).toHaveCount(1, { timeout: 45_000 });
      await expect
        .poll(
          () =>
            remote(client).evaluate(
              (video: HTMLVideoElement) =>
                video.videoWidth > 0 &&
                video.videoHeight > 0 &&
                video.getVideoPlaybackQuality().totalVideoFrames > 25,
            ),
          { timeout: 45_000 },
        )
        .toBe(true);
    }
    await info.attach(`${plugin}-video`, {
      body: await first.screenshot(),
      contentType: "image/png",
    });
    await first.bringToFront();
    await first.getByRole("button", { name: "Disable camera" }).click();
    await expect(remote(second)).toHaveCount(0, { timeout: 20_000 });
    await first.getByRole("button", { name: "Enable camera" }).click();
    await expect(remote(second)).toHaveCount(1, { timeout: 20_000 });
    await expect
      .poll(
        () =>
          remote(second).evaluate(
            (video: HTMLVideoElement) =>
              video.getVideoPlaybackQuality().totalVideoFrames,
          ),
        { timeout: 20_000 },
      )
      .toBeGreaterThan(25);
    await first.getByRole("button", { name: "Leave room" }).click();
    await expect(first.locator("video")).toHaveCount(0);
    await expect(remote(second)).toHaveCount(0, { timeout: 20_000 });
    await first.getByRole("button", { name: "Join room", exact: true }).click();
    await expect(first.getByRole("status")).toHaveText(`Connected to ${room}`, {
      timeout: 45_000,
    });
    await expect
      .poll(
        () =>
          remote(first).evaluate(
            (video: HTMLVideoElement) =>
              video.getVideoPlaybackQuality().totalVideoFrames,
          ),
        { timeout: 30_000 },
      )
      .toBeGreaterThan(25);
    await first.getByRole("button", { name: "Leave room" }).click();
    await second.getByRole("button", { name: "Leave room" }).click();
    expect(errors).toEqual([]);
  });
}
