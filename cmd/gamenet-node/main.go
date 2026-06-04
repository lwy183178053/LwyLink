package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/LwyV-Labs/LwyV-GameNet/internal/account"
	"github.com/LwyV-Labs/LwyV-GameNet/internal/cache"
	"github.com/LwyV-Labs/LwyV-GameNet/internal/config"
	"github.com/LwyV-Labs/LwyV-GameNet/internal/db"
	"github.com/LwyV-Labs/LwyV-GameNet/internal/node"
	"github.com/LwyV-Labs/LwyV-GameNet/internal/overlay"
	"github.com/LwyV-Labs/LwyV-GameNet/internal/room"
	"github.com/LwyV-Labs/LwyV-GameNet/internal/vdhcp"
)

func main() {
	configPath := flag.String("config", "configs/node.example.json", "room node config json path")
	printPublicKey := flag.Bool("print-public-key", false, "print derived overlay public key and exit")
	flag.Parse()

	cfg, err := config.LoadNodeConfig(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if *printPublicKey {
		log.Print(cfg.OverlayPublicKey)
		return
	}
	if err := config.ValidateNodeConfig(cfg); err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sqlDB, err := db.Open(ctx, cfg.MySQLDSN)
	if err != nil {
		log.Fatalf("connect mysql: %v", err)
	}
	defer sqlDB.Close()

	online := cache.NewOnlineStore(cfg.RedisAddr, cfg.RedisPassword)
	defer online.Close()
	if err := online.Ping(ctx); err != nil {
		log.Printf("redis unavailable, overlay auth sessions and presence will fail closed: %v", err)
	}

	nodeSvc := node.NewService(node.NewRepository(sqlDB))
	n, err := nodeSvc.FindByPublicKey(ctx, cfg.OverlayPublicKey)
	if err != nil {
		log.Fatalf("resolve node by overlay public key: %v", err)
	}
	nodeID := n.ID
	if err := nodeSvc.SetStatus(ctx, nodeID, node.StatusOnline, ""); err != nil {
		log.Printf("mark node online failed: %v", err)
	}

	accountSvc := account.NewService(account.NewRepository(sqlDB), cfg.TokenSecret, online)
	roomSvc := room.NewService(room.NewRepository(sqlDB), online, nil, cfg.RoomNetworkBase, cfg.RoomSubnetPrefix)
	leaseMgr := vdhcp.NewManager()
	log.Printf("room node id=%d overlay listening=%s publicKey=%s", nodeID, cfg.OverlayAddr, cfg.OverlayPublicKey)
	overlayServer := overlay.NewServer(cfg.OverlayAddr, nodeID, cfg.OverlayPrivateKey, accountSvc, roomSvc, leaseMgr, online, cfg.DefaultDNS, cfg.DefaultMTU)
	if err := overlayServer.ListenAndServe(ctx); err != nil {
		log.Fatalf("room node failed: %v", err)
	}
}
