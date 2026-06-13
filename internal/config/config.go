package config

import (
	"errors"
	"os"
)

type Config struct {
	HttpAddr string
	DBUrl    string
}

func LoadConfig() (Config, error) {
	httpAddr := os.Getenv("HTTP_ADDR")
	if httpAddr == "" {
		return Config{}, errors.New("missing HTTP_ADDR env variable")
	}
	dbUrl := os.Getenv("DATABASE_URL")
	if dbUrl == "" {
		return Config{}, errors.New("missing DATABSE_URL env variable")
	}

	config := Config{
		HttpAddr: httpAddr,
		DBUrl:    dbUrl,
	}

	return config, nil
}
