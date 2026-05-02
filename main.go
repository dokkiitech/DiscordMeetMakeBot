package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/dokkiitech/discordmeetmakebot/internal/bot"
	"github.com/dokkiitech/discordmeetmakebot/internal/meet"
)

func main() {
	discordToken := mustEnv("DISCORD_BOT_TOKEN")
	guildID := os.Getenv("DISCORD_GUILD_ID")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	meetClient, err := meet.NewClient(ctx, meet.Config{
		ClientID:     mustEnv("GOOGLE_CLIENT_ID"),
		ClientSecret: mustEnv("GOOGLE_CLIENT_SECRET"),
		RefreshToken: mustEnv("GOOGLE_REFRESH_TOKEN"),
		CalendarID:   os.Getenv("GOOGLE_CALENDAR_ID"),
	})
	if err != nil {
		log.Fatalf("init google meet client: %v", err)
	}

	b, err := bot.New(discordToken, guildID, meetClient)
	if err != nil {
		log.Fatalf("init discord bot: %v", err)
	}
	if err := b.Start(ctx); err != nil {
		log.Fatalf("start discord bot: %v", err)
	}
	log.Println("bot is running. press Ctrl+C to stop.")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("shutting down...")
	if err := b.Stop(); err != nil {
		log.Printf("stop bot: %v", err)
	}
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("environment variable %s is required", key)
	}
	return v
}
