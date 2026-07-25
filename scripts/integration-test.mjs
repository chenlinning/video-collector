import { createHash } from "node:crypto";
import { createReadStream, existsSync } from "node:fs";
import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { spawnSync } from "node:child_process";

const projectRoot = path.resolve(import.meta.dirname, "..");
const outputDirectory = path.join(projectRoot, "cache", "integration");
const ytDlp = "D:\\Program Files\\yt-dlp\\yt-dlp.exe";
const ffmpeg = "D:\\Program Files\\ffmpeg\\bin\\ffmpeg.exe";
const ffprobe = "D:\\Program Files\\ffmpeg\\bin\\ffprobe.exe";
const ffmpegDirectory = path.dirname(ffmpeg);
const sourceUrl =
  process.env.VIDEO_COLLECTOR_TEST_URL ||
  "https://www.tiktok.com/@wowohpanda/video/7576493197174541588?is_from_webapp=1&sender_device=pc&web_id=7666377691603568159";

function run(executable, args, options = {}) {
  const result = spawnSync(executable, args, {
    encoding: "utf8",
    windowsHide: true,
    maxBuffer: 32 * 1024 * 1024,
    ...options
  });
  if (result.status !== 0) {
    throw new Error(result.stderr || result.stdout || `${executable} failed`);
  }
  return result.stdout;
}

async function sha256(filePath) {
  const hash = createHash("sha256");
  for await (const chunk of createReadStream(filePath)) hash.update(chunk);
  return hash.digest("hex");
}

await mkdir(outputDirectory, { recursive: true });

const metadata = JSON.parse(
  run(ytDlp, [
    "--no-playlist",
    "--skip-download",
    "--dump-single-json",
    "--no-warnings",
    "--ffmpeg-location",
    ffmpegDirectory,
    "--",
    sourceUrl
  ])
);

if (metadata.id !== "7576493197174541588") {
  throw new Error(`Unexpected media id: ${metadata.id}`);
}

const formats = Array.isArray(metadata.formats) ? metadata.formats : [];
const selected = formats
  .filter((format) => format.vcodec && format.vcodec !== "none" && format.acodec && format.acodec !== "none")
  .sort((left, right) => (right.height || 0) - (left.height || 0))[0];
if (!selected) throw new Error("No combined video format found");

const outputTemplate = path.join(outputDirectory, "%(id)s.%(ext)s");
run(ytDlp, [
  "--no-playlist",
  "--continue",
  "--no-overwrites",
  "--no-warnings",
  "--ffmpeg-location",
  ffmpegDirectory,
  "-f",
  selected.format_id,
  "-o",
  outputTemplate,
  "--",
  sourceUrl
]);

const mediaPath = path.join(outputDirectory, `${metadata.id}.${selected.ext || "mp4"}`);
if (!existsSync(mediaPath)) throw new Error(`Downloaded media is missing: ${mediaPath}`);

const probe = JSON.parse(
  run(ffprobe, [
    "-v",
    "error",
    "-show_entries",
    "format=format_name,duration,size,bit_rate",
    "-show_entries",
    "stream=index,codec_type,codec_name,width,height,sample_rate,channels,duration",
    "-of",
    "json",
    "--",
    mediaPath
  ])
);

const streamTypes = new Set(probe.streams.map((stream) => stream.codec_type));
if (!streamTypes.has("video") || !streamTypes.has("audio")) {
  throw new Error("Downloaded file does not contain both audio and video streams");
}

run(ffmpeg, ["-v", "error", "-xerror", "-i", mediaPath, "-map", "0:v:0", "-map", "0:a:0?", "-f", "null", "NUL"]);

const report = {
  passed: true,
  testedAt: new Date().toISOString(),
  sourceUrl,
  id: metadata.id,
  title: metadata.title,
  uploader: metadata.uploader,
  extractor: metadata.extractor,
  selectedFormat: selected.format_id,
  formatCount: formats.length,
  mediaPath,
  sha256: await sha256(mediaPath),
  probe
};

const reportPath = path.join(outputDirectory, "report.json");
await writeFile(reportPath, `${JSON.stringify(report, null, 2)}\n`, "utf8");
process.stdout.write(`${JSON.stringify({ ...report, probe: undefined }, null, 2)}\n`);
