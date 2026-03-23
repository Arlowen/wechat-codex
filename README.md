# wechat-codex

`wechat-codex` 是一个基于 Go 语言重写的微信机器人轮询服务客户端。它使用了腾讯 iLink (微信机器人平台) 的 API 实现扫码登录，并支持在前台或后台守护进程中接收与处理微信消息。

## 特性

- **扫码登录体验**：直接在终端通过 ASCII 二维码（或点击链接）扫码授权。
- **现代化命令行**：基于业界标准的 `spf13/cobra` 框架构建。
- **后台守护进程**：原生支持将服务放入后台运行，自动管理 PID 与日志记录。
- **配置与缓存管理**：将运行时凭据自动存储在隐藏文件夹 `.runtime/wechat/` 内以策安全。

## 编译方法

确保你的机器上安装了 Go 1.16+。

你可以直接运行提供的快捷构建脚本（MacOS/Linux）：

```bash
./all_build.sh
```

或者手动使用 Go 命令编译：

```bash
go build -o wechat-codex .
```

## 使用指南

### 1. 扫码登录 (初次使用必执行)
在使用机器人服务前，必须先获取凭证（Token）。
```bash
./wechat-codex login
```
执行后终端会渲染一个二维码，请用拟作为机器人的微信账号扫描二维码，并在手机端点击确认授权。成功后，凭证会自动保存在本地。

### 2. 启动轮询服务
你可以选择两种方式来接收微信消息：

- **前台运行**（可直接看到实时日志，按 `Ctrl+C` 退出）：
  ```bash
  ./wechat-codex start
  ```

- **后台运行**（推荐）：
  ```bash
  ./wechat-codex start -d
  # 或
  ./wechat-codex start --daemon
  ```
  后台运行模式下，日志会自动输出到 `.runtime/wechat/service.log`。

### 3. 查看状态与停止服务
如果此前使用了后台运行模式，可以通过以下命令管理后台进程：

- **查看服务状态**：
  ```bash
  ./wechat-codex status
  ```
  
- **停止后台服务**：
  ```bash
  ./wechat-codex stop
  ```
