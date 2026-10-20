package config

import (
	"log"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	AppEnv string
	Port   string

	// PostgreSQL
	DatabaseURL          string
	DBMaxConns          int32
	DBMinConns          int32
	DBMaxConnLifetime    time.Duration
	DBMaxConnIdleTime    time.Duration
	DBHealthCheckPeriod  time.Duration

	// Redis
	RedisURL         string
	RedisHost        string
	RedisPort        string
	RedisPassword    string
	RedisPoolSize    int
	RedisMinIdleConns int
	RedisDialTimeout time.Duration
	RedisReadTimeout time.Duration
	RedisWriteTimeout time.Duration

	// RabbitMQ
	RabbitMQURL string
}

func Load() *Config {
	// Try loading .env from current directory, parent directory, or root
	_ = godotenv.Load(".env")
	_ = godotenv.Load("../.env")
	_ = godotenv.Load("../../.env")

	cfg := &Config{
		AppEnv: getEnv("APP_ENV", "development"),
		Port:   getEnv("PORT", "8080"),

		// PostgreSQL
		DatabaseURL:         getEnv("DATABASE_URL", "postgres://flashpass:flashpass_secret@localhost:5432/flashpass?sslmode=disable"),
		DBMaxConns:          int32(getEnvAsInt("DB_MAX_CONNS", 50)),
		DBMinConns:          int32(getEnvAsInt("DB_MIN_CONNS", 10)),
		DBMaxConnLifetime:   time.Duration(getEnvAsInt("DB_MAX_CONN_LIFETIME_MINUTES", 60)) * time.Minute,
		DBMaxConnIdleTime:   time.Duration(getEnvAsInt("DB_MAX_CONN_IDLE_MINUTES", 30)) * time.Minute,
		DBHealthCheckPeriod: 1 * time.Minute,

		// Redis
		RedisURL:          getEnv("REDIS_URL", "redis://localhost:6379/0"),
		RedisHost:         getEnv("REDIS_HOST", "localhost"),
		RedisPort:         getEnv("REDIS_PORT", "6379"),
		RedisPassword:     getEnv("REDIS_PASSWORD", ""),
		RedisPoolSize:     getEnvAsInt("REDIS_POOL_SIZE", 100),
		RedisMinIdleConns: getEnvAsInt("REDIS_MIN_IDLE_CONNS", 20),
		RedisDialTimeout: 5 * time.Second,
		RedisReadTimeout: 3 * time.Second,
		RedisWriteTimeout: 3 * time.Second,

		// RabbitMQ
		RabbitMQURL: getEnv("RABBITMQ_URL", "amqp://flashpass:flashpass_secret@localhost:5672/"),
	}

	log.Printf("[Config] Loaded configuration: APP_ENV=%s, PORT=%s, DB_POOL=%d-%d, REDIS_POOL=%d",
		cfg.AppEnv, cfg.Port, cfg.DBMinConns, cfg.DBMaxConns, cfg.RedisPoolSize)

	return cfg
}

func getEnv(key, defaultVal string) string {
	if val, exists := os.LookupEnv(key); exists && val != "" {
		return val
	}
	return defaultVal
}

func getEnvAsInt(key string, defaultVal int) int {
	valStr := getEnv(key, "")
	if val, err := strconv.Atoi(valStr); err == nil {
		return val
	}
	return defaultVal
}
