# Video Collector 匿名 Web 服务实施与验收任务书

> 状态：本地实施与容器验收完成，远程部署待执行
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
- [x] 大文件流式保存；不支持文件系统 API 时由浏览器直接下载。
- [x] Docker 多阶段构建、非 root 和只读根文件系统。
- [x] 项目缓存全部定向项目根 `cache`。
- [x] Nginx 反向代理配置。
- [x] 完整部署文档。
- [ ] 在 `47.251.87.147` 完成实际部署、DNS 与 HTTPS 验收。

## 7. 自动化验收

- Go：`go test ./...` 通过。
- 前端：7 个测试文件、32 项测试通过。
- TypeScript：`tsc --noEmit` 通过。
- Vite Web 生产构建通过。
- Docker Compose 配置展开通过。
- Docker 镜像 `ximoai/video-collector:0.1.2` 构建通过。
- 容器健康状态为 `healthy`。
- 浏览器无登录即可完成 AcFun 解析、画质选择、任务创建与完成，控制台错误为 0。

## 8. 真实平台验收

### Bilibili

- URL：`https://www.bilibili.com/video/BV1ex4y1P7Xm/`
- 解析器：`BiliBili`
- 返回 7 个格式，最高 1080p。
- 真实下载 3,449,504 字节。
- FFprobe：H.264 视频 + AAC 音频，分离流自动合并通过。
- SHA-256：`FF313C59BB8BC32B4B163ED37F2EF86D2562A7CAE92ACC32B151FFF2315AA514`

### AcFun

- URL：`https://www.acfun.cn/v/ac48722683`
- 解析器：`AcFunVideo`
- 返回 3 个格式，最高 720p。
- 修复了 HLS 格式编码字段为空时被错误过滤的问题。
- 容器内真实下载 1,706,444 字节。
- FFprobe：H.264 视频 + AAC 音频。
- 容器样本 SHA-256：`0932D7E0BB79F3902D9560FDDDC9FFB6408C417E605BBD135371AC9DD3D2D640`

### TikTok

- 用户样本：`https://www.tiktok.com/@wowohpanda/video/7576493197174541588`
- 历史可行性验收已通过。
- 2026-07-26 本机出口复测因连接 TikTok 超时失败，属于当前网络出口问题。
- 部署到唯一生产服务器后必须重新执行真实解析与下载验收，不能以本机历史结果代替服务器验收。

## 9. 完成定义

本地交付完成条件已经满足。正式上线还必须：

1. 将项目部署到 `47.251.87.147`。
2. 将 `video-collector.ximoai.cn` 的 A 记录指向该 IP。
3. 配置 HTTPS 并确认自动续期。
4. 从生产域名复测健康接口、Bilibili、AcFun 和 TikTok。
5. 验证限流、取消、文件 TTL、磁盘告警和回滚。
