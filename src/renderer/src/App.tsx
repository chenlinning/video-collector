import { useEffect, useMemo, useState } from "react";
import type {
  DownloadHistoryItem,
  DownloadProgress,
  MediaFormat,
  MediaInfo,
  RuntimeStatus,
  VideoCollectorApi
} from "../../shared/contracts";
import { resolveLocale, uiCopy, type Locale } from "./i18n";
import { loadTheme, saveTheme, type Theme } from "./theme";
import {
  buildPreferencesReadyMessage,
  readParentPreferences,
  resolveParentOrigin
} from "./theme-bridge";
import { createWebVideoCollectorApi, saveWebDownload } from "./web-api";

const webMode = !window.videoCollector;
const api: VideoCollectorApi = window.videoCollector ?? createWebVideoCollectorApi();

function Icon({ name }: { name: "download" | "folder" | "play" | "clock" | "trash" | "spark" }) {
  const paths = {
    download: <path d="M12 3v12m0 0 5-5m-5 5-5-5M5 21h14" />,
    folder: <path d="M3 7h7l2 2h9v10H3V7Zm0 0V5h7l2 2" />,
    play: <path d="m9 7 8 5-8 5V7Z" />,
    clock: <path d="M12 7v5l3 2m6-2a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z" />,
    trash: <path d="M4 7h16M9 7V4h6v3m3 0-1 14H7L6 7m4 4v6m4-6v6" />,
    spark: <path d="m12 3 1.4 4.6L18 9l-4.6 1.4L12 15l-1.4-4.6L6 9l4.6-1.4L12 3Zm6 11 .8 2.2L21 17l-2.2.8L18 20l-.8-2.2L15 17l2.2-.8L18 14Z" />
  };
  return <svg className="icon" viewBox="0 0 24 24" aria-hidden="true">{paths[name]}</svg>;
}

function formatDuration(seconds: number | undefined, locale: Locale): string {
  if (!seconds) return uiCopy[locale].unknownDuration;
  const minutes = Math.floor(seconds / 60);
  const remainder = Math.round(seconds % 60);
  return `${minutes}:${remainder.toString().padStart(2, "0")}`;
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

function readableError(error: unknown): string {
  const message = error instanceof Error ? error.message : String(error);
  return message.replace(/^Error invoking remote method '[^']+':\s*/, "");
}

function stateText(progress: DownloadProgress, locale: Locale): string {
  return uiCopy[locale].states[progress.state];
}

export default function App() {
  const [locale, setLocale] = useState<Locale>(() => resolveLocale(window.navigator.language));
  const [theme, setTheme] = useState<Theme>(() => {
    const initialTheme = loadTheme(window.localStorage);
    document.documentElement.dataset.theme = initialTheme;
    return initialTheme;
  });
  const [url, setUrl] = useState("");
  const [media, setMedia] = useState<MediaInfo | null>(null);
  const [selectedFormatId, setSelectedFormatId] = useState("");
  const [directory, setDirectory] = useState("");
  const [runtime, setRuntime] = useState<RuntimeStatus | null>(null);
  const [history, setHistory] = useState<DownloadHistoryItem[]>([]);
  const [progress, setProgress] = useState<DownloadProgress | null>(null);
  const [isParsing, setIsParsing] = useState(false);
  const [isSaving, setIsSaving] = useState(false);
  const [saveProgress, setSaveProgress] = useState(0);
  const [deleteAt, setDeleteAt] = useState("");
  const [error, setError] = useState("");
  const copy = uiCopy[locale];
  const dateLocale = locale === "en" ? "en-US" : "zh-CN";

  const selectedFormat = useMemo(
    () => media?.formats.find((format) => format.id === selectedFormatId),
    [media, selectedFormatId]
  );
  const activeDownload = Boolean(
    progress && ["starting", "downloading", "processing"].includes(progress.state)
  );

  const refreshHistory = async () => {
    setHistory(await api.listHistory());
  };

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
    void api.getRuntimeStatus().then((status) => {
      setRuntime(status);
      setDirectory(status.defaultDownloadDirectory);
    }).catch((reason: unknown) => setError(readableError(reason)));
    void refreshHistory();
    return api.onDownloadProgress((nextProgress) => {
      setProgress(nextProgress);
      if (nextProgress.state === "completed") {
        void refreshHistory();
      }
    });
  }, []);

  const handleParse = async () => {
    if (!url.trim()) {
      setError(copy.missingUrl);
      return;
    }
    setError("");
    setProgress(null);
    setIsParsing(true);
    try {
      const result = await api.parseUrl(url.trim());
      setMedia(result);
      setSelectedFormatId(result.formats[0]?.id ?? "");
    } catch (reason) {
      setMedia(null);
      setError(readableError(reason));
    } finally {
      setIsParsing(false);
    }
  };

  const handleChooseDirectory = async () => {
    const selected = await api.chooseDirectory();
    if (selected) setDirectory(selected);
  };

  const handleDownload = async () => {
    if (!media || !selectedFormat || (!directory && !webMode)) return;
    setError("");
    setProgress({ taskId: "pending", state: "starting", percent: 0 });
    try {
      const result = await api.startDownload({
        sourceUrl: media.sourceUrl,
        mediaId: media.id,
        title: media.title,
        formatId: selectedFormat.id,
        hasAudio: selectedFormat.hasAudio,
        outputDirectory: directory
      });
      setProgress((current) => ({
        taskId: result.taskId,
        state: current?.state ?? "starting",
        percent: current?.percent ?? 0
      }));
    } catch (reason) {
      setProgress(null);
      setError(readableError(reason));
    }
  };

  const handleSaveWebDownload = async () => {
    if (!webMode || !progress?.outputPath) return;
    setError("");
    setIsSaving(true);
    setSaveProgress(0);
    try {
      const expiresAt = await saveWebDownload(
        progress.outputPath,
        progress.fileName || `${media?.title || "video"}.mp4`,
        setSaveProgress
      );
      setDeleteAt(expiresAt || "");
    } catch (reason) {
      if ((reason as { name?: string })?.name !== "AbortError") setError(readableError(reason));
    } finally {
      setIsSaving(false);
    }
  };

  const handleCancel = async () => {
    if (progress && progress.taskId !== "pending") {
      await api.cancelDownload(progress.taskId);
    }
  };

  const handleClearHistory = async () => {
    await api.clearHistory();
    setHistory([]);
  };

  return (
    <div className="app-shell">
      <main className="workspace">
        <section className="content-column">
          <div className="hero-card">
            <div className="hero-meta">
              <div className="eyebrow"><Icon name="spark" /> PUBLIC MEDIA COLLECTOR</div>
              <div className="privacy-pill"><span /> {webMode ? copy.retentionNotice : copy.localNotice}</div>
            </div>
            <h1>{copy.titleLead}<em>{copy.titleEmphasis}</em></h1>
            <p>{webMode ? copy.webDescription : copy.localDescription}</p>
            <div className="url-composer">
              <div className="url-input-wrap">
                <span className="link-symbol">↗</span>
                <input
                  data-testid="url-input"
                  value={url}
                  onChange={(event) => setUrl(event.target.value)}
                  onKeyDown={(event) => {
                    if (event.key === "Enter") void handleParse();
                  }}
                  placeholder={copy.urlPlaceholder}
                  spellCheck={false}
                />
              </div>
              <button
                data-testid="parse-button"
                className="primary-button"
                onClick={() => void handleParse()}
                disabled={isParsing}
              >
                {isParsing ? <span className="spinner" /> : <Icon name="spark" />}
                {isParsing ? copy.parsing : copy.parse}
              </button>
            </div>
            <div className="support-row">
              <span>{copy.publicContent}</span><span>{copy.multipleQualities}</span><span>{copy.avMerge}</span><span>{copy.resume}</span>
            </div>
          </div>

          {error && <div className="error-banner" role="alert"><strong>{copy.operationFailed}</strong><span>{error}</span></div>}

          {!media && !isParsing && (
            <div className="empty-card">
              <div className="empty-icon"><Icon name="download" /></div>
              <h2>{copy.emptyTitle}</h2>
              <p>{copy.emptyDescription}</p>
            </div>
          )}

          {isParsing && (
            <div className="empty-card loading-card">
              <div className="orbit"><span /><span /><span /></div>
              <h2>{copy.loadingTitle}</h2>
              <p>{copy.loadingDescription}</p>
            </div>
          )}

          {media && !isParsing && (
            <section className="result-card" data-testid="media-result">
              <div className="media-summary">
                <div className="cover-wrap">
                  {media.thumbnail ? <img src={media.thumbnail} alt={copy.thumbnailAlt} /> : <Icon name="play" />}
                  <span className="duration-badge"><Icon name="clock" />{formatDuration(media.duration, locale)}</span>
                </div>
                <div className="media-copy">
                  <div className="platform-tag">{media.extractor}</div>
                  <h2>{media.title}</h2>
                  <p>@{media.uploader} · ID {media.id}</p>
                </div>
              </div>

              <div className="section-heading">
                <div><span>01</span><h3>{copy.chooseFormat}</h3></div>
                <small>{copy.availableOptions(media.formats.length)}</small>
              </div>
              <div className="format-list">
                {media.formats.map((format: MediaFormat) => (
                  <label className={`format-option ${selectedFormatId === format.id ? "selected" : ""}`} key={format.id}>
                    <input
                      type="radio"
                      name="format"
                      value={format.id}
                      checked={selectedFormatId === format.id}
                      onChange={() => setSelectedFormatId(format.id)}
                    />
                    <span className="radio-indicator" />
                    <span className="format-main"><strong>{format.label}</strong><small>{format.id}</small></span>
                    <span className="format-size">{formatBytes(format.approximateBytes, locale)}</span>
                  </label>
                ))}
              </div>

              {!webMode && <>
                <div className="section-heading directory-heading">
                  <div><span>02</span><h3>{copy.saveLocation}</h3></div>
                </div>
                <button className="directory-button" onClick={() => void handleChooseDirectory()}>
                  <Icon name="folder" /><span>{directory || copy.chooseDirectory}</span><b>{copy.change}</b>
                </button>
              </>}

              {progress && (
                <div className={`progress-panel state-${progress.state}`}>
                  <div className="progress-copy">
                    <strong>{stateText(progress, locale)}</strong>
                    <span>{progress.speed || ""}{progress.eta ? ` · ${copy.remaining(progress.eta)}` : ""}</span>
                    <b>{Math.round(progress.percent)}%</b>
                  </div>
                  <div className="progress-track"><span style={{ width: `${progress.percent}%` }} /></div>
                  {progress.error && <p>{progress.error}</p>}
                  {progress.state === "completed" && progress.outputPath && (
                    <div className="completion-actions">
                      {webMode ? (
                        <button disabled={isSaving} onClick={() => void handleSaveWebDownload()}>
                          {isSaving ? copy.savingProgress(saveProgress) : copy.downloadLocal}
                        </button>
                      ) : <>
                        <button onClick={() => void api.openPath(progress.outputPath!)}>{copy.openVideo}</button>
                        <button onClick={() => void api.showInFolder(progress.outputPath!)}>{copy.showInFolder}</button>
                      </>}
                    </div>
                  )}
                  {webMode && deleteAt && <p>{copy.deleteAt(new Date(deleteAt).toLocaleString(dateLocale))}</p>}
                </div>
              )}

              <div className="download-actions">
                {activeDownload ? (
                  <button className="cancel-button" onClick={() => void handleCancel()}>{copy.cancelTask}</button>
                ) : (
                  <button className="download-button" onClick={() => void handleDownload()} disabled={!selectedFormat || (!directory && !webMode)}>
                    <Icon name="download" /><span>{webMode ? copy.prepareVideo : copy.downloadLocal}</span><small>{selectedFormat?.extension.toUpperCase() || ""}</small>
                  </button>
                )}
              </div>
            </section>
          )}
        </section>

        <aside className="sidebar">
          <section className="status-card">
            <div className="status-heading"><span className="status-dot" /><strong>{webMode ? copy.onlineReady : copy.localReady}</strong></div>
            <dl>
              <div><dt>yt-dlp</dt><dd>{runtime?.ytDlpVersion || copy.checking}</dd></div>
              <div><dt>FFmpeg</dt><dd>{runtime?.ffmpegVersion.split(" ")[0] || copy.checking}</dd></div>
            </dl>
          </section>

          {!webMode && <section className="history-card">
            <div className="history-heading">
              <div><span>{copy.recentDownloads}</span><small>{copy.records(history.length)}</small></div>
              {history.length > 0 && <button title={copy.clearHistory} onClick={() => void handleClearHistory()}><Icon name="trash" /></button>}
            </div>
            <div className="history-list">
              {history.length === 0 ? (
                <div className="history-empty"><Icon name="clock" /><p>{copy.historyEmpty}</p></div>
              ) : history.map((item) => (
                <article className="history-item" key={item.id}>
                  <div className="history-play"><Icon name="play" /></div>
                  <div><strong>{item.title}</strong><span>{new Date(item.completedAt).toLocaleString(dateLocale)}</span></div>
                  <button title={copy.showInFolder} onClick={() => void api.showInFolder(item.outputPath)}><Icon name="folder" /></button>
                </article>
              ))}
            </div>
          </section>}

          {webMode && <section className="history-card">
            <div className="history-heading"><div><span>{copy.temporaryFiles}</span><small>{copy.automaticCleanup}</small></div></div>
            <div className="history-empty">
              <Icon name="clock" />
              <p>{copy.retentionDetails}</p>
            </div>
          </section>}

          <div className="legal-note">
            <strong>{copy.legalTitle}</strong>
            <p>{copy.legalDescription}</p>
          </div>
        </aside>
      </main>
    </div>
  );
}
