# GitHub Actions → Docker Hub → 服务器

## 1. 配置 Docker Hub 和 GitHub

在 Docker Hub 创建镜像仓库，例如 `your-user/tanoimg`，创建对此仓库有写权限的 Personal Access Token。Token 不要写入 Git、Dockerfile 或聊天记录。

GitHub 仓库 → Settings → Secrets and variables → Actions：

| 类型 | 名称 | 内容 |
|---|---|---|
| Secret | `DOCKERHUB_USERNAME` | Docker Hub 登录用户名，也是镜像命名空间 |
| Secret | `DOCKERHUB_TOKEN` | Docker Hub Token |

镜像名自动拼接为 `${DOCKERHUB_USERNAME}/tanoimg`，无需额外 Variables。

工作流位于 `.github/workflows/docker.yml`。配置缺失时会明确报错，不会推送到默认占位仓库。GitHub Token 只需要读取仓库内容权限。

## 2. 触发构建

| 触发方式 | 镜像标签 | 推送 |
|---|---|---|
| 向 `main` push | `latest`、`edge`、`sha-7位提交ID` | 是 |
| push `v1.2.3` 标签 | `v1.2.3`、`1.2.3`、`1.2`、`latest`、SHA | 是 |
| push `v1.3.0-rc.1` | 原始标签、预发布版本、SHA；不更新 `latest` | 是 |
| Actions → Docker Hub → Run workflow | 按所选分支或标签生成标签；普通分支至少有 SHA | 是 |
| 向 `main` 提交 PR | PR、SHA | 否；不登录 Docker Hub |

版本标签使用 SemVer，例如 `v1.2.3`。正式发布示例：

```bash
git tag v1.2.3
git push origin v1.2.3
```

`main` 每次构建都会更新 `latest`；发布旧版本或手动重跑旧正式版本也可能更新 `latest`，生产环境建议固定版本标签或镜像 digest。

测试作业先安装前端依赖、运行前端测试、构建静态资源并执行 Go 测试。通过后 Buildx 构建 `linux/amd64` 和 `linux/arm64`，推送同一个多架构镜像。前端和 Go 在构建机本机架构运行，Go 交叉编译目标程序；QEMU 只用于目标运行镜像的少量准备命令。程序版本号来自镜像元数据。GitHub Actions 缓存复用构建层。

## 3. 服务器部署

服务器安装 Docker Engine 和 Compose 插件，然后复制仓库里的 `deploy/compose.yml` 与 `deploy/.env.example` 到固定目录，例如 `/opt/tanoimg/`。不需要安装 Go、Node.js 或复制源码。

```bash
cd /opt/tanoimg
cp .env.example .env
chmod 600 .env
# 编辑 .env：镜像、强密码、监听地址等

docker compose pull
docker compose up -d
docker compose logs --tail=100 tanoimg
```

`.env` 示例：

```dotenv
TANOIMG_IMAGE=your-user/tanoimg:1.2.3
TANOIMG_ADMIN_PASSWORD='替换为强密码'
TANOIMG_BIND=127.0.0.1
TANOIMG_PORT=3000
TANOIMG_TRUST_PROXY=false
```

- 默认仅绑定本机 `127.0.0.1:3000`，适合宿主机 Nginx/Caddy 反向代理；域名及 HTTPS 在反向代理配置。
- 只有可信代理会覆盖转发头、且应用无法绕过代理访问时，才设置 `TANOIMG_TRUST_PROXY=true`，以正确识别公共上传 IP。
- 需要直接访问时可设 `TANOIMG_BIND=0.0.0.0` 并配置防火墙；管理员登录建议使用 HTTPS。
- 私有 Docker Hub 仓库需要先在服务器执行 `docker login`，使用有读取权限的 Token。
- 管理员密码环境变量用于首次初始化，已有账号不会被该变量重置。
- `/data` 使用 Compose 命名卷，含 SQLite 数据库和图片。固定部署目录与 Compose 项目名，避免切换目录后创建另一个空卷。
- `.env` 已排除出 Git 和 Docker 构建上下文。示例文件没有真实凭证。

仓库根目录的 `docker-compose.yml` 继续用于本地源码构建；服务器使用 `deploy/compose.yml`，不要把两个文件合并执行。

## 4. 更新和回滚

先备份数据，修改 `.env` 的 `TANOIMG_IMAGE` 为目标版本，然后：

```bash
docker compose pull
docker compose up -d
```

回退镜像时改回此前版本再执行上述命令。若版本升级包含不兼容的数据迁移，需要同时恢复对应备份，不能仅更换镜像。

备份完整数据卷时应先停止服务，确保数据库与原图一致，再复制整个卷。不要在运行中仅复制 `tanoimg.db`，也不要执行 `docker compose down -v`，该命令会删除数据卷。

GitHub Actions 只负责构建与推送镜像，服务器更新由以上命令完成。本工作流不会自动 SSH 到服务器。

## 参考

- https://docs.docker.com/build/ci/github-actions/manage-tags-labels/
- https://docs.docker.com/build/building/multi-platform/
