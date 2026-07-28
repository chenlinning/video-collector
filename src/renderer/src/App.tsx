import { useEffect, useMemo, useRef, useState } from "react";
import type {
  BatchParseItem,
  CollectionInfo,
  DownloadProgress,
  MediaFormat,
  MediaInfo,
  RuntimeStatus,
  SubtitleTrack,
  TaskKind,
  WebTask
} from "../../shared/contracts";
import { resolveLocale, uiCopy, type Locale } from "./i18n";
import { downloadMediaCsv, splitBatchUrls } from "./media-export";
import { loadTheme, saveTheme, type Theme } from "./theme";
import { buildPreferencesReadyMessage, readParentPreferences, resolveParentOrigin } from "./theme-bridge";
import TextFormatter from "./TextFormatter";
import { createWebVideoCollectorApi, saveWebDownload } from "./web-api";

const api = createWebVideoCollectorApi();
type ToolKind = TaskKind | "text";
const toolKinds: ToolKind[] = ["media", "audio", "image", "subtitle", "transcript", "text"];
const activeStates = new Set(["starting", "downloading", "processing"]);
type InputMode = "single" | "batch" | "collection" | "file";
type TaskFilter = "all" | "active" | "completed" | "failed";

interface SessionTask extends DownloadProgress {
  title: string;
  kind: TaskKind;
}

function Icon({ name }: { name: "audio" | "check" | "clock" | "download" | "file" | "image" | "spark" | "subtitle" | "video" }) {
  const paths = {
    audio: <><path d="M9 18V5l10-2v13" /><circle cx="6" cy="18" r="3" /><circle cx="16" cy="16" r="3" /></>,
    check: <path d="m5 12 4 4L19 6" />,
    clock: <><circle cx="12" cy="12" r="9" /><path d="M12 7v5l3 2" /></>,
    download: <path d="M12 3v12m0 0 5-5m-5 5-5-5M5 21h14" />,
    file: <><path d="M6 2h8l4 4v16H6z" /><path d="M14 2v5h5" /></>,
    image: <><rect x="3" y="4" width="18" height="16" rx="2" /><circle cx="9" cy="10" r="2" /><path d="m4 18 5-5 4 4 2-2 5 4" /></>,
    spark: <path d="m12 3 1.4 4.6L18 9l-4.6 1.4L12 15l-1.4-4.6L6 9l4.6-1.4L12 3Zm6 11 .8 2.2L21 17l-2.2.8L18 20l-.8-2.2L15 17l2.2-.8L18 14Z" />,
    subtitle: <><rect x="3" y="5" width="18" height="14" rx="2" /><path d="M6 14h5m2 0h5M6 10h8" /></>,
    video: <><rect x="3" y="5" width="14" height="14" rx="2" /><path d="m17 10 4-2v8l-4-2" /></>
  };
  return <svg className="icon" viewBox="0 0 24 24" aria-hidden="true">{paths[name]}</svg>;
}

function toolIcon(kind: ToolKind): "audio" | "file" | "image" | "subtitle" | "spark" | "video" {
  return kind === "media" ? "video" : kind === "transcript" ? "spark" : kind === "text" ? "file" : kind;
}

function formatDuration(seconds: number | undefined, locale: Locale): string {
  if (!seconds) return uiCopy[locale].unknownDuration;
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  const remainder = Math.round(seconds % 60);
  return hours > 0
    ? `${hours}:${minutes.toString().padStart(2, "0")}:${remainder.toString().padStart(2, "0")}`
    : `${minutes}:${remainder.toString().padStart(2, "0")}`;
}

function formatBytes(bytes: number | undefined, locale: Locale): string {
  if (!bytes) return uiCopy[locale].unknownSize;
  const units = ["B", "KB", "MB", "GB"];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value.toFixed(unit > 1 ? 1 : 0)} ${units[unit]}`;
}

function formatMetric(value: number | undefined, locale: Locale): string {
  if (!value) return "—";
  return new Intl.NumberFormat(locale === "en" ? "en-US" : "zh-CN", { notation: value >= 10000 ? "compact" : "standard" }).format(value);
}

function readableError(error: unknown): string {
  const message = error instanceof Error ? error.message : String(error);
  return message.replace(/^Error invoking remote method '[^']+':\s*/, "");
}

function taskFromSnapshot(task: WebTask, title: string): SessionTask {
  return {
    taskId: task.id,
    kind: task.kind,
    title,
    state: task.state === "queued" ? "starting" : task.state === "expired" ? "failed" : task.state,
    percent: task.percent,
    speed: task.speed,
    eta: task.eta,
    outputPath: task.state === "completed" ? task.id : undefined,
    fileName: task.fileName,
    fileSize: task.fileSize,
    textPreview: task.textPreview,
    createdAt: task.createdAt,
    deleteAt: task.deleteAt,
    error: task.error
  };
}

export default function App() {
  const [locale, setLocale] = useState<Locale>(() => resolveLocale(window.navigator.language));
  const [theme, setTheme] = useState<Theme>(() => loadTheme(window.localStorage));
  const [tool, setTool] = useState<ToolKind>("media");
  const [inputMode, setInputMode] = useState<InputMode>("single");
  const [input, setInput] = useState("");
  const [results, setResults] = useState<MediaInfo[]>([]);
  const [batchItems, setBatchItems] = useState<BatchParseItem[]>([]);
  const [collection, setCollection] = useState<CollectionInfo | null>(null);
  const [selectedFormats, setSelectedFormats] = useState<Record<string, string>>({});
  const [tasks, setTasks] = useState<SessionTask[]>([]);
  const [taskFilter, setTaskFilter] = useState<TaskFilter>("all");
  const [runtime, setRuntime] = useState<RuntimeStatus | null>(null);
  const [isWorking, setIsWorking] = useState(false);
  const [savingTaskId, setSavingTaskId] = useState("");
  const [error, setError] = useState("");
  const fileInputRef = useRef<HTMLInputElement>(null);
  const copy = uiCopy[locale];

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    saveTheme(window.localStorage, theme);
  }, [theme]);

  useEffect(() => {
    document.documentElement.lang = locale;
  }, [locale]);

  useEffect(() => {
    if (window.parent === window) return;
    const parentOrigin = resolveParentOrigin(document.referrer);
    if (!parentOrigin) return;
    const handlePreferences = (event: MessageEvent) => {
      const preferences = readParentPreferences(event, window.parent, parentOrigin);
      if (!preferences) return;
      setTheme(preferences.theme);
      setLocale(preferences.locale);
    };
    window.addEventListener("message", handlePreferences);
    window.parent.postMessage(buildPreferencesReadyMessage(), parentOrigin);
    return () => window.removeEventListener("message", handlePreferences);
  }, []);

  useEffect(() => {
    void api.getRuntimeStatus().then(setRuntime).catch((reason: unknown) => setError(readableError(reason)));
    return api.onDownloadProgress((progress) => {
      setTasks((current) => {
        const index = current.findIndex((item) => item.taskId === progress.taskId);
        if (index < 0) {
          return [{ ...progress, kind: progress.kind ?? "media", title: progress.fileName ?? copy.untitledTask }, ...current];
        }
        return current.map((item, itemIndex) => itemIndex === index ? { ...item, ...progress, kind: progress.kind ?? item.kind } : item);
      });
    });
  }, [copy.untitledTask]);

  const filteredTasks = useMemo(() => tasks.filter((task) => {
    if (taskFilter === "active") return activeStates.has(task.state);
    if (taskFilter === "completed") return task.state === "completed";
    if (taskFilter === "failed") return task.state === "failed" || task.state === "cancelled";
    return true;
  }), [taskFilter, tasks]);

  const resetResults = () => {
    setResults([]);
    setBatchItems([]);
    setCollection(null);
    setSelectedFormats({});
  };

  const changeTool = (next: ToolKind) => {
    setTool(next);
    if (inputMode === "file" && next !== "transcript") setInputMode("single");
    resetResults();
    setError("");
  };

  const registerTask = (task: WebTask, title: string) => {
    setTasks((current) => {
      const next = taskFromSnapshot(task, title);
      const existing = current.findIndex((item) => item.taskId === task.id);
      return existing < 0 ? [next, ...current] : current.map((item, index) => index === existing ? { ...item, ...next } : item);
    });
  };

  const startTask = async (media: MediaInfo | null, options?: { format?: MediaFormat; resourceId?: string; subtitle?: SubtitleTrack }) => {
    if (tool === "text") return;
    const sourceUrl = media?.sourceUrl || input.trim();
    if (!sourceUrl) {
      setError(copy.missingUrl);
      return;
    }
    const format = options?.format;
    setError("");
    try {
      const task = await api.startTask({
        kind: tool,
        sourceUrl,
        mediaId: media?.id,
        title: media?.title || copy.transcriptionTitle,
        formatId: format?.id,
        hasAudio: format?.hasAudio,
        resourceId: options?.resourceId || options?.subtitle?.language,
        automatic: options?.subtitle?.automatic
      });
      registerTask(task, media?.title || copy.transcriptionTitle);
    } catch (reason) {
      setError(readableError(reason));
    }
  };

  const handleSubmit = async () => {
    if (tool === "transcript" && inputMode === "file") {
      fileInputRef.current?.click();
      return;
    }
    if (!input.trim()) {
      setError(copy.missingUrl);
      return;
    }
    if (tool === "transcript" && inputMode === "single") {
      await startTask(null);
      return;
    }
    setError("");
    setIsWorking(true);
    resetResults();
    try {
      if (inputMode === "single") {
        const media = await api.parseUrl(input.trim());
        setResults([media]);
      } else if (inputMode === "batch") {
        const urls = splitBatchUrls(input);
        if (urls.length === 0) throw new Error(copy.missingUrl);
        const items = await api.parseBatch(urls);
        setBatchItems(items);
        setResults(items.flatMap((item) => item.media ? [item.media] : []));
      } else {
        const parsedCollection = await api.parseCollection(input.trim());
        setCollection(parsedCollection);
        if (parsedCollection.items.length > 0) {
          const items = await api.parseBatch(parsedCollection.items.map((item) => item.sourceUrl));
          setBatchItems(items);
          setResults(items.flatMap((item) => item.media ? [item.media] : []));
        }
      }
    } catch (reason) {
      setError(readableError(reason));
    } finally {
      setIsWorking(false);
    }
  };

  const handleUpload = async (file: File | undefined) => {
    if (!file) return;
    setError("");
    setIsWorking(true);
    try {
      const task = await api.uploadTranscription(file);
      registerTask(task, file.name);
    } catch (reason) {
      setError(readableError(reason));
    } finally {
      setIsWorking(false);
      if (fileInputRef.current) fileInputRef.current.value = "";
    }
  };

  const handleDownloadTask = async (task: SessionTask) => {
    if (!task.outputPath) return;
    setSavingTaskId(task.taskId);
    try {
      await saveWebDownload(task.outputPath, task.fileName || "media.bin");
      const estimatedDeleteAt = new Date(Date.now() + 15 * 60 * 1000).toISOString();
      setTasks((current) => current.map((item) => item.taskId === task.taskId ? { ...item, deleteAt: estimatedDeleteAt } : item));
      window.setTimeout(() => void api.refreshTask(task.taskId).catch(() => undefined), 750);
    } finally {
      setSavingTaskId("");
    }
  };

  const renderAction = (media: MediaInfo) => {
    if (tool === "text") return null;
    const images = media.images ?? [];
    const subtitles = media.subtitles ?? [];
    if (tool === "media") {
      const selectedId = selectedFormats[media.id] || media.formats[0]?.id || "";
      const selected = media.formats.find((format) => format.id === selectedId);
      return <>
        <div className="option-grid">
          {media.formats.map((format) => (
            <label className={`format-option ${selectedId === format.id ? "selected" : ""}`} key={format.id}>
              <input type="radio" name={`format-${media.id}`} checked={selectedId === format.id} onChange={() => setSelectedFormats((value) => ({ ...value, [media.id]: format.id }))} />
              <span className="radio-indicator" />
              <span><strong>{format.label || `${format.height || "?"}p`}</strong><small>{format.extension.toUpperCase()} · {format.hasAudio ? copy.withAudio : copy.audioMerged}</small></span>
              <em>{formatBytes(format.approximateBytes, locale)}</em>
            </label>
          ))}
        </div>
        <button className="primary-button wide" disabled={!selected} onClick={() => void startTask(media, { format: selected })}><Icon name="download" />{copy.prepareVideo}</button>
      </>;
    }
    if (tool === "audio") {
      return <button className="primary-button wide" onClick={() => void startTask(media)}><Icon name="audio" />{copy.extractMp3}</button>;
    }
    if (tool === "image") {
      return images.length > 0 ? <div className="image-grid">{images.slice(0, 10).map((image) => (
        <article className="image-option" key={image.id}>
          <img src={image.url} alt={copy.imagePreview} />
          <div><span>{image.width && image.height ? `${image.width} × ${image.height}` : image.extension?.toUpperCase() || copy.image}</span><button onClick={() => void startTask(media, { resourceId: image.id })}><Icon name="download" />{copy.download}</button></div>
        </article>
      ))}</div> : <p className="inline-empty">{copy.noImages}</p>;
    }
    if (tool === "subtitle") {
      return subtitles.length > 0 ? <div className="subtitle-list">{subtitles.map((subtitle) => (
        <button key={`${subtitle.language}-${subtitle.automatic}`} onClick={() => void startTask(media, { subtitle })}>
          <Icon name="subtitle" /><span><strong>{subtitle.name || subtitle.language}</strong><small>{subtitle.automatic ? copy.automaticSubtitle : copy.manualSubtitle} · SRT</small></span><Icon name="download" />
        </button>
      ))}</div> : <p className="inline-empty">{copy.noSubtitles}</p>;
    }
    return <button className="primary-button wide" onClick={() => void startTask(media)}><Icon name="spark" />{copy.startTranscription}</button>;
  };

  return (
    <div className="app-shell">
      <main className="workspace">
        <section className="hero-card">
          <div className="hero-copy">
            <div className="eyebrow"><Icon name="spark" /> {copy.eyebrow}</div>
            <h1>{copy.titleLead}<em>{copy.titleEmphasis}</em></h1>
            <p>{copy.webDescription}</p>
          </div>
          <div className="retention-callout"><Icon name="clock" /><div><strong>{copy.temporaryTitle}</strong><span>{copy.retentionDetails}</span></div></div>
        </section>

        <section className="tool-tabs" aria-label={copy.toolNavigation}>
          {toolKinds.map((kind) => <button key={kind} className={tool === kind ? "active" : ""} onClick={() => changeTool(kind)}>
            <Icon name={toolIcon(kind)} /><span><strong>{copy.tools[kind]}</strong><small>{copy.toolHints[kind]}</small></span>
          </button>)}
        </section>

        {tool === "text" ? <TextFormatter locale={locale} extractDocument={(file) => api.extractTextDocument(file)} /> : <div className="main-grid">
          <section className="content-column">
            <section className="composer-card">
              <div className="composer-heading"><div><span className="step-number">01</span><div><h2>{copy.tools[tool]}</h2><p>{copy.toolDescriptions[tool]}</p></div></div></div>
              <div className="mode-tabs">
                {(["single", "batch", "collection"] as InputMode[]).map((mode) => <button key={mode} className={inputMode === mode ? "active" : ""} onClick={() => setInputMode(mode)}>{copy.modes[mode]}</button>)}
                {tool === "transcript" && <button className={inputMode === "file" ? "active" : ""} onClick={() => setInputMode("file")}>{copy.modes.file}</button>}
              </div>

              {inputMode === "file" ? (
                <div className="upload-zone" onClick={() => fileInputRef.current?.click()}>
                  <input ref={fileInputRef} type="file" accept="audio/*,video/*,.mkv,.webm,.flac,.opus" onChange={(event) => void handleUpload(event.target.files?.[0])} />
                  <Icon name="file" /><strong>{isWorking ? copy.uploading : copy.chooseMediaFile}</strong><span>{copy.uploadLimits}</span>
                </div>
              ) : (
                <div className={`url-composer ${inputMode === "batch" ? "multiline" : ""}`}>
                  {inputMode === "batch" ? <textarea value={input} onChange={(event) => setInput(event.target.value)} placeholder={copy.batchPlaceholder} rows={5} /> : <input data-testid="url-input" value={input} onChange={(event) => setInput(event.target.value)} onKeyDown={(event) => { if (event.key === "Enter") void handleSubmit(); }} placeholder={inputMode === "collection" ? copy.collectionPlaceholder : copy.urlPlaceholder} />}
                  <button data-testid="parse-button" className="primary-button" onClick={() => void handleSubmit()} disabled={isWorking}>{isWorking ? <span className="spinner" /> : <Icon name={tool === "transcript" ? "spark" : "download"} />}{isWorking ? copy.parsing : tool === "transcript" && inputMode === "single" ? copy.startTranscription : copy.parse}</button>
                </div>
              )}
              <p className="input-help">{inputMode === "batch" ? copy.batchHelp : inputMode === "collection" ? copy.collectionHelp : copy.publicContentHelp}</p>
            </section>

            {error && <div className="error-banner" role="alert"><strong>{copy.operationFailed}</strong><span>{error}</span></div>}

            {(results.length > 0 || batchItems.length > 0 || collection) && <div className="results-heading">
              <div><span className="step-number">02</span><div><h2>{collection?.title || copy.parseResults}</h2><p>{copy.resultsSummary(results.length, batchItems.filter((item) => item.error).length)}</p></div></div>
              <button className="secondary-button" disabled={results.length === 0} onClick={() => downloadMediaCsv(results)}>{copy.exportCsv}</button>
            </div>}

            {batchItems.filter((item) => item.error).map((item) => <div className="batch-error" key={item.url}><strong>{item.url}</strong><span>{item.error}</span></div>)}

            {results.length === 0 && !isWorking && !error && <section className="empty-card"><div><Icon name={toolIcon(tool)} /></div><h2>{copy.emptyTitles[tool]}</h2><p>{copy.emptyDescriptions[tool]}</p></section>}

            {isWorking && <section className="empty-card loading"><span className="spinner large" /><h2>{copy.loadingTitle}</h2><p>{copy.loadingDescription}</p></section>}

            {results.map((media) => <article className="media-card" key={`${media.extractor}-${media.id}`}>
              <div className="media-summary">
                <div className="cover-wrap">{media.thumbnail ? <img src={media.thumbnail} alt={copy.thumbnailAlt} /> : <Icon name="video" />}<span>{formatDuration(media.duration, locale)}</span></div>
                <div className="media-copy"><div className="platform-tag">{media.extractor}</div><h2>{media.title}</h2><p>{media.uploader || copy.unknownCreator}</p></div>
              </div>
              <div className="metrics-grid">
                {(["views", "likes", "comments", "reposts"] as const).map((metric) => <div key={metric}><span>{copy.metrics[metric]}</span><strong>{formatMetric(media.metrics?.[metric], locale)}</strong></div>)}
              </div>
              <div className="media-actions">{renderAction(media)}</div>
            </article>)}
          </section>

          <aside className="sidebar">
            <section className="task-card">
              <div className="sidebar-heading"><div><span className="status-dot" /><div><strong>{copy.taskCenter}</strong><small>{copy.sessionOnly}</small></div></div><b>{tasks.length}</b></div>
              <div className="task-filters">{(["all", "active", "completed", "failed"] as TaskFilter[]).map((filter) => <button key={filter} className={taskFilter === filter ? "active" : ""} onClick={() => setTaskFilter(filter)}>{copy.taskFilters[filter]}</button>)}</div>
              <div className="task-list">
                {filteredTasks.length === 0 ? <div className="task-empty"><Icon name="clock" /><span>{copy.noTasks}</span></div> : filteredTasks.map((task) => <article className="task-item" key={task.taskId}>
                  <div className="task-title"><span><Icon name={toolIcon(task.kind)} /></span><div><strong>{task.title}</strong><small>{copy.tools[task.kind]} · {copy.states[task.state]}</small></div><b>{Math.round(task.percent)}%</b></div>
                  <div className="progress-track"><span style={{ width: `${task.percent}%` }} /></div>
                  {task.error && <p className="task-error">{task.error}</p>}
                  {task.textPreview && <p className="text-preview">{task.textPreview}</p>}
                  {task.state === "completed" && task.outputPath && <button className="task-download" disabled={savingTaskId === task.taskId} onClick={() => void handleDownloadTask(task)}><Icon name="download" />{copy.download} {task.fileName ? `· ${task.fileName}` : ""}</button>}
                  {activeStates.has(task.state) && <button className="task-cancel" onClick={() => void api.cancelDownload(task.taskId)}>{copy.cancelTask}</button>}
                  <p className="task-retention">{task.deleteAt ? copy.deleteAt(new Date(task.deleteAt).toLocaleString(locale === "en" ? "en-US" : "zh-CN")) : copy.unclaimedRetention}</p>
                </article>)}
              </div>
            </section>

            <section className="runtime-card">
              <div className="sidebar-heading"><div><span className="status-dot" /><div><strong>{copy.onlineReady}</strong><small>{copy.runtimeStatus}</small></div></div></div>
              <dl><div><dt>yt-dlp</dt><dd>{runtime?.ytDlpVersion || copy.checking}</dd></div><div><dt>FFmpeg</dt><dd>{runtime?.ffmpegVersion.split(" ")[0] || copy.checking}</dd></div><div><dt>Whisper</dt><dd>{[runtime?.whisperVersion, runtime?.whisperModel].filter(Boolean).join(" · ") || copy.checking}</dd></div></dl>
            </section>

            <section className="legal-card"><Icon name="check" /><div><strong>{copy.legalTitle}</strong><p>{copy.legalDescription}</p></div></section>
          </aside>
        </div>}
      </main>
    </div>
  );
}
