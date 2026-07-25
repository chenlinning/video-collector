import { fileURLToPath } from "node:url";
import { mkdirSync } from "node:fs";
import path from "node:path";
import { app, BrowserWindow, dialog, ipcMain, shell } from "electron";
import type { DownloadRequest } from "../shared/contracts";
import { MediaEngine } from "./media-engine";
import { resolveRuntimePaths } from "./runtime-paths";

const currentDirectory = path.dirname(fileURLToPath(import.meta.url));
const runtimePaths = resolveRuntimePaths({
  isPackaged: app.isPackaged,
  appPath: app.getAppPath(),
  executablePath: process.execPath,
  resourcesPath: process.resourcesPath,
  portableExecutableDirectory: process.env.PORTABLE_EXECUTABLE_DIR
});
const electronDataPaths = {
  userData: path.join(runtimePaths.cacheDirectory, "electron-user-data"),
  sessionData: path.join(runtimePaths.cacheDirectory, "electron-session"),
  diskCache: path.join(runtimePaths.cacheDirectory, "chromium-cache"),
  logs: path.join(runtimePaths.cacheDirectory, "logs"),
  crashDumps: path.join(runtimePaths.cacheDirectory, "crash-dumps")
};

for (const directory of Object.values(electronDataPaths)) {
  mkdirSync(directory, { recursive: true });
}
app.setPath("userData", electronDataPaths.userData);
app.setPath("sessionData", electronDataPaths.sessionData);
app.setPath("logs", electronDataPaths.logs);
app.setPath("crashDumps", electronDataPaths.crashDumps);
app.commandLine.appendSwitch("disk-cache-dir", electronDataPaths.diskCache);

let mainWindow: BrowserWindow | null = null;
let mediaEngine: MediaEngine;

function requireAbsolutePath(value: unknown): string {
  if (typeof value !== "string" || !path.isAbsolute(value)) {
    throw new Error("无效的本地文件路径");
  }
  return value;
}

function registerIpcHandlers(): void {
  ipcMain.handle("media:parse", (_event, url: string) => mediaEngine.parseUrl(url));
  ipcMain.handle("runtime:status", () => mediaEngine.getRuntimeStatus());
  ipcMain.handle("download:start", (_event, request: DownloadRequest) =>
    mediaEngine.startDownload(request)
  );
  ipcMain.handle("download:cancel", (_event, taskId: string) =>
    mediaEngine.cancelDownload(taskId)
  );
  ipcMain.handle("history:list", () => mediaEngine.listHistory());
  ipcMain.handle("history:clear", () => mediaEngine.clearHistory());
  ipcMain.handle("directory:choose", async () => {
    const options: Electron.OpenDialogOptions = {
      title: "选择视频保存目录",
      properties: ["openDirectory", "createDirectory"]
    };
    const result = mainWindow
      ? await dialog.showOpenDialog(mainWindow, options)
      : await dialog.showOpenDialog(options);
    return result.canceled ? null : result.filePaths[0] ?? null;
  });
  ipcMain.handle("path:open", async (_event, targetPath: unknown) => {
    const error = await shell.openPath(requireAbsolutePath(targetPath));
    return !error;
  });
  ipcMain.handle("path:show", (_event, targetPath: unknown) => {
    shell.showItemInFolder(requireAbsolutePath(targetPath));
    return true;
  });
}

function createMainWindow(): BrowserWindow {
  const window = new BrowserWindow({
    width: 1280,
    height: 820,
    minWidth: 980,
    minHeight: 680,
    show: false,
    autoHideMenuBar: true,
    backgroundColor: "#080b14",
    title: "Video Collector",
    webPreferences: {
      preload: path.join(currentDirectory, "../preload/index.js"),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true
    }
  });

  window.once("ready-to-show", () => {
    if (process.env.VIDEO_COLLECTOR_SMOKE_TEST !== "1") {
      window.show();
    }
  });
  if (process.env.VIDEO_COLLECTOR_SMOKE_TEST === "1") {
    window.webContents.once("did-finish-load", async () => {
      try {
        const status = await window.webContents.executeJavaScript(`
          (async () => {
            if (!window.videoCollector || typeof window.videoCollector.getRuntimeStatus !== "function") {
              throw new Error("Desktop preload API is missing");
            }
            return window.videoCollector.getRuntimeStatus();
          })()
        `);
        process.stdout.write(`VIDEO_COLLECTOR_SMOKE_OK ${JSON.stringify(status)}\n`);
        app.exit(0);
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error);
        process.stderr.write(`VIDEO_COLLECTOR_SMOKE_FAILED ${message}\n`);
        app.exit(1);
      }
    });
  }
  window.webContents.setWindowOpenHandler(({ url }) => {
    if (url.startsWith("https://") || url.startsWith("http://")) {
      void shell.openExternal(url);
    }
    return { action: "deny" };
  });
  window.webContents.on("will-navigate", (event) => event.preventDefault());

  if (process.env.ELECTRON_RENDERER_URL) {
    void window.loadURL(process.env.ELECTRON_RENDERER_URL);
  } else {
    void window.loadFile(path.join(currentDirectory, "../renderer/index.html"));
  }

  return window;
}

async function bootstrap(): Promise<void> {
  mediaEngine = new MediaEngine(runtimePaths, (progress) => {
    mainWindow?.webContents.send("download:progress", progress);
  });
  await mediaEngine.initialize();
  registerIpcHandlers();
  mainWindow = createMainWindow();
  mainWindow.on("closed", () => {
    mainWindow = null;
  });
}

app.setAppUserModelId("com.videocollector.desktop");
app.whenReady().then(bootstrap).catch((error: unknown) => {
  const message = error instanceof Error ? error.message : String(error);
  dialog.showErrorBox("Video Collector 启动失败", message);
  app.quit();
});

app.on("activate", () => {
  if (BrowserWindow.getAllWindows().length === 0 && mediaEngine) {
    mainWindow = createMainWindow();
  }
});

app.on("window-all-closed", () => {
  if (process.platform !== "darwin") {
    app.quit();
  }
});
