package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	HttpAddr               string
	DBUrl                  string
	RabbitMQURL            string
	RabbitMQDialTimeout    time.Duration
	RabbitMQPublishTimeout time.Duration
	RabbitMQMaxAttempts    int
}

func LoadConfig() (Config, error) {
	httpAddr := os.Getenv("HTTP_ADDR")
	if httpAddr == "" {
		return Config{}, errors.New("missing HTTP_ADDR env variable")
	}
	dbUrl := os.Getenv("DATABASE_URL")
	if dbUrl == "" {
		return Config{}, errors.New("missing DATABASE_URL env variable")
	}
	rabbitMQURL := os.Getenv("RABBITMQ_URL")
	if rabbitMQURL == "" {
		return Config{}, errors.New("missing RABBITMQ_URL env variable")
	}
	rabbitMQDialTimeout, err := loadPositiveDuration("RABBITMQ_DIAL_TIMEOUT")
	if err != nil {
		return Config{}, err
	}
	rabbitMQPublishTimeout, err := loadPositiveDuration("RABBITMQ_PUBLISH_TIMEOUT")
	if err != nil {
		return Config{}, err
	}
	rabbitMQMaxAttempts, err := loadPositiveInt("RABBITMQ_MAX_ATTEMPTS")
	if err != nil {
		return Config{}, err
	}

	config := Config{
		HttpAddr:               httpAddr,
		DBUrl:                  dbUrl,
		RabbitMQURL:            rabbitMQURL,
		RabbitMQDialTimeout:    rabbitMQDialTimeout,
		RabbitMQPublishTimeout: rabbitMQPublishTimeout,
		RabbitMQMaxAttempts:    rabbitMQMaxAttempts,
	}

	return config, nil
}

func loadPositiveDuration(name string) (time.Duration, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return 0, fmt.Errorf("missing %s env variable", name)
	}

	duration, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s env variable: %w", name, err)
	}
	if duration <= 0 {
		return 0, fmt.Errorf("invalid %s env variable: must be greater than zero", name)
	}

	return duration, nil
}

func loadPositiveInt(name string) (int, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return 0, fmt.Errorf("missing %s env variable", name)
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s env variable: %w", name, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf(
			"invalid %s env variable: must be greater than zero",
			name,
		)
	}

	return value, nil
}
