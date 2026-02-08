package main

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"os"
	"time"

	"openbot/internal/config"

	"github.com/spf13/cobra"
	_ "modernc.org/sqlite"
)

func doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Run diagnostic checks on your OpenBot installation",
		Long: `Verifies that OpenBot's configuration, providers, database, and
workspace are correctly set up. Reports pass/fail for each check.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath := resolveConfigPath()
			fmt.Printf("OpenBot Doctor v%s\n", version)
			fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

			passed := 0
			failed := 0
			warned := 0

			// 1. Config file exists
			if _, err := os.Stat(cfgPath); err != nil {
				printFail("Config file", fmt.Sprintf("not found at %s", cfgPath))
				failed++
				fmt.Printf("\nRun 'openbot init' to create a default configuration.\n")
				return nil
			}
			printPass("Config file", cfgPath)
			passed++

			// 2. Config loads and validates
			cfg, err := config.Load(cfgPath)
			if err != nil {
				printFail("Config validation", err.Error())
				failed++
			} else {
				printPass("Config validation", "valid")
				passed++
			}

			if cfg == nil {
				fmt.Printf("\n%d passed, %d failed\n", passed, failed)
				return nil
			}

			// 3. Workspace directory exists
			if cfg.General.Workspace != "" {
				if info, err := os.Stat(cfg.General.Workspace); err != nil {
					printFail("Workspace", fmt.Sprintf("not found: %s", cfg.General.Workspace))
					failed++
				} else if !info.IsDir() {
					printFail("Workspace", fmt.Sprintf("not a directory: %s", cfg.General.Workspace))
					failed++
				} else {
					printPass("Workspace", cfg.General.Workspace)
					passed++
				}
			} else {
				printWarn("Workspace", "not configured (using current directory)")
				warned++
			}

			// 4. Database writable
			dbPath := cfg.Memory.DBPath
			if dbPath == "" {
				home, _ := os.UserHomeDir()
				dbPath = home + "/.openbot/openbot.db"
			}
			if err := checkDatabase(dbPath); err != nil {
				printFail("Database", err.Error())
				failed++
			} else {
				printPass("Database", dbPath)
				passed++
			}

			// 5. Check providers
			providerCount := 0
			for name, p := range cfg.Providers {
				if !p.Enabled {
					continue
				}
				providerCount++
				if p.APIKey == "" && p.APIBase == "" {
					printWarn("Provider: "+name, "enabled but no API key/base configured")
					warned++
				} else {
					printPass("Provider: "+name, "configured")
					passed++
				}
			}
			if providerCount == 0 {
				printFail("Providers", "no providers enabled")
				failed++
			}

			// 6. Check ports
			if cfg.Channels.Web.Enabled {
				port := cfg.Channels.Web.Port
				if port == 0 {
					port = 3000
				}
				if err := checkPort(port); err != nil {
					printWarn("Web port", fmt.Sprintf("port %d may be in use: %v", port, err))
					warned++
				} else {
					printPass("Web port", fmt.Sprintf(":%d available", port))
					passed++
				}
			}

			if cfg.API.Enabled {
				port := cfg.API.Port
				if port == 0 {
					port = 8080
				}
				if err := checkPort(port); err != nil {
					printWarn("API port", fmt.Sprintf("port %d may be in use: %v", port, err))
					warned++
				} else {
					printPass("API port", fmt.Sprintf(":%d available", port))
					passed++
				}
			}

			// 7. Check log file writable
			if cfg.General.LogFile != "" {
				dir := cfg.General.LogFile
				for i := len(dir) - 1; i >= 0; i-- {
					if dir[i] == '/' || dir[i] == '\\' {
						dir = dir[:i]
						break
					}
				}
				if err := os.MkdirAll(dir, 0o755); err != nil {
					printWarn("Log file", fmt.Sprintf("cannot create log directory: %v", err))
					warned++
				} else {
					printPass("Log file", cfg.General.LogFile)
					passed++
				}
			}

			// Summary
			fmt.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
			fmt.Printf("Results: %d passed, %d warnings, %d failed\n", passed, warned, failed)
			if failed > 0 {
				fmt.Printf("\nPlease fix the failed checks before running OpenBot.\n")
				return fmt.Errorf("%d check(s) failed", failed)
			}
			if warned > 0 {
				fmt.Printf("\nOpenBot should work but consider fixing the warnings.\n")
			} else {
				fmt.Printf("\nAll checks passed! OpenBot is ready to run.\n")
			}
			return nil
		},
	}
}

func checkDatabase(dbPath string) error {
	dir := dbPath
	for i := len(dir) - 1; i >= 0; i-- {
		if dir[i] == '/' || dir[i] == '\\' {
			dir = dir[:i]
			break
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("cannot create database directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return fmt.Errorf("cannot open: %w", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("cannot ping: %w", err)
	}

	// Try a write.
	if _, err := db.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS _doctor_test (id INTEGER PRIMARY KEY)"); err != nil {
		return fmt.Errorf("not writable: %w", err)
	}
	db.ExecContext(ctx, "DROP TABLE IF EXISTS _doctor_test")

	return nil
}

func checkPort(port int) error {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return err
	}
	ln.Close()
	return nil
}

func printPass(check, detail string) {
	fmt.Printf("  [PASS] %-20s %s\n", check, detail)
}

func printFail(check, detail string) {
	fmt.Printf("  [FAIL] %-20s %s\n", check, detail)
}

func printWarn(check, detail string) {
	fmt.Printf("  [WARN] %-20s %s\n", check, detail)
}
