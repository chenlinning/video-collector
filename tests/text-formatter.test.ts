import { describe, expect, it } from "vitest";
import {
  countTextCharacters,
  createTextFile,
  formatText,
  joinTextSegments,
  safeTextFileName,
  splitFormattedText
} from "../src/renderer/src/text-formatter";

describe("formatText", () => {
  it("removes Chinese and English punctuation without removing text", () => {
    const result = formatText("你好，world!价格是$5＋2=7🙂");

    expect(result.text).toBe("你好\nworld\n价格是$5＋2=7🙂");
    expect(result.removedPunctuationCount).toBe(2);
    expect(result.characterCount).toBe(17);
  });

  it("collapses consecutive punctuation and existing line breaks to one break", () => {
    const result = formatText("第一行\r\n\r\n第二行，第三行！！！\n第四行。\n");

    expect(result.text).toBe("第一行\n第二行\n第三行\n第四行\n");
    expect(result.removedPunctuationCount).toBe(5);
    expect(result.lineCount).toBe(5);
  });

  it("collapses whitespace-only blank lines while preserving inline whitespace", () => {
    expect(formatText("甲  乙\n \t\n丙").text).toBe("甲  乙\n丙");
    expect(formatText(" \t\n甲\n\t ").text).toBe("\n甲\n");
  });

  it("keeps one break for a punctuation run adjacent to an original break", () => {
    expect(formatText("你好？！……\n世界").text).toBe("你好\n世界");
  });

  it("uses Unicode punctuation categories without deleting math, currency, or emoji", () => {
    const result = formatText("甲_乙—丙（丁）@戊$5＋2=7🙂");

    expect(result.text).toBe("甲\n乙\n丙\n丁\n戊$5＋2=7🙂");
    expect(result.removedPunctuationCount).toBe(5);
  });

  it("normalizes CRLF, CR, and LF consistently without truncating large text", () => {
    expect(formatText("甲\r\n乙\r丙\n丁").text).toBe("甲\n乙\n丙\n丁");
    const large = "长文本，".repeat(100_000);
    const result = formatText(large);
    expect(result.characterCount).toBe(300_000);
    expect(result.removedPunctuationCount).toBe(100_000);
  });
});

describe("countTextCharacters", () => {
  it("does not count whitespace or line breaks", () => {
    expect(countTextCharacters("甲 乙\nA\t1🙂")).toBe(5);
  });
});

describe("splitFormattedText", () => {
  it("keeps totals below or exactly at the limit in one segment", () => {
    expect(splitFormattedText("甲甲\n乙乙", 5).segments).toHaveLength(1);
    expect(splitFormattedText("甲甲\n乙乙", 4).segments).toHaveLength(1);
  });

  it("starts the next segment with the line that would exceed the limit", () => {
    const result = splitFormattedText("甲甲甲甲\n乙乙乙乙\n丙丙丙丙", 10);

    expect(result.issues).toEqual([]);
    expect(result.segments).toEqual([
      { index: 1, text: "甲甲甲甲\n乙乙乙乙", characterCount: 8, lineCount: 2 },
      { index: 2, text: "丙丙丙丙", characterCount: 4, lineCount: 1 }
    ]);
  });

  it("never exceeds the limit or cuts a complete line", () => {
    const text = "第一行\nsecond line\n第三行\nfourth";
    const result = splitFormattedText(text, 11);

    expect(result.issues).toEqual([]);
    expect(result.segments.every((segment) => segment.characterCount <= 11)).toBe(true);
    expect(joinTextSegments(result.segments)).toBe(text);
  });

  it("returns a line-specific issue instead of creating a noncompliant segment", () => {
    const result = splitFormattedText("短行\n这一整行有十个文字", 5);

    expect(result.segments).toEqual([]);
    expect(result.issues).toEqual([{ lineNumber: 2, characterCount: 9, limit: 5 }]);
  });

  it("rejects invalid limits", () => {
    expect(() => splitFormattedText("内容", 0)).toThrow("positive integer");
    expect(() => splitFormattedText("内容", 1.5)).toThrow("positive integer");
  });
});

describe("safeTextFileName", () => {
  it("removes unsafe path characters and the original extension", () => {
    expect(safeTextFileName('章:节/标题?.md')).toBe("章-节-标题-");
  });
});

describe("createTextFile", () => {
  it("keeps the preview text byte-for-byte in a UTF-8 text blob", async () => {
    const text = "第一行\n第二行🙂";
    const file = createTextFile(text);

    expect(file.type).toBe("text/plain;charset=utf-8");
    expect(await file.text()).toBe(text);
    expect(new Uint8Array(await file.arrayBuffer())).toEqual(new TextEncoder().encode(text));
  });
});
