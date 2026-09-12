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

	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string
	DBSSLMode  string

	TelegramBotToken   string
	TelegramBotEnabled bool

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

	RetryMaxAttempts         int
	RetryMaxBackoffSeconds   int
	RetryPollIntervalSeconds int
	RetryBatchSize           int

	RateLimitEnabled     bool
	RateLimitDiscordRPS  float64
	RateLimitTelegramRPS float64
	RateLimitWebhookRPS  float64
	RateLimitEmailRPS    float64

	MetricsEnabled bool
	MetricsPort    string
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

		DBHost:     getEnv("DB_HOST", "localhost"),
		DBPort:     getEnv("DB_PORT", "5432"),
		DBUser:     getEnv("DB_USER", "engine"),
		DBPassword: getEnv("DB_PASSWORD", "engine"),
		DBName:     getEnv("DB_NAME", "notification_engine"),
		DBSSLMode:  getEnv("DB_SSL_MODE", "disable"),

		TelegramBotToken:   getEnv("TELEGRAM_BOT_TOKEN", ""),
		TelegramBotEnabled: getEnvAsBool("TELEGRAM_BOT_ENABLED", false),

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

		RetryMaxAttempts:         getEnvAsInt("RETRY_MAX_ATTEMPTS", 5),
		RetryMaxBackoffSeconds:   getEnvAsInt("RETRY_MAX_BACKOFF_SECONDS", 3600),
		RetryPollIntervalSeconds: getEnvAsInt("RETRY_POLL_INTERVAL_SECONDS", 10),
		RetryBatchSize:           getEnvAsInt("RETRY_BATCH_SIZE", 50),

		RateLimitEnabled:     getEnvAsBool("RATE_LIMIT_ENABLED", true),
		RateLimitDiscordRPS:  getEnvAsFloat("RATE_LIMIT_DISCORD_RPS", 10),
		RateLimitTelegramRPS: getEnvAsFloat("RATE_LIMIT_TELEGRAM_RPS", 20),
		RateLimitWebhookRPS:  getEnvAsFloat("RATE_LIMIT_WEBHOOK_RPS", 50),
		RateLimitEmailRPS:    getEnvAsFloat("RATE_LIMIT_EMAIL_RPS", 5),

		MetricsEnabled: getEnvAsBool("METRICS_ENABLED", true),
		MetricsPort:    getEnv("METRICS_PORT", "9091"),
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

func getEnvAsFloat(key string, fallback float64) float64 {
	valueStr := getEnv(key, "")
	if valueStr == "" {
		return fallback
	}
	value, err := strconv.ParseFloat(valueStr, 64)
	if err != nil {
		log.Printf("[config] valor inválido para %s (%s), usando default %.2f", key, valueStr, fallback)
		return fallback
	}
	return value
}

func getEnvAsBool(key string, fallback bool) bool {
	valueStr := getEnv(key, "")
	if valueStr == "" {
		return fallback
	}
	value, err := strconv.ParseBool(valueStr)
	if err != nil {
		log.Printf("[config] valor inválido para %s (%s), usando default %v", key, valueStr, fallback)
		return fallback
	}
	return value
}
