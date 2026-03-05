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

The functionality off the Serial line is rewritten to allow for different commands to be sent of the serial line, instead of always being a list. this allows individual buttons presses to be sent. Buttons outputs can be set in the config, with the following options:

##### basic
(not implemented yet, but planned)
1. functions keys, f1-24
2. simple macros: ctrl+key, shift+key
3. media keys

##### Special

(not implemented yet)
1. Audio device switching, intended for a switch instead of button, 2 audio devices can be defined and the switch/button will swap between them on states
2. Webhook, a webhook can be defined, and it will be called on press


## Computer Diagnostics
(not implemented yet)

Sends CPU, Memory, and GPU statistics to the Microcontroller, intended to be used in a display
