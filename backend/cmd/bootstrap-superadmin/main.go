package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/shivam3746/audio-speech-vault/internal/auth"
	"github.com/shivam3746/audio-speech-vault/internal/platform/database"
	"golang.org/x/term"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("bootstrap failed: %v", err)
	}
}

func run() error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("run this command in an interactive terminal")
	}
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Superadmin email: ")
	email, err := reader.ReadString('\n')
	if err != nil {
		return err
	}
	fmt.Print("Display name: ")
	name, err := reader.ReadString('\n')
	if err != nil {
		return err
	}
	fmt.Print("Password (12-72 bytes): ")
	password, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return err
	}
	fmt.Print("Confirm password: ")
	confirmation, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return err
	}
	if string(password) != string(confirmation) {
		return fmt.Errorf("passwords do not match")
	}
	databaseURL := os.Getenv("DATABASE_URL")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := auth.BootstrapSuperadmin(ctx, pool, strings.TrimSpace(email), strings.TrimSpace(name), string(password)); err != nil {
		return err
	}
	fmt.Println("Superadmin created.")
	return nil
}
