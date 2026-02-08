package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func backupCmd() *cobra.Command {
	var outputPath string

	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Create a backup of OpenBot data (database + config)",
		Long: `Creates a compressed .tar.gz archive containing the SQLite database
and configuration file. The backup is timestamped by default.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath := resolveConfigPath()

			// Resolve database path from config
			dbPath := resolveDBPath(cfgPath)

			if outputPath == "" {
				home, _ := os.UserHomeDir()
				backupDir := filepath.Join(home, ".openbot", "backups")
				if err := os.MkdirAll(backupDir, 0o755); err != nil {
					return fmt.Errorf("cannot create backup directory: %w", err)
				}
				ts := time.Now().Format("20060102-150405")
				outputPath = filepath.Join(backupDir, fmt.Sprintf("openbot-backup-%s.tar.gz", ts))
			}

			files := []string{}

			// Add database file if it exists
			if _, err := os.Stat(dbPath); err == nil {
				files = append(files, dbPath)
				// Also include WAL and SHM files if present
				for _, suffix := range []string{"-wal", "-shm"} {
					walPath := dbPath + suffix
					if _, err := os.Stat(walPath); err == nil {
						files = append(files, walPath)
					}
				}
			}

			// Add config file if it exists
			if _, err := os.Stat(cfgPath); err == nil {
				files = append(files, cfgPath)
			}

			if len(files) == 0 {
				return fmt.Errorf("no files to backup (db: %s, config: %s)", dbPath, cfgPath)
			}

			if err := createTarGz(outputPath, files); err != nil {
				return fmt.Errorf("backup failed: %w", err)
			}

			fmt.Printf("Backup created: %s\n", outputPath)
			fmt.Printf("Files included: %d\n", len(files))
			for _, f := range files {
				info, _ := os.Stat(f)
				size := int64(0)
				if info != nil {
					size = info.Size()
				}
				fmt.Printf("  - %s (%s)\n", filepath.Base(f), humanSize(size))
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "output file path (default: ~/.openbot/backups/openbot-backup-<timestamp>.tar.gz)")
	return cmd
}

func restoreCmd() *cobra.Command {
	var inputPath string
	var force bool

	cmd := &cobra.Command{
		Use:   "restore",
		Short: "Restore OpenBot data from a backup archive",
		Long: `Restores the SQLite database and configuration file from a .tar.gz
backup archive created by 'openbot backup'.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if inputPath == "" && len(args) > 0 {
				inputPath = args[0]
			}
			if inputPath == "" {
				return fmt.Errorf("specify a backup file: openbot restore <file.tar.gz>")
			}

			cfgPath := resolveConfigPath()
			dbPath := resolveDBPath(cfgPath)

			// Safety: warn before overwriting
			if !force {
				existing := false
				if _, err := os.Stat(dbPath); err == nil {
					existing = true
				}
				if _, err := os.Stat(cfgPath); err == nil {
					existing = true
				}
				if existing {
					fmt.Printf("WARNING: This will overwrite existing data.\n")
					fmt.Printf("  Database: %s\n", dbPath)
					fmt.Printf("  Config:   %s\n", cfgPath)
					fmt.Printf("Use --force to skip this warning.\n")
					return fmt.Errorf("restore aborted (use --force to proceed)")
				}
			}

			restored, err := extractTarGz(inputPath, dbPath, cfgPath)
			if err != nil {
				return fmt.Errorf("restore failed: %w", err)
			}

			fmt.Printf("Restore completed from: %s\n", inputPath)
			fmt.Printf("Files restored: %d\n", len(restored))
			for _, f := range restored {
				fmt.Printf("  - %s\n", f)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&inputPath, "input", "i", "", "backup file to restore from")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing data without warning")
	return cmd
}

// resolveDBPath determines the database file path based on config location.
func resolveDBPath(cfgPath string) string {
	dir := filepath.Dir(cfgPath)
	return filepath.Join(dir, "openbot.db")
}

// createTarGz creates a .tar.gz archive from the given files.
func createTarGz(outputPath string, files []string) error {
	outFile, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	gzWriter := gzip.NewWriter(outFile)
	defer gzWriter.Close()

	tarWriter := tar.NewWriter(gzWriter)
	defer tarWriter.Close()

	for _, filePath := range files {
		if err := addFileToTar(tarWriter, filePath); err != nil {
			return fmt.Errorf("add %s: %w", filePath, err)
		}
	}

	return nil
}

func addFileToTar(tw *tar.Writer, filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return err
	}

	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	// Use just the base filename in the archive.
	header.Name = filepath.Base(filePath)

	if err := tw.WriteHeader(header); err != nil {
		return err
	}

	_, err = io.Copy(tw, file)
	return err
}

// extractTarGz extracts relevant files from a backup archive.
func extractTarGz(archivePath, dbPath, cfgPath string) ([]string, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return nil, fmt.Errorf("not a valid gzip file: %w", err)
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)
	var restored []string

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		// Determine target path based on filename.
		var targetPath string
		baseName := filepath.Base(header.Name)
		switch {
		case baseName == "config.json":
			targetPath = cfgPath
		case strings.HasSuffix(baseName, ".db"):
			targetPath = dbPath
		case strings.HasSuffix(baseName, ".db-wal"):
			targetPath = dbPath + "-wal"
		case strings.HasSuffix(baseName, ".db-shm"):
			targetPath = dbPath + "-shm"
		default:
			// Unknown file, restore to same directory as config
			targetPath = filepath.Join(filepath.Dir(cfgPath), baseName)
		}

		// Create parent directory.
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return nil, err
		}

		outFile, err := os.Create(targetPath)
		if err != nil {
			return nil, fmt.Errorf("create %s: %w", targetPath, err)
		}

		if _, err := io.Copy(outFile, tarReader); err != nil {
			outFile.Close()
			return nil, fmt.Errorf("extract %s: %w", targetPath, err)
		}
		outFile.Close()

		restored = append(restored, targetPath)
	}

	return restored, nil
}

func humanSize(bytes int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case bytes >= gb:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(gb))
	case bytes >= mb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
