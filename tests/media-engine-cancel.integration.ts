import { mkdir } from "node:fs/promises";
import path from "node:path";
import { describe, expect, it } from "vitest";
import type { DownloadProgress } from "../src/shared/contracts";
import { MediaEngine } from "../src/main/media-engine";

const sourceUrl =
  "https://www.tiktok.com/@wowohpanda/video/7576493197174541588?is_from_webapp=1";

function waitForCancelled(events: DownloadProgress[]): Promise<DownloadProgress> {
  return new Promise((resolve, reject) => {
    const deadline = Date.now() + 15_000;
    const timer = setInterval(() => {
      const cancelled = events.find((event) => event.state === "cancelled");
      if (cancelled) {
        clearInterval(timer);
        resolve(cancelled);
      } else if (Date.now() >= deadline) {
        clearInterval(timer);
        reject(new Error("Timed out waiting for cancelled state"));
      }
    }, 50);
  });
}

describe("MediaEngine cancellation", () => {
  it("cancels an active real yt-dlp task", async () => {
    const cacheDirectory = path.resolve("cache/integration-cancel");
    const outputDirectory = path.join(cacheDirectory, "downloads");
    await mkdir(outputDirectory, { recursive: true });
    const events: DownloadProgress[] = [];
    const engine = new MediaEngine(
      {
        ytDlpPath: "D:\\Program Files\\yt-dlp\\yt-dlp.exe",
        ffmpegPath: "D:\\Program Files\\ffmpeg\\bin\\ffmpeg.exe",
        ffprobePath: "D:\\Program Files\\ffmpeg\\bin\\ffprobe.exe",
        ffmpegDirectory: "D:\\Program Files\\ffmpeg\\bin",
        cacheDirectory,
        historyPath: path.join(cacheDirectory, "history.json"),
        defaultDownloadDirectory: outputDirectory
      },
      (progress) => events.push(progress)
    );
    await engine.initialize();

    const { taskId } = await engine.startDownload({
      sourceUrl,
      mediaId: "7576493197174541588",
      title: "取消测试",
      formatId: "h264_540p_581929-0",
      hasAudio: true,
      outputDirectory
    });

    expect(engine.cancelDownload(taskId)).toBe(true);
    await expect(waitForCancelled(events)).resolves.toMatchObject({
      taskId,
      state: "cancelled"
    });
    expect(engine.cancelDownload(taskId)).toBe(false);
  });
});
