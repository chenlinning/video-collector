# 视频站主站嵌入软门禁

本项目支持不修改主站代码的软门禁模式。生产环境可通过环境变量启用：

```dotenv
VIDEO_COLLECTOR_EMBED_MODE=soft
VIDEO_COLLECTOR_EMBED_ALLOWED_ORIGINS=https://ximoai.cn,https://www.ximoai.cn
VIDEO_COLLECTOR_EMBED_SESSION_TTL_SECONDS=3600
```

启用后：

- `/health` 保持公开，供 Docker 和运维探针使用。
- 根页面只能由允许来源发起的浏览器 iframe 首次加载；请求必须带 `Sec-Fetch-Dest: iframe`、同站 Fetch Metadata，以及匹配的 `Origin` 或 `Referer`。
- 首次合法加载发放短期、HttpOnly、Secure、SameSite=Lax 的内存会话 Cookie。
- 解析、任务、查询、取消、下载和上传接口都要求该会话 Cookie；没有会话的直链/API 请求返回 403。
- CSP 的 `frame-ancestors` 仅允许配置的主站来源；软模式下静态资源使用 `no-store`，避免共享缓存绕过来源检查。

这不是密码学访问控制：客户端可以伪造 Fetch Metadata、Origin、Referer，或转发已经取得的会话 Cookie。因此，在不修改主站的前提下，它能阻止普通直链和匿名 API 访问，但不能保证绝对的“只有主站用户”边界。若需要强制边界，仍需主站反向代理私有上游或签发一次性签名票据。

默认值为 `VIDEO_COLLECTOR_EMBED_MODE=off`，以保持本地开发和其他部署的原有匿名 API 行为。
