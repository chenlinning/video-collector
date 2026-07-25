import { mkdir, rm } from "node:fs/promises";
import path from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { HistoryStore } from "../src/main/history-store";

const testDirectory = path.resolve("cache/tests/history-store");
const historyPath = path.join(testDirectory, "history.json");

describe("HistoryStore", () => {
  beforeEach(async () => {
    await rm(testDirectory, { recursive: true, force: true });
    await mkdir(testDirectory, { recursive: true });
  });

  afterEach(async () => {
    await rm(testDirectory, { recursive: true, force: true });
  });

  it("persists successful downloads and reads them newest first", async () => {
    const store = new HistoryStore(historyPath);

    await store.add({
      id: "task-1",
      mediaId: "video-1",
      title: "第一条",
      sourceUrl: "https://example.com/1",
      outputPath: "D:\\Videos\\1.mp4",
      completedAt: "2026-07-25T08:00:00.000Z"
    });
    await store.add({
      id: "task-2",
      mediaId: "video-2",
      title: "第二条",
      sourceUrl: "https://example.com/2",
      outputPath: "D:\\Videos\\2.mp4",
      completedAt: "2026-07-25T09:00:00.000Z"
    });

    await expect(new HistoryStore(historyPath).list()).resolves.toMatchObject([
      { id: "task-2", title: "第二条" },
      { id: "task-1", title: "第一条" }
    ]);
  });

  it("returns an empty list for missing or invalid files", async () => {
    const store = new HistoryStore(historyPath);
    await expect(store.list()).resolves.toEqual([]);
  });
});
