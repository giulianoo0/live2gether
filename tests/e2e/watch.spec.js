const { test, expect } = require("@playwright/test");

test("starts a room, exposes a shareable watch URL, and syncs chat", async ({ page, browser, request }) => {
  const streamURL = `https://www.youtube.com/watch?v=i6-j6_5aXL8&live2gether_test=${Date.now()}`;

  await page.goto("/");
  await expect(page.locator("h1")).toHaveText("live2gether");
  await page.locator("#stream-url").fill(streamURL);
  await page.locator("#stream-form button[type='submit']").click();

  await expect(page.locator("#share-row")).toBeVisible();
  await expect(page.locator("#host-row")).toBeVisible();
  await expect(page.locator("#share-url")).toHaveValue(/\/watch\/[A-Za-z0-9_-]+$/);
  await expect(page).toHaveURL(/\/watch\/[A-Za-z0-9_-]+$/);
  await expect(page.locator("#host-label")).toHaveText("host");
  await expect(page.locator("#quality-select")).toBeEnabled();

  await expect
    .poll(async () => page.locator("#status").textContent(), {
      message: "stream becomes ready",
      timeout: 90000
    })
    .toContain("Stream ready");

  const mediaSrc = await page.locator("#media").getAttribute("src");
  expect(mediaSrc).toMatch(/^\/hls\/[A-Za-z0-9_-]+\/index\.m3u8\?v=\d+$/);

  const playlist = await request.get(mediaSrc);
  expect(playlist.ok()).toBeTruthy();
  await expect
    .poll(async () => await playlist.text(), {
      message: "playlist includes at least one segment",
      timeout: 10000
    })
    .toContain("#EXTINF");

  const viewerContext = await browser.newContext();
  const viewer = await viewerContext.newPage();
  await viewer.goto(await page.locator("#share-url").inputValue());
  await expect(viewer.locator("#host-label")).toHaveText("viewer");
  await expect(viewer.locator("#quality-select")).toBeDisabled();

  await expect
    .poll(async () => page.locator("#viewer-count").textContent(), {
      message: "two viewers are present",
      timeout: 15000
    })
    .toBe("2");

  await viewer.locator("#chat-input").fill("hello from the shared room");
  await viewer.locator("#chat-form button").click();
  await expect(page.locator("#chat-log")).toContainText("hello from the shared room");
  await viewerContext.close();
});
