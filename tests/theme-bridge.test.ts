import { describe, expect, it } from "vitest";
import {
  buildPreferencesReadyMessage,
  readParentTheme,
  resolveParentOrigin
} from "../src/renderer/src/theme-bridge";

describe("XimoAI theme bridge", () => {
  it("resolves the exact parent origin from the iframe referrer", () => {
    expect(resolveParentOrigin("https://ximoai.cn/home?tab=video")).toBe("https://ximoai.cn");
    expect(resolveParentOrigin("not a url")).toBeNull();
    expect(resolveParentOrigin("")).toBeNull();
  });

  it("builds the ready message expected by the main site", () => {
    expect(buildPreferencesReadyMessage()).toEqual({
      source: "ximoai-embedded",
      version: 1,
      type: "preferences:ready"
    });
  });

  it("accepts theme updates only from the exact parent window and origin", () => {
    const parentWindow = {} as Window;
    const message = {
      source: "ximoai",
      version: 1,
      type: "preferences:update",
      payload: { theme: "light", locale: "zh-CN" }
    };

    expect(readParentTheme({
      data: message,
      origin: "https://ximoai.cn",
      source: parentWindow
    }, parentWindow, "https://ximoai.cn")).toBe("light");

    expect(readParentTheme({
      data: message,
      origin: "https://attacker.example",
      source: parentWindow
    }, parentWindow, "https://ximoai.cn")).toBeNull();

    expect(readParentTheme({
      data: message,
      origin: "https://ximoai.cn",
      source: {} as Window
    }, parentWindow, "https://ximoai.cn")).toBeNull();
  });
});
