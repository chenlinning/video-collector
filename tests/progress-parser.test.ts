import { describe, expect, it } from "vitest";
import { parseYtDlpLine } from "../src/main/progress-parser";

describe("parseYtDlpLine", () => {
  it("parses structured download progress", () => {
    expect(
      parseYtDlpLine("__VC_PROGRESS__:42.5%|1.20MiB/s|00:10|6451200|15217445")
    ).toEqual({
      kind: "progress",
      percent: 42.5,
      speed: "1.20MiB/s",
      eta: "00:10",
      downloadedBytes: 6_451_200,
      totalBytes: 15_217_445
    });
  });

  it("parses processing and completion lines", () => {
    expect(parseYtDlpLine("__VC_PROCESSING__:合并音视频")).toEqual({
      kind: "processing",
      message: "合并音视频"
    });
    expect(parseYtDlpLine("__VC_DONE__:D:\\Videos\\作品.mp4")).toEqual({
      kind: "done",
      outputPath: "D:\\Videos\\作品.mp4"
    });
  });

  it("ignores unrelated yt-dlp output", () => {
    expect(parseYtDlpLine("[download] Destination: video.mp4")).toBeNull();
  });
});
