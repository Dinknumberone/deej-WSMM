package deej

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/omriharel/deej/pkg/deej/util"
)

const statusFileName = "deej-status.txt"

func (d *Deej) startStatusFileUpdater() {
	statusFilePath := filepath.Join(logDirectory, statusFileName)

	go func() {
		if err := util.EnsureDirExists(logDirectory); err != nil {
			d.logger.Warnw("Failed to ensure status directory exists", "error", err)
			return
		}

		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		lastSnapshot := ""
		for range ticker.C {
			snapshot := d.statusSnapshot()
			if snapshot == lastSnapshot {
				continue
			}

			if err := os.WriteFile(statusFilePath, []byte(snapshot), 0644); err != nil {
				d.logger.Warnw("Failed to write status file", "path", statusFilePath, "error", err)
				continue
			}

			lastSnapshot = snapshot
		}
	}()
}

func (d *Deej) statusSnapshot() string {
	currentTarget := d.sessions.currentTargetStatus()
	if currentTarget == "" {
		currentTarget = "(none)"
	}

	lockedSliders := d.sessions.currentLockedSliderIDs()
	lockedSliderText := "(none)"
	if len(lockedSliders) > 0 {
		idStrings := make([]string, len(lockedSliders))
		for idx, sliderID := range lockedSliders {
			idStrings[idx] = fmt.Sprintf("%d", sliderID)
		}

		lockedSliderText = strings.Join(idStrings, ", ")
	}

	currentSliderIDs := d.sessions.currentSliderIDs()
	currentSliderText := "(none)"
	if len(currentSliderIDs) > 0 {
		idStrings := make([]string, len(currentSliderIDs))
		for idx, sliderID := range currentSliderIDs {
			idStrings[idx] = fmt.Sprintf("%d", sliderID)
		}

		currentSliderText = strings.Join(idStrings, ", ")
	}

	return fmt.Sprintf(
		"deej live status\nCurrent slider(s): %s\nCurrent app: %s\nLocked slider(s): %s\n",
		currentSliderText,
		currentTarget,
		lockedSliderText,
	)
}
