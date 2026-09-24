# 从 EasyImg 迁移并部署到 Zeabur

使用 GitHub Actions 已发布的 Docker Hub 镜像，无需在 Zeabur 重新构建。迁移期间保留原 EasyImg 和备份，验证完成后再切换域名。

也可以从本地直接调用 TanoImg 迁移 API 上传，省去手动文件管理操作：见 [远程迁移指南](remote-migration.md)。以下保留手动上传方式。

## 1. 准备完整备份

停止 EasyImg 写入，备份以下目录，保持同一个时间点：

```text
easyimg-backup/
├── db/
│   ├── images.db
│   ├── users.db
│   ├── apikeys.db
│   └── ...
└── uploads/
    └── 原始图片文件
```

`db/` 是 NeDB 数据，不能直接当作 TanoImg 的 SQLite 使用。缺少 `uploads/` 就无法恢复原图。迁移前应保留整个 `db/`，包括设置、审核任务、IP 黑名单文件。

## 2. 本地转换

在本仓库执行；将示例路径替换成备份的绝对路径，目标 `./zeabur-data` 必须是尚未使用的新目录：

```bash
make build
./tanoimg migrate -from /absolute/path/easyimg-backup -data ./zeabur-data
```

命令成功后检查 JSON 报告：`missingFiles` 应为 `0`。若非零，先补齐源备份里的原图并在正式启用前重新迁移。报告计数反映处理的 NeDB 文档版本，不一定等于最终去重后的记录数量。

迁移不会初始化随机管理员；导入的账户保留原密码。若源数据没有有效账户，后续首次启动才会按环境变量或随机密码创建管理员。

迁移进程退出后，打包完整输出目录。不要在 TanoImg 正运行并写入该目录时打包：

```bash
tar -czf tanoimg-data.tar.gz -C ./zeabur-data .
```

包内应直接包含 `tanoimg.db`、`auth.key`、`uploads/` 等内容，不应多套一层 `zeabur-data/`。不要把数据库、密钥或图片打包进公开 Docker 镜像或提交到 Git。

## 3. 创建 Zeabur 服务

添加服务 → Docker Images，设置：

| 项目 | 值 |
| --- | --- |
| 镜像 | `你的DockerHub用户名/tanoimg:sha-实际7位提交号`，也可用 `latest` |
| HTTP 端口 | `3000`，端口名称可设为 `web` |
| 持久卷 ID | `data` |
| 持久卷挂载路径 | `/data` |
| `TANOIMG_ADDR` | `:3000` |
| `TANOIMG_DATA` | 暂设 `/data/bootstrap` |
| `TANOIMG_PUBLIC_URL` | 最终访问地址，如 `https://img.example.com`；尚未确定可先留空 |

先使用 `/data/bootstrap` 启动临时实例，以便文件管理可用。暂不向它上传图片或修改设置。该目录中首次生成的账户仅属于临时实例；正式切换后使用迁移账户。

镜像的 `VOLUME /data` 声明不能替代在 Zeabur 中配置持久卷。保持单实例运行；本项目使用本地 SQLite 和本地图片目录，不适合多个实例分别持有独立卷。

仅在确认入口代理覆盖转发头、没有不可信的直连入口时配置 `TANOIMG_TRUST_PROXY=true`，否则保持默认。这个配置影响按 IP 限流及自动封禁。

## 4. 上传并切换数据

在同一服务的文件管理中，将 `tanoimg-data.tar.gz` 上传到 `/data`。使用 Zeabur 的命令执行功能：

```sh
# 先检查包内路径，不应出现绝对路径或 ../
tar -tzf /data/tanoimg-data.tar.gz

# mkdir 不带 -p：如果目标已存在则停止，换新目录，不覆盖现有数据
mkdir /data/migrated && tar -xzf /data/tanoimg-data.tar.gz -C /data/migrated
```

确认解压成功，且存在 `/data/migrated/tanoimg.db`、`/data/migrated/auth.key` 和 `/data/migrated/uploads/`。容器运行用户 `tanoimg` 必须能读写该目录及文件；遇到 `permission denied` 先检查卷所有权，不要使用 `chmod 777`。若命令执行使用 root，管理员可运行：

```sh
chown -R tanoimg:tanoimg /data/migrated
chmod 700 /data/migrated
chmod 600 /data/migrated/tanoimg.db /data/migrated/auth.key
```

将环境变量 `TANOIMG_DATA` 改为 `/data/migrated`，重新部署服务。启动命令保持镜像默认值，不要设置为 `migrate`，也不要在每次启动时重复执行迁移。

## 5. 验证后切换域名

- 使用原 EasyImg 管理员账号登录，抽查公开、私有、回收站图片和原图链接。
- 检查图库最终数量、设置、API Key；可以用原 Key 上传一张测试图片。
- 重启服务，再确认图片和账户仍存在，验证持久卷生效。
- 配置原域名及 HTTPS 后，旧 `/i/...` 图片路径可继续使用。旧登录会话需要重新登录。
- 使用 Passkey 时，设置 `TANOIMG_PUBLIC_URL` 为实际 HTTPS origin（不带路径），再绑定设备。
- 验证完成后备份完整 `/data/migrated`，包含 `auth.key`；根据空间需要再清理上传的压缩包和临时 bootstrap 目录。

只有没有有效迁移账户时才会初始化密码：优先使用 `TANOIMG_ADMIN_PASSWORD`；没有或为空则生成 12 位随机密码，在 Zeabur 服务日志的 `initial administrator` 行读取。修改该变量不会重置已迁移账号。

正式启用后不要再次导入旧备份，否则旧记录可能覆盖新修改。需要回滚时先停止新服务写入，保留新数据备份，再恢复原服务和域名。

## 官方操作文档

- [自定义 Docker 镜像、端口、环境变量和存储卷](https://zeabur.com/docs/zh-CN/deploy/methods/custom-docker-image)
- [文件管理与上传目录](https://zeabur.com/docs/zh-CN/operations/data/file-management)
- [存储卷](https://zeabur.com/docs/en-US/operations/data/volumes)
- [备份与还原](https://zeabur.com/docs/zh-CN/operations/data/backup-restore)
