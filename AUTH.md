# 客户端认证 v3

speed-control 管理 Telegram 用户，speed-probe 管理独立客户端。远程使用 WSS，握手通过 `Authorization: Bearer <token>` 认证。此认证不证明后端程序未修改或测速结果真实。

## 创建并接入

```powershell
.\dist\speed-probe.exe clients add -id main-bot -address ws://127.0.0.1:8765 -out connection.json -capabilities ping,script,topo,speed -max-nodes 300 -max-jobs 2 -max-seconds 600 -max-scripts 32
.\dist\speed-probe.exe server -bind 127.0.0.1:8765 -clients clients.json
```

在管理员与 Bot 的私聊中上传 connection.json，回复发送 `/backend import`。导入时调用 describe 验证凭据、获取能力与限制。远程改用 wss://，服务端配置 -tls/-tls-cert/-tls-key，或让本机 TLS 反向代理转发至回环监听地址。服务端拒绝来自非回环 TCP 对端的明文连接，不信任 X-Forwarded-Proto。

客户端存储默认是当前目录下的 clients.json，CLI 的 -file 与服务端的 -clients 应指向同一个文件。存储仅保存高熵 token 的 SHA-256 哈希。token 格式为 client_id.随机64位十六进制字符串。

原 token 仅在 add 或 rotate 时写入 -out 指定的新文件，不打印到终端。连接文件保管在部署环境，不加入 Git。文件创建使用 0600；Windows 上另以账户及目录 ACL 管理访问。

## 管理

```powershell
.\dist\speed-probe.exe clients list
.\dist\speed-probe.exe clients set -id main-bot -capabilities ping,topo -max-nodes 100 -max-jobs 1
.\dist\speed-probe.exe clients rotate -id main-bot -address wss://probe.example.com:8765 -out connection-new.json
.\dist\speed-probe.exe clients revoke -id main-bot
```

set 只改显式参数。rotate 后重新导入新连接文件；由于服务端没有原 token，不再提供 export。丢失连接文件也通过 rotate 重新签发。revoke 禁用客户端，重新接入可创建新 ID。

CLI 使用锁文件及临时文件替换客户端存储。服务端检查文件元数据，在文件替换/变化时刷新经过验证的内存快照，不在每个节点上重复解析整个文件。无效更新失败关闭，不继续使用旧策略。每个节点开始前复查授权，活动任务另外每秒复查；撤销、轮换或能力收紧使不再被允许的任务失败并取消剩余工作。新任务采用当前限额；已启动任务保留原先的总期限。

## 能力与限制

| 能力 | 测试 |
| --- | --- |
| ping | HTTP 延迟、RTT、UDP/NAT |
| script | 运行后端已安装的媒体测试脚本 |
| topo | 入口/出口 GeoIP |
| speed | 下载速度；全局 -nospeed 优先 |
| custom_script | 上传自定义 JS 内容，包括自定义 IP 脚本；需显式授权 |

script 与 custom_script 分开；上传媒体测试脚本需要两项授权。普通客户端默认没有 custom_script。自定义 JS 可主动发起网络请求，不等同于严格流量配额，Goja 也不构成不可信代码隔离沙箱。

max_nodes 为单次节点上限，max_jobs 同时统计排队与执行任务；max_seconds 覆盖排队和执行的总时间；max_scripts 为单次脚本数。取消或断线后，运行的工作实际退出才释放任务配额。

## 安装脚本

服务端通过 `-scripts probe-scripts.json` 加载操作者维护的脚本目录：

```json
[
  {
    "ID": "example",
    "Type": "media",
    "Content": "function handler() { return 'ok'; }",
    "TimeoutMillis": 10000
  }
]
```

Type 为 media 或 ip，ID 唯一；目录最多 4 MB，启动时读取，修改后重启后端。请求只发送 ID 时使用目录中的内容与超时，不允许客户端覆盖已安装内容，除非拥有 custom_script。describe 仅返回脚本 ID 和类型，不泄露源码。

在 speed-control 根目录可导出配置中的 JS，交由后端操作者审阅并安装：

```powershell
.\.venv\Scripts\python.exe -m utils.export_probe_scripts --config config.yaml --out probe-scripts.json
```

## 从 v2 升级

这是不兼容升级，两端需同时更新：

1. 停止旧服务并备份客户端存储及控制端 access.sqlite3。
2. 用新的文件名创建 v3 客户端，例如 add -file clients-v3.json ...；不要覆盖旧文件试图自动转换。
3. 后端以 -clients clients-v3.json 启动，按需要安装脚本目录。
4. 控制端导入新连接文件。使用相同 id 可替换旧连接，用户授权不变。未重新导入的 v2 后端不会被使用。

旧 HMAC、nonce、旧连接文件与旧客户端存储均不兼容。协议详情见 [PROTOCOL.md](PROTOCOL.md)。
