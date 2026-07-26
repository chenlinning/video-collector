import { describe, expect, it } from "vitest";
import { resolveLocale, uiCopy } from "../src/renderer/src/i18n";

describe("renderer localization", () => {
  it("normalizes main-site and browser language values", () => {
    expect(resolveLocale("zh-CN")).toBe("zh-CN");
    expect(resolveLocale("zh-Hans")).toBe("zh-CN");
    expect(resolveLocale("en-US")).toBe("en");
    expect(resolveLocale("fr-FR")).toBe("en");
  });

  it("provides complete Chinese and English retention and action copy", () => {
    expect(uiCopy["zh-CN"].parse).toBe("开始解析");
    expect(uiCopy.en.parse).toBe("Parse video");
    expect(uiCopy["zh-CN"].retentionNotice).toContain("30 分钟");
    expect(uiCopy["zh-CN"].retentionNotice).toContain("15 分钟");
    expect(uiCopy.en.retentionNotice).toContain("30 minutes");
    expect(uiCopy.en.retentionNotice).toContain("15 minutes");
  });
});
