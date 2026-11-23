# BT Code Review

BTCR 是本地代码审核 CLI，支持 ChatGPT 订阅、OpenAI 兼容 API 和浏览器会话查看器。

## 安装

需要 Node.js 18 或更高版本和 Git。以下命令从 GitHub Release 安装预编译程序，适用于 macOS、Linux 和 Windows 的 x64、arm64 架构。

```sh
npm install -g https://github.com/jccapril/bt-code-review/releases/download/v0.1.4/bt-code-review-0.1.4.tgz
btcr --version
```

## 使用

```sh
btcr config chatgpt
btcr login
btcr models
btcr review --preview
btcr review
btcr viewer --open
```

使用 API 时，设置环境变量 `BTCR_API_KEY` 或 `OPENAI_API_KEY`，运行 `btcr config api`，再运行 `btcr models`。自定义接口可通过 `btcr config api --base-url URL` 配置。

## 打包

维护者可以运行 `npm pack` 构建包含六个平台程序的安装包。推送与 `package.json` 版本一致的 `vX.Y.Z` 标签会触发测试、打包与 GitHub Release 发布。
