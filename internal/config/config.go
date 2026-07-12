package config

import (
	"errors"
	"fmt"
	"os"
	"time"
)

type Config struct {
	HttpAddr               string
	DBUrl                  string
	RabbitMQURL            string
	RabbitMQDialTimeout    time.Duration
	RabbitMQPublishTimeout time.Duration
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

	config := Config{
		HttpAddr:               httpAddr,
		DBUrl:                  dbUrl,
		RabbitMQURL:            rabbitMQURL,
		RabbitMQDialTimeout:    rabbitMQDialTimeout,
		RabbitMQPublishTimeout: rabbitMQPublishTimeout,
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
