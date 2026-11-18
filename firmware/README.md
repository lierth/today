# Today firmware

ESP32 + 4.2" 400×300 e-paper. The firmware is intentionally small: it sleeps,
wakes on a timer or a button, draws the 1-bit frame the server renders, and
queues button actions locally.

The contract is `internal/frame/frame.go` and `docs/PLAN.md` §5.2. Nothing else
is shared between the Go server and this C++ code.

## Hardware

| Signal | ESP32 | Notes |
|---|---|---|
| EPD CS | GPIO5 | |
| EPD DC | GPIO17 | |
| EPD RST | GPIO16 | |
| EPD BUSY | GPIO4 | |
| EPD SCK | GPIO18 | VSPI |
| EPD MOSI | GPIO23 | VSPI |
| Button up | GPIO32 | to 3V3, external 100k to GND, RTC GPIO |
| Button down | GPIO33 | to 3V3, 100k to GND |
| Button OK | GPIO25 | to 3V3, 100k to GND |
| Button refresh | GPIO26 | to 3V3, 100k to GND |
| Battery | GPIO34 | 100k/100k divider, ×2 in software |

Panel: 4.2" 400×300, UC8176 (`GDEW042T2`) by default. If the silkscreen says
SSD1619 (`GYE042A3`), change the panel type in `include/board.h` to
`GxEPD2_420_GYE042A3` and keep the same pins.

Buttons must stay on RTC GPIOs (0, 2, 4, 12–15, 25–27, 32–39) for `ext1` wake.
The reference map uses 25/26/32/33.

**The buttons are active high on purpose.** The classic ESP32 has only two
`ext1` wake modes, `ALL_LOW` and `ANY_HIGH`. With the common "button to GND plus
internal pull-up" wiring, `ALL_LOW` would need all four buttons pressed at once,
so a single press could never wake the board — the device would only ever wake
on its timer. Wiring the buttons to 3V3 with external pull-downs lets
`ANY_HIGH` fire on the first press, and the pull-downs keep the pins at a
defined level while the RTC domain is off.

If you already soldered the active-low wiring, set `board::kButtonsActiveHigh`
to `false` in `include/board.h`. Sleep then falls back to `ext0` on
`board::kWakePin` only: the OK button wakes the device, up/down/refresh work
once it is awake. `ext0` needs `ESP_PD_DOMAIN_RTC_PERIPH` kept on, which costs a
few microamps, so fix the wiring when you can.

## Build and flash

```sh
cd firmware
pio run                 # compile
pio run -t upload       # flash over USB
pio device monitor      # serial, 115200
```

The partition table reserves two 1.7 MB app slots and 64 KB NVS. NVS survives a
reflash, so Wi-Fi credentials, device token, cursor and the cached frame are not
lost when you update the app. OTA is implemented: upload an ESP32 image from
Settings → 固件, the next frame response carries `X-Firmware-Version`, and the
device streams the image into the inactive slot with `Update`, then reboots.
Bump `board::kFirmwareVersion` for every build or the device will keep the
version it already has. The A/B slots keep the previous image for rollback.

PlatformIO pins a pioarduino Arduino-ESP32 3.x build (ESP-IDF 5). With the
Arduino IDE, select "ESP32 Dev Module", the same partition scheme, and install
GxEPD2 and ArduinoJson.

## Provisioning

On a cold boot (USB or reset) the firmware opens a 4-second serial window:

```
wifi <ssid> <password>
server http://192.168.1.10:8787
device dev1
token <token from Settings → 设备>
mode battery|mains
show
reboot
```

Values are stored in NVS. `server` must be reachable from the device; the
default LAN listener is `0.0.0.0:8787`; if the Mac's port was taken and the
app moved to a neighbour, the settings page shows the address to type here.
HTTPS works but uses
`WiFiClientSecure::setInsecure()` because the Funnel certificate is public and
rotates; for a trusted deployment pin the CA instead.

## State machine

```
deep sleep
  ├─ button wake → local cursor / toggle → outbox → sleep ≥60 s (no Wi-Fi)
  └─ timer wake  → Wi-Fi → GET frame (ETag) → draw → POST acks → sleep
```

- `GET /v1/devices/<id>/frame` sends `If-None-Match`; `304` keeps the cached
  bitmap and only refreshes the sleep deadline.
- `X-Next-Wake-At` and `X-Server-Now` are absolute Unix seconds. The firmware
  sleeps `next − now`, so it does not need NTP.
- Button actions append `{op_id,item_id,status}` to a 32-entry NVS outbox; the
  next timer fetch posts them to `/v1/devices/<id>/acks`. The server dedupes by
  `op_id`, so retries are safe.
- OK short press toggles the cursor row; long press (≥0.7 s) forces a full
  refresh and sets a one-shot flag, so the next fetch sends `?force=1` and the
  server answers 200 with a full frame even when the cached `ETag` matches.
- Refresh forces a full panel refresh and the same one-shot `?force=1` fetch.
- Battery millivolts are sent with every acks request and shown in Settings.

## Power notes (read before trusting battery life)

Measure deep-sleep current before optimizing the UI. The plan's order is:

1. Hold SPI/CS levels with `gpio_hold_en()` and isolate button pins with
   `rtc_gpio_isolate()` if they leak.
2. `Serial.end()`, `WiFi.mode(WIFI_OFF)`, `esp_wifi_stop()`.
3. Only then consider dropping the Arduino runtime for plain ESP-IDF.

The reference code calls `display.hibernate()`, turns Wi-Fi off and sleeps, but
does not yet hold the SPI pins. Add that after measuring the board.

The ext1 wake mask is armed `ANY_HIGH` over all four buttons, so the pull-downs
draw no current at rest and any single press wakes the board. If you keep the
active-low wiring, `kButtonsActiveHigh = false` switches to `ext0` on one pin and
keeps `ESP_PD_DOMAIN_RTC_PERIPH` powered — that domain stays on for the whole
sleep, so expect a few extra microamps.

`mode mains` keeps Wi-Fi up and pulls every 60 s instead of sleeping; use it
when the board runs from USB or when deep-sleep current cannot be brought
below the target. `mode battery` is the default deep-sleep state machine.

## Current limits

- Local toggle draws a strike-through but cannot erase a server-rendered strike
  until the next fetched frame reflects the ack.
- Cursor movement uses `displayWindow()` per 24 px row. If the panel/controller
  loses RAM across deep sleep on your board, set the button path to redraw the
  full frame after `InitDisplay()`.
- A cold boot repaints the cached NVS frame before the first fetch; after a
  deliberate power cycle the screen should never stay blank on a 304.
- HTTPS certificate validation is disabled (see above).
- No config portal; provisioning is over USB serial.
- OTA trusts the uploaded image as far as `Update` validates it; the upload is
  protected by the web session, the download by the device token.
