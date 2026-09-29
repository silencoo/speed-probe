# speed-probe

仓库：[silencoo/speed-probe](https://github.com/silencoo/speed-probe)。本项目基于 [MiaoSpeed](https://github.com/miaokobot/miaospeed)，配套 Telegram 控制端为 [speed-control](https://github.com/silencoo/speed-control)。

speed-probe 是一个通过 WebSocket 接收任务的网络质量测试后端。它不提供代理能力，而是通过 Vendor 适配器连接节点，执行延迟、吞吐、GeoIP、流媒体脚本等测试。

本分支使用 [Mihomo](https://github.com/MetaCubeX/mihomo) 作为 Clash 内核，以支持包括 `2022-blake3` 在内的现代协议。

## 环境要求

家庭 NAS 可使用 `agent -config agent.json -scripts probe-scripts.json` 主动连接 VPS 控制端，无需开放端口；Docker 使用 `compose.agent.yaml`，详见 [部署指南](DOCKER.md)。传统 `server` 模式仍可用。

Docker / TrueNAS / QNAP 部署见 [容器部署指南](DOCKER.md)。容器运行无需宿主机安装 Go；以下要求适用于源码构建。

- Go 1.25.5 或更高版本
- Bash 或 PowerShell
- OpenSSL（可选，用于生成本地开发 TLS 证书）

## 构建

Linux 与 macOS：

```bash
bash ./build.sh
```

Windows PowerShell：

```powershell
.\build.ps1
```

构建结果分别位于：

- Linux/macOS：`dist/speed-probe`
- Windows：`dist/speed-probe.exe`

构建脚本不会修改 `go.mod` 或 `go.sum`。如果系统安装了 OpenSSL，脚本还会在 `dist/certs/` 缺少证书时生成一对开发用自签名证书；已有证书不会被覆盖。

也可以直接构建：

```bash
go build -tags with_utls -o dist/speed-probe .
```

直接构建不会生成 TLS 证书。服务端通过 TLS + 独立 API Token 验证客户端，创建与管理方式见 [客户端认证](AUTH.md)。

## TLS 证书

TLS 私钥不会嵌入源码或二进制。启用 TLS 时必须同时指定证书和私钥：

```bash
./dist/speed-probe server \
  -bind 0.0.0.0:8765 \
  -clients ./clients.json \
  -tls \
  -tls-cert ./dist/certs/speed-probe.crt \
  -tls-key ./dist/certs/speed-probe.key
```

`-tls` 启用服务端 TLS；客户端身份由 API Token 验证。

构建脚本支持从外部路径复制固定证书，而不是生成开发证书：

```bash
SPEED_PROBE_TLS_CERT_FILE=/run/secrets/speed-probe.crt \
SPEED_PROBE_TLS_KEY_FILE=/run/secrets/speed-probe.key \
bash ./build.sh
```

可以通过 `SPEED_PROBE_TLS_OUTPUT_DIR` 修改证书输出目录。脚本会验证证书和私钥是否匹配，并将私钥权限设为仅当前用户可读（PowerShell 除外）。

注意：

- 自动生成的是开发用自签名证书，远程部署需使用控制端信任的证书。
- 客户端固定证书时，不要反复重新生成；应保存证书对并通过部署环境注入。
- `dist/` 和客户端凭据文件已加入 `.gitignore`。

## 启动服务

先创建客户端：`./dist/speed-probe clients add -id main-bot -address ws://127.0.0.1:8765 -out connection.json`。签发连接、撤销、轮换、安装脚本和修改权限的命令见 [AUTH.md](AUTH.md)。

最小示例：

```bash
./dist/speed-probe server \
  -bind 0.0.0.0:8765 \
  -clients ./clients.json
```

常用参数：

| 参数 | 说明 |
| --- | --- |
| `-bind` | TCP 地址或 Unix socket，例如 `0.0.0.0:8765` |
| `-scripts` | 操作者安装的脚本目录 JSON，详见 AUTH.md |
| `-clients` | 客户端配置文件，默认 `clients.json` |
| `-connthread` | 普通连接测试的并发数，默认 `64` |
| `-speedlimit` | 速度测试限速，单位 Byte/s；`0` 表示不限制 |
| `-pausesecond` | 每次速度测试后的暂停秒数 |
| `-nospeed` | 拒绝所有速度测试任务 |
| `-mmdb` | 本地 MMDB 文件列表，以逗号分隔 |
| `-verbose` | 输出详细运行日志；不会记录 token 或完整请求内容 |

查看完整参数：

```bash
./dist/speed-probe server -help
```

## GeoIP 数据库

MMDB 数据库体积较大且会定期更新，因此不进入版本控制。下载后可以在启动时指定：

```bash
./dist/speed-probe server \
  -bind 0.0.0.0:8765 \
  -clients ./clients.json \
  -mmdb './mmdb/GeoLite2-ASN.mmdb,./mmdb/GeoLite2-City.mmdb,./mmdb/GeoLite2-Country.mmdb'
```

程序还提供 MaxMind 更新入口：

```bash
./dist/speed-probe misc -maxmind-update-license 'your-license-key'
```

## 脚本测试

使用本地 Vendor 运行 JavaScript 测试：

```bash
./dist/speed-probe script -file ./example.js
```

默认脚本位于 `engine/embeded/`，会在编译时嵌入程序。

## 客户端对接

客户端使用 [v3 协议](PROTOCOL.md)：WebSocket 握手携带 Authorization Bearer Token，再发送 describe 或 run。describe 分开返回 supported、allowed、limits、scripts；任务返回 accepted、progress、finished，并支持 cancel。

每个任务有独立 ID，每个节点结果带原始 index。任务断线取消，不自动恢复或重试。软件版本与协议版本分开；旧 HMAC 信封和 v2 客户端存储不兼容。迁移步骤见 [AUTH.md](AUTH.md)。

## 项目结构

- **Matrix**：单个结果字段，例如 RTT、出口 IP 或平均速度。
- **Macro**：可被多个 Matrix 复用的一次实际测试任务。
- **Vendor**：节点连接方式的适配层；speed-probe 本身不提供代理服务。

## 安全说明

- 不要将客户端密钥、TLS 私钥或生产配置提交到 Git。
- 生产环境应从 secret manager 或受限文件挂载中读取 TLS 私钥。
- 如果凭据曾推送到公开仓库，仅从最新提交删除并不够，还应轮换凭据并清理历史。

## 许可证

speed-probe 使用 AGPL-3.0 许可证。修改、分发或通过网络提供服务时，请遵守该许可证的相关义务。

主要依赖包括 Mihomo、goja、json-iterator、pion/stun、go-yaml 和 gorilla/websocket；各依赖分别遵循其自身许可证。

## Mihomo / sing-box 双内核

内核版本、原生参数、构建标签及支持范围见 [CORES.md](CORES.md)。
