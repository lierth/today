# 开发

## 目录

| 目录 | 内容 |
|---|---|
| `internal/frame` | 帧契约（fmt 3）、版式、字体、1-bit 渲染、PNG/PBM 预览、农历 |
| `internal/store` | SQLite：事项、重复物化、设备、op 去重、设置 |
| `internal/server` | 路由、口令会话、设备 token、ETag 帧、JSON API、设置页 |
| `internal/host` | mygo 托盘、菜单、单实例、深链、Funnel 守护、休眠唤醒、回环探测 |
| `internal/config` | 启动前可读的 `config.json`（监听端口） |
| `web/` | 模板与静态资源（HTMX + 一份 `app.css`） |
| `firmware/` | ESP32-S3 + ST7305 固件（PlatformIO，常亮双键） |
| `cmd/todayd` | 无 GUI 的 serve / preview / decode / sim / passwd |

## 测试

```sh
go test ./...                                   # 全部
TZ=UTC go test ./...                            # 不依赖宿主时区
go test ./internal/frame -run TestBuildGolden -update   # 重生成 golden PBM
```

固件侧的主机测试（不需要 Arduino、不需要板子）：

```sh
clang++ -std=c++17 -Wall -Wextra -I firmware/src \
  firmware/test/pack_test.cpp firmware/src/st7305.cpp firmware/src/tbf.cpp \
  -o /tmp/pack_test
/tmp/pack_test /tmp/frame.bin    # /tmp/frame.bin 由 todayd preview --out 生成
```

它断言 ST7305 的 2×4 打包双射、四角像素、全黑/全白帧，以及 fmt 3 帧的几何、widget 表和 `X-Night` 窗口边界。

分区表检查（必须过；`otadata` 与 PlatformIO 写 `boot_app0.bin` 的偏移同址，并校验 `storage.cpp` 的 LittleFS 分区名真实存在）：

```sh
python3 firmware/test/check_partitions.py
```

## `todayd`

```sh
go run ./cmd/todayd serve --dev --addr 127.0.0.1:8787
go run ./cmd/todayd preview --demo --out /tmp/frame.png --png --scale 2
go run ./cmd/todayd preview --db <path> --out /tmp/frame.bin   # 原始 fmt 3 帧
go run ./cmd/todayd decode /tmp/frame.bin                      # 头、行、widget 表
go run ./cmd/todayd passwd --db <path>                         # 从 stdin 读新口令
```

## 契约

- 帧格式：`internal/frame/frame.go`（fmt 3：header、行表、footer 动作表、widget 表、位图、CRC32）。中文与业务文字只在位图里；设备只在帧声明的小部件矩形里画时钟/Wi-Fi/温湿度/电量。
- 易变字段走 header（`X-Server-Now`、`X-UTC-Offset`、`X-Night`、`X-Refresh`、`X-Next-Wake-At`），稳定字段走 body；`GET` 永不写库。
- 固件不共享 Go 代码，只共享这几百字节的二进制契约。

## 固件

```sh
cd firmware
./flash.sh doctor     # 工具与串口
./flash.sh build      # pio run
./flash.sh flash      # 上传
./flash.sh monitor    # 串口 115200
./flash.sh merge      # 生成 dist/today-rlcd-merged.bin（浏览器刷机用）
```

细节见 `firmware/README.md`。板子的官方资料在 <https://docs.waveshare.net/ESP32-S3-RLCD-4.2/>。

## 打包

```sh
go tool mygo build             # dist/darwin-arm64/Today.app + dmg
```

配置在 `mygo.json`：`LSUIElement`（不占 Dock）、`today://` URL scheme、应用图标。
