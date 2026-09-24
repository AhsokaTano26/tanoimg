# TOTP 与 Passkey

## 登录方式

- 尚未启用 TOTP：保留原有用户名／密码登录。
- 已启用 TOTP：密码正确后进入第二步，必须提供动态码或一次性恢复码。第一步不会创建会话或返回管理员 token。
- 已绑定 Passkey：可通过设备指纹、面容、PIN 或安全密钥直接登录，无需密码及 TOTP。服务端强制要求用户验证（UV）和用户在场。
- API Key 的上传用途保持不变，不能用于管理二次验证或 Passkey。

## Docker 配置 Passkey

在部署 `.env` 中填写浏览器实际访问的站点 **origin**：

```dotenv
TANOIMG_PUBLIC_URL=https://img.example.com
```

然后拉取新镜像并重新创建服务：

```bash
docker compose pull
docker compose up -d
```

`deploy/compose.yml` 和根目录本地构建 Compose 均已传递此变量。公网必须 HTTPS，地址不可包含路径、查询参数或用户名密码。开发环境只允许 `http://localhost:端口` 例外。留空时禁用 Passkey，TOTP 不受影响。

服务端固定使用该 origin 的 hostname 作为 WebAuthn RP ID，并校验完整 origin（包括非默认端口）。不从客户端 Host 或 Origin 自动推导可信域名。绑定后更换域名可能使原有 Passkey 不再可用，应先确保仍可使用密码与 TOTP／恢复码登录。

## 绑定 TOTP

1. 打开后台“账户安全”，点击“绑定验证器”。
2. 输入当前密码，扫描二维码或手动输入密钥。
3. 输入验证器的 6 位动态码，验证成功才会启用。
4. 下载／复制 10 个一次性恢复码并离线保存。恢复码只展示一次，服务端仅保存哈希。

标准为 30 秒、6 位、SHA-1，兼容常见验证器。允许设备时间误差前后一个周期；已验证的周期不可重复使用。刚用于登录的动态码不能立即再次用于安全设置，请等待下一周期或使用恢复码。

关闭 TOTP、重新生成恢复码、添加／删除 Passkey，均需再次确认当前密码；已启用 TOTP 时还需动态码或恢复码。重新生成恢复码会让旧码全部失效。启用／关闭 TOTP、重新生成恢复码、添加／删除 Passkey 会撤销其他设备会话，当前会话保留。修改密码需要同样的二次验证，并撤销全部会话和待完成挑战。

## Passkey 管理

“账户安全 → 添加 Passkey”，输入名称与身份验证信息后，由浏览器和操作系统完成设备验证。最多保存 20 个凭证，可查看添加／最近使用时间，也可单独删除。私钥保留在设备或系统密码管理器，服务器仅保存验证所需凭证信息。

密码与 TOTP 是备用登录方式。没有验证器但保留了 Passkey 时，仍可登录；修改安全设置时需当前密码和一个恢复码。两种方式与恢复码都丢失时，应恢复可信的完整备份；本版本不提供绕过验证的网页重置入口。

## 存储与保护

- TOTP 密钥、待完成挑战及 Passkey 凭证使用 AES-256-GCM 加密，认证数据与用户、用途或凭证 ID 绑定。
- 加密密钥为 `/data/auth.key`，权限 0600。**备份必须包含整个数据卷，尤其是 auth.key 与 tanoimg.db**。不要只恢复数据库或删除 auth.key；已有认证数据但密钥丢失时启动会报错。
- 登录挑战有效期 5 分钟，绑定浏览器 HttpOnly Cookie；注册挑战绑定当前管理员会话。每个挑战最多尝试 5 次，成功后不可重放；Passkey 完成请求只允许一次校验。
- 认证写请求每 IP 每 5 分钟最多 60 次，返回 429 和 Retry-After；与公共上传限流独立。
- 认证响应禁止缓存；敏感设置需要重新验证密码及已启用的第二因素。
- 本服务仍按单实例部署设计。SQLite 存储认证状态，不新增外部缓存服务。

## API 概览

| 接口 | 用途 |
|---|---|
| `GET /api/auth/methods` | Passkey 可用状态 |
| `POST /api/auth/login` | 密码登录；启用 TOTP 时返回 `requiresTOTP`、`challenge` |
| `POST /api/auth/totp` | 提交 `challenge` 和 `code`，浏览器必须保留第一步 Cookie |
| `POST /api/auth/passkey/begin`、`finish` | Passkey 直接登录 |
| `GET /api/admin/security` | 当前账户的 TOTP、恢复码数量和 Passkey 列表 |
| `POST /api/admin/totp/setup`、`enable` | 建立并确认 TOTP |
| `DELETE /api/admin/totp` | 关闭 TOTP |
| `POST /api/admin/recovery-codes` | 重新生成恢复码 |
| `POST /api/admin/passkeys/register/begin`、`finish` | 注册 Passkey |
| `DELETE /api/admin/passkeys/{id}` | 删除当前账户的 Passkey |

需要重新确认身份的接口接收 `password`、`code`；首次 TOTP 绑定无须 code。Passkey 注册 begin 还需 `name`，finish 接收 begin 返回的 `challenge` 与浏览器返回的 `credential`。脚本使用密码登录时也必须处理 TOTP 第二步；原有未启用 TOTP 的接口返回保持兼容。

## 验证范围

Go 集成测试覆盖：动态码／恢复码登录、已用验证码拒绝、恢复码轮换、过期／尝试上限、旧会话撤销、密码修改不能绕过 TOTP、认证限流、重启后读取密钥、真实 ECDSA WebAuthn 注册与登录、浏览器绑定、错误 origin、缺少 UV、凭证删除和重放拒绝。

可见浏览器使用独立测试目录检查 UI 与两步登录。真实系统 Passkey 绑定需要用户亲自完成设备认证，不由自动化代替；服务端完整签名流程已用测试生成的凭证验证。
