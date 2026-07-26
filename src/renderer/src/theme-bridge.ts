import type { Theme } from "./theme";

interface MessageEventLike {
  data: unknown;
  origin: string;
  source: MessageEventSource | null;
}

export function resolveParentOrigin(referrer: string): string | null {
  if (!referrer) return null;
  try {
    return new URL(referrer).origin;
  } catch {
    return null;
  }
}

export function buildPreferencesReadyMessage() {
  return {
    source: "ximoai-embedded" as const,
    version: 1 as const,
    type: "preferences:ready" as const
  };
}

export function readParentTheme(
  event: MessageEventLike,
  parentWindow: Window,
  parentOrigin: string
): Theme | null {
  if (event.origin !== parentOrigin || event.source !== parentWindow) return null;
  if (!event.data || typeof event.data !== "object") return null;

  const data = event.data as Record<string, unknown>;
  if (data.source !== "ximoai" || data.version !== 1 || data.type !== "preferences:update") {
    return null;
  }
  if (!data.payload || typeof data.payload !== "object") return null;

  const theme = (data.payload as Record<string, unknown>).theme;
  return theme === "light" || theme === "dark" ? theme : null;
}
