# Python 调用 lwylink-net 示例

这个目录演示 Python 如何接入 LwyLink 房间系统和独立虚拟组网引擎。

## 文件

| 文件 | 说明 |
| --- | --- |
| `lwylink.py` | 可复制到项目里的标准库客户端小库 |
| `lwylink_room_example.py` | HTTP JSON API + SSE 房间事件示例 |
| `lwylink_net_example.py` | 调用 `lwylink-net.exe` 的虚拟组网示例 |
| `FULL_FLOW.md` | 从登录、建房、监听事件到启动组网的完整流程 |

`lwylink.py` 封装了房间 API、SSE 事件流，以及启动/停止 `lwylink-net.exe` 的基础逻辑；两个 `*_example.py` 文件只保留 CLI 演示代码。

`lwylink_net_example.py` 会：

- 启动 `lwylink-net.exe connect`
- 按行读取 stdout JSON Lines 状态
- 在 Python 里解析 `preparing`、`connected`、`transport`、`error` 等事件
- 支持优雅停止子进程
- 提供 `--self-test`，用于验证 Python 能正常调用本仓库的 `build/lwylink-net.exe`

## 自测

仓库当前环境可以用 `uv` 运行 Python：

```powershell
uv run python .\examples\python\lwylink_net_example.py --self-test
uv run python .\examples\python\lwylink_room_example.py --self-test
```

如果系统已经安装 Python，也可以直接：

```powershell
python .\examples\python\lwylink_net_example.py --self-test
python .\examples\python\lwylink_room_example.py --self-test
```

## API 模式启动

示例默认连接 `https://www.lwylink.xyz`。本地开发时再显式传 `--api http://127.0.0.1:8080`，或设置 `LWYLINK_API`。

```powershell
$env:LWYLINK_TOKEN = "<login-token>"
uv run python .\examples\python\lwylink_net_example.py connect `
  --exe .\build\lwylink-net.exe `
  --room-id 123 `
  --if-name LwyLink
```

## 直连模式启动

```powershell
$env:LWYLINK_TOKEN = "<login-token>"
uv run python .\examples\python\lwylink_net_example.py connect `
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
uv run python .\examples\python\lwylink_net_example.py cleanup `
  --exe .\build\lwylink-net.exe `
  --if-name LwyLink `
  --cidr "172.16.1.0/24"
```

启动虚拟网卡、配置路由和释放 Wintun 通常需要管理员权限。
