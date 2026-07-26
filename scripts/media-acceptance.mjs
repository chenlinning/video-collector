import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";

const baseUrl = process.env.VIDEO_COLLECTOR_ACCEPTANCE_URL || "http://127.0.0.1:8788";
const outputDirectory = path.resolve("cache", "acceptance");
const samples = {
  video: "https://www.acfun.cn/v/ac48722683",
  audio: "https://soundcloud.com/nasa/apollo-13-houston-weve-had-a",
  subtitles: "https://www.ted.com/talks/ted_ed_would_you_pass_the_wallet_test",
  collection: "https://soundcloud.com/nasa/sets/apollo-sounds"
};

async function requestJson(route, init) {
  const response = await fetch(`${baseUrl}${route}`, init);
  const payload = await response.json().catch(() => null);
  if (!response.ok) throw new Error(payload?.message || `${route} returned ${response.status}`);
  return payload;
}

async function postJson(route, value) {
  return requestJson(route, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(value)
  });
}

async function waitForTask(taskId, timeoutMinutes = 12) {
  const deadline = Date.now() + timeoutMinutes * 60_000;
  while (Date.now() < deadline) {
    const task = await requestJson(`/api/v1/tasks/${encodeURIComponent(taskId)}`);
    if (task.state === "completed") return task;
    if (["failed", "cancelled", "expired"].includes(task.state)) {
      throw new Error(`${task.kind} task ${task.id} ended as ${task.state}: ${task.error || "unknown error"}`);
    }
    await new Promise((resolve) => setTimeout(resolve, 1000));
  }
  throw new Error(`task ${taskId} timed out`);
}

async function startTask(value) {
  const task = await postJson("/api/v1/tasks", value);
  return waitForTask(task.id);
}

async function downloadTask(task, fallbackName) {
  const response = await fetch(`${baseUrl}/api/v1/tasks/${encodeURIComponent(task.id)}/download`);
  if (!response.ok) throw new Error(`download ${task.id} returned ${response.status}`);
  const deleteAt = response.headers.get("x-delete-at");
  const remaining = deleteAt ? Date.parse(deleteAt) - Date.now() : 0;
  if (remaining < 14 * 60_000 || remaining > 16 * 60_000) {
    throw new Error(`download ${task.id} did not receive a 15-minute retention lease`);
  }
  const bytes = Buffer.from(await response.arrayBuffer());
  if (bytes.length === 0) throw new Error(`download ${task.id} returned an empty file`);
  const fileName = task.fileName || fallbackName;
  const filePath = path.join(outputDirectory, fileName);
  await writeFile(filePath, bytes);
  return { fileName, filePath, bytes: bytes.length, deleteAt };
}

await mkdir(outputDirectory, { recursive: true });
const health = await requestJson("/health");
for (const required of ["ytDlpVersion", "ffmpegVersion", "whisperVersion", "whisperModel"]) {
  if (!health[required]) throw new Error(`health response is missing ${required}`);
}

const batch = await postJson("/api/v1/media/batch", {
  urls: [samples.video, samples.audio, samples.subtitles, "https://example.com/not-media"]
});
const successful = batch.items.filter((item) => item.media);
const failed = batch.items.filter((item) => item.error);
if (successful.length !== 3 || failed.length !== 1) throw new Error("mixed real batch did not preserve per-item success and failure");

const videoMedia = successful.find((item) => item.url === samples.video).media;
const audioMedia = successful.find((item) => item.url === samples.audio).media;
const subtitleMedia = successful.find((item) => item.url === samples.subtitles).media;
if (!videoMedia.formats.length) throw new Error("video sample returned no formats");
if (!audioMedia.images?.length) throw new Error("audio sample returned no image candidates");
if (!subtitleMedia.subtitles?.length) throw new Error("subtitle sample returned no subtitle tracks");

const collection = await postJson("/api/v1/collections/parse", { url: samples.collection });
if (!collection.items?.length || collection.items.length > 10) throw new Error("public collection did not return between 1 and 10 items");

const format = videoMedia.formats[0];
const videoTask = await startTask({
  kind: "media", sourceUrl: videoMedia.sourceUrl, mediaId: videoMedia.id, title: videoMedia.title,
  formatId: format.id, hasAudio: format.hasAudio
});
const videoDownload = await downloadTask(videoTask, "video.mp4");

const audioTask = await startTask({ kind: "audio", sourceUrl: audioMedia.sourceUrl, mediaId: audioMedia.id, title: audioMedia.title });
const audioDownload = await downloadTask(audioTask, "audio.mp3");

const image = audioMedia.images[0];
const imageTask = await startTask({ kind: "image", sourceUrl: audioMedia.sourceUrl, mediaId: audioMedia.id, title: audioMedia.title, resourceId: image.id });
const imageDownload = await downloadTask(imageTask, "image.jpg");

const subtitle = subtitleMedia.subtitles.find((item) => item.language.startsWith("en") && !item.automatic) || subtitleMedia.subtitles[0];
const subtitleTask = await startTask({
  kind: "subtitle", sourceUrl: subtitleMedia.sourceUrl, mediaId: subtitleMedia.id, title: subtitleMedia.title,
  resourceId: subtitle.language, automatic: subtitle.automatic
});
const subtitleDownload = await downloadTask(subtitleTask, "subtitle.srt");

const urlTranscriptTask = await startTask({ kind: "transcript", sourceUrl: audioMedia.sourceUrl, mediaId: audioMedia.id, title: audioMedia.title });
if (!urlTranscriptTask.textPreview) throw new Error("URL transcription returned no text preview");
const urlTranscriptDownload = await downloadTask(urlTranscriptTask, "url-transcript.srt");

const audioBytes = await readFile(audioDownload.filePath);
const form = new FormData();
form.append("file", new Blob([audioBytes], { type: "audio/mpeg" }), "nasa-apollo-13.mp3");
const uploadTask = await requestJson("/api/v1/transcriptions/upload", { method: "POST", body: form });
const completedUploadTask = await waitForTask(uploadTask.id);
if (!completedUploadTask.textPreview) throw new Error("uploaded-file transcription returned no text preview");
const uploadTranscriptDownload = await downloadTask(completedUploadTask, "upload-transcript.srt");

const result = {
  checkedAt: new Date().toISOString(),
  baseUrl,
  health,
  samples,
  parsed: successful.map((item) => ({
    platform: item.media.extractor,
    title: item.media.title,
    formats: item.media.formats.length,
    images: item.media.images?.length || 0,
    subtitles: item.media.subtitles?.length || 0,
    metrics: item.media.metrics
  })),
  batchFailure: failed[0].error,
  collectionItems: collection.items.length,
  downloads: { videoDownload, audioDownload, imageDownload, subtitleDownload, urlTranscriptDownload, uploadTranscriptDownload },
  transcripts: {
    urlPreview: urlTranscriptTask.textPreview,
    uploadPreview: completedUploadTask.textPreview
  }
};
await writeFile(path.join(outputDirectory, "acceptance-results.json"), `${JSON.stringify(result, null, 2)}\n`, "utf8");
console.log(JSON.stringify(result, null, 2));
