# Today

macOS 菜单栏日程应用，可搭配微雪 ESP32-S3-RLCD-4.2 显示屏使用。Mac 面板、网页和显示屏共用本机 SQLite 数据。

## 功能

- 菜单栏面板：查看今天、快速添加、勾选事项和查看设备状态。
- 网页：今日、即将到来、历史、搜索、分类和回收站；创建与编辑使用居中弹窗。
- 设备：局域网发现、屏幕配对码绑定、离线勾选、声音提醒和固件升级。
- 数据：自动备份保留 7 天；删除的事项在回收站保留 3 天。

没有显示屏也可以使用 Mac 面板和网页。显示屏为 400×300 的反射 LCD，无背光，不支持亮度调节。

## 从源码构建

需要 macOS、`go.mod` 指定的 Go 版本、Node.js 22 以上和 pnpm 11.28.12。在仓库根目录运行：

```sh
pnpm -C frontend install --frozen-lockfile
go tool mygo build -platform darwin/arm64 -skip-dmg -skip-notarize
open dist/darwin-arm64/Today.app
```

构建会生成前端并嵌入 App，不会发布到 GitHub。本地默认使用 ad-hoc 签名，未经苹果公证。

## 文档

- [使用指南](docs/usage.md)：事项、分类、回收站、显示屏配网与配对、设置和备份。
- [开发指南](docs/developing.md)：本地调试、测试、目录、协议和打包。
- [刷机指南](docs/flashing.md)：USB 刷机、下载模式、OTA 和固件维护。

源码采用 [MIT 许可证](LICENSE)。第三方组件及字体声明见 [THIRD_PARTY_NOTICES](resources/THIRD_PARTY_NOTICES.txt)、[字体许可证](internal/frame/assets/OFL.txt)和[固件声明](firmware/NOTICE)。
