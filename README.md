# TanoImg

轻量的 Go 图床，支持从 [EasyImg](https://github.com/AhsokaTano26/easyimg) 迁移。图片保存在文件系统，元数据存于 SQLite；上传和图片响应采用流式读写，图库分页查询，不会把整个图库载入内存。

## 功能

- 图片拖拽、选择、粘贴及批量上传，直链和 Markdown 复制
- 批量上传固定 4 路并发，提供进度、取消和失败重试；结果列表只保留最近 24 条，避免大量 DOM 和预览对象占用浏览器内存
- 公开/私有上传、URL 单张或批量导入、管理员图库、批量软删除与回收站恢复/清空；管理员可从图库右上角进入回收站
- 管理员账户、API Key 增删改与重置、IP 黑名单、空间与审核统计
- 可选图片压缩和 WebP/JPEG/PNG 转换；默认直存，启用处理后同一时间只解码一张
- 可选 NSFW 后台审核（nsfwdet、Elysia Tools、自建 nsfw_detector），Webhook、Telegram、Email、Server酱通知
- 瀑布流图库、深色模式、公告、API 指南、统计页和移动端界面
- 石墨深色 / 浅色界面，所有图标使用本地 Lucide；管理员可上传背景图片、填写 HTTPS 图片地址，并调节 0–40 px 模糊度
- 导入 EasyImg 的 NeDB `images.db`、`users.db`、`apikeys.db`、`settings.db`、`moderation_tasks.db`、`ip_blacklist.db` 和 `uploads/`
- 保留 EasyImg 的图片 UUID、记录 ID、原文件名和 `/i/<uuid>.<格式>` 直链

### EasyImg 功能对照

| 功能 | TanoImg 状态 |
| --- | --- |
| 点击、拖拽、粘贴和批量上传；URL 上传 | 已实现；URL 批量另有 SSE 进度接口 |
| 图库、预览、批量删除、回收站、违规图片管理 | 已实现；回收站支持逐张恢复 |
| 公共/私有上传、API Key、IP 黑名单 | 已实现 |
| 三种 NSFW 服务、自动拉黑、通知渠道 | 已实现；任务在 SQLite 中持久化并限流处理 |
| 网站设置、背景模糊、公告、深色模式、统计 | 已实现 |
| 图片格式与转换 | JPEG、PNG、GIF、WebP、AVIF、SVG、BMP、ICO、APNG、TIFF 可直存；JPEG/PNG/WebP 可处理，GIF/APNG 动画保持原文件 |
| 版本检查 | 管理员手动检查 TanoImg 的 GitHub 发布版本 |

访客只看到公共图片页（`/`）和图库上方的公共上传框（`/#public-upload`，旧 `/upload` 自动跳转），通过 header 的登录入口进入后台。后台位于 `/admin/*`，包括图片管理、上传、回收站、统计、API 指南和设置，需管理员会话才能访问。Vue Router 的 `RouterLink` / `RouterView` 负责页面导航、刷新及浏览器前进后退；旧设置、统计和回收站链接仍会转到对应的受保护页面。旧 JWT 会话不会迁移；旧账户和 API Key 会迁移。

## 快速启动

源码构建需要 Go 1.26、Node.js 22.12+ 和 npm，或使用 Docker。运行编译后的程序不需要 Node.js。

```bash
# 可选：不设置或留空时，首次启动自动生成密码并输出到日志
# export TANOIMG_ADMIN_PASSWORD='请换成强密码'
make build
./tanoimg serve -data ./data -addr :3000
```

打开 `http://localhost:3000`。新安装默认关闭公开上传；管理员登录后可私有上传并在“公共上传”设置页开启公开上传。首次初始化时，优先使用 `TANOIMG_ADMIN_PASSWORD`；未设置或为空时，自动生成 12 位密码（包含大小写字母、数字和特殊字符），在启动日志的 `initial administrator` 行查看。默认用户名为 `admin`，可通过 `TANOIMG_ADMIN_USER` 指定。已有账号不会在重启时被重置，随机密码只在创建时输出。

在“外观与背景”中，可以从现有图库选择、上传背景图片或填写 HTTPS 图片地址，拖动滑块预览模糊度后保存。上传的背景图片使用私有上传接口，保存后其直链会作为全站背景；“恢复默认背景”会清除背景和模糊度。外部图片由访客浏览器直接加载，建议使用自己控制的 HTTPS 图片源。

Docker 本地构建：

```bash
docker compose up -d --build
docker compose logs tanoimg
```

## Docker Hub 自动发布与部署

支持 GitHub Actions 自动测试并构建 amd64 / arm64 镜像，推送 Docker Hub。配置 `DOCKERHUB_USERNAME`、`DOCKERHUB_TOKEN` 两个 Secrets 后，镜像自动发布到 `<用户名>/tanoimg`；`main` 发布 `latest`、`edge` 和 7 位短 SHA 标签，正式 `v*` 版本发布版本标签与 `latest`。服务器直接使用 `deploy/compose.yml` 拉取镜像，无需本地编译。

完整配置、发布、更新与备份步骤见 [Docker Hub 部署说明](docs/docker-hub.md)。

## 登录安全

支持可选 TOTP 二次验证、一次性恢复码和 Passkey 直接登录。在“账户安全”管理；Docker 部署启用 Passkey 需设置 `TANOIMG_PUBLIC_URL=https://你的域名`。备份完整数据卷，包含新增的 `auth.key` 加密密钥。详见 [认证配置与使用](docs/authentication.md)。

## 图库与上传体验

- 图片右键或“更多”菜单可复制直链、HTML、Markdown、BBCode；管理员可设置全局背景、网站 Logo，或将图片移入回收站。
- 管理员上传可选择私人／公开，默认私人。私人表示不出现在公共图库，已知直链仍可访问；“私人上传”中的兼容设置可以显式开放私人图片展示。
- 访客在图库上方直接选择、拖放、粘贴图片或输入 URL 上传，无需切换页面。管理员文件和 URL 队列最多 4 路并发；访客队列逐张发送，遵守每 IP 限制。
- 站点、外观、公共上传、私人上传、密钥、审核、通知、账户、黑名单、存储清理与版本均为独立后台页面。表单、下拉、复选框、菜单、弹窗与移动端抽屉统一封装 PrimeVue；图标使用本地 Lucide 图标库。
- 允许格式通过复选框选择；页面切换有轻量过渡，遵循系统减少动态效果偏好。
- 站点公告支持段落、标题、加粗、链接和列表等安全 HTML；横幅与弹窗共用过滤后的渲染，脚本、图片、事件属性及自定义样式不会执行或显示。

### 公共上传自动封禁

在“公共上传”中配置启用状态、统计时间窗（1–1440 分钟）及最多请求次数（1–100000）。默认启用：每 IP 从首个请求开始的固定 10 分钟窗口内最多 120 次，**第 121 次自动加入 IP 黑名单**。文件和 URL 请求共用计数，失败、被限流的请求也计入；公开上传关闭时不计数。管理员／API Key 认证上传不受此策略限制。

计数和黑名单持久化在 SQLite，重启不会清除。管理员可在“IP 黑名单”解除封禁，解除时同时重置该 IP 的频率及自动封禁计数。原有每分钟限流仍然独立生效，客户端应遵守 `Retry-After`，不要立即重复请求。

## 从 EasyImg 迁移

**本地直接通过图床 API 迁移到远端：**使用 [远程迁移脚本与操作指南](docs/remote-migration.md)，支持分块续传、SHA-256 校验，并保留原图片 ID 和 `/i/` 路径。

迁移后部署到 Zeabur，请按 [Zeabur 迁移部署指南](docs/zeabur.md) 操作，包含数据转换、持久卷和上传切换流程。

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
| `POST /api/upload/public/url` | 访客 URL 上传，JSON `{"url":"https://…"}`；共用格式、大小、限流、封禁及审核策略 |
| `POST /api/upload/url`、`POST /api/upload/urls` | 管理员或 API Key 上传远程图片；后者使用 SSE 返回进度 |
| `GET /api/images?page=1&limit=20` | 分页图库；匿名用户只见公开图片 |
| `GET /i/<uuid>.<格式>` | 图片直链 |
| `GET /t/<uuid>.<格式>` | 缓存的 320 像素缩略图；JPEG、PNG、GIF/APNG 可生成，其余格式返回 415，访问权限与原图一致 |
| `POST /api/admin/images/<id>/replace` | 管理员原位替换；格式须与原图相同，ID、UUID 和 `/i/` 路径不变 |
| `GET /api/admin/images/<id>/versions`、`POST /api/admin/images/<id>/rollback/<version>` | 管理员查看与恢复历史版本 |
| `GET/PUT /api/settings/image-lifecycle` | 配置历史版本保留数量（默认 3，范围 0–10）及可选 JPEG 元数据移除 |
| `DELETE /api/images/<id>`、`DELETE /api/images/batch` | 管理员软删除 |
| `GET /api/images/deleted`、`PUT /api/images/<id>/restore` | 查看和恢复回收站图片 |
| `GET /api/images/nsfw`、`PUT /api/images/<id>/unmark-nsfw` | 违规图片管理 |
| `GET/POST /api/apikeys`、`PUT/DELETE /api/apikeys/<id>` | API Key 管理 |
| `PUT /api/admin/password` | 管理员使用旧密码修改密码，并注销所有会话 |
| `GET/PUT /api/config/public`、`GET/PUT /api/config/private` | 上传、压缩和格式转换配置 |
| `GET/POST /api/blacklist`、`DELETE /api/blacklist/<id>` | 管理 IP 黑名单 |
| `GET /api/settings/stats` | 管理员空间统计 |
| `GET /api/settings/public` | 公开的站点名称与外观设置 |
| `PUT /api/settings/appearance` | 管理员更新 `backgroundUrl` 和 `backgroundBlur`（0–40） |
| `GET/PUT /api/notification`、`POST /api/notification/test` | 通知设置与测试 |
| `GET /api/version/check` | 管理员主动检查 TanoImg 发布版本 |

原图和缩略图使用 `no-cache` 与修订号 ETag，使同一路径替换后的请求重新验证内容。若外部 CDN 覆盖缓存响应头或独立缓存旧文件，仍需在 CDN 侧清除原图和缩略图 URL。启用 JPEG 元数据移除时会清除 EXIF/XMP、IPTC 与注释；含非默认 EXIF 旋转方向的 JPEG 会被拒绝，以避免去除方向标签后显示错误。

上传示例：

```bash
curl -H 'X-API-Key: sk-...' -F 'file=@photo.jpg' http://localhost:3000/api/upload/private
```

认证上传的 `/api/upload/private`、`/api/upload/url`、`/api/upload/urls` 支持查询参数 `visibility=private`（默认）或 `visibility=public`。后台 `/admin/api` 提供完整参数说明、代码高亮示例和错误处理建议。远程导入只允许 HTTP(S)，阻止内网和本机地址。

## 批量上传与资源

管理员网页会将所选图片加入上传队列，最多同时发送 4 张。API Key 客户端也建议使用约 4 路并发；服务端同时处理 4 个上传请求，另有 32 个等待位置。队列满时返回 HTTP 429 和 `Retry-After: 1`，客户端应等待后重试。匿名公开上传仍受每 IP 限流约束。

性能测试已独立整理为 [性能测试报告（图表、实测数据及 1 GiB / 2 GiB 分析）](PERFORMANCE.md)。报告包含上传并发与错误率、万张实际耗时、转换内存压力、图库与原图读取，以及可复现的科研绘图脚本。

## 资源与兼容性

- SQLite 连接池限制为一个连接；上传最多同时处理四个请求。上传流式写入目标磁盘上的临时文件，再以重命名完成落盘，避免把大图读入 Go 堆和跨磁盘复制。
- 公开上传按客户端 IP 限流并检查黑名单。默认使用直连地址；在可信反向代理后运行时设置 `TANOIMG_TRUST_PROXY=true`，并确保外部流量只能经过该代理，才会读取 `X-Forwarded-For`。
- 新上传会按内容识别 JPEG、PNG、GIF、WebP、AVIF、SVG、BMP、ICO、APNG、TIFF。SVG 会校验 XML，响应设置限制性 CSP；迁移来的 SVG 仍可读取。
- 处理图片时最多解码 800 万像素，并且只运行一个编码任务。JPEG、PNG、WebP 可压缩或转换；GIF 和 APNG 始终保留动画。AVIF、SVG、BMP、ICO、TIFF 可直存，但不支持对它们进行压缩或转换。
- 迁移会保留 EasyImg 图片、账户、密钥、设置、审核任务与 IP 黑名单数据。迁移来的审核任务和新产生的通知事件使用 SQLite 队列；启用相应服务后由后台工作器逐项处理。外部审核、通知和版本检查需要部署环境可访问对应服务。
- 图片是软删除，原文件保留。手动部署备份 `data/` 即可备份数据库与图片。

## 开发检查

```bash
npm ci --prefix frontend
npm test --prefix frontend
make build
go test ./...
```

`make build` 会在仓库根目录生成 `./tanoimg` 可执行文件；发布时可用 `make build VERSION=1.2.3` 注入版本号。首次运行可设置 `TANOIMG_ADMIN_PASSWORD`，不设置则自动生成密码并记录在启动日志中；已迁移的 EasyImg 管理员账户保留原密码。

### 前端开发

Vue 单文件页面组件位于 `frontend/src/views/`，路由定义位于 `frontend/src/routes.js`；共享布局、图标和登录状态分别在组件与运行时模块中维护。页面切换不再依赖手写的 DOM 显隐和 History API。原有表单、图片操作及上传队列通过页面生命周期挂载，离开时中止请求并释放队列；上传进行中离开会先提示确认。

`make build` 自动安装缺失的前端依赖并构建前端，再将产物嵌入 Go 二进制。生成目录 `internal/app/web/` 已提交，方便直接运行 Go 测试；修改前端请编辑 `frontend/` 源文件，再运行 `make build`，不要手工编辑生成目录。Docker 使用独立 Node 构建阶段，最终运行镜像仍只有 Go 程序。

公共图库使用 `GET /api/images?scope=public`：即使管理员在访问，也遵循站点的公共展示规则（含管理员明确开启的“在首页展示私有图片”）。管理页使用默认管理员查询，显示全部可管理图片。
