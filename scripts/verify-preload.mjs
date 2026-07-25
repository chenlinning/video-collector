import { readFile } from "node:fs/promises";
import path from "node:path";

const projectRoot = path.resolve(import.meta.dirname, "..");
const preloadPath = path.join(projectRoot, "out", "preload", "index.js");

let source;
try {
  source = await readFile(preloadPath, "utf8");
} catch {
  throw new Error(`Expected CommonJS preload output is missing: ${preloadPath}`);
}

if (/^\s*import\s/m.test(source)) {
  throw new Error("Preload output contains ESM imports and cannot run in the Electron sandbox");
}

if (!source.includes("contextBridge") || !source.includes("videoCollector")) {
  throw new Error("Preload output does not expose the Video Collector desktop API");
}

process.stdout.write("PRELOAD_BUILD_OK\n");
