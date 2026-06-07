# Java 完整接入流程

1. 编译示例：

```powershell
javac .\examples\java\LwyLink.java .\examples\java\LwyLinkRoomExample.java
javac .\examples\java\LwyLink.java .\examples\java\LwyLinkNetExample.java
```

2. 登录并保存 token：

```powershell
java -cp .\examples\java LwyLinkRoomExample --api http://127.0.0.1:8080 login --email alice@example.com --password password
```

3. 创建房间：

```powershell
java -cp .\examples\java LwyLinkRoomExample --api http://127.0.0.1:8080 --token "<token>" create-room --name "Alice Room" --max-members 8
```

4. 监听房间事件：

```powershell
java -cp .\examples\java LwyLinkRoomExample --api http://127.0.0.1:8080 --token "<token>" events
```

5. 启动虚拟组网：

```powershell
$env:LWYLINK_TOKEN = "<token>"
java -cp .\examples\java LwyLinkNetExample connect --api http://127.0.0.1:8080 --room-id 123
```
