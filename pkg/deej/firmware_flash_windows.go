//go:build windows
// +build windows

package deej

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/lxn/win"
)

const (
	esptoolUtilityName   = "esptool.exe"
	esp32S3Chip          = "esp32s3"
	firmwareFlashAddress = "0x10000"
	firmwareFlashBaud    = "460800"
	bootloaderCommand    = "bootloader"
	bootloaderSettleTime = 5000 * time.Millisecond
)

// startFirmwareFlash shows the image picker on the tray event goroutine, then
// performs the potentially long-running USB transfer in the background.
func (d *Deej) startFirmwareFlash() {
	imagePath, err := selectFirmwareImage()
	if err != nil {
		d.logger.Warnw("Unable to select firmware image", "error", err)
		d.notifier.Notify("Firmware update", "Couldn't open the firmware image picker. See the log for details.")
		return
	}
	if imagePath == "" { // user cancelled
		return
	}

	if win.MessageBox(0,
		utf16Ptr("Flash the selected image to the connected ESP32-S3?\r\n\r\n"+imagePath+"\r\n\r\nDeej will stop controlling sliders until flashing finishes."),
		utf16Ptr("Flash MCU firmware"), win.MB_YESNO|win.MB_ICONWARNING|win.MB_SETFOREGROUND) != win.IDYES {
		return
	}

	d.firmwareFlashMu.Lock()
	if d.firmwareFlashActive {
		d.firmwareFlashMu.Unlock()
		d.notifier.Notify("Firmware update already running", "Wait for the current flash operation to finish.")
		return
	}
	d.firmwareFlashActive = true
	d.firmwareFlashMu.Unlock()

	go func() {
		defer d.finishFirmwareFlash()
		d.flashFirmware(imagePath)
	}()
}

func (d *Deej) finishFirmwareFlash() {
	d.firmwareFlashMu.Lock()
	d.firmwareFlashActive = false
	d.firmwareFlashMu.Unlock()
}

func (d *Deej) flashFirmware(imagePath string) {
	esptoolPath, err := bundledEsptoolPath()
	if err != nil {
		d.logger.Errorw("Bundled esptool.exe is unavailable", "error", err)
		d.notifier.Notify("Firmware update unavailable", err.Error())
		return
	}

	if err := d.serial.SendCommand(bootloaderCommand); err != nil {
		d.logger.Warnw("Unable to send bootloader command", "error", err)
		d.notifier.Notify("Firmware update not started", "Could not tell the MCU to enter serial bootloader mode. Ensure it is connected and try again.")
		return
	}

	// esptool.exe must own the serial port. Release deej's COM connection only after
	// the command has been written, then allow the ESP32-S3 to enter bootloader mode.
	time.Sleep(100 * time.Millisecond)
	d.serial.Stop()
	time.Sleep(bootloaderSettleTime)

	d.notifier.Notify("Flashing firmware", "The ESP32-S3 is being updated. Do not disconnect it.")
	output, err := exec.Command(esptoolPath,
		"--chip", esp32S3Chip,
		//"--port", d.serial.connOptions.PortName,
		"--before", "no_reset",
		"--after", "hard_reset",
		"--baud", firmwareFlashBaud,
		"write_flash", "-z", firmwareFlashAddress, imagePath,
	).CombinedOutput()
	if err != nil {
		d.logger.Errorw("ESP32-S3 firmware flash failed", "error", err, "output", string(output))
		showFlashFailure(err, output)
		return
	}

	d.logger.Infow("ESP32-S3 firmware flash completed", "image", imagePath, "port", d.serial.connOptions.PortName, "output", string(output))
	d.notifier.Notify("Firmware update complete", "The ESP32-S3 firmware was flashed successfully. Restart deej if it does not reconnect automatically.")
}

func selectFirmwareImage() (string, error) {
	fileBuffer := make([]uint16, 32768)
	filter := utf16MultiSz("ESP32-S3 firmware images (*.bin)\x00*.bin\x00All files (*.*)\x00*.*\x00\x00")
	title := utf16Ptr("Select ESP32-S3 firmware image")

	ofn := win.OPENFILENAME{
		LStructSize: uint32(unsafe.Sizeof(win.OPENFILENAME{})),
		LpstrFilter: filter,
		LpstrFile:   &fileBuffer[0],
		NMaxFile:    uint32(len(fileBuffer)),
		LpstrTitle:  title,
		Flags:       win.OFN_FILEMUSTEXIST | win.OFN_PATHMUSTEXIST | win.OFN_HIDEREADONLY | win.OFN_EXPLORER,
	}
	if !win.GetOpenFileName(&ofn) {
		if dialogErr := win.CommDlgExtendedError(); dialogErr != 0 {
			return "", fmt.Errorf("open firmware image dialog: 0x%X", dialogErr)
		}
		return "", nil
	}

	return stringFromUTF16(fileBuffer), nil
}

func bundledEsptoolPath() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate deej executable: %w", err)
	}

	path := filepath.Join(filepath.Dir(executable), esptoolUtilityName)
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("bundled %s is missing next to deej.exe", esptoolUtilityName)
	}
	if info.IsDir() {
		return "", errors.New("bundled esptool.exe is a directory")
	}

	return path, nil
}

func showFlashFailure(flashErr error, output []byte) {
	details := strings.TrimSpace(string(output))
	if len(details) > 1400 {
		details = details[len(details)-1400:]
	}
	if details == "" {
		details = flashErr.Error()
	}

	win.MessageBox(0,
		utf16Ptr("Flashing failed. Make sure the ESP32-S3 is in serial bootloader mode and its COM port is available. Restart deej before retrying.\r\n\r\n"+details),
		utf16Ptr("Firmware update failed"), win.MB_OK|win.MB_ICONERROR|win.MB_SETFOREGROUND)
}

func utf16Ptr(value string) *uint16 {
	value = strings.ReplaceAll(value, "\x00", "")
	encoded, _ := syscall.UTF16PtrFromString(value)
	return encoded
}

// utf16MultiSz creates the double-NUL-terminated string required by the
// Windows common-dialog filter field. Unlike utf16Ptr, its embedded NULs are
// meaningful and must be preserved.
func utf16MultiSz(value string) *uint16 {
	encoded := append(utf16.Encode([]rune(value)), 0)
	return &encoded[0]
}

func stringFromUTF16(value []uint16) string {
	for index, character := range value {
		if character == 0 {
			return string(utf16.Decode(value[:index]))
		}
	}
	return string(utf16.Decode(value))
}
