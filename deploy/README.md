# LwyLink 部署与发布

这个目录保存生产部署、发布和运维相关文件。日常发布优先使用 `release.cmd`；只有排查问题、临时跳过某个阶段或服务器初始化时，才需要拆开执行下面的命令。

## 文件速览

| 文件 | 说明 |
| --- | --- |
| `release.cmd` / `release.ps1` | 一键发布入口：同步版本、构建、部署、发布 Windows 客户端并校验 latest 接口 |
| `check.ps1` | 日常开发自检入口：Go 测试、前端构建、发布脚本自测和空白检查 |
| `build-all.ps1` | 本机一键全编译 |
| `upload-deploy.ps1` | 上传已有构建产物、安装 systemd、重启并可选 smoke 验证 |
| `test-release.ps1` | 发布脚本自测，检查版本同步幂等和后台占位符不被误改 |
| `docker-compose.prod.yml` | 生产版 MySQL 和 Redis，只绑定 `127.0.0.1` |
| `lwylink-control.service` | 控制中心 systemd 服务 |
| `lwylink-node.service` | 房间节点 systemd 服务 |
| `TEST_SERVER.md` | 当前测试服务器、端口、远端路径和自动部署速查 |

生产配置位于：

- `configs/control.prod.json`：控制平面配置，包含 HTTP API、管理令牌、数据库、Redis、房间调度和清理参数。
- `configs/node.prod.json`：房间节点配置，包含 Overlay 监听、节点私钥、数据库、Redis、vDHCP 参数。

这些生产配置包含密钥和密码，不要提交、截图或完整贴到聊天里。仓库只保留 `.example.json` 示例配置，真实 `*.prod.json` 只放在本机或服务器。

## 程序分工

| 程序 | 部署位置 | 职责 |
| --- | --- | --- |
| `lwylink-control` | 控制服务器 | 玩家 API、管理后台、节点调度、节点健康检查、空房清理、客户端更新发布 |
| `lwylink-node` | 每台房间节点服务器 | `tcpx` 加密 Overlay、UDP 打洞协商、vDHCP、房间内 IPv4 包转发 |
| `lwylink-smoke` | 本地或服务器 | 端到端验证注册、登录、房间、Overlay、广播、踢人、删除和清理 |

控制 API 和 Overlay 端口分开配置。当前测试服约定：

| 端口 | 归属 | 说明 |
| --- | --- | --- |
| `127.0.0.1:8080/tcp` | `lwylink-control` | 本机 HTTP API 和管理后台，只允许 HTTPS 反向代理访问 |
| `35768/tcp` | `lwylink-node` | Overlay TCP 控制/中继 |
| `35768/udp` | `lwylink-node` | UDP 打洞协商 |
| `443/tcp` | HTTPS 反向代理 | `www.lwylink.xyz` / `lwylink.xyz`，反代到控制端 `8080` |

## 日常一键发布

```powershell
$env:LWYLINK_DEPLOY_SSH_PASSWORD = "这里填 SSH 密码"
.\deploy\release.cmd -Version <version> -Notes "本次更新说明"
```

发布脚本会依次执行：

1. 同步 Windows 客户端版本号到 Go、Wails、前端包文件、前端默认状态和管理端占位文本。
2. 调用 `build-all.ps1 -Vet -AppVersion <version>` 全量构建。
3. 调用 `upload-deploy.ps1` 上传 Linux 服务端二进制、安装 systemd、重启并跑 smoke。
4. 上传 `build/windows-amd64/LwyLink.exe` 到管理端“更新发布”。
5. 请求 `/api/client/releases/latest` 校验线上 latest 版本号和 SHA256。

当前阶段客户端协议和账号/网络逻辑经常是不兼容更新，发布脚本会默认把 Windows 客户端标记为强制更新。
客户端发布和 smoke 默认使用公网 HTTPS API：`https://www.lwylink.xyz`。控制端 `8080` 只监听本机，不再作为公网排障入口；如需排查反向代理，优先在服务器本机 `curl http://127.0.0.1:8080`。

常用参数：

```powershell
.\deploy\release.cmd -Version <version> -Notes "说明" -SkipSmoke
.\deploy\release.cmd -Version <version> -Notes "说明" -SkipDeploy
.\deploy\release.cmd -Version <version> -Notes "说明" -SkipClientRelease
.\deploy\release.cmd -Version <version> -Notes "说明" -SkipBuild
.\deploy\release.cmd -Version <version> -Notes "说明" -UploadConfigs
.\deploy\release.cmd -Version <version> -Notes "说明" -Commit
```

`release.cmd` 会用 `PowerShell -ExecutionPolicy Bypass` 调用 `release.ps1`，适合本机执行策略较严格时使用。

`-SkipBuild` 会跳过全量构建并复用 `build/artifacts.json` 和 `build/windows-amd64/LwyLink.exe` 等已有产物，适合只重试部署或只重试客户端发布。

发布脚本改动后可以先跑：

```powershell
PowerShell -ExecutionPolicy Bypass -File .\deploy\test-release.ps1
```

## 日常自检

代码整理、UI 调整或语音/Overlay 改动后，优先跑统一自检：

```powershell
PowerShell -ExecutionPolicy Bypass -File .\deploy\check.ps1
```

它会依次执行：

1. `go test ./...`
2. Windows UI `npm.cmd run build`
3. `deploy/test-release.ps1`
4. `git diff --check`

需要更重的并发检查时加 `-Race`；需要静态检查时加 `-Vet`。只改服务端时可以加 `-SkipFrontend`，只改业务代码且不涉及发布脚本时可以加 `-SkipReleaseScript`。

## 构建产物

```powershell
.\deploy\build-all.ps1 -Vet
```

默认流程：

1. `go test -p 1 ./...`
2. 可选 `go vet -p 1 ./...`
3. 编译 Linux `amd64` 版 `lwylink-control`、`lwylink-node`、`lwylink-smoke` 到 `deploy/dist/`
4. 编译 Windows `amd64` 版独立虚拟组网引擎到 `build/lwylink-net.exe`
5. 构建 Wails 图形客户端，并复制 `LwyLink.exe` 到 `build/windows-amd64/`
6. 输出 `build/artifacts.json`，包含产物路径、大小和 SHA256

常用参数：

```powershell
.\deploy\build-all.ps1 -SkipTests
.\deploy\build-all.ps1 -SkipWindowsUI
.\deploy\build-all.ps1 -AppVersion <version>
.\deploy\build-all.ps1 -NoInstallWails
```

`-AppVersion` 只通过 Wails `ldflags` 注入 Go 端 `main.AppVersion`，不会修改 `wails.json`、`package.json` 或 `package-lock.json`。正式发布客户端版本时，建议使用 `release.cmd` 或手动同步版本文件。

如果本机找不到 `wails`，脚本会尝试自动安装 `github.com/wailsapp/wails/v2/cmd/wails@v2.12.0`。不希望自动安装时加 `-NoInstallWails`。

## 上传部署

先编译：

```powershell
.\deploy\build-all.ps1
```

再上传到默认测试服务器：

```powershell
$env:LWYLINK_DEPLOY_SSH_PASSWORD = "这里填 SSH 密码"
.\deploy\upload-deploy.ps1
```

也可以显式指定目标：

```powershell
.\deploy\upload-deploy.ps1 -HostName 64.83.34.53 -User root -Port 22
```

脚本默认上传 `deploy/dist/` 下的三个 Linux 产物和两个 systemd 文件，然后安装到 `/opt/lwylink`、重启 `lwylink-control` 和 `lwylink-node`，最后跑 smoke。

常用参数：

```powershell
.\deploy\upload-deploy.ps1 -UploadConfigs
.\deploy\upload-deploy.ps1 -SkipSmoke
.\deploy\upload-deploy.ps1 -RemoteRoot /opt/lwylink
.\deploy\upload-deploy.ps1 -AdminToken "这里填 adminToken"
```

没有传密码时脚本会使用系统 `ssh/scp`，适合已经配置 SSH key 的机器。传了 `-Password` 或设置 `LWYLINK_DEPLOY_SSH_PASSWORD` 时，会使用 `Posh-SSH`。

可用环境变量：

```powershell
$env:LWYLINK_DEPLOY_HOST = "64.83.34.53"
$env:LWYLINK_DEPLOY_SSH_USER = "root"
$env:LWYLINK_DEPLOY_SSH_PORT = "22"
$env:LWYLINK_DEPLOY_SSH_PASSWORD = "这里填 SSH 密码"
$env:LWYLINK_ADMIN_TOKEN = "这里填控制端 adminToken"
$env:LWYLINK_API_URL = "https://www.lwylink.xyz"
```

## 新增房间节点

新节点必须能访问控制中心使用的 MySQL 和 Redis。生产环境建议通过内网或专用网络访问数据库，并用防火墙限制来源。

1. 在新服务器创建目录：

```bash
mkdir -p /opt/lwylink/configs /opt/lwylink/deploy/dist
```

2. 上传节点程序、systemd 文件和节点配置：

```powershell
scp .\deploy\dist\lwylink-node root@<NODE_IP>:/opt/lwylink/lwylink-node
scp .\deploy\lwylink-node.service root@<NODE_IP>:/etc/systemd/system/lwylink-node.service
scp .\configs\node.prod.example.json root@<NODE_IP>:/opt/lwylink/configs/node.prod.json
```

3. 调整 `/opt/lwylink/configs/node.prod.json`：

| 字段 | 说明 |
| --- | --- |
| `overlayAddr` | 节点监听地址，例如 `0.0.0.0:35768` |
| `mysqlDsn` | 指向控制平面使用的 MySQL |
| `redisAddr` / `redisPassword` | 指向控制平面使用的 Redis |
| `tokenSecret` | 必须复制控制平面 `configs/control.prod.json` 中的值 |
| `overlayPrivateKey` | 新节点可以留空，打印公钥时会自动生成并写回 |

4. 生成节点公钥：

```bash
chmod +x /opt/lwylink/lwylink-node
/opt/lwylink/lwylink-node -config /opt/lwylink/configs/node.prod.json -print-public-key
```

5. 在控制后台“节点”模块新增节点：

| 字段 | 示例 |
| --- | --- |
| 名称 | `node-2` |
| 公网 Overlay 地址 | `<NODE_PUBLIC_IP>:35768` |
| 健康检查地址 | 控制平面能访问的地址，可以是内网地址 |
| Overlay 公钥 | 第 4 步输出的值 |
| 最大房间数 | 例如 `100` |
| 状态 | 启用 |

6. 启动节点：

```bash
chmod 600 /opt/lwylink/configs/node.prod.json
systemctl daemon-reload
systemctl enable --now lwylink-node
systemctl restart lwylink-node
systemctl status lwylink-node --no-pager
```

控制后台点“检查”，节点状态为 `online` 后，新房间会自动调度到该节点。

## 同机多节点

同一台机器跑多个节点时，每个节点必须使用独立目录、独立配置、独立端口和独立 `overlayPrivateKey`：

```text
/opt/lwylink-node-1/configs/node.prod.json -> 0.0.0.0:35768
/opt/lwylink-node-2/configs/node.prod.json -> 0.0.0.0:35769
```

每个节点都要分别打印 `overlayPublicKey` 并在后台登记。防火墙也要开放对应的 TCP/UDP 端口。

## 安全注意

- `configs/control.prod.json`、`configs/node.prod.json`、`deploy/.env` 必须保密，并且不应提交到 Git。
- 控制平面配置里的 `tokenSecret`、`adminToken` 可以留空；控制端第一次启动时会自动生成并写回 JSON 文件。
- 房间节点配置里的 `overlayPrivateKey` 可以留空；节点第一次打印公钥时会自动生成并写回。
- 房间节点配置里的 `tokenSecret` 必须和控制平面一致。
- 公网入口使用 HTTPS 反向代理，控制端必须只监听 `127.0.0.1:8080`，不要把 `8080/tcp` 暴露到公网。

## 故障排查命令

```bash
systemctl is-active lwylink-control
systemctl is-active lwylink-node
ss -ltnup | grep -E '(127\.0\.0\.1:8080|:443|:35768) ' || true
journalctl -u lwylink-control -n 50 --no-pager
journalctl -u lwylink-node -n 50 --no-pager
```

本地跑远端 smoke：

```powershell
$env:LWYLINK_ADMIN_TOKEN = "这里填控制端 adminToken"
go run .\cmd\lwylink-smoke -api https://www.lwylink.xyz -overlay 64.83.34.53:35768
```
