import type { CollectionItem, MediaInfo, MediaMetrics } from "../../shared/contracts";

type ExportableMedia = Pick<MediaInfo, "sourceUrl" | "title" | "uploader" | "extractor" | "duration" | "metrics"> | CollectionItem;

function csvCell(value: string | number | undefined): string {
  const text = value == null ? "" : String(value);
  return /[",\r\n]/.test(text) ? `"${text.replaceAll('"', '""')}"` : text;
}

function metricsOf(item: ExportableMedia): MediaMetrics {
  return item.metrics ?? {};
}

export function buildMediaCsv(items: ExportableMedia[]): string {
  const header = ["Title", "Creator", "Platform", "URL", "DurationSeconds", "Views", "Likes", "Comments", "Reposts"];
  const rows = items.map((item) => {
    const metrics = metricsOf(item);
    const creator = "uploader" in item ? item.uploader : "";
    const platform = "extractor" in item ? item.extractor : "";
    return [item.title, creator, platform, item.sourceUrl, item.duration, metrics.views, metrics.likes, metrics.comments, metrics.reposts]
      .map(csvCell)
      .join(",");
  });
  return `\uFEFF${[header.join(","), ...rows].join("\r\n")}`;
}

export function downloadMediaCsv(items: ExportableMedia[], fileName = "media-results.csv"): void {
  const blob = new Blob([buildMediaCsv(items)], { type: "text/csv;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = fileName;
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}

export function splitBatchUrls(value: string): string[] {
  const urls = value.split(/\r?\n/).map((item) => item.trim()).filter(Boolean);
  if (urls.length > 10) throw new Error("A batch can contain at most 10 URLs");
  return urls;
}
