package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/LwyV-Labs/LwyV-GameNet/internal/client"
)

func main() {
	apiURL := flag.String("api", "http://127.0.0.1:8080", "GameNet HTTP API base URL")
	ifName := flag.String("if", "LwyV-GameNet", "Windows Wintun adapter name")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := client.RunConsole(ctx, *apiURL, *ifName); err != nil {
		log.Fatal(err)
	}
}
