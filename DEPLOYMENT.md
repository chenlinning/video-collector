# Video Collector 生产部署指南

> 更新日期：2026-07-26
> 服务形态：无需登录的匿名公开视频解析与临时下载网站
> 当前状态：已部署至唯一生产服务器，HTTPS、核心平台与浏览器下载验收通过

## 1. 固定生产目标

- 唯一授权服务器公网 IPv4：`47.251.87.147`
- 唯一正式网址：`https://video-collector.ximoai.cn`
- DNS A 记录必须为：`video-collector.ximoai.cn → 47.251.87.147`
- 未经用户明确修改，不得部署到其他服务器或使用其他正式域名。

## 2. 部署后提供的能力

同一个 Docker 容器提供：

- React/Vite 网站。
- Go 匿名 HTTP API。
- yt-dlp 多平台解析和下载。
- FFmpeg 音视频合并、MP3 提取和字幕转换。
- whisper.cpp `base` 多语言模型离线转录 URL 或上传文件。
- 图片下载、字幕 SRT、公开指标、批量/集合解析和 UTF-8 CSV 导出。
- 下载进度、取消、Range 下载和文件 TTL。
- IP 限流、全局并发、有界队列和 URL 安全检查。

不包含账户、注册、登录、会员、JWT、API Key 或第三方 SSO。

```text
浏览器
  │ HTTPS
  ▼
Nginx :443
  │ http://127.0.0.1:8787
  ▼
video-collector 容器
  ├── 静态前端
  ├── 匿名 API
  ├── yt-dlp
  ├── FFmpeg
  ├── whisper-cli + ggml-base.bin
  └── /app/cache
```

## 3. 服务器要求

建议：

- Ubuntu LTS 或 Debian stable x64。
- 2 vCPU、4 GiB 内存起步。
- 至少 30 GiB 可用磁盘；高并发或大文件应提高容量。
- Docker Engine、Docker Compose Plugin、Nginx、curl。
- 入站只开放 TCP 22、80、443。
- 容器端口 8787 只绑定 `127.0.0.1`。
- 允许服务器向公开视频页面和媒体 CDN 发起出站 HTTPS。

本项目已验证：

| 组件 | 版本 |
|---|---:|
| Docker Engine | 29.6.2 |
| Docker Compose | 5.3.1 |
| Go 构建镜像 | 1.26.5-alpine |
| Node 构建镜像 | 22-alpine |
| yt-dlp | 2026.07.04 |
| 容器 FFmpeg | 6.1.2 |
| whisper.cpp | 1.8.6 |
| Whisper 模型 | `ggml-base.bin`（多语言，官方 SHA-1 已校验） |

当前完整镜像约 240 MB，以 UID 100、GID 101 的 `collector` 用户运行。Whisper 为 CPU 推理；增加转录并发前应先测量 CPU 和内存。

## 4. 上传项目

服务器目录固定为：

```text
/opt/video-collector/
├── Dockerfile
├── docker-compose.yml
├── .env
├── cache/
├── deploy/
├── server/
├── src/
├── package.json
├── pnpm-lock.yaml
├── go.mod
└── go.sum
```

创建目录：

```bash
sudo install -d -m 0755 /opt/video-collector
sudo chown "$USER":"$USER" /opt/video-collector
```

从公开仓库拉取部署文件。不要上传本机的 `node_modules`、`release`、`out`、测试媒体或旧缓存：

```bash
git clone https://github.com/chenlinning/video-collector.git /opt/video-collector
```

## 5. 环境配置

在服务器执行：

```bash
cd /opt/video-collector
cp .env.example .env
```

默认配置：

```dotenv
VIDEO_COLLECTOR_IMAGE=ghcr.io/chenlinning/video-collector:latest
VIDEO_COLLECTOR_MAX_CONCURRENT=2
VIDEO_COLLECTOR_MAX_QUEUED=4
VIDEO_COLLECTOR_PARSE_RATE_LIMIT=20
VIDEO_COLLECTOR_TASK_RATE_LIMIT=5
VIDEO_COLLECTOR_TASK_TIMEOUT_SECONDS=7200
VIDEO_COLLECTOR_TRUST_PROXY=true
VIDEO_COLLECTOR_EGRESS_MODE=off
VIDEO_COLLECTOR_CN_PROXY_URL=
VIDEO_COLLECTOR_CN_PROXY_SOURCE_HOSTS=
VIDEO_COLLECTOR_CN_PROXY_ROUTE_TTL_SECONDS=1800
VIDEO_COLLECTOR_CN_PROXY_CONNECT_TIMEOUT_SECONDS=5
VIDEO_COLLECTOR_CN_PROXY_BREAKER_FAILURES=3
VIDEO_COLLECTOR_CN_PROXY_BREAKER_SECONDS=60
```

变量含义：

| 变量 | 默认值 | 说明 |
|---|---:|---|
| `VIDEO_COLLECTOR_IMAGE` | `ghcr.io/chenlinning/video-collector:latest` | GitHub Actions 发布的生产镜像；可改为 `sha-...` 标签以固定版本 |
| `VIDEO_COLLECTOR_MAX_CONCURRENT` | 2 | 同时执行的解析/下载数量，允许 1–8 |
| `VIDEO_COLLECTOR_MAX_QUEUED` | 4 | 等待任务数量，允许 1–32 |
| `VIDEO_COLLECTOR_PARSE_RATE_LIMIT` | 20 | 每个 IP 在 15 分钟内的解析次数 |
| `VIDEO_COLLECTOR_TASK_RATE_LIMIT` | 5 | 每个 IP 在 1 小时内创建任务次数 |
| `VIDEO_COLLECTOR_TASK_TIMEOUT_SECONDS` | 7200 | 单任务硬超时，允许 60–7200 秒；为最长 60 分钟 CPU 转录预留处理时间 |
| `VIDEO_COLLECTOR_TRUST_PROXY` | true | 信任唯一前置 Nginx 的真实 IP 请求头 |
| `VIDEO_COLLECTOR_EGRESS_MODE` | `off` | 国内临时出口模式；只允许 `off` 或 `auto` |
| `VIDEO_COLLECTOR_CN_PROXY_URL` | 空 | `auto` 时必填；只接受固定私有 IPv4 WireGuard 地址上的 `http://` 代理，不允许凭据、路径、查询或片段 |
| `VIDEO_COLLECTOR_CN_PROXY_SOURCE_HOSTS` | 空 | `auto` 时必填；逗号分隔的受控源页面主机，基础域名同时匹配其子域名 |
| `VIDEO_COLLECTOR_CN_PROXY_ROUTE_TTL_SECONDS` | 1800 | 国内出口成功后的主机路由缓存，允许 60–3600 秒 |
| `VIDEO_COLLECTOR_CN_PROXY_CONNECT_TIMEOUT_SECONDS` | 5 | 国内代理连接超时，允许 1–15 秒 |
| `VIDEO_COLLECTOR_CN_PROXY_BREAKER_FAILURES` | 3 | 连续代理连接失败后熔断，允许 1–10 次 |
| `VIDEO_COLLECTOR_CN_PROXY_BREAKER_SECONDS` | 60 | 熔断时间，允许 10–600 秒 |

匿名版没有必填密钥。`.env` 不应提交到公开仓库。

### 5.1 国内临时出口（可选）

国内服务器只作为临时网络出口，不是第二套生产环境。浏览器、Nginx、Go API、任务队列、yt-dlp、FFmpeg、Whisper 和全部媒体临时文件仍只在 `47.251.87.147`。国内服务器不得克隆本仓库、运行项目容器、挂载项目缓存或保存媒体文件。

架构：

```text
47.251.87.147 上的项目容器
  ├── 默认：直接访问公开平台
  └── 受控主机且出现地域/IP/网络类失败
        └── 10.77.0.1 → WireGuard → 10.77.0.2:3128 → 公开平台
```

实施前必须确认：

- 国内服务器公网 IPv4、Linux 发行版和 CPU 架构。
- SSH 管理方式、云安全组、UDP 端口可用性和本机防火墙。
- 公网带宽、月流量、计费规则及云服务商是否允许作为私有服务器间出口。
- 两个无需登录、Cookie 或 DRM 的公开国内平台验收链接。
- 示例网段 `10.77.0.0/30` 不与两台服务器现有网络重叠。

无上述信息时只能保持 `VIDEO_COLLECTOR_EGRESS_MODE=off`，不得猜测地址或修改国内服务器。

#### 5.1.1 网络组件

仓库提供无密钥模板：

```text
deploy/domestic-egress/wg-us.conf.example
deploy/domestic-egress/wg-cn.conf.example
deploy/domestic-egress/squid.conf.example
deploy/domestic-egress/README.md
```

在两台服务器使用操作系统稳定仓库安装 WireGuard；国内服务器另外安装其发行版维护的 Squid 6/7。私钥必须在各自服务器本地生成并以 `0600` 保存，不能写入 Git、聊天记录、Docker 环境或命令行参数。

安全组和监听要求：

1. 国内服务器 UDP `51820` 只允许来源 `47.251.87.147`。
2. TCP `3128` 不建立任何公网安全组规则。
3. Squid 只监听 `10.77.0.2:3128`，只接受来源 `10.77.0.1/32`。
4. 美国服务器只把 `10.77.0.2/32` 交给 WireGuard，不改变默认路由。
5. 项目容器不增加 `NET_ADMIN`、host 网络或特权模式。
6. Squid 拒绝 80/443 以外端口、所有私网/链路本地/元数据/保留目标，并以 `deny all` 结束。
7. Squid 禁用缓存、关闭包含 URL 的访问日志，不启用 SSL Bump。

安装配置后必须先执行版本对应的语法检查：

```bash
sudo squid -k parse
sudo systemctl restart squid
sudo systemctl enable squid
sudo ss -lntp | grep 10.77.0.2:3128
```

#### 5.1.2 纯网络 PoC

从 `47.251.87.147` 验证隧道和代理：

```bash
sudo wg show wg-video-collector
ping -c 3 10.77.0.2
curl --fail --show-error --proxy http://10.77.0.2:3128 https://example.com/ -o /dev/null
curl --fail --show-error --proxy http://10.77.0.2:3128 http://169.254.169.254/
```

前三项应成功，元数据地址必须被拒绝。还必须从一台公网主机确认 TCP `3128` 不可达。

随后在项目容器内对一个直连确实受限的公开样本执行 yt-dlp 代理解析和最小完整格式下载。确认：

- 代理解析和浏览器最终下载成功。
- 国内服务器没有项目目录、媒体文件或对象缓存。
- 未列入受控主机的海外平台继续直连。
- 代理停止时非受限站点仍可用。

#### 5.1.3 应用启用

网络 PoC 通过后才允许在 `/opt/video-collector/.env` 写入：

```dotenv
VIDEO_COLLECTOR_EGRESS_MODE=auto
VIDEO_COLLECTOR_CN_PROXY_URL=http://10.77.0.2:3128
VIDEO_COLLECTOR_CN_PROXY_SOURCE_HOSTS=bilibili.com,b23.tv
VIDEO_COLLECTOR_CN_PROXY_ROUTE_TTL_SECONDS=1800
VIDEO_COLLECTOR_CN_PROXY_CONNECT_TIMEOUT_SECONDS=5
VIDEO_COLLECTOR_CN_PROXY_BREAKER_FAILURES=3
VIDEO_COLLECTOR_CN_PROXY_BREAKER_SECONDS=60
```

不要设置容器级 `HTTP_PROXY`、`HTTPS_PROXY` 或 `ALL_PROXY`。应用只为当前任务的 yt-dlp 和图片 HTTP 请求注入受信任代理；本地 FFmpeg、Whisper 和上传文件转录不会访问代理。

启用并检查：

```bash
cd /opt/video-collector
docker compose config
docker compose up -d --no-build
curl -fsS http://127.0.0.1:8787/health
```

`egressStatus` 只返回：

- `off`：功能关闭。
- `available`：自动模式可接受备用出口任务。
- `degraded`：存在连接失败或处于半开恢复阶段。
- `unavailable`：连续连接失败后正在熔断；非受限站点仍直接访问。

接口不会返回代理 URL、WireGuard 地址、密钥或域名规则。同一任务最多切换一次出口；解析、视频、音频、字幕、图片与 URL 转录输入固定使用同一出口，切换前删除当前尝试的部分文件。

#### 5.1.4 关闭与回滚

无需回滚镜像即可立即关闭：

```bash
cd /opt/video-collector
sed -i 's/^VIDEO_COLLECTOR_EGRESS_MODE=.*/VIDEO_COLLECTOR_EGRESS_MODE=off/' .env
docker compose up -d --no-build
curl -fsS http://127.0.0.1:8787/health
```

确认返回 `"egressStatus":"off"`。随后可停止国内 Squid 和 WireGuard；正式网站和非受限站点必须继续工作。

## 6. 缓存目录

项目所有运行缓存必须位于项目根 `cache`：

```bash
cd /opt/video-collector
mkdir -p cache/tasks cache/tmp
sudo chown -R 100:101 cache
sudo chmod -R 0750 cache
```

容器映射：

```text
/opt/video-collector/cache  →  /app/cache
/app/cache/tasks            临时任务与完成文件
/app/cache/tmp              Python、yt-dlp、FFmpeg 临时目录
```

服务启动时会删除上次运行遗留的任务目录。

保留规则：

- 用户首次请求完成文件后，15 分钟删除。
- 完成但未领取的文件，30 分钟删除。
- 活动下载结束前不会删除正在读取的文件。
- 失败或取消任务清理部分文件。

这些文件是临时交付物，不需要备份。

## 7. 构建和启动

```bash
cd /opt/video-collector
docker compose config
docker compose pull
docker compose up -d --no-build
```

公开 GHCR 镜像无需服务器登录 GitHub。若需要从源码复现镜像，可改用 `docker compose build && docker compose up -d`。

检查：

```bash
docker compose ps
docker compose logs --tail=100 video-collector
curl -fsS http://127.0.0.1:8787/health
```

健康响应应包含：

```json
{
  "status": "ok",
  "ytDlpVersion": "2026.07.04",
  "ffmpegVersion": "ffmpeg version 6.1.2 ...",
  "whisperVersion": "whisper.cpp 1.8.6",
  "whisperModel": "ggml-base.bin",
  "egressStatus": "off"
}
```

容器必须显示 `healthy`。如果 8787 被其他进程占用，应先确认进程归属；生产环境不允许随意改用公开监听端口。

## 8. Nginx

仓库已提供：

```text
deploy/nginx/video-collector.ximoai.cn.conf
```

安装：

```bash
sudo cp deploy/nginx/video-collector.ximoai.cn.conf /etc/nginx/sites-available/video-collector.ximoai.cn
sudo ln -sfn /etc/nginx/sites-available/video-collector.ximoai.cn /etc/nginx/sites-enabled/video-collector.ximoai.cn
sudo nginx -t
sudo systemctl reload nginx
```

关键要求：

- `server_name` 必须是 `video-collector.ximoai.cn`。
- 只代理到 `http://127.0.0.1:8787`。
- 传递 `X-Real-IP`、`X-Forwarded-For` 和 `X-Forwarded-Proto`。
- 不启用跨域；页面和 API 使用同一域名。
- 长任务由前端轮询，不需要 WebSocket。
- `client_max_body_size 251m` 允许 250 MiB 转录文件及 multipart 开销；应用仍执行 250 MiB 硬限制。
- `proxy_request_buffering off` 将上传直接流式送入项目容器，避免 Nginx 在项目外落地请求体缓存。

## 9. DNS 与 HTTPS

在 DNS 控制台设置：

| 类型 | 主机记录 | 值 |
|---|---|---|
| A | `video-collector` | `47.251.87.147` |

确认解析：

```bash
dig +short video-collector.ximoai.cn A
```

结果只能包含 `47.251.87.147`。

使用受信任 ACME 客户端签发证书，例如 Certbot：

```bash
sudo certbot --nginx -d video-collector.ximoai.cn
sudo certbot renew --dry-run
```

上线后确认：

```bash
curl -fsSI https://video-collector.ximoai.cn/
curl -fsS https://video-collector.ximoai.cn/health
```

HTTP 必须跳转到 HTTPS，证书必须覆盖正式域名。

## 10. 匿名 API

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/health` | 健康检查与依赖版本 |
| GET | `/api/v1/status` | 前端运行状态 |
| POST | `/api/v1/media/parse` | 解析公开视频 |
| POST | `/api/v1/media/batch` | 逐条解析 1–10 个 URL |
| POST | `/api/v1/collections/parse` | 读取公开集合前 10 条 |
| POST | `/api/v1/tasks` | 创建视频、MP3、图片、字幕或 URL 转录任务 |
| POST | `/api/v1/transcriptions/upload` | 上传最大 250 MiB 音视频创建转录任务 |
| GET | `/api/v1/tasks/{id}` | 查询任务 |
| DELETE | `/api/v1/tasks/{id}` | 取消任务 |
| GET | `/api/v1/tasks/{id}/download` | 下载完成文件 |

解析请求：

```bash
curl -fsS https://video-collector.ximoai.cn/api/v1/media/parse \
  -H 'Content-Type: application/json' \
  --data '{"url":"https://www.acfun.cn/v/ac48722683"}'
```

API 只接受 `application/json`，请求体上限 16 KiB，禁止未知字段和尾随 JSON。

## 11. 无登录模式的安全模型

本项目不做登录鉴权。安全依赖以下边界：

1. 任务 ID 由 16 字节密码学随机数生成，以 32 位十六进制返回。
2. 任务 ID 是临时能力标识；不知道 ID 的访问者无法枚举任务。
3. 浏览器使用 `Referrer-Policy: no-referrer`，降低任务 URL 泄露。
4. IP 限流限制解析和创建任务。
5. 全局并发和有界队列限制 CPU、内存和磁盘消耗。
6. 文件短期保存并自动删除。

匿名服务无法像账户系统一样提供跨设备历史和强用户归属。不要把任务 URL、任务 ID或服务器日志公开分享。

## 12. SSRF 与子进程安全

服务端已经：

- 只接受 HTTP/HTTPS URL。
- 拒绝用户名密码、localhost、`.local`、回环、私网、链路本地、组播和未指定地址。
- 对域名执行 DNS 解析并检查全部 IPv4/IPv6 结果。
- 再次验证下载任务中的源 URL。
- 限制 URL 长度为 2048。
- 使用参数数组启动 yt-dlp/FFmpeg，不经过 Shell。
- 单任务默认 2 小时硬超时，转录媒体本身仍限制为 60 分钟。
- 校验格式 ID，只允许有限字符。
- 输出目录和文件模板由服务器决定。
- 规范化下载文件名并检查最终路径仍在任务目录。
- 限制元数据为 16 MiB，yt-dlp 单文件最大 2 GiB。
- 对临时网络错误最多重试 3 次。

生产服务器还应通过安全组或出口防火墙拒绝访问 RFC1918、链路本地和云元数据地址。yt-dlp 内部重定向由外部进程处理，不能只依赖应用首次 DNS 校验。

## 13. 平台支持

支持范围由固定版本 yt-dlp 决定，只处理不需要登录、Cookie、DRM 或权限绕过的公开页面。

已通过：

- AcFun：解析、MP4 下载与浏览器领取。
- SoundCloud：NASA 公开音频的 MP3、图片、URL/文件转录及 10 条集合。
- TED：公开视频解析和人工字幕 SRT。
- TikTok：历史生产服务器真实下载通过。

不作为通过项：

- 旧 Vimeo 样本：当前返回 OAuth 401。
- 西瓜视频样本：平台要求 Cookie。

平台更新可能导致临时失效。升级 yt-dlp 前后都应执行真实回归。

## 14. 日志和监控

常用命令：

```bash
docker compose ps
docker compose logs --since=30m video-collector
docker stats --no-stream
du -sh /opt/video-collector/cache
df -h /opt/video-collector
curl -fsS http://127.0.0.1:8787/health
```

至少监控：

- 容器健康状态。
- Nginx 4xx、429 和 5xx。
- CPU、内存、磁盘空间和 inode。
- `cache` 目录大小。
- yt-dlp/FFmpeg 失败率。
- 解析和任务延迟。
- 自动出口启用时的 `egressStatus`、WireGuard 最近握手、Squid 当前连接数和总流量；不得采集完整 URL。

建议告警：

- 健康检查连续 3 次失败。
- 磁盘剩余低于 20%。
- 5xx 持续超过 5%。
- 429 持续上升。
- 容器反复重启。

日志不得记录 Cookie、令牌、完整带敏感参数 URL、媒体直链或环境变量。

## 15. 防火墙

阿里云安全组和服务器防火墙：

- 22：只允许管理来源 IP。
- 80：公网开放，用于跳转和证书签发。
- 443：公网开放。
- 8787：禁止公网开放。

验证 8787 仅本机：

```bash
ss -lntp | grep 8787
```

应显示 `127.0.0.1:8787`，而不是 `0.0.0.0:8787` 的宿主机公开端口。

## 16. 更新

更新前保留当前镜像：

```bash
cd /opt/video-collector
BACKUP_TAG="backup-$(date +%Y%m%d-%H%M%S)"
docker tag ghcr.io/chenlinning/video-collector:latest "ghcr.io/chenlinning/video-collector:${BACKUP_TAG}"
echo "Backup image tag: ${BACKUP_TAG}"
```

部署新代码：

```bash
git pull --ff-only
docker compose pull
docker compose up -d --no-build
docker compose ps
curl -fsS http://127.0.0.1:8787/health
```

随后从正式域名执行浏览器和真实平台冒烟测试。活动任务是临时数据，更新会中断它们；部署前应选择低使用时段。

## 17. 回滚

1. 停止当前服务。
2. 将备份镜像重新标记为 Compose 使用的标签。
3. 启动服务。
4. 检查健康接口和正式域名。

```bash
docker compose down
sed -i 's|^VIDEO_COLLECTOR_IMAGE=.*|VIDEO_COLLECTOR_IMAGE=ghcr.io/chenlinning/video-collector:BACKUP_TAG|' .env
docker compose up -d --no-build
curl -fsS http://127.0.0.1:8787/health
```

将命令中的 `BACKUP_TAG` 替换为更新前输出的实际备份标签。恢复稳定版后，再将 `.env` 的 `VIDEO_COLLECTOR_IMAGE` 改回 `ghcr.io/chenlinning/video-collector:latest`。

至少保留一个已验证镜像。不要在新版本稳定前执行 `docker image prune`。

## 18. 常见故障

| 现象 | 检查 |
|---|---|
| 容器启动失败 | `docker compose logs`、`cache` UID/GID、8787 占用 |
| 健康检查失败 | yt-dlp/FFmpeg 是否存在、容器是否只读写入失败 |
| Whisper 不就绪 | `/app/models/ggml-base.bin`、`whisper-cli`、模型内存和容器 CPU |
| 页面 502 | 容器状态、Nginx `proxy_pass`、本机 8787 |
| 页面有界面但不能解析 | 是否部署了完整容器而非单独 `dist-web` |
| 400 无格式 | 平台是否公开、是否需要登录/Cookie、yt-dlp 是否兼容 |
| TikTok 超时 | 服务器出口网络、DNS、区域限制和 CDN 连接 |
| `egressStatus` 为 `degraded` | 检查 WireGuard 握手、国内代理连接和受控主机近期失败；非受限站点应仍直连 |
| `egressStatus` 为 `unavailable` | 代理连续连接失败并熔断；检查国内服务器和私网监听，不要改成公网代理 |
| `auto` 模式无法启动 | 检查代理是否为固定私有 IPv4 的 `http://host:port`、主机规则和数值范围 |
| 429 | IP 限流或队列已满；检查是否遭到滥用 |
| 下载失败 | FFmpeg、临时磁盘、格式 ID 和平台 CDN |
| 文件已过期 | 首次下载后超过 15 分钟或未领取超过 30 分钟 |
| 磁盘增长 | 活动任务、容器重启、`cache` 权限和清理循环 |
| 主题仍是旧版 | 浏览器/CDN 缓存和实际运行镜像 ID |

## 19. 正式上线验收

- [x] A 记录只指向 `47.251.87.147`。
- [x] 正式地址为 `https://video-collector.ximoai.cn`。
- [x] HTTPS 有效并自动续期。
- [x] 8787 只绑定本机。
- [x] Compose 配置展开通过。
- [x] 容器状态为 `healthy`。
- [x] `/health` 返回真实工具版本。
- [ ] 国内临时出口网络 PoC、安全组和私网监听通过。
- [ ] `auto` 模式健康状态、熔断和无需换镜像的 `off` 回滚通过。
- [ ] 国内服务器无项目目录、任务文件、媒体缓存或完整 URL 访问日志。
- [x] 无需登录即可打开首页和调用 API。
- [x] 旧 `/sso/entry` 返回 404。
- [ ] Bilibili 生产解析和下载通过。
- [x] AcFun 生产解析、下载和浏览器原生下载事件通过。
- [x] TikTok 用户样本在生产服务器出口通过。
- [x] SoundCloud MP3、图片、集合和 URL 转录在本次生产版本通过。
- [x] TED 字幕 SRT 在本次生产版本通过。
- [x] 上传文件转录在本次生产版本通过。
- [ ] 音视频分离格式自动合并。
- [ ] 取消任务清理部分文件。
- [ ] IP 限流、并发和队列上限生效。
- [x] 15/30 分钟 TTL 生效。
- [x] 390px 与桌面布局无横向溢出。
- [x] 嵌入主站时深浅主题实时同步，独立打开时本地主题生效。
- [x] 嵌入主站时中英文随主站语言协议实时同步。
- [x] 主按钮字体、字号、字重、颜色、圆角和阴影与主站通用按钮一致。
- [ ] 日志、监控、磁盘告警和回滚已验证。

生产验收记录（2026-07-26）：

- 当前运行提交：`a31d4f6`；GitHub Actions：`30188369329`。
- AcFun MP4 文件 5,936,297 字节，SHA-256：`1A7F2473287DA4D9391463D4F067121AF59F2AEB3805B2E17CC30C1A2E0DFE89`。
- TikTok 文件 15,217,445 字节，SHA-256：`8165BBE71146F9CD633F89787AFBAAE8912C1E89CA51DCE8445D0FBB2BABF8C3`。
- SoundCloud MP3 为 549,165 字节，图片为 40,518 字节，公开集合返回 10 条；URL 转录生成 429 字节 SRT。
- TED 人工字幕生成 8,000 字节 SRT；上传 SoundCloud MP3 后离线转录生成 433 字节 SRT，并返回文本预览。
- 正式站点内置浏览器解析 AcFun 后点击下载，收到原生 `download` 事件；页面随后显示“首次下载已触发”及约 15 分钟后的清理时间。
- 下载响应 `X-Delete-At` 实测剩余 14.93–14.94 分钟；超过未领取保留期的任务返回 404，活动下载结束前不删除。
- 正式站点 390px 视口实测 `scrollWidth === clientWidth`，无横向溢出；桌面布局同时通过。
- Bilibili 在美国服务器出口返回 412，属于平台 IP 风控；不使用 Cookie、代理或规避逻辑。
- CSP 包含 `frame-ancestors *`，且未发送 `X-Frame-Options`，允许主站嵌入。
- 主站主题从深色切换至浅色时，嵌入页 `data-theme` 实时同步为 `light`；切回后双方恢复为 `dark`。
- 主站从中文切换至英文时，嵌入页实时更新为 `lang="en"` 和完整英文文案；切回后恢复为 `lang="zh-CN"`。
- 生产“开始解析”按钮实测为 `#5a8b67`、白字、14px/500、12px 圆角、系统 UI 字体栈和主站轻阴影。

完成全部检查后，才可把远程部署状态标记为正式上线。
