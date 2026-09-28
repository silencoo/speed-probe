# 测速内核与参数

后端内嵌 Mihomo v1.19.31 和 sing-box v1.14.2。版本锁定在 go.mod；describe 返回实际构建版本、支持的配置格式及编译能力。稳定版核对时间：2026-09-28。

- 上游：[Mihomo 发布页](https://github.com/MetaCubeX/mihomo/releases)、[sing-box 发布页](https://github.com/SagerNet/sing-box/releases)。
- 构建需要 Go 1.25.5 或更新版本。使用 `go build -tags with_utls -o dist/speed-probe .`，Windows 输出名加 .exe。build.sh 已包含标签。
- 普通构建也包含 Hysteria/Hysteria2/TUIC；sing-box uTLS 需要 with_utls，describe.features 会反映是否编入。
- 单节点独立创建内核实例，无本地代理监听端口。节点完成或任务取消后关闭实例及连接。

## 请求

`Vendor: "Mihomo"`：每个 Nodes[].Payload 是一个完整的 Mihomo 节点 YAML；保留 Clash 作为兼容别名。

`Vendor: "SingBox"`：Payload 是一个完整的 sing-box outbound JSON。例如：

```json
{"type":"socks","tag":"test","server":"127.0.0.1","server_port":1080,"version":"5"}
```

节点身份使用外层 Name，sing-box 内部 tag 归一为 test。支持独立的 outbound；不是运行整份路由配置。依赖其他节点的 detour、selector/urltest、WireGuard endpoint、TUN、需要额外编译支持的 Naive/Cronet 等不属于当前测速适配器的支持范围。不得把“支持 sing-box”理解为支持它的全部应用功能。UDP 还取决于节点协议及服务端支持。

配置交由对应上游内核解析，取消旧 ProxyType 白名单。Mihomo 的 SS2022、Hysteria2、AnyTLS、新传输选项不会再被前后端旧白名单过滤。解析失败作为该节点的 error 返回，其他节点继续，结果表保留错误行而不发生列错位；不输出带密码的原始配置。

## 控制端转换

控制端原生读取 Mihomo proxies 或 sing-box outbounds。原生配置保留字段，由对应内核负责验证。Mihomo 到 sing-box 的常用 SS、VMess、VLESS、Trojan、SOCKS、HTTP、Hysteria2、TUIC、AnyTLS 可转换，包括受支持的 TLS/REALITY、WS、gRPC 参数。

转换遇到未知或语义不等价的参数会拒绝提交并列出参数名；请改用 Mihomo 或原生 sing-box 配置。不会丢弃未知参数后冒充转换成功。不实现 sing-box 到 Mihomo 的反向转换。

## 测速与脚本

新配置默认从 Cloudflare 的测速下载接口测量下载速率，地址可在控制端 runtime.speedFiles 或请求 DownloadURL 中覆盖；现有用户配置不自动改写。[上游接口说明](https://github.com/cloudflare/speedtest)。

测速所有下载连接使用统一计时窗口，连接建立时间包括在窗口内，仅 200/206 响应体计入流量，限速等待可取消。测得的是指定目标路径的吞吐，不等同于全网带宽。历史 DYNAMIC:INTL / DYNAMIC:FAST 仍为兼容选项，不建议新配置使用。

GeoIP 优先使用本地 MaxMind；远程使用 HTTPS ipwho.is / ipapi.co，拒绝错误响应并避免缓存空结果。批量任务建议配置 -mmdb 以减少第三方限流。IPv4/IPv6 出口分别探测。[ipwho.is 接口说明](https://ipwhois.io/documentation)。

## 验证

```sh
go test -tags with_utls ./...
```

测试覆盖双内核 SOCKS5 实际代理转发、UDP、SS2022/新协议配置、取消、错误页面不计流量、脚本超时和 GeoIP 失败回退。未以真实付费代理、外部测速节点验证全部协议。
