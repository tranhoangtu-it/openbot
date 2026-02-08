package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

func installDaemonCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Install OpenBot as a system daemon (launchd/systemd)",
		Long:  "Generates and installs a service file to run OpenBot as a background daemon on system startup.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath := resolveConfigPath()
			execPath, err := os.Executable()
			if err != nil {
				return fmt.Errorf("cannot determine executable path: %w", err)
			}

			switch runtime.GOOS {
			case "darwin":
				return installLaunchd(execPath, cfgPath)
			case "linux":
				return installSystemd(execPath, cfgPath)
			default:
				return fmt.Errorf("unsupported OS: %s (supported: darwin, linux)", runtime.GOOS)
			}
		},
	}
}

func uninstallDaemonCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove OpenBot system daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			switch runtime.GOOS {
			case "darwin":
				return uninstallLaunchd()
			case "linux":
				return uninstallSystemd()
			default:
				return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
			}
		},
	}
}

const launchdLabel = "com.openbot.gateway"

func installLaunchd(execPath, cfgPath string) error {
	home, _ := os.UserHomeDir()
	plistDir := filepath.Join(home, "Library", "LaunchAgents")
	plistPath := filepath.Join(plistDir, launchdLabel+".plist")

	logPath := filepath.Join(home, ".openbot", "logs", "openbot.log")
	errLogPath := filepath.Join(home, ".openbot", "logs", "openbot-error.log")

	// Ensure log directory exists.
	os.MkdirAll(filepath.Dir(logPath), 0o755)

	plist := strings.ReplaceAll(launchdTemplate, "{{EXEC}}", execPath)
	plist = strings.ReplaceAll(plist, "{{CONFIG}}", cfgPath)
	plist = strings.ReplaceAll(plist, "{{LABEL}}", launchdLabel)
	plist = strings.ReplaceAll(plist, "{{LOG}}", logPath)
	plist = strings.ReplaceAll(plist, "{{ERR_LOG}}", errLogPath)

	if err := os.MkdirAll(plistDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(plistPath, []byte(plist), 0o644); err != nil {
		return err
	}

	fmt.Printf("Daemon installed: %s\n", plistPath)
	fmt.Printf("To start: launchctl load %s\n", plistPath)
	fmt.Printf("To stop:  launchctl unload %s\n", plistPath)
	return nil
}

func uninstallLaunchd() error {
	home, _ := os.UserHomeDir()
	plistPath := filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist")
	if err := os.Remove(plistPath); err != nil {
		return fmt.Errorf("remove plist: %w", err)
	}
	fmt.Printf("Daemon uninstalled: %s\n", plistPath)
	return nil
}

func installSystemd(execPath, cfgPath string) error {
	home, _ := os.UserHomeDir()
	unitDir := filepath.Join(home, ".config", "systemd", "user")
	unitPath := filepath.Join(unitDir, "openbot.service")

	unit := strings.ReplaceAll(systemdTemplate, "{{EXEC}}", execPath)
	unit = strings.ReplaceAll(unit, "{{CONFIG}}", cfgPath)

	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(unitPath, []byte(unit), 0o644); err != nil {
		return err
	}

	fmt.Printf("Daemon installed: %s\n", unitPath)
	fmt.Printf("To start:  systemctl --user start openbot\n")
	fmt.Printf("To enable: systemctl --user enable openbot\n")
	fmt.Printf("To stop:   systemctl --user stop openbot\n")
	return nil
}

func uninstallSystemd() error {
	home, _ := os.UserHomeDir()
	unitPath := filepath.Join(home, ".config", "systemd", "user", "openbot.service")
	if err := os.Remove(unitPath); err != nil {
		return fmt.Errorf("remove unit: %w", err)
	}
	fmt.Printf("Daemon uninstalled: %s\n", unitPath)
	return nil
}

const launchdTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>{{LABEL}}</string>
    <key>ProgramArguments</key>
    <array>
        <string>{{EXEC}}</string>
        <string>gateway</string>
        <string>--config</string>
        <string>{{CONFIG}}</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>{{LOG}}</string>
    <key>StandardErrorPath</key>
    <string>{{ERR_LOG}}</string>
</dict>
</plist>`

const systemdTemplate = `[Unit]
Description=OpenBot AI Assistant Gateway
After=network.target

[Service]
Type=simple
ExecStart={{EXEC}} gateway --config {{CONFIG}}
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target`
