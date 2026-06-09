# Java 调用 lwylink-net 示例

这个目录演示 Java 如何接入 LwyLink 房间系统和独立虚拟组网引擎，适合游戏 mod、启动器或服务端工具嵌入组网能力。

这里不包含房间语音或 WebRTC 示例。语音是官方 Windows 客户端能力；Java 示例只负责房间、事件和虚拟组网进程控制。

示例代码只使用 JDK 标准库。

## 文件

| 文件 | 说明 |
| --- | --- |
| `LwyLink.java` | 可复制到项目里的 JDK 标准库客户端小库 |
| `LwyLinkRoomExample.java` | HTTP JSON API + SSE 房间事件示例 |
| `LwyLinkNetExample.java` | 调用 `lwylink-net.exe` 的虚拟组网示例 |
| `FULL_FLOW.md` | 从登录、建房、监听事件到启动组网的完整流程 |

`LwyLink.java` 封装了房间 API、SSE 事件流，以及启动/停止 `lwylink-net.exe` 的基础逻辑；两个 `*Example.java` 文件只保留 CLI 演示代码。

`LwyLinkNetExample.java` 会：

- `ProcessBuilder` 启动 `lwylink-net.exe`
- 按行读取 stdout JSON Lines
- 解析 `preparing`、`connected`、`transport`、`error` 等状态
- 支持 Ctrl+C / JVM 退出时结束子进程
- 提供 `--self-test`，验证 Java 能正常调用本仓库的 `build/lwylink-net.exe`

## 编译

```powershell
javac .\examples\java\LwyLink.java .\examples\java\LwyLinkRoomExample.java
javac .\examples\java\LwyLink.java .\examples\java\LwyLinkNetExample.java
```

## 自测

```powershell
java -cp .\examples\java LwyLinkRoomExample --self-test
java -cp .\examples\java LwyLinkNetExample --self-test
```

## API 模式启动

示例默认连接 `https://www.lwylink.xyz`。本地开发时再显式传 `--api http://127.0.0.1:8080`，或设置 `LWYLINK_API`。

```powershell
$env:LWYLINK_TOKEN = "<login-token>"
java -cp .\examples\java LwyLinkNetExample connect `
  --exe .\build\lwylink-net.exe `
  --room-id 123 `
  --if-name LwyLink
```

## 直连模式启动

```powershell
$env:LWYLINK_TOKEN = "<login-token>"
java -cp .\examples\java LwyLinkNetExample connect `
  --exe .\build\lwylink-net.exe `
  --user-id 1 `
  --room-id 123 `
  --room-public-key "<room-public-key>" `
  --network-cidr "172.16.1.0/24" `
  --overlay-addr "example.com:35768" `
  --udp-overlay-addr "example.com:35768" `
  --server-public-key "<node-public-key>"
```

## 清理

```powershell
java -cp .\examples\java LwyLinkNetExample cleanup `
  --exe .\build\lwylink-net.exe `
  --if-name LwyLink `
  --cidr "172.16.1.0/24"
```

启动虚拟网卡、配置路由和释放 Wintun 通常需要管理员权限。
