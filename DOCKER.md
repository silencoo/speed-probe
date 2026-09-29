# Docker / NAS 后端部署

**家庭 NAS 推荐 agent 模式**：使用 `compose.agent.yaml` 主动连接 VPS，无需 NAS 公网 IP 或端口映射。复制 `agent.example.json` 到 `data/agent.json`，填写自己的 `name` 和 `controller`，运行 `docker compose -f compose.agent.yaml up -d --build`。NAS 自动生成并保存 `agent.json.identity`，管理员在 Bot 私聊 `/backend pending` 核对日志指纹后，用 `/backend approve 指纹` 批准即可。两端没有启动先后要求，不需要下载接入文件。数据目录需允许容器用户写入。媒体脚本可稍后通过 `.env` 的 `PROBE_SCRIPTS=/data/probe-scripts.json` 加载。完整配置见 [主动连接指南](https://github.com/silencoo/speed-control/blob/main/doc/reverse-deployment.md)。以下内容保留传统 server 模式。

镜像包含 Mihomo、sing-box（含 uTLS、Hysteria/Hysteria2/TUIC）、系统 CA 和时区数据，支持 Linux amd64 / arm64。默认 UID/GID 为 `1000:1000`，不需要 privileged、NET_ADMIN 或 `/dev/net/tun`。

Linux NAS 若出现“宿主机连接节点正常，但容器内 TCP/TLS 超时”，先检查 Docker bridge 的路由、防火墙和 NAT。agent 模式可使用主机网络对照或部署：`docker compose -f compose.agent.yaml -f compose.agent.host.yaml up -d`。此覆盖文件只改变 probe 的网络路径；agent 不监听入站端口，不需要额外 capabilities。后续更新时保留两个 `-f` 参数。主机网络仍受宿主机或路由器的透明代理影响，不自动绕过 TUN。

Bot 与后端都在同一台 NAS 时，优先使用相邻 speed-control 仓库的 `compose.yaml` 和 [同机部署指南](https://github.com/silencoo/speed-control/blob/main/doc/docker.md)。它们通过共享网络命名空间的回环连接通信，无需对外发布端口。

本仓库的 `compose.yaml` 用于**独立后端**，默认直接提供 TLS。其他容器即使在同一个 bridge 网络，也属于非回环对端，必须使用 WSS。

## 1. 构建与目录

在 speed-probe 目录执行：

```sh
cp .env.example .env
mkdir -p data/tls
docker compose build
```

在 `.env` 设置 NAS 用户的 `PUID`、`PGID`，确保该用户能够读写 `data` 目录。可将 `PROBE_DATA_DIR` 改为实际数据集绝对路径，例如 `/mnt/tank/apps/speed/probe` 或 QNAP 的 `/share/Container/speed/probe`。只给此应用目录授权，不要递归修改整个存储池。

## 2. 证书与脚本

将实际证书链和私钥放到：

```text
data/tls/fullchain.pem
data/tls/privkey.pem
```

WSS 地址使用证书覆盖的域名，证书必须被 Bot 信任。可以使用已有 ACME 证书，不要直接使用开发自签证书并关闭验证。私钥只授予容器 UID 读取；证书续签替换文件后重启容器。如果使用私有 CA，需要另外将其配置到控制端的信任链，当前连接文件不支持跳过校验或证书指纹固定。

先只测试延迟、速度、拓扑时，可初始化一个空的脚本目录：

```sh
printf '[]\n' > data/probe-scripts.json
```

需要四项内置媒体检测时，在 speed-control 中按配置导出：

```sh
python -m utils.export_probe_scripts --config config.yaml --out probe-scripts.json
```

然后将导出的文件安装到本后端 `data/probe-scripts.json`。也可以用已构建的 Bot 镜像导出它自带的默认脚本，NAS 无需安装 Python（输出文件必须尚不存在）：

```sh
docker run --rm --user 1000:1000 -v "$PWD/data:/export" \
  --entrypoint python speed-control:local -m utils.export_probe_scripts \
  --config docker/config.example.yaml --out /export/probe-scripts.json
```

这里的 UID/GID 和数据路径按实际配置替换。自定义脚本先审阅再安装。更新目录后重启 Probe；普通客户端不需要授予 `custom_script` 权限。

## 3. 签发并启动

将下方域名替换为 Bot 能访问、与证书匹配的地址：

```sh
docker compose run --rm --no-deps probe clients add \
  -file /data/clients.json -id main-bot \
  -address wss://probe.example.com:8765 -out /data/connection.json
docker compose up -d
docker compose ps
docker compose logs --tail=100
```

NAS 与 Bot 同局域网时可使用局域网 DNS 将该域名解析到 NAS，无需公网端口映射。异地连接则需可达的网络通路。

把 `data/connection.json` 上传到 Bot 管理员私聊，回复 `/backend import`。这个文件包含只签发一次的 token，不能公开。`clients.json` 只保存哈希，丢失连接文件使用 `clients rotate` 重新签发。

## 运维

```sh
docker compose exec probe speed-probe clients list -file /data/clients.json
docker compose restart probe
docker compose stop
```

修改授权/轮换凭据时，同样通过 `docker compose exec probe speed-probe clients ...`，始终使用 `/data/clients.json`。凭据会热加载；脚本和证书需要重启。

`/data` 为持久化目录。升级前备份；本地镜像更新使用 `docker compose build --pull` 后 `docker compose up -d`。Compose 配置重启策略、只读根文件系统、临时目录和日志轮转。端口健康检查只代表监听正常，不代替 Bot 的凭据/能力检测。

默认映射 TCP 8765；被测节点的 UDP 是出站连接，不需要发布 UDP 8765。可通过 `.env` 的 `PROBE_BIND_IP` 限定端口发布到指定 NAS 地址。

Linux host 网络为可选项：添加 `network_mode: host` 并删除 `ports`。这会使用宿主机网络，服务仍需 TLS。Docker 不会自动绕过宿主机或路由器的透明代理。

## TrueNAS / QNAP UI 与镜像发布

TrueNAS SCALE 24.10+ 的自定义 Apps YAML、QNAP Container Station Compose 都可采用此配置。预先准备数据集、凭据和证书，UI 不支持本地构建时去掉 `build`，替换 `image` 为已构建/导入或已发布的版本标签，并将变量替换为实际值及绝对路径。TrueNAS CORE 需 Linux VM。

GitHub Actions 在手动触发或 `v*` 标签时发布 `ghcr.io/<owner>/speed-probe` 的 amd64/arm64 镜像，PR 只构建。新增工作流不等于镜像已发布；首次发布前使用本地构建，发布后按需要设置 Packages 可见性。

手动双架构发布命令（需要先登录自己的镜像仓库）：

```sh
docker buildx build --platform linux/amd64,linux/arm64 \
  -t YOUR_REGISTRY/speed-probe:YOUR_VERSION --push .
```

不使用镜像仓库时可以单架构 `--load`，随后 `docker save` 导出，在 NAS 导入。常见 x86 NAS 选 amd64，ARM64 NAS 选 arm64。
