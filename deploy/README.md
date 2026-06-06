# 生产部署说明

这个目录放生产部署相关文件：

- `docker-compose.prod.yml`：生产版 MySQL 和 Redis，只绑定 `127.0.0.1`。
- `lwylink-control.service`：控制平面 systemd 服务。
- `lwylink-node.service`：房间节点 systemd 服务。
- `TEST_SERVER.md`：当前测试服务器、端口、远端路径和自动部署流程速查。
- `deploy-test.ps1`：本机 PowerShell 自动构建、上传、重启并 smoke 测试当前测试服务器。

如果是新会话接手项目，优先阅读 `deploy/TEST_SERVER.md`，里面写了测试服务器连接方式、不能碰的 443 旧服务、自动部署命令和 smoke 验证点。

生产配置已经拆成两份：

- `configs/control.prod.json`：控制平面配置，包含 HTTP API、管理令牌、数据库、Redis、房间调度和清理参数。
- `configs/node.prod.json`：房间节点配置，包含 Overlay 监听、节点私钥、数据库、Redis、vDHCP 参数。

公网 HTTP API 和 Overlay 监听端口分别配置。当前建议：

- 控制 API：`0.0.0.0:8080`，Windows 客户端通过这个端口登录、创建房间、加入房间。
- 房间 Overlay：`0.0.0.0:35768`，因为 443 已经被旧服务占用，LwyLink 不要抢占 443。

## 程序分工

- `lwylink-control`：部署在管理服务器上的 LwyLink 控制中心，负责玩家 API、管理后台、节点调度、节点健康检查、空房清理。
- `lwylink-node`：部署在每台房间节点服务器上的 LwyLink 房间节点，负责 tcpx 加密 Overlay、vDHCP、房间内 IP 包转发。

节点服务器不需要配置 `nodeID`。节点只保存自己的 `overlayPrivateKey`，启动时会派生出 `overlayPublicKey`，再用这个公钥到控制平面数据库里找到自己的节点记录。

## 新增房间节点

1. 在新服务器创建目录：

```bash
mkdir -p /opt/lwylink/configs
```

2. 上传 `lwylink-node` 和节点配置到新服务器：

```text
/opt/lwylink/lwylink-node
/opt/lwylink/configs/node.prod.json
```

3. 在节点配置中把 `overlayPrivateKey` 留空，然后生成并打印该节点的 Overlay 公钥：

```bash
/opt/lwylink/lwylink-node -config /opt/lwylink/configs/node.prod.json -print-public-key
```

这条命令会自动生成 `overlayPrivateKey` 并写回配置文件，同时打印要填入控制后台的 `overlayPublicKey`。

4. 确认该节点的 `configs/node.prod.json` 中这些配置正确：

- `overlayAddr`：节点监听地址，例如 `0.0.0.0:35768`。
- `mysqlDsn`：指向控制平面使用的 MySQL。
- `redisAddr` / `redisPassword`：指向控制平面使用的 Redis。
- `tokenSecret`：必须复制控制平面 `configs/control.prod.json` 中的值。

5. 在控制后台“节点”模块新增节点：

- 名称：例如 `node-2`。
- 公网 Overlay 地址：玩家客户端能访问的地址，例如 `2.2.2.2:35768`。
- 健康检查地址：控制平面能访问的地址，可以是内网地址，例如 `10.0.0.12:35768`。
- Overlay 公钥：填第 3 步输出的 `overlayPublicKey`。
- 最大房间数：按机器配置设置，例如 `100`。

6. 安装并启动 systemd：

```bash
cp /opt/lwylink/deploy/lwylink-node.service /etc/systemd/system/lwylink-node.service
systemctl daemon-reload
systemctl enable --now lwylink-node
```

7. 回到控制后台点“检查”。节点状态变成 `online` 后，新创建的房间就会按负载自动分配到这个节点。

## 安全注意

`configs/control.prod.json`、`configs/node.prod.json` 和 `.env` 必须保密，它们包含数据库密码、管理令牌和长期加密密钥。

控制平面配置里的 `tokenSecret`、`adminToken` 可以留空。控制端第一次启动时会自动生成这些值并写回 JSON 文件；如果已经配置了真实值，则不会覆盖。房间节点配置里的 `overlayPrivateKey` 可以留空，但 `tokenSecret` 必须和控制平面一致。
