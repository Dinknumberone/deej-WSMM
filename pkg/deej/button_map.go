package deej

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/lxn/win"
)

type buttonCommand struct {
	Raw  string
	Keys []uint16

	// If Special is non-empty, this command isn't a key combo and may carry extra args.
	// Currently supported: "deej.audio_device_switch" with exactly 2 device names.
	Special string
	Args    []string
}

type buttonMap struct {
	m    map[int][]buttonCommand
	lock sync.Locker
}

func newButtonMap() *buttonMap {
	return &buttonMap{
		m:    make(map[int][]buttonCommand),
		lock: &sync.Mutex{},
	}
}

func buttonMapFromConfigs(userMapping map[string][]string, internalMapping map[string][]string) *buttonMap {
	result := newButtonMap()

	for indexString, rawValues := range userMapping {
		index, _ := strconv.Atoi(indexString)
		parsed := parseButtonCommands(rawValues)
		if len(parsed) > 0 {
			result.set(index, parsed)
		}
	}

	for indexString, rawValues := range internalMapping {
		index, _ := strconv.Atoi(indexString)
		existing, _ := result.get(index)

		for _, command := range parseButtonCommands(rawValues) {
			if containsButtonCommand(existing, command.Raw) {
				continue
			}
			existing = append(existing, command)
		}

		if len(existing) > 0 {
			result.set(index, existing)
		}
	}

	return result
}

func parseButtonCommands(rawValues []string) []buttonCommand {
	commands := make([]buttonCommand, 0, len(rawValues))

	for i := 0; i < len(rawValues); i++ {
		trimmed := strings.TrimSpace(rawValues[i])
		if trimmed == "" {
			continue
		}

		// Allow a "multi-line" special command to consume extra entries.
		// Example:
		//   button_mapping:
		//     0:
		//       - deej.audio_device_switch
		//       - Speakers (...)
		//       - Headphones (...)
		if isAudioDeviceSwitchToken(trimmed) {
			if i+2 >= len(rawValues) {
				continue
			}

			first := strings.TrimSpace(rawValues[i+1])
			second := strings.TrimSpace(rawValues[i+2])
			if first == "" || second == "" {
				i += 2
				continue
			}

			commands = append(commands, buttonCommand{
				Raw:     trimmed,
				Special: specialAudioDeviceSwitch,
				Args:    []string{first, second},
			})

			i += 2
			continue
		}

		command := newButtonCommand(trimmed)
		if len(command.Keys) == 0 {
			continue
		}

		commands = append(commands, command)
	}

	return commands
}

func containsButtonCommand(existing []buttonCommand, raw string) bool {
	for _, existingCommand := range existing {
		if strings.EqualFold(existingCommand.Raw, raw) {
			return true
		}
	}

	return false
}

func newButtonCommand(raw string) buttonCommand {
	command := buttonCommand{Raw: raw}

	for _, rawToken := range strings.Split(raw, "+") {
		if vk := virtualKeyForToken(rawToken); vk != 0 {
			command.Keys = append(command.Keys, vk)
		}
	}

	return command
}

const (
	specialAudioDeviceSwitch = "deej.audio_device_switch"
)

func isAudioDeviceSwitchToken(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	normalized = strings.ReplaceAll(normalized, " ", "_")
	normalized = strings.Trim(normalized, "_")

	switch normalized {
	case specialAudioDeviceSwitch, "deej.device_switch", "deej.audio_device_toggle", "deej.device_toggle":
		return true
	default:
		return false
	}
}

func virtualKeyForToken(token string) uint16 {
	normalized := strings.ToLower(strings.TrimSpace(token))
	if normalized == "" {
		return 0
	}

	normalized = strings.ReplaceAll(normalized, "-", " ")
	normalized = strings.ReplaceAll(normalized, "_", " ")
	normalized = strings.Join(strings.Fields(normalized), " ")

	if vk, ok := buttonTokenToVK[normalized]; ok {
		return vk
	}

	collapsed := strings.ReplaceAll(normalized, " ", "")
	if collapsed != "" && collapsed != normalized {
		if vk, ok := buttonTokenToVK[collapsed]; ok {
			return vk
		}
	}

	if len(normalized) == 1 {
		ch := normalized[0]
		if ch >= 'a' && ch <= 'z' {
			// Virtual-key codes for A-Z match ASCII 'A'..'Z' (0x41..0x5A).
			return uint16('A' + (ch - 'a'))
		}
		if ch >= '0' && ch <= '9' {
			// Virtual-key codes for 0-9 match ASCII '0'..'9' (0x30..0x39).
			return uint16(ch)
		}
	}

	return 0
}

var buttonTokenToVK = map[string]uint16{
	"ctrl":              win.VK_CONTROL,
	"control":           win.VK_CONTROL,
	"lctrl":             win.VK_LCONTROL,
	"left ctrl":         win.VK_LCONTROL,
	"left control":      win.VK_LCONTROL,
	"rctrl":             win.VK_RCONTROL,
	"right ctrl":        win.VK_RCONTROL,
	"right control":     win.VK_RCONTROL,
	"shift":             win.VK_SHIFT,
	"lshift":            win.VK_LSHIFT,
	"left shift":        win.VK_LSHIFT,
	"rshift":            win.VK_RSHIFT,
	"right shift":       win.VK_RSHIFT,
	"alt":               win.VK_MENU,
	"menu":              win.VK_MENU,
	"lalt":              win.VK_LMENU,
	"left alt":          win.VK_LMENU,
	"ralt":              win.VK_RMENU,
	"right alt":         win.VK_RMENU,
	"win":               win.VK_LWIN,
	"windows":           win.VK_LWIN,
	"command":           win.VK_LWIN,
	"cmd":               win.VK_LWIN,
	"super":             win.VK_LWIN,
	"apps":              win.VK_APPS,
	"enter":             win.VK_RETURN,
	"return":            win.VK_RETURN,
	"space":             win.VK_SPACE,
	"spacebar":          win.VK_SPACE,
	"tab":               win.VK_TAB,
	"backspace":         win.VK_BACK,
	"back":              win.VK_BACK,
	"delete":            win.VK_DELETE,
	"del":               win.VK_DELETE,
	"insert":            win.VK_INSERT,
	"ins":               win.VK_INSERT,
	"home":              win.VK_HOME,
	"end":               win.VK_END,
	"page up":           win.VK_PRIOR,
	"pageup":            win.VK_PRIOR,
	"pgup":              win.VK_PRIOR,
	"page down":         win.VK_NEXT,
	"pagedown":          win.VK_NEXT,
	"pgdn":              win.VK_NEXT,
	"escape":            win.VK_ESCAPE,
	"esc":               win.VK_ESCAPE,
	"caps lock":         win.VK_CAPITAL,
	"capslock":          win.VK_CAPITAL,
	"num lock":          win.VK_NUMLOCK,
	"numlock":           win.VK_NUMLOCK,
	"scroll lock":       win.VK_SCROLL,
	"scrolllock":        win.VK_SCROLL,
	"scroll":            win.VK_SCROLL,
	"print screen":      win.VK_SNAPSHOT,
	"printscreen":       win.VK_SNAPSHOT,
	"prt sc":            win.VK_SNAPSHOT,
	"prtsc":             win.VK_SNAPSHOT,
	"prtscr":            win.VK_SNAPSHOT,
	"pause":             win.VK_PAUSE,
	"break":             win.VK_PAUSE,
	"up":                win.VK_UP,
	"down":              win.VK_DOWN,
	"left":              win.VK_LEFT,
	"right":             win.VK_RIGHT,
	"f1":                win.VK_F1,
	"f2":                win.VK_F2,
	"f3":                win.VK_F3,
	"f4":                win.VK_F4,
	"f5":                win.VK_F5,
	"f6":                win.VK_F6,
	"f7":                win.VK_F7,
	"f8":                win.VK_F8,
	"f9":                win.VK_F9,
	"f10":               win.VK_F10,
	"f11":               win.VK_F11,
	"f12":               win.VK_F12,
	"f13":               win.VK_F13,
	"f14":               win.VK_F14,
	"f15":               win.VK_F15,
	"f16":               win.VK_F16,
	"f17":               win.VK_F17,
	"f18":               win.VK_F18,
	"f19":               win.VK_F19,
	"f20":               win.VK_F20,
	"f21":               win.VK_F21,
	"f22":               win.VK_F22,
	"f23":               win.VK_F23,
	"f24":               win.VK_F24,
	"volume mute":       win.VK_VOLUME_MUTE,
	"volume_mute":       win.VK_VOLUME_MUTE,
	"mute":              win.VK_VOLUME_MUTE,
	"volume down":       win.VK_VOLUME_DOWN,
	"volume_down":       win.VK_VOLUME_DOWN,
	"volume up":         win.VK_VOLUME_UP,
	"volume_up":         win.VK_VOLUME_UP,
	"media next":        win.VK_MEDIA_NEXT_TRACK,
	"medianext":         win.VK_MEDIA_NEXT_TRACK,
	"media previous":    win.VK_MEDIA_PREV_TRACK,
	"mediaprevious":     win.VK_MEDIA_PREV_TRACK,
	"media prev":        win.VK_MEDIA_PREV_TRACK,
	"media back":        win.VK_MEDIA_PREV_TRACK,
	"media stop":        win.VK_MEDIA_STOP,
	"mediastop":         win.VK_MEDIA_STOP,
	"media play":        win.VK_MEDIA_PLAY_PAUSE,
	"mediaplay":         win.VK_MEDIA_PLAY_PAUSE,
	"media play pause":  win.VK_MEDIA_PLAY_PAUSE,
	"mediaplaypause":    win.VK_MEDIA_PLAY_PAUSE,
	"browser back":      win.VK_BROWSER_BACK,
	"browserback":       win.VK_BROWSER_BACK,
	"browser forward":   win.VK_BROWSER_FORWARD,
	"browserforward":    win.VK_BROWSER_FORWARD,
	"browser refresh":   win.VK_BROWSER_REFRESH,
	"browserrefresh":    win.VK_BROWSER_REFRESH,
	"browser stop":      win.VK_BROWSER_STOP,
	"browserstop":       win.VK_BROWSER_STOP,
	"browser search":    win.VK_BROWSER_SEARCH,
	"browsersearch":     win.VK_BROWSER_SEARCH,
	"browser favorites": win.VK_BROWSER_FAVORITES,
	"browserfavorites":  win.VK_BROWSER_FAVORITES,
	"browser home":      win.VK_BROWSER_HOME,
	"browserhome":       win.VK_BROWSER_HOME,
	"numpad0":           win.VK_NUMPAD0,
	"numpad1":           win.VK_NUMPAD1,
	"numpad2":           win.VK_NUMPAD2,
	"numpad3":           win.VK_NUMPAD3,
	"numpad4":           win.VK_NUMPAD4,
	"numpad5":           win.VK_NUMPAD5,
	"numpad6":           win.VK_NUMPAD6,
	"numpad7":           win.VK_NUMPAD7,
	"numpad8":           win.VK_NUMPAD8,
	"numpad9":           win.VK_NUMPAD9,
	"numpad add":        win.VK_ADD,
	"numpad subtract":   win.VK_SUBTRACT,
	"numpad multiply":   win.VK_MULTIPLY,
	"numpad divide":     win.VK_DIVIDE,
	"plus":              win.VK_OEM_PLUS,
	"equal":             win.VK_OEM_PLUS,
	"minus":             win.VK_OEM_MINUS,
	"comma":             win.VK_OEM_COMMA,
	"dot":               win.VK_OEM_PERIOD,
	"period":            win.VK_OEM_PERIOD,
	"slash":             win.VK_OEM_2,
	"question":          win.VK_OEM_2,
	"backslash":         win.VK_OEM_5,
	"pipe":              win.VK_OEM_5,
	"semicolon":         win.VK_OEM_1,
	"colon":             win.VK_OEM_1,
	"quote":             win.VK_OEM_7,
	"apostrophe":        win.VK_OEM_7,
	"bracket left":      win.VK_OEM_4,
	"bracket right":     win.VK_OEM_6,
	"bracketleft":       win.VK_OEM_4,
	"bracketright":      win.VK_OEM_6,
	"grave":             win.VK_OEM_3,
	"tilde":             win.VK_OEM_3,
	"`":                 win.VK_OEM_3,
}

func (m *buttonMap) iterate(f func(int, []buttonCommand)) {
	m.lock.Lock()
	defer m.lock.Unlock()

	for key, value := range m.m {
		f(key, value)
	}
}

func (m *buttonMap) get(key int) ([]buttonCommand, bool) {
	m.lock.Lock()
	defer m.lock.Unlock()

	value, ok := m.m[key]
	return value, ok
}

func (m *buttonMap) set(key int, value []buttonCommand) {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.m[key] = value
}

func (m *buttonMap) String() string {
	m.lock.Lock()
	defer m.lock.Unlock()

	buttonCount := 0
	commandCount := 0

	for _, value := range m.m {
		buttonCount++
		commandCount += len(value)
	}

	return fmt.Sprintf("<%d buttons mapped to %d commands>", buttonCount, commandCount)
}
