import { fileURLToPath } from "node:url";
import path from "node:path";
import react from "@vitejs/plugin-react";
import { defineConfig, externalizeDepsPlugin } from "electron-vite";

const projectRoot = path.dirname(fileURLToPath(import.meta.url));

export default defineConfig({
  main: {
    cacheDir: path.join(projectRoot, "cache/vite/main"),
    plugins: [externalizeDepsPlugin()],
    build: {
      rollupOptions: {
        input: path.join(projectRoot, "src/main/index.ts")
      }
    }
  },
  preload: {
    cacheDir: path.join(projectRoot, "cache/vite/preload"),
    plugins: [externalizeDepsPlugin()],
    build: {
      rollupOptions: {
        input: path.join(projectRoot, "src/preload/index.ts"),
        output: {
          format: "cjs",
          entryFileNames: "index.js"
        }
      }
    }
  },
  renderer: {
    root: path.join(projectRoot, "src/renderer"),
    cacheDir: path.join(projectRoot, "cache/vite/renderer"),
    plugins: [react()],
    build: {
      rollupOptions: {
        input: path.join(projectRoot, "src/renderer/index.html")
      }
    }
  }
});
