# wechat-codex

`wechat-codex` 是一个基于 Go 语言编写的微信机器人客户端工具。它使用了腾讯 iLink (微信机器人平台) 的 API 实现扫码登录，原生支持长连接轮询收取微信消息，并能够在前台或后台守护进程中稳定运行。

## 🚀 一键安装

```bash
curl -fsSL https://raw.githubusercontent.com/Arlowen/wechat-codex/main/install.sh | bash
```

## ⬆️ 一键升级

```bash
curl -fsSL https://raw.githubusercontent.com/Arlowen/wechat-codex/main/upgrade.sh | bash
```

升级脚本会优先复用当前安装目录并覆盖更新二进制，默认保留 `~/.wechat-codex` 下的运行时数据。
如果后台守护进程已经在运行，升级完成后执行 `wechat-codex stop && wechat-codex start -d` 以切换到新版本。

## 🧹 一键卸载

```bash
curl -fsSL https://raw.githubusercontent.com/Arlowen/wechat-codex/main/uninstall.sh | bash
```
