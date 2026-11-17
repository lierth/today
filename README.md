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
- `cmd/todayd` — 无 GUI 的 serve / preview / decode / sim / passwd

## 状态

M0a（frame + golden）、M0b（外壳骨架）、M1（store + server + HTMX + ticker + sim）已完成。

M2 起是固件与硬件闭环（Arduino + GxEPD2、按键 outbox、电池、历史页），尚未实现。
