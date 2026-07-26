export interface MediaFormat {
  id: string;
  label: string;
  extension: string;
  width?: number;
  height?: number;
  videoCodec?: string;
  audioCodec?: string;
  approximateBytes?: number;
  bitrateKbps?: number;
  hasVideo: boolean;
  hasAudio: boolean;
}

export interface MediaImage {
  id: string;
  url: string;
  extension?: string;
  width?: number;
  height?: number;
}

export interface SubtitleTrack {
  language: string;
  name?: string;
  extension?: string;
  automatic: boolean;
}

export interface MediaMetrics {
  views?: number;
  likes?: number;
  comments?: number;
  reposts?: number;
}

export interface MediaInfo {
  id: string;
  sourceUrl: string;
  title: string;
  uploader: string;
  thumbnail?: string;
  duration?: number;
  extractor: string;
  formats: MediaFormat[];
  images?: MediaImage[];
  subtitles?: SubtitleTrack[];
  metrics?: MediaMetrics;
}

export interface CollectionItem {
  id: string;
  sourceUrl: string;
  title: string;
  thumbnail?: string;
  duration?: number;
  metrics?: MediaMetrics;
}

export interface CollectionInfo {
  id: string;
  sourceUrl: string;
  title: string;
  uploader?: string;
  items: CollectionItem[];
}

export interface BatchParseItem {
  url: string;
  media?: MediaInfo;
  error?: string;
}

export type TaskKind = "media" | "audio" | "image" | "subtitle" | "transcript";

export interface WebTaskRequest {
  sourceUrl: string;
  mediaId?: string;
  title: string;
  formatId?: string;
  hasAudio?: boolean;
  kind: TaskKind;
  resourceId?: string;
  automatic?: boolean;
}

export interface WebTask {
  id: string;
  kind: TaskKind;
  state: "queued" | "downloading" | "processing" | "completed" | "cancelled" | "failed" | "expired";
  percent: number;
  speed?: string;
  eta?: string;
  downloadedBytes?: number;
  totalBytes?: number;
  fileName?: string;
  fileSize?: number;
  textPreview?: string;
  error?: string;
  createdAt: string;
  completedAt?: string;
  deleteAt?: string;
}

export interface DownloadRequest {
  sourceUrl: string;
  mediaId: string;
  title: string;
  formatId: string;
  hasAudio: boolean;
  outputDirectory: string;
  kind?: TaskKind;
  resourceId?: string;
  automatic?: boolean;
}

export type DownloadState =
  | "starting"
  | "downloading"
  | "processing"
  | "completed"
  | "cancelled"
  | "failed";

export interface DownloadProgress {
  taskId: string;
  state: DownloadState;
  percent: number;
  speed?: string;
  eta?: string;
  downloadedBytes?: number;
  totalBytes?: number;
  outputPath?: string;
  fileName?: string;
  deleteAt?: string;
  kind?: TaskKind;
  fileSize?: number;
  textPreview?: string;
  createdAt?: string;
  error?: string;
}

export interface DownloadHistoryItem {
  id: string;
  mediaId: string;
  title: string;
  sourceUrl: string;
  outputPath: string;
  completedAt: string;
}

export interface RuntimeStatus {
  ytDlpVersion: string;
  ffmpegVersion: string;
  defaultDownloadDirectory: string;
  whisperVersion?: string;
  whisperModel?: string;
}

export interface StartDownloadResult {
  taskId: string;
}

export interface VideoCollectorApi {
  parseUrl(url: string): Promise<MediaInfo>;
  chooseDirectory(): Promise<string | null>;
  getRuntimeStatus(): Promise<RuntimeStatus>;
  startDownload(request: DownloadRequest): Promise<StartDownloadResult>;
  cancelDownload(taskId: string): Promise<boolean>;
  listHistory(): Promise<DownloadHistoryItem[]>;
  clearHistory(): Promise<void>;
  openPath(targetPath: string): Promise<boolean>;
  showInFolder(targetPath: string): Promise<boolean>;
  onDownloadProgress(listener: (progress: DownloadProgress) => void): () => void;
}
