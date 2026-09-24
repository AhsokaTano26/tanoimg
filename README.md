# TanoImg

轻量的 Go 图床，支持从 [EasyImg](https://github.com/AhsokaTano26/easyimg) 迁移。图片保存在文件系统，元数据存于 SQLite；上传和图片响应采用流式读写，图库分页查询，不会把整个图库载入内存。

## 功能

- 图片拖拽、选择、粘贴及批量上传，直链和 Markdown 复制
- 批量上传固定 4 路并发，提供进度、取消和失败重试；结果列表只保留最近 24 条，避免大量 DOM 和预览对象占用浏览器内存
- 公开上传开关、私有上传、管理员图库与软删除
- API Key 管理、管理员密码修改、空间统计、移动端界面
- Crypto Blue 界面；管理员可上传背景图片、填写 HTTPS 图片地址，并调节 0–40 px 模糊度
- 导入 EasyImg 的 NeDB `images.db`、`users.db`、`apikeys.db`、`settings.db`、`moderation_tasks.db`、`ip_blacklist.db` 和 `uploads/`
- 保留 EasyImg 的图片 UUID、记录 ID、原文件名和 `/i/<uuid>.<格式>` 直链

## 快速启动

需要 Go 1.26 或 Docker。

```bash
export TANOIMG_ADMIN_PASSWORD='请换成强密码'
go run ./cmd/tanoimg serve -data ./data -addr :3000
```

打开 `http://localhost:3000`。新安装默认关闭公开上传；管理员登录后可私有上传并在“管理”里开启公开上传。启动前必须设置管理员密码，系统不会创建默认弱密码。

在“管理 → 外观设置”中，可以上传背景图片或填写 HTTPS 图片地址，拖动滑块预览模糊度后保存。上传的背景图片使用私有上传接口，保存后其直链会作为全站背景；“恢复默认背景”会清除背景和模糊度。外部图片由访客浏览器直接加载，建议使用自己控制的 HTTPS 图片源。

Docker：

```bash
TANOIMG_ADMIN_PASSWORD='请换成强密码' docker compose up -d --build
```

## 从 EasyImg 迁移

1. 停止 EasyImg 写入，并备份原有的 `db/` 与 `uploads/`。
2. 执行迁移命令；`-from` 指向同时包含这两个目录的 EasyImg 根目录：

```bash
go run ./cmd/tanoimg migrate -from /path/to/easyimg -data ./data
```

3. 检查输出的 `missingFiles`。非零表示数据库存在图片记录，但原文件缺失，需要从备份补齐。
4. 使用原 EasyImg 管理员用户名和密码登录 TanoImg。已有 API Key 也会保留。旧 JWT 会话不会迁移，需要重新登录。
5. 将原域名反向代理到 TanoImg 后，旧 `/i/...` 链接可以继续使用。

迁移按 NeDB 文件的顺序逐行处理，重复执行不会复制记录。迁移不会修改原始 EasyImg 数据。旧审核任务及其他原始字段保存在 SQLite 的 `source_documents` 表中，方便以后继续扩展。迁移时请让 EasyImg 停止写入，避免文件与记录处在不同时间点。若原图很多，建议先预留足够的磁盘空间；迁移会复制原图。迁移完成并开始使用 TanoImg 后不要再次执行迁移，否则旧记录和设置可能覆盖新改动。

Docker 部署也可以用 `docker compose run --rm -v /path/to/easyimg:/old:ro tanoimg migrate -from /old -data /data`。Compose 使用名为 `tanoimg_data` 的数据卷，备份时也要备份该卷。

## API

| 接口 | 说明 |
| --- | --- |
| `POST /api/auth/login` | JSON `username`、`password`；返回会话 token，并设置 HttpOnly Cookie |
| `POST /api/upload/private` | `multipart/form-data`，字段 `file` 或 `image`；使用 `X-API-Key`、`?apiKey=` 或管理员会话 |
| `POST /api/upload/public` | 公开上传开启后可用，表单字段同上 |
| `GET /api/images?page=1&limit=20` | 分页图库；匿名用户只见公开图片 |
| `GET /i/<uuid>.<格式>` | 图片直链 |
| `DELETE /api/images/<id>` | 管理员软删除 |
| `GET/POST /api/apikeys` | 管理员查询或创建 API Key |
| `PUT /api/admin/password` | 管理员使用旧密码修改密码，并注销所有会话 |
| `GET/PUT /api/config/public` | 查询或更新公开上传配置 |
| `GET/POST /api/blacklist`、`DELETE /api/blacklist/<id>` | 管理 IP 黑名单 |
| `GET /api/settings/stats` | 管理员空间统计 |
| `GET /api/settings/public` | 公开的站点名称与外观设置 |
| `PUT /api/settings/appearance` | 管理员更新 `backgroundUrl` 和 `backgroundBlur`（0–40） |

上传示例：

```bash
curl -H 'X-API-Key: sk-...' -F 'file=@photo.jpg' http://localhost:3000/api/upload/private
```

## 批量上传与资源

管理员网页会将所选图片加入上传队列，最多同时发送 4 张。API Key 客户端也建议使用约 4 路并发；服务端同时处理 4 个上传请求，另有 32 个等待位置。队列满时返回 HTTP 429 和 `Retry-After: 1`，客户端应等待后重试。匿名公开上传仍受每 IP 限流约束。

可在目标机器上运行以下基准。它会在测试临时目录生成约 2.5 GiB 文件，结束后自动清理：

```bash
go test ./internal/app -run '^$' -bench '^BenchmarkPrivateUploadHTTP$' -benchtime=10000x -benchmem -count=1
```

本机 Apple M4 Pro、有效的随机像素 PNG、HTTP 回环与 4 路并发下，约 256 KiB 的 10,000 张图片处理阶段耗时约 6.3 秒，采样 Go 堆峰值约 5.2 MiB；约 2 MiB 的 10,000 张图片处理阶段耗时约 32.4 秒，采样 Go 堆峰值约 15.0 MiB。采样堆包含同进程中的压测客户端和服务端。此结果包含本机 HTTP、SQLite 和文件写入，不代表公网、机械硬盘或同步到远端存储时的速度；实际完成时间取决于图片大小、网络上行与磁盘持续写入能力。

## 资源与兼容性

- SQLite 连接池限制为一个连接；上传最多同时处理四个请求。上传流式写入目标磁盘上的临时文件，再以重命名完成落盘，避免把大图读入 Go 堆和跨磁盘复制。
- 公开上传按客户端 IP 限流并检查黑名单。默认使用直连地址；在可信反向代理后运行时设置 `TANOIMG_TRUST_PROXY=true`，并确保外部流量只能经过该代理，才会读取 `X-Forwarded-For`。
- 新上传会按文件内容识别 JPG、PNG、GIF、WebP、AVIF、BMP、ICO；为了避免同源脚本风险，新 SVG 上传关闭。迁移的 SVG 仍可读取。
- 当前版本保留并导入 EasyImg 的图片、账户、密钥、设置、审核任务和 IP 黑名单数据。EasyImg 的图片压缩、格式转换、URL 抓取、AI 内容审核与通知服务尚未实现；迁移来的对应设置和审核任务会保存在 SQLite 中，但不会自动执行这些服务。
- 图片是软删除，原文件保留。手动部署备份 `data/` 即可备份数据库与图片。

## 开发检查

```bash
go test ./...
go build ./cmd/tanoimg
```
