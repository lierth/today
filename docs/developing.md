# 开发

## 目录

| 目录 | 内容 |
|---|---|
| `internal/frame` | 帧契约（fmt 3）、版式、字体、1-bit 渲染、PNG/PBM 预览、农历 |
| `internal/store` | SQLite：事项、重复物化、设备、op 去重、设置 |
| `internal/server` | 路由、口令会话、设备 token、ETag 帧、JSON API `/api/v2`、设置页；`service.go` 是与界面无关的规则层 |
| `internal/server/webapp` | React 构建产物的托管（根路径兜底路由）；仓库里只有占位页，发布时嵌入真构建 |
| `internal/host` | mygo 托盘、菜单、单实例、深链、Funnel 守护、休眠唤醒、回环探测、本地网络权限探测；`bindings.go` 是给 Svelte 面板的 mygo 绑定 |
| `internal/config` | 启动前可读的 `config.json`（监听端口） |
| `frontend/` | pnpm workspace：`shared/`（TS 类型与语义色）、`web/`（React + shadcn/ui，网页）、`desktop/`（Svelte 5 + mygo，菜单栏面板与设置窗口） |
| `firmware/` | ESP32-S3 + ST7305 固件（PlatformIO，常亮双键） |
| `cmd/todayd` | 无 GUI 的 serve / preview / decode / sim / passwd |

## 测试

```sh
go test ./...                                   # 全部
TZ=UTC go test ./...                            # 不依赖宿主时区
TZ=UTC go test -race -count=3 ./...             # 提交前必过：数据竞争 + 偶发失败
go test ./internal/frame -run TestBuildGolden -update   # 重生成 golden PBM
pnpm -C frontend check                          # 前端：tsc / svelte-check / eslint / prettier
pnpm -C frontend test                           # 前端：web 与 desktop 的 vitest
```

前端需要 Node 22 与 pnpm 10 以上（`frontend/package.json` 的 `engines` 是下限）。`packageManager`
钉在 **pnpm 11.28.12**，不是最新的 12：Node 22 自带的 corepack 0.31 只会用 `bin/pnpm.cjs`
启动 pnpm，而 pnpm 12 只发 `bin/pnpm.mjs`，于是每条 `corepack pnpm …` 都报
`Error: Cannot find module '…/bin/pnpm.cjs'`。11.28.12 两个入口都发，所以 corepack 能用，
直接跑 `pnpm` 也能用。CI 用 `corepack prepare pnpm@11.28.12 --activate` 钉自己那份。

另一件坑：mise/Homebrew 装的 pnpm 10.x 读到 `packageManager` 会往 `~/Library/pnpm` 再下载一份
自己的二进制，下载残缺时那个 shim 是没有 shebang 的脚本，之后每条命令都报
`Error: Unknown system error -8`。`frontend/.npmrc` 的 `manage-package-manager-versions=false`
就是关掉这个行为。真撞上了，删掉 `~/Library/pnpm` 里对应版本的目录再装一次即可。

`frontend/package.json` 的脚本都写 `"$npm_execpath"` 而不是 `pnpm`：脚本内部再调 `pnpm`
会解析到 PATH 上另一个版本，pnpm 看到 `packageManager` 不符就报 `ERR_PNPM_BAD_PM_VERSION`。

`go test ./...` 本身不需要 Node：`internal/server/webapp`
嵌的是仓库里提交的占位页，只有 `scripts/build-web.sh` 或 `scripts/release.sh` 才会把
`frontend/web/dist` 拷进 `internal/server/webapp/build` 供发布版嵌入。

开发时两层前端各自热更新：

```sh
go run . &                                     # Go 服务，默认 8787
pnpm -C frontend dev:web                       # React，http://localhost:5173，代理 /api /login /logout
pnpm -C frontend dev:desktop                   # Svelte 面板，端口 5173；mygo dev 会把 devUrl 指向它
go tool mygo dev                               # 托盘 app + Svelte 热更新
```

`go run .` 会优先用磁盘上的 `frontend/web/dist`（存在就直出真页面），所以改完 React 只需
`pnpm -C frontend build`，不用重编 Go。`mygo.json` 的 `bindings` 指向
`frontend/desktop/src/mygo.ts`：`go tool mygo generate` 或 `mygo dev` 会从 `internal/host/bindings.go`
的类型注释重新生成它，**不要手改**。`main.go` 在 `MYGO_GENERATE` 下只注册服务、不打开数据库，
所以生成不会碰真实数据。

`-race` 不是可选项：帧渲染共享一份 `opentype.Face`，它的 `Metrics`/`Glyph` 用内部缓冲
（`internal/frame/font.go` 的全局渲染锁就是为此），设备并发取帧时普通测试看不出来。
`-count=3` 用来撞出「关了库还有后台 goroutine 在查」这类偶发问题
（`Server.Shutdown` 会等 ticker 退出，见 `internal/app/app_test.go` 的 `TestRestartWaitsForTicker`）。

固件侧的主机测试（不需要 Arduino、不需要板子）：

```sh
firmware/test/host.sh   # 编译并运行 pack_test（源文件清单只写在这个脚本里）
```

它写出的是 PBM（P4），不是 PNG：测试里不再手写 PNG 编码器（旧的实现把 chunk CRC 算错、zlib 流也没结束，看图的人会卡住）。转成 PNG：

```sh
for f in /tmp/todayui/device-*.pbm; do
  go run ./cmd/todayd pbm2png "$f" "${f%.pbm}.png"
done
```

它断言 ST7305 的 2×4 打包双射、四角像素、全黑/全白帧，以及 fmt 3 帧的几何、widget 表和 `X-Night` 窗口边界，还有配网页的 SSID 转义（HTML/JSON）与热点开关的退避节奏。设备自己画的每种画面（开机、等待首帧、配网、无 Wi-Fi、升级中、坏帧、设置超时）都从 `firmware/src/screens.cpp` 渲染到 `/tmp/todayui/device-*.pbm`（用 `todayd pbm2png` 转 PNG），供人查看。

## 图标

图标只有一个源文件：`resources/gen/main.go` 里的蜗牛几何（44×44 网格上的贝塞尔轮廓）。
改完重新生成：

```sh
go run ./resources/gen            # 生成全部产物
go run ./resources/gen -ascii     # 在终端里看形状，16/18/24/32 px 也逐像素打印
```

它输出 `resources/logo.svg`（≥32 px 用）、`logo-small.svg`（14–24 px 用）、`icon.svg` 与
`icon.png`（1024，macOS squircle，`mygo.json` 打包的就是它）、`tray.svg` 与 18/36 px 的模板
PNG（正常与「需要注意」两态）、浅色与暗色两版 favicon、`apple-touch-icon.png`，板子开机画面
用的 `firmware/src/logo_inc.h`（48×34 的 1-bit 位图），以及面板里那段蜗牛动画的三段路径
`frontend/shared/src/snail.ts`。预览图写到 `/tmp/todayui/hw13/`。

网页、菜单栏和内置占位页各自需要一份副本：`frontend/web/public/`、
`internal/server/webapp/dist/` 与 `internal/host/assets/`（`go:embed` 只能嵌入本包目录下的
文件，所以是复制而非引用），生成器会一并更新它们。

蜗牛动画（`frontend/desktop/src/Snail.svelte`）按 SKILL §8.5 把触角、尾尖、身体三段分开
旋转，只在窗口打开与鼠标悬停时各播一次；`prefers-reduced-motion: reduce` 时只画静止的。

macOS 本地网络权限的探测（`internal/host/localnet.go`）在 CI 上会真的发一次局域网连接；`TODAY_NO_LOCAL_NETWORK_PROBE=1` 可关掉，直接按已授权处理。

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

## 事项的三个纯函数

这几处逻辑没有数据库也能测，改它们之前先看表驱动测试：

- `server.ParseQuickAdd(text, now, loc)`（`internal/server/quickadd.go`）：把「明天 下午3点 每周一三五 开会」拆成标题/日期/时间/RRULE，`quickadd_test.go` 里 40 多条用例写明了每条歧义的裁定（裸 `3点` 是 03:00、`周五前` 是截止日不是重复、`一共3点建议` 不是时间……）。
- `store.occurrence(anchor, rec, i)`：RRULE 的第 i 次发生时间。`BYDAY` 的周从周一算，`INTERVAL` 按整周跳，`COUNT` 数实际发生次数。
- `screens::BannerTextX/BannerTextY`：设备两块横幅共用的文字定位，`pack_test` 里断言。

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

串口日志分两类：`reset: reason=…`、`portal: submitted/closed`、`acks: failed`、`ota: …` 是事件，任何构建都有，排查现场问题就看这几行；`setup: KEY held`、`portal: draw/pack/flush`、`painted screen, ink=`、`keys: KEY down|up` 是计时和输入边沿，只在带线调试时需要，默认编译不进固件。要它们：

```sh
pio run -e esp32-s3-devkitc1-n16r8-debug               # 打开 -DTODAY_DEBUG_LOG=1
pio run -e esp32-s3-devkitc1-n16r8-debug -t upload
```

细节见 `firmware/README.md`。板子的官方资料在 <https://docs.waveshare.net/ESP32-S3-RLCD-4.2/>。

## 打包

```sh
scripts/build-web.sh           # React 产物 → internal/server/webapp/build（嵌入用）
go tool mygo build             # dist/darwin-arm64/Today.app + dmg
```

`go tool mygo build` 自己会跑 `mygo.json` 的 `buildCommand`（`pnpm -C frontend install
--frozen-lockfile && pnpm -C frontend build`），并把它 `frontendDist` 指向的
`frontend/desktop/dist` 注入 app；`scripts/build-web.sh` 多做的是把 `frontend/web/dist`
拷进 `internal/server/webapp/build`，让 Go 的 `//go:embed` 带上网页。`scripts/release.sh`
两件都做，所以发版只需一条命令。

配置在 `mygo.json`：`LSUIElement`（不占 Dock）、`today://` URL scheme、应用图标、`devUrl`
（`http://localhost:5173`）、`frontendDist`（`frontend/desktop/dist`）、`bindings`
（`frontend/desktop/src/mygo.ts`）。版本号只在 tag 上：`scripts/release.sh` 在构建期间把 tag 写进 `mygo.json`，构建完再还原，**不要手改** `mygo.json` 的 `version`。

## 发版与自动更新

一条命令做完整包：

```sh
TODAY_RELEASE_KEY_FILE=~/.today/release-key \
TODAY_RELEASE_PUBLIC_KEY=$PUBLIC_KEY \
CODESIGN_IDENTITY="Today Self-Signed" \
scripts/release.sh 0.4.0
```

产物在 `dist/release/`：`Today-0.4.0-arm64.zip`、`Today-0.4.0.dmg`、`today-rlcd-0.4.0.bin`、`SHA256SUMS`、`SHA256SUMS.sig`。CI（`.github/workflows/release.yml`）在推送 `v*` tag 时跑同一个脚本并创建 release。

### 更新密钥（ed25519）

```sh
todayd release-keygen --out ~/.today/release-key      # 私钥 0600，公钥打印出来
```

- 公钥通过 `-ldflags -X today/internal/release.PublicKey=<base64>` 编进 app；`scripts/release.sh` 从 `TODAY_RELEASE_PUBLIC_KEY` 取。fork 的人用自己的密钥，别人的 release 签不动。
- GitHub Secrets：`TODAY_RELEASE_KEY`（私钥文件内容，base64）、`TODAY_RELEASE_PUBLIC_KEY`（公钥）、`TODAY_CODESIGN_P12`、`TODAY_CODESIGN_PASSWORD`、`CODESIGN_IDENTITY`。
- CI 在临时 keychain 里导入 p12，最后（`if: always()`）删掉。公钥的一半在 `release.PublicKey` 为空时也能构建，但自动更新会拒绝一切（「no public key built in」）——这是故意的，不做「无签名也照装」。

### 代码签名证书（免费自签名）

```sh
scripts/make-signing-cert.sh            # CN 默认 Today Self-Signed，可用参数改
```

它用 openssl 生成一张 10 年、带 `codeSigning` EKU 的自签名证书，导出 p12 导入登录钥匙串，再用 `security add-trusted-cert` 设为可信的代码签名证书（这一步会弹系统密码框，脚本里已经说明），最后把 base64 的 p12 和三行 secrets 写进一个 0600 文件。私钥不会打印到终端，也不会进仓库；用完 `rm -rf $TMPDIR/today-signing`。

`CODESIGN_IDENTITY` 没设时退回 ad-hoc 签名并打警告。**hardened runtime（`--options runtime`）没有开**：mygo 的更新窗口是 WKWebView，自签名证书带不动库校验所需的 entitlements，开了只会让 app 起不来；app 不离开本机，这一条写在这里作为记录。

### 更新时的两道校验

1. `SHA256SUMS` 的 ed25519 签名，再用清单里的 sha256 比对 zip 和固件；任何一步失败都删掉下载。
2. 新 `.app` 的 designated requirement（`codesign -d -r-`）必须和当前运行的 app 相同；ad-hoc 版本的 requirement 是 cdhash、每次构建都变，这种情况跳过这一条并当作「无法固定身份」。

### 更新的风险（也写进了用户文档）

- **自签名只能保证「更新后权限不丢」**：本地网络授权、钥匙串访问都绑在签名身份上，证书不变就还在。它过不了 Gatekeeper 与公证，别人第一次打开要右键「打开」，或者 `xattr -dr com.apple.quarantine /Applications/Today.app`。
- 证书丢了：新证书签出来的版本权限要重新授权一次，旧版本会因为第 2 道校验拒绝自动安装，需要手动下载。
- 只替换 `.app`；`~/.today/` 不动，schema 迁移由新版本启动时做。

### 代理

所有出站请求（WeatherAPI、检查更新、下载）共用一个 `*http.Client`（`server.OutboundClient()`），`Transport.Proxy` 每次拨号重新读 `proxy_mode`/`proxy_url`。`proxy_mode=on` 且 `proxy_url` 为空时跟随系统代理：解析 `scutil --proxy`，优先级 HTTPS → SOCKS → HTTP，`ExceptionsList` 支持 `*.x`、`.x`、`10.*`、`169.254/16`。PAC 不支持，设置页会明说。回环与私网地址永远直连；Tailscale CLI 与设备访问本机的流量本来就不经过这个 client。
