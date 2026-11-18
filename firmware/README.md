# Today firmware

ESP32-S3 + 4.2" ST7305 reflective LCD (Waveshare ESP32-S3-RLCD-4.2).
The firmware is intentionally small: it stays awake, fetches the 1-bit frame
the server renders, draws the local widgets (clock, Wi-Fi, climate, battery)
inside the rectangles the frame declares, and queues button actions locally.

The contract is `internal/frame/frame.go` (format 3) and `docs/PLAN.md` §5.2.
Nothing else is shared between the Go server and this C++ code.

## Hardware

Target board: **Waveshare ESP32-S3-RLCD-4.2** (ESP32-S3-WROOM-1-N16R8,
16 MB flash, 8 MB octal PSRAM, 1× 18650 holder).

| Signal | ESP32-S3 | Notes |
|---|---|---|
| LCD SCK | GPIO11 | SPI3, mode 0, 10 MHz |
| LCD MOSI | GPIO12 | |
| LCD CS | GPIO40 | |
| LCD DC/RS | GPIO5 | |
| LCD RST | GPIO41 | |
| LCD TE | GPIO6 | not used |
| KEY | GPIO18 | to GND, active low, long = toggle, 3 s = setup |
| BOOT | GPIO0 | to GND, strapping pin: read at runtime only |
| I2C SDA | GPIO13 | SHTC3 0x70, PCF85063 0x51, ES8311 0x18, ES7210 0x40 |
| I2C SCL | GPIO14 | |
| RTC INT | GPIO15 | PCF85063 open-drain alarm output |
| Battery | GPIO4 | ADC1 channel 3, 1:3 divider (×3 in software) |

There is **no VBUS/CHG GPIO** on this board: the charger's `STAT` only drives
an LED, so the firmware never claims "charging" — the power widget shows the
battery gauge and percentage only.

The panel is a reflective LCD, not e-paper. It keeps its picture while the
controller is powered and scanning; `DISPOFF (0x28)` stops it and keeps RAM, so
the night mode is a DISPOFF plus a repaint on the next key, not a full re-init.

## Build and flash

See [`../docs/flashing.md`](../docs/flashing.md) for the full walkthrough
(download mode, browser flashing without a toolchain, esptool offsets, first
provisioning). Short version:

```sh
cd firmware
./flash.sh doctor       # tools and serial ports
./flash.sh build        # pio run
./flash.sh flash        # upload over USB
./flash.sh monitor      # serial, 115200 over USB CDC
./flash.sh merge        # dist/today-rlcd-merged.bin for esptool-js
```

or with PlatformIO directly (`brew install platformio`):

```sh
pio run && pio run -t upload && pio device monitor
```

`Serial` is the USB CDC (`-DARDUINO_USB_CDC_ON_BOOT=1 -DARDUINO_USB_MODE=1` in
`platformio.ini`). The port is `/dev/cu.usbmodem*`; if upload cannot open it,
hold BOOT while tapping RESET and retry.

The partition table uses Arduino's standard prefix: `nvs` at `0x9000`
(20 KB), `otadata` at `0xe000`, two 3 MB app slots from `0x10000`, and a 1 MB
LittleFS partition. `otadata` must sit at `0xe000` because PlatformIO writes
`boot_app0.bin` there; an OTA table with otadata anywhere else makes every USB
flash corrupt the start of NVS. `python3 test/check_partitions.py` enforces it.

NVS keeps Wi-Fi credentials, device token, cursor, outbox and the row table;
the 15 KB frame cache lives in LittleFS (`/frame.bin`) so it does not crowd
the small NVS partition. The mount passes the partition name explicitly
(`LittleFS.begin(true, "/littlefs", 10, "littlefs")`): the Arduino default
label is `spiffs`, which would fail silently against our `littlefs` partition. OTA: upload an ESP32 image from Settings → 固件, the
next frame response carries `X-Firmware-Version`, and the device streams the
image into the inactive slot.

PlatformIO pins a pioarduino Arduino-ESP32 3.x build (ESP-IDF 5). With the
Arduino IDE, select "ESP32S3 Dev Module", 16 MB flash, OPI PSRAM, the same
partition scheme, and install ArduinoJson.

## Provisioning

On a cold boot without credentials the firmware opens a 10-minute serial
window; `setup` (or a 3 s KEY hold) opens the same portal later:

```
wifi <ssid> <password>
setup
server http://192.168.1.10:8787
device dev1
token <token from Settings → 设备>
show
reboot
```

The SoftAP portal (`Today-XXXX`, WPA2, random 10-character password without
look-alike letters/digits) serves a form
at `http://192.168.4.1` that only collects SSID and password. Credentials are
stored in NVS and the board reboots. Five minutes without a submission closes
the portal. HTTPS works but uses `WiFiClientSecure::setInsecure()` because the
Funnel certificate is public and rotates; pin a CA for a trusted deployment.

## State machine

```
always on
  ├─ every 60 s → GET frame (ETag) → repaint + widgets → POST acks
  ├─ KEY short → next row, repaint
  ├─ KEY long (0.6 s) → toggle done, POST ack, force=1 refetch
  ├─ KEY hold (3 s) → SoftAP portal
  ├─ BOOT short → force=1 refetch (BOOT is never a wake key)
  └─ night window → DISPOFF; any key lights for 30 s
```

- `GET /v1/devices/<id>/frame` sends `If-None-Match`; `304` keeps the cached
  bitmap and only repaints the local widgets. The cursor never goes over the
  wire: the device inverts the row itself, so a key press works offline and
  does not change the ETag.
- `X-Server-Now` (Unix seconds) and `X-UTC-Offset` (seconds east of UTC, DST
  included) set the ESP32 clock and are written to the PCF85063, so a cold
  boot without network still has the right time.
- `X-Night: <start>-<end>` carries the night window in local minutes
  (`1380-420`); the device blanks the panel inside it and wakes on any key.
- Button actions append `{op_id,item_id,status}` to a 32-entry NVS outbox and
  are posted immediately; the server dedupes by `op_id`.
- Battery millivolts ride on every acks request; `power` is omitted because the
  board has no VBUS sense line.

## Widgets

The server leaves four rectangles blank and the device draws them:

| kind | what | source |
|---|---|---|
| 1 clock | `HH:MM`, 3× scale | system clock, calibrated by `X-Server-Now` |
| 2 wifi | 3 signal bars / `--` / `x` / `AP` | `WiFi.RSSI()` |
| 3 climate | `23° 45%` or `-- --%` | SHTC3 0x70, every 60 s |
| 4 power | battery gauge + `%`, or a plug when the reading is < 2.5 V | GPIO4 ×3, median of 7 samples |

No CJK font is linked into the firmware: boot/error screens use ASCII only,
and all Chinese text lives in the server-rendered bitmap.

## Current limits

- Local toggles are not drawn until the next fetched frame reflects the ack;
  the request is immediate, but the panel updates when the server answers.
- The night window comes from the server, so the first boot before any frame
  has no night mode. While dark, fetches still update the controller RAM but
  never send DISPON; only a key or the end of the window lights the panel.
- HTTPS certificate validation is disabled (see above).
- No config portal on first boot until the 10-minute serial window ends.
- OTA trusts the uploaded image as far as `Update` validates it; the upload is
  protected by the web session, the download by the device token.
- Charging state is not shown: there is no VBUS/CHG GPIO.
