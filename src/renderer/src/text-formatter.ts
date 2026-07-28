const punctuationPattern = /\p{P}/u;
const punctuationRunPattern = /\p{P}+/gu;
const blankLineContentPattern = /(^|\n)[\t\f\v ]+(?=\n|$)/g;
const repeatedBreakPattern = /\n(?:[\t\f\v ]*\n)+/g;
const whitespacePattern = /\s/u;

export interface TextFormatResult {
  text: string;
  removedPunctuationCount: number;
  characterCount: number;
  lineCount: number;
}

export interface TextSegment {
  index: number;
  text: string;
  characterCount: number;
  lineCount: number;
}

export interface TextSegmentIssue {
  lineNumber: number;
  characterCount: number;
  limit: number;
}

export interface TextSegmentationResult {
  segments: TextSegment[];
  issues: TextSegmentIssue[];
}

export function countTextCharacters(value: string): number {
  let count = 0;
  for (const character of value) {
    if (!whitespacePattern.test(character)) count += 1;
  }
  return count;
}

export function formatText(source: string): TextFormatResult {
  const normalized = source.replace(/\r\n?/g, "\n");
  let removedPunctuationCount = 0;
  for (const character of normalized) {
    if (punctuationPattern.test(character)) removedPunctuationCount += 1;
  }
  const text = normalized
    .replace(punctuationRunPattern, "\n")
    .replace(blankLineContentPattern, "$1")
    .replace(repeatedBreakPattern, "\n");

  return {
    text,
    removedPunctuationCount,
    characterCount: countTextCharacters(text),
    lineCount: text === "" ? 0 : text.split("\n").length
  };
}

export function splitFormattedText(text: string, limit: number): TextSegmentationResult {
  if (!Number.isSafeInteger(limit) || limit <= 0) {
    throw new Error("Text segment limit must be a positive integer");
  }
  if (countTextCharacters(text) === 0) return { segments: [], issues: [] };

  const lines = text.split("\n");
  const issues: TextSegmentIssue[] = [];
  lines.forEach((line, index) => {
    const characterCount = countTextCharacters(line);
    if (characterCount > limit) {
      issues.push({ lineNumber: index + 1, characterCount, limit });
    }
  });
  if (issues.length > 0) return { segments: [], issues };

  const segments: TextSegment[] = [];
  let currentLines: string[] = [];
  let currentCount = 0;

  const finishCurrent = () => {
    if (currentLines.length === 0) return;
    segments.push({
      index: segments.length + 1,
      text: currentLines.join("\n"),
      characterCount: currentCount,
      lineCount: currentLines.length
    });
    currentLines = [];
    currentCount = 0;
  };

  lines.forEach((line) => {
    const lineCount = countTextCharacters(line);
    if (currentLines.length > 0 && currentCount + lineCount > limit) {
      finishCurrent();
    }
    currentLines.push(line);
    currentCount += lineCount;
  });
  finishCurrent();

  return { segments, issues: [] };
}

export function joinTextSegments(segments: readonly TextSegment[]): string {
  return segments.map((segment) => segment.text).join("\n");
}

export function resolveTextSegment(segments: readonly TextSegment[], selectedIndex: number | null): TextSegment | null {
  return segments.find((segment) => segment.index === selectedIndex) ?? segments[0] ?? null;
}

export function safeTextFileName(value: string, fallback = "formatted-text"): string {
  const stem = value
    .replace(/\.[^.]+$/, "")
    .replace(/[<>:"/\\|?*\u0000-\u001f]/g, "-")
    .replace(/[. ]+$/g, "")
    .trim();
  return stem || fallback;
}

export function createTextFile(text: string): Blob {
  return new Blob([text], { type: "text/plain;charset=utf-8" });
}

export function downloadText(text: string, fileName: string): void {
  const blob = createTextFile(text);
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = fileName;
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}
