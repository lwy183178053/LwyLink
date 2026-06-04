package web

import "html/template"

var adminModuleTemplate = template.Must(template.New("admin-module").Funcs(template.FuncMap{
	"activeClass":     activeClass,
	"statusClass":     statusClass,
	"userStatusText":  userStatusText,
	"userStatusClass": userStatusClass,
	"timeText":        timeText,
	"timePtrText":     timePtrText,
}).Parse(`<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}} - LwyV-GameNet Admin</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f4f6f8;
      --side: #17202a;
      --side-soft: #202b38;
      --panel: #ffffff;
      --line: #d8dee6;
      --line-soft: #e8edf2;
      --text: #19212b;
      --muted: #657282;
      --accent: #176b74;
      --accent-soft: #e7f5f3;
      --good: #147346;
      --warn: #946200;
      --bad: #b42318;
    }
    * { box-sizing: border-box; }
    body { margin: 0; min-height: 100vh; font-family: system-ui, "Segoe UI", Arial, sans-serif; color: var(--text); background: var(--bg); }
    a { color: inherit; }
    .shell { display: grid; grid-template-columns: 236px minmax(0, 1fr); min-height: 100vh; }
    aside { position: sticky; top: 0; height: 100vh; padding: 20px 14px; color: #eef4f8; background: var(--side); }
    main { min-width: 0; padding: 24px 30px 42px; }
    .brand { padding: 4px 10px 18px; border-bottom: 1px solid rgba(255,255,255,.12); margin-bottom: 14px; }
    .brand strong { display: block; font-size: 18px; line-height: 1.2; }
    .brand span { display: block; margin-top: 4px; color: #a8b5c2; font-size: 12px; }
    nav { display: grid; gap: 4px; }
    .nav-link { display: flex; align-items: center; min-height: 38px; padding: 8px 10px; border-radius: 6px; color: #dbe5ed; text-decoration: none; font-size: 14px; }
    .nav-link:hover, .nav-link.active { color: #fff; background: var(--side-soft); }
    .topbar { display: flex; align-items: flex-end; justify-content: space-between; gap: 18px; margin-bottom: 18px; }
    h1 { margin: 0; font-size: 24px; line-height: 1.2; letter-spacing: 0; }
    h2 { margin: 0 0 12px; font-size: 17px; letter-spacing: 0; }
    .muted { color: var(--muted); font-size: 13px; }
    .metrics { display: grid; grid-template-columns: repeat(4, minmax(138px, 1fr)); gap: 12px; margin-bottom: 18px; }
    .metric { min-height: 74px; padding: 12px 14px; border: 1px solid var(--line); border-radius: 8px; background: var(--panel); }
    .metric strong { display: block; font-size: 24px; line-height: 1.1; }
    .metric span { display: block; margin-top: 6px; color: var(--muted); font-size: 12px; }
    .module { border: 1px solid var(--line); border-radius: 8px; background: var(--panel); overflow: hidden; margin-bottom: 18px; }
    .module-head { display: flex; align-items: center; justify-content: space-between; gap: 14px; padding: 14px 16px; border-bottom: 1px solid var(--line-soft); background: #fbfcfd; }
    .module-body { padding: 14px 16px 16px; }
    .toolbar { display: grid; grid-template-columns: minmax(220px, 1fr) 180px auto auto; gap: 10px; align-items: end; padding: 14px 16px; border-bottom: 1px solid var(--line-soft); background: #fbfcfd; }
    .toolbar label, .form-grid label { display: grid; gap: 5px; color: #475569; font-size: 12px; }
    input, select { width: 100%; min-height: 34px; padding: 6px 8px; border: 1px solid #c8d2dc; border-radius: 6px; background: #fff; color: var(--text); font: inherit; }
    button, .button { min-height: 34px; display: inline-flex; align-items: center; justify-content: center; gap: 6px; padding: 6px 11px; border: 1px solid #a8b5c1; border-radius: 6px; background: #fff; color: var(--text); cursor: pointer; text-decoration: none; white-space: nowrap; font: inherit; font-size: 13px; }
    button:hover, .button:hover { background: #f1f5f8; }
    .primary { color: #fff; border-color: var(--accent); background: var(--accent); }
    .primary:hover { background: #125b62; }
    .danger { color: var(--bad); border-color: #e0aaa5; background: #fff6f5; }
    .danger:hover { background: #ffeceb; }
    .form-grid { display: grid; grid-template-columns: repeat(4, minmax(150px, 1fr)); gap: 10px; }
    .span-2 { grid-column: span 2; }
    .span-all { grid-column: 1 / -1; }
    .table-wrap { width: 100%; overflow-x: auto; }
    table { width: 100%; min-width: 760px; border-collapse: collapse; }
    th, td { padding: 9px 10px; border-bottom: 1px solid var(--line-soft); text-align: left; vertical-align: top; font-size: 13px; }
    th { background: #f0f3f6; color: #334155; font-weight: 650; white-space: nowrap; }
    tr:last-child td { border-bottom: 0; }
    tbody tr:hover { background: #fafcff; }
    .status { display: inline-flex; align-items: center; justify-content: center; min-width: 58px; padding: 3px 7px; border-radius: 999px; border: 1px solid var(--line); background: #f8fafc; font-size: 12px; }
    .status.good { color: var(--good); border-color: #9bd7b4; background: #effaf3; }
    .status.warn { color: var(--warn); border-color: #e6c278; background: #fff8e6; }
    .status.bad { color: var(--bad); border-color: #efb2ac; background: #fff1f0; }
    .mono { font-family: ui-monospace, SFMono-Regular, Consolas, monospace; word-break: break-all; }
    .actions { display: flex; gap: 6px; flex-wrap: wrap; align-items: center; }
    .empty { padding: 22px; color: var(--muted); text-align: center; }
    .compact { min-width: 0; }
    @media (max-width: 980px) {
      .shell { grid-template-columns: 1fr; }
      aside { position: static; height: auto; }
      nav { grid-template-columns: repeat(4, minmax(0, 1fr)); }
      main { padding: 18px 14px 30px; }
      .topbar { display: block; }
      .metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); margin-top: 14px; }
      .toolbar { grid-template-columns: 1fr; }
      .form-grid { grid-template-columns: 1fr; }
      .span-2, .span-all { grid-column: auto; }
    }
  </style>
</head>
<body>
  <div class="shell">
    <aside>
      <div class="brand">
        <strong>LwyV-GameNet</strong>
        <span>Control Plane</span>
      </div>
      <nav>
        {{range .Nav}}
          <a class="nav-link {{activeClass $.Active .Key}}" href="{{.URL}}">{{.Label}}</a>
        {{end}}
      </nav>
    </aside>
    <main>
      <div class="topbar">
        <div>
          <h1>{{.Title}}</h1>
          <div class="muted">后台管理控制台</div>
        </div>
      </div>

      {{if eq .Active "overview"}}
        <div class="metrics">
          <div class="metric"><strong>{{.Stats.Users}}</strong><span>玩家，{{.Stats.ActiveUsers}} 正常 / {{.Stats.DisabledUsers}} 禁用</span></div>
          <div class="metric"><strong>{{.Stats.Rooms}}</strong><span>房间，{{.Stats.OpenRooms}} 开放 / {{.Stats.ClosedRooms}} 关闭</span></div>
          <div class="metric"><strong>{{.Stats.Nodes}}</strong><span>节点，{{.Stats.OnlineNodes}} 在线 / {{.Stats.PausedNodes}} 暂停</span></div>
          <div class="metric"><strong>{{.Stats.OpenRooms}}</strong><span>当前可加入房间</span></div>
        </div>
        <section class="module">
          <div class="module-head"><h2>最近玩家</h2><a class="button" href="/admin/users?token={{.Token}}">打开玩家模块</a></div>
          <div class="table-wrap">
            <table>
              <thead><tr><th>ID</th><th>用户名</th><th>昵称</th><th>状态</th><th>创建时间</th></tr></thead>
              <tbody>
              {{range .Users}}
                <tr><td>{{.ID}}</td><td>{{.Username}}</td><td>{{.DisplayName}}</td><td><span class="status {{userStatusClass .Disabled}}">{{userStatusText .Disabled}}</span></td><td>{{timeText .CreatedAt}}</td></tr>
              {{else}}
                <tr><td colspan="5" class="empty">暂无玩家</td></tr>
              {{end}}
              </tbody>
            </table>
          </div>
        </section>
        <section class="module">
          <div class="module-head"><h2>最近房间</h2><a class="button" href="/admin/rooms?token={{.Token}}">打开房间模块</a></div>
          <div class="table-wrap">
            <table>
              <thead><tr><th>ID</th><th>名称</th><th>房主</th><th>节点</th><th>人数</th><th>网段</th><th>状态</th></tr></thead>
              <tbody>
              {{range .Rooms}}
                <tr><td>{{.ID}}</td><td>{{.Name}}</td><td>{{.OwnerUserID}}</td><td>{{if .NodeName}}{{.NodeName}}{{else}}未分配{{end}}</td><td>{{.MemberCount}} / {{.MaxMembers}}</td><td>{{.NetworkCIDR}}</td><td><span class="status {{statusClass .Status}}">{{.Status}}</span></td></tr>
              {{else}}
                <tr><td colspan="7" class="empty">暂无房间</td></tr>
              {{end}}
              </tbody>
            </table>
          </div>
        </section>
        <section class="module">
          <div class="module-head"><h2>最近节点</h2><a class="button" href="/admin/nodes?token={{.Token}}">打开节点模块</a></div>
          <div class="table-wrap">
            <table>
              <thead><tr><th>ID</th><th>名称</th><th>Overlay</th><th>负载</th><th>状态</th></tr></thead>
              <tbody>
              {{range .Nodes}}
                <tr><td>{{.ID}}</td><td>{{.Name}}</td><td class="mono">{{.PublicOverlayAddr}}</td><td>{{.CurrentRooms}} / {{.MaxRooms}}</td><td><span class="status {{statusClass .Status}}">{{.Status}}</span></td></tr>
              {{else}}
                <tr><td colspan="5" class="empty">暂无节点</td></tr>
              {{end}}
              </tbody>
            </table>
          </div>
        </section>
      {{end}}

      {{if eq .Active "nodes"}}
        <section class="module">
          <form class="toolbar" method="get" action="/admin/nodes">
            <input type="hidden" name="token" value="{{.Token}}">
            <label><span>搜索</span><input name="q" value="{{.Query}}" placeholder="名称、地址、错误"></label>
            <label><span>状态</span>
              <select name="status">
                <option value="">全部</option>
                <option value="online" {{if eq .Status "online"}}selected{{end}}>online</option>
                <option value="paused" {{if eq .Status "paused"}}selected{{end}}>paused</option>
                <option value="disabled" {{if eq .Status "disabled"}}selected{{end}}>disabled</option>
                <option value="unknown" {{if eq .Status "unknown"}}selected{{end}}>unknown</option>
              </select>
            </label>
            <button class="primary" type="submit">查询</button>
            <a class="button" href="/admin/nodes?token={{.Token}}">重置</a>
          </form>
          <div class="module-body">
            <form class="form-grid" method="post" action="/admin/nodes/save">
              <input type="hidden" name="token" value="{{.Token}}">
              <label>节点 ID<input name="nodeID" placeholder="新增留空"></label>
              <label>名称<input name="name" placeholder="hk-1"></label>
              <label>最大房间数<input name="maxRooms" value="100"></label>
              <label class="span-2">公网 Overlay 地址<input name="publicOverlayAddr" placeholder="1.2.3.4:35768"></label>
              <label class="span-2">健康检查地址<input name="healthAddr" placeholder="127.0.0.1:35768"></label>
              <label class="span-2">Overlay 公钥<input name="overlayPublicKey" class="mono"></label>
              <label>启用
                <select name="enabled"><option value="true">启用</option><option value="false">禁用</option></select>
              </label>
              <div class="span-all"><button class="primary" type="submit">保存节点</button></div>
            </form>
          </div>
        </section>
        <section class="module">
          <div class="table-wrap">
            <table>
              <thead><tr><th>ID</th><th>名称</th><th>Overlay</th><th>健康检查</th><th>负载</th><th>状态</th><th>最近在线</th><th>错误</th><th>操作</th></tr></thead>
              <tbody>
              {{range .Nodes}}
                <tr>
                  <td>{{.ID}}</td>
                  <td>{{.Name}}</td>
                  <td class="mono">{{.PublicOverlayAddr}}<br><span class="muted">{{.OverlayPublicKey}}</span></td>
                  <td class="mono">{{.HealthAddr}}</td>
                  <td>{{.CurrentRooms}} / {{.MaxRooms}}</td>
                  <td><span class="status {{statusClass .Status}}">{{.Status}}</span></td>
                  <td>{{timePtrText .LastSeenAt}}</td>
                  <td class="mono">{{.LastError}}</td>
                  <td>
                    <div class="actions">
                      <form method="post" action="/admin/nodes/check">
                        <input type="hidden" name="token" value="{{$.Token}}">
                        <input type="hidden" name="nodeID" value="{{.ID}}">
                        <button type="submit">检查</button>
                      </form>
                      <form method="post" action="/admin/nodes/enabled">
                        <input type="hidden" name="token" value="{{$.Token}}">
                        <input type="hidden" name="nodeID" value="{{.ID}}">
                        <input type="hidden" name="enabled" value="{{if .Enabled}}false{{else}}true{{end}}">
                        <button type="submit">{{if .Enabled}}停用{{else}}启用{{end}}</button>
                      </form>
                    </div>
                  </td>
                </tr>
              {{else}}
                <tr><td colspan="9" class="empty">没有匹配的节点</td></tr>
              {{end}}
              </tbody>
            </table>
          </div>
        </section>
      {{end}}

      {{if eq .Active "users"}}
        <section class="module">
          <form class="toolbar" method="get" action="/admin/users">
            <input type="hidden" name="token" value="{{.Token}}">
            <label><span>搜索</span><input name="q" value="{{.Query}}" placeholder="ID、用户名、昵称、角色"></label>
            <label><span>状态</span>
              <select name="status">
                <option value="">全部</option>
                <option value="active" {{if eq .Status "active"}}selected{{end}}>正常</option>
                <option value="disabled" {{if eq .Status "disabled"}}selected{{end}}>禁用</option>
              </select>
            </label>
            <button class="primary" type="submit">查询</button>
            <a class="button" href="/admin/users?token={{.Token}}">重置</a>
          </form>
          <div class="table-wrap">
            <table>
              <thead><tr><th>ID</th><th>用户名</th><th>昵称</th><th>角色</th><th>状态</th><th>创建时间</th><th>更新时间</th><th>操作</th></tr></thead>
              <tbody>
              {{range .Users}}
                <tr>
                  <td>{{.ID}}</td>
                  <td>{{.Username}}</td>
                  <td>{{.DisplayName}}</td>
                  <td>{{.Role}}</td>
                  <td><span class="status {{userStatusClass .Disabled}}">{{userStatusText .Disabled}}</span></td>
                  <td>{{timeText .CreatedAt}}</td>
                  <td>{{timeText .UpdatedAt}}</td>
                  <td>
                    <div class="actions">
                      <form method="post" action="/admin/users/status">
                        <input type="hidden" name="token" value="{{$.Token}}">
                        <input type="hidden" name="q" value="{{$.Query}}">
                        <input type="hidden" name="status" value="{{$.Status}}">
                        <input type="hidden" name="userID" value="{{.ID}}">
                        <input type="hidden" name="disabled" value="{{if .Disabled}}false{{else}}true{{end}}">
                        <button type="submit">{{if .Disabled}}启用{{else}}禁用{{end}}</button>
                      </form>
                      <form method="post" action="/admin/users/delete" onsubmit="return confirm('确认删除这个玩家账户？该玩家创建的房间会一起删除。');">
                        <input type="hidden" name="token" value="{{$.Token}}">
                        <input type="hidden" name="q" value="{{$.Query}}">
                        <input type="hidden" name="status" value="{{$.Status}}">
                        <input type="hidden" name="userID" value="{{.ID}}">
                        <button class="danger" type="submit">删除</button>
                      </form>
                    </div>
                  </td>
                </tr>
              {{else}}
                <tr><td colspan="8" class="empty">没有匹配的玩家</td></tr>
              {{end}}
              </tbody>
            </table>
          </div>
        </section>
      {{end}}

      {{if eq .Active "rooms"}}
        <section class="module">
          <form class="toolbar" method="get" action="/admin/rooms">
            <input type="hidden" name="token" value="{{.Token}}">
            <label><span>搜索</span><input name="q" value="{{.Query}}" placeholder="ID、名称、房主 ID、公钥、网段、节点"></label>
            <label><span>状态</span>
              <select name="status">
                <option value="">全部</option>
                <option value="open" {{if eq .Status "open"}}selected{{end}}>open</option>
                <option value="closed" {{if eq .Status "closed"}}selected{{end}}>closed</option>
              </select>
            </label>
            <button class="primary" type="submit">查询</button>
            <a class="button" href="/admin/rooms?token={{.Token}}">重置</a>
          </form>
          <div class="table-wrap">
            <table>
              <thead><tr><th>ID</th><th>名称</th><th>房主</th><th>节点</th><th>人数</th><th>房间公钥</th><th>网段</th><th>状态</th><th>创建时间</th><th>操作</th></tr></thead>
              <tbody>
              {{range .Rooms}}
                <tr>
                  <td>{{.ID}}</td>
                  <td>{{.Name}}</td>
                  <td>{{.OwnerUserID}}</td>
                  <td>{{if .NodeName}}{{.NodeName}}<br><span class="mono">{{.NodeOverlayAddr}}</span>{{else}}未分配{{end}}</td>
                  <td>{{.MemberCount}} / {{.MaxMembers}}</td>
                  <td class="mono compact">{{.RoomPublicKey}}</td>
                  <td>{{.NetworkCIDR}}</td>
                  <td><span class="status {{statusClass .Status}}">{{.Status}}</span></td>
                  <td>{{timeText .CreatedAt}}</td>
                  <td>
                    <form method="post" action="/admin/rooms/status">
                      <input type="hidden" name="token" value="{{$.Token}}">
                      <input type="hidden" name="q" value="{{$.Query}}">
                      <input type="hidden" name="filterStatus" value="{{$.Status}}">
                      <input type="hidden" name="roomID" value="{{.ID}}">
                      <input type="hidden" name="newStatus" value="{{if eq .Status "open"}}closed{{else}}open{{end}}">
                      <button type="submit">{{if eq .Status "open"}}关闭{{else}}重新开放{{end}}</button>
                    </form>
                  </td>
                </tr>
              {{else}}
                <tr><td colspan="10" class="empty">没有匹配的房间</td></tr>
              {{end}}
              </tbody>
            </table>
          </div>
        </section>
      {{end}}
    </main>
  </div>
</body>
</html>`))
