package config

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// Config holds all application configuration values loaded from the environment.
type Config struct {
	MongoURI        string
	MongoDB         string
	Port            string
	APILogin        string
	APIPassword     string
	JWTSecret       string
	JWTExpiresHours int
}

// Load reads the .env file (if present) and returns a populated Config.
// In production containers the variables should already be present in the
// environment, so a missing .env file is not treated as a fatal error.
// An error is returned if any required variable is absent or empty.
func Load() (*Config, error) {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, reading config from environment variables")
	}

	cfg := &Config{
		MongoURI:        getEnv("MONGO_URI", "mongodb://localhost:27017"),
		MongoDB:         getEnv("MONGO_DB", "finager"),
		Port:            getEnv("PORT", "8080"),
		APILogin:        getEnv("API_LOGIN", "admin"),
		APIPassword:     getEnv("API_PASSWORD", ""),
		JWTSecret:       getEnv("JWT_SECRET", ""),
		JWTExpiresHours: getEnvInt("JWT_EXPIRATION_HOURS", 24),
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// validate checks that all required fields are present.
func (c *Config) validate() error {
	var missing []string

	if c.JWTSecret == "" {
		missing = append(missing, "JWT_SECRET")
	}
	if c.APIPassword == "" {
		missing = append(missing, "API_PASSWORD")
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required environment variables: %s", errors.Join(toErrors(missing)...))
	}

	return nil
}

func toErrors(ss []string) []error {
	errs := make([]error, len(ss))
	for i, s := range ss {
		errs[i] = errors.New(s)
	}
	return errs
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if value, ok := os.LookupEnv(key); ok {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return fallback
}
