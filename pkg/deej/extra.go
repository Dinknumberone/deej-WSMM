package deej

import (
	"encoding/binary"
	"math"
	"strconv"
	"strings"
	"time"

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

	go func() {

		for {
			time.Sleep(200 * time.Millisecond)

			var err error

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

			//d.logger.Debug(currentWindowProcessNames[0])

			if currentWindowProcessName == lastSession {
				continue
			}

			if d.sessions.lastSessionRefresh.Add(maxTimeBetweenSessionRefreshes).Before(time.Now()) {
				d.sessions.logger.Debug("Stale session map detected on slider move, refreshing")
				d.sessions.refreshSessions(true)
			}

			//d.logger.Debug(currentWindowProcessName)

			sessions, ok := d.sessions.get(currentWindowProcessName)

			//d.logger.Debug(d.sessions.m)

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
