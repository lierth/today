# 开发指南

所有命令均从仓库根目录运行，另有说明的除外。日常提交不触发发布；推送版本 tag 才会构建并发布安装包和固件。

## 环境与启动

- Go：以 `go.mod` 的 `go` 指令为准。
- 前端：Node.js 22 以上、pnpm 11.28.12，以 `frontend/package.json` 的 `packageManager` 为准。
- 固件：PlatformIO Core，平台及依赖已写在 `firmware/platformio.ini`。

```sh
pnpm -C frontend install --frozen-lockfile
scripts/build-web.sh
go run . --dev
```

这会启动菜单栏应用和本地服务。`scripts/build-web.sh` 构建 React 与 Svelte，并把网页复制到 `internal/server/webapp/build`，供 Go 嵌入。开发时服务也可直接读取 `frontend/web/dist`。

原生前端热更新：

```sh
go tool mygo dev
```

MyGo 自动运行 Svelte 开发服务器，不要再单独启动 `dev:desktop`。原生调试使用默认数据目录，开始前退出已运行的 Today，并备份需要保留的数据。

只调试网页和接口时，可使用独立数据库。以下两个命令分别在两个终端运行；先退出占用 8787 端口的 Today：

```sh
go run ./cmd/todayd serve --db /tmp/today-dev.db --addr 127.0.0.1:8787 --dev
pnpm -C frontend dev:web
```

网页开发服务器默认在 `http://localhost:5173`，将 `/api`、`/login`、`/logout` 代理到本机 8787。网页和原生开发服务器默认都使用 5173，不要同时按默认配置启动。

`mygo.json` 的 `bindings` 指向生成文件 `frontend/desktop/src/mygo.ts`。修改 Go 绑定后运行 `go tool mygo generate`，不要直接编辑生成文件。生成过程不会打开数据库。

## 目录

| 路径 | 职责 |
|---|---|
| `internal/store` | SQLite 事项、重复规则、分类、回收站、设备、设置和操作去重 |
| `internal/server` | 业务规则、登录与设备鉴权、JSON API、帧响应 |
| `internal/frame` | TBF 二进制格式、排序、版式、中文字体及 1-bit 渲染 |
| `internal/host` | MyGo 菜单栏、设置绑定、单实例、权限、Funnel 与更新 |
| `internal/backup` | 备份、保留策略和恢复 |
| `frontend/web` | React 网页及 UI 组件 |
| `frontend/desktop` | Svelte 面板与设置窗口 |
| `frontend/shared` | 共用类型、颜色、圆角和图标路径 |
| `firmware/src` | 取帧、配网、配对、显示、按键、音频与本地缓存 |
| `cmd/todayd` | 无界面服务、预览、解码、模拟、口令与签名工具 |

## 验证

按修改范围运行检查：

```sh
go test ./...
TZ=UTC go test -race ./...
pnpm -C frontend check
pnpm -C frontend lint
pnpm -C frontend test
firmware/test/host.sh
python3 firmware/test/check_partitions.py
```

Go 测试不需要 Node；没有真实前端产物时会嵌入占位页。帧渲染和后台任务涉及共享状态，修改这些部分需要并发检测。固件主机测试不依赖开发板，但不能代替实机联网、声音和 OTA 验收。

测试数据库、日志、截图、构建和签名材料留在忽略目录或项目外；不要提交。`node_modules/`、`.mygo/`、`.tmp/`、`dist/`、`.pio/` 和设计草稿均应保持未跟踪。

## 预览与图标

```sh
go run ./cmd/todayd preview --demo --out /tmp/today-frame.png --png --scale 2
go run ./cmd/todayd preview --demo --out /tmp/today-frame.bin
go run ./cmd/todayd decode /tmp/today-frame.bin
```

固件主机测试的 PBM 预览位于 `/tmp/todayui/`，可转换后检查：

```sh
go run ./cmd/todayd pbm2png /tmp/todayui/device-boot.pbm /tmp/today-boot.png
```

图标源在 `resources/gen/main.go`，修改后运行 `go run ./resources/gen`。它会同步正式 SVG、PNG、favicon、托盘图标、固件位图和共用路径；`internal/host/assets`、`frontend/web/public`、`internal/server/webapp/dist` 的副本用于各自的构建和嵌入，不能当作临时产物删除。

UI 颜色和圆角集中在 `frontend/shared/src/tokens.css`。网页复用已有的 Radix/shadcn 组件和日历组件，保持鼠标、键盘焦点、禁用状态及浅色/深色主题一致。

## 协议与数据

- 帧定义在 `internal/frame/frame.go`，固件解析在 `firmware/src/tbf.h`、`tbf.cpp`。当前为 TBF 格式 3，包含行表、动作表、小部件、400×300 的 1-bit 位图和 CRC32。
- 中文与日程文字由服务器渲染；设备在预留区域绘制时钟、信号、温湿度和电量。显示屏按时间顺序列出事项，不使用突出首项的布局。
- `GET /v1/devices/<id>/frame` 使用设备口令与 ETag；`304` 复用缓存。GET 不写库，设备操作经确认接口按 `op_id` 去重。
- 时钟、时区、夜间窗口、提醒和声音策略通过响应头下发，相关处理见 `internal/server` 与 `firmware/src/net.cpp`。当前固件每 5 秒发起条件取帧，不等于每 5 秒重画屏幕。
- 配对发现与绑定见 `internal/boardlink`、`internal/server/boardpair.go`、`firmware/src/pairing.cpp`。设备列表只在手动刷新时扫描，已配对状态单独轮询。
- 口令使用 Argon2id 哈希；原文由宿主保存在钥匙串。网页使用 HttpOnly 会话 cookie，设备使用独立口令。
- 分类和回收站逻辑见 `internal/store/organize.go`；删除分类保留事项，事项软删除后保留 3 天。
- 普通备份保留 7 天，恢复前副本保留 5 份；规则在 `internal/backup`。恢复时必须保持确认、事务和会话失效处理。
- 天气、更新等外部 HTTP 请求共用 `server.OutboundClient()`；代理配置从设置读取，PAC 不支持。

固件的存储、音频、分区与刷机方式见[刷机指南](flashing.md)。

## 本地打包与 tag 发布

```sh
go tool mygo build -platform darwin/arm64 -skip-dmg -skip-notarize
```

产物为 `dist/darwin-arm64/Today.app`；构建会通过 `mygo.json` 调用 `scripts/build-web.sh`。不传 `-skip-dmg` 时也会生成 DMG。应用标识是 `com.lierth.today`，数据目录仍为 `~/.today/`。

在要发布的提交上创建 `v主版本.次版本.修订版本` 格式的 tag，再推送到 GitHub：

```sh
git tag -a v0.3.0 -m "Today 0.3.0"
git push origin v0.3.0
```

`.github/workflows/release.yml` 会检查代码、编译前端与固件、生成安装包，再发布到这个 tag 的 Release。当前只接受 `v1.2.3` 这样的正式版本号，与应用的更新版本规则一致。重跑同一 tag 的任务会替换对应附件。

Release 附件包括 Apple Silicon 的 `Today-<版本>.dmg`、App ZIP、OTA 固件 `today-rlcd-<版本>.bin`、USB 镜像 `today-rlcd-<版本>-full.bin` 和 `SHA256SUMS`。配置更新签名后还会附上 `SHA256SUMS.sig`。历史版本说明在 `docs/releases/`，没有对应文件时自动生成 Release 说明。

发布使用工作流自带的 `GITHUB_TOKEN`，无需额外 PAT，也不要求 Apple Developer 账号。没有可选签名 Secrets 时使用 ad-hoc 签名并提供手动下载安装包。

只在本地打包完整产物时使用 `ALLOW_UNSIGNED_RELEASE=1 scripts/release.sh <版本>`；本地脚本不会上传。配置签名时的参数以脚本开头的说明为准。

自动更新需要 `TODAY_RELEASE_KEY` / `TODAY_RELEASE_KEY_FILE` 和匹配的 `TODAY_RELEASE_PUBLIC_KEY`，先验 ed25519 签名，再校验 SHA256；未配置公钥的构建不自动安装更新。私钥、证书和凭据不得入库。Apple Developer ID 签名和公证为可选发布方式；ad-hoc 签名无法提供跨构建稳定的签名身份。
