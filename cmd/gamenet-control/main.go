package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/LwyV-Labs/LwyV-GameNet/internal/account"
	"github.com/LwyV-Labs/LwyV-GameNet/internal/cache"
	"github.com/LwyV-Labs/LwyV-GameNet/internal/config"
	"github.com/LwyV-Labs/LwyV-GameNet/internal/db"
	"github.com/LwyV-Labs/LwyV-GameNet/internal/node"
	"github.com/LwyV-Labs/LwyV-GameNet/internal/room"
	"github.com/LwyV-Labs/LwyV-GameNet/internal/web"
)

func main() {
	configPath := flag.String("config", "configs/control.example.json", "control plane config json path")
	flag.Parse()

	cfg, err := config.LoadControlConfig(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if err := config.ValidateControlConfig(cfg); err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sqlDB, err := db.Open(ctx, cfg.MySQLDSN)
	if err != nil {
		log.Fatalf("connect mysql: %v", err)
	}
	defer sqlDB.Close()
	if err := db.Migrate(ctx, sqlDB); err != nil {
		log.Fatalf("migrate mysql: %v", err)
	}
	log.Println("mysql migrated")

	online := cache.NewOnlineStore(cfg.RedisAddr, cfg.RedisPassword)
	defer online.Close()
	if err := online.Ping(ctx); err != nil {
		log.Printf("redis unavailable, login sessions and room keys will fail closed: %v", err)
	} else {
		log.Println("redis connected")
	}

	accountSvc := account.NewService(account.NewRepository(sqlDB), cfg.TokenSecret, online)
	nodeSvc := node.NewService(node.NewRepository(sqlDB))
	roomSvc := room.NewService(room.NewRepository(sqlDB), online, nodeSvc, cfg.RoomNetworkBase, cfg.RoomSubnetPrefix)
	if err := roomSvc.SyncOpenRoomKeys(ctx); err != nil {
		log.Fatalf("sync room public keys to redis: %v", err)
	}
	nodeSvc.StartHealthMonitor(ctx, time.Duration(cfg.NodeHealthCheckIntervalSeconds)*time.Second)
	roomSvc.StartEmptyRoomCleaner(ctx, time.Duration(cfg.RoomCleanupIntervalSeconds)*time.Second, time.Duration(cfg.RoomEmptyGraceSeconds)*time.Second)

	httpServer := web.NewServer(cfg.HTTPAddr, cfg.AdminToken, accountSvc, roomSvc, nodeSvc)
	if err := httpServer.ListenAndServe(ctx); err != nil {
		log.Fatalf("control plane failed: %v", err)
	}
}
