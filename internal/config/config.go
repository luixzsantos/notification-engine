package config

import (
	"log"
	"os"
	"strconv"
	"strings"

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

	DBHost         string
	DBPort         string
	DBUser         string
	DBPassword     string
	DBName         string
	DBSSLMode      string
	DBMaxOpenConns int

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

	// APIKey, quando definido, exige o header "Authorization: Bearer
	// <chave>" ou "X-API-Key: <chave>" em todos os endpoints de negócio da
	// API (/api/v1/*). Vazio (default) desativa a autenticação — adequado
	// só para uso local/dev.
	APIKey string

	// CORSAllowedOrigins restringe quais origens podem chamar a API a
	// partir do navegador. ["*"] (default) libera qualquer origem.
	CORSAllowedOrigins []string

	// TelegramAllowedChatIDs restringe quem pode executar comandos
	// administrativos (/retry, /status) no bot do Telegram. Vazio (default)
	// permite qualquer chat — recomendado configurar em produção.
	TelegramAllowedChatIDs []int64

	// AllowPrivateNetworkTargets desliga a proteção contra SSRF (bloqueio de
	// IPs privados/loopback/link-local como destino de webhook/discord).
	// Existe só para permitir testar contra serviços internos em
	// desenvolvimento — NUNCA deveria ficar ligado em produção.
	AllowPrivateNetworkTargets bool
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

		DBHost:         getEnv("DB_HOST", "localhost"),
		DBPort:         getEnv("DB_PORT", "5432"),
		DBUser:         getEnv("DB_USER", "engine"),
		DBPassword:     getEnv("DB_PASSWORD", "engine"),
		DBName:         getEnv("DB_NAME", "notification_engine"),
		DBSSLMode:      getEnv("DB_SSL_MODE", "disable"),
		DBMaxOpenConns: getEnvAsInt("DB_MAX_OPEN_CONNS", 25),

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

		APIKey:                     getEnv("API_KEY", ""),
		CORSAllowedOrigins:         getEnvAsStringList("CORS_ALLOWED_ORIGINS", []string{"*"}),
		TelegramAllowedChatIDs:     getEnvAsInt64List("TELEGRAM_ALLOWED_CHAT_IDS", nil),
		AllowPrivateNetworkTargets: getEnvAsBool("ALLOW_PRIVATE_NETWORK_TARGETS", false),
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

// getEnvAsStringList lê uma lista separada por vírgulas (ex: "a,b,c"),
// removendo espaços em branco de cada item.
func getEnvAsStringList(key string, fallback []string) []string {
	valueStr := getEnv(key, "")
	if valueStr == "" {
		return fallback
	}

	parts := strings.Split(valueStr, ",")
	list := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			list = append(list, trimmed)
		}
	}
	return list
}

// getEnvAsInt64List lê uma lista de inteiros separados por vírgulas (ex:
// "111,222,333"), ignorando itens que não sejam números válidos.
func getEnvAsInt64List(key string, fallback []int64) []int64 {
	valueStr := getEnv(key, "")
	if valueStr == "" {
		return fallback
	}

	parts := strings.Split(valueStr, ",")
	list := make([]int64, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		v, err := strconv.ParseInt(trimmed, 10, 64)
		if err != nil {
			log.Printf("[config] valor inválido em %s (%q ignorado): %v", key, trimmed, err)
			continue
		}
		list = append(list, v)
	}
	return list
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
