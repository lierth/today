# Today firmware

ESP32 + 4.2" 400×300 e-paper. The firmware is intentionally small: it sleeps,
wakes on a timer or a button, draws the 1-bit frame the server renders, and
queues button actions locally.

The contract is `internal/frame/frame.go` and `docs/PLAN.md` §5.2. Nothing else
is shared between the Go server and this C++ code.

## Hardware

Target board: **ESP32-S3-DevKitC-1 N16R8** (16 MB flash, 8 MB octal PSRAM).
The board enumerates as `USB JTAG/serial debug unit` (VID 0x303A, PID 0x1001)
over its built-in USB-Serial/JTAG port, so no CP210x/CH340 driver is needed and
a plain USB-C data cable is enough; a charge-only cable shows no `/dev/cu.*`.

| Signal | ESP32-S3 | Notes |
|---|---|---|
| EPD CS | GPIO10 | |
| EPD DC | GPIO17 | |
| EPD RST | GPIO16 | |
| EPD BUSY | GPIO4 | |
| EPD SCK | GPIO12 | default SPI SCK of the DevKitC-1 |
| EPD MOSI | GPIO11 | default SPI MOSI of the DevKitC-1 |
| Button up | GPIO6 | to GND, internal pull-up, RTC GPIO |
| Button down | GPIO7 | to GND, internal pull-up |
| Button OK | GPIO15 | to GND, internal pull-up |
| Button refresh | GPIO21 | to GND, internal pull-up |
| Battery | GPIO1 | ADC1 channel 0, 100k/100k divider, ×2 in software |

Panel: 4.2" 400×300, UC8176 (`GDEW042T2`) by default. If the silkscreen says
SSD1619 (`GYE042A3`), change the panel type in `include/board.h` to
`GxEPD2_420_GYE042A3` and keep the same pins.

Pins are not free: the S3 package has **no GPIO 22–25**, deep-sleep wake only
works on RTC GPIO 0–21, and ADC1 is GPIO 1–10. That rules out the classic-ESP32
map (MOSI 23, OK 25, battery 34). Buttons must stay within 0–21 for `ext1`.

**The buttons are active low**, which is fine on this chip: unlike the classic
ESP32, the S3 offers `ESP_EXT1_WAKEUP_ANY_LOW`, so "button to GND plus internal
pull-up" wakes on the first press. `DeepSleep()` keeps
`ESP_PD_DOMAIN_RTC_PERIPH` powered so those pull-ups survive sleep; measure the
standby current before turning that off, and fit 100k pull-ups to 3V3 if it
matters.

## Build and flash

```sh
cd firmware
pio run                 # compile
pio run -t upload       # flash over USB
pio device monitor      # serial, 115200 over USB CDC
```

`Serial` is the USB CDC (`-DARDUINO_USB_CDC_ON_BOOT=1 -DARDUINO_USB_MODE=1` in
`platformio.ini`), because the board has no UART bridge chip. The port is
`/dev/cu.usbmodem*`; if `pio run -t upload` cannot open it, hold BOOT while
tapping RESET and retry, or call esptool with a lower `-b 460800`.

The partition table reserves two 1.7 MB app slots and 64 KB NVS. NVS survives a
reflash, so Wi-Fi credentials, device token, cursor and the cached frame are not
lost when you update the app. OTA is implemented: upload an ESP32 image from
Settings → 固件, the next frame response carries `X-Firmware-Version`, and the
device streams the image into the inactive slot with `Update`, then reboots.
Bump `board::kFirmwareVersion` for every build or the device will keep the
version it already has. The A/B slots keep the previous image for rollback.

PlatformIO pins a pioarduino Arduino-ESP32 3.x build (ESP-IDF 5). With the
Arduino IDE, select "ESP32S3 Dev Module", 16 MB flash, OPI PSRAM, the same
partition scheme, and install GxEPD2 and ArduinoJson.

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
