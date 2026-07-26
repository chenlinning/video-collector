import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const styles = readFileSync(new URL("../src/renderer/src/styles.css", import.meta.url), "utf8");

describe("main-site visual contract", () => {
  it("uses the main-site font and primary button tokens", () => {
    expect(styles).toContain("--primary: #5a8b67;");
    expect(styles).toContain("--primary-hover: #4a7355;");
    expect(styles).toContain("font-family: system-ui, -apple-system, BlinkMacSystemFont");
    expect(styles).toMatch(/\.primary-button[^}]+border-radius:\s*12px/);
    expect(styles).toMatch(/\.primary-button[^}]+font-weight:\s*500/);
  });
});
