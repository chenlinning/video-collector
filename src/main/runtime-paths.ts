import path from "node:path";

export interface RuntimePaths {
  ytDlpPath: string;
  ffmpegPath: string;
  ffprobePath: string;
  ffmpegDirectory: string;
  cacheDirectory: string;
  historyPath: string;
  defaultDownloadDirectory: string;
}

interface ResolveRuntimePathsInput {
  isPackaged: boolean;
  appPath: string;
  executablePath: string;
  resourcesPath: string;
  portableExecutableDirectory?: string;
}

export function resolveRuntimePaths(input: ResolveRuntimePathsInput): RuntimePaths {
  const runtimeRoot = input.isPackaged
    ? input.portableExecutableDirectory || path.dirname(input.executablePath)
    : input.appPath;
  const binaryDirectory = input.isPackaged
    ? path.join(input.resourcesPath, "bin")
    : "D:\\Program Files\\ffmpeg\\bin";

  const cacheDirectory = path.join(runtimeRoot, "cache");
  return {
    ytDlpPath: input.isPackaged
      ? path.join(input.resourcesPath, "bin", "yt-dlp.exe")
      : "D:\\Program Files\\yt-dlp\\yt-dlp.exe",
    ffmpegPath: path.join(binaryDirectory, "ffmpeg.exe"),
    ffprobePath: path.join(binaryDirectory, "ffprobe.exe"),
    ffmpegDirectory: binaryDirectory,
    cacheDirectory,
    historyPath: path.join(cacheDirectory, "history.json"),
    defaultDownloadDirectory: path.join(runtimeRoot, "downloads")
  };
}
