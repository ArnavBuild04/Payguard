package config

import (
	"encoding/json"
	"os"
)

type Config struct {
	AppName  string `json:"app_name"`
	Version  string `json:"version"`
	HTTPPort int    `json:"http_port"`
	Database struct {
		Host     string `json:"host"`
		User     string `json:"user"`
		Password string `json:"password"`
		Name     string `json:"name"`
		Port     int    `json:"port"`
	} `json:"database"`
	Provider struct {
		BaseURL   string `json:"base_url"`
		TimeoutMS int    `json:"timeout_ms"`
	} `json:"provider"`
	Coordinator struct {
		AssetServiceURL  string `json:"asset_service_url"`
		TicketServiceURL string `json:"ticket_service_url"`
	} `json:"coordinator"`
	Kafka struct {
		Brokers []string `json:"brokers"`
	} `json:"kafka"`
	Redis struct {
		Addr string `json:"addr"`
	} `json:"redis"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	err = json.Unmarshal(data, &cfg)
	if err != nil {
		return nil, err
	}

	return &cfg, nil
}
