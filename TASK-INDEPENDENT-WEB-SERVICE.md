# Video Collector 匿名 Web 服务实施与验收任务书

> 状态：远程生产部署与核心下载验收完成；Bilibili 受美国出口 IP 的 412 风控限制
> 日期：2026-07-26

## 1. 固定生产目标

- 唯一授权服务器公网 IPv4：`47.251.87.147`
- 唯一正式网址：`https://video-collector.ximoai.cn`
- 未经用户明确修改，不部署到其他服务器或正式域名。

## 2. 产品要求

1. 网站无需注册、登录、会员或第三方 SSO。
2. 用户粘贴有权保存的公开视频页面 URL。
3. 服务器通过 yt-dlp 解析标题、作者、封面、时长和格式。
4. 用户选择格式后创建临时下载任务。
5. 服务器通过 FFmpeg 自动合并分离的音视频。
6. 浏览器显示任务进度，并将完成文件下载到本机。
7. 首次领取后 10 分钟删除；未领取文件 30 分钟后删除。
8. 支持 yt-dlp 可解析且无需登录、Cookie、DRM 或访问控制绕过的公开视频平台。

## 3. 匿名服务安全边界

- 不实现账户、登录鉴权、会员校验或主站凭据。
- 任务 ID 使用 128 位加密随机值，同时充当不可猜测的任务能力标识。
- 解析和创建任务按客户端 IP 限流。
- 下载并发和排队任务都有全局硬上限。
- URL 在执行前进行协议、主机、DNS 和公网 IP 检查。
- yt-dlp/FFmpeg 使用参数数组和 `shell: false` 启动。
- 客户端不能提供服务器保存目录、输出模板或任意命令行参数。
- 请求体限制为 16 KiB，只接受单个 JSON 对象。
- 文件名规范化，任务目录限制在项目 `cache` 内。
- 任务过期和进程重启时清理临时文件。

## 4. 实现架构

```text
浏览器
  │ HTTPS，同源 JSON API
  ▼
Nginx
  │ 127.0.0.1:8787
  ▼
Go Web 服务
  ├── React/Vite 静态前端
  ├── 匿名 API 与 IP 限流
  ├── 任务队列与过期清理
  ├── yt-dlp
  └── FFmpeg / ffprobe
```

Docker 镜像同时包含前端、Go 后端、yt-dlp 和 FFmpeg。容器以 UID 100、GID 101 的非 root 用户运行，根文件系统只读，所有项目缓存写入 `/app/cache`。

## 5. API

| 方法 | 路径 | 用途 |
|---|---|---|
| GET | `/health` | 容器健康与依赖版本 |
| GET | `/api/v1/status` | 前端显示运行时状态 |
| POST | `/api/v1/media/parse` | 解析公开视频 |
| POST | `/api/v1/tasks` | 创建临时下载任务 |
| GET | `/api/v1/tasks/{id}` | 查询任务进度 |
| DELETE | `/api/v1/tasks/{id}` | 取消任务 |
| GET | `/api/v1/tasks/{id}/download` | 流式下载完成文件 |

所有 API 均为匿名同源接口，不需要登录 Cookie、JWT 或 API Key。

## 6. 实施状态

- [x] Go 解析、下载、进度、取消和过期清理。
- [x] 公网 URL 与 DNS 解析安全校验。
- [x] 128 位随机任务能力 ID。
- [x] 匿名 API，旧 SSO 路径返回 404。
- [x] IP 限流、全局并发和有界队列。
- [x] 单任务 30 分钟硬超时。
- [x] 严格 JSON 请求校验。
- [x] React 前端接入同源 API。
- [x] 深色/浅色主题与本地持久化。
- [x] 网站完成文件统一交给浏览器原生下载流程。
- [x] Docker 多阶段构建、非 root 和只读根文件系统。
- [x] 项目缓存全部定向项目根 `cache`。
- [x] Nginx 反向代理配置。
- [x] 完整部署文档。
- [x] 在 `47.251.87.147` 完成实际部署、DNS、HTTPS 与跨站嵌入验收。

## 7. 自动化验收

- Go：`go test ./...` 通过。
- 前端：7 个测试文件、34 项测试通过。
- TypeScript：`tsc --noEmit` 通过。
- Vite Web 生产构建通过。
- Docker Compose 配置展开通过。
- GitHub Actions `30182499658` 的前端、Go 后端和 GHCR 发布任务通过。
- 生产镜像 `ghcr.io/chenlinning/video-collector:latest` 已部署到提交 `ce41045`。
- 容器健康状态为 `healthy`。
- 浏览器无登录即可完成 AcFun 解析、画质选择、任务创建与完成。
- 点击“下载到本机”后，内置浏览器收到原生 `download` 事件。

## 8. 真实平台验收

### Bilibili

- URL：`https://www.bilibili.com/video/BV1ex4y1P7Xm/`
- 解析器：`BiliBili`
- 返回 7 个格式，最高 1080p。
- 真实下载 3,449,504 字节。
- FFprobe：H.264 视频 + AAC 音频，分离流自动合并通过。
- SHA-256：`FF313C59BB8BC32B4B163ED37F2EF86D2562A7CAE92ACC32B151FFF2315AA514`
- 美国生产服务器出口解析返回 412；yt-dlp 上游确认属于平台 IP 风控，不使用 Cookie、代理或规避逻辑。

### AcFun

- URL：`https://www.acfun.cn/v/ac48722683`
- 解析器：`AcFunVideo`
- 返回 3 个格式，最高 720p。
- 修复了 HLS 格式编码字段为空时被错误过滤的问题。
- 容器内真实下载 1,706,444 字节。
- FFprobe：H.264 视频 + AAC 音频。
- 容器样本 SHA-256：`0932D7E0BB79F3902D9560FDDDC9FFB6408C417E605BBD135371AC9DD3D2D640`
- 生产域名完成 360p 解析、任务生成和浏览器原生下载事件验收。

### TikTok

- 用户样本：`https://www.tiktok.com/@wowohpanda/video/7576493197174541588`
- 生产服务器真实下载 15,217,445 字节。
- 生产样本 SHA-256：`8165BBE71146F9CD633F89787AFBAAE8912C1E89CA51DCE8445D0FBB2BABF8C3`

## 9. 完成定义

核心上线条件已经满足：唯一服务器和域名已启用，HTTPS 与自动续期通过，容器健康，匿名 API、AcFun、TikTok 和浏览器下载流程均已完成生产验收。

后续运维项：

1. 更换未受 Bilibili 412 风控影响的合规服务器出口后复测 Bilibili。
2. 在低流量时段执行生产限流、取消和 10/30 分钟 TTL 演练。
3. 持续检查磁盘告警、日志和镜像回滚流程。
