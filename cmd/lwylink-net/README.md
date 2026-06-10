# lwylink-net

`lwylink-net.exe` 是 LwyLink 的独立虚拟组网引擎。它只负责启动 Wintun、连接 Overlay 节点、配置路由、转发 TCP/UDP 虚拟网络流量，并通过标准输出返回 JSON Lines 状态。

Wintun DLL 已经内嵌在 `lwylink-net.exe` 里。启动 `connect` 时如果 exe 同目录没有 `wintun.dll`，或文件内容和内嵌版本不一致，程序会自动释放一份新的 `wintun.dll` 到 exe 所在目录。

## 构建

```powershell
go build -trimpath -o build\lwylink-net.exe .\cmd\lwylink-net
```

发布脚本也会构建：

```powershell
.\deploy\build-all.ps1
```

## API 模式

API 模式会用登录 token 调用控制端，自动读取当前用户和已加入房间的信息。

```powershell
$env:LWYLINK_TOKEN = "<login-token>"
.\build\lwylink-net.exe connect `
  --api "https://www.lwylink.xyz" `
  --room-id 123 `
  --if "LwyLink"
```

`--room-id` 可以省略，省略时使用当前用户加入的第一个房间。传输选路自动完成：先用 TCP relay 保证可用，再在后台探测 UDP P2P，成功后切到直连。

## 直连模式

直连模式不访问 HTTP API，调用方必须传完整房间和节点参数。

```powershell
$env:LWYLINK_TOKEN = "<login-token>"
.\build\lwylink-net.exe connect `
  --user-id 1 `
  --room-id 123 `
  --room-public-key "<room-public-key>" `
  --network-cidr "172.16.1.0/24" `
  --overlay-addr "example.com:35768" `
  --udp-overlay-addr "example.com:35768" `
  --server-public-key "<node-public-key>" `
  --if "LwyLink"
```

## 状态输出

程序会持续运行，直到收到 Ctrl+C、SIGTERM 或父进程结束。状态通过 stdout 每行一个 JSON 对象输出：

```json
{"type":"connecting","message":"正在连接虚拟网络","roomID":123}
{"type":"connected","message":"虚拟网络已连接","ip":"172.16.1.2","gateway":"172.16.1.254","mtu":1400}
{"type":"transport","transportMode":"udp-direct","transportMessage":"P2P 直连 · 1 条隧道","directPeers":1,"transportDiagnostics":[{"virtualIP":"172.16.1.3","candidateCount":3,"candidateTypes":["lan","srflx-stun","observed"],"selectedType":"srflx-stun","selectedPath":"selected","lastRTTMS":18,"failedProbes":0,"consecutiveFailures":0}]}
{"type":"disconnected","message":"虚拟网络已断开"}
```

调用方可以按行读取 stdout 来更新 UI。普通 UI 只需要 `transportMode`、`transportMessage`、`directPeers`；`transportDiagnostics` 用于日志、smoke 和开发诊断。

## 清理

如果进程被强杀导致路由或防火墙配置没有恢复，可以运行：

```powershell
.\build\lwylink-net.exe cleanup --if "LwyLink" --cidr "172.16.1.0/24"
```

## 环境变量

所有敏感参数优先建议用环境变量传入，避免出现在进程命令行里。

| 变量 | 说明 |
| --- | --- |
| `LWYLINK_TOKEN` | 登录 token |
| `LWYLINK_API` | 控制端 API 地址，默认 `https://www.lwylink.xyz` |
| `LWYLINK_ROOM_ID` | 房间 ID |
| `LWYLINK_IF` | Wintun 网卡名 |
| `LWYLINK_USER_ID` | 直连模式用户 ID |
| `LWYLINK_ROOM_PUBLIC_KEY` | 直连模式房间密钥 |
| `LWYLINK_NETWORK_CIDR` | 直连模式房间网段 |
| `LWYLINK_OVERLAY_ADDR` | 直连模式 Overlay 地址 |
| `LWYLINK_UDP_OVERLAY_ADDR` | 直连模式 UDP 打洞地址，留空时同 Overlay 地址 |
| `LWYLINK_STUN` | UDP P2P 的 STUN 地址，默认使用当前房间节点 UDP 地址 |
| `LWYLINK_SERVER_PUBLIC_KEY` | 直连模式节点公钥 |
