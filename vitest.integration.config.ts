import { defineConfig } from "vitest/config";

export default defineConfig({
  cacheDir: "cache/vitest-integration",
  test: {
    environment: "node",
    include: ["tests/**/*.integration.ts"],
    testTimeout: 30_000,
    hookTimeout: 30_000
  }
});
