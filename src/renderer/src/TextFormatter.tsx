import { useMemo, useRef, useState } from "react";
import type { ExtractedTextDocument } from "../../shared/contracts";
import type { Locale } from "./i18n";
import {
  downloadText,
  countTextCharacters,
  formatText,
  joinTextSegments,
  resolveTextSegment,
  safeTextFileName,
  splitFormattedText,
  type TextFormatResult,
  type TextSegment,
  type TextSegmentIssue
} from "./text-formatter";

const maxTextFileBytes = 20 * 1024 * 1024;
const maxSegmentLimit = 1_000_000;

interface TextFormatterProps {
  locale: Locale;
  extractDocument(file: File): Promise<ExtractedTextDocument>;
}

interface ImportedFileInfo {
  fileName: string;
  format: string;
  byteSize: number;
  characterCount?: number;
  status: "extracting" | "complete" | "failed";
}

const textCopy = {
  "zh-CN": {
    eyebrow: "LOCAL TEXT UTILITY",
    title: "文本格式化",
    description: "完整提取文章文字，移除标点并整理为单换行；按换行边界在字数上限内分段。",
    inputTitle: "输入原文",
    inputDescription: "直接粘贴，或导入 TXT、Markdown、DOCX、DOC 文件。",
    importFile: "导入文件",
    importing: "正在完整提取",
    clear: "清空",
    placeholder: "在这里粘贴需要格式化的文章……",
    supported: "支持 TXT、MD、Markdown、DOCX、DOC · 单文件最大 20 MiB",
    format: "移除标点并整理换行",
    outputTitle: "格式化结果",
    outputDescription: "保留原换行；连续标点与连续换行最终只保留一个换行。",
    emptyOutput: "完成格式化后，结果会显示在这里。",
    copyAll: "复制全部",
    copied: "已复制完整结果",
    exportAll: "导出完整 TXT",
    originalCharacters: "原文字数",
    resultCharacters: "结果字数",
    removedPunctuation: "移除标点",
    resultLines: "结果行数",
    segmentTitle: "按换行限字分段",
    segmentDescription: "下一行加入将超限时，在该行之前结束当前段；绝不截断完整行。",
    limit: "每段最大字数",
    segment: "生成分段",
    segmentSelect: "选择分段",
    exportSegments: "依次导出全部分段",
    copySegments: "复制全部分段",
    copySelectedSegment: "复制分段",
    copySegment: "复制本段",
    exportSegment: "导出本段",
    segmentLabel: (index: number) => `第 ${index} 段`,
    segmentStats: (characters: number, lines: number) => `${characters} 字 · ${lines} 行`,
    noSource: "请先输入文章或导入文件",
    invalidLimit: `字数上限必须是 1 到 ${maxSegmentLimit.toLocaleString("zh-CN")} 的整数`,
    extractionFailed: "文件未能完整提取",
    extractionStatus: { extracting: "提取中", complete: "提取成功", failed: "提取失败" },
    fileTooLarge: "文件超过 20 MiB 限制",
    emptyResult: "格式化结果为空，请输入包含文字的内容",
    copyFailed: "浏览器未允许写入剪贴板",
    downloadFailed: "浏览器未能创建下载文件",
    segmentsCopied: "全部分段已复制",
    segmentCopied: (index: number) => `第 ${index} 段已复制`,
    lineTooLong: (issue: TextSegmentIssue) => `第 ${issue.lineNumber} 行共 ${issue.characterCount} 字，超过每段 ${issue.limit} 字限制；请提高字数上限或在原文中增加换行。`,
    imported: (name: string, format: string) => `已完整提取 ${name} · ${format.toUpperCase()}`,
    localOnly: "格式化、分段、复制和导出均在浏览器本地完成。"
  },
  en: {
    eyebrow: "LOCAL TEXT UTILITY",
    title: "Text formatter",
    description: "Extract complete document text, replace punctuation with single line breaks, and split only at line boundaries under a character limit.",
    inputTitle: "Source text",
    inputDescription: "Paste text or import TXT, Markdown, DOCX, or DOC.",
    importFile: "Import file",
    importing: "Extracting complete text",
    clear: "Clear",
    placeholder: "Paste the article to format…",
    supported: "TXT, MD, Markdown, DOCX, DOC · 20 MiB maximum",
    format: "Remove punctuation and normalize breaks",
    outputTitle: "Formatted result",
    outputDescription: "Original breaks are preserved; consecutive punctuation and breaks collapse to one break.",
    emptyOutput: "The complete formatted result will appear here.",
    copyAll: "Copy all",
    copied: "Complete result copied",
    exportAll: "Export complete TXT",
    originalCharacters: "Source characters",
    resultCharacters: "Result characters",
    removedPunctuation: "Punctuation removed",
    resultLines: "Result lines",
    segmentTitle: "Split by line and character limit",
    segmentDescription: "If the next line would exceed the limit, the current segment ends before it. Complete lines are never cut.",
    limit: "Maximum characters per segment",
    segment: "Create segments",
    segmentSelect: "Select segment",
    exportSegments: "Export every segment",
    copySegments: "Copy every segment",
    copySelectedSegment: "Copy selected",
    copySegment: "Copy segment",
    exportSegment: "Export segment",
    segmentLabel: (index: number) => `Segment ${index}`,
    segmentStats: (characters: number, lines: number) => `${characters} characters · ${lines} lines`,
    noSource: "Enter text or import a document first",
    invalidLimit: `The limit must be an integer from 1 to ${maxSegmentLimit.toLocaleString("en-US")}`,
    extractionFailed: "The complete file could not be extracted",
    extractionStatus: { extracting: "Extracting", complete: "Complete", failed: "Failed" },
    fileTooLarge: "The file exceeds the 20 MiB limit",
    emptyResult: "The formatted result is empty. Enter content containing text.",
    copyFailed: "Clipboard access was not allowed",
    downloadFailed: "The browser could not create the download file",
    segmentsCopied: "Every segment copied",
    segmentCopied: (index: number) => `Segment ${index} copied`,
    lineTooLong: (issue: TextSegmentIssue) => `Line ${issue.lineNumber} contains ${issue.characterCount} characters, above the ${issue.limit} limit. Increase the limit or add a source line break.`,
    imported: (name: string, format: string) => `Extracted all readable text from ${name} · ${format.toUpperCase()}`,
    localOnly: "Formatting, segmentation, copying, and export stay in this browser."
  }
} as const;

export default function TextFormatter({ locale, extractDocument }: TextFormatterProps) {
  const copy = textCopy[locale];
  const fileInput = useRef<HTMLInputElement>(null);
  const [source, setSource] = useState("");
  const [sourceName, setSourceName] = useState("article");
  const [fileInfo, setFileInfo] = useState<ImportedFileInfo | null>(null);
  const [formatted, setFormatted] = useState<TextFormatResult | null>(null);
  const [limit, setLimit] = useState("1000");
  const [segments, setSegments] = useState<TextSegment[]>([]);
  const [selectedSegmentIndex, setSelectedSegmentIndex] = useState<number | null>(null);
  const [issues, setIssues] = useState<TextSegmentIssue[]>([]);
  const [isImporting, setIsImporting] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const sourceCharacterCount = useMemo(() => countTextCharacters(source), [source]);
  const selectedSegment = useMemo(
    () => resolveTextSegment(segments, selectedSegmentIndex),
    [segments, selectedSegmentIndex]
  );

  const invalidateResults = () => {
    setFormatted(null);
    setSegments([]);
    setSelectedSegmentIndex(null);
    setIssues([]);
    setMessage("");
    setError("");
  };

  const updateSource = (value: string) => {
    setSource(value);
    setFileInfo(null);
    setSourceName("article");
    invalidateResults();
  };

  const handleFile = async (file: File | undefined) => {
    if (!file) return;
    const format = file.name.split(".").pop()?.toLowerCase() || "unknown";
    setMessage("");
    setError("");
    setSegments([]);
    setSelectedSegmentIndex(null);
    setIssues([]);
    setFileInfo({ fileName: file.name, format, byteSize: file.size, status: "extracting" });
    if (file.size > maxTextFileBytes) {
      setFileInfo({ fileName: file.name, format, byteSize: file.size, status: "failed" });
      setError(copy.fileTooLarge);
      return;
    }
    setIsImporting(true);
    try {
      const extracted = await extractDocument(file);
      setSource(extracted.text);
      setSourceName(safeTextFileName(extracted.fileName, "article"));
      setFileInfo({ ...extracted, status: "complete" });
      setFormatted(null);
      setMessage(copy.imported(extracted.fileName, extracted.format));
    } catch (reason) {
      setFileInfo({ fileName: file.name, format, byteSize: file.size, status: "failed" });
      setError(`${copy.extractionFailed}: ${reason instanceof Error ? reason.message : String(reason)}`);
    } finally {
      setIsImporting(false);
      if (fileInput.current) fileInput.current.value = "";
    }
  };

  const handleFormat = () => {
    if (!source) {
      setError(copy.noSource);
      return;
    }
    setError("");
    setMessage("");
    setSegments([]);
    setSelectedSegmentIndex(null);
    setIssues([]);
    const result = formatText(source);
    if (result.characterCount === 0) {
      setFormatted(null);
      setError(copy.emptyResult);
      return;
    }
    setFormatted(result);
  };

  const copyText = async (value: string, successMessage: string) => {
    try {
      await navigator.clipboard.writeText(value);
      setMessage(successMessage);
      setError("");
    } catch {
      setError(copy.copyFailed);
    }
  };

  const handleSegments = () => {
    if (!formatted) {
      setError(copy.noSource);
      return;
    }
    setSegments([]);
    setSelectedSegmentIndex(null);
    setIssues([]);
    const numericLimit = Number(limit);
    if (!Number.isSafeInteger(numericLimit) || numericLimit < 1 || numericLimit > maxSegmentLimit) {
      setError(copy.invalidLimit);
      return;
    }
    const result = splitFormattedText(formatted.text, numericLimit);
    setSegments(result.segments);
    setSelectedSegmentIndex(result.segments[0]?.index ?? null);
    setIssues(result.issues);
    setError(result.issues[0] ? copy.lineTooLong(result.issues[0]) : "");
    setMessage("");
  };

  const exportSegment = (segment: TextSegment) => {
    return exportText(segment.text, `${sourceName}-${String(segment.index).padStart(3, "0")}.txt`);
  };

  const exportAllSegments = () => {
    for (const segment of segments) {
      if (!exportSegment(segment)) break;
    }
  };

  const exportText = (text: string, fileName: string): boolean => {
    try {
      downloadText(text, fileName);
      setError("");
      return true;
    } catch {
      setError(copy.downloadFailed);
      return false;
    }
  };

  const copyAllSegments = () => {
    void copyText(joinTextSegments(segments), copy.segmentsCopied);
  };

  const copySelectedSegment = () => {
    if (!selectedSegment) return;
    void copyText(selectedSegment.text, copy.segmentCopied(selectedSegment.index));
  };

  const updateLimit = (value: string) => {
    setLimit(value);
    setSegments([]);
    setSelectedSegmentIndex(null);
    setIssues([]);
    setMessage("");
    setError("");
  };

  const clearAll = () => {
    setSource("");
    setSourceName("article");
    setFileInfo(null);
    invalidateResults();
  };

  return (
    <section className="text-formatter-shell">
      <header className="text-formatter-hero">
        <div><span>{copy.eyebrow}</span><h2>{copy.title}</h2><p>{copy.description}</p></div>
        <div className="text-local-badge"><span className="status-dot" />{copy.localOnly}</div>
      </header>

      <div className="text-formatter-grid">
        <section className="text-panel">
          <div className="text-panel-heading">
            <div><span className="step-number">01</span><div><h3>{copy.inputTitle}</h3><p>{copy.inputDescription}</p></div></div>
            <div className="text-heading-actions">
              <input ref={fileInput} type="file" hidden accept=".txt,.md,.markdown,.docx,.doc,text/plain,text/markdown,application/msword,application/vnd.openxmlformats-officedocument.wordprocessingml.document" onChange={(event) => void handleFile(event.target.files?.[0])} />
              <button className="secondary-button" disabled={isImporting} onClick={() => fileInput.current?.click()}>{isImporting ? copy.importing : copy.importFile}</button>
              <button className="secondary-button" disabled={!source || isImporting} onClick={clearAll}>{copy.clear}</button>
            </div>
          </div>
          {fileInfo && <div className={`text-file-info ${fileInfo.status}`}><strong>{fileInfo.fileName}</strong><span>{fileInfo.format.toUpperCase()} · {(fileInfo.byteSize / 1024).toFixed(1)} KiB · {copy.extractionStatus[fileInfo.status]}{fileInfo.characterCount === undefined ? "" : ` · ${fileInfo.characterCount.toLocaleString(locale === "en" ? "en-US" : "zh-CN")} ${locale === "en" ? "characters" : "字符"}`}</span></div>}
          <textarea className="text-source-input" value={source} onChange={(event) => updateSource(event.target.value)} placeholder={copy.placeholder} spellCheck={false} />
          <div className="text-input-footer"><span>{copy.supported}</span><strong>{sourceCharacterCount.toLocaleString(locale === "en" ? "en-US" : "zh-CN")}</strong></div>
          <button className="primary-button text-primary-action" disabled={isImporting || !source} onClick={handleFormat}>{copy.format}</button>
        </section>

        <section className="text-panel">
          <div className="text-panel-heading">
            <div><span className="step-number">02</span><div><h3>{copy.outputTitle}</h3><p>{copy.outputDescription}</p></div></div>
            <div className="text-heading-actions">
              <button className="secondary-button" disabled={!formatted} onClick={() => formatted && void copyText(formatted.text, copy.copied)}>{copy.copyAll}</button>
              <button className="secondary-button" disabled={!formatted} onClick={() => formatted && exportText(formatted.text, `${sourceName}-formatted.txt`)}>{copy.exportAll}</button>
            </div>
          </div>
          {formatted ? <>
            <div className="text-stats">
              <div><span>{copy.originalCharacters}</span><strong>{sourceCharacterCount.toLocaleString()}</strong></div>
              <div><span>{copy.resultCharacters}</span><strong>{formatted.characterCount.toLocaleString()}</strong></div>
              <div><span>{copy.removedPunctuation}</span><strong>{formatted.removedPunctuationCount.toLocaleString()}</strong></div>
              <div><span>{copy.resultLines}</span><strong>{formatted.lineCount.toLocaleString()}</strong></div>
            </div>
            <textarea className="text-result-output" readOnly value={formatted.text} spellCheck={false} />
          </> : <div className="text-result-empty">{copy.emptyOutput}</div>}
        </section>
      </div>

      {(message || error) && <div className={`text-notice ${error ? "error" : "success"}`} role={error ? "alert" : "status"}>{error || message}</div>}

      <section className="text-segment-panel">
        <div className="text-panel-heading">
          <div><span className="step-number">03</span><div><h3>{copy.segmentTitle}</h3><p>{copy.segmentDescription}</p></div></div>
          <div className="segment-controls">
            <label>{copy.limit}<input type="number" min="1" max={maxSegmentLimit} step="1" value={limit} onChange={(event) => updateLimit(event.target.value)} /></label>
            <button className="primary-button" disabled={!formatted} onClick={handleSegments}>{copy.segment}</button>
            <label>{copy.segmentSelect}<select disabled={!selectedSegment} value={selectedSegment?.index ?? ""} onChange={(event) => setSelectedSegmentIndex(Number(event.target.value))}>
              {segments.length === 0 && <option value="">—</option>}
              {segments.map((segment) => <option key={segment.index} value={segment.index}>{copy.segmentLabel(segment.index)} · {copy.segmentStats(segment.characterCount, segment.lineCount)}</option>)}
            </select></label>
            <button className="secondary-button" disabled={!selectedSegment || issues.length > 0} onClick={copySelectedSegment}>{copy.copySelectedSegment}</button>
            <button className="secondary-button" disabled={segments.length === 0 || issues.length > 0} onClick={copyAllSegments}>{copy.copySegments}</button>
            <button className="secondary-button" disabled={segments.length === 0 || issues.length > 0} onClick={exportAllSegments}>{copy.exportSegments}</button>
          </div>
        </div>

        {issues.length > 0 && <div className="segment-issues">{issues.map((issue) => <p key={issue.lineNumber}>{copy.lineTooLong(issue)}</p>)}</div>}
        {segments.length > 0 && <div className="segment-list">{segments.map((segment) => <article className="segment-card" key={segment.index}>
          <header><div><strong>{copy.segmentLabel(segment.index)}</strong><span>{copy.segmentStats(segment.characterCount, segment.lineCount)}</span></div><div><button onClick={() => void copyText(segment.text, `${copy.segmentLabel(segment.index)} · ${copy.copied}`)}>{copy.copySegment}</button><button onClick={() => exportSegment(segment)}>{copy.exportSegment}</button></div></header>
          <pre>{segment.text}</pre>
        </article>)}</div>}
      </section>
    </section>
  );
}
