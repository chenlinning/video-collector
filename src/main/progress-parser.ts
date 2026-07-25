export type ParsedYtDlpLine =
  | {
      kind: "progress";
      percent: number;
      speed?: string;
      eta?: string;
      downloadedBytes?: number;
      totalBytes?: number;
    }
  | { kind: "processing"; message: string }
  | { kind: "done"; outputPath: string };

function optionalText(value: string | undefined): string | undefined {
  const text = value?.trim();
  return text && text !== "NA" && text !== "Unknown" ? text : undefined;
}

function optionalNumber(value: string | undefined): number | undefined {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : undefined;
}

export function parseYtDlpLine(line: string): ParsedYtDlpLine | null {
  const text = line.trim();

  if (text.startsWith("__VC_PROGRESS__:")) {
    const payload = text.slice("__VC_PROGRESS__:".length);
    const [percentText, speedText, etaText, downloadedText, totalText] = payload.split("|");
    const percent = Math.min(
      100,
      Math.max(0, Number.parseFloat(percentText?.replace("%", "").trim() || "0") || 0)
    );

    return {
      kind: "progress",
      percent,
      speed: optionalText(speedText),
      eta: optionalText(etaText),
      downloadedBytes: optionalNumber(downloadedText),
      totalBytes: optionalNumber(totalText)
    };
  }

  if (text.startsWith("__VC_PROCESSING__:")) {
    return {
      kind: "processing",
      message: text.slice("__VC_PROCESSING__:".length).trim()
    };
  }

  if (text.startsWith("__VC_DONE__:")) {
    return {
      kind: "done",
      outputPath: text.slice("__VC_DONE__:".length).trim()
    };
  }

  return null;
}
