# 外部语言接入示例

这个目录用于放其他语言接入 LwyLink 的示例。

完整接入分两层：

1. 房间系统：HTTP JSON API + SSE 房间事件流。
2. 虚拟组网：调用 `build/lwylink-net.exe`，读取 stdout JSON Lines 状态。

## Python

- `examples/python/lwylink.py`：可复制到项目里的标准库客户端小库。
- `examples/python/lwylink_room_example.py`：账号、房间、聊天、SSE 示例。
- `examples/python/lwylink_net_example.py`：调用独立虚拟组网 EXE 示例。

## Java

- `examples/java/LwyLink.java`：可复制到项目里的 JDK 标准库客户端小库。
- `examples/java/LwyLinkRoomExample.java`：账号、房间、聊天、SSE 示例。
- `examples/java/LwyLinkNetExample.java`：调用独立虚拟组网 EXE 示例。

## 推荐流程

1. 使用房间示例登录，拿到 token。
2. 使用房间示例创建或加入房间。
3. 使用房间示例订阅 `/api/rooms/events`，监听成员、聊天、踢出、房间删除等事件。
4. 使用组网示例调用 `lwylink-net.exe connect` 启动虚拟网络。

虚拟组网推荐使用 API 模式，只需要把登录 token 传给 `lwylink-net.exe`，它会自动向控制端拉取当前房间和节点信息。
