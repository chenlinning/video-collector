import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const styles = readFileSync(new URL("../src/renderer/src/styles.css", import.meta.url), "utf8");
const app = readFileSync(new URL("../src/renderer/src/App.tsx", import.meta.url), "utf8");

describe("main-site visual contract", () => {
  it("uses the reference workspace surfaces and mesh background", () => {
    expect(styles).toContain("--background: #2c2c2c;");
    expect(styles).toContain("--card: #2c2c2c;");
    expect(styles).toContain("--border: #3a3a3a;");
    expect(styles).toContain("--background: #faf9f6;");
    expect(styles).toContain("--card: #ffffff;");
    expect(styles).toContain("--border: #e8e5df;");
    expect(styles).toMatch(/\.app-shell\s*\{[^}]*radial-gradient\(\s*at 40% 20%[^}]*radial-gradient\(\s*at 80% 0%[^}]*radial-gradient\(\s*at 0% 50%/s);
  });

  it("uses the main-site card, border, font, and primary button treatments", () => {
    expect(styles).toContain("--primary: #5a8b67;");
    expect(styles).toContain("--primary-hover: #4a7355;");
    expect(styles).toContain("font-family: system-ui, -apple-system, BlinkMacSystemFont");
    expect(styles).toContain("--shadow-card: 0 1px 3px rgba(0, 0, 0, .04);");
    expect(styles).toMatch(/\.hero-card[^}]+border-radius:\s*16px[^}]+box-shadow:\s*var\(--shadow-card\)/);
    expect(styles).toMatch(/\.composer-card,[^{]+\{[^}]+border:\s*1px solid var\(--card-border\)[^}]+border-radius:\s*16px[^}]+box-shadow:\s*var\(--shadow-card\)/s);
    expect(styles).toMatch(/\.primary-button[^}]+border-radius:\s*12px/);
    expect(styles).toMatch(/\.primary-button[^}]+font-weight:\s*500/);
    expect(styles).toMatch(/\.primary-button[^}]+box-shadow:\s*var\(--shadow-sm\)/);
    expect(styles).toMatch(/\.primary-button:active[^}]+scale\(\.98\)/);
  });

  it("does not render standalone language or theme controls", () => {
    expect(app).not.toContain('className="utility-bar"');
    expect(app).not.toContain("copy.toggleTheme");
    expect(app).not.toContain("setLocale(locale ===");
  });
});
