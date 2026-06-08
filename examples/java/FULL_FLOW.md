# Java 完整接入流程

1. 编译示例：

```powershell
javac .\examples\java\LwyLink.java .\examples\java\LwyLinkRoomExample.java
javac .\examples\java\LwyLink.java .\examples\java\LwyLinkNetExample.java
```

2. 登录并保存 token：

```powershell
java -cp .\examples\java LwyLinkRoomExample login --email alice@example.com --password password
```

3. 创建房间：

```powershell
java -cp .\examples\java LwyLinkRoomExample --token "<token>" create-room --name "Alice Room" --max-members 8
```

4. 监听房间事件：

```powershell
java -cp .\examples\java LwyLinkRoomExample --token "<token>" events
```

5. 启动虚拟组网：

```powershell
$env:LWYLINK_TOKEN = "<token>"
java -cp .\examples\java LwyLinkNetExample connect --room-id 123
```

以上命令默认连接 `https://www.lwylink.xyz`。本地开发时可追加 `--api http://127.0.0.1:8080`。
