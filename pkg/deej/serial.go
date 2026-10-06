package deej

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jacobsa/go-serial/serial"
	"go.uber.org/zap"

	"github.com/omriharel/deej/pkg/deej/util"
)

const (
	// silenceTimeout is how long we're willing to go without receiving a single
	// line from the MCU before considering the connection dead.
	// the firmware emits a line every ~10ms, so 2 seconds of complete silence
	// can only mean the device was unplugged / reset / crashed.
	// this is needed because on windows, a blocking read on a yanked USB serial
	// device hangs indefinitely instead of returning an error
	silenceTimeout = 2 * time.Second

	// watchdogInterval is how often we check whether the connection went silent
	watchdogInterval = 500 * time.Millisecond

	// connectGracePeriod is how long after a fresh connection we wait before
	// the silence watchdog is allowed to fire. USB MCUs (Arduino auto-reset,
	// ESP32-S3 CDC re-enumeration) often need a few seconds after the port is
	// opened before they start streaming lines. Without this grace period the
	// reconnect loop opens the port (resetting the MCU), kills the connection
	// after 2s of silence, reopens it (resetting the MCU again), and never
	// lets the device stabilize - an endless find-device loop that only a
	// physical re-plug breaks.
	connectGracePeriod = 5 * time.Second

	// wakeGapThreshold is how far apart two watchdog ticks may be before we
	// assume the PC just woke from sleep/hibernation. time.Sleep-based tickers
	// don't fire while suspended, so a large gap is a reliable, message-loop
	// free wake detector on every platform.
	wakeGapThreshold = 5 * time.Second

	// wakeSettleDelay is how long we pause after a sleep/wake cycle before
	// letting the reconnect loop open the port again, giving Windows time to
	// re-enumerate the USB CDC device and release the stale handle. Opening
	// the port too eagerly after resume yields "access denied" / missing-port
	// errors in a tight loop.
	wakeSettleDelay = 3 * time.Second

	// stopTimeout is how long Stop() waits for the port to be released
	stopTimeout = 2 * time.Second
)

// errDeviceSilent is returned when we stop reading due to a communication timeout
var errDeviceSilent = errors.New("serial: no data received from device, assuming disconnected")

// serialLine is a single result of a read attempt - either a line or a terminal error
type serialLine struct {
	line string
	err  error
}

// SerialIO provides a deej-aware abstraction layer to managing serial I/O
type SerialIO struct {
	comPort  string
	baudRate uint

	deej   *Deej
	logger *zap.SugaredLogger

	stopChannel chan bool
	disconnectChannel chan struct{}
	connected   bool
	connOptions serial.OpenOptions
	conn        io.ReadWriteCloser
	connRetryInterval time.Duration

	// connMu guards connected, conn, lastReadTime, connectTime and closedChannel, which are
	// written by the read loop goroutine and read by other goroutines (the
	// reconnect loop in deej.go, Stop(), SendCommand() and the extra.go writers)
	connMu sync.RWMutex

	// closedChannel is closed by the read loop once it has fully torn down the
	// current connection, so Stop() can wait for the port to actually be released
	closedChannel chan struct{}

	lastReadTime time.Time

	// connectTime is when the current connection was established. The silence
	// watchdog ignores quiet periods younger than connectGracePeriod so the
	// MCU has time to boot/stream after the port open (which itself resets
	// most Arduino-style boards).
	connectTime time.Time

	// wakeMu guards wakeSettleUntil, which marks how long after a detected
	// sleep/wake cycle the reconnect loop should hold off opening the port
	wakeMu           sync.Mutex
	wakeSettleUntil  time.Time

	lastKnownNumSliders        int
	currentSliderPercentValues []float32

	sliderMoveConsumers []chan SliderMoveEvent
}

// SliderMoveEvent represents a single slider move captured by deej
type SliderMoveEvent struct {
	SliderID     int
	PercentValue float32
}

var expectedLinePattern = regexp.MustCompile(`^\d{1,4}(\|\d{1,4})*\r\n$`)

// NewSerialIO creates a SerialIO instance that uses the provided deej
// instance's connection info to establish communications with the arduino chip
func NewSerialIO(deej *Deej, logger *zap.SugaredLogger) (*SerialIO, error) {
	logger = logger.Named("serial")

	sio := &SerialIO{
		deej:                deej,
		logger:              logger,
		stopChannel:         make(chan bool, 1),
		disconnectChannel:   make(chan struct{}, 1),
		connected:           false,
		conn:                nil,

		// set here as well as in Start(), so it's readable before the first connection attempt
		connRetryInterval:   deej.config.ReconnectInterval,

		sliderMoveConsumers: []chan SliderMoveEvent{},
	}

	logger.Debug("Created serial i/o instance")

	// respond to config changes
	sio.setupOnConfigReload()

	// wake detection while idle (no active watchdog), so reconnects after
	// sleep back off instead of hammering a half-enumerated USB port
	go sio.watchForWakeWhileIdle()

	return sio, nil
}

// IsConnected returns whether we currently have an active serial connection
func (sio *SerialIO) IsConnected() bool {
	sio.connMu.RLock()
	defer sio.connMu.RUnlock()
	return sio.connected
}

// SubscribeToDisconnectEvents returns a buffered channel which receives a single
// value whenever an established connection is lost (either due to a read error
// or a communication timeout). It is safe to call while disconnected.
func (sio *SerialIO) SubscribeToDisconnectEvents() <-chan struct{} {
	return sio.disconnectChannel
}

// signalDisconnected notifies any subscriber (e.g. the reconnect loop in deej.go)
// that the connection went away, without ever blocking
func (sio *SerialIO) signalDisconnected() {
	select {
	case sio.disconnectChannel <- struct{}{}:
	default:
		// a disconnect is already pending, no need to queue another one
	}
}

// Start attempts to connect to our arduino chip
func (sio *SerialIO) Start() error {

	// don't allow multiple concurrent connections
	if sio.IsConnected() {
		sio.logger.Warn("Already connected, can't start another without closing first")
		return errors.New("serial: connection already active")
	}

	// set minimum read size according to platform (0 for windows, 1 for linux)
	// this prevents a rare bug on windows where serial reads get congested,
	// resulting in significant lag
	minimumReadSize := 0
	if util.Linux() {
		minimumReadSize = 1
	}
	sio.connRetryInterval = sio.deej.config.ReconnectInterval

	sio.connOptions = serial.OpenOptions{
		PortName:        sio.deej.config.ConnectionInfo.COMPort,
		BaudRate:        uint(sio.deej.config.ConnectionInfo.BaudRate),
		DataBits:        8,
		StopBits:        1,
		MinimumReadSize: uint(minimumReadSize),
	}

	sio.logger.Debugw("Attempting serial connection",
		"comPort", sio.connOptions.PortName,
		"baudRate", sio.connOptions.BaudRate,
		"minReadSize", minimumReadSize)

	var err error
	conn, err := serial.Open(sio.connOptions)
	if err != nil {

		// might need a user notification here, TBD
		sio.logger.Warnw("Failed to open serial connection", "error", err)
		return fmt.Errorf("open serial connection: %w", err)
	}

	namedLogger := sio.logger.Named(strings.ToLower(sio.connOptions.PortName))

	namedLogger.Infow("Connected", "conn", conn)

	sio.connMu.Lock()
	sio.conn = conn
	sio.connected = true
	sio.lastReadTime = time.Now()
	sio.connectTime = time.Now()
	closedChannel := make(chan struct{})
	sio.closedChannel = closedChannel
	sio.connMu.Unlock()

	// drain any stop signal left over from a previous connection, so the new
	// read loop doesn't immediately tear itself down
	select {
	case <-sio.stopChannel:
	default:
	}

	// read lines or await a stop
	go func() {
		defer func() {
			// if this goroutine dies for any reason, make sure we never stay
			// "connected" while nothing is actually reading from the port
			sio.handleDisconnect(namedLogger, nil)

			// let anyone waiting in Stop() know the port has been released.
			// use our own captured channel - sio.closedChannel may already point
			// at a newer connection by now
			close(closedChannel)
		}()

		connReader := bufio.NewReader(conn)
		lineChannel := sio.readLine(namedLogger, connReader)

		// watchdog: on windows, unplugging the device makes the blocking read hang
		// forever instead of erroring out, so we track time since the last line
		// ourselves and treat prolonged silence as a disconnect.
		// the tick-gap check doubles as a sleep/wake detector: ticks don't fire
		// while the PC is suspended, so a large gap means we just resumed with
		// a stale USB handle that must be torn down and re-opened after a
		// short settle delay (see WakeDetected / WaitForWakeSettle).
		watchdog := time.NewTicker(watchdogInterval)
		defer watchdog.Stop()

		lastTick := time.Now()

		for {
			select {
			case <-sio.stopChannel:
				sio.close(namedLogger)
				return
			case res := <-lineChannel:
				if res.err != nil {
					sio.handleDisconnect(namedLogger, res.err)
					return
				}

				sio.connMu.Lock()
				sio.lastReadTime = time.Now()
				sio.connMu.Unlock()

				sio.handleCommand(namedLogger, res.line)
			case now := <-watchdog.C:
				if now.Sub(lastTick) > wakeGapThreshold {
					namedLogger.Infow("Detected system wake from sleep, recycling serial connection",
						"gap", now.Sub(lastTick).String())
					sio.WakeDetected()
					sio.handleDisconnect(namedLogger, errors.New("serial: system woke from sleep, recycling connection"))
					return
				}
				lastTick = now

				sio.connMu.RLock()
				last := sio.lastReadTime
				sinceConnect := now.Sub(sio.connectTime)
				sio.connMu.RUnlock()

				// give a freshly opened port time to start streaming before
				// treating silence as a dead device. opening the port resets
				// most MCUs, and killing the connection during that boot loop
				// is exactly what caused the endless post-sleep retry loop.
				if sinceConnect < connectGracePeriod {
					continue
				}

				if time.Since(last) > silenceTimeout {
					namedLogger.Warnw("Serial connection went silent, assuming device was disconnected",
						"silenceTimeout", silenceTimeout.String())

					sio.handleDisconnect(namedLogger, errDeviceSilent)
					return
				}
			}
		}
	}()

	//periodically send the active application name and volume level if it has changed, and pc statistics

	return nil
}

// Stop signals us to shut down our serial connection, if one is active.
// It blocks until the port has actually been released (or the timeout expires),
// so a caller can safely Start() a new connection right after this returns.
func (sio *SerialIO) Stop() {
	sio.connMu.RLock()
	connected := sio.connected
	closed := sio.closedChannel
	sio.connMu.RUnlock()

	if !connected {
		sio.logger.Debug("Not currently connected, nothing to stop")
		return
	}

	sio.logger.Debug("Shutting down serial connection")

	// non-blocking send: the read loop may already be gone (e.g. after a disconnect),
	// and we must never deadlock a caller such as the firmware flash flow
	select {
	case sio.stopChannel <- true:
	default:
		sio.logger.Debug("Serial stop already signaled")
	}

	if closed == nil {
		return
	}

	// wait for the read loop to release the port. on windows the port must be fully
	// closed before it can be reopened, so this matters for reconnects
	select {
	case <-closed:
	case <-time.After(stopTimeout):
		sio.logger.Warn("Timed out waiting for serial connection to close")
	}
}

// SendCommand sends a newline-terminated control command to the connected MCU.
// Firmware updates use this to ask the normal application firmware to enter DFU
// boot mode before this process releases the serial port.
func (sio *SerialIO) SendCommand(command string) error {
	sio.connMu.RLock()
	connected := sio.connected
	conn := sio.conn
	sio.connMu.RUnlock()

	if !connected || conn == nil {
		return errors.New("serial: no active connection")
	}

	message := []byte(strings.TrimSpace(command) + "\n")
	if _, err := conn.Write(message); err != nil {
		return fmt.Errorf("write command: %w", err)
	}

	return nil
}

// SubscribeToSliderMoveEvents returns an unbuffered channel that receives
// a sliderMoveEvent struct every time a slider moves
func (sio *SerialIO) SubscribeToSliderMoveEvents() chan SliderMoveEvent {
	ch := make(chan SliderMoveEvent)
	sio.sliderMoveConsumers = append(sio.sliderMoveConsumers, ch)

	return ch
}

func (sio *SerialIO) handleCommand(logger *zap.SugaredLogger, line string) {
	splitLine := strings.Split(line, ":")

	if len(splitLine) != 2 {
		logger.Error("Invalid Line of length: " + strconv.Itoa(len(splitLine)) + ", expected 2")
		return
	}

	command := splitLine[0]
	line = splitLine[1]

	switch command {
	case "F": //fader command
		sio.handleLine(logger, line)
	case "B": //button command
		handleButtonLine(sio.deej, line)
	case "D": //destination command, sent when arrived at target after moving
		sio.deej.sessions.unlockCurrentSliders()
	case "M": //mute command
	default:
		logger.Error("unknown command recieved")
		return
	}

}

func (sio *SerialIO) setupOnConfigReload() {
	configReloadedChannel := sio.deej.config.SubscribeToChanges()

	const stopDelay = 50 * time.Millisecond

	go func() {
		for {
			select {
			case <-configReloadedChannel:

				// make any config reload unset our slider number to ensure process volumes are being re-set
				// (the next read line will emit SliderMoveEvent instances for all sliders)\
				// this needs to happen after a small delay, because the session map will also re-acquire sessions
				// whenever the config file is reloaded, and we don't want it to receive these move events while the map
				// is still cleared. this is kind of ugly, but shouldn't cause any issues
				go func() {
					<-time.After(stopDelay)
					sio.lastKnownNumSliders = 0
				}()

				// if connection params have changed, attempt to stop and start the connection
				if sio.deej.config.ConnectionInfo.COMPort != sio.connOptions.PortName ||
					uint(sio.deej.config.ConnectionInfo.BaudRate) != sio.connOptions.BaudRate {

					sio.logger.Info("Detected change in connection parameters, attempting to renew connection")
					sio.Stop()

					// let the connection close
					<-time.After(stopDelay)

					if err := sio.Start(); err != nil {
						sio.logger.Warnw("Failed to renew connection after parameter change", "error", err)
					} else {
						sio.logger.Debug("Renewed connection successfully")
					}
				}
			}
		}
	}()
}

// WakeDetected marks the start of a post-sleep settle window during which the
// reconnect loop should not open the serial port, giving Windows time to
// re-enumerate the USB CDC device. It is called by the read-loop watchdog when
// it observes a tick gap, and by the background wake watcher when idle.
func (sio *SerialIO) WakeDetected() {
	sio.wakeMu.Lock()
	sio.wakeSettleUntil = time.Now().Add(wakeSettleDelay)
	sio.wakeMu.Unlock()
}

// WaitForWakeSettle blocks until any post-sleep settle window has elapsed, or
// done is closed. It returns false if done was closed (caller should exit).
func (sio *SerialIO) WaitForWakeSettle(done <-chan struct{}) bool {
	for {
		sio.wakeMu.Lock()
		until := sio.wakeSettleUntil
		sio.wakeMu.Unlock()

		wait := time.Until(until)
		if wait <= 0 {
			return true
		}

		sio.logger.Infow("Waiting for USB device to settle after system wake",
			"wait", wait.String())

		select {
		case <-time.After(wait):
		case <-done:
			return false
		}
	}
}

// watchForWakeWhileIdle detects sleep/wake cycles while no connection (and
// therefore no watchdog) is active, so the reconnect loop still backs off
// after resume instead of hammering a half-enumerated port.
func (sio *SerialIO) watchForWakeWhileIdle() {
	ticker := time.NewTicker(watchdogInterval)
	defer ticker.Stop()

	lastTick := time.Now()

	for range ticker.C {
		now := time.Now()
		if now.Sub(lastTick) > wakeGapThreshold {
			sio.logger.Infow("Detected system wake from sleep while disconnected, delaying reconnect",
				"gap", now.Sub(lastTick).String())
			sio.WakeDetected()
		}
		lastTick = now
	}
}

// handleDisconnect tears down the active connection and notifies any subscriber
// (e.g. the reconnect loop in deej.run) that we lost it, so a reconnect can be
// initiated. It is safe to call multiple times and is a no-op if already
// disconnected - this is what the read loop's deferred call relies on.
func (sio *SerialIO) handleDisconnect(logger *zap.SugaredLogger, cause error) {
	sio.connMu.Lock()
	if !sio.connected {
		sio.connMu.Unlock()
		return
	}

	conn := sio.conn
	sio.conn = nil
	sio.connected = false
	sio.connMu.Unlock()

	if conn != nil {
		if err := conn.Close(); err != nil {
			logger.Warnw("Failed to close serial connection", "error", err)
		} else {
			logger.Debug("Serial connection closed")
		}
	}

	// force a full re-detection of sliders when we come back
	sio.lastKnownNumSliders = 0

	if cause != nil {
		logger.Warnw("Lost serial connection", "error", cause)
	}

	sio.signalDisconnected()
}

func (sio *SerialIO) close(logger *zap.SugaredLogger) {
	sio.connMu.Lock()
	conn := sio.conn
	sio.conn = nil
	sio.connected = false
	sio.connMu.Unlock()

	if conn == nil {
		return
	}

	if err := conn.Close(); err != nil {
		logger.Warnw("Failed to close serial connection", "error", err)
	} else {
		logger.Debug("Serial connection closed")
	}

	sio.lastKnownNumSliders = 0
}

func (sio *SerialIO) readLine(logger *zap.SugaredLogger, reader *bufio.Reader) chan serialLine {
	ch := make(chan serialLine)

	go func() {
		// always deliver a terminal error, so the read loop can react to it
		// instead of waiting forever on a channel that will never produce
		defer close(ch)

		for {
			line, err := reader.ReadString('\n')
			if err != nil {

				if sio.deej.Verbose() {
					logger.Warnw("Failed to read line from serial", "error", err, "line", line)
				}

				ch <- serialLine{err: err}
				return
			}

			if sio.deej.Verbose() {
				logger.Debugw("Read new line", "line", line)
			}

			// deliver the line to the channel
			ch <- serialLine{line: line}
		}
	}()

	return ch
}

func (sio *SerialIO) handleLine(logger *zap.SugaredLogger, line string) {

	// this function receives an unsanitized line which is guaranteed to end with LF,
	// but most lines will end with CRLF. it may also have garbage instead of
	// deej-formatted values, so we must check for that! just ignore bad ones
	if !expectedLinePattern.MatchString(line) {
		logger.Debug("string not matched with value of", line, "  and expected to be", expectedLinePattern.String())

		return

	}

	// trim the suffix
	line = strings.TrimSuffix(line, "\r\n")

	// split on pipe (|), this gives a slice of numerical strings between "0" and "1023"
	splitLine := strings.Split(line, "|")
	numSliders := len(splitLine)

	// update our slider count, if needed - this will send slider move events for all
	if numSliders != sio.lastKnownNumSliders {
		logger.Infow("Detected sliders", "amount", numSliders)
		sio.lastKnownNumSliders = numSliders
		sio.currentSliderPercentValues = make([]float32, numSliders)

		// reset everything to be an impossible value to force the slider move event later
		for idx := range sio.currentSliderPercentValues {
			sio.currentSliderPercentValues[idx] = -1.0
		}
	}

	// for each slider:
	moveEvents := []SliderMoveEvent{}
	for sliderIdx, stringValue := range splitLine {

		// convert string values to integers ("1023" -> 1023)
		number, _ := strconv.Atoi(stringValue)

		// turns out the first line could come out dirty sometimes (i.e. "4558|925|41|643|220")
		// so let's check the first number for correctness just in case
		if sliderIdx == 0 && number > 1023 {
			sio.logger.Debugw("Got malformed line from serial, ignoring", "line", line)
			return
		}

		// map the value from raw to a "dirty" float between 0 and 1 (e.g. 0.15451...)
		dirtyFloat := float32(number) / 1023.0

		// normalize it to an actual volume scalar between 0.0 and 1.0 with 2 points of precision
		normalizedScalar := util.NormalizeScalar(dirtyFloat)

		// if sliders are inverted, take the complement of 1.0
		if sio.deej.config.InvertSliders {
			normalizedScalar = 1 - normalizedScalar
		}

		// check if it changes the desired state (could just be a jumpy raw slider value)
		if util.SignificantlyDifferent(sio.currentSliderPercentValues[sliderIdx], normalizedScalar, sio.deej.config.NoiseReductionLevel) {

			// if it does, update the saved value and create a move event
			sio.currentSliderPercentValues[sliderIdx] = normalizedScalar

			moveEvents = append(moveEvents, SliderMoveEvent{
				SliderID:     sliderIdx,
				PercentValue: normalizedScalar,
			})

			if sio.deej.Verbose() {
				logger.Debugw("Slider moved", "event", moveEvents[len(moveEvents)-1])
			}
		}
	}

	// deliver move events if there are any, towards all potential consumers
	if len(moveEvents) > 0 {
		for _, consumer := range sio.sliderMoveConsumers {
			for _, moveEvent := range moveEvents {
				consumer <- moveEvent
			}
		}
	}
}
