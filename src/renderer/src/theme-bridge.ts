import type { Theme } from "./theme";
import type { Locale } from "./i18n";

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

export interface EmbeddedPreferences {
  theme: Theme;
  locale: Locale;
}

export function readParentPreferences(
  event: MessageEventLike,
  parentWindow: Window,
  parentOrigin: string
): EmbeddedPreferences | null {
  if (event.origin !== parentOrigin || event.source !== parentWindow) return null;
  if (!event.data || typeof event.data !== "object") return null;

  const data = event.data as Record<string, unknown>;
  if (data.source !== "ximoai" || data.version !== 1 || data.type !== "preferences:update") {
    return null;
  }
  if (!data.payload || typeof data.payload !== "object") return null;

  const payload = data.payload as Record<string, unknown>;
  const theme = payload.theme;
  const locale = payload.locale;
  if (theme !== "light" && theme !== "dark") return null;
  if (locale !== "zh-CN" && locale !== "en") return null;
  return { theme, locale };
}
