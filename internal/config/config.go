package config

import (
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// Config centraliza todas as variáveis de ambiente da aplicação.
type Config struct {
	APIPort string

	RedisAddr          string
	RedisPassword      string
	RedisDB            int
	RedisStreamName    string
	RedisConsumerGroup string
	RedisConsumerName  string

	TelegramBotToken string

	GmailSMTPHost    string
	GmailSMTPPort    string
	GmailUsername    string
	GmailAppPassword string
	GmailFromName    string

	DefaultDiscordTarget  string
	DefaultTelegramTarget string
	DefaultEmailTarget    string

	HTTPClientTimeoutSeconds int
	WorkerConcurrency        int
}

// Load lê o arquivo .env (se existir) e as variáveis de ambiente do sistema,
// retornando uma struct Config totalmente preenchida com defaults seguros.
func Load() *Config {
	if err := godotenv.Load(); err != nil {
		log.Println("[config] arquivo .env não encontrado, usando variáveis de ambiente do sistema")
	}

	return &Config{
		APIPort: getEnv("API_PORT", "8080"),

		RedisAddr:          getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword:      getEnv("REDIS_PASSWORD", ""),
		RedisDB:            getEnvAsInt("REDIS_DB", 0),
		RedisStreamName:    getEnv("REDIS_STREAM_NAME", "notifications:stream"),
		RedisConsumerGroup: getEnv("REDIS_CONSUMER_GROUP", "notification-workers"),
		RedisConsumerName:  getEnv("REDIS_CONSUMER_NAME", "worker-1"),

		TelegramBotToken: getEnv("TELEGRAM_BOT_TOKEN", ""),

		GmailSMTPHost:    getEnv("GMAIL_SMTP_HOST", "smtp.gmail.com"),
		GmailSMTPPort:    getEnv("GMAIL_SMTP_PORT", "587"),
		GmailUsername:    getEnv("GMAIL_USERNAME", ""),
		GmailAppPassword: getEnv("GMAIL_APP_PASSWORD", ""),
		GmailFromName:    getEnv("GMAIL_FROM_NAME", ""),

		DefaultDiscordTarget:  getEnv("DEFAULT_DISCORD_TARGET", ""),
		DefaultTelegramTarget: getEnv("DEFAULT_TELEGRAM_TARGET", ""),
		DefaultEmailTarget:    getEnv("DEFAULT_EMAIL_TARGET", ""),

		HTTPClientTimeoutSeconds: getEnvAsInt("HTTP_CLIENT_TIMEOUT_SECONDS", 10),
		WorkerConcurrency:        getEnvAsInt("WORKER_CONCURRENCY", 10),
	}
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}
	return fallback
}

func getEnvAsInt(key string, fallback int) int {
	valueStr := getEnv(key, "")
	if valueStr == "" {
		return fallback
	}
	value, err := strconv.Atoi(valueStr)
	if err != nil {
		log.Printf("[config] valor inválido para %s (%s), usando default %d", key, valueStr, fallback)
		return fallback
	}
	return value
}
