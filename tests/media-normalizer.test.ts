import { describe, expect, it } from "vitest";
import { normalizeMediaInfo } from "../src/main/media-normalizer";

describe("normalizeMediaInfo", () => {
  it("normalizes and sorts yt-dlp formats for the renderer", () => {
    const result = normalizeMediaInfo(
      {
        id: "7576493197174541588",
        webpage_url: "https://www.tiktok.com/@wowohpanda/video/7576493197174541588",
        title: "第32集 | 梵高的星月夜",
        uploader: "wowohpanda",
        thumbnail: "https://example.com/cover.jpg",
        duration: 209,
        extractor: "TikTok",
        formats: [
          {
            format_id: "audio-low",
            ext: "m4a",
            acodec: "aac",
            vcodec: "none",
            abr: 64,
            filesize_approx: 1_000_000
          },
          {
            format_id: "h264_540p_581929-1",
            ext: "mp4",
            width: 576,
            height: 1280,
            vcodec: "h264",
            acodec: "aac",
            tbr: 581,
            filesize_approx: 15_217_445
          },
          {
            format_id: "video-only-1080",
            ext: "mp4",
            width: 1080,
            height: 1920,
            vcodec: "h264",
            acodec: "none",
            tbr: 1_800
          },
          {
            format_id: "storyboard",
            ext: "mhtml",
            vcodec: "none",
            acodec: "none"
          }
        ]
      },
      "https://www.tiktok.com/@wowohpanda/video/7576493197174541588"
    );

    expect(result.id).toBe("7576493197174541588");
    expect(result.title).toContain("梵高的星月夜");
    expect(result.formats).toHaveLength(3);
    expect(result.formats[0]).toMatchObject({
      id: "video-only-1080",
      width: 1080,
      height: 1920,
      hasVideo: true,
      hasAudio: false
    });
    expect(result.formats[1]).toMatchObject({
      id: "h264_540p_581929-1",
      hasVideo: true,
      hasAudio: true
    });
    expect(result.formats[1].label).toContain("1280p");
    expect(result.formats[2].label).toContain("仅音频");
  });

  it("uses stable fallbacks for incomplete metadata", () => {
    const result = normalizeMediaInfo(
      { id: "abc", formats: [] },
      "https://example.com/watch/abc"
    );

    expect(result).toMatchObject({
      id: "abc",
      title: "未命名视频",
      uploader: "未知作者",
      extractor: "未知平台",
      sourceUrl: "https://example.com/watch/abc"
    });
  });
});
