import { test, expect, uniqueName, select } from "./fixtures";

for (const plugin of ["livekit", "janus", "jitsi", "mediasoup"]) {
  test(`${plugin} run receives video, stops and remains in Runs`, async ({
    page,
    request,
  }, info) => {
    test.skip(
      !process.env.RTCBENCH_MEDIA_DIR,
      "Needs managed SFUs and server-side VP9 media",
    );
    test.setTimeout(180_000);
    const inventory = await (await request.get("api/v1/systems")).json();
    const deployment = inventory.deployments.find(
      (d: { plugin: string; status: string }) =>
        d.plugin === plugin && d.status === "ready",
    );
    expect(deployment, `A ready ${plugin} deployment is required`).toBeTruthy();
    const name = uniqueName(plugin);
    const config = `apiVersion: rtcbench/v1
kind: VideoCallStressTest
metadata:
  name: ${name}
spec:
  plugin: ${plugin}
  conference:
    name: room-1234
    usersPerRoom: 2
    totalRooms: 1
    cameras:
      perRoom: 1
      fileType: ivf
      videoCodec: vp9
      directory: ${JSON.stringify(process.env.RTCBENCH_MEDIA_DIR)}
      inMemory: true
    joinPolicy:
      concurrency: 2
      alwaysRetryFailedJoins: true
      joinStartSpacing: 2s
  logging:
    directory: ./logs
    streams:
      - name: general
        level: info
      - name: signaling
        level: info
      - name: video_stats
        level: info
`;
    let id: string | undefined;
    try {
      expect(
        (
          await request.post("api/v1/configs", { data: { name, yaml: config } })
        ).ok(),
      ).toBe(true);
      await page.goto("runs");
      await page.getByRole("button", { name: "New run", exact: true }).click();
      await page.getByRole("textbox", { name: "Name", exact: true }).fill(name);
      await select(page, "Config", name);
      await select(page, "Deployment", deployment.name);
      const started = page.waitForResponse(
        (r) =>
          r.url().endsWith("/api/v1/runs") && r.request().method() === "POST",
      );
      await page.getByRole("button", { name: "Start", exact: true }).click();
      const response = await started;
      expect(response.ok(), await response.text()).toBe(true);
      id = (await response.json()).id;
      await expect(
        page.getByRole("heading", { name, exact: true }),
      ).toBeVisible();
      await expect
        .poll(
          async () => {
            const metrics = await (
              await request.get(`api/v1/runs/${id}/viewers`)
            ).json();
            return metrics.viewers.some(
              (v: { frames_complete: number; smooth_bitrate_bps: number }) =>
                v.frames_complete >= 30 && v.smooth_bitrate_bps > 0,
            );
          },
          { timeout: 120_000 },
        )
        .toBe(true);
      await info.attach("receiving-video", {
        body: await page.screenshot(),
        contentType: "image/png",
      });
      await page.getByRole("button", { name: "Stop", exact: true }).click();
      await page
        .getByRole("alertdialog")
        .getByRole("button", { name: "Stop", exact: true })
        .click();
      await expect(
        page.getByRole("button", { name: "Run again", exact: true }),
      ).toBeVisible();
      const run = await (await request.get(`api/v1/runs/${id}`)).json();
      expect(run.status).toBe("completed");
      expect(run.error).toBeFalsy();
      expect(run.summary.Operations.length).toBeGreaterThan(0);
      expect(
        run.summary.Operations.every(
          (op: { Failures: number }) => op.Failures === 0,
        ),
      ).toBe(true);
      await page.goto("runs");
      await expect(
        page.getByRole("row", { name: new RegExp(name) }),
      ).toContainText("completed");
    } finally {
      if (id) await request.post(`api/v1/runs/${id}/stop`);
      if (!process.env.RTCBENCH_KEEP_RUNS) {
        if (id) await request.delete(`api/v1/runs/${id}`);
        await request.delete(`api/v1/configs/${name}`);
      }
    }
  });
}
