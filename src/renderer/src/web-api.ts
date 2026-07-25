import type {
  DownloadProgress,
  DownloadRequest,
  MediaInfo,
  RuntimeStatus,
  StartDownloadResult,
  VideoCollectorApi
} from "../../shared/contracts";

const activeStates = new Set(["queued", "downloading", "processing"]);
const httpNoContent = 204;

interface WebTask {
  id: string;
  state: "queued" | "downloading" | "processing" | "completed" | "cancelled" | "failed" | "expired";
  percent: number;
  speed?: string;
  eta?: string;
  downloadedBytes?: number;
  totalBytes?: number;
  fileName?: string;
  fileSize?: number;
  error?: string;
  deleteAt?: string;
}

interface StreamWriter {
  write(chunk: Uint8Array): Promise<unknown>;
  close(): Promise<unknown>;
  abort?(reason?: unknown): Promise<unknown>;
}

interface SaveFilePickerWindow extends Window {
  showSaveFilePicker?: (options: { suggestedName: string }) => Promise<{
    createWritable(): Promise<StreamWriter>;
  }>;
}

async function requestJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    ...init,
    credentials: "same-origin",
    cache: "no-store",
    headers: {
      ...(init?.body ? { "Content-Type": "application/json" } : {}),
      ...init?.headers
    }
  });
  if (!response.ok) {
    const payload = await response.json().catch(() => null) as { message?: string } | null;
    throw new Error(payload?.message || `Request failed with status ${response.status}`);
  }
  if (response.status === httpNoContent) {
    return undefined as T;
  }
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
    deleteAt: task.deleteAt,
    error: task.state === "expired" ? "临时文件已过期" : task.error
  };
}

export function createWebVideoCollectorApi(): VideoCollectorApi {
  const listeners = new Set<(progress: DownloadProgress) => void>();
  let pollTimer: ReturnType<typeof setTimeout> | undefined;

  const emit = (task: WebTask) => {
    const progress = taskProgress(task);
    listeners.forEach((listener) => listener(progress));
  };
  const poll = async (taskId: string) => {
    try {
      const task = await requestJSON<WebTask>(`/api/v1/tasks/${encodeURIComponent(taskId)}`);
      emit(task);
      if (activeStates.has(task.state)) {
        pollTimer = setTimeout(() => void poll(taskId), 1000);
      }
    } catch (error) {
      listeners.forEach((listener) => listener({
        taskId,
        state: "failed",
        percent: 0,
        error: error instanceof Error ? error.message : "下载状态获取失败"
      }));
    }
  };

  return {
    parseUrl(url: string): Promise<MediaInfo> {
      return requestJSON<MediaInfo>("/api/v1/media/parse", {
        method: "POST",
        body: JSON.stringify({ url })
      });
    },
    async chooseDirectory() {
      return "浏览器下载目录";
    },
    async getRuntimeStatus(): Promise<RuntimeStatus> {
      const status = await requestJSON<Omit<RuntimeStatus, "defaultDownloadDirectory">>("/api/v1/status");
      return { ...status, defaultDownloadDirectory: "浏览器下载目录" };
    },
    async startDownload(request: DownloadRequest): Promise<StartDownloadResult> {
      if (pollTimer) clearTimeout(pollTimer);
      const task = await requestJSON<WebTask>("/api/v1/tasks", {
        method: "POST",
        body: JSON.stringify({
          sourceUrl: request.sourceUrl,
          mediaId: request.mediaId,
          title: request.title,
          formatId: request.formatId,
          hasAudio: request.hasAudio
        })
      });
      emit(task);
      pollTimer = setTimeout(() => void poll(task.id), 300);
      return { taskId: task.id };
    },
    async cancelDownload(taskId: string) {
      if (pollTimer) clearTimeout(pollTimer);
      await requestJSON<void>(`/api/v1/tasks/${encodeURIComponent(taskId)}`, { method: "DELETE" });
      listeners.forEach((listener) => listener({
        taskId,
        state: "cancelled",
        percent: 0
      }));
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
  const picker = (window as SaveFilePickerWindow).showSaveFilePicker;
  const downloadPath = `/api/v1/tasks/${encodeURIComponent(taskId)}/download`;
  let writer: StreamWriter | null = null;
  if (picker) {
    const handle = await picker({ suggestedName: fileName || "video.mp4" });
    writer = await handle.createWritable();
  } else {
    const anchor = document.createElement("a");
    anchor.href = downloadPath;
    anchor.download = fileName || "video.mp4";
    document.body.append(anchor);
    anchor.click();
    anchor.remove();
    onProgress?.(100);
    return null;
  }

  const response = await fetch(downloadPath, {
    credentials: "same-origin",
    cache: "no-store"
  });
  if (!response.ok) {
    const payload = await response.json().catch(() => null) as { message?: string } | null;
    await writer?.abort?.(new Error(payload?.message || "Download failed"));
    throw new Error(payload?.message || "Download failed");
  }
  await streamResponseToWriter(response, writer, onProgress);
  return response.headers.get("X-Delete-At");
}
