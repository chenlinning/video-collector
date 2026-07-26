# Video Collector（视频收藏器）

Video Collector 是一个无需登录的公开多媒体解析与临时下载网站。React 前端、Go 后端、yt-dlp、FFmpeg 和离线 Whisper 可以构建为一个 Docker 镜像，部署后由同一域名提供页面和 API。

## 固定生产目标

- 唯一服务器公网 IP：`47.251.87.147`
- 正式网址：`https://video-collector.ximoai.cn`

## 功能

- 无需注册、登录、会员或第三方 SSO。
- 解析 yt-dlp 支持且无需登录的公开视频、纯音频、图片和字幕页面。
- 提供视频、MP3、图片、SRT 和 AI 转录五类处理入口。
- 支持单条、最多 10 条批量，以及公开创作者/播放列表前 10 条解析。
- 显示标题、作者、封面、时长、画质、大小、格式及公开播放/点赞/评论/转发指标。
- 支持画质选择、下载进度、取消和断点续传。
- FFmpeg 自动合并分离的音视频流。
- FFmpeg 提取 MP3；字幕统一转换为 SRT。
- 容器内 whisper.cpp `base` 多语言模型支持 URL 和最大 250 MiB 文件离线转录，最长 60 分钟。
- 批量与集合结果可导出 Excel 可直接打开的 UTF-8 CSV。
- 浏览器流式保存完成文件，避免服务器长期存储。
- 服务器不永久保存视频；未领取文件 30 分钟删除，首次下载请求后 15 分钟删除。
- 深色、浅色两套 Deerflow 风格主题；嵌入 XimoAI 主站时实时跟随主站，独立打开时使用已保存主题。
- 嵌入页面不显示独立顶部导航，临时文件规则在主内容区显著展示。
- 允许通过 iframe 嵌入其它 HTTP/HTTPS 网站，不限制父页面域名。
- IP 限流、全局并发、有界队列、SSRF 防护和严格 JSON 校验。

不支持 DRM、付费、登录或权限受限媒体，也不保证所有平台永久可用。

## 快速验证

前端和 Go 测试：

```powershell
pnpm install --frozen-lockfile
pnpm test
pnpm typecheck
pnpm build:web

$env:GOCACHE = "$PWD\cache\go-build"
$env:GOMODCACHE = "D:\Program Files\VideoCollector-go-mod"
& "D:\Program Files\Go-1.26.5\bin\go.exe" test ./server/...
```

构建并检查容器：

```powershell
Copy-Item .env.example .env
docker compose config
docker compose build
docker compose up -d
Invoke-RestMethod http://127.0.0.1:8787/health
```

完整容器启动后，可运行真实公开样本验收；结果与下载文件只写入项目根 `cache/acceptance`：

```powershell
$env:VIDEO_COLLECTOR_ACCEPTANCE_URL = "http://127.0.0.1:8787"
pnpm test:acceptance
```

本机若 8787 已被占用，可临时修改 Compose 的宿主机端口；生产配置仍固定由 Nginx 代理到 `127.0.0.1:8787`。

## 服务器部署

生产部署包括：

1. 从公开仓库 `https://github.com/chenlinning/video-collector` 拉取部署文件到 `47.251.87.147`。
2. 创建项目根 `cache` 并赋予容器 UID 100、GID 101 写权限。
3. 执行 `docker compose pull && docker compose up -d --no-build`，使用 GitHub Actions 发布的公开 GHCR 镜像。
4. 安装 [Nginx 配置](./deploy/nginx/video-collector.ximoai.cn.conf)。
5. 将 `video-collector.ximoai.cn` 的 A 记录指向 `47.251.87.147`。
6. 配置 HTTPS，并从正式域名完成平台验收。

完整命令、安全配置、监控、更新和回滚流程见 [DEPLOYMENT.md](./DEPLOYMENT.md)。

## 已验证平台

| 平台 | 样本 | 结果 |
|---|---|---|
| AcFun | `https://www.acfun.cn/v/ac48722683` | 3 个格式；MP4 与浏览器下载通过 |
| SoundCloud / NASA | `https://soundcloud.com/nasa/apollo-13-houston-weve-had-a` | 4 个音频格式、10 张图片；MP3、JPG、URL/文件转录通过 |
| SoundCloud 集合 | `https://soundcloud.com/nasa/sets/apollo-sounds` | 公开集合前 10 条通过 |
| TED | `https://www.ted.com/talks/ted_ed_would_you_pass_the_wallet_test` | 11 个格式、29 条字幕；英文 SRT 下载通过 |
| TikTok | 用户提供的公开视频 | 历史生产下载通过；平台可用性取决于服务器出口 |

平台支持由 yt-dlp 提供。旧 Vimeo 样本当前返回 OAuth 401；西瓜视频样本要求 Cookie，因此未作为无登录通过项。

## 目录

```text
server/                 Go HTTP 服务、解析引擎和任务管理
src/renderer/           React 网站
deploy/nginx/           正式域名 Nginx 配置
cache/                  本项目全部构建及运行缓存
dist-web/               Web 生产构建
Dockerfile              前端、后端和媒体工具多阶段镜像
docker-compose.yml      单服务生产编排
```

## 默认运行限制

| 项目 | 默认值 |
|---|---:|
| 同时解析/下载任务 | 2 |
| 排队任务 | 4 |
| 单 IP 解析 | 20 次/15 分钟 |
| 单 IP 创建任务 | 5 次/小时 |
| 单任务硬超时 | 2 小时 |
| yt-dlp 单文件上限 | 2 GiB |
| 转录上传上限 | 250 MiB |
| 转录时长上限 | 60 分钟 |
| 图片响应上限 | 50 MiB |
| 首次下载后保留 | 15 分钟 |
| 未领取文件保留 | 30 分钟 |

可通过 [.env.example](./.env.example) 调整并发、排队和限流。

## 文档

- [生产部署指南](./DEPLOYMENT.md)
- [多媒体工作台功能与验收任务书](./TASK_MEDIA_WORKBENCH_PARITY.md)
- [匿名 Web 服务任务书](./TASK-INDEPENDENT-WEB-SERVICE.md)
- [Web 主题与构建验收](./TASK-WEB-DEPLOYMENT.md)
- [历史桌面端任务书](./TASK-DESKTOP-VIDEO-COLLECTOR.md)

## 使用边界

- 只处理用户拥有权利、已获授权或法律允许保存的公开内容。
- 不读取浏览器 Cookie，不绕过登录、DRM、付费墙或访问控制。
- 不向客户端返回服务器绝对路径或任意命令行能力。
- 平台页面和接口会变化；生产环境应固定 yt-dlp 版本并定期回归测试。
