import { describe, expect, it, vi } from "vitest";
import { loadTheme, saveTheme, THEME_STORAGE_KEY } from "../src/renderer/src/theme";

describe("theme preference", () => {
  it.each([
    ["dark", "dark"],
    ["light", "light"],
    ["system", "dark"],
    [null, "dark"]
  ] as const)("loads %s as %s", (stored, expected) => {
    const storage = { getItem: vi.fn(() => stored) };

    expect(loadTheme(storage)).toBe(expected);
    expect(storage.getItem).toHaveBeenCalledWith(THEME_STORAGE_KEY);
  });

  it("falls back to dark when storage is unavailable", () => {
    const storage = { getItem: vi.fn(() => { throw new Error("blocked"); }) };

    expect(loadTheme(storage)).toBe("dark");
  });

  it("persists the selected theme without surfacing storage errors", () => {
    const storage = { setItem: vi.fn() };
    const blockedStorage = { setItem: vi.fn(() => { throw new Error("blocked"); }) };

    expect(() => saveTheme(storage, "light")).not.toThrow();
    expect(storage.setItem).toHaveBeenCalledWith(THEME_STORAGE_KEY, "light");
    expect(() => saveTheme(blockedStorage, "dark")).not.toThrow();
  });
});
