# 客户端认证 v2

speed-probe 管理客户端身份与能力，speed-control 管理 Telegram 用户。后端不需要维护 Telegram 用户或 Bot ID 名单。

## 创建并接入

```powershell
.\dist\speed-probe.exe clients add -id main-bot -capabilities ping,script,topo,speed -max-nodes 300 -max-jobs 2
.\dist\speed-probe.exe clients export -id main-bot -address ws://127.0.0.1:8765 -out connection.json
.\dist\speed-probe.exe server -bind 127.0.0.1:8765 -clients clients.json
```

在 speed-control 管理员私聊中上传 `connection.json`，回复文件发送 `/backend import`。控制端会验证连接并保存凭据。同机测试可使用上述回环地址；远程部署使用 `wss://`，并在服务端配置 `-tls -tls-cert ... -tls-key ...` 或通过 TLS 反向代理提供服务。

`clients.json` 默认位于当前工作目录，可通过所有 `clients` 子命令的 `-file` 和服务端 `-clients` 指定同一个绝对路径。客户端密钥由安全随机数生成，导出文件只写到 `-out` 指定的新文件，不在终端打印密钥。

## 日常管理

```powershell
.\dist\speed-probe.exe clients list
.\dist\speed-probe.exe clients set -id main-bot -capabilities ping,topo -max-nodes 100 -max-jobs 1
.\dist\speed-probe.exe clients rotate -id main-bot
.\dist\speed-probe.exe clients export -id main-bot -address wss://probe.example.com:8765 -out connection-new.json
.\dist\speed-probe.exe clients revoke -id main-bot
```

`set` 只修改显式给出的参数；`rotate` 更换密钥，需要重新导出并导入；`revoke` 禁用该客户端，不影响其他客户端。禁用后若需要新接入，可创建一个新客户端 ID。

配置在每个新请求和每个待执行节点开始前重新读取，不需要重启。撤销或轮换后，排队的节点不再开始测试；已经执行中的网络操作会自然结束，不保证立即中断。连接断开会移除尚未开始的节点；运行中的节点结束前仍占用客户端任务配额。

| 能力 | 对应测试 |
| --- | --- |
| `ping` | HTTP 延迟、RTT、UDP/NAT |
| `script` | 自定义 JavaScript，包括自定义 IP 脚本 |
| `topo` | 入口/出口 GeoIP |
| `speed` | 平均、最大、每秒下载速度 |

客户端有单次节点上限 `max_nodes` 和排队/执行任务上限 `max_jobs`。全局 `-nospeed` 仍优先于客户端的 `speed` 权限。`script` 允许执行会发起网络请求的自定义检测脚本，并不等同于严格的流量配额；原有脚本引擎不是不可信代码沙箱。

## 签名协议

每个 WebSocket 连接提交一个 v2 信封：

```json
{
  "version": 2,
  "client_id": "main-bot",
  "timestamp": 1700000000,
  "nonce": "32位十六进制随机字符串",
  "signature": "HMAC-SHA256 的十六进制结果",
  "payload": "原请求的 JSON 字符串"
}
```

以 UTF-8 编码的密钥字符串为 HMAC key，以以下拼接结果为 message：

```text
speed-probe/v2\n{client_id}\n{timestamp}\n{nonce}\n{payload}
```

这里的 `\n` 表示换行。`payload` 保留客户端原始字符串字节，不重新序列化，签名覆盖完整请求（含 Vendor）。有效时间窗口为前后 120 秒。相同客户端的 nonce 在窗口内只能使用一次；重放缓存保存在进程内，重启会清空。两端需要正确系统时间。

空节点请求用于已认证的健康检查，响应 `Capabilities` 返回当前有效能力（包含 `-nospeed` 的限制），不创建测试任务。响应中的 `Version` 表示后端版本，`Progress` 和 `Result` 分别返回进度与最终结果。

## 凭据存储

客户端存储、导出连接文件及其中的密钥应保留在部署环境，默认文件名已加入 `.gitignore`。CLI 使用临时文件原子替换，并使用锁文件防止同时写入；如果管理进程异常终止，确认没有其他管理命令运行后再删除残留 `.lock` 文件。
