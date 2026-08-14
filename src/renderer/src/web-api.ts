import type {
  BatchParseItem,
  CollectionInfo,
  DownloadProgress,
  DownloadRequest,
  ExtractedTextDocument,
  MediaInfo,
  RuntimeStatus,
  StartDownloadResult,
  VideoCollectorApi,
  WebTask,
  WebTaskRequest
} from "../../shared/contracts";

const activeStates = new Set(["queued", "downloading", "processing"]);
const httpNoContent = 204;
const apiRoot = "api/v1";

export interface WebVideoCollectorApi extends VideoCollectorApi {
  parseBatch(urls: string[]): Promise<BatchParseItem[]>;
  parseCollection(url: string): Promise<CollectionInfo>;
  startTask(request: WebTaskRequest): Promise<WebTask>;
  uploadTranscription(file: File): Promise<WebTask>;
  extractTextDocument(file: File): Promise<ExtractedTextDocument>;
  getTask(taskId: string): Promise<WebTask>;
  refreshTask(taskId: string): Promise<WebTask>;
}

interface StreamWriter {
  write(chunk: Uint8Array): Promise<unknown>;
  close(): Promise<unknown>;
  abort?(reason?: unknown): Promise<unknown>;
}

async function requestJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    ...init,
    credentials: "same-origin",
    cache: "no-store",
    headers: {
      ...(typeof init?.body === "string" ? { "Content-Type": "application/json" } : {}),
      ...init?.headers
    }
  });
  if (!response.ok) {
    const payload = await response.json().catch(() => null) as { message?: string } | null;
    throw new Error(payload?.message || `Request failed with status ${response.status}`);
  }
  if (response.status === httpNoContent) return undefined as T;
  return response.json() as Promise<T>;
}

function taskProgress(task: WebTask): DownloadProgress {
  const state = task.state === "queued" ? "starting" : task.state === "expired" ? "failed" : task.state;
  return {
    taskId: task.id,
    state,
    percent: task.percent,
    speed: task.speed,
    eta: task.eta,
    downloadedBytes: task.downloadedBytes,
    totalBytes: task.totalBytes,
    outputPath: task.state === "completed" ? task.id : undefined,
    fileName: task.fileName,
    fileSize: task.fileSize,
    kind: task.kind,
    textPreview: task.textPreview,
    createdAt: task.createdAt,
    deleteAt: task.deleteAt,
    error: task.state === "expired" ? "临时文件已过期" : task.error
  };
}

export function createWebVideoCollectorApi(): WebVideoCollectorApi {
  const listeners = new Set<(progress: DownloadProgress) => void>();
  const pollTimers = new Map<string, ReturnType<typeof setTimeout>>();

  const emit = (task: WebTask) => {
    const progress = taskProgress(task);
    listeners.forEach((listener) => listener(progress));
  };

  const poll = async (taskId: string) => {
    try {
      const task = await requestJSON<WebTask>(`${apiRoot}/tasks/${encodeURIComponent(taskId)}`);
      emit(task);
      if (activeStates.has(task.state)) {
        pollTimers.set(taskId, setTimeout(() => void poll(taskId), 1000));
      } else {
        pollTimers.delete(taskId);
      }
    } catch (error) {
      listeners.forEach((listener) => listener({
        taskId,
        state: "failed",
        percent: 0,
        error: error instanceof Error ? error.message : "获取任务状态失败"
      }));
      pollTimers.delete(taskId);
    }
  };

  const startPolling = (task: WebTask) => {
    emit(task);
    const existing = pollTimers.get(task.id);
    if (existing) clearTimeout(existing);
    if (activeStates.has(task.state)) {
      pollTimers.set(task.id, setTimeout(() => void poll(task.id), 300));
    }
  };

  return {
    parseUrl(url: string): Promise<MediaInfo> {
      return requestJSON<MediaInfo>(`${apiRoot}/media/parse`, {
        method: "POST",
        body: JSON.stringify({ url })
      });
    },
    async parseBatch(urls: string[]) {
      const result = await requestJSON<{ items: BatchParseItem[] }>(`${apiRoot}/media/batch`, {
        method: "POST",
        body: JSON.stringify({ urls })
      });
      return result.items;
    },
    parseCollection(url: string) {
      return requestJSON<CollectionInfo>(`${apiRoot}/collections/parse`, {
        method: "POST",
        body: JSON.stringify({ url })
      });
    },
    async chooseDirectory() {
      return "浏览器下载目录";
    },
    async getRuntimeStatus(): Promise<RuntimeStatus> {
      const status = await requestJSON<Omit<RuntimeStatus, "defaultDownloadDirectory">>(`${apiRoot}/status`);
      return { ...status, defaultDownloadDirectory: "浏览器下载目录" };
    },
    async startDownload(request: DownloadRequest): Promise<StartDownloadResult> {
      const task = await requestJSON<WebTask>(`${apiRoot}/tasks`, {
        method: "POST",
        body: JSON.stringify({
          sourceUrl: request.sourceUrl,
          mediaId: request.mediaId,
          title: request.title,
          formatId: request.formatId,
          hasAudio: request.hasAudio,
          kind: request.kind ?? "media",
          resourceId: request.resourceId,
          automatic: request.automatic
        })
      });
      startPolling(task);
      return { taskId: task.id };
    },
    async startTask(request: WebTaskRequest) {
      const task = await requestJSON<WebTask>(`${apiRoot}/tasks`, {
        method: "POST",
        body: JSON.stringify(request)
      });
      startPolling(task);
      return task;
    },
    async uploadTranscription(file: File) {
      const form = new FormData();
      form.append("file", file);
      const task = await requestJSON<WebTask>(`${apiRoot}/transcriptions/upload`, {
        method: "POST",
        body: form
      });
      startPolling(task);
      return task;
    },
    async extractTextDocument(file: File) {
      const form = new FormData();
      form.append("file", file);
      return requestJSON<ExtractedTextDocument>(`${apiRoot}/text/extract`, {
        method: "POST",
        body: form
      });
    },
    getTask(taskId: string) {
      return requestJSON<WebTask>(`${apiRoot}/tasks/${encodeURIComponent(taskId)}`);
    },
    async refreshTask(taskId: string) {
      const task = await requestJSON<WebTask>(`${apiRoot}/tasks/${encodeURIComponent(taskId)}`);
      emit(task);
      return task;
    },
    async cancelDownload(taskId: string) {
      const timer = pollTimers.get(taskId);
      if (timer) clearTimeout(timer);
      pollTimers.delete(taskId);
      await requestJSON<void>(`${apiRoot}/tasks/${encodeURIComponent(taskId)}`, { method: "DELETE" });
      listeners.forEach((listener) => listener({ taskId, state: "cancelled", percent: 0 }));
      return true;
    },
    async listHistory() {
      return [];
    },
    async clearHistory() {},
    async openPath() {
      return false;
    },
    async showInFolder() {
      return false;
    },
    onDownloadProgress(listener) {
      listeners.add(listener);
      return () => listeners.delete(listener);
    }
  };
}

export async function streamResponseToWriter(
  response: Response,
  writer: StreamWriter,
  onProgress?: (percent: number) => void
): Promise<void> {
  if (!response.body) {
    await writer.abort?.(new Error("Download stream is unavailable"));
    throw new Error("Download stream is unavailable");
  }
  const total = Number(response.headers.get("Content-Length")) || 0;
  const reader = response.body.getReader();
  let received = 0;
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      await writer.write(value);
      received += value.byteLength;
      if (total > 0) onProgress?.(Math.min(100, Math.round((received / total) * 100)));
    }
    await writer.close();
    onProgress?.(100);
  } catch (error) {
    await writer.abort?.(error);
    throw error;
  }
}

export async function saveWebDownload(
  taskId: string,
  fileName: string,
  onProgress?: (percent: number) => void
): Promise<string | null> {
  const anchor = document.createElement("a");
  anchor.href = `${apiRoot}/tasks/${encodeURIComponent(taskId)}/download`;
  anchor.download = fileName || "media.bin";
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  onProgress?.(100);
  return null;
}
