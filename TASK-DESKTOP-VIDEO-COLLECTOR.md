# Video Collector 桌面端实施与验收任务书

> 文档状态：0.1.1 修复版实施完成，验收通过
> 创建日期：2026-07-25
> 目标平台：Windows 10/11 x64
> 实施原则：先测试、后实现；所有阶段必须留下可复现的验收证据

## 1. 背景与可行性结论

目标是制作一个本地运行的视频收藏工具，提供类似在线视频解析站点的“粘贴链接、解析格式、选择画质、下载到本机”体验，但不调用第三方站点的私有接口，也不建设云端解析服务。

给定 TikTok 样本已经完成真实可行性测试：

- URL：`https://www.tiktok.com/@wowohpanda/video/7576493197174541588`
- 无登录、无浏览器 Cookie 即可解析。
- 解析器识别为 TikTok，共返回 3 个 MP4 候选格式。
- 最佳格式为 576×1280、H.264 High + HE-AACv2、时长 209.2 秒。
- 下载文件大小为 15,217,445 字节。
- FFmpeg 全文件解码通过，无损坏。
- 测试文件 SHA-256：`8165BBE71146F9CD633F89787AFBAAE8912C1E89CA51DCE8445D0FBB2BABF8C3`。
- 证据目录：`cache/tiktok-feasibility/`。

结论：Electron 本地桌面端方案可行。纯静态网站会受到跨域、平台反爬和音视频合并能力限制，不作为本任务的主交付形态。

## 2. 产品目标

交付一个名为 **Video Collector（视频收藏器）** 的 Windows 桌面软件，满足以下核心流程：

1. 用户粘贴公开的视频页面 URL。
2. 软件在本地解析标题、作者、封面、时长和格式列表。
3. 用户选择画质及保存目录。
4. 软件显示实时下载进度、速度和预计剩余时间。
5. 下载完成后可打开文件或所在目录，并保存本地历史记录。

## 3. 范围与边界

### 3.1 本期范围

- Windows x64 桌面端。
- 中文界面。
- 公共 HTTP/HTTPS 视频页面解析。
- TikTok 样本的端到端支持。
- 基于 yt-dlp 能力提供其他公开平台的尽力支持。
- MP4 直下、音视频合并和 HLS 下载。
- 下载进度、取消、重试和断点续传。
- 本地历史记录。
- Windows 便携版构建产物。

### 3.2 明确不做

- 不绕过 DRM、付费墙、登录限制或访问控制。
- 不默认读取浏览器 Cookie。
- 不复制 DataTool 的商标、视觉资产或私有 API。
- 不承诺支持“任何网站”。
- 不建设账号、会员、云同步或远程解析服务。

## 4. 技术方案

### 4.1 技术栈

- Electron：桌面容器和系统能力。
- React + Vite + TypeScript：渲染进程界面。
- yt-dlp：本地媒体信息提取与下载。
- FFmpeg / ffprobe：音视频合并、转封装和验收。
- Vitest：核心逻辑测试。
- electron-builder：Windows 便携版打包。

### 4.2 运行架构

```text
React 渲染进程
    │ 受限 IPC
    ▼
Electron preload
    │ 白名单 API
    ▼
Electron 主进程
    ├─ URL 安全校验
    ├─ yt-dlp 元数据解析
    ├─ yt-dlp 下载任务
    ├─ FFmpeg 合并/转封装
    └─ 本地历史记录与文件系统
```

### 4.3 安全要求

- `contextIsolation: true`。
- `nodeIntegration: false`。
- `sandbox: true`。
- 渲染进程只能通过 preload 暴露的白名单 API 调用主进程。
- 只接受 `http:` 和 `https:` URL。
- 拒绝 localhost、回环地址、链路本地地址和常见私网地址。
- 启动 yt-dlp/FFmpeg 时必须使用参数数组及 `shell: false`，禁止字符串拼接命令。
- 下载目录必须是绝对路径。
- 解析结果与子进程错误必须规范化，不能向界面泄露无关系统信息。

### 4.4 本机依赖位置

- Node.js：`D:\Program Files\nodejs\node.exe`
- pnpm：`D:\Program Files\pnpm-10.26.2\node_modules\.bin\pnpm.cmd`
- yt-dlp：`D:\Program Files\yt-dlp\yt-dlp.exe`
- FFmpeg：`D:\Program Files\ffmpeg\bin\ffmpeg.exe`
- ffprobe：`D:\Program Files\ffmpeg\bin\ffprobe.exe`
- 前端依赖物理存储：`D:\Program Files\VideoCollector-dependencies\`

### 4.5 数据位置

- 项目缓存：项目根目录 `cache/`。
- 开发下载目录：项目根目录 `downloads/`。
- 构建产物：项目根目录 `release/`。
- 不使用系统临时目录存放项目缓存。
- 便携版运行时在程序所在目录旁创建 `cache/` 和 `downloads/`。

## 5. 数据契约

### 5.1 解析结果

```ts
interface MediaInfo {
  id: string;
  sourceUrl: string;
  title: string;
  uploader: string;
  thumbnail?: string;
  duration?: number;
  extractor: string;
  formats: MediaFormat[];
}

interface MediaFormat {
  id: string;
  label: string;
  extension: string;
  width?: number;
  height?: number;
  videoCodec?: string;
  audioCodec?: string;
  approximateBytes?: number;
  hasVideo: boolean;
  hasAudio: boolean;
}
```

### 5.2 下载事件

```ts
interface DownloadProgress {
  taskId: string;
  state: "starting" | "downloading" | "processing" | "completed" | "cancelled" | "failed";
  percent: number;
  speed?: string;
  eta?: string;
  outputPath?: string;
  error?: string;
}
```

## 6. 分阶段实施任务

### 阶段 A：测试与项目骨架

- [x] A1 创建 Electron/Vite/React/TypeScript 配置。
- [x] A2 先编写 URL 校验测试。
- [x] A3 先编写 yt-dlp 元数据规范化测试。
- [x] A4 先编写下载进度解析测试。
- [x] A5 运行测试并确认实现前按预期失败。

### 阶段 B：主进程能力

- [x] B1 实现 URL 安全校验。
- [x] B2 实现依赖路径解析与启动前检查。
- [x] B3 实现 yt-dlp 元数据解析。
- [x] B4 实现格式规范化与排序。
- [x] B5 实现下载任务、进度事件、取消和断点续传。
- [x] B6 实现下载历史持久化。
- [x] B7 使阶段 A 测试通过。

### 阶段 C：桌面界面

- [x] C1 实现安全的 Electron 主窗口和 preload IPC。
- [x] C2 实现 URL 输入、粘贴和解析状态。
- [x] C3 实现媒体信息及格式选择界面。
- [x] C4 实现目录选择、下载进度、取消和重试。
- [x] C5 实现下载历史、打开文件和打开目录。
- [x] C6 实现空状态、错误状态和响应式布局。

### 阶段 D：构建与验收

- [x] D1 TypeScript 类型检查通过。
- [x] D2 单元测试全部通过。
- [x] D3 Electron 生产构建通过。
- [x] D4 使用给定 TikTok URL 完成真实解析验收。
- [x] D5 使用给定 TikTok URL 完成真实下载与 ffprobe 验收。
- [x] D6 Windows 便携版打包成功。
- [x] D7 回填本任务书的验收证据和最终状态。

## 7. 验收标准

| 编号 | 验收项 | 通过条件 | 状态 | 证据 |
|---|---|---|---|---|
| AC-01 | 应用启动 | Windows 桌面窗口可正常打开，无主进程错误 | 通过 | 开发与打包应用均通过桌面 API 冒烟测试并返回真实依赖版本 |
| AC-02 | URL 安全校验 | 接受公共 HTTPS URL；拒绝 file、localhost、回环及私网 URL | 通过 | `tests/url-policy.test.ts`，覆盖 14 个有效/无效场景 |
| AC-03 | TikTok 解析 | 给定样本显示正确 ID、标题、作者、时长和至少一个格式 | 通过 | `cache/integration/report.json`：ID 正确、TikTok、3 个格式 |
| AC-04 | 格式信息 | 显示 576×1280、H.264、AAC 格式 | 通过 | 集成报告与 `cache/tiktok-feasibility/*.info.json` |
| AC-05 | 下载 | 给定样本可下载为可播放 MP4 | 通过 | `cache/integration/7576493197174541588.mp4`，15,217,445 字节 |
| AC-06 | 媒体完整性 | ffprobe 可读取音视频流，FFmpeg 全文件解码无错误 | 通过 | 集成脚本同时执行 ffprobe 与 `ffmpeg -xerror` 全解码 |
| AC-07 | 下载状态 | 界面显示进度、速度、ETA 和完成状态 | 通过 | `tests/progress-parser.test.ts` 与浏览器桌面/响应式界面验收 |
| AC-08 | 取消与恢复 | 活动任务可取消；相同 URL 可重新开始并利用 `.part` 续传 | 通过 | `media-engine-cancel.integration.ts`；下载参数测试确认 `--continue` |
| AC-09 | 历史记录 | 下载成功后写入本地历史，重启后仍能读取 | 通过 | `tests/history-store.test.ts` 原子写入与重新实例化读取测试 |
| AC-10 | Electron 安全 | 隔离开启、Node 集成关闭、IPC 白名单化 | 通过 | CommonJS preload 构建检查、桌面 API 注入断言和打包冒烟测试 |
| AC-11 | 缓存约束 | 项目运行产生的缓存只位于根目录 `cache/` | 通过 | Vite/Vitest/Electron 用户数据、会话、磁盘缓存、日志和崩溃转储均定向 `cache/` |
| AC-12 | 便携版 | `release/` 中生成可执行的 Windows x64 便携版 | 通过 | `release/Video-Collector-0.1.1-Windows-x64.exe`，桌面 API 调用通过 |

## 8. 验收命令

实现后以下命令必须可重复执行：

```powershell
pnpm test
pnpm typecheck
pnpm build
pnpm test:integration
pnpm package:win
```

真实媒体验收使用用户提供的 TikTok URL，不使用登录态或浏览器 Cookie。测试文件只用于当前技术验收与个人收藏，保存在项目缓存或下载目录中。

## 9. 风险与处理

| 风险 | 处理方式 |
|---|---|
| 平台页面结构变化 | 将 yt-dlp 作为独立可更新组件；界面展示清晰错误信息 |
| 媒体 CDN 地址短期有效 | 每次下载开始前重新解析，不持久化媒体直链 |
| 音视频分离 | 使用 FFmpeg 自动合并 |
| 杀毒软件误报未签名 Electron 包 | 首版提供透明的便携版、版本信息和 SHA-256；后续再加入代码签名 |
| 应用体积较大 | 首版优先可靠性；后续可评估 Tauri |
| 平台条款或版权限制 | 只支持用户有权保存的公开内容，不绕过任何访问控制 |

## 10. 完成定义

只有在以下条件全部满足后，本任务才可标记完成：

1. 阶段 A–D 的任务全部勾选。
2. AC-01 至 AC-12 均有明确结果和可复现证据。
3. 单元测试、类型检查、生产构建和真实 TikTok 集成测试全部通过。
4. `release/` 中存在 Windows 便携版及 SHA-256 文件。
5. README 包含安装、使用、隐私、限制和故障排查说明。

## 11. 最终验收记录

- 单元测试：5 个测试文件、23 项测试，全部通过。
- 取消集成测试：1 个测试文件、1 项真实子进程取消测试，通过。
- TypeScript：`tsc --noEmit` 通过。
- Electron：主进程、preload、renderer 生产构建通过。
- UI：桌面默认尺寸与窄窗口响应式布局验收通过。
- TikTok：真实解析、下载、ffprobe 和 FFmpeg 全文件解码通过。
- 样本媒体 SHA-256：`8165bbe71146f9cd633f89787afbaae8912c1e89ca51dce8445d0fbb2babf8c3`。
- Windows 便携版大小：150,736,568 字节。
- Windows 便携版 SHA-256：`2740458568b9eec32bc7f9e0413d014b14147843672a269cc8bf594bdd0e456a`。
- 便携版已确认内置 yt-dlp、FFmpeg、ffprobe，打包后启动测试通过。

## 12. 0.1.1 回归修复记录

- 发现方式：用户运行 0.1.0 便携版后界面仍显示“浏览器预览”。
- 根因：沙箱模式下 preload 产物为 ESM `index.mjs`，Electron 按普通 preload 加载时抛出 `Cannot use import statement outside a module`。
- 修复：preload 强制构建为 CommonJS `out/preload/index.js`，主进程同步修改加载路径。
- 防回归：生产构建增加 `scripts/verify-preload.mjs`；桌面冒烟测试必须通过 preload 调用 `getRuntimeStatus()` 并取得真实版本号。
- 附加修复：Electron 用户数据、会话、磁盘缓存、日志和崩溃转储全部写入项目/便携版旁的 `cache`。
- 验收：打包版返回 `yt-dlp 2026.07.04`、`FFmpeg 8.1.2`，preload 错误 0 条、缓存错误 0 条。
- 旧的 0.1.0 缺陷包及哈希已删除，发布目录只保留 0.1.1。
