# 刷机指南

适用板子：微雪 **ESP32-S3-RLCD-4.2**，400×300 ST7305 反射 LCD，16 MB Flash、8 MB PSRAM。需要可传数据的 USB-C 线。tag 对应的 GitHub Release 提供应用固件与 USB 完整镜像，也可从源码编译。

## 按键与下载模式

| 按键 | 用途 |
|---|---|
| PWR | 单击上电，长按断电；不是复位键 |
| KEY | 日程操作；运行时按住 3 秒打开配网热点 |
| BOOT | 运行时刷新或打开配对码；上电时按住用于下载模式 |

自动上传失败时：

1. 保持 USB 连接，长按 PWR 关机后松开。
2. 按住 BOOT，单击 PWR 开机。
3. 继续保持 BOOT 约 2 秒，再松开。
4. 重新运行上传命令。

下载模式可能保持原串口名称，以工具能否识别芯片为准。刷机后若没有运行，长按 PWR 关机，再单击上电，此时不要按 BOOT。

## 从源码编译与上传

先安装 PlatformIO Core，例如 `brew install platformio`。在仓库根目录运行：

```sh
cd firmware
./flash.sh doctor
./flash.sh build
./flash.sh flash --port /dev/cu.usbmodem1101
./flash.sh monitor --port /dev/cu.usbmodem1101
```

将示例端口替换为 `doctor` 输出的端口。`flash.sh` 使用 PlatformIO 自带的 esptool；首次构建会下载 `platformio.ini` 固定的工具链和依赖。

也可直接使用 PlatformIO，只选择需要的构建：

```sh
pio run -e esp32-s3-devkitc1-n16r8
pio run -e esp32-s3-devkitc1-n16r8 -t upload
```

需要按键边沿、绘制耗时和音频诊断日志时，使用调试构建：

```sh
pio run -e esp32-s3-devkitc1-n16r8-debug
pio run -e esp32-s3-devkitc1-n16r8-debug -t upload
```

正常上传保留 NVS 中的 Wi-Fi 和配对信息。除非需要恢复出厂，不要执行擦除闪存。

## 配网与配对

刷机完成后，按住 KEY 打开热点，设置 2.4 GHz Wi-Fi，再在 Mac App 中手动刷新附近设备、输入屏幕配对码。完整步骤见[使用指南：连接显示屏](usage.md#连接显示屏)。

不要通过手动新建 ID 代替配对。运行时按住 BOOT 3 秒可以重新打开配对码；上电时按住 BOOT 则是下载模式。

## USB 合并镜像与 OTA

两种文件用途不同：

| 文件 | 用途 |
|---|---|
| `firmware.bin` / `today-rlcd-<版本>.bin` | 应用固件，用于 OTA；不能按完整镜像写到 `0x0` |
| `today-rlcd-merged.bin` / `today-rlcd-<版本>-full.bin` | USB 合并镜像，写到 `0x0`，可能覆盖已有配置 |

在 `firmware/` 下运行 `./flash.sh merge`，生成 `firmware/dist/today-rlcd-merged.bin`（路径相对于仓库根目录）。它包含 bootloader、分区表、OTA 数据和应用，可交给支持 Web Serial 的[esptool-js](https://espressif.github.io/esptool-js/)刷写。

OTA 在 Mac App「设置 → 数据 → 固件升级」操作：选择应用 `.bin`、填写匹配版本、上传，再点击「推送升级」。只上传不会切换设备使用的版本。设备收到新版本后写入备用分区并重启，失败保留旧应用。

分区表见 `firmware/partitions.csv`：NVS 位于 `0x9000`，`otadata` 必须位于 `0xe000`，应用从 `0x10000` 开始。这与 PlatformIO 的 `boot_app0.bin` 写入偏移一致，修改后必须运行：

```sh
python3 firmware/test/check_partitions.py
```

## 固件实现

- **硬件引脚**：统一在 `firmware/include/board.h`，不要在驱动里另写一套。LCD 使用 SPI，RTC、温湿度与音频编解码器共用 I2C。
- **显示**：服务器提供 1-bit 帧，ST7305 打包规则在 `firmware/src/st7305.h`。固件不带中文字库；本机生成的提示屏使用 ASCII，中文日程由服务器绘制。
- **存储**：NVS 保存 Wi-Fi、设备口令、光标、行表和离线操作队列；完整帧缓存在 LittleFS 的 `/frame.bin`。必须显式使用 `littlefs` 分区名。
- **离线**：按键操作进入本地 outbox，恢复网络后提交；时钟由服务器校准，RTC 保持本地时间。
- **声音**：ES8311 + I2S，扬声器接 SPK。`audio.cpp` 将合成的单声道采样复制到双声道输出；GPIO46 只在播放期间启用功放。波形与静音规则分别在 `sound_synth.cpp`、`sound_policy.cpp`。
- **夜间**：DISPOFF 关闭显示并保留 RAM；按键临时唤醒，不重新初始化整个屏幕。首次尚未收到服务器配置时没有夜间策略。
- **网络**：重连不阻塞按键；多次失败后会自动开放配网热点，手动按住 KEY 可立即开放。进入配网页后暂停旧网络重连，避免打断输入。
- **HTTPS**：使用固定 ESP-IDF SDK 的 CA 包验证证书，并通过 NTP 校准用于证书验证的 UTC 时间。
- **电源**：没有 VBUS/CHG 检测线，不显示正在充电；屏幕没有背光。断电保持 RTC 需要可充电 ML1220 电池。

主机测试与帧预览见[开发指南](developing.md#验证)。协议以 `internal/frame/frame.go` 和 `firmware/src/tbf.h` 为准。

## 串口维护与恢复出厂

串口为 USB CDC，115200。未完成配置时，开机有 10 分钟维护窗口；逐行发送命令：

```text
wifi <SSID> <密码>
setup
show
reboot
```

手动排查连接时也支持 `server <地址>`、`device <ID>`、`token <口令>`；正常使用通过屏幕配对码绑定。

确实需要清除全部配置时，在 `firmware/` 下运行：

```sh
pio run -e esp32-s3-devkitc1-n16r8 -t erase
```

这会清掉 Wi-Fi、配对、光标、离线操作和缓存，之后必须重新配网、配对。

## 故障排查

| 问题 | 处理 |
|---|---|
| 找不到串口 | 确认板子上电，换数据线或 USB 口，再运行 `doctor` |
| `Failed to connect` | 按前文步骤进入下载模式；短按已上电的 PWR 不会复位 |
| 屏幕看起来很暗 | 这是无背光反射 LCD，增加环境光；确认不是夜间熄屏 |
| 找不到 Wi-Fi | 只支持 2.4 GHz；在配网页刷新，或手动输入隐藏网络 |
| App 找不到板子 | 确认同一局域网、本地网络权限和屏幕配对码，再点击设备页刷新 |
| 旧配置异常 | 先检查刷写时是否覆盖分区；仅在确认可丢弃配置后恢复出厂 |

板子资料：[微雪说明](https://docs.waveshare.com/ESP32-S3-RLCD-4.2)。
