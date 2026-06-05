# LwyV-GameNet

LwyV-GameNet 是一个从 LwyV-Net 思路拆出来的“游戏虚拟组网后端 + Windows 客户端”原型。

它保留三层虚拟组网的关键能力：Wintun、虚拟 DHCP、房间内 IP 包转发和 `tcpx` 加密传输；同时去掉复杂的防火墙、全局代理、服务端 NAT 等生产 VPN 逻辑，把重点放到游戏联机房间、玩家账号和房间隔离上。

## 目标

- 玩家可以注册、登录。
- 用户名限制 3-32 位，只允许小写字母、数字、下划线和短横线；用户名不能重复。密码限制 6-128 位，不要求复杂度。
- 同一个玩家账号同一时间只允许一个有效登录；新登录会让旧 token 失效，旧 Overlay 连接会被节点检测后断开。
- 玩家可以创建房间、通过房间公钥加入房间。
- 服务端限制每个玩家同一时间只能处在一个房间中；没有加入房间时才可以创建或加入。
- 房间内只有两种权限：房主和普通成员。房主可以踢出普通成员，也可以把房主转让给房间内其他玩家。
- 房间默认最多 8 人，创建时可设置 2-64 人。
- 每个房间对应一个独立虚拟网段和一个房间公钥。
- 玩家创建或加入房间后，可以手动启动 Wintun 虚拟网卡，并通过 vDHCP 自动获得虚拟 IP、网关、DNS、MTU。
- 房间内玩家的三层 IP 包通过服务端转发，并支持 UDP 广播/组播发现，实现类似局域网游戏联机的效果。
- 账号管理和房间管理分离，后期方便接入更完整的 Web 管理平台或 Windows UI。
- Windows UI 使用 Wails + React + TypeScript，控制台客户端保留为测试入口。
- Overlay 传输使用从 LwyV-Net 迁入的 `tcpx`，客户端会校验服务端公钥。

## 当前架构

```text
Windows 控制台客户端
  -> HTTP API 注册/登录/创建房间/通过房间公钥加入房间
  -> Wintun 虚拟网卡
  -> tcpx 加密 Overlay 隧道
  -> GameNet Room Node

GameNet Control Plane
       -> account 账号域
       -> room 房间域
       -> node 服务器节点域
       -> web 管理界面和 REST API
       -> MySQL 持久化账号/房间
       -> Redis 记录当前有效登录会话、在线状态、房间公钥和房间事件 Pub/Sub

GameNet Room Node
       -> vdhcp 虚拟地址域
       -> overlay 房间内 IP 包转发
       -> tcpx 加密传输
       -> 订阅房间事件，主动断开被踢出、退出、删房或关房后的 Overlay 连接
```

## 目录

```text
cmd/gamenet-control  控制中心入口，只运行 HTTP API 和管理后台
cmd/gamenet-node     房间节点入口，只运行加密 Overlay 和 vDHCP
cmd/gamenet-client   Windows 控制台客户端入口
cmd/gamenet-windows-ui  Wails Windows 图形客户端入口
configs/             示例配置
internal/account     玩家账号、密码、令牌
internal/room        房间、成员、房间公钥、网段
internal/node        房间服务器节点、调度、健康检查
internal/vdhcp       每个房间的虚拟 DHCP 租约
internal/overlay     tcpx 加密隧道协议和房间内 IP 包转发
internal/tcpx        从 LwyV-Net 迁入的 X25519/AES-GCM 加密 TCP 通道
internal/tun         Windows Wintun 封装；非 Windows 为桩实现
internal/web         REST API 和管理页面
internal/db          MySQL 连接和表结构迁移
internal/state       Redis 运行态：登录会话、在线状态、房间公钥索引和事件
```

测试服务器、自动部署和远端服务维护说明见 `deploy/TEST_SERVER.md`。新会话接手部署前请先读这个文件，尤其注意测试服务器上的 `443` 端口属于旧服务，GameNet 只使用 `8080` 和 `35768`。

## 快速开始

### 1. 启动 MySQL 和 Redis

```powershell
docker compose up -d
```

### 2. 启动控制中心

```powershell
Copy-Item .\configs\control.example.json .\configs\control.local.json
go run .\cmd\gamenet-control -config .\configs\control.local.json
```

控制中心会自动创建 MySQL 表。

管理页面：

```text
http://127.0.0.1:8080/admin?token=dev-admin-token
```

### 3. 添加并启动房间节点

先获取节点公钥：

```powershell
Copy-Item .\configs\node.example.json .\configs\node.local.json
go run .\cmd\gamenet-node -config .\configs\node.local.json -print-public-key
```

然后在管理页面添加节点：

- 公网 Overlay 地址：本地测试填 `127.0.0.1:7777`
- 健康检查地址：本地测试填 `127.0.0.1:7777`
- Overlay 公钥：填上一步输出的值
- 最大房间数：例如 `100`

保存后启动房间节点：

```powershell
go run .\cmd\gamenet-node -config .\configs\node.local.json
```

### 4. 启动控制台客户端

Windows 请用管理员 PowerShell 运行，因为 Wintun 和路由配置需要权限。

```powershell
go run .\cmd\gamenet-client
```

控制台里可以依次选择：

未登录时：

1. 注册玩家
2. 登录玩家
3. 忘记密码提示

登录后：

1. 创建房间
2. 使用房间公钥加入房间
3. 查看当前房间和成员
4. 重新连接当前房间虚拟网络
5. 删除/退出当前房间
6. 踢出成员（房主）
7. 转让房主（房主）
8. 退出登录

当前服务端限制一个玩家同一时间只能处在一个房间中。创建或加入房间后，控制台客户端会立即尝试连接当前房间虚拟网络；第 4 项只用于断开后的手动重连。如果需要重新开房，房主先删除自己创建的旧房间；普通成员先退出当前房间。旧房间被删除后，房间公钥会同时从 Redis 失效。

### 5. 启动 Wails Windows 客户端

图形客户端位于 `cmd/gamenet-windows-ui`，使用 Wails + React + TypeScript + Fluent UI。默认 API 地址是 `http://64.83.34.53:8080`，也可以在客户端设置页改成其他服务器 IP。Windows 版 EXE 内嵌 `wintun.dll`，启动时会自动释放到 EXE 同目录；连接虚拟网络时仍然需要管理员权限，因为 Wintun 和路由配置需要写入系统网络设置。

图形客户端按 3 个页面组织界面：账号页负责登录、注册和账号状态；房间页负责创建、加入、成员管理、房间聊天和虚拟网卡连接；设置页负责服务器 API、服务器 IP、虚拟网卡名和本地日志。房主可以复制房间公钥、踢出普通成员、转让房主或删除房间；普通成员可以退出房间。断开后仍可在房间页手动重新启动虚拟网卡。

```powershell
cd .\cmd\gamenet-windows-ui
npm.cmd install
wails dev
```

正式打包：

```powershell
cd .\cmd\gamenet-windows-ui
wails build
```

构建产物：

```text
cmd/gamenet-windows-ui/build/bin/LwyV-GameNet.exe
```

如果 `wails` 不在 PATH，可以使用：

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@v2.12.0
& "$env:USERPROFILE\go\bin\wails.exe" build
```

## REST API

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `POST` | `/api/register` | 注册玩家 |
| `POST` | `/api/login` | 登录并获得 token |
| `GET` | `/api/me` | 查询当前玩家 |
| `POST` | `/api/rooms` | 创建房间 |
| `POST` | `/api/rooms/join` | 使用房间公钥加入房间 |
| `POST` | `/api/rooms/delete` | 房主删除房间，并让房间公钥失效 |
| `GET` | `/api/rooms/members?roomID=1` | 查询房间成员 |
| `POST` | `/api/rooms/chat` | 发送房间聊天消息，并通过 SSE 推送给房间成员 |
| `POST` | `/api/rooms/leave` | 普通成员退出房间 |
| `POST` | `/api/rooms/kick` | 房主踢出普通成员 |
| `POST` | `/api/rooms/transfer-owner` | 房主转让给房间内其他成员 |
| `GET` | `/api/rooms` | 查询自己加入的房间 |
| `GET` | `/api/rooms/events` | SSE 房间事件流，用于客户端实时刷新 |

创建房间请求可以带最大人数：

```json
{
  "name": "我的房间",
  "maxMembers": 8
}
```

登录后的接口使用：

```text
Authorization: Bearer <token>
```

## Overlay 协议

客户端连接 `overlayAddr` 后，先用 `tcpx` 建立加密通道。`tcpx` 使用 X25519 握手、AES-GCM、心跳、重连、密钥轮换和重放窗口。加密通道建立后，客户端发送认证帧：

```json
{
  "token": "<login-token>",
  "roomID": 1,
  "roomPublicKey": "<room-public-key>",
  "requestID": "<dhcp-request-id>"
}
```

服务端验证账号、房间成员关系，并确认 Redis 中仍存在这个房间公钥后，下发 vDHCP 租约。客户端会校验 `requestID`，避免使用过期或错配的地址响应：

```json
{
  "requestID": "<dhcp-request-id>",
  "ip": "172.16.12.1",
  "subnetMask": "255.255.255.0",
  "gateway": "172.16.12.254",
  "dns": ["223.5.5.5", "119.29.29.29"],
  "mtu": 1300
}
```

之后客户端和服务端用 `IP` 帧传输原始 IPv4 包。服务端只在同一个房间内转发，且会校验源 IP 必须等于该玩家租约 IP。

房间发现类流量也会在房间内转发：

- 普通单播：只发给目标虚拟 IP 对应的在线玩家。
- 有限广播 `255.255.255.255`：复制给同房间其他在线玩家。
- 子网广播：例如 `/24` 房间中的 `172.16.80.255`，复制给同房间其他在线玩家。
- IPv4 组播 `224.0.0.0/4`：复制给同房间其他在线玩家，用于支持 mDNS、SSDP 或游戏自定义组播发现。

Windows 客户端连接房间时会临时把房间网段和 IPv4 组播路由指向 Wintun，断开后清理这些路由。

为了提高游戏大厅自动发现的成功率，Windows 客户端还会在连接期间把 `255.255.255.255/32` 有限广播路由到 Wintun，打开虚拟适配器入站/出站防火墙规则，并临时关闭该虚拟适配器的 IPv6 绑定，避免只支持 IPv4 的房间 overlay 被游戏优先使用 IPv6 link-local 发现绕开。

仍有一类情况需要注意：如果游戏强制绑定物理网卡，或者只向物理局域网广播地址例如 `192.168.1.255` 发包，这些包不会进入 Wintun。遇到这种游戏时，先连接 LwyV-GameNet 再启动游戏，并在游戏或启动器里选择 `LwyV-GameNet` / Wintun 虚拟网卡，成功率会高很多。

## 房间公钥策略

创建房间时，服务端会生成一个 X25519 公钥形式的房间公钥：

- MySQL 保存房间、公钥、房主和网段，用于展示和服务端重启后的恢复。
- Redis 保存“房间公钥 -> 房间 ID”的当前有效索引。
- 玩家加入房间时必须提交房间公钥，服务端先查 Redis，再写入成员表。
- 房主删除房间或管理员关闭房间时，服务端会删除 Redis 中的房间公钥索引。
- Redis 中没有这个公钥时，即使玩家知道旧公钥，也不能再加入或连接该房间。
- 控制中心会定期清理没有在线 Overlay 连接的空房间，默认每 10 秒扫描一次，房间创建超过 20 秒仍没有任何在线玩家就会删除房间并让公钥失效。

这里有两类公钥：

- 房间公钥 `roomPublicKey`：房主创建房间后拿到的邀请凭证，其他玩家用它加入房间。
- 节点公钥 `nodePublicKey`：房间节点的 tcpx 身份公钥，客户端连接节点时用它校验服务端身份。

房间公钥不是节点传输密钥。房间公钥用于加入/连接授权，实际传输加密仍由 tcpx 使用节点密钥完成。

## 空房间清理

房间是否“有人”以客户端房间心跳或 Overlay 在线状态为准。玩家打开 Windows 客户端后会建立 `/api/rooms/events` SSE 事件流；这个事件流会给控制面发送轻量房间心跳，不再依赖客户端每 2 秒轮询完整房间状态。创建、加入、查询成员、踢人、转让房主等房间操作也会在服务端刷新操作者的房间心跳，避免手动启动虚拟网卡前被误判为空房。

玩家真正启动虚拟网卡后，节点还会写入带虚拟 IP 的 Overlay 在线记录。房间成员接口会返回玩家是否在线以及当前虚拟 IP，如果只显示在线但没有虚拟 IP，表示客户端还在房间里但没有启动虚拟网卡。踢出或退出房间时，服务端只会清理该玩家在当前房间的在线状态，不会影响房间内其他成员。

房间成员变化、房主转让、删房、关房和 Overlay 在线变化都会通过 Redis Pub/Sub 发布房间事件。Windows 客户端收到自己的用户事件后立即刷新房间状态；房间节点订阅房间事件后会立刻 revalidate 当前 Overlay 连接，被踢出、退出、删房或关房的连接会被主动断开。节点仍保留短周期 revalidate 作为兜底，用于处理 Redis 事件丢失或节点刚重连的情况。

控制中心启动 `roomCleanupIntervalSeconds` 定时任务，默认 10 秒检查一次；对于超过 `roomEmptyGraceSeconds` 的房间，默认 20 秒，如果同房间没有任何客户端心跳或 Overlay 在线玩家，就删除 MySQL 房间记录、成员关系、Redis 房间公钥索引和在线状态。

这意味着房主创建房间后如果一直没有启动虚拟网卡，或者所有玩家都掉线/退出导致房间没有在线连接，房间会在宽限期后自动消失，其他人也不能再用旧房间公钥加入。

## vDHCP 地址

默认配置 `roomNetworkBase = 172.16.0.0/16`，`roomSubnetPrefix = 24`，所以每个房间会获得一个独立 `/24` 网段，例如 `172.16.80.0/24`。客户端拿到的虚拟 IP 掩码就是 `255.255.255.0`，网关是该 `/24` 的 `.254`，玩家 IP 从 `.1` 开始分配。

## 多节点拆分

项目现在固定拆成两个程序：

- `gamenet-control`：控制中心，只跑玩家 API、管理后台、节点调度和健康检查。
- `gamenet-node`：房间节点，只跑 tcpx Overlay、vDHCP 和房间内 IP 包转发。

控制中心的管理页可以添加服务器节点。每个节点包含：

- 公网 Overlay 地址，例如 `1.2.3.4:35768`。
- 健康检查地址，例如控制中心可访问的内网地址 `10.0.0.8:35768` 或本机 `127.0.0.1:35768`。
- Overlay 公钥，用于客户端校验连接的节点身份。
- 最大房间数，用于负载上限。

房间创建时，控制中心会在可用节点中自动选择：只使用启用、在线、未达到最大房间数的节点，然后按 `当前房间数 / 最大房间数` 计算负载，选择负载最低的节点；负载相同则选择 ID 更小的节点。

节点健康检查会定期探测节点的公网 Overlay 端口。端口不可达时，节点会被标记为 `paused`，调度器不会再给它分配新房间，等待管理员处理。节点进程本身交给 systemd、Docker 或你的运维脚本管理。

### 新增房间节点

当前节点程序会直接访问 MySQL 和 Redis 校验账号、房间公钥和成员关系，所以新节点服务器必须能访问控制中心使用的 MySQL/Redis。单机部署时直接在同一台服务器启动多个节点即可；多服务器部署时，建议让节点通过内网或专用网络访问控制中心的 MySQL/Redis，并用防火墙只允许节点服务器访问这些端口。

新增节点的大致流程：

1. 在新服务器准备 `/opt/lwyv-gamenet`，放入 `gamenet-node` 和 `configs/node.prod.json`。
2. 在 `configs/node.prod.json` 中把 `overlayPrivateKey` 留空，然后生成并打印该节点的 Overlay 公钥：

```bash
/opt/lwyv-gamenet/gamenet-node -config /opt/lwyv-gamenet/configs/node.prod.json -print-public-key
```

这条命令会自动生成 `overlayPrivateKey` 并写回配置文件，同时打印要填入控制后台的 `overlayPublicKey`。

3. 确认新节点的 `configs/node.prod.json` 中这些配置正确：
   - `overlayAddr`：例如 `0.0.0.0:35768`。
   - `mysqlDsn`：指向控制中心 MySQL。
   - `redisAddr` / `redisPassword`：指向控制中心 Redis。
   - `tokenSecret`：必须复制控制中心 `configs/control.prod.json` 中的值。
4. 在管理后台的“节点”模块新增节点：
   - 名称：例如 `node-2`。
   - 公网 Overlay 地址：玩家客户端能访问的地址，例如 `2.2.2.2:35768`。
   - 健康检查地址：控制中心能访问的地址，可以是内网地址，例如 `10.0.0.12:35768`。
   - Overlay 公钥：填第 2 步打印的公钥。
   - 最大房间数：按机器配置设置，例如 `100`。
5. 安装并启动 systemd：

```bash
cp deploy/lwyv-gamenet-node.service /etc/systemd/system/lwyv-gamenet-node.service
systemctl daemon-reload
systemctl enable --now lwyv-gamenet-node
```

6. 回到管理后台点“检查”，状态变成 `online` 后，新创建的房间就会按负载自动分配到这个节点。

节点服务器不需要配置 `nodeID`。节点启动时会用本机 `overlayPrivateKey` 派生出的 `overlayPublicKey` 到控制平面数据库中查找自己的节点记录，所以控制平面只需要知道“这个公钥对应哪个节点”。

控制中心配置里的 `tokenSecret`、`adminToken` 可以留空。控制端第一次启动时会自动生成这些值并写回 JSON 文件；如果已经配置了真实值，则不会覆盖。房间节点配置里的 `overlayPrivateKey` 可以留空，但 `tokenSecret` 必须和控制中心一致。

## 有意简化的部分

- Windows 侧只做连接期间必要的 Wintun 路由和虚拟适配器防火墙放行，断开后清理。
- 暂不做全局代理路由。
- 暂不做服务端 NAT 出口。
- 暂不做 P2P 打洞，全部先走服务端中继。
- HTTP API 当前仍是普通 HTTP，公网部署建议放到 HTTPS 反向代理后面。

这些简化是为了先把“账号 -> 房间 -> 虚拟网段 -> vDHCP -> 房间内转发”的主链路打通。

## 后续建议

- Windows UI 后续接入系统托盘、自动更新、配置持久化和后台服务模式。
- 房间增加密码、白名单、房间聊天和邀请链接过期策略。
- 账号增加邮箱、头像、封禁、角色权限。
- HTTP API 增加 HTTPS、CSRF 策略和更完整的管理权限。
- vDHCP 租约持久化，服务端重启后保持同房间同玩家 IP 稳定。
- Web 管理页替换为正式前端，例如 Vue/React。
