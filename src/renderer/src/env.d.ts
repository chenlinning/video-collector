/// <reference types="vite/client" />

import type { VideoCollectorApi } from "../../shared/contracts";

declare global {
  interface Window {
    videoCollector?: VideoCollectorApi;
  }
}

export {};
