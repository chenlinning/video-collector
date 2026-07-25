import type { MediaFormat, MediaInfo } from "../shared/contracts";

interface RawFormat {
  format_id?: unknown;
  ext?: unknown;
  width?: unknown;
  height?: unknown;
  vcodec?: unknown;
  acodec?: unknown;
  filesize?: unknown;
  filesize_approx?: unknown;
  tbr?: unknown;
  abr?: unknown;
}

interface RawMediaInfo {
  id?: unknown;
  webpage_url?: unknown;
  original_url?: unknown;
  title?: unknown;
  uploader?: unknown;
  uploader_id?: unknown;
  thumbnail?: unknown;
  duration?: unknown;
  extractor?: unknown;
  extractor_key?: unknown;
  formats?: unknown;
}

function asString(value: unknown): string | undefined {
  return typeof value === "string" && value.trim() ? value.trim() : undefined;
}

function asNumber(value: unknown): number | undefined {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

function hasCodec(value: string | undefined): boolean {
  return Boolean(value && value.toLowerCase() !== "none");
}

function buildFormatLabel(format: MediaFormat): string {
  const codecParts = [format.videoCodec, format.audioCodec].filter(Boolean).join(" + ");
  const container = format.extension.toUpperCase();

  if (!format.hasVideo) {
    return ["仅音频", container, codecParts].filter(Boolean).join(" · ");
  }

  const dimensions =
    format.width && format.height ? `${format.width}×${format.height}` : undefined;
  const resolution = format.height ? `${format.height}p` : "视频";
  const audioStatus = format.hasAudio ? undefined : "需合并音频";
  return [dimensions, resolution, container, codecParts, audioStatus]
    .filter(Boolean)
    .join(" · ");
}

function normalizeFormat(raw: RawFormat): MediaFormat | null {
  const videoCodec = asString(raw.vcodec);
  const audioCodec = asString(raw.acodec);
  const hasVideo = hasCodec(videoCodec);
  const hasAudio = hasCodec(audioCodec);

  if (!hasVideo && !hasAudio) {
    return null;
  }

  const format: MediaFormat = {
    id: asString(raw.format_id) ?? "unknown",
    label: "",
    extension: asString(raw.ext) ?? (hasVideo ? "mp4" : "m4a"),
    width: asNumber(raw.width),
    height: asNumber(raw.height),
    videoCodec: hasVideo ? videoCodec : undefined,
    audioCodec: hasAudio ? audioCodec : undefined,
    approximateBytes: asNumber(raw.filesize) ?? asNumber(raw.filesize_approx),
    bitrateKbps: asNumber(raw.tbr) ?? asNumber(raw.abr),
    hasVideo,
    hasAudio
  };

  format.label = buildFormatLabel(format);
  return format;
}

function compareFormats(left: MediaFormat, right: MediaFormat): number {
  if (left.hasVideo !== right.hasVideo) {
    return left.hasVideo ? -1 : 1;
  }

  const heightDifference = (right.height ?? 0) - (left.height ?? 0);
  if (heightDifference !== 0) {
    return heightDifference;
  }

  if (left.hasAudio !== right.hasAudio) {
    return left.hasAudio ? -1 : 1;
  }

  return (right.bitrateKbps ?? 0) - (left.bitrateKbps ?? 0);
}

export function normalizeMediaInfo(raw: RawMediaInfo, sourceUrl: string): MediaInfo {
  const rawFormats = Array.isArray(raw.formats) ? (raw.formats as RawFormat[]) : [];
  const formats = rawFormats
    .map(normalizeFormat)
    .filter((format): format is MediaFormat => format !== null)
    .sort(compareFormats);

  return {
    id: asString(raw.id) ?? "unknown",
    sourceUrl:
      asString(raw.webpage_url) ?? asString(raw.original_url) ?? sourceUrl,
    title: asString(raw.title) ?? "未命名视频",
    uploader: asString(raw.uploader) ?? asString(raw.uploader_id) ?? "未知作者",
    thumbnail: asString(raw.thumbnail),
    duration: asNumber(raw.duration),
    extractor: asString(raw.extractor) ?? asString(raw.extractor_key) ?? "未知平台",
    formats
  };
}
