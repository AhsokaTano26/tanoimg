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

### 公共上传自动封禁

在“公共上传”中配置启用状态、统计时间窗（1–1440 分钟）及最多请求次数（1–100000）。默认启用：每 IP 从首个请求开始的固定 10 分钟窗口内最多 120 次，**第 121 次自动加入 IP 黑名单**。文件和 URL 请求共用计数，失败、被限流的请求也计入；公开上传关闭时不计数。管理员／API Key 认证上传不受此策略限制。

计数和黑名单持久化在 SQLite，重启不会清除。管理员可在“IP 黑名单”解除封禁，解除时同时重置该 IP 的频率及自动封禁计数。原有每分钟限流仍然独立生效，客户端应遵守 `Retry-After`，不要立即重复请求。

## 从 EasyImg 迁移

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

上传示例：

```bash
curl -H 'X-API-Key: sk-...' -F 'file=@photo.jpg' http://localhost:3000/api/upload/private
```

认证上传的 `/api/upload/private`、`/api/upload/url`、`/api/upload/urls` 支持查询参数 `visibility=private`（默认）或 `visibility=public`。后台 `/admin/api` 提供完整参数说明、代码高亮示例和错误处理建议。远程导入只允许 HTTP(S)，阻止内网和本机地址。

## 批量上传与资源

管理员网页会将所选图片加入上传队列，最多同时发送 4 张。API Key 客户端也建议使用约 4 路并发；服务端同时处理 4 个上传请求，另有 32 个等待位置。队列满时返回 HTTP 429 和 `Retry-After: 1`，客户端应等待后重试。匿名公开上传仍受每 IP 限流约束。

### 本机实测（2026-09-25）

测试业务版本 `292fd2b`。本机 Apple M4 Pro / 14 核 / 24 GiB，macOS + 本地 APFS；客户端与生产服务分进程运行。1 GiB、2 GiB 场景使用 Docker Desktop Linux arm64 的实际容器硬限制，均为 **2 vCPU、禁用容器 swap、本地匿名卷**。容器网络和存储经过虚拟机，不能将其与原生差距全部归因于内存，也不能当作 Zeabur 实机数据。

API Key 上传有效随机像素 PNG，每张约 256 KiB 或 2 MiB；默认关闭压缩/转换、无客户端重试。每组新库，并发表示在途请求数而非在线用户数；依次测试 1/4/16/32/64/128 个闭环客户端，各发送 8 秒并等待收尾。**成功吞吐仅计完整收到 HTTP 200 的请求，429 和传输错误均不计入。**

#### 上传峰值与过载边界

下表取各环境、各图片大小的“本轮零错误最高吞吐”，是短测观测值，不是长期稳定性保证。RSS 只统计服务进程，不含客户端；原生为每 100 ms 采样，容器为内核记录的进程高水位。

| 环境 | 单图 | 峰值客户端并发 | 成功张/秒 | 成功 MiB/s | P95 延迟 | 服务 RSS 峰值 | 万张估算 |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 本机原生 | 256 KiB | 16 | 2007.2 | 502.7 | 13.0 ms | 32.4 MiB | 5.0 秒 |
| 本机原生 | 2 MiB | 4 | 442.5 | 884.7 | 12.2 ms | 29.8 MiB | 22.6 秒 |
| 1 GiB / 2 vCPU | 256 KiB | 16 | 426.3 | 106.8 | 29.3 ms | 26.1 MiB | 23.5 秒 |
| 1 GiB / 2 vCPU | 2 MiB | 16 | 67.4 | 134.8 | 219.0 ms | 22.6 MiB | 148.4 秒 |
| 2 GiB / 2 vCPU | 256 KiB | 4 | 454.0 | 113.7 | 11.7 ms | 26.1 MiB | 22.0 秒 |
| 2 GiB / 2 vCPU | 2 MiB | 4 | 85.8 | 171.5 | 58.6 ms | 23.2 MiB | 116.6 秒 |

- 所测环境在 1–32 路上传均未出现 HTTP/传输错误；32 路不是推荐并发，也不是物理极限。处理槽固定为 **4 个执行 + 32 个等待**，等待槽不增加实际处理能力。
- 本机小图 4 → 16 路从 1,887.5 提升至 2,007.2 张/秒，仅增加约 6.3%，P95 从 4.1 ms 升至 13.0 ms；大图 4 路最快。通常从 **4 路**开始，按实机测量调整。
- 64/128 路属于过载：本机错误率约 59%–99%，容器约 13%–51%；提高并发可能使成功吞吐反而下降。本机部分请求已落库但客户端未完整确认，直接重传可能产生重复图片。429 应遵守 `Retry-After` 退避，不要无限堆积请求。

#### 10,000 张实际批量上传

固定 **4 路并发**，每组完整上传 10,000 张，没有重试。以下六组均 **0 错误、落库 10,000 条、SQLite 检查正常**，时间为实测，不是短测折算。CPU 数值只统计服务进程/容器消耗，不是压测客户端或 Docker Desktop 整台虚拟机的总占用；1.0 表示约占满一核。

| 环境 | 单图 | 实际耗时 | 平均张/秒 | 服务 RSS 峰值 | 平均 CPU 核数 |
| --- | --- | ---: | ---: | ---: | ---: |
| 本机原生 | 256 KiB | 5.1 秒 | 1956.4 | 31.8 MiB | 2.57 |
| 本机原生 | 2 MiB | 23.6 秒 | 424.1 | 30.5 MiB | 3.27 |
| 1 GiB / 2 vCPU | 256 KiB | 43.7 秒 | 228.9 | 25.9 MiB | 0.15 |
| 1 GiB / 2 vCPU | 2 MiB | 248.1 秒 | 40.3 | 24.3 MiB | 0.18 |
| 2 GiB / 2 vCPU | 256 KiB | 26.2 秒 | 381.1 | 26.4 MiB | 0.21 |
| 2 GiB / 2 vCPU | 2 MiB | 123.5 秒 | 80.9 | 23.8 MiB | 0.27 |

两种容器限制下，所测普通上传均达到“10 分钟万张”；这不构成公网部署保证。完整批次会经历文件缓存回收，不能只依据 8 秒峰值安排持续批量任务。

#### 1 GiB / 2 GiB 的容量解释

这里的限制是应用容器内存，不是整台服务器总内存。整机只有 1 GB/2 GB 时，还需扣除操作系统、Docker、代理等占用；本测试也没有模拟 Zeabur 共享 CPU 或网络限额。

普通上传并发阶梯中，1 GiB 容器服务进程 RSS 最高约 31.5 MiB，2 GiB 约 31.7 MiB，均未发生 OOM。Linux cgroup 总内存包含文件页缓存，1 GiB 场景会触及限额并回收缓存，不能把它解释为 Go 堆占满。RSS 较低也不意味着可以无限增加连接数；代码中的处理队列、磁盘持续写入与网络仍限制吞吐。更大内存不会自动增加上传处理槽。

#### 接近像素上限的转换压力

使用 2828×2828（7,997,584 像素，源 PNG 约 30.5 MiB）的随机图片，接近代码的 800 万像素转换上限，4 路客户端。JPEG 发送窗口 20 秒，WebP 持续窗口 30 秒，均等待已发请求完成；各组 0 错误、无 OOM。WebP 另有每组 4 张的短样本，原始记录一并保留。

| 容器限制 | 转换 | 完成张数 | 成功张/秒 | P95 延迟 | 服务 RSS 峰值 |
| --- | --- | ---: | ---: | ---: | ---: |
| 1 GiB / 2 vCPU | PNG → JPEG | 52 | 2.45 | 1.72 秒 | 142.3 MiB |
| 2 GiB / 2 vCPU | PNG → JPEG | 53 | 2.47 | 1.62 秒 | 140.4 MiB |
| 1 GiB / 2 vCPU | PNG → WebP | 16 | 0.43 | 10.12 秒 | 595.4 MiB |
| 2 GiB / 2 vCPU | PNG → WebP | 16 | 0.43 | 10.02 秒 | 600.7 MiB |

转换只有一个执行槽，用于控制解码/编码像素缓冲区的内存。**1 GiB 在本轮样本中可运行，但 WebP 大图转换明显挤占内存余量；2 GiB 提供更多余量，吞吐却没有随之翻倍。** 这两类大图转换均达不到 16.67 张/秒，不能按普通流式上传推算“10 分钟万张”。未测所有格式、图片内容和长时间混合负载，595–601 MiB 不是严格的最坏内存上界。

#### 图库与原图读取

图库预置 1,000 张公开图片，查询第一页 50 条；分别测试 16/64/256 路，各发送 8 秒。原图下载仅测试本机重复读取同一张约 2 MiB 图片的热缓存路径。所有读取组均 0 错误，下表取各工作负载观测到的最高成功吞吐。

| 环境 | 工作负载 | 客户端并发 | 成功请求/秒 | P95 延迟 | 服务 RSS 峰值 |
| --- | --- | ---: | ---: | ---: | ---: |
| 本机原生 | 图库分页 | 256 | 3948.0 | 135.6 ms | 47.7 MiB |
| 1 GiB / 2 vCPU | 图库分页 | 16 | 2344.3 | 12.6 ms | 23.6 MiB |
| 2 GiB / 2 vCPU | 图库分页 | 256 | 2341.6 | 227.3 ms | 35.9 MiB |
| 本机原生 | 原图热读 | 16 | 2873.2 | 7.0 ms | 33.2 MiB |

原图热读峰值约 5746 MiB/s，仅反映本机缓存/回环上界；256 路时因收尾长尾，成功吞吐降至约 523 次/秒。这不是全图库冷读、百万记录图库或混合读写压力的结论。图库 16 路通常已接近本轮峰值，提高到 256 路主要增加等待延迟。

#### 10 分钟上传 10,000 张需要什么

目标至少为 `10000 / 600 = 16.67 张/秒`。忽略协议开销，单图 256 KiB 需要约 **4.17 MiB/s（35 Mbit/s）**有效上行；单图 2 MiB 需要约 **33.33 MiB/s（280 Mbit/s）**。100 Mbit/s 上行传 10,000 张 2 MiB 图片，理论上至少约 **28 分钟**，即使服务端足够快也无法达到 10 分钟目标。

本机 HTTP 回环不包含公网 TLS、代理、CDN 和远程磁盘；测试写入也不是每张图片原文件都执行 `fsync` 后的断电持久性基准。请根据平均图片大小、真实上行和目标机器重新测量。

详细原始记录、计算口径和测试局限见 [性能测量记录](docs/benchmarks/README.md)，可复现工具见 [tools/loadtest](tools/loadtest/README.md)。汇总命令：

```bash
python3 tools/loadtest/summarize.py
```

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
