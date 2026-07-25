# Video Collector（视频收藏器）

Video Collector 是一个无需登录的公开视频解析与临时下载网站。React 前端、Go 后端、yt-dlp 和 FFmpeg 可以构建为一个 Docker 镜像，部署后由同一域名提供页面和 API。

## 固定生产目标

- 唯一服务器公网 IP：`47.251.87.147`
- 正式网址：`https://video-collector.ximoai.cn`

## 功能

- 无需注册、登录、会员或第三方 SSO。
- 解析 yt-dlp 支持且无需登录的公开视频页面。
- 显示标题、作者、封面、时长、画质、大小和格式。
- 支持画质选择、下载进度、取消和断点续传。
- FFmpeg 自动合并分离的音视频流。
- 浏览器流式保存完成文件，避免服务器长期存储。
- 首次领取后 10 分钟删除；未领取文件 30 分钟后删除。
- 深色、浅色两套 Deerflow 风格主题，可切换并持久化。
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
& "D:\Program Files\Go-1.26.5\bin\go.exe" test ./...
```

构建并检查容器：

```powershell
Copy-Item .env.example .env
docker compose config
docker compose build
docker compose up -d
Invoke-RestMethod http://127.0.0.1:8787/health
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
| Bilibili | `https://www.bilibili.com/video/BV1ex4y1P7Xm/` | 解析 7 个格式；真实下载和音视频合并通过 |
| AcFun | `https://www.acfun.cn/v/ac48722683` | 解析 3 个格式；容器真实下载通过 |
| TikTok | 用户提供的公开视频 | 历史可行性通过；当前本机出口超时，需在生产服务器复测 |

平台支持由 yt-dlp 提供。Vimeo、Dailymotion 在当前网络环境连接超时；西瓜视频样本要求 Cookie，因此未作为无登录通过项。

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
| 单任务硬超时 | 30 分钟 |
| yt-dlp 单文件上限 | 2 GiB |
| 首次下载后保留 | 10 分钟 |
| 未领取文件保留 | 30 分钟 |

可通过 [.env.example](./.env.example) 调整并发、排队和限流。

## 文档

- [生产部署指南](./DEPLOYMENT.md)
- [匿名 Web 服务任务书](./TASK-INDEPENDENT-WEB-SERVICE.md)
- [Web 主题与构建验收](./TASK-WEB-DEPLOYMENT.md)
- [历史桌面端任务书](./TASK-DESKTOP-VIDEO-COLLECTOR.md)

## 使用边界

- 只处理用户拥有权利、已获授权或法律允许保存的公开内容。
- 不读取浏览器 Cookie，不绕过登录、DRM、付费墙或访问控制。
- 不向客户端返回服务器绝对路径或任意命令行能力。
- 平台页面和接口会变化；生产环境应固定 yt-dlp 版本并定期回归测试。
