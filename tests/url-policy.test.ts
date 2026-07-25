import { describe, expect, it } from "vitest";
import { assertPublicMediaUrl } from "../src/main/url-policy";

describe("assertPublicMediaUrl", () => {
  it("accepts a public TikTok HTTPS URL", () => {
    const value = assertPublicMediaUrl(
      "https://www.tiktok.com/@wowohpanda/video/7576493197174541588?is_from_webapp=1"
    );

    expect(value.protocol).toBe("https:");
    expect(value.hostname).toBe("www.tiktok.com");
  });

  it.each([
    "file:///C:/Windows/System32/drivers/etc/hosts",
    "ftp://example.com/video.mp4",
    "http://localhost:8080/video",
    "http://127.0.0.1/video",
    "http://10.1.2.3/video",
    "http://172.16.4.2/video",
    "http://172.31.255.2/video",
    "http://192.168.1.9/video",
    "http://169.254.10.2/video",
    "http://[::1]/video",
    "http://[fc00::1]/video",
    "http://[fe80::1]/video"
  ])("rejects unsafe URL %s", (value) => {
    expect(() => assertPublicMediaUrl(value)).toThrow();
  });

  it("rejects malformed input", () => {
    expect(() => assertPublicMediaUrl("not a URL")).toThrow("请输入有效的视频链接");
  });
});
