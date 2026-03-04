package deej

import (
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/getlantern/systray"

	"github.com/omriharel/deej/pkg/deej/icon"
	"github.com/omriharel/deej/pkg/deej/util"
)

func (d *Deej) initializeTray(onDone func()) {
	logger := d.logger.Named("tray")

	onReady := func() {
		logger.Debug("Tray instance ready")

		systray.SetTemplateIcon(icon.DeejLogo, icon.DeejLogo)
		systray.SetTitle("deej")
		systray.SetTooltip("deej")

		editConfig := systray.AddMenuItem("Edit configuration", "Open config file with notepad")
		editConfig.SetIcon(icon.EditConfig)

		refreshSessions := systray.AddMenuItem("Re-scan audio sessions", "Manually refresh audio sessions if something's stuck")
		refreshSessions.SetIcon(icon.RefreshSessions)
		openStatusWindow := systray.AddMenuItem("Open live status window", "Open a persistent auto-refreshing status console")

		systray.AddSeparator()
		currentTargetInfo := systray.AddMenuItem("Current app: (none)", "Resolved target for deej.current")
		currentTargetInfo.Disable()
		currentLocksInfo := systray.AddMenuItem("Locked slider(s): (none)", "Sliders temporarily locked while motorized slider moves")
		currentLocksInfo.Disable()

		if d.version != "" {
			systray.AddSeparator()
			versionInfo := systray.AddMenuItem(d.version, "")
			versionInfo.Disable()
		}

		systray.AddSeparator()
		quit := systray.AddMenuItem("Quit", "Stop deej and quit")

		// wait on things to happen
		go func() {
			for {
				select {

				// quit
				case <-quit.ClickedCh:
					logger.Info("Quit menu item clicked, stopping")

					d.signalStop()

				// edit config
				case <-editConfig.ClickedCh:
					logger.Info("Edit config menu item clicked, opening config for editing")

					editor := "notepad.exe"
					if util.Linux() {
						editor = "gedit"
					}

					if err := util.OpenExternal(logger, editor, userConfigFilepath); err != nil {
						logger.Warnw("Failed to open config file for editing", "error", err)
					}

				// refresh sessions
				case <-refreshSessions.ClickedCh:
					logger.Info("Refresh sessions menu item clicked, triggering session map refresh")

					// performance: the reason that forcing a refresh here is okay is that users can't spam the
					// right-click -> select-this-option sequence at a rate that's meaningful to performance
					d.sessions.refreshSessions(true)

				// open live status console
				case <-openStatusWindow.ClickedCh:
					logger.Info("Open status window clicked")

					statusFilePath, err := filepath.Abs(filepath.Join(logDirectory, statusFileName))
					if err != nil {
						logger.Warnw("Failed to resolve status file path", "error", err)
						break
					}

					script := "$p='" + strings.ReplaceAll(statusFilePath, "'", "''") + "'; " +
						"$last=''; " +
						"$rows=8; " +
						"while ($true) { " +
						"if (Test-Path $p) { $content = [System.IO.File]::ReadAllText($p) } else { $content = 'Waiting for status file...'; }; " +
						"if ($content -ne $last) { " +
						"$last = $content; " +
						"$width = [Math]::Max([Console]::WindowWidth - 1, 1); " +
						"$lines = $content -split \"`r?`n\"; " +
						"[Console]::SetCursorPosition(0, 0); " +
						"for ($i = 0; $i -lt $rows; $i++) { " +
						"if ($i -lt $lines.Count) { " +
						"$line = $lines[$i]; " +
						"if ($line.Length -gt $width) { $line = $line.Substring(0, $width) }; " +
						"Write-Host ($line.PadRight($width)); " +
						"} else { " +
						"Write-Host (''.PadRight($width)); " +
						"} " +
						"} " +
						"}; " +
						"Start-Sleep -Milliseconds 100 " +
						"}"

					if err := util.OpenExternalArgs(
						logger,
						"powershell.exe",
						"-NoExit",
						"-ExecutionPolicy",
						"Bypass",
						"-Command",
						script,
					); err != nil {
						logger.Warnw("Failed to open status window", "error", err)
					}
				}
			}
		}()

		go func() {
			ticker := time.NewTicker(250 * time.Millisecond)
			defer ticker.Stop()

			for range ticker.C {
				currentTarget := d.sessions.currentTargetStatus()
				if currentTarget == "" {
					currentTarget = "(none)"
				}

				lockedSliderIDs := d.sessions.currentLockedSliderIDs()
				lockedSummary := "(none)"
				if len(lockedSliderIDs) > 0 {
					idStrings := make([]string, len(lockedSliderIDs))
					for idx, sliderID := range lockedSliderIDs {
						idStrings[idx] = strconv.Itoa(sliderID)
					}

					lockedSummary = strings.Join(idStrings, ", ")
				}

				currentTargetTitle := "Current app: " + currentTarget
				if len(currentTargetTitle) > 80 {
					currentTargetTitle = currentTargetTitle[:76] + "..."
				}
				currentTargetInfo.SetTitle(currentTargetTitle)

				currentLocksTitle := "Locked slider(s): " + lockedSummary
				if len(currentLocksTitle) > 80 {
					currentLocksTitle = currentLocksTitle[:76] + "..."
				}

				currentLocksInfo.SetTitle(currentLocksTitle)
			}
		}()

		// actually start the main runtime
		onDone()
	}

	onExit := func() {
		logger.Debug("Tray exited")
	}

	// start the tray icon
	logger.Debug("Running in tray")
	systray.Run(onReady, onExit)
}

func (d *Deej) stopTray() {
	d.logger.Debug("Quitting tray")
	systray.Quit()
}
