import { fileURLToPath } from "node:url";
import path from "node:path";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

const projectRoot = path.dirname(fileURLToPath(import.meta.url));

export default defineConfig({
  root: path.join(projectRoot, "src/renderer"),
  base: "./",
  cacheDir: path.join(projectRoot, "cache/vite/web-preview"),
  plugins: [react()],
  build: {
    outDir: path.join(projectRoot, "dist-web"),
    emptyOutDir: true
  },
  server: {
    host: "127.0.0.1",
    port: 4173,
    strictPort: true
  }
});
