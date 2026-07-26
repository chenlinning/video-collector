import { describe, expect, it } from "vitest";
import { buildMediaCsv, splitBatchUrls } from "../src/renderer/src/media-export";

describe("media workbench export", () => {
  it("builds an Excel-compatible UTF-8 CSV with escaped public metrics", () => {
	const csv = buildMediaCsv([{
	  sourceUrl: "https://example.com/video",
	  title: "A, \"quoted\" title",
	  uploader: "Creator",
	  extractor: "Example",
	  duration: 12,
	  metrics: { views: 100, likes: 8, comments: 2, reposts: 1 }
	}]);

	expect(csv.startsWith("\uFEFF")).toBe(true);
	expect(csv).toContain('"A, ""quoted"" title"');
	expect(csv).toContain(",100,8,2,1");
  });

  it("normalizes lines into a bounded ten-URL batch", () => {
	expect(splitBatchUrls(" https://a.example/1\n\nhttps://b.example/2 ")).toEqual([
	  "https://a.example/1",
	  "https://b.example/2"
	]);
	expect(() => splitBatchUrls(Array.from({ length: 11 }, (_, index) => `https://example.com/${index}`).join("\n"))).toThrow(/10/);
  });
});
