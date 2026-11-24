# Today

Mac mini 上的单用户「今天」应用：菜单栏外壳 + 本地 HTTP 服务 + 一块 4.2" 反射 LCD。界面有三处——菜单栏的托盘面板、一个设置窗口、以及浏览器里的网页，全部由同一套 Go 规则层（`internal/server`）驱动；SQLite 是唯一真源，设备只拿到一张已经排好版的 1-bit 位图。

![今日页面](docs/img/web-today-light.png)

## 下载安装

从 [GitHub Releases](https://github.com/lierth/today/releases) 下载 `Today-<版本>.dmg`，把 Today 拖到「应用程序」，然后打开菜单栏图标。当前安装包适用于 Apple Silicon Mac，未经过苹果公证；首次打开时可能需要按系统提示允许运行。

没有板子也可以使用 Mac 面板和网页。显示屏的应用固件为 `today-rlcd-<版本>.bin`；`-full.bin` 仅用于首次 USB 刷机，见[刷机教程](docs/flashing.md)。

0.2.0 的自动测试和刷机校验已通过。

未配置更新签名密钥的发行包使用手动下载更新。项目源码采用 [MIT 许可证](LICENSE)，第三方组件和字体保留各自许可证。

## 硬件

- **微雪 ESP32-S3-RLCD-4.2**（ESP32-S3-WROOM-1-N16R8，16 MB Flash / 8 MB PSRAM）
- 4.2" ST7305 全反射 LCD，400×300，1-bit，**无背光**
- 板上：1×18650 电池座、SHTC3 温湿度、PCF85063 RTC、双麦克风/ES8311、TF 卡槽
- 两个可编程键：KEY（下一项 / 长按勾选 / 3 s 配网）、BOOT（强制刷新）
- 常亮模式，需要 USB 供电；深睡电流约 6–7 mA，电池只适合短时移动

## 界面

- **托盘面板**：点菜单栏的图标弹出，今天的进度、事项、设备状态与一行天气；勾选、快速添加都在这里完成。右键仍然是原生菜单。
- **设置窗口**：常规、设备、显示与声音、网络、数据、关于六组。
- **网页**：今日、即将到来、历史、搜索四个页面，桌面 1280 与手机 390 都排好版。

![深色主题](docs/img/web-today-dark.png)

<p align="center">
  <img src="docs/img/web-today-phone.png" alt="手机上的今日页面" width="320">
</p>

## 文档

- **→ [刷机教程](docs/flashing.md)**：数据线、下载模式、免工具链刷合并镜像、从源码编译、首次配网、OTA、恢复出厂、故障排查
- **→ [使用教程](docs/usage.md)**：安装、口令、今日/事项/历史/设置、设备双键、夜间熄屏、Tailscale Funnel、备份与卸载
- **→ [开发](docs/developing.md)**：目录结构、`go test ./...`、`todayd` 子命令、帧契约与固件测试、前端工作区

## 快速开始

```sh
cd firmware && ./flash.sh doctor && ./flash.sh build && ./flash.sh flash
go test ./...                  # 服务端与帧渲染，不需要 Node
go run . --dev                 # 菜单栏应用（托盘 + 本地服务）
```

前端（Mac app 的 Svelte 页面与网页的 React 页面）在 `frontend/`，需要 Node 22 以上与 pnpm 11.28.12（项目已固定版本）：

```sh
pnpm -C frontend install
pnpm -C frontend build         # 或者 scripts/build-web.sh，会同时嵌入网页
go tool mygo build             # 出 Today.app 与 dmg
```

固件、协议与版式的细节见 `firmware/README.md` 和 `docs/PLAN.md`（后者是本地设计文档，不进 git）。
