package deej

import (
	"strconv"
	"time"

	"github.com/lxn/win"
	"github.com/shirou/gopsutil/v4/mem"
)

func newExtraUtils(d *Deej) {

	currentWindowUpdater(d)

	computerStatisticUpdater(d)
}

func handleButton(d *Deej, line string) {
	d.logger.Debug("recieved button command with value: ", line)
}

func currentWindowUpdater(d *Deej) {

	var lastSession string
	lastCurrentSliderID := -1
	var lastwindow win.HWND

	go func() {

		for {
			time.Sleep(100 * time.Millisecond)

			//cheap current window check just to see if the active window has changed
			currentWindow := win.GetForegroundWindow()
			if currentWindow == lastwindow {
				continue
			}
			lastwindow = currentWindow

			currentSliderIDs := d.sessions.currentSliderIDs()
			if len(currentSliderIDs) == 0 {
				continue
			}

			currentSliderID := currentSliderIDs[0]

			resolvedTargets := d.sessions.resolveCurrentWindowTarget(currentSliderID, true)
			if len(resolvedTargets) == 0 {
				continue
			}

			currentWindowProcessName := resolvedTargets[0]
			if currentWindowProcessName == lastSession && currentSliderID == lastCurrentSliderID {
				continue
			}

			if d.sessions.lastSessionRefresh.Add(maxTimeBetweenSessionRefreshes).Before(time.Now()) {
				d.sessions.logger.Debug("Stale session map detected on slider move, refreshing")
				d.sessions.refreshSessions(true)
			}

			sessions, ok := d.sessions.get(currentWindowProcessName)

			if !ok {
				continue
			}

			d.sessions.lockCurrentSlider(currentSliderID)

			currentVol := sessions[0].GetVolume()
			currentVol *= 255
			intVol := int(currentVol)

			//transform scale to proper for the microcontroller

			d.logger.Debug(currentWindowProcessName, currentVol)

			message := []byte("goto " + strconv.Itoa(intVol) + "\n")

			d.logger.Debug("sending message: ", string(message))
			if _, err := d.serial.conn.Write(message); err != nil {
				d.logger.Warnw("failed to write goto command", "error", err)
			}

			lastSession = currentWindowProcessName
			lastCurrentSliderID = currentSliderID
		}
	}()
}

func computerStatisticUpdater(d *Deej) {

	go func() {
		for {
			time.Sleep(10000 * time.Millisecond)
			//create btye array with text for command

		}
	}()

}

func (d *Deej) GetMemoryInfo() (*mem.VirtualMemoryStat, error) {
	return mem.VirtualMemory()

}
