//
// main.go
// Antidubl — Telegram anti-duplicate bot
//
// Long polling, one chat (config chat_id, or auto-locked to the
// first group seen). Graceful shutdown on SIGTERM/SIGINT.
//

package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func envOr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func main() {
	configPath := envOr("ANTIDUBL_CONFIG", "config.json")
	dbPath := envOr("ANTIDUBL_DB", filepath.Join(filepath.Dir(configPath), "antidubl.db"))

	cfg, err := LoadConfig(configPath)
	if err != nil {
		log.Fatalf("[boot] конфигурация: %v", err)
	}
	if cfg.BotToken == "" {
		log.Fatal("[boot] bot_token не задан в конфигурации")
	}

	bot, err := tgbotapi.NewBotAPI(cfg.BotToken)
	if err != nil {
		log.Fatalf("[boot] инициализация бота: %v", err)
	}
	log.Printf("[boot] авторизован как @%s", bot.Self.UserName)

	st, err := OpenStore(dbPath)
	if err != nil {
		log.Fatalf("[boot] база данных: %v", err)
	}
	defer st.Close()

	det := NewDetector(cfg, st, bot, configPath)

	// Initial retention sweep, then hourly.
	if err := st.CleanupAll(cfg.RetentionDays); err != nil {
		log.Printf("[boot] очистка: %v", err)
	}

	// Expose the command menu in Telegram.
	_, _ = bot.MakeRequest("setMyCommands", tgbotapi.Params{
		"commands": `[{"command":"settings","description":"Настройки бота"},{"command":"status","description":"Состояние бота"},{"command":"help","description":"Справка"}]`,
	})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go det.autoDeleteLoop(ctx)
	go retentionLoop(ctx, st, cfg)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 50
	updates := bot.GetUpdatesChan(u)

	log.Printf("[boot] Antidubl запущен (окно по умолчанию %d суток, %d чатов в базе)", cfg.RetentionDays, chatCount(st))

	for {
		select {
		case <-ctx.Done():
			log.Printf("[boot] остановка...")
			return
		case update, ok := <-updates:
			if !ok {
				log.Printf("[boot] канал обновлений закрыт")
				return
			}
			if update.Message != nil {
				if update.Message.IsCommand() {
					det.HandleCommand(update.Message)
				} else {
					det.Process(update.Message)
				}
			}
			if update.EditedMessage != nil {
				det.ProcessEdited(update.EditedMessage)
			}
			if update.CallbackQuery != nil {
				det.HandleCallback(update.CallbackQuery)
			}
		}
	}
}

// retentionLoop sweeps old rows hourly (all chats).
func retentionLoop(ctx context.Context, st *Store, cfg *Config) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := st.CleanupAll(cfg.RetentionDays); err != nil {
				log.Printf("[retention] очистка: %v", err)
			}
		}
	}
}

// chatCount returns the number of known chats (informational).
func chatCount(st *Store) int {
	chats, err := st.KnownChats()
	if err != nil {
		return 0
	}
	return len(chats)
}
