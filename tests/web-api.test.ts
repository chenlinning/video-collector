import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createWebVideoCollectorApi, saveWebDownload, streamResponseToWriter } from "../src/renderer/src/web-api";

describe("web video collector API", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("parses media through the anonymous same-origin API", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify({
      id: "media-1",
      sourceUrl: "https://example.com/video",
      title: "Video",
      uploader: "Creator",
      extractor: "Test",
      formats: [{ id: "best", extension: "mp4", hasVideo: true, hasAudio: true }]
    }), { status: 200, headers: { "Content-Type": "application/json" } }));

    const api = createWebVideoCollectorApi();
    const media = await api.parseUrl("https://example.com/video");

    expect(media.id).toBe("media-1");
    expect(fetchMock).toHaveBeenCalledWith("/api/v1/media/parse", expect.objectContaining({
      method: "POST",
      credentials: "same-origin"
    }));
  });

  it("streams a browser download without buffering the whole file", async () => {
    const writes: Uint8Array[] = [];
    const writer = {
      write: vi.fn(async (chunk: Uint8Array) => { writes.push(chunk); }),
      close: vi.fn(async () => undefined),
      abort: vi.fn(async () => undefined)
    };
    const progress: number[] = [];
    const response = new Response(new TextEncoder().encode("video-data"), {
      headers: { "Content-Length": "10" }
    });

    await streamResponseToWriter(response, writer, (value) => progress.push(value));

    expect(new TextDecoder().decode(writes[0])).toBe("video-data");
    expect(writer.close).toHaveBeenCalledOnce();
    expect(progress.at(-1)).toBe(100);
  });

  it("attaches the fallback download link before clicking it", async () => {
    const anchor = {
      href: "",
      download: "",
      click: vi.fn(),
      remove: vi.fn()
    };
    const append = vi.fn();
    vi.stubGlobal("window", {});
    vi.stubGlobal("document", {
      createElement: vi.fn(() => anchor),
      body: { append }
    });

    await saveWebDownload("task-1", "video.mp4");

    expect(append).toHaveBeenCalledWith(anchor);
    expect(anchor.click).toHaveBeenCalledOnce();
    expect(anchor.remove).toHaveBeenCalledOnce();
  });

  it("uses the browser download flow even when a save file picker is available", async () => {
    const picker = vi.fn(async () => ({
      createWritable: vi.fn(async () => ({
        write: vi.fn(async () => undefined),
        close: vi.fn(async () => undefined)
      }))
    }));
    const anchor = {
      href: "",
      download: "",
      click: vi.fn(),
      remove: vi.fn()
    };
    const append = vi.fn();
    vi.stubGlobal("window", { showSaveFilePicker: picker });
    vi.stubGlobal("document", {
      createElement: vi.fn(() => anchor),
      body: { append }
    });
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response("video-data"));

    await saveWebDownload("task-1", "video.mp4");

    expect(picker).not.toHaveBeenCalled();
    expect(fetchMock).not.toHaveBeenCalled();
    expect(append).toHaveBeenCalledWith(anchor);
    expect(anchor.click).toHaveBeenCalledOnce();
  });

  it("emits a cancelled state after cancelling an anonymous task", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(null, { status: 204 }));
    const listener = vi.fn();
    const api = createWebVideoCollectorApi();
    api.onDownloadProgress(listener);

    await api.cancelDownload("task-1");

    expect(listener).toHaveBeenCalledWith(expect.objectContaining({
      taskId: "task-1",
      state: "cancelled"
    }));
  });
});
