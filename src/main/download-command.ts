import path from "node:path";

interface DownloadCommandInput {
  url: string;
  formatId: string;
  hasAudio: boolean;
  outputDirectory: string;
  ffmpegDirectory: string;
}

export function buildDownloadArgs(input: DownloadCommandInput): string[] {
  const formatExpression = input.hasAudio
    ? input.formatId
    : `${input.formatId}+bestaudio/best`;
  const outputTemplate = path.join(
    input.outputDirectory,
    "%(title).120B [%(id)s].%(ext)s"
  );

  return [
    "--newline",
    "--continue",
    "--no-playlist",
    "--no-warnings",
    "--windows-filenames",
    "--ffmpeg-location",
    input.ffmpegDirectory,
    "-f",
    formatExpression,
    "--merge-output-format",
    "mp4",
    "--progress-template",
    "download:__VC_PROGRESS__:%(progress._percent_str)s|%(progress._speed_str)s|%(progress._eta_str)s|%(progress.downloaded_bytes)s|%(progress.total_bytes_estimate)s",
    "--print",
    "before_dl:__VC_PROCESSING__:正在准备下载",
    "--print",
    "post_process:__VC_PROCESSING__:正在合并和封装",
    "--print",
    "after_move:__VC_DONE__:%(filepath)s",
    "-o",
    outputTemplate,
    "--",
    input.url
  ];
}
