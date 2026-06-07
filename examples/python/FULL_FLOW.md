# Python 完整接入流程

1. 登录并保存 token：

```powershell
uv run python .\examples\python\lwylink_room_example.py --api http://127.0.0.1:8080 login --email alice@example.com --password password
```

2. 创建房间：

```powershell
uv run python .\examples\python\lwylink_room_example.py --api http://127.0.0.1:8080 --token "<token>" create-room --name "Alice Room" --max-members 8
```

3. 监听房间事件：

```powershell
uv run python .\examples\python\lwylink_room_example.py --api http://127.0.0.1:8080 --token "<token>" events
```

4. 启动虚拟组网：

```powershell
$env:LWYLINK_TOKEN = "<token>"
uv run python .\examples\python\lwylink_net_example.py connect --api http://127.0.0.1:8080 --room-id 123
```
