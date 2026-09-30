package deej

import (
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/lxn/win"
	"github.com/shirou/gopsutil/v4/mem"
)

const (
	currentWindowUpdaterQuickCheckInterval = 200 * time.Millisecond
	currentWindowUpdaterFullCheckInterval  = 10000 * time.Second
)

func newExtraUtils(d *Deej) {

	go func(){
		setCurrentWindowEventHook(d)
	}()


	//computerStatisticUpdater(d)
}

func handleButtonLine(d *Deej, line string) {

	splitLine := strings.Split(line, ",")

	if len(splitLine) >= 2 {
		// Toggle/switch buttons can send state (e.g. "0,1" or "0,0").
		handleButtonWithState(d, splitLine[0], splitLine[1])
		d.logger.Debug("recieved button command with value: ", line)
		return
	}

	if len(splitLine) >= 1 {
		handleButton(d, splitLine[0])
	}

	//d.logger.Debug("recieved button command with value: ", line)
}

func handleButton(d *Deej, index string) {
	index = strings.TrimSpace(index)
	if index == "" {
		return
	}

	buttonIdx, err := strconv.Atoi(index)
	if err != nil {
		d.logger.Warnw("Received malformed button index", "value", index, "error", err)
		return
	}

	buttonMap := d.config.ButtonMapping
	if buttonMap == nil {
		d.logger.Debugw("Button mapping unavailable", "index", buttonIdx)
		return
	}

	commands, ok := buttonMap.get(buttonIdx)
	if !ok || len(commands) == 0 {
		d.logger.Debugw("No button mapping for index", "index", buttonIdx, "hint", "add button_mapping.<index> to config.yaml or verify your firmware's button numbering")
		return
	}

	for _, command := range commands {
		if command.Special != "" {
			if err := handleSpecialButtonCommand(d, command, false, 0); err != nil {
				d.logger.Warnw("Failed to handle special button command", "index", buttonIdx, "special", command.Special, "error", err)
			}
			continue
		}

		if len(command.Keys) == 0 {
			d.logger.Warnw("Button mapping did not resolve to any virtual keys", "index", buttonIdx, "value", command.Raw)
			continue
		}

		sent := injectKeyboardCombo(command.Keys)
		expected := uint32(len(command.Keys) * 2)
		if expected > 0 && sent != expected {
			d.logger.Warnw("Failed to inject all key events",
				"index", buttonIdx,
				"command", command.Raw,
				"expectedEvents", expected,
				"sentEvents", sent)
		}
	}
}

func handleButtonWithState(d *Deej, index string, stateString string) {
	index = strings.TrimSpace(index)
	stateString = strings.TrimSpace(stateString)
	if index == "" || stateString == "" {
		return
	}

	buttonIdx, err := strconv.Atoi(index)
	if err != nil {
		d.logger.Warnw("Received malformed button index", "value", index, "error", err)
		return
	}

	state, err := strconv.Atoi(stateString)
	if err != nil {
		d.logger.Warnw("Received malformed button state", "value", stateString, "error", err)
		return
	}

	if state != 0 && state != 1 {
		d.logger.Warnw("Received unsupported button state", "value", state)
		return
	}

	buttonMap := d.config.ButtonMapping
	if buttonMap == nil {
		d.logger.Debugw("Button mapping unavailable", "index", buttonIdx)
		return
	}

	commands, ok := buttonMap.get(buttonIdx)
	if !ok || len(commands) == 0 {
		d.logger.Debugw("No button mapping for index", "index", buttonIdx, "hint", "add button_mapping.<index> to config.yaml or verify your firmware's button numbering")
		return
	}

	for _, command := range commands {
		if command.Special != "" {
			if err := handleSpecialButtonCommand(d, command, true, state); err != nil {
				d.logger.Warnw("Failed to handle special switch command", "index", buttonIdx, "special", command.Special, "state", state, "error", err)
			}
			continue
		}

		// For regular key combos, treat state=1 as "press" and state=0 as "release".
		if state == 0 {
			continue
		}

		if len(command.Keys) == 0 {
			d.logger.Warnw("Button mapping did not resolve to any virtual keys", "index", buttonIdx, "value", command.Raw)
			continue
		}

		sent := injectKeyboardCombo(command.Keys)
		expected := uint32(len(command.Keys) * 2)
		if expected > 0 && sent != expected {
			d.logger.Warnw("Failed to inject all key events",
				"index", buttonIdx,
				"command", command.Raw,
				"expectedEvents", expected,
				"sentEvents", sent)
		}
	}
}

func injectKeyboardCombo(keys []uint16) uint32 {
	inputs := make([]win.KEYBD_INPUT, 0, len(keys)*2)

	for _, key := range keys {
		if key == 0 {
			continue
		}

		inputs = append(inputs, win.KEYBD_INPUT{
			Type: win.INPUT_KEYBOARD,
			Ki: win.KEYBDINPUT{
				WVk: key,
			},
		})
	}

	for i := len(keys) - 1; i >= 0; i-- {
		key := keys[i]

		if key == 0 {
			continue
		}

		inputs = append(inputs, win.KEYBD_INPUT{
			Type: win.INPUT_KEYBOARD,
			Ki: win.KEYBDINPUT{
				WVk:     key,
				DwFlags: win.KEYEVENTF_KEYUP,
			},
		})
	}

	if len(inputs) == 0 {
		return 0
	}

	return win.SendInput(uint32(len(inputs)), unsafe.Pointer(&inputs[0]), int32(unsafe.Sizeof(inputs[0])))
}

func handleSpecialButtonCommand(d *Deej, command buttonCommand, stateProvided bool, state int) error {
	switch command.Special {
	case specialAudioDeviceSwitch:
		if len(command.Args) != 2 {
			return nil
		}

		firstDevice := command.Args[0]
		secondDevice := command.Args[1]

		if stateProvided {
			// Switch: state=0 => first, state=1 => second.
			if state == 0 {
				return setDefaultAudioRenderDeviceByFriendlyName(d.logger, firstDevice)
			}
			return setDefaultAudioRenderDeviceByFriendlyName(d.logger, secondDevice)
		}

		// Momentary button: toggle between the two devices on press.
		return toggleDefaultAudioRenderDeviceByFriendlyNames(d.logger, firstDevice, secondDevice)
	default:
		return nil
	}
}

func setCurrentWindowEventHook(d *Deej) {
	
		callback := win.WINEVENTPROC(func(
			hWinEventHook win.HWINEVENTHOOK,
			event uint32,
			hwnd win.HWND,
			idObject int32,
			idChild int32,
			idEventThread uint32,
			dwmsEventTime uint32,
		) uintptr {
			onCurrentWindowChange(hwnd, d)
			return 0
		})

		hook, _ := win.SetWinEventHook(
			win.EVENT_SYSTEM_FOREGROUND,
			win.EVENT_SYSTEM_FOREGROUND,
			0,
			callback,
			0,
			0,
			win.WINEVENT_OUTOFCONTEXT,
		)

		defer win.UnhookWinEvent(hook)
		
		var msg win.MSG
		for win.GetMessage(&msg, 0, 0, 0) != 0 {
			win.TranslateMessage(&msg)
			win.DispatchMessage(&msg)
		}

		_ = unsafe.Pointer(nil)
}


func onCurrentWindowChange(hwnd win.HWND, d *Deej) {
	time.Sleep(30 * time.Millisecond) 

	if (d.sessions.currentWindow == hwnd) {
		return
	}

	currentSliderIDs := d.sessions.currentSliderIDs()
	
	if len(currentSliderIDs) == 0 {
		d.logger.Debug("no current slider found, skipping current window update")
		return
	}
	
	currentSliderID := currentSliderIDs[0]
	registeredCurrentTarget := d.sessions.currentTargetStatus()
	
	resolvedTargets := d.sessions.resolveCurrentWindowTarget(currentSliderID, true)
	if len(resolvedTargets) == 0 {
		d.logger.Debug("no resolved target found, skipping current window update")
		return
	}

	
	currentWindowProcessName := resolvedTargets[0]

	d.logger.Debug("before resolved target check | current: ", currentWindowProcessName, " | registered: ", registeredCurrentTarget)

	

	if currentWindowProcessName == registeredCurrentTarget{
		return
	}

	d.sessions.logger.Debug("Current window changed, updating slider target: ", string(currentWindowProcessName))

	if d.sessions.lastSessionRefresh.Add(maxTimeBetweenSessionRefreshes).Before(time.Now()) {
		d.sessions.logger.Debug("Stale session map detected on slider move, refreshing")
		d.sessions.refreshSessions(true)
	}

	sessions, ok := d.sessions.get(currentWindowProcessName)

	if !ok || len(sessions) == 0 {
		d.sessions.refreshSessions(false)
		sessions, ok = d.sessions.get(currentWindowProcessName)
	}
	if !ok || len(sessions) == 0 {
		return
	}

	d.sessions.lockCurrentSlider(currentSliderID)

	currentVol := sessions[0].GetVolume()
	currentVol *= 255
	intVol := int(currentVol)

	//transform scale to proper for the microcontroller

	d.logger.Debug(currentWindowProcessName, currentVol)

	d.sessions.currentWindow = hwnd

	message := "goto " + strconv.Itoa(intVol)

	d.logger.Debug("sending message: ", string(message))

	d.serial.SendCommand(message)

}

func oldcurrentWindowUpdater(d *Deej) {

	lastCurrentSliderID := -1
	var lastwindow win.HWND
	var lastFullCheckAt time.Time

	go func() {

		for {
			time.Sleep(currentWindowUpdaterQuickCheckInterval)

			//cheap current window check just to see if the active window has changed
			currentWindow := win.GetForegroundWindow()
			windowChanged := currentWindow != lastwindow
			fullCheckDue := lastFullCheckAt.IsZero() || lastFullCheckAt.Add(currentWindowUpdaterFullCheckInterval).Before(time.Now())
			if !windowChanged && !fullCheckDue {
				continue
			}

			lastwindow = currentWindow
			lastFullCheckAt = time.Now()

			currentSliderIDs := d.sessions.currentSliderIDs()
			if len(currentSliderIDs) == 0 {
				continue
			}

			currentSliderID := currentSliderIDs[0]
			registeredCurrentTarget := d.sessions.currentTargetStatus()

			resolvedTargets := d.sessions.resolveCurrentWindowTarget(currentSliderID, true)
			if len(resolvedTargets) == 0 {
				continue
			}

			currentWindowProcessName := resolvedTargets[0]
			if currentWindowProcessName == registeredCurrentTarget && currentSliderID == lastCurrentSliderID{
				continue
			}

			if d.sessions.lastSessionRefresh.Add(maxTimeBetweenSessionRefreshes).Before(time.Now()) {
				d.sessions.logger.Debug("Stale session map detected on slider move, refreshing")
				d.sessions.refreshSessions(true)
			}

			sessions, ok := d.sessions.get(currentWindowProcessName)

			if !ok || len(sessions) == 0 {
				d.sessions.refreshSessions(false)
				sessions, ok = d.sessions.get(currentWindowProcessName)
			}
			if !ok || len(sessions) == 0 {
				continue
			}

			d.sessions.lockCurrentSlider(currentSliderID)

			currentVol := sessions[0].GetVolume()
			currentVol *= 255
			intVol := int(currentVol)

			//transform scale to proper for the microcontroller

			d.logger.Debug(currentWindowProcessName, currentVol)

			message := "goto " + strconv.Itoa(intVol)

			d.logger.Debug("sending message: ", message)
			if err := d.serial.SendCommand(message); err != nil {
				d.logger.Warnw("failed to write goto command", "error", err)
			}

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
