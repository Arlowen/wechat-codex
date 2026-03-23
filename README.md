# wechat-codex

`wechat-codex` 是一个基于 Go 语言编写的微信机器人客户端工具。它使用了腾讯 iLink (微信机器人平台) 的 API 实现扫码登录，原生支持长连接轮询收取微信消息，并能够在前台或后台守护进程中稳定运行。

## 🎯 核心功能

- **极简扫码登录**：终端直接渲染二维码，微信扫码一键完成授权。
- **稳定消息轮询**：基于 iLink API 稳定接收并处理微信消息。
- **进程守护管理**：原生支持将轮询服务一键挂载至操作系统后台运行。

## 🚀 一键安装

发布版本会在推送 `v*` tag 后自动构建并上传到 GitHub Releases，当前提供：

- macOS `amd64` / `arm64`
- Linux `amd64` / `arm64`

直接安装最新版本：

```bash
curl -fsSL https://raw.githubusercontent.com/Arlowen/wechat-codex/main/install.sh | bash
```

安装指定版本：

```bash
curl -fsSL https://raw.githubusercontent.com/Arlowen/wechat-codex/main/install.sh | WECHAT_CODEX_VERSION=v0.1.0 bash
```

安装到自定义目录：

```bash
curl -fsSL https://raw.githubusercontent.com/Arlowen/wechat-codex/main/install.sh | INSTALL_DIR="$HOME/.local/bin" bash
```

安装完成后可执行：

```bash
wechat-codex version
```

## 🧹 一键卸载

卸载已安装的二进制：

```bash
curl -fsSL https://raw.githubusercontent.com/Arlowen/wechat-codex/main/uninstall.sh | bash
```

如果是安装到了自定义目录：

```bash
curl -fsSL https://raw.githubusercontent.com/Arlowen/wechat-codex/main/uninstall.sh | INSTALL_DIR="$HOME/.local/bin" bash
```

如果还需要一并清理运行时数据 `~/.wechat-codex`：

```bash
curl -fsSL https://raw.githubusercontent.com/Arlowen/wechat-codex/main/uninstall.sh | WECHAT_CODEX_PURGE_DATA=1 bash
```

## 🛠 本地编译

确保你的环境中已安装 Go 1.25+。

**使用构建脚本（macOS/Linux）：**
```bash
./all_build.sh
```

**或者手动构建：**
```bash
go build -o bin/wechat-codex .
```

**本地生成 release 包：**
```bash
./scripts/build-release.sh v0.1.0
```

## 📖 使用指南

### 1. 启动服务与微信扫码登录
启动长连接接收微信消息的服务。如果是初次运行（或凭证过期），系统会自动在终端打印微信二维码，请直接扫码授权：

- **前台运行**（适用于调试环境，直接查看打印日志，按 `Ctrl+C` 退出）：
  ```bash
  bin/wechat-codex start
  ```

- **后台运行**（推荐生产环境使用，服务将在后台常驻）：
  ```bash
  bin/wechat-codex start -d
  # 也可以使用长参数 --daemon
  ```

### 2. 查看状态与停止服务
如果你将进程托管至后台，可以使用以下命令进行管理：

- **确认运行状态**：
  ```bash
  bin/wechat-codex status
  ```
  
- **安全停止服务**：
  ```bash
  bin/wechat-codex stop
  ```
