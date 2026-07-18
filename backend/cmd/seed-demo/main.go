package main

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	_ "github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

//go:embed seed-demo.sql
var seedSQL embed.FS

const defaultDemoPassword = "Demo123!"

func main() {
	password := strings.TrimSpace(os.Getenv("DEMO_SEED_PASSWORD"))
	if password == "" {
		password = defaultDemoPassword
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		fatal("hash demo password", err)
	}

	dsn := databaseDSN()
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		fatal("open database", err)
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		fatal("connect to database", err)
	}

	script, err := seedSQL.ReadFile("seed-demo.sql")
	if err != nil {
		fatal("read embedded seed", err)
	}
	sqlText := strings.ReplaceAll(string(script), "__DEMO_PASSWORD_HASH__", string(passwordHash))

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		fatal("begin seed transaction", err)
	}

	if _, err := tx.ExecContext(ctx, sqlText); err != nil {
		_ = tx.Rollback()
		fatal("execute demo seed", err)
	}
	if err := tx.Commit(); err != nil {
		fatal("commit demo seed", err)
	}

	fmt.Println("Demo data seeded successfully.")
	fmt.Printf("Admin login: demo.admin@sub2api.local / %s\n", password)
	fmt.Println("User login:  demo.analyst@sub2api.local / same password")
	fmt.Println("All demo upstream credentials are placeholders and accounts are unschedulable.")
}

func databaseDSN() string {
	if dsn := strings.TrimSpace(os.Getenv("DATABASE_URL")); dsn != "" {
		return dsn
	}

	if cfg, err := config.LoadForBootstrap(); err == nil && cfg != nil {
		return cfg.Database.DSNWithTimezone("Asia/Shanghai")
	}

	host := envOrDefault("DATABASE_HOST", "localhost")
	port := envOrDefault("DATABASE_PORT", "5432")
	user := envOrDefault("DATABASE_USER", "postgres")
	password := os.Getenv("DATABASE_PASSWORD")
	if password == "" {
		password = "postgres"
	}
	database := envOrDefault("DATABASE_DBNAME", "sub2api")
	sslMode := envOrDefault("DATABASE_SSLMODE", "prefer")
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s TimeZone=Asia/Shanghai", host, port, user, password, database, sslMode)
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func fatal(action string, err error) {
	fmt.Fprintf(os.Stderr, "seed-demo: %s: %v\n", action, err)
	os.Exit(1)
}
