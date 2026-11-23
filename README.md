# BT Code Review

BTCR 是本地代码审核 CLI，支持 ChatGPT 订阅、OpenAI 兼容 API 和浏览器会话查看器。

## 安装

需要 Node.js 18 或更高版本和 Git。以下命令从 GitHub Release 安装预编译程序，适用于 macOS、Linux 和 Windows 的 x64、arm64 架构。

```sh
npm install -g https://github.com/jccapril/bt-code-review/releases/download/v0.1.5/bt-code-review-0.1.5.tgz
btcr --version
```

## 使用

```sh
btcr config provider
btcr review --preview
btcr review
btcr viewer --open
```

运行 `btcr config provider` 后，使用方向键选择 ChatGPT plan、OpenAI API（Chat Completions）或 OpenAI Responses API，按 Enter 确认，再从当前账号的模型列表中选择。首次使用订阅模式会打开浏览器授权；API 模式会复用已有密钥，也支持隐藏输入。

使用 API 时，设置环境变量 `BTCR_API_KEY` 或 `OPENAI_API_KEY`，运行 `btcr config api`，再运行 `btcr config model`。Responses 协议对应 `btcr config responses`，也支持 `--model`、`--base-url` 和 `--api-key-stdin`。自定义接口可通过 `btcr config api --base-url URL` 配置。

单独切换模型时，运行 `btcr config model`，使用 ↑ / ↓ 选择，按 Enter 保存或 Ctrl+C 取消。只查看列表可运行 `btcr config model --list`。

## 打包

维护者可以运行 `npm pack` 构建包含六个平台程序的安装包。推送与 `package.json` 版本一致的 `vX.Y.Z` 标签会触发测试、打包与 GitHub Release 发布。
