package main

import (
	"fmt"
	"log"
	"log/slog"

	"github.com/ArnavBuild04/payguard/core/internal/config"
	"github.com/ArnavBuild04/payguard/core/internal/wallet/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {

	cfg, err := config.Load("config.json")
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	slog.Info("starting service", "app", cfg.AppName, "version", cfg.Version)

	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%d sslmode=disable TimeZone=UTC", cfg.Database.Host, cfg.Database.User, cfg.Database.Password, cfg.Database.Name, cfg.Database.Port)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}

	// Auto-migrate the Account and LedgerEntry models
	err = db.AutoMigrate(&models.Account{}, &models.LedgerEntry{})
	if err != nil {
		log.Fatalf("failed to auto-migrate models: %v", err)
	}

}
