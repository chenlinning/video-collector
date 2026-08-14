import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const nginx = readFileSync(new URL("../deploy/nginx/video-collector.ximoai.cn.conf", import.meta.url), "utf8");
const mainPathNginx = readFileSync(new URL("../deploy/nginx/ximoai-video-collector-location.conf", import.meta.url), "utf8");
const compose = readFileSync(new URL("../docker-compose.yml", import.meta.url), "utf8");
const environment = readFileSync(new URL("../.env.example", import.meta.url), "utf8");
const deployment = readFileSync(new URL("../DEPLOYMENT.md", import.meta.url), "utf8");

describe("production upload proxy", () => {
  it("accepts the bounded transcription upload without buffering it outside project cache", () => {
    expect(nginx).toContain("client_max_body_size 251m;");
    expect(nginx).toContain("proxy_request_buffering off;");
  });
});

describe("main-site path deployment", () => {
  it("proxies only the dedicated path without defining another host or certificate", () => {
    expect(mainPathNginx).toContain("location = /video-collector");
    expect(mainPathNginx).toContain("return 308 /video-collector/;");
    expect(mainPathNginx).toContain("location ^~ /video-collector/");
    expect(mainPathNginx).toContain("proxy_pass http://127.0.0.1:8787/;");
    expect(mainPathNginx).toContain("proxy_request_buffering off;");
    expect(mainPathNginx).not.toMatch(/\b(server_name|ssl_certificate|listen)\b/);
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

  it("documents the verified AcFun host in the production auto-route example", () => {
    expect(deployment).toContain(
      "VIDEO_COLLECTOR_CN_PROXY_SOURCE_HOSTS=bilibili.com,b23.tv,acfun.cn"
    );
  });
});
