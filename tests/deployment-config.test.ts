import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const nginx = readFileSync(new URL("../deploy/nginx/video-collector.ximoai.cn.conf", import.meta.url), "utf8");
const compose = readFileSync(new URL("../docker-compose.yml", import.meta.url), "utf8");
const environment = readFileSync(new URL("../.env.example", import.meta.url), "utf8");

describe("production upload proxy", () => {
  it("accepts the bounded transcription upload without buffering it outside project cache", () => {
    expect(nginx).toContain("client_max_body_size 251m;");
    expect(nginx).toContain("proxy_request_buffering off;");
  });
});

describe("domestic egress deployment", () => {
  it("stays disabled by default and passes only task-scoped proxy settings", () => {
    expect(environment).toContain("VIDEO_COLLECTOR_EGRESS_MODE=off");
    for (const name of [
      "VIDEO_COLLECTOR_EGRESS_MODE",
      "VIDEO_COLLECTOR_CN_PROXY_URL",
      "VIDEO_COLLECTOR_CN_PROXY_SOURCE_HOSTS",
      "VIDEO_COLLECTOR_CN_PROXY_ROUTE_TTL_SECONDS",
      "VIDEO_COLLECTOR_CN_PROXY_CONNECT_TIMEOUT_SECONDS",
      "VIDEO_COLLECTOR_CN_PROXY_BREAKER_FAILURES",
      "VIDEO_COLLECTOR_CN_PROXY_BREAKER_SECONDS",
    ]) {
      expect(compose).toContain(`${name}:`);
    }
    expect(compose).not.toMatch(/^\s+HTTPS?_PROXY:/m);
    expect(compose).toContain("- ./cache:/app/cache");
  });
});
