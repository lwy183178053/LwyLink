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
  --api "http://127.0.0.1:8080" `
  --room-id 123 `
  --if "LwyLink" `
  --p2p=true
```

`--room-id` 可以省略，省略时使用当前用户加入的第一个房间。

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
  --server-public-key "<node-public-key>" `
  --if "LwyLink" `
  --p2p=true
```

## 状态输出

程序会持续运行，直到收到 Ctrl+C、SIGTERM 或父进程结束。状态通过 stdout 每行一个 JSON 对象输出：

```json
{"type":"connecting","message":"正在连接虚拟网络","roomID":123}
{"type":"connected","message":"虚拟网络已连接","ip":"172.16.1.2","gateway":"172.16.1.254","mtu":1400}
{"type":"transport","transportMode":"udp-direct","transportMessage":"智能加速已开启","directPeers":1}
{"type":"disconnected","message":"虚拟网络已断开"}
```

调用方可以按行读取 stdout 来更新 UI。

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
| `LWYLINK_API` | 控制端 API 地址 |
| `LWYLINK_ROOM_ID` | 房间 ID |
| `LWYLINK_IF` | Wintun 网卡名 |
| `LWYLINK_P2P` | 是否启用 P2P |
| `LWYLINK_USER_ID` | 直连模式用户 ID |
| `LWYLINK_ROOM_PUBLIC_KEY` | 直连模式房间密钥 |
| `LWYLINK_NETWORK_CIDR` | 直连模式房间网段 |
| `LWYLINK_OVERLAY_ADDR` | 直连模式 Overlay 地址 |
| `LWYLINK_SERVER_PUBLIC_KEY` | 直连模式节点公钥 |
