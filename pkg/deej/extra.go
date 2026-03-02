package deej

import (
	"encoding/binary"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/lxn/win"
	"github.com/omriharel/deej/pkg/deej/util"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/thoas/go-funk"
)

func newExtraUtils(d *Deej) {

	currentWindowUpdater(d)

	computerStatisticUpdater(d)
}

func currentWindowUpdater(d *Deej) {

	var lastSession string
	var lastwindow win.HWND

	go func() {

		for {
			time.Sleep(100 * time.Millisecond)

			var err error

			//cheap current window check just to see if the active window has changed
			currentWindow := win.GetForegroundWindow()
			if currentWindow == lastwindow {
				continue
			}
			lastwindow = currentWindow

			//only if window has changed, get full current proccess info
			rawCurrentWindowProcessNames, err := util.GetCurrentWindowProcessNames()
			if err != nil {
				continue
			}

			currentWindowProcessNames := funk.UniqString(rawCurrentWindowProcessNames)
			if len(currentWindowProcessNames) == 0 {
				d.logger.Warn("found 0 length in windows process names")
				continue
			}

			currentWindowProcessName := currentWindowProcessNames[0]

			currentWindowProcessName = strings.ToLower(currentWindowProcessName)

			if currentWindowProcessName == lastSession {
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

			currentVol := sessions[0].GetVolume()
			currentVol *= 255
			intVol := int(currentVol)

			//transform scale to proper for the microcontroller

			d.logger.Debug(currentWindowProcessName, currentVol)

			//turn volume number into byte array
			b := make([]byte, 4)
			binary.LittleEndian.PutUint32(b, math.Float32bits(currentVol))

			message := append([]byte("goto "), []byte(strconv.Itoa(intVol))...)

			d.logger.Debug("sending message: ", string(message))
			d.serial.conn.Write(message)

			lastSession = currentWindowProcessName
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
