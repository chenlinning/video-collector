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

export interface MediaInfo {
  id: string;
  sourceUrl: string;
  title: string;
  uploader: string;
  thumbnail?: string;
  duration?: number;
  extractor: string;
  formats: MediaFormat[];
}

export interface DownloadRequest {
  sourceUrl: string;
  mediaId: string;
  title: string;
  formatId: string;
  hasAudio: boolean;
  outputDirectory: string;
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
