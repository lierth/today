# 刷入开发板

目标读者：一台 Apple Silicon Mac，一块 **微雪 ESP32-S3-RLCD-4.2**，之前没装过任何 ESP32 工具。所有命令都能从零照抄；每一步都写明工具来源。

> 板子参数：ESP32-S3-WROOM-1-N16R8（16 MB Flash / 8 MB OPI PSRAM）、4.2" ST7305 全反射 LCD（400×300，无背光）、1×18650 电池座、PCF85063 RTC、SHTC3 温湿度。

## 0. 硬件准备

| 物品 | 说明 |
|---|---|
| USB-C **数据线** | 充电线只有电源针脚，插上不会出现 `/dev/cu.usbmodem*`；换一根线先排除这个 |
| 18650（可选） | 常亮模式建议插 USB；电池只用于移动或断电后维持 RTC |
| Type-C 口 | 板子侧边唯一的 USB-C，既供电也是 USB-Serial/JTAG |
| PWR 键 | **单击上电，长按断电**；首次装电池后需要插一次 USB 激活保护电路 |
| BOOT 键 | 只在需要进下载模式时使用；**正常上电不要按住**，否则会进下载模式而不是运行固件 |
| KEY 键 | 运行时按键（下一项 / 长按勾选 / 按住 3 s 开配网热点） |

### 进入下载模式

固件上传失败、或串口一直连不上时：

1. 按住 **BOOT** 不放；
2. 单击 **PWR**（或重新插 USB）；
3. 松开 BOOT。

屏幕这时不会显示正常画面，串口会重新枚举成可烧录设备。烧录完成后单击 PWR 正常启动。

## 1. 两条路径

### 路径 A：不装工具链（只想用）

**A1 浏览器（Chrome / Edge）**

1. 从 release 下载合并镜像 `today-rlcd-<版本>.bin`（约 1.3 MB，包含 bootloader、分区表、boot_app0、应用）。
2. 打开 <https://espressif.github.io/esptool-js/>（乐鑫官方 esptool-js，Web Serial）。
3. 连接板子（Chrome/Edge 才支持 Web Serial，Safari 不支持），选择串口，Flash address 填 `0x0`，选文件，Program。
4. 烧完单击 PWR 重启。

也可以用 ESP Web Tools（<https://esphome.github.io/esp-web-tools/>）刷同一个合并镜像；它只是一个 Web Serial 前端，协议相同。

**A2 命令行 esptool**

```sh
brew install esptool          # 或：uv tool install esptool
esptool.py --chip esp32s3 --port /dev/cu.usbmodem1101 write_flash 0x0 today-rlcd-<版本>.bin
```

`/dev/cu.usbmodem*` 用 `ls /dev/cu.*` 查；只有一个时也可以省略 `--port`，esptool 会自己找。

合并镜像的偏移（`firmware/flash.sh merge` 已封装，偏移不是手写的）：

| 偏移 | 内容 |
|---|---|
| `0x0` | bootloader.bin |
| `0x8000` | partitions.bin |
| `0xe000` | boot_app0.bin（Arduino-ESP32 自带；必须与 otadata 同址） |
| `0x10000` | firmware.bin（应用，app0 起点） |

> 前三个偏移由 `pio project metadata --json-output` 的 `flash_images` 给出，应用偏移从 `firmware/partitions.csv` 的 app0 读出，所以和 `pio run -t upload` 完全一致。
>
> **为什么 otadata 必须在 0xe000**：PlatformIO 对任何带 OTA 数据分区的表都把 `boot_app0.bin` 写到 `0xe000`。本项目早期的表把 `otadata` 放在 `0x19000`、NVS 覆盖 `0x9000–0x19000`，于是每次 USB 刷机都会写坏 NVS 开头。现在分区表是 Arduino 标准前缀：`nvs 0x9000 0x5000`、`otadata 0xe000 0x2000`、`app0 0x10000`；15 KB 的帧缓存放在 LittleFS，NVS 只放凭据、光标、outbox 和行表。
>
> `python3 firmware/test/check_partitions.py` 会断言 `otadata == boot_app0 偏移`、`app0 == 0x10000`、无重叠、NVS 大小合理。

### 路径 B：从源码编译

工具：**PlatformIO Core**（官方安装方式 <https://docs.platformio.org/en/latest/core/installation/index.html>）。

```sh
brew install platformio       # 官方推荐 macOS 用 Homebrew；也可 uv tool install platformio
cd firmware
pio run                       # 编译
pio run -t upload             # 编译并烧录（自动找串口）
pio device monitor            # 115200 串口日志
```

本项目固定使用 pioarduino 的 Arduino-ESP32 3.x 平台（`platformio.ini` 里的 URL），首次 `pio run` 会下载工具链，需要网络。

`firmware/flash.sh` 把常用命令包了一层，零额外依赖：

```sh
cd firmware
./flash.sh doctor             # 检查 pio/esptool、列出串口
./flash.sh build              # pio run
./flash.sh flash --port /dev/cu.usbmodem1101
./flash.sh monitor --port /dev/cu.usbmodem1101
./flash.sh merge              # 生成 dist/today-rlcd-merged.bin
```

`flash.sh` 只依赖 bash 和 PlatformIO 自带的 esptool（`pio pkg exec -- esptool.py`），不额外装东西。

## 2. 首次配网

设备启动后需要三样东西：Wi-Fi、服务端地址、设备 token。两种方式，任选。

### 方式 A：串口（推荐，能看到结果）

1. 在网页「设置 → 设备」里添加设备并生成 token（**只显示一次**，同一处会给出四行串口命令）。
2. 打开串口：`./flash.sh monitor --port …` 或 `pio device monitor`。
3. 冷启动（单击 PWR 或重新插 USB）后 10 分钟内有配置窗口，逐行粘贴：

```
wifi <SSID> <password>
server http://<Mac 的局域网地址>:8787
device dev1
token <token>
show
reboot
```

也可以用 `./flash.sh provision --server http://192.168.1.10:8787 --device dev1 --token <token> --ssid <SSID> --pass <密码>`，脚本会写串口并回读 `show`；`--dry-run` 只打印将发送的行（token 打码）。

4. 屏上出现日期、农历和事项，即成功。

### 方式 B：SoftAP 配网（只收 Wi-Fi）

1. 冷启动时按住 **KEY** 3 秒（或已存 Wi-Fi 连续失败 5 次后自动进入）。
2. 屏上显示热点名 `Today-XXXX`、10 位密码、`http://192.168.4.1`。
3. 手机/电脑连该热点，打开 `http://192.168.4.1`，填 SSID 和密码，保存后设备自动重启。
4. token 仍需走方式 A（串口）或网页设置页复制命令；SoftAP 只收 Wi-Fi。

## 3. OTA 升级

网页「设置 → 固件」上传 `firmware.bin`（不是合并镜像）。设备下次取帧看到新版本号会下载到备用分区并重启，失败保留旧版本。

## 4. 恢复出厂

```sh
pio run -t erase              # 或 esptool.py --chip esp32s3 erase_flash
```

会清掉 NVS：Wi-Fi、token、光标、本地 outbox、缓存帧全部丢失。之后重新走第 2 节。

## 5. 故障排查

| 现象 | 排查 |
|---|---|
| 找不到 `/dev/cu.usbmodem*` | 换数据线；单击 PWR 上电；macOS 不需要额外驱动（USB-Serial/JTAG） |
| 上传失败 / `Failed to connect` | 按住 BOOT → 单击 PWR → 松开 BOOT 进下载模式；或换 USB 口 |
| 屏不亮 | 单击 PWR；确认 USB 供电；RLCD 无背光，弱光下本来就很暗（见使用教程） |
| 连不上 Wi-Fi | 只支持 **2.4 GHz**；SSID/密码区分大小写；SoftAP 配网会覆盖旧凭据 |
| 取帧 404 | `device` 或 `token` 与网页「设置 → 设备」不一致；token 重新生成后旧值立刻失效 |
| 屏幕一直是 `WIFI SETUP` | 没有保存成功：重新连接热点提交，或改用串口方式 |
| 刷过旧固件后 NVS 报错 / Wi-Fi 丢失 | 旧分区表把 `boot_app0` 写进了 NVS；升级到当前分区表后重新配网一次即可（`pio run -t erase` 可彻底清空） |
| 时间不对 | 首次联网会写入服务器时间；RTC 电池（ML1220 可充电）没装时断电会丢时间 |

## 6. 参考

- 板子文档：<https://docs.waveshare.net/ESP32-S3-RLCD-4.2/>
- 原理图：<https://www.waveshare.net/w/upload/e/e6/ESP32-S3-RLCD-4.2-schematic.pdf>
- PlatformIO Core 安装：<https://docs.platformio.org/en/latest/core/installation/index.html>
- esptool：<https://docs.espressif.com/projects/esptool/en/latest/esp32s3/>
- esptool-js（Web Serial）：<https://espressif.github.io/esptool-js/>
