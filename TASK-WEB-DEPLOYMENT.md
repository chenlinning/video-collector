# Video Collector Web 主题与部署任务书

> 文档状态：实施完成，验收通过
> 创建日期：2026-07-25
> 交付形态：可静态托管的 Web 前端
>
> 后续状态：2026-07-26 已完成无需登录的 Go 后端、Docker 镜像和多平台真实验收；当前实施状态以 `TASK-INDEPENDENT-WEB-SERVICE.md` 为准。
> 固定生产目标：唯一服务器 `47.251.87.147`，唯一正式网址 `https://video-collector.ximoai.cn`。

## 1. 目标

在保留现有桌面代码的前提下，将 Video Collector 前端准备为可部署到服务器的独立网站，并完成以下界面要求：

1. 参考 Deerflow 的中性暖灰视觉语言。
2. 提供深色、浅色两套主题。
3. 用户可从顶部导航栏切换主题，刷新后保持选择。
4. 桌面与移动布局均无横向溢出。
5. 生成可由普通静态服务器托管的 `dist-web` 目录。

## 2. 技术范围

### 本次包含

- React 前端主题切换与本地持久化。
- Deerflow 风格的深色、浅色设计令牌。
- 独立 Vite Web 生产构建。
- 相对静态资源路径，兼容根目录和子目录部署。
- 单元测试、类型检查、构建和浏览器视觉验收。

### 本次不包含

- Windows 安装包或便携版更新。
- 服务器购买、域名、HTTPS 证书和真实线上发布。
- 在线 yt-dlp/FFmpeg 后端 API。

## 3. 关键边界

静态网页无法直接运行本机 yt-dlp 和 FFmpeg。当前 `dist-web` 是可部署的前端产物；视频解析与下载仍需要后续实现同源服务器 API。不得把仅完成前端部署描述为“在线视频下载功能已经可用”。

## 4. 实施步骤与验收

- [x] W1 添加主题偏好单元测试。
- [x] W2 实现深色/浅色切换与本地持久化。
- [x] W3 完成浅色设计令牌和无障碍按钮标注。
- [x] W4 浏览器验证两套主题及刷新持久化。
- [x] W5 运行完整单元测试与 TypeScript 检查。
- [x] W6 生成 `dist-web` 生产构建。
- [x] W7 使用本地静态服务器验证生产产物。
- [x] W8 补充完整生产部署指南，覆盖后端实现后的运维与安全要求。

## 5. 验收命令

```powershell
pnpm test
pnpm typecheck
pnpm build:web
pnpm preview:site
```

## 6. 验收标准

| 编号 | 验收项 | 通过条件 | 状态 |
|---|---|---|---|
| WEB-01 | 默认主题 | 首次访问显示 Deerflow 风格深色主题 | 通过 |
| WEB-02 | 浅色主题 | 切换后显示暖白背景、白色卡片和深色主按钮 | 通过 |
| WEB-03 | 持久化 | 刷新页面后仍保持用户选择 | 通过 |
| WEB-04 | 响应式 | 桌面和移动宽度均无横向溢出 | 通过 |
| WEB-05 | 构建 | `pnpm build:web` 成功并生成 `dist-web` | 通过 |
| WEB-06 | 静态托管 | 生产产物可由本地静态服务器打开，资源无 404 | 通过 |
| WEB-07 | 功能边界 | 文档明确说明 Web 后端尚未实现 | 通过 |
| WEB-08 | 部署指南 | 依赖、API、代理、配置、缓存、守护、安全、监控和回滚均有说明 | 通过 |

## 7. 服务器部署示例

将 `dist-web` 中的全部文件复制到站点目录。Nginx 最小配置示例：

```nginx
server {
    listen 80;
    server_name example.com;
    root /var/www/video-collector;
    index index.html;

    location / {
        try_files $uri $uri/ /index.html;
    }
}
```

正式上线时应启用 HTTPS，并在接入后端 API 后同步收紧或扩展 Content Security Policy。

## 8. 最终验收记录

- 单元测试：6 个测试文件、29 项测试，全部通过。
- TypeScript：`tsc --noEmit` 通过。
- Web 构建：Vite 生产构建通过，输出 `dist-web/index.html`、1 个 CSS 和 1 个 JavaScript 文件。
- 静态资源：首页、CSS、JavaScript 经本地静态服务器访问均返回 HTTP 200。
- 主题：默认深色、浅色切换、刷新持久化均通过浏览器验收。
- 响应式：390px 测试宽度无横向溢出，主题按钮保持可见。
- 浏览器控制台：生产站点错误日志为 0。
- 生产预览：`http://127.0.0.1:4174/`。
- 完整部署说明：`DEPLOYMENT.md`；明确区分当前可执行步骤与后端实现后的目标配置。
