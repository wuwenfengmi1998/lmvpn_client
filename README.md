# LMVPN Client

LMVPN 是一个基于 WebSocket 隧道与 TUN 虚拟网卡的**三层（网络层）VPN 客户端**。客户端通过 WebSocket（`ws://`/`wss://`）连接服务端，完成认证后在本地 TUN 虚拟网卡与 WebSocket 连接之间双向转发原始 IP 数据包，实现透明隧道。

客户端使用 [Go](https://go.dev/) + [Fyne](https://fyne.io/) 开发，支持 macOS、Windows、Linux 三大桌面平台。

> **服务端项目**：<https://github.com/wuwenfengmi1998/lmvpn_server>

---

## 目录

- [功能特性](#功能特性)
- [系统要求](#系统要求)
- [各平台编译](#各平台编译)
  - [通用前置依赖](#通用前置依赖)
  - [macOS](#macos)
  - [Windows（交叉编译）](#windows交叉编译)
  - [Linux](#linux)
  - [Make 目标速查表](#make-目标速查表)
- [运行使用](#运行使用)
- [配置与数据目录](#配置与数据目录)
- [架构概览](#架构概览)
- [开发指南](#开发指南)
- [注意事项](#注意事项)

---

## 功能特性

- **双进程架构**：GUI（`lmvpn`，普通用户）+ 守护进程（`lmvpnd`，root/管理员），自动拉起与生命周期管理
- **多种认证**：JWT 令牌 / 用户名密码
- **隧道模式**：全隧道、代理 CIDR（指定 CIDR 走隧道）、绕过 CIDR（指定 CIDR 绕过隧道），支持 IPv4/IPv6 分开配置与 URL 动态获取 CIDR 列表
  - **URL 获取时机**：代理前（直连获取，适用于 GitHub 等外部源）或代理后（通过隧道获取，适用于 VPN 服务器可达的源）
  - **注意**：绕过 CIDR 模式下"代理后获取"可能失败——/1 覆盖路由会将 HTTP 请求导入隧道，若 VPN 服务器无法访问目标 URL 则超时。建议将 GitHub 等外部源设置为"代理前获取"
  - **CIDR 聚合**：自动合并相邻 CIDR 块以减少路由数量，配合批量脚本并行执行加速路由添加
  - **实时统计**：状态栏显示路由模式、CIDR 命中数、加载进度；支持手动刷新 CIDR 列表
- **多服务器管理**：配置文件 + SQLite 存储多个服务器配置（Profile）
- **国际化**：中文（简体）、英文，跟随系统语言
- **安全存储**：macOS Keychain / Windows Credential Manager 加密保存凭据
- **系统集成**：系统托盘图标、Dock 显隐、开机自启
- **日志轮转**：基于 lumberjack 的文件日志自动轮转

---

## 系统要求

| 平台 | 最低版本 | 架构 | 备注 |
|------|---------|------|------|
| macOS | 11.0（Big Sur） | amd64 / arm64 | 需 Xcode Command Line Tools |
| Windows | 10 | x86_64 | 仅支持 64 位，需 WinTun 驱动（已内置） |
| Linux | 现代发行版 | amd64 / arm64 | 需 OpenGL/GLFW/libdbus 开发库 |

**所有平台均要求 `CGO_ENABLED=1`**（Fyne 依赖 OpenGL/GLFW 的 CGO 绑定，不可关闭）。

---

## 各平台编译

### 通用前置依赖

- **Go 1.26** 或更高版本（见 `go.mod`）
- **Git**：用于在编译时注入版本号，格式为 `0.3.7-<git短哈希>`（如 `0.3.7-019df7b`）
- **C 编译器**：GCC（Linux/Windows 交叉编译）或 clang（macOS），由 CGO 调用
- **网络访问**：首次构建需拉取 Go 模块依赖

版本号通过 `-ldflags "-X lmvpn/internal/version.Version=$(VERSION)"` 在链接期注入到 `internal/version` 包，GUI 与守护进程共享同一版本字符串。

### macOS

#### 依赖

- **Xcode Command Line Tools**：提供 clang（CGO）、Cocoa 框架、`sips`、`iconutil`（图标生成）

  ```bash
  xcode-select --install
  ```

- 最低部署目标 macOS 11.0（见 `resources/Info.plist` 的 `LSMinimumSystemVersion`）

#### 编译命令

```bash
# 默认目标：编译二进制并组装 LMVPN.app bundle
make            # 等价于 make app

# 仅编译裸二进制（build/lmvpn、build/lmvpnd），不打包 .app
make build

# 重新生成 macOS 图标 resources/icon.icns（需要 icon.png 或 logo.svg）
make icon

# 编译并直接运行 GUI
make run
```

`make app` 会将 `build/lmvpn`、`build/lmvpnd` 连同 `resources/Info.plist`、`resources/icon.icns` 组装成 `LMVPN.app/`。

#### 运行

守护进程创建 TUN 网卡与修改路由需要 root 权限，GUI 通过 `osascript ... with administrator privileges` 弹出系统授权对话框提权拉起守护进程。日常使用直接双击 `LMVPN.app` 即可。

---

### Windows（交叉编译）

Windows 版本从 macOS 或 Linux **交叉编译**生成（Fyne 无法用 `CGO_ENABLED=0` 构建，因此必须借助 mingw-w64 提供 C 交叉编译器）。仅支持 **x86_64** 架构。

#### 依赖

- **mingw-w64 工具链**：提供 `x86_64-w64-mingw32-gcc`（C 编译器）与 `x86_64-w64-mingw32-windres`（资源编译器）

  ```bash
  # macOS
  brew install mingw-w64
  ```

- **Inno Setup 6**（仅打包安装程序时需要）：用于生成 `.exe` 安装包
  - 原生 Windows：将 `ISCC` 加入 `PATH`
  - macOS/Linux：通过 Wine 调用，需安装 Wine 并将 Inno Setup 6 装到 Wine 的 `C:\Program Files (x86)\Inno Setup 6\`

#### 编译命令

```bash
# 1. 生成 Windows 图标与资源（.ico + .syso），并交叉编译 exe
make build-windows

# 2. 单独生成图标资源（生成 resource_windows_amd64.syso，会被 go build 自动链接）
make icon-windows

# 3. 编译并打包 Inno Setup 安装程序
make installer-windows
```

`make build-windows` 实际执行：

```bash
# GUI（带 -H windowsgui，无控制台窗口）
CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc \
    go build -ldflags "-s -w -X ... -H windowsgui" -o build/lmvpn.exe ./cmd/lmvpn

# 守护进程（控制台程序）
CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc \
    go build -ldflags "-s -w -X ..." -o build/lmvpnd.exe ./cmd/lmvpnd
```

产物：`build/lmvpn.exe`、`build/lmvpnd.exe`，安装包 `build/LMVPN-Setup-<version>.exe`。

#### 关于 WinTun

Windows 版使用 [WinTun](https://www.wintun.net/) 驱动创建虚拟网卡。`wintun.dll` 在编译期通过 `//go:embed` 嵌入二进制，运行时释放到 exe 同级目录；安装包也会单独安装一份 `wintun.dll`。

#### 运行

守护进程需要管理员权限，GUI 通过 UAC（`ShellExecuteW` + `runas`）提权拉起。

---

### Linux

Linux 没有独立的 Make 目标，使用平台无关的 `make build` 原生编译。

#### 依赖

- **GCC**（CGO 编译器）
- **OpenGL 与 GLFW 开发库**（Fyne 依赖）：`libgl`、`libglfw`、`libx11`、xorg 开发头
- **libdbus-1 开发库**（系统托盘，经 `godbus/dbus`）：`libdbus-1-dev` 或对应开发包

> 请根据所用发行版（apt / dnf / pacman 等）自行安装上述开发库。

#### 编译命令

```bash
# 原生编译（平台无关，直接产出 build/lmvpn、build/lmvpnd）
make build

# 编译并运行 GUI
make run

# 以 root 运行守护进程（调试用）
make daemon
```

#### 运行

- TUN 网卡创建与路由配置需要 **root 或 `CAP_NET_ADMIN`** 能力
- 提权方式：`pkexec`（`internal/ui/elevation_other.go`）
- 系统命令依赖：`ip`、`ip route`（`internal/route/route_linux.go`、`internal/tun/tun_linux.go`）
- **密钥链**：Linux 暂使用内存存储（不持久化），重启后凭据丢失，后续需接入 Secret Service 等后端

---

### Make 目标速查表

| 目标 | 说明 |
|------|------|
| `make` / `make all` / `make app` | 编译并组装 macOS `LMVPN.app` bundle（默认） |
| `make build` | 仅编译 `lmvpn` + `lmvpnd` 裸二进制（macOS/Linux 原生） |
| `make run` | 编译并运行 GUI |
| `make daemon` | 编译并以 `sudo` 运行守护进程 |
| `make build-windows` | 交叉编译 Windows x64 exe（含图标资源生成） |
| `make icon-windows` | 生成 Windows `.ico` 与 `.syso` 资源文件 |
| `make installer-windows` | 编译 exe 并打包 Inno Setup 安装程序 |
| `make icon` | 由 `icon.png`/`logo.svg` 重新生成 macOS `icon.icns` |
| `make vet` | 运行 `go vet ./...` |
| `make fmt` | 运行 `go fmt ./...` |
| `make tidy` | 运行 `go mod tidy` |
| `make clean` | 清理 `build/` 与 `LMVPN.app/` |

---

## 运行使用

GUI 会自动管理守护进程的生命周期（拉起、停止），无需手动启动 `lmvpnd`。

- **macOS**：双击 `LMVPN.app`，首次连接时系统弹出授权对话框输入密码以提权守护进程
- **Windows**：运行安装包或直接运行 `lmvpn.exe`，首次连接时 UAC 弹窗确认提权
- **Linux**：`make run` 或运行编译好的 `lmvpn`，首次连接时 `pkexec` 弹窗输入密码提权

GUI 与守护进程通过本地 IPC 通信：macOS/Linux 使用 Unix socket `/tmp/lmvpn.sock`；Windows 使用 TCP `127.0.0.1:18923`（因 Windows 上 AF_UNIX 有完整性级别校验，会阻止非提权 GUI 访问提权守护进程的 socket）。

---

## 配置与数据目录

各平台的数据目录布局（Bundle ID 为 `com.lmvpn.client`）：

| 平台 | 配置/数据 | 缓存 | 日志 |
|------|----------|------|------|
| macOS | `~/Library/Application Support/com.lmvpn.client/` | `~/Library/Caches/com.lmvpn.client/` | `~/Library/Logs/com.lmvpn.client/` |
| Windows | `%APPDATA%\com.lmvpn.client\` | `%LOCALAPPDATA%\com.lmvpn.client\` | 同数据目录下 `log/` |
| Linux | `~/.local/share/com.lmvpn.client/` | `~/.cache/com.lmvpn.client/` | `~/.local/state/com.lmvpn.client/log/` |

- 配置文件支持 TOML 与 YAML 两种格式（`internal/config`）
- 服务器配置（Profile）存储于 SQLite 数据库（`internal/db`，使用纯 Go 的 `modernc.org/sqlite`，无需 CGO）
- 凭据保存于系统密钥链（macOS Keychain / Windows Credential Manager / Linux 内存）

---

## 架构概览

### 双进程设计

```
┌──────────────┐   IPC (Unix socket / TCP)   ┌──────────────┐
│   lmvpn      │ ◄─────────────────────────► │   lmvpnd     │
│   (GUI)      │      控制命令 / 状态         │  (守护进程)   │
│  Fyne UI     │                             │  WebSocket   │
│  普通用户     │                             │  TUN 网卡    │
│              │                             │  root/管理员  │
└──────────────┘                             └──────┬───────┘
                                                    │
                                              WebSocket (ws/wss)
                                                    ▼
                                              ┌──────────────┐
                                              │  LMVPN 服务端 │
                                              └──────────────┘
```

拆分 GUI 与守护进程的原因：避免 Fyne（及其 locale/字体初始化）加载进 root 进程，同时让提权范围最小化——仅守护进程需要 root 权限操作网卡与路由。

### 目录结构

```
lmvpn_client/
├── cmd/
│   ├── lmvpn/          # GUI 入口
│   └── lmvpnd/         # 守护进程入口
├── internal/
│   ├── auth/           # 认证（JWT / 用户名密码）
│   ├── config/         # 配置文件解析（TOML / YAML）
│   ├── daemon/         # 守护进程生命周期、IPC、提权拉起
│   ├── db/             # SQLite 存储、Profile、日志记录
│   ├── i18n/           # 国际化（en / zh-Hans）
│   ├── ipc/            # GUI ↔ 守护进程 IPC 协议
│   ├── keychain/       # 密钥链（darwin/windows/other）
│   ├── log/            # 日志（lumberjack 轮转）
│   ├── model/          # 数据模型
│   ├── paths/          # 平台路径解析（darwin/windows/other）
│   ├── protocol/       # 与服务端的 WebSocket 协议
│   ├── route/          # 路由管理（全隧道/代理CIDR/绕过CIDR）
│   ├── stats/          # 流量统计
│   ├── transport/      # WebSocket 传输层
│   ├── tun/            # TUN 虚拟网卡（darwin/linux/windows）
│   ├── ui/             # Fyne 界面、托盘、提权、Dock
│   ├── version/        # 版本号（链接期注入）
│   └── vpn/            # VPN 会话管理
├── resources/          # 图标、Info.plist、wintun.dll、资源生成器
├── installer/          # Inno Setup 安装脚本（lmvpn.iss）
├── docs/               # 开发文档（协议规范）
└── Makefile
```

### 平台适配约定

项目使用 Go 文件后缀构建约束实现平台适配，主要分布：

| 模块 | macOS | Windows | Linux |
|------|-------|---------|-------|
| `tun` | `tun_darwin.go`（water/utun） | `tun_windows.go`（wintun + embed dll） | `tun_linux.go`（water/tun） |
| `route` | `route_darwin.go`（`route`） | `route_windows.go`（`route`） | `route_linux.go`（`ip route`） |
| `keychain` | `keychain_darwin.go`（Keychain） | `keychain_windows.go`（Credential Manager） | `keychain_other.go`（内存） |
| `paths` | `paths_darwin.go` | `paths_windows.go` | `paths_other.go`（XDG） |
| `daemon` 提权 | `launch_unix.go` + osascript | `launch_windows.go` + UAC runas | `launch_unix.go` + pkexec |
| `ui` Dock | `dock_darwin.go`（Cocoa CGO） | `dock_other.go`（no-op） | `dock_other.go`（no-op） |

---

## 开发指南

```bash
# 静态检查
make vet

# 格式化
make fmt

# 整理依赖
make tidy

# 清理构建产物
make clean
```

### 添加新平台适配

如需适配新的平台能力，参考现有约定新增带构建约束的文件，例如：

- TUN 实现 → `internal/tun/tun_<os>.go`
- 路由命令 → `internal/route/route_<os>.go`
- 密钥存储 → `internal/keychain/keychain_<os>.go`
- 路径解析 → `internal/paths/paths_<os>.go`

### 协议规范

客户端与服务端的完整通信协议见 [`docs/client-development.md`](docs/client-development.md)（含认证、握手、数据面、心跳、错误码等）。

---

## 注意事项

- **不支持移动端**：虽 Fyne 支持 Android/iOS，本项目未配置移动端构建目标与适配代码
- **Windows 仅 x64**：交叉编译仅目标 `GOARCH=amd64`，内置的 `wintun.dll` 为 x64 版本
- **Linux 密钥链待完善**：当前为内存存储，凭据不持久化，生产环境建议接入 Secret Service
- **CI/CD**：通过 GitHub Actions 自动构建 macOS / Windows 产物并发布 Release（见 [`.github/workflows/release.yml`](.github/workflows/release.yml)），打 `v*` tag 即触发
- **CGO 必开**：任何平台都不可关闭 CGO，否则 Fyne 无法编译
- **版本一致性**：GUI 与守护进程共享同一版本字符串，若版本不一致通常意味着旧守护进程仍在运行

---

## 许可证

详见仓库 LICENSE 文件。
