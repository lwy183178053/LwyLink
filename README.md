# LwyLink

LwyLink（联微互联）是一个面向朋友小队、小型私服、宿舍网络和轻量联机场景的虚拟组网项目。

项目保留三层虚拟组网的核心能力：Wintun 虚拟网卡、虚拟 DHCP、房间内 IPv4 包转发，以及基于 `tcpx` 的加密传输；同时避免接管全局代理、防火墙和服务端 NAT，把主链路收敛到：

```text
账号登录 -> 创建/加入房间 -> 分配虚拟网段 -> 游戏局域网联机
```

## 功能概览

- 账号注册、登录，并限制同一账号同时只有一个有效会话。
- 玩家创建房间，其他玩家通过房间公钥加入。
- 每个玩家同一时间只能处在一个房间。
- 每个房间拥有独立虚拟网段，节点通过 vDHCP 分配虚拟 IP。
- 房间内转发三层 IPv4 包，支持单播、广播和 IPv4 组播发现。
- 房主可以踢人、转让房主或删除房间。
- 控制中心和房间节点分离，支持多节点调度。
- Windows 客户端基于 Wails + React + TypeScript，支持系统托盘、单实例和自动更新。

## 架构

```text
Windows Client
  - HTTP API：登录、创建房间、加入房间、查询更新
  - Wintun：创建本机虚拟网卡
  - tcpx Overlay：连接房间节点并传输虚拟网卡流量

Control Plane
  - REST API / 管理后台
  - 账号、房间、节点调度、客户端更新
  - MySQL：持久化账号、房间、节点和发布记录
  - Redis：保存会话、在线状态、房间公钥和事件

Room Node
  - Overlay 连接认证
  - vDHCP 租约
  - 房间内 IPv4 包转发
  - 订阅房间事件并主动断开无效连接
```

## 目录速览

| 路径 | 说明 |
| --- | --- |
| `cmd/lwylink-control` | 控制中心入口 |
| `cmd/lwylink-node` | 房间节点入口 |
| `cmd/lwylink-client` | Windows 控制台客户端 |
| `cmd/lwylink-windows-ui` | Wails 图形客户端 |
| `cmd/lwylink-smoke` | 远端功能 smoke 工具 |
| `configs/` | 本地和生产配置示例 |
| `deploy/` | 构建、部署、systemd 和发布脚本 |
| `internal/` | 账号、房间、节点、Overlay、vDHCP、Web 等内部模块 |

## 文档入口

- [deploy/README.md](deploy/README.md)：生产部署、发布脚本、扩容节点和安全注意。
- [deploy/TEST_SERVER.md](deploy/TEST_SERVER.md)：当前测试服务器、端口、远端路径和自动部署速查。
- [MANUAL_UPDATE.md](MANUAL_UPDATE.md)：不走一键脚本时的手动构建、上传、发布和回滚流程。

## 本地快速开始

### 1. 准备依赖

本地开发常用依赖：

- Go `1.26`
- Docker / Docker Compose
- Node.js + npm
- Wails CLI（构建图形客户端时需要）

启动 MySQL 和 Redis：

```powershell
docker compose up -d
```

### 2. 启动控制中心

```powershell
Copy-Item .\configs\control.example.json .\configs\control.local.json
go run .\cmd\lwylink-control -config .\configs\control.local.json
```

管理后台：

```text
http://127.0.0.1:8080/admin?token=dev-admin-token
```

### 3. 登记并启动房间节点

先复制节点配置，并生成节点公钥：

```powershell
Copy-Item .\configs\node.example.json .\configs\node.local.json
go run .\cmd\lwylink-node -config .\configs\node.local.json -print-public-key
```

在管理后台的“节点”页面新增节点：

| 字段 | 本地测试值 |
| --- | --- |
| 公网 Overlay 地址 | `127.0.0.1:7777` |
| 健康检查地址 | `127.0.0.1:7777` |
| Overlay 公钥 | 上一步输出的值 |
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

本地测试：

```powershell
go test -p 1 ./...
go vet -p 1 ./...
```

一键构建全部产物：

```powershell
.\deploy\build-all.ps1 -Vet
```

常用构建参数：

```powershell
.\deploy\build-all.ps1 -SkipTests
.\deploy\build-all.ps1 -SkipWindowsUI
.\deploy\build-all.ps1 -AppVersion 0.1.12
```

默认产物：

| 产物 | 路径 |
| --- | --- |
| Linux 控制中心 | `deploy/dist/lwylink-control` |
| Linux 房间节点 | `deploy/dist/lwylink-node` |
| Linux smoke 工具 | `deploy/dist/lwylink-smoke` |
| Windows 控制台工具 | `build/windows-amd64/lwylink-client.exe` |
| Windows smoke 工具 | `build/windows-amd64/lwylink-smoke.exe` |
| Windows 图形客户端 | `build/windows-amd64/LwyLink.exe` |
| 产物清单 | `build/artifacts.json` |

## 发布与部署

日常发布优先使用：

```powershell
$env:LWYLINK_DEPLOY_SSH_PASSWORD = "这里填 SSH 密码"
.\deploy\release.cmd -Version 0.1.12 -Notes "本次更新说明"
```

这个入口会同步客户端版本号、构建、上传部署、重启服务、运行 smoke，并发布 Windows 客户端更新包。

只重试部署或只重试客户端发布时，可以给 `release.cmd` 加 `-SkipBuild` 复用已有构建产物。

只上传已有构建产物：

```powershell
.\deploy\upload-deploy.ps1
```

配置有变更时再加：

```powershell
.\deploy\upload-deploy.ps1 -UploadConfigs
```

更完整的发布流程见 [deploy/README.md](deploy/README.md)。当前测试服务器上的 `443` 端口属于旧服务，LwyLink 只使用 `8080` 和 `35768`，细节见 [deploy/TEST_SERVER.md](deploy/TEST_SERVER.md)。

## 客户端更新

控制后台提供“更新发布”页面。发布 Windows 客户端时上传：

```text
build/windows-amd64/LwyLink.exe
```

正式发布新版本时，建议同步这些版本位置：

- `cmd/lwylink-windows-ui/app.go`
- `cmd/lwylink-windows-ui/wails.json`
- `cmd/lwylink-windows-ui/frontend/package.json`
- `cmd/lwylink-windows-ui/frontend/package-lock.json`
- `cmd/lwylink-windows-ui/frontend/src/types.ts`
- `internal/web/admin_assets/admin.html`

`deploy/release.ps1` 会自动同步这些位置。`deploy/build-all.ps1 -AppVersion <version>` 只通过 Go `ldflags` 临时注入版本号，不会修改版本文件。

## REST API 概览

登录后接口使用：

```text
Authorization: Bearer <token>
```

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `POST` | `/api/register` | 注册 |
| `POST` | `/api/login` | 登录 |
| `GET` | `/api/me` | 当前账号 |
| `GET` | `/api/announcement` | 公告 |
| `POST` | `/api/rooms` | 创建房间 |
| `GET` | `/api/rooms` | 当前房间 |
| `POST` | `/api/rooms/join` | 加入房间 |
| `GET` | `/api/rooms/members?roomID=1` | 房间成员 |
| `POST` | `/api/rooms/chat` | 房间聊天 |
| `GET` | `/api/rooms/events` | 房间事件 SSE |
| `POST` | `/api/rooms/leave` | 退出房间 |
| `POST` | `/api/rooms/kick` | 踢人 |
| `POST` | `/api/rooms/transfer-owner` | 转让房主 |
| `POST` | `/api/rooms/delete` | 删除房间 |
| `GET` | `/api/client/releases/latest?platform=windows-amd64` | 查询客户端最新版 |

## 关键概念

- 房间公钥是加入房间和连接房间的授权凭证，不是节点传输密钥。
- 节点公钥用于客户端校验 Overlay 节点身份。
- 实际传输加密由 `tcpx` 完成。
- 空房间会按配置自动清理，旧房间公钥随房间删除而失效。
- HTTP API 当前仍是普通 HTTP，公网生产部署建议放到 HTTPS 反向代理后面。
