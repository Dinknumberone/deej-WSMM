//go:build !windows
// +build !windows

package deej

func (d *Deej) startFirmwareFlash() {
	d.logger.Warn("Firmware flashing from the tray is currently supported on Windows only")
	d.notifier.Notify("Firmware update unavailable", "Firmware flashing from the tray is currently supported on Windows only.")
}
