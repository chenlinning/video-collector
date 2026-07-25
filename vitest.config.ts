import { defineConfig } from "vitest/config";

export default defineConfig({
  cacheDir: "cache/vitest",
  test: {
    environment: "node",
    include: ["tests/**/*.test.ts"],
    coverage: {
      reporter: ["text", "json-summary"],
      reportsDirectory: "cache/coverage"
    }
  }
});
