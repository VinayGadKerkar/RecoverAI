package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds all application configuration loaded from environment variables.
type Config struct {
	// Server
	Port string
	Env  string

	// Database
	DatabaseURL string

	// Redis
	RedisURL string

	// Kafka
	KafkaBrokers string

	// Auth
	JWTSecret    string
	DemoPassword string // password accepted by POST /auth/login

	// Razorpay
	RazorpayKeyID     string
	RazorpayKeySecret string
	RazorpayWebhookSecret string

	// AI Service
	AIServiceURL string
	GroqAPIKey   string
	GeminiAPIKey string
	LLMProvider  string // "groq" | "gemini"

	// Policy limits
	MaxRetryAttempts    int
	MaxDailyRetries     int
	RetryWindowMinutes  int
	HighValueThreshold  int64 // in paise

	// Outage detection
	OutageDetectionThreshold int // failures per 5-minute window to trigger outage flag

	// Demo Mode - reduces delays to 1 minute for presentations
	DemoMode bool
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:               getEnv("PORT", "8080"),
		Env:                getEnv("ENV", "development"),
		DatabaseURL:        mustGetEnv("DATABASE_URL"),
		RedisURL:           getEnv("REDIS_URL", "redis://localhost:6379"),
		KafkaBrokers:       getEnv("KAFKA_BROKERS", "localhost:9092"),
		JWTSecret:          mustGetEnv("JWT_SECRET"),
		DemoPassword:       getEnv("DEMO_PASSWORD", "demo"),
		RazorpayKeyID:      getEnv("RAZORPAY_KEY_ID", ""),
		RazorpayKeySecret:  getEnv("RAZORPAY_KEY_SECRET", ""),
		RazorpayWebhookSecret: getEnv("RAZORPAY_WEBHOOK_SECRET", ""),
		AIServiceURL:       getEnv("AI_SERVICE_URL", "http://localhost:8000"),
		GroqAPIKey:         getEnv("GROQ_API_KEY", ""),
		GeminiAPIKey:       getEnv("GEMINI_API_KEY", ""),
		LLMProvider:        getEnv("LLM_PROVIDER", "groq"),
		MaxRetryAttempts:   getEnvInt("MAX_RETRY_ATTEMPTS", 3),
		MaxDailyRetries:    getEnvInt("MAX_DAILY_RETRIES", 5),
		RetryWindowMinutes: getEnvInt("RETRY_WINDOW_MINUTES", 30),
		HighValueThreshold: int64(getEnvInt("HIGH_VALUE_THRESHOLD_PAISE", 5000000)), // ₹50,000
		OutageDetectionThreshold: getEnvInt("OUTAGE_DETECTION_THRESHOLD", 10),
		DemoMode:           getEnvBool("DEMO_MODE", false),
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func mustGetEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic(fmt.Sprintf("required environment variable %q is not set", key))
	}
	return v
}

func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func getEnvBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}
