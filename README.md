# LwyLink

LwyLink（联微互联）是一个面向朋友小队、小型私服、宿舍网络和轻量联机场景的虚拟组网项目。

它把使用流程收敛为：

```text
账号登录 -> 创建/加入房间 -> 分配虚拟 IP -> 游戏局域网联机
```

项目保留三层虚拟组网需要的核心能力：Wintun 虚拟网卡、vDHCP、房间内 IPv4 包转发，以及基于 `tcpx` 的加密 Overlay；同时避免接管全局代理、防火墙和服务端 NAT。

## 功能概览

- 账号注册、登录，并限制同一账号同时只有一个有效会话。
- 玩家创建房间，其他玩家通过房间公钥加入。
- 每个玩家同一时间只能处在一个房间。
- 每个房间拥有独立虚拟网段，由节点分配虚拟 IP。
- 房间内转发 IPv4 单播、广播和组播发现包。
- 房主可以踢人、转让房主或删除房间。
- 控制中心和房间节点分离，支持多节点调度。
- Windows 客户端基于 Wails + React + TypeScript，支持系统托盘、单实例和自动更新。

## 架构

```text
Windows Client
  - HTTP API：登录、房间、成员、公告、更新查询
  - Wintun：创建本机虚拟网卡
  - TCP/UDP Overlay：传输虚拟网卡流量

Control Plane
  - REST API / 管理后台
  - 账号、房间、节点调度、客户端更新
  - MySQL：持久化账号、房间、节点和发布记录
  - Redis：保存会话、在线状态、房间公钥和事件

Room Node
  - Overlay 连接认证
  - vDHCP 租约
  - 房间内 IPv4 包转发
  - UDP 打洞协商和 TCP 中继兜底
```

## 目录速览

| 路径 | 说明 |
| --- | --- |
| `cmd/lwylink-control` | 控制中心入口 |
| `cmd/lwylink-node` | 房间节点入口 |
| `cmd/lwylink-client` | 本地调试用控制台客户端 |
| `cmd/lwylink-net` | 独立虚拟组网引擎 |
| `cmd/lwylink-windows-ui` | Windows 图形客户端 |
| `cmd/lwylink-smoke` | 端到端 smoke 工具 |
| `configs/` | 本地和生产配置示例 |
| `deploy/` | 构建、部署、systemd 和发布脚本 |
| `internal/` | 账号、房间、节点、Overlay、vDHCP、Web 等内部模块 |

## 文档入口

- [deploy/README.md](deploy/README.md)：生产部署、发布脚本、扩容节点和安全注意。
- [deploy/TEST_SERVER.md](deploy/TEST_SERVER.md)：测试服务器、端口、远端路径和自动部署速查。
- [MANUAL_UPDATE.md](MANUAL_UPDATE.md)：手动构建、上传、发布和回滚流程。
- [examples/README.md](examples/README.md)：Python / Java 调用示例。

## 本地快速开始

### 1. 准备依赖

- Go `1.26`
- Docker / Docker Compose
- Node.js + npm
- Wails CLI（运行或构建图形客户端时需要）

启动 MySQL 和 Redis：

```powershell
docker compose up -d
```

### 2. 启动控制中心

```powershell
Copy-Item .\configs\control.example.json .\configs\control.local.json
go run .\cmd\lwylink-control -config .\configs\control.local.json
```

本地管理后台默认地址：

```text
http://127.0.0.1:8080/admin?token=dev-admin-token
```

### 3. 登记并启动房间节点

复制节点配置并打印节点公钥：

```powershell
Copy-Item .\configs\node.example.json .\configs\node.local.json
go run .\cmd\lwylink-node -config .\configs\node.local.json -print-public-key
```

在管理后台新增节点，填入本地 Overlay 地址和上一步输出的公钥：

| 字段 | 本地测试值 |
| --- | --- |
| 公网 Overlay 地址 | `127.0.0.1:7777` |
| 健康检查地址 | `127.0.0.1:7777` |
| Overlay 公钥 | `-print-public-key` 输出值 |
| 最大房间数 | 例如 `100` |
| 状态 | 启用 |

然后启动节点：

```powershell
go run .\cmd\lwylink-node -config .\configs\node.local.json
```

### 4. 启动客户端

控制台客户端：

```powershell
go run .\cmd\lwylink-client -api http://127.0.0.1:8080
```

Windows 图形客户端：

```powershell
Push-Location .\cmd\lwylink-windows-ui
npm.cmd install --prefix .\frontend
wails dev
Pop-Location
```

连接虚拟网络需要管理员权限，因为 Wintun 和路由配置需要写入系统网络设置。

## 常用命令

测试和静态检查：

```powershell
go test -p 1 ./...
go vet -p 1 ./...
```

构建全部产物：

```powershell
.\deploy\build-all.ps1 -Vet
```

常用构建参数：

```powershell
.\deploy\build-all.ps1 -SkipTests
.\deploy\build-all.ps1 -SkipWindowsUI
.\deploy\build-all.ps1 -AppVersion <version>
```

发布和部署细节统一维护在 [deploy/README.md](deploy/README.md)。

## 配置说明

- `configs/*.example.json` 用于本地开发。
- `configs/*.prod.example.json` 用于生产配置模板。
- 真实的 `configs/*.local.json` 和 `configs/*.prod.json` 会被 `.gitignore` 忽略，不要提交密钥、密码或服务器信息。
- 控制中心和房间节点必须使用相同的 `tokenSecret`。
- 生产环境建议把 HTTP API 放在 HTTPS 反向代理后面。

## 关键概念

- 房间公钥是加入房间和连接房间的授权凭证，不是节点传输密钥。
- 节点公钥用于客户端校验 Overlay 节点身份。
- 实际传输加密由 `tcpx` 完成。
- Redis 中的房间公钥、会话和在线状态决定运行期访问权限。
- 空房间会按配置自动清理，旧房间公钥随房间删除而失效。
