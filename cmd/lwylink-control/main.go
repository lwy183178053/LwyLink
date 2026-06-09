package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/LwyV-Labs/LwyLink/internal/account"
	"github.com/LwyV-Labs/LwyLink/internal/appsettings"
	"github.com/LwyV-Labs/LwyLink/internal/clientrelease"
	"github.com/LwyV-Labs/LwyLink/internal/config"
	"github.com/LwyV-Labs/LwyLink/internal/db"
	"github.com/LwyV-Labs/LwyLink/internal/mail"
	"github.com/LwyV-Labs/LwyLink/internal/node"
	"github.com/LwyV-Labs/LwyLink/internal/room"
	"github.com/LwyV-Labs/LwyLink/internal/state"
	"github.com/LwyV-Labs/LwyLink/internal/web"
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

	runtimeState := state.NewRedisStore(cfg.RedisAddr, cfg.RedisPassword)
	defer runtimeState.Close()
	if err := runtimeState.Ping(ctx); err != nil {
		log.Printf("redis unavailable, login sessions and room keys will fail closed: %v", err)
	} else {
		log.Println("redis connected")
	}

	accountSvc := account.NewService(account.NewRepository(sqlDB), cfg.TokenSecret, runtimeState)
	mailCfg, err := config.LoadMailConfig(cfg.MailConfigPath)
	if err != nil {
		log.Fatalf("load mail config: %v", err)
	}
	var mailSvc *mail.Service
	if mailCfg.Enabled {
		sender, err := mail.NewSMTPSenderFromConfig(mailCfg)
		if err != nil {
			log.Fatalf("init mail sender: %v", err)
		}
		mailSvc = mail.NewService(mail.NewRepository(sqlDB), sender, accountSvc, true)
		log.Printf("mail verification enabled from=%s host=%s:%d", mailCfg.FromAddress, mailCfg.SMTPHost, mailCfg.SMTPPort)
	} else {
		mailSvc = mail.NewService(mail.NewRepository(sqlDB), nil, accountSvc, false)
		log.Println("mail verification disabled")
	}
	settingsSvc := appsettings.NewService(appsettings.NewRepository(sqlDB))
	nodeSvc := node.NewService(node.NewRepository(sqlDB))
	roomSvc := room.NewService(room.NewRepository(sqlDB), runtimeState, nodeSvc, cfg.RoomNetworkBase, cfg.RoomSubnetPrefix)
	releaseSvc := clientrelease.NewService(clientrelease.NewRepository(sqlDB), cfg.ClientReleaseDir)
	if err := roomSvc.SyncOpenRoomKeys(ctx); err != nil {
		log.Fatalf("sync room public keys to redis: %v", err)
	}
	nodeSvc.StartHealthMonitor(ctx, time.Duration(cfg.NodeHealthCheckIntervalSeconds)*time.Second)
	roomSvc.StartEmptyRoomCleaner(ctx, time.Duration(cfg.RoomCleanupIntervalSeconds)*time.Second, time.Duration(cfg.RoomEmptyGraceSeconds)*time.Second)

	httpServer := web.NewServer(cfg.HTTPAddr, cfg.AdminToken, accountSvc, settingsSvc, roomSvc, nodeSvc, releaseSvc, mailSvc, cfg.VoiceICEServers, cfg.VoiceTURNCredential)
	if err := httpServer.ListenAndServe(ctx); err != nil {
		log.Fatalf("control plane failed: %v", err)
	}
}
