# speed-probe 协议 v3

WSS 根路径 /，握手头 Authorization: Bearer TOKEN。无效凭据返回 HTTP 401，未使用 TLS 的远程 TCP 对端返回 403。令牌不得放在 URL 中。

外层协议字段用 snake_case。检测请求和指标的已有字段保留 PascalCase，避免重写 Vendor/Macro/Matrix 引擎。每个连接用于一次 describe 或一个 run。命令与事件都包含 protocol: 3。

## 后端描述

请求：

```json
{"protocol":3,"type":"describe"}
```

响应：

```json
{
  "protocol": 3,
  "type": "description",
  "description": {
    "software_version": "1.0",
    "supported": ["ping","script","topo","speed","custom_script"],
    "allowed": ["ping","script"],
    "limits": {"max_nodes":300,"max_jobs":2,"max_seconds":600,"max_scripts":32},
    "scripts": [{"id":"example","type":"media"}]
  }
}
```

supported 表示实现能力；allowed 是当前客户端授权与全局策略交集；limits 为资源限制。software_version 与 protocol 独立。控制端缓存只用于展示，实际提交仍由服务端授权。

## 任务

```json
{
  "protocol": 3,
  "type": "run",
  "task_id": "task-123",
  "request": {
    "Vendor": "Clash",
    "Nodes": [{"Name":"node-a","Payload":"name: node-a\ntype: direct"}],
    "Options": {"Matrices":[{"Type":"TEST_SCRIPT","Params":"example"}]},
    "Configs": {"Scripts":[{"ID":"example","Type":"media","Content":""}]}
  }
}
```

task_id 必须是 1–64 位 ASCII 字母、数字、下划线或短横线，可使用 UUID 等合法值。Nodes 不得为空。Payload 为原有节点 YAML，只有执行端接收它；结果不回传完整请求。检测字段见 interfaces/api_request.go 和 interfaces/api_request_config.go。

事件顺序为 accepted → 零个或多个 progress → 恰好一个 finished。无效任务可直接返回 failed 终态；认证失败发生在升级前。连接中断无法保证终态送达。

- accepted：state=queued，确认任务已接收。
- progress：state=running，record 为一条节点结果，queuing 为任务池剩余节点数。
- heartbeat：运行连接每 15 秒发送一次，不改变任务状态。
- finished：state=succeeded/failed/cancelled，results 为已完成节点的结果数组（无结果时可省略），error 为可选的 code/message。

record 和 results 的每项包含 index（原请求节点数组的零基索引）、ProxyInfo、InvokeDuration、Matrices。终态按 index 排序；同名节点仍可区分。只有所有节点完成才返回 succeeded；网络检测失败可作为某节点的测试指标，不等于整个任务执行异常。

取消请求：

```json
{"protocol":3,"type":"cancel","task_id":"task-123"}
```

cancel 必须匹配当前连接的任务。收到取消后停止调度并取消运行中的工作，完成清理后返回 cancelled。断线执行相同清理，但不再保证结果送达。权限撤销返回 failed/permission_revoked，超时返回 failed/deadline_exceeded。部分已完成结果可随终态返回，未执行节点不会填成伪造的默认结果。

错误码：invalid_request、permission_denied、capacity_exceeded、permission_revoked、deadline_exceeded、execution_failed、cancelled。

## 生命周期约束

首条命令需在 10 秒内发送，单条输入最多 16 MB，网络写入超时 10 秒。任务总期限覆盖排队时间。任务池在锁外发布结束回调，网络层是唯一事件写入方。配额直到运行工作实际退出才释放。

不提供任务持久化、断线续跑、自动提交重试或跨连接幂等。task_id 是关联标识，控制端每次新测试和重测都生成新 ID。重连发送 run 会创建另一次测试。

Python 与 Go 共用的契约样例分别位于 tests/fixtures/v3.json 和 auth/testdata/v3.json。

## 内核能力与节点错误

description 增加 cores 列表，每项包含 id、version、formats、features。Vendor 支持 Mihomo、SingBox、Local，并保留 Clash 兼容别名。Mihomo Payload 为单节点 YAML，SingBox Payload 为单 outbound JSON。节点配置解析失败时 record.error 给出不含凭据的错误，Matrices 可为空，索引仍保持不变。finished.succeeded 表示任务调度完成，不保证每个节点网络或配置都有效。详见 [CORES.md](CORES.md)。


## 下载预算与测量明细

新版 description.features 包含 `download_budget`、`measurement_details`。这两个特性不改变 v3 认证，不需要重新签发 Token。旧后端未声明预算时，控制端不得提交非零预算；零预算省略该字段以兼容旧 v3 解码器。

`Configs.DownloadBytes` 为每节点所有下载线程共享的 uint64 正文读取预算，0／省略表示不限。时间与字节预算任一达到即停止；读取前预留额度，短读退还额度，所有线程退出后汇总。预算不含其他 Macro 的流量及传输层开销，不是整任务总流量限制。

SPEED_AVERAGE payload 除 Value 外新增 TotalBytes、ElapsedMillis、StopReason。StopReason 为 duration、byte_limit、download_error、source_exhausted、cancelled。Value 是实际应用层正文量除以实际耗时（含建立连接）；仅 200／206 响应正文计入下载。SPEED_PER_SECOND 的最后一段可能不足一秒，该段按实际间隔归一化成 Byte/s。

TEST_PING_CONN／TEST_PING_RTT payload 增加 Max、StdDev、Attempts、Failures、HTTPCode。每个采样新建连接，按 PingAverageOver 固定次数执行（取消除外），TaskTimeout 为每次采样超时，TaskRetry 不改变延迟样本数。RTT 字段在本版本表示建连耗时，HTTPS 包含 TLS 握手；HTTP Value 包含获取响应头的时间。延迟均值／标准差使用全部成功样本，不丢弃慢样本。Failures 表示网络请求失败，不表示 ICMP 丢包；HTTPCode 是最后一次成功收到的响应状态，403 等响应也代表网络可达。
