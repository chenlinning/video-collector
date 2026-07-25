import { mkdir, readFile, rename, writeFile } from "node:fs/promises";
import path from "node:path";
import type { DownloadHistoryItem } from "../shared/contracts";

const MAX_HISTORY_ITEMS = 100;

function isHistoryItem(value: unknown): value is DownloadHistoryItem {
  if (!value || typeof value !== "object") {
    return false;
  }

  const item = value as Partial<DownloadHistoryItem>;
  return Boolean(
    item.id &&
      item.mediaId &&
      item.title &&
      item.sourceUrl &&
      item.outputPath &&
      item.completedAt
  );
}

export class HistoryStore {
  constructor(private readonly filePath: string) {}

  async list(): Promise<DownloadHistoryItem[]> {
    try {
      const content = await readFile(this.filePath, "utf8");
      const parsed: unknown = JSON.parse(content);
      if (!Array.isArray(parsed)) {
        return [];
      }

      return parsed
        .filter(isHistoryItem)
        .sort((left, right) => right.completedAt.localeCompare(left.completedAt))
        .slice(0, MAX_HISTORY_ITEMS);
    } catch {
      return [];
    }
  }

  async add(item: DownloadHistoryItem): Promise<void> {
    const existing = await this.list();
    const updated = [item, ...existing.filter((entry) => entry.id !== item.id)].slice(
      0,
      MAX_HISTORY_ITEMS
    );
    await this.write(updated);
  }

  async clear(): Promise<void> {
    await this.write([]);
  }

  private async write(items: DownloadHistoryItem[]): Promise<void> {
    const directory = path.dirname(this.filePath);
    const temporaryPath = `${this.filePath}.tmp`;
    await mkdir(directory, { recursive: true });
    await writeFile(temporaryPath, `${JSON.stringify(items, null, 2)}\n`, "utf8");
    await rename(temporaryPath, this.filePath);
  }
}
