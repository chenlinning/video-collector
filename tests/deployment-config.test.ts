import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const nginx = readFileSync(new URL("../deploy/nginx/video-collector.ximoai.cn.conf", import.meta.url), "utf8");

describe("production upload proxy", () => {
  it("accepts the bounded transcription upload without buffering it outside project cache", () => {
    expect(nginx).toContain("client_max_body_size 251m;");
    expect(nginx).toContain("proxy_request_buffering off;");
  });
});
