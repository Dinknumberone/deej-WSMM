package main

import (
	"flag"
	"fmt"

	"github.com/omriharel/deej/pkg/deej"
	"github.com/omriharel/deej/pkg/deej/util"
)

var (
	gitCommit  string
	versionTag string
	buildType  string

	verbose bool
)

func init() {
	flag.BoolVar(&verbose, "verbose", false, "show verbose logs (useful for debugging serial)")
	flag.BoolVar(&verbose, "v", false, "shorthand for --verbose")
	flag.Parse()
}

func main() {

	// first we need a logger
	logger, err := deej.NewLogger(buildType)
	if err != nil {
		panic(fmt.Sprintf("Failed to create logger: %v", err))
	}

	named := logger.Named("main")
	named.Debug("Created logger")

	named.Infow("Version info",
		"gitCommit", gitCommit,
		"versionTag", versionTag,
		"buildType", buildType)

	// provide a fair warning if the user's running in verbose mode
	if verbose {
		named.Debug("Verbose flag provided, all log messages will be shown")
	}

	// only one deej instance may run at a time - two would fight over the serial
	// port, the tray icon and the config file. now that a busy serial port no
	// longer causes an immediate exit, this is the only thing stopping a
	// double-launched second instance from sitting in a silent retry loop
	if otherPID, err := util.AlreadyRunningInstancePID(); err != nil {
		// never block startup on this check - it's a convenience, not a guarantee
		named.Warnw("Failed to check for other running deej instances", "error", err)
	} else if otherPID != 0 {
		named.Warnw("Another deej instance is already running, exiting",
			"otherPID", otherPID)

		notifier, err := deej.NewToastNotifier(logger)
		if err != nil {
			named.Warnw("Failed to create notifier", "error", err)
		} else {
			notifier.Notify("Deej is already running",
				"Only one deej instance can run at a time. Close the other one before starting a new one.")
		}

		return
	}

	// create the deej instance
	d, err := deej.NewDeej(logger, verbose)
	if err != nil {
		named.Fatalw("Failed to create deej object", "error", err)
	}

	// mark this as a dev or release build to gate dev-only features
	d.SetDevBuild(deej.IsDevBuild(buildType))

	// if injected by build process, set version info to show up in the tray
	if buildType != "" && (versionTag != "" || gitCommit != "") {
		identifier := gitCommit
		if versionTag != "" {
			identifier = versionTag
		}

		versionString := fmt.Sprintf("Version %s-%s", buildType, identifier)
		d.SetVersion(versionString)
	}

	// onwards, to glory
	if err = d.Initialize(); err != nil {
		named.Fatalw("Failed to initialize deej", "error", err)
	}
}
