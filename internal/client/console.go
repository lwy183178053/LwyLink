package client

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/LwyV-Labs/LwyV-GameNet/internal/account"
	"github.com/LwyV-Labs/LwyV-GameNet/internal/room"
)

func RunConsole(ctx context.Context, apiBaseURL, ifName string) error {
	api := NewAPI(apiBaseURL)
	reader := bufio.NewReader(os.Stdin)
	var currentUser *account.PublicUser

	for {
		if currentUser == nil {
			user, exit := runGuestMenu(ctx, reader, api)
			if exit {
				return nil
			}
			currentUser = user
			continue
		}

		logout, exit := runPlayerMenu(ctx, reader, api, ifName, *currentUser)
		if exit {
			return nil
		}
		if logout {
			api.ClearToken()
			currentUser = nil
		}
	}
}

// runGuestMenu handles unauthenticated flows only. Password recovery is a safe
// placeholder until the account domain has verified email/phone support.
func runGuestMenu(ctx context.Context, reader *bufio.Reader, api *API) (*account.PublicUser, bool) {
	for {
		fmt.Println()
		fmt.Println("LwyV-GameNet Console")
		fmt.Println("1. 注册玩家")
		fmt.Println("2. 登录玩家")
		fmt.Println("3. 忘记密码")
		fmt.Println("0. 退出")
		switch prompt(reader, "选择: ") {
		case "1":
			username := prompt(reader, "用户名: ")
			password := prompt(reader, "密码: ")
			displayName := prompt(reader, "昵称(可空): ")
			res, err := api.Register(ctx, username, password, displayName)
			if err != nil {
				fmt.Println("注册失败:", err)
				continue
			}
			fmt.Printf("注册成功，已登录为 %s\n", res.User.Username)
			return &res.User, false
		case "2":
			username := prompt(reader, "用户名: ")
			password := prompt(reader, "密码: ")
			res, err := api.Login(ctx, username, password)
			if err != nil {
				fmt.Println("登录失败:", err)
				continue
			}
			fmt.Printf("登录成功，当前用户: %s\n", res.User.Username)
			return &res.User, false
		case "3":
			_ = prompt(reader, "请输入用户名: ")
			fmt.Println("当前测试版没有开放自助找回密码。请联系管理员在管理端核验身份后处理。")
		case "0":
			return nil, true
		default:
			fmt.Println("未知选择")
		}
	}
}

// runPlayerMenu handles authenticated room operations. Server-side checks still
// enforce ownership and membership; the console only guides the player.
func runPlayerMenu(ctx context.Context, reader *bufio.Reader, api *API, ifName string, user account.PublicUser) (logout bool, exit bool) {
	for {
		current, hasRoom, _ := currentRoom(ctx, api)
		fmt.Println()
		fmt.Printf("玩家大厅 - %s(id=%d)\n", user.Username, user.ID)
		if hasRoom {
			role := "普通成员"
			if current.OwnerUserID == user.ID {
				role = "房主"
			}
			fmt.Printf("当前房间: %s(id=%d) %s members=%d/%d cidr=%s\n", current.Name, current.ID, role, current.MemberCount, current.MaxMembers, current.NetworkCIDR)
		} else {
			fmt.Println("当前没有加入房间，可以创建或使用房间公钥加入。")
		}
		fmt.Println("1. 创建房间")
		fmt.Println("2. 加入房间")
		fmt.Println("3. 查看当前房间和成员")
		fmt.Println("4. 启动/连接当前房间虚拟网络")
		fmt.Println("5. 删除/退出当前房间")
		fmt.Println("6. 踢出成员(房主)")
		fmt.Println("7. 转让房主(房主)")
		fmt.Println("8. 退出登录")
		fmt.Println("0. 退出程序")

		switch prompt(reader, "选择: ") {
		case "1":
			if hasRoom {
				fmt.Println("你已经在一个房间中，请先退出或删除当前房间。")
				continue
			}
			name := prompt(reader, "房间名: ")
			maxMembers := promptInt(reader, fmt.Sprintf("最大人数(%d-%d，默认%d): ", room.MinMaxMembers, room.MaxMaxMembers, room.DefaultMaxMembers), room.DefaultMaxMembers)
			r, err := api.CreateRoom(ctx, name, maxMembers)
			if err != nil {
				fmt.Println("创建失败:", err)
				continue
			}
			fmt.Printf("房间创建成功: id=%d name=%s members=%d/%d roomPublicKey=%s cidr=%s\n", r.ID, r.Name, r.MemberCount, r.MaxMembers, r.RoomPublicKey, r.NetworkCIDR)
			fmt.Println("把 roomPublicKey 发给朋友，朋友用它加入房间。")
			fmt.Println("需要联机时请选择 4 手动启动虚拟网络。")
		case "2":
			if hasRoom {
				fmt.Println("你已经在一个房间中，请先退出当前房间。")
				continue
			}
			publicKey := prompt(reader, "房间公钥: ")
			r, err := api.JoinRoom(ctx, publicKey)
			if err != nil {
				fmt.Println("加入失败:", err)
				continue
			}
			fmt.Printf("加入成功: id=%d name=%s cidr=%s\n", r.ID, r.Name, r.NetworkCIDR)
			fmt.Println("需要联机时请选择 4 手动启动虚拟网络。")
		case "3":
			if err := printCurrentRoom(ctx, api); err != nil {
				fmt.Println("查询失败:", err)
			}
		case "4":
			if err := connectCurrentRoom(ctx, reader, api, ifName); err != nil {
				fmt.Println("连接失败:", err)
			}
		case "5":
			if err := leaveOrDeleteCurrentRoom(ctx, reader, api, user.ID); err != nil {
				fmt.Println("操作失败:", err)
			}
		case "6":
			if err := kickMember(ctx, reader, api, user.ID); err != nil {
				fmt.Println("踢人失败:", err)
			}
		case "7":
			if err := transferOwner(ctx, reader, api, user.ID); err != nil {
				fmt.Println("转让失败:", err)
			}
		case "8":
			fmt.Println("已退出登录")
			return true, false
		case "0":
			return false, true
		default:
			fmt.Println("未知选择")
		}
	}
}

func currentRoom(ctx context.Context, api *API) (room.Room, bool, error) {
	rooms, err := api.Rooms(ctx)
	if err != nil {
		return room.Room{}, false, err
	}
	if len(rooms) == 0 {
		return room.Room{}, false, nil
	}
	return rooms[0], true, nil
}

func printCurrentRoom(ctx context.Context, api *API) error {
	r, ok, err := currentRoom(ctx, api)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Println("当前没有加入房间")
		return nil
	}
	printRooms([]room.Room{r})
	members, err := api.RoomMembers(ctx, r.ID)
	if err != nil {
		return err
	}
	printMembers(members)
	return nil
}

func connectCurrentRoom(ctx context.Context, reader *bufio.Reader, api *API, ifName string) error {
	selected, ok, err := currentRoom(ctx, api)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("当前没有加入房间")
	}
	return connectSelectedRoom(ctx, reader, api, selected, ifName)
}

func connectSelectedRoom(ctx context.Context, reader *bufio.Reader, api *API, selected room.Room, ifName string) error {
	overlayTarget := selected.NodeOverlayAddr
	serverPublicKey := selected.NodePublicKey
	if overlayTarget == "" || serverPublicKey == "" {
		return fmt.Errorf("房间缺少节点连接信息，请确认控制中心已配置可用房间节点")
	}
	connectCtx, cancel := context.WithCancel(ctx)
	errCh := make(chan error, 1)
	go func() {
		errCh <- ConnectRoom(connectCtx, ConnectOptions{
			OverlayAddr:     overlayTarget,
			ServerPublicKey: serverPublicKey,
			IfName:          ifName,
			Token:           api.Token(),
			Room:            selected,
		})
	}()
	_ = prompt(reader, "已开始连接。按回车断开虚拟网络...")
	cancel()
	if err := <-errCh; err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}
func leaveOrDeleteCurrentRoom(ctx context.Context, reader *bufio.Reader, api *API, userID int64) error {
	selected, ok, err := currentRoom(ctx, api)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("当前没有加入房间")
	}
	action := "退出"
	if selected.OwnerUserID == userID {
		action = "删除"
	}
	confirm := strings.ToLower(prompt(reader, fmt.Sprintf("确认%s房间 %s ? 输入 yes 确认: ", action, selected.Name)))
	if confirm != "yes" {
		fmt.Println("已取消")
		return nil
	}
	if selected.OwnerUserID == userID {
		if err := api.DeleteRoom(ctx, selected.ID); err != nil {
			return err
		}
		fmt.Println("删除成功。该房间公钥已失效，你可以重新创建一个房间。")
		return nil
	}
	if err := api.LeaveRoom(ctx, selected.ID); err != nil {
		return err
	}
	fmt.Println("已退出房间。")
	return nil
}

func kickMember(ctx context.Context, reader *bufio.Reader, api *API, userID int64) error {
	r, ok, err := currentRoom(ctx, api)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("当前没有加入房间")
	}
	if r.OwnerUserID != userID {
		return fmt.Errorf("只有房主可以踢人")
	}
	members, err := api.RoomMembers(ctx, r.ID)
	if err != nil {
		return err
	}
	printMembers(members)
	targetID, _ := strconv.ParseInt(prompt(reader, "输入要踢出的用户ID: "), 10, 64)
	if targetID <= 0 {
		return fmt.Errorf("用户ID无效")
	}
	return api.KickMember(ctx, r.ID, targetID)
}

func transferOwner(ctx context.Context, reader *bufio.Reader, api *API, userID int64) error {
	r, ok, err := currentRoom(ctx, api)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("当前没有加入房间")
	}
	if r.OwnerUserID != userID {
		return fmt.Errorf("只有房主可以转让房主")
	}
	members, err := api.RoomMembers(ctx, r.ID)
	if err != nil {
		return err
	}
	printMembers(members)
	targetID, _ := strconv.ParseInt(prompt(reader, "输入新房主用户ID: "), 10, 64)
	if targetID <= 0 {
		return fmt.Errorf("用户ID无效")
	}
	_, err = api.TransferOwner(ctx, r.ID, targetID)
	return err
}

func prompt(reader *bufio.Reader, label string) string {
	fmt.Print(label)
	line, _ := reader.ReadString('\n')
	return strings.TrimSpace(line)
}

func promptInt(reader *bufio.Reader, label string, fallback int) int {
	text := prompt(reader, label)
	if text == "" {
		return fallback
	}
	v, err := strconv.Atoi(text)
	if err != nil {
		return fallback
	}
	return v
}

func printRooms(rooms []room.Room) {
	if len(rooms) == 0 {
		fmt.Println("暂无房间")
		return
	}
	for _, r := range rooms {
		ownerTag := ""
		if r.OwnerUserID > 0 {
			ownerTag = fmt.Sprintf(" owner=%d", r.OwnerUserID)
		}
		nodeTag := ""
		if r.NodeName != "" {
			nodeTag = fmt.Sprintf(" node=%s", r.NodeName)
			if r.NodeOverlayAddr != "" {
				nodeTag += "@" + r.NodeOverlayAddr
			}
		}
		fmt.Printf("id=%d name=%s%s%s members=%d/%d roomPublicKey=%s cidr=%s status=%s\n", r.ID, r.Name, ownerTag, nodeTag, r.MemberCount, r.MaxMembers, r.RoomPublicKey, r.NetworkCIDR, r.Status)
	}
}

func printMembers(members []room.MemberView) {
	if len(members) == 0 {
		fmt.Println("暂无成员")
		return
	}
	for _, m := range members {
		role := "普通成员"
		if m.Role == room.MemberRoleOwner {
			role = "房主"
		}
		fmt.Printf("user=%d username=%s display=%s role=%s\n", m.UserID, m.Username, m.DisplayName, role)
	}
}
