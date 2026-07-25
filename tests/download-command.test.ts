import { describe, expect, it } from "vitest";
import { buildDownloadArgs } from "../src/main/download-command";

describe("buildDownloadArgs", () => {
  it("builds a combined-format download without a shell", () => {
    const args = buildDownloadArgs({
      url: "https://www.tiktok.com/@wowohpanda/video/7576493197174541588",
      formatId: "h264_540p_581929-1",
      hasAudio: true,
      outputDirectory: "D:\\Videos",
      ffmpegDirectory: "D:\\Program Files\\ffmpeg\\bin"
    });

    expect(args).toContain("h264_540p_581929-1");
    expect(args).toContain("--continue");
    expect(args).toContain("D:\\Program Files\\ffmpeg\\bin");
    expect(args.at(-1)).toBe(
      "https://www.tiktok.com/@wowohpanda/video/7576493197174541588"
    );
    expect(args.join(" ")).toContain("__VC_PROGRESS__");
    expect(args.join(" ")).toContain("__VC_DONE__");
  });

  it("adds best audio when the selected format is video-only", () => {
    const args = buildDownloadArgs({
      url: "https://example.com/video",
      formatId: "video-only-1080",
      hasAudio: false,
      outputDirectory: "D:\\Videos",
      ffmpegDirectory: "D:\\Program Files\\ffmpeg\\bin"
    });

    const formatIndex = args.indexOf("-f");
    expect(args[formatIndex + 1]).toBe("video-only-1080+bestaudio/best");
  });
});
