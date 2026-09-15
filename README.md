## Deej-WSMM (Watermelon skip max mixer)

Deej-WSMM is an exspansion of the Deej source code for a couple of main purposes:

1. Adding functionality to motorized faders with the "active application" setting
2. Adding support for buttons, and some custom macros
3. Computer Diagnostics sent to Microcontroller


## Motorized Fader (mFader)

The mFader, intended to be set to the "active application" setting in Deej, will move whenever the active application changes, to be at the volume level of that application.
Blacklists may be set manually, but will also automatically have any other applications set on other sliders blacklisted. when a non valid application is the currently active one, the current slider will instead change the volume of the last application that was valid.
The current/last active application is also sent to the Microcontroller, intended to be displayed on a screen.

## Buttons and Switches

The functionality of the Serial line is rewritten to allow for different commands to be sent over the serial line, instead of always being a list. This allows individual button presses to be sent. Button outputs can be set in the config, with the following options:

#### Basic (implemented)
1. Single keys (e.g. `space`, `f13`, `media play`)
2. Key combos (e.g. `ctrl + h`, `shift + f5`)

#### Special (implemented)
1. Default playback device switching:
   - Momentary button: send only the index (toggles between the two devices)
   - Switch/toggle: send `index,state` where state `0` selects the first device and `1` selects the second
   - Config format:
     - `deej.audio_device_switch`, then 2 lines with device friendly names

#### Special (not implemented yet)
1. (reserved)
2. Webhook, a webhook can be defined, and it will be called on press


## Computer Diagnostics
(not implemented yet)

Sends CPU, Memory, and GPU statistics to the Microcontroller, intended to be used in a display
