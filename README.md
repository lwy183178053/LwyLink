# LwyLink

LwyLink，中文名“联微互联”，是一个面向朋友小队、小型私服、宿舍网络和轻量联机场景的虚拟组网项目。

它保留三层虚拟组网的核心能力：Wintun、虚拟 DHCP、房间内 IP 包转发和 `tcpx` 加密传输；同时避免复杂的全局代理、防火墙接管和服务端 NAT，把重点放在“账号 -> 房间 -> 虚拟网段 -> 游戏联机”这条主链路上。

## 核心能力

- 玩家注册、登录，同一账号同一时间只允许一个有效登录。
- 玩家可以创建房间，其他玩家通过房间公钥加入。
- 每个玩家同一时间只能处在一个房间中。
- 每个房间拥有独立虚拟网段和 vDHCP 地址分配。
- 房间内转发三层 IPv4 包，支持单播、广播和 IPv4 组播发现。
- 房主可以踢人、转让房主或删除房间。
- Windows 客户端基于 Wails + React + TypeScript，支持系统托盘、单实例和自动更新。
- 服务端拆分为控制中心和房间节点，支持多节点调度。

## 架构

```text
Windows 客户端
  -> HTTP API 登录、创建房间、加入房间
  -> Wintun 虚拟网卡
  -> tcpx 加密 Overlay
  -> LwyLink Room Node

LwyLink Control Plane
  -> REST API / 管理后台
  -> 账号、房间、节点调度、客户端更新
  -> MySQL 持久化
  -> Redis 保存会话、在线状态、房间公钥和事件

LwyLink Room Node
  -> Overlay 连接认证
  -> vDHCP 租约
  -> 房间内 IP 包转发
  -> 订阅房间事件并主动断开无效连接
```

## 目录

```text
cmd/lwylink-control      控制中心入口
cmd/lwylink-node         房间节点入口
cmd/lwylink-client       Windows 控制台客户端
cmd/lwylink-windows-ui   Wails 图形客户端
cmd/lwylink-smoke        远端功能 smoke 工具
configs/                 本地和生产配置示例
deploy/                  构建、部署、systemd 和生产说明
internal/                账号、房间、节点、Overlay、vDHCP、Web 等内部模块
```

更多部署说明见 `deploy/README.md` 和 `deploy/TEST_SERVER.md`。测试服务器上的 `443` 端口属于旧服务，LwyLink 当前只使用 `8080` 和 `35768`。

## 快速开始

### 1. 启动 MySQL 和 Redis

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

### 3. 启动房间节点

先生成并复制节点公钥：

```powershell
Copy-Item .\configs\node.example.json .\configs\node.local.json
go run .\cmd\lwylink-node -config .\configs\node.local.json -print-public-key
```

在管理后台新增节点：

- 公网 Overlay 地址：本地测试填 `127.0.0.1:7777`
- 健康检查地址：本地测试填 `127.0.0.1:7777`
- Overlay 公钥：填上一步输出的值
- 最大房间数：例如 `100`

然后启动节点：

```powershell
go run .\cmd\lwylink-node -config .\configs\node.local.json
```

### 4. 启动客户端

控制台客户端：

```powershell
go run .\cmd\lwylink-client
```

Windows 图形客户端：

```powershell
cd .\cmd\lwylink-windows-ui
npm.cmd install
wails dev
```

连接虚拟网络需要管理员权限，因为 Wintun 和路由配置需要写入系统网络设置。

## 构建

推荐使用一键构建脚本：

```powershell
.\deploy\build-all.ps1
```

默认会生成：

- Linux 服务端：`deploy/dist/lwylink-control`、`deploy/dist/lwylink-node`、`deploy/dist/lwylink-smoke`
- Windows 工具：`build/windows-amd64/lwylink-client.exe`、`build/windows-amd64/lwylink-smoke.exe`
- Windows 图形客户端：`build/windows-amd64/LwyLink.exe`
- 产物清单：`build/artifacts.json`

常用参数：

```powershell
.\deploy\build-all.ps1 -Vet
.\deploy\build-all.ps1 -SkipTests
.\deploy\build-all.ps1 -SkipWindowsUI
.\deploy\build-all.ps1 -AppVersion 0.1.10
```

## 部署

先构建，再上传部署：

```powershell
.\deploy\build-all.ps1

$env:LWYLINK_DEPLOY_SSH_PASSWORD = "这里填 SSH 密码"
.\deploy\upload-deploy.ps1
```

配置有变更时再加 `-UploadConfigs`：

```powershell
.\deploy\upload-deploy.ps1 -UploadConfigs
```

只想上传重启、不跑 smoke：

```powershell
.\deploy\upload-deploy.ps1 -SkipSmoke
```

生产环境、测试服、端口和 systemd 细节见 `deploy/README.md`。

## 客户端更新

控制后台提供“更新发布”页面。发布 Windows 客户端时，上传：

```text
build/windows-amd64/LwyLink.exe
```

正式发布新版本时建议同步这些版本位置：

- `cmd/lwylink-windows-ui/app.go`
- `cmd/lwylink-windows-ui/wails.json`
- `cmd/lwylink-windows-ui/frontend/package.json`
- `cmd/lwylink-windows-ui/frontend/package-lock.json`
- `cmd/lwylink-windows-ui/frontend/src/types.ts`

也可以构建时临时注入 Go 端版本：

```powershell
.\deploy\build-all.ps1 -AppVersion 0.1.10
```

这个参数不会修改版本文件。

## REST API 概览

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `POST` | `/api/register` | 注册 |
| `POST` | `/api/login` | 登录 |
| `GET` | `/api/me` | 当前账号 |
| `POST` | `/api/rooms` | 创建房间 |
| `POST` | `/api/rooms/join` | 加入房间 |
| `GET` | `/api/rooms` | 当前房间 |
| `GET` | `/api/rooms/members?roomID=1` | 房间成员 |
| `POST` | `/api/rooms/chat` | 房间聊天 |
| `GET` | `/api/rooms/events` | 房间事件 SSE |
| `POST` | `/api/rooms/leave` | 退出房间 |
| `POST` | `/api/rooms/kick` | 踢人 |
| `POST` | `/api/rooms/transfer-owner` | 转让房主 |
| `POST` | `/api/rooms/delete` | 删除房间 |
| `GET` | `/api/client/releases/latest` | 查询客户端最新版 |

登录后接口使用：

```text
Authorization: Bearer <token>
```

## 关键说明

- 房间公钥是加入房间和连接房间的授权凭证，不是节点传输密钥。
- 节点公钥用于客户端校验 Overlay 节点身份。
- 实际传输加密由 `tcpx` 完成。
- 空房间会按配置自动清理，旧房间公钥随房间删除而失效。
- HTTP API 当前仍是普通 HTTP，公网生产部署建议放到 HTTPS 反向代理后面。
