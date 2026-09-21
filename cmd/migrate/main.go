package main

import (
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("migration failed: %v", err)
	}
}

func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: go run ./cmd/migrate [up|down|version]")
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}

	m, err := migrate.New("file://migrations", databaseURL)
	if err != nil {
		return fmt.Errorf("initialize migrations: %w", err)
	}
	defer func() {
		_, _ = m.Close()
	}()

	switch os.Args[1] {
	case "up":
		err = m.Up()

	case "down":
		err = m.Steps(-1)

	case "version":
		version, dirty, versionErr := m.Version()
		if errors.Is(versionErr, migrate.ErrNilVersion) {
			fmt.Println("database has no migration version")
			return nil
		}
		if versionErr != nil {
			return versionErr
		}

		fmt.Printf("version=%d dirty=%t\n", version, dirty)
		return nil

	default:
		return fmt.Errorf("unknown migration command %q", os.Args[1])
	}

	if errors.Is(err, migrate.ErrNoChange) {
		fmt.Println("no migration changes")
		return nil
	}

	return err
}
