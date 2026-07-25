export type Theme = "dark" | "light";

export const THEME_STORAGE_KEY = "video-collector-theme";

interface ReadableStorage {
  getItem(key: string): string | null;
}

interface WritableStorage {
  setItem(key: string, value: string): void;
}

export function loadTheme(storage: ReadableStorage): Theme {
  try {
    return storage.getItem(THEME_STORAGE_KEY) === "light" ? "light" : "dark";
  } catch {
    return "dark";
  }
}

export function saveTheme(storage: WritableStorage, theme: Theme): void {
  try {
    storage.setItem(THEME_STORAGE_KEY, theme);
  } catch {
    // The theme still works for this session when persistent storage is unavailable.
  }
}
