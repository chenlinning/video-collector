import { describe, expect, it } from "vitest";
import {
  buildPreferencesReadyMessage,
  readParentPreferences,
  resolveParentOrigin
} from "../src/renderer/src/theme-bridge";

describe("XimoAI preferences bridge", () => {
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

  it("accepts theme and locale updates only from the exact parent window and origin", () => {
    const parentWindow = {} as Window;
    const message = {
      source: "ximoai",
      version: 1,
      type: "preferences:update",
      payload: { theme: "light", locale: "zh-CN" }
    };

    expect(readParentPreferences({
      data: message,
      origin: "https://ximoai.cn",
      source: parentWindow
    }, parentWindow, "https://ximoai.cn")).toEqual({ theme: "light", locale: "zh-CN" });

    expect(readParentPreferences({
      data: message,
      origin: "https://attacker.example",
      source: parentWindow
    }, parentWindow, "https://ximoai.cn")).toBeNull();

    expect(readParentPreferences({
      data: message,
      origin: "https://ximoai.cn",
      source: {} as Window
    }, parentWindow, "https://ximoai.cn")).toBeNull();
  });

  it("rejects unsupported locale values", () => {
    const parentWindow = {} as Window;
    expect(readParentPreferences({
      data: {
        source: "ximoai",
        version: 1,
        type: "preferences:update",
        payload: { theme: "dark", locale: "fr" }
      },
      origin: "https://ximoai.cn",
      source: parentWindow
    }, parentWindow, "https://ximoai.cn")).toBeNull();
  });
});
