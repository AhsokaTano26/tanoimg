# 本机隔离压测

仅对脚本创建的临时实例压测，不使用现有图库。需要 Go、Python 3、Docker；运行时占用本机 3042 端口。测试创建大量文件并在每轮结束删除自己的临时目录和匿名卷，建议预留 30 GiB 以上空间。

```sh
make build
go build -o /tmp/tanoimg-loadtest ./tools/loadtest
mkdir -p /tmp/tanoimg-perf-context
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags '-s -w' -o /tmp/tanoimg-perf-context/tanoimg-perf-linux ./cmd/tanoimg
cp tools/loadtest/Dockerfile /tmp/tanoimg-perf-context/Dockerfile
docker build -t tanoimg-perf:local /tmp/tanoimg-perf-context
python3 tools/loadtest/run.py --out /tmp/tanoimg-sweep.jsonl
```

Linux 架构需与 Docker 主机一致，x86 主机将 `arm64` 改为 `amd64`。脚本通过管理员登录 API 创建临时 API Key；凭证不写入结果。

默认：原生服务、1 GiB/2 GiB 容器（均 2 vCPU、无 swap），随机 RGBA PNG 256×256 和 724×724，1/4/16/32/64/128 个闭环客户端，每组发送窗口 8 秒，超时 30 秒，不重试。普通上传使用出厂配置，关闭转换与压缩。每组独立新库，不并行运行各组。

其他测试示例：

```sh
# 10,000 张固定数量的上传，重复三次
python3 tools/loadtest/run.py --concurrency 4 --count 10000 --repeat 3 --out /tmp/tanoimg-batch.jsonl
# 约 8 百万像素图片，启用 JPEG 转换，测试解码内存
python3 tools/loadtest/run.py --profiles 1g,2g --sides 2828 --concurrency 4 --seconds 20 --convert-jpg --out /tmp/tanoimg-convert.jsonl
# WebP 转换的重负载样本（4 张约 8 百万像素图片）
python3 tools/loadtest/run.py --profiles 1g,2g --sides 2828 --concurrency 4 --count 4 --convert-webp --out /tmp/tanoimg-webp.jsonl
# 连续 WebP 转换，观察重复处理后的内存峰值
python3 tools/loadtest/run.py --profiles 1g,2g --sides 2828 --concurrency 4 --seconds 30 --convert-webp --out /tmp/tanoimg-webp-sustained.jsonl
# 先用 API 上传 1000 张公开图片，然后查询每页 50 条图库
python3 tools/loadtest/run.py --sides 256 --concurrency 16,64,256 --workload gallery --out /tmp/tanoimg-gallery.jsonl
# 同一张热缓存原图下载，读完整响应体
python3 tools/loadtest/run.py --sides 724 --concurrency 16,64,256 --workload download --out /tmp/tanoimg-download.jsonl
```

## 指标口径

- `success_rps`：完整读完响应体且 HTTP 200 的请求数 / 实际压测耗时（含最后一批完成时间）。429、其他 HTTP 错误及传输错误单独统计，状态 `0` 表示传输/读响应失败。
- `p50/p95/p99_ms`：成功请求端到端延迟；不把快速拒绝请求混入成功延迟。
- `upload_mib_s`：成功图片的实际字节数 / 耗时；不含 multipart 及 TCP 开销。
- `server_peak_rss_mib`：原生服务由 `ps` 每 100 ms 采样（可能遗漏短峰值），Linux 使用服务进程 `/proc/1/status` 的 VmHWM。不含独立客户端内存。
- `cgroup_peak_mib`：Linux 内存组历史峰值，包含文件页缓存和内核内存；不是 Go 堆，不应与 RSS 混用。`memory_events` / `oom_killed` 记录是否撞内存限制。`cpu` 包含 CPU quota 节流时间。
- `server_cpu_cores`：服务端 CPU 秒数 / 外层测量窗口（含客户端启动和图片生成，略大于 HTTP 测试窗口）；1 表示约占满一核。
- 测量结束后等待 2 秒，让已断开的客户端对应处理有机会收尾；停服后用 SQLite 检查实际行数和 `quick_check`，原生场景额外核对图片文件数。`stored_minus_acknowledged` 非零说明落库记录与客户端成功确认数不一致；不能据此宣称零损失或安全重试。
- 读取测试的服务进程内存峰值包含准备 1000 张图片阶段。下载重复同一文件，属于热缓存上界，不代表整个图库的冷读性能。

闭环测试有客户端数上限，不测无限制开放到达率；“峰值”只针对所测工作负载和并发范围。没有模拟公网 TLS、代理、远程盘、长时间稳态、多人共享宿主机等条件。容器使用 Docker 虚拟机的本地匿名卷，非 macOS bind mount，也非 Zeabur 实际机器。

## 科研图表

运行 `plot_results.py` 可从已归档的 JSONL 生成性能图表，不会启动服务或重新压测。
依赖单独保存在 `requirements-plot.txt`，不影响压测脚本的标准库运行方式。
安装、字体参数、PNG/PDF 图表和结果分析见根目录 [性能测试报告](../../PERFORMANCE.md#图表复现与字体)。
