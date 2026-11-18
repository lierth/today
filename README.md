# Today

Mac mini 上的单用户「今天」应用：菜单栏外壳 + 本地 HTTP 服务 + 一块 4.2" 反射 LCD。浏览器是唯一 UI（HTMX），SQLite 是唯一真源，设备只拿到一张已经排好版的 1-bit 位图。

## 硬件

- **微雪 ESP32-S3-RLCD-4.2**（ESP32-S3-WROOM-1-N16R8，16 MB Flash / 8 MB PSRAM）
- 4.2" ST7305 全反射 LCD，400×300，1-bit，**无背光**
- 板上：1×18650 电池座、SHTC3 温湿度、PCF85063 RTC、双麦克风/ES8311、TF 卡槽
- 两个可编程键：KEY（下一项 / 长按勾选 / 3 s 配网）、BOOT（强制刷新）
- 常亮模式，需要 USB 供电；深睡电流约 6–7 mA，电池只适合短时移动

## 文档

- **→ [刷机教程](docs/flashing.md)**：数据线、下载模式、免工具链刷合并镜像、从源码编译、首次配网、OTA、恢复出厂、故障排查
- **→ [使用教程](docs/usage.md)**：安装、口令、今日/事项/历史/设置、设备双键、夜间熄屏、Tailscale Funnel、备份与卸载
- **→ [开发](docs/developing.md)**：目录结构、`go test ./...`、`todayd` 子命令、帧契约与固件测试

## 快速开始

```sh
cd firmware && ./flash.sh doctor && ./flash.sh build && ./flash.sh flash
go test ./...                  # 服务端与帧渲染
go run . --dev                 # 菜单栏应用（托盘 + 本地服务）
```

固件、协议与版式的细节见 `firmware/README.md` 和 `docs/PLAN.md`（后者是本地设计文档，不进 git）。
