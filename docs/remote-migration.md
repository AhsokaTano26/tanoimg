# 本地备份通过 TanoImg API 迁移到远端

本脚本保留 EasyImg 原始 `id`、`uuid`、`filename` 和 `/i/<filename>` 路径，不通过普通图片上传接口重新生成 ID。旧域名不变时，完整图片 URL 也不变；更换域名时仅域名改变。

## 1. 准备远端

1. 更新到包含迁移 API 的 TanoImg 镜像并重新部署。仅将 Docker Hub 的 `latest` 推送完成，不代表 Zeabur 已拉取新镜像。
2. 在 Zeabur 显式挂载 **`/data` 持久卷**。上传、准备和最终使用的数据目录都必须位于该卷内。
3. 暂时保持当前 `TANOIMG_DATA` 不变（通常为 `/data`）。当前管理员只用于验证迁移权限，完成后仍用 EasyImg 原账户登录。
4. 预留磁盘：准备期间需同时容纳 TAR 归档与解包文件，通常至少为备份的两倍加数据库开销。不支持硬链接的文件系统还会多复制一份原图，峰值约三倍。准备成功后自动删除远端归档和解包暂存，保留最终数据及迁移状态。失败时保留暂存以便重试。

这是独立数据集迁移，不会合并或覆盖当前正在使用的图库。即使远端已有图片也不会被修改；切换后旧图库不会自动出现在新图库里。

## 2. 检查本地备份

需要 Python 3，无第三方依赖。源备份保持不变，停止原 EasyImg 写入后复制完整的 `db/` 与 `uploads/`：

```bash
python3 scripts/migrate_easyimg.py --source ./tmp --dry-run
```

检查会按 NeDB 日志顺序还原最新记录，处理更新与删除标记，检查原图存在性、文件大小、UUID/文件名、重复引用和符号链接。输出仅含数量，不打印密码哈希、API Key 或设置中的密钥。

## 3. 上传

```bash
python3 scripts/migrate_easyimg.py \
  --source ./tmp \
  --url https://your-image-host.example \
  --username admin
```

`--username` 是**当前远端**管理员用户名。脚本交互询问其密码；若启用了 TOTP，再输入动态码或一次性恢复码。普通上传 API Key 无法执行迁移。不要把密码作为命令行参数；自动化时可通过 `TANOIMG_MIGRATION_PASSWORD` 环境变量提供，脚本不会将其保存到磁盘。

远端必须使用 HTTPS（本机回环测试可用 HTTP）。脚本拒绝 HTTP 重定向，避免将认证信息发送到其他地址。

脚本会在 `tmp/.tanoimg-migration/` 创建权限受限的归档缓存，包含原账户和密钥，请像原备份一样保管。整个 `tmp/` 必须排除在 Git 与 Docker 构建上下文之外。脚本只压缩 NeDB 日志中的历史版本，不做图片重编码；TAR 为无压缩归档，图片字节保持原样。

每块最多 8 MiB，断线后读取远端偏移量再续传。中断后重新运行**同一条命令**即可；换了备份内容时会生成新的归档标识。缓存不保存管理员密码或会话。

若上传内容损坏，或需要清除本次失败/未完成的远端暂存：

```bash
python3 scripts/migrate_easyimg.py \
  --source ./tmp --url https://your-image-host.example \
  --username admin --restart-upload
```

该选项不会删除已经准备完成的数据集，也不能取消正在进行的转换。归档上传完成后，服务器在后台校验 SHA-256、解包并转成 SQLite，脚本轮询等待结果。此阶段关闭脚本不会取消准备，重新运行可以查询结果。服务器重启中断了准备时，重试会从已上传归档重新准备。

## 4. 切换数据目录

成功时脚本会打印类似：

```text
TANOIMG_DATA=/data/imports/<归档SHA256>/ready
```

**复制脚本实际输出的整行**，在 Zeabur 修改这个环境变量，然后重新部署。不要先猜测路径或在显示成功前切换；本机验证脚本输出的临时路径也不能用于 Zeabur。

切换后：

- 使用原 EasyImg 账户及密码登录；原 API Key 保留。
- 检查图片数量、公开/私有状态、回收站，抽查原 `/i/...` 地址。
- 再重启一次，确认持久卷中的数据仍然存在。
- 当前空站点的登录会话、TOTP 和 Passkey 不会合并到原 EasyImg 账户；需要时为原账户重新绑定。
- 导入的公开上传、审核和通知设置也会生效。若不希望立即恢复原外部服务，切换前应先安排好这些设置。
- 如果迁回原域名，继续设置正确的 `TANOIMG_PUBLIC_URL`，并在 Zeabur/域名服务商完成域名绑定。

新数据位于原持久卷的子目录中，**不要删除包含 `ready` 的父目录或整个 `/data`**。原数据库文件仍在，回滚可将 `TANOIMG_DATA` 改回切换前的值。切换后不要反复导入同一备份；已产生新上传时，回滚前先保留新数据备份。确认迁移无误后可自行清理本地归档缓存；原 EasyImg 备份建议保留。

## 可选参数及验证

- `--prepare-only`：仅生成本地归档，不连接远端。
- `--cache-dir /path/to/cache`：改变本地缓存目录。
- `--dry-run`：只检查原始数据，不打包、不上传。

测试：

```bash
python3 -m unittest discover -s scripts -p 'test_migrate_easyimg.py'
go test ./internal/app -run 'TestRemoteMigration|TestMigrationArchive' -count=1
```

在隔离的本机服务验证实际备份（不向公网发送、不启用导入的通知/审核工作器）：

```bash
go build -o /tmp/tanoimg-migration-check ./cmd/tanoimg
python3 scripts/verify_migration_local.py \
  --source ./tmp --binary /tmp/tanoimg-migration-check
```

该验证逐一核对迁移前后的 ID、UUID、文件名、大小、删除状态、所有图片 SHA-256、账户密码哈希、API Key 和设置，并确认原运行数据库未被修改。
