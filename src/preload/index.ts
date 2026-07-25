import { contextBridge, ipcRenderer } from "electron";
import type {
  DownloadProgress,
  DownloadRequest,
  VideoCollectorApi
} from "../shared/contracts";

const api: VideoCollectorApi = {
  parseUrl: (url) => ipcRenderer.invoke("media:parse", url),
  chooseDirectory: () => ipcRenderer.invoke("directory:choose"),
  getRuntimeStatus: () => ipcRenderer.invoke("runtime:status"),
  startDownload: (request: DownloadRequest) => ipcRenderer.invoke("download:start", request),
  cancelDownload: (taskId) => ipcRenderer.invoke("download:cancel", taskId),
  listHistory: () => ipcRenderer.invoke("history:list"),
  clearHistory: () => ipcRenderer.invoke("history:clear"),
  openPath: (targetPath) => ipcRenderer.invoke("path:open", targetPath),
  showInFolder: (targetPath) => ipcRenderer.invoke("path:show", targetPath),
  onDownloadProgress: (listener) => {
    const handler = (_event: Electron.IpcRendererEvent, progress: DownloadProgress) =>
      listener(progress);
    ipcRenderer.on("download:progress", handler);
    return () => ipcRenderer.removeListener("download:progress", handler);
  }
};

contextBridge.exposeInMainWorld("videoCollector", api);
