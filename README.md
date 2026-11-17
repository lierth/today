# Today

Mac mini 上的单用户「今天」应用：菜单栏外壳 + 本地 HTTP 服务 + 一块 400×300 的 1-bit 墨水屏。

浏览器是唯一 UI（HTMX），SQLite 是唯一真源，设备只拿到一张已经排好版的位图帧。

## 开发

```sh
go test ./...                  # frame / store / server / app / host
go run . --dev                 # 菜单栏应用（托盘 + 本地服务）
go run ./cmd/todayd serve --dev --addr 127.0.0.1:8787
go run ./cmd/todayd preview --demo --out /tmp/frame.png --png --scale 2
go run ./cmd/todayd passwd --db ~/Library/Application\ Support/Today/today.db
```

首次启动会生成 12 位访问口令并打印到 stderr，同时复制到剪贴板。之后在 `/login` 输口令进入 `/app`。

`--dev` 会关闭 Funnel 接管，并允许 `http://127.0.0.1` 下的会话 cookie；正式运行不要带。

## 打包

```sh
go tool mygo build             # dist/darwin-arm64/Today.app + dmg
```

配置在 `mygo.json`：`LSUIElement`（不占 Dock）、`today://` URL scheme、应用图标。

## 目录

- `internal/frame` — 帧契约、版式、字体、1-bit 渲染、PNG/PBM 预览
- `internal/store` — SQLite：事项、重复物化、设备、op 去重、设置
- `internal/server` — 路由、口令会话、设备 token、ETag 帧、JSON API
- `internal/host` — mygo 托盘、菜单、单实例、深链、Funnel 守护、休眠唤醒
- `web/` — 模板与静态资源（HTMX）
- `firmware/` — ESP32 + GxEPD2 固件（PlatformIO，串口配置，按键 outbox）
- `cmd/todayd` — 无 GUI 的 serve / preview / decode / sim / passwd

网页包含 `/app`（今天）、`/items`（管理，行标题可进详情）、`/history`（今天之前）、`/settings`（常规、安全、设备）。

## 固件

`firmware/` 是独立的 PlatformIO 工程，接线、分区、串口配置和电源注意事项见 `firmware/README.md`。固件只依赖 `internal/frame` 的线格式：

- 定时唤醒：连 Wi-Fi，`GET /v1/devices/<id>/frame`（ETag），按 `X-Next-Wake-At` 休眠
- 按键唤醒：不连 Wi-Fi，本地移动光标/勾选，写入 NVS outbox，60 秒后再睡
- 下一次定时唤醒把 outbox POST 到 `/v1/devices/<id>/acks`，服务端按 `op_id` 去重
- 电池毫伏随 acks 上报

## 状态

M0a（frame + golden）、M0b（外壳骨架）、M1（store + server + HTMX + ticker + sim）已完成。

M5 的网页部分已提前落地：事项详情页、历史页、多设备添加/单独预览、重复系列编辑会同步未来实例，`today://item/<id>` 直达详情。

M2–M4 的固件工程已搭好（帧解析、NVS 缓存/outbox、Wi-Fi/HTTP、按键与休眠状态机），但还没有真机验证；深睡电流、局刷行为、面板型号都要等设备到货后实测。固件 OTA 已实现（设置页上传镜像，帧响应带版本号）；应用的“检查更新…”会读取设置里的更新源并提示新版本，但不会自动安装。托盘“打开今日页面”→“快速面板”是一个只读 WebView，使用一小时有效的签名 URL，不携带会话 cookie。
