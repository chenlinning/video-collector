import { randomUUID } from "node:crypto";
import { access, mkdir } from "node:fs/promises";
import path from "node:path";
import { createInterface } from "node:readline";
import { spawn, type ChildProcessByStdio } from "node:child_process";
import type { Readable } from "node:stream";
import type {
  DownloadProgress,
  DownloadRequest,
  MediaInfo,
  RuntimeStatus,
  StartDownloadResult
} from "../shared/contracts";
import { buildDownloadArgs } from "./download-command";
import { HistoryStore } from "./history-store";
import { normalizeMediaInfo } from "./media-normalizer";
import { parseYtDlpLine } from "./progress-parser";
import type { RuntimePaths } from "./runtime-paths";
import { assertPublicMediaUrl } from "./url-policy";

const MAX_CAPTURE_BYTES = 16 * 1024 * 1024;

interface ActiveTask {
  child: ChildProcessByStdio<null, Readable, Readable>;
  cancelled: boolean;
  lastPercent: number;
  outputPath?: string;
  errors: string[];
  request: DownloadRequest;
}

interface ProcessResult {
  stdout: string;
  stderr: string;
}

type ProgressListener = (progress: DownloadProgress) => void;

function friendlyProcessError(stderr: string, fallback: string): string {
  const errorLine = stderr
    .split(/\r?\n/)
    .map((line) => line.trim())
    .reverse()
    .find((line) => line.startsWith("ERROR:"));
  return (errorLine?.replace(/^ERROR:\s*/, "") || fallback).slice(0, 500);
}

function collectProcess(
  executable: string,
  args: string[],
  maxBytes = MAX_CAPTURE_BYTES
): Promise<ProcessResult> {
  return new Promise((resolve, reject) => {
    const child = spawn(executable, args, {
      shell: false,
      windowsHide: true,
      stdio: ["ignore", "pipe", "pipe"]
    });
    let stdout = "";
    let stderr = "";

    child.stdout.setEncoding("utf8");
    child.stderr.setEncoding("utf8");
    child.stdout.on("data", (chunk: string) => {
      stdout += chunk;
      if (stdout.length > maxBytes) {
        child.kill();
        reject(new Error("解析结果过大，已停止处理"));
      }
    });
    child.stderr.on("data", (chunk: string) => {
      stderr = `${stderr}${chunk}`.slice(-32_000);
    });
    child.on("error", (error) => reject(error));
    child.on("close", (code) => {
      if (code === 0) {
        resolve({ stdout, stderr });
      } else {
        reject(new Error(friendlyProcessError(stderr, `子进程退出，代码 ${code ?? "未知"}`)));
      }
    });
  });
}

export class MediaEngine {
  private readonly tasks = new Map<string, ActiveTask>();
  private readonly history: HistoryStore;

  constructor(
    private readonly paths: RuntimePaths,
    private readonly notifyProgress: ProgressListener
  ) {
    this.history = new HistoryStore(paths.historyPath);
  }

  async initialize(): Promise<void> {
    await Promise.all([
      access(this.paths.ytDlpPath),
      access(this.paths.ffmpegPath),
      access(this.paths.ffprobePath),
      mkdir(this.paths.cacheDirectory, { recursive: true }),
      mkdir(this.paths.defaultDownloadDirectory, { recursive: true })
    ]);
  }

  async getRuntimeStatus(): Promise<RuntimeStatus> {
    const [ytDlp, ffmpeg] = await Promise.all([
      collectProcess(this.paths.ytDlpPath, ["--version"], 32_000),
      collectProcess(this.paths.ffmpegPath, ["-version"], 32_000)
    ]);

    return {
      ytDlpVersion: ytDlp.stdout.trim().split(/\r?\n/)[0] || "未知",
      ffmpegVersion:
        ffmpeg.stdout.trim().split(/\r?\n/)[0]?.replace(/^ffmpeg version\s+/, "") || "未知",
      defaultDownloadDirectory: this.paths.defaultDownloadDirectory
    };
  }

  async parseUrl(value: string): Promise<MediaInfo> {
    const url = assertPublicMediaUrl(value).toString();
    const result = await collectProcess(this.paths.ytDlpPath, [
      "--no-playlist",
      "--skip-download",
      "--dump-single-json",
      "--no-warnings",
      "--ffmpeg-location",
      this.paths.ffmpegDirectory,
      "--",
      url
    ]);

    let parsed: unknown;
    try {
      parsed = JSON.parse(result.stdout);
    } catch {
      throw new Error("解析器返回了无法识别的数据");
    }

    const media = normalizeMediaInfo(parsed as Record<string, unknown>, url);
    if (media.formats.length === 0) {
      throw new Error("没有找到可下载的公开媒体格式");
    }
    return media;
  }

  async startDownload(request: DownloadRequest): Promise<StartDownloadResult> {
    const sourceUrl = assertPublicMediaUrl(request.sourceUrl).toString();
    if (!path.isAbsolute(request.outputDirectory)) {
      throw new Error("下载目录必须是绝对路径");
    }

    await mkdir(request.outputDirectory, { recursive: true });
    const taskId = randomUUID();
    const child = spawn(
      this.paths.ytDlpPath,
      buildDownloadArgs({
        url: sourceUrl,
        formatId: request.formatId,
        hasAudio: request.hasAudio,
        outputDirectory: request.outputDirectory,
        ffmpegDirectory: this.paths.ffmpegDirectory
      }),
      {
        shell: false,
        windowsHide: true,
        stdio: ["ignore", "pipe", "pipe"] as const
      }
    );

    const task: ActiveTask = {
      child,
      cancelled: false,
      lastPercent: 0,
      errors: [],
      request: { ...request, sourceUrl }
    };
    this.tasks.set(taskId, task);
    this.emit({ taskId, state: "starting", percent: 0 });
    this.attachTask(taskId, task);
    return { taskId };
  }

  cancelDownload(taskId: string): boolean {
    const task = this.tasks.get(taskId);
    if (!task) {
      return false;
    }

    task.cancelled = true;
    task.child.kill();
    return true;
  }

  listHistory() {
    return this.history.list();
  }

  clearHistory() {
    return this.history.clear();
  }

  private attachTask(taskId: string, task: ActiveTask): void {
    task.child.stdout.setEncoding("utf8");
    task.child.stderr.setEncoding("utf8");
    const stdoutLines = createInterface({ input: task.child.stdout });
    const stderrLines = createInterface({ input: task.child.stderr });

    stdoutLines.on("line", (line) => this.handleTaskLine(taskId, task, line));
    stderrLines.on("line", (line) => {
      task.errors.push(line);
      task.errors = task.errors.slice(-20);
      this.handleTaskLine(taskId, task, line);
    });
    task.child.on("error", (error) => {
      this.finishFailed(taskId, task, error.message);
    });
    task.child.on("close", (code) => {
      stdoutLines.close();
      stderrLines.close();
      if (!this.tasks.has(taskId)) {
        return;
      }

      if (task.cancelled) {
        this.emit({
          taskId,
          state: "cancelled",
          percent: task.lastPercent
        });
        this.tasks.delete(taskId);
        return;
      }

      if (code !== 0) {
        this.finishFailed(
          taskId,
          task,
          friendlyProcessError(task.errors.join("\n"), "下载失败，请稍后重试")
        );
        return;
      }

      void this.finishCompleted(taskId, task);
    });
  }

  private handleTaskLine(taskId: string, task: ActiveTask, line: string): void {
    const parsed = parseYtDlpLine(line);
    if (!parsed) {
      return;
    }

    if (parsed.kind === "progress") {
      task.lastPercent = parsed.percent;
      this.emit({
        taskId,
        state: "downloading",
        percent: parsed.percent,
        speed: parsed.speed,
        eta: parsed.eta,
        downloadedBytes: parsed.downloadedBytes,
        totalBytes: parsed.totalBytes
      });
    } else if (parsed.kind === "processing") {
      this.emit({
        taskId,
        state: "processing",
        percent: Math.max(task.lastPercent, 99)
      });
    } else {
      task.outputPath = parsed.outputPath;
    }
  }

  private async finishCompleted(taskId: string, task: ActiveTask): Promise<void> {
    const outputPath = task.outputPath;
    if (!outputPath) {
      this.finishFailed(taskId, task, "下载已结束，但未能确定输出文件位置");
      return;
    }

    await this.history.add({
      id: taskId,
      mediaId: task.request.mediaId,
      title: task.request.title,
      sourceUrl: task.request.sourceUrl,
      outputPath,
      completedAt: new Date().toISOString()
    });
    this.emit({
      taskId,
      state: "completed",
      percent: 100,
      outputPath
    });
    this.tasks.delete(taskId);
  }

  private finishFailed(taskId: string, task: ActiveTask, error: string): void {
    if (!this.tasks.has(taskId)) {
      return;
    }
    this.emit({
      taskId,
      state: "failed",
      percent: task.lastPercent,
      error: error.slice(0, 500)
    });
    this.tasks.delete(taskId);
  }

  private emit(progress: DownloadProgress): void {
    this.notifyProgress(progress);
  }
}
