# BT Code Review

BTCR 是本地代码审核 CLI，支持 ChatGPT 订阅、OpenAI 兼容 API 和浏览器会话查看器。

## 安装

需要 Node.js 18 或更高版本和 Git。以下命令从 GitHub Release 安装预编译程序，适用于 macOS、Linux 和 Windows 的 x64、arm64 架构。

```sh
npm install -g https://github.com/jccapril/bt-code-review/releases/download/v0.1.6/bt-code-review-0.1.6.tgz
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

## 用户全局审核规则

从 v0.1.6 起， `btcr review` 会自动读取 `~/.btcodereview/rule.json`，供同一用户在多个仓库中使用。v0.1.5 使用旧路径 `~/.opencodereview/rule.json`。

如果已有 OCR 全局规则，可以在 macOS 或 Linux 上复制一份作为 BTCR 的初始配置：

```sh
mkdir -p ~/.btcodereview
cp -n ~/.opencodereview/rule.json ~/.btcodereview/rule.json
```

`cp -n` 会保留已有的 BTCR 配置。如果规则通过相对路径引用 Markdown 等文件，也要将这些文件按原目录结构复制到 `~/.btcodereview/`。

例如，要给 Objective-C 和 Swift 文件补充一条个人规则，可以在 `~/.btcodereview/rule.json` 中写入：

```json
{
  "rules": [
    {
      "path": "**/*.{h,m,mm,swift}",
      "rule": "检查异步回调更新 UI 时是否切回主线程。",
      "merge_system_rule": true
    }
  ]
}
```

`merge_system_rule` 设为 `true` 会同时应用匹配语言的内置规则。在 Git 仓库中运行 `btcr rules check Example.swift`，输出中的 `Source: global` 表示命中了用户全局规则。

## 项目审核规则

将团队共用的规则写入 `<仓库根目录>/.btcodereview/rule.json`，格式与上面的用户全局规则相同。使用旧路径的项目，可以把规则文件从仓库内的 `.opencodereview/rule.json` 复制到新位置。

BTCR 对每个文件按 `--rule` 指定的规则、项目规则、用户全局规则、内置规则的顺序查找。运行 `btcr rules check Example.swift`，输出 `Source: project` 表示该文件命中了项目规则。

## 打包

维护者可以运行 `npm pack` 构建包含六个平台程序的安装包。推送与 `package.json` 版本一致的 `vX.Y.Z` 标签会触发测试、打包与 GitHub Release 发布。
