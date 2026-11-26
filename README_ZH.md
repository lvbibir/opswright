<p align="left"><a href="README.md">English</a> · <strong>简体中文</strong></p>

<p align="center">
  <img src="resources/icon.png" alt="Opswright 图标" width="128" height="128">
</p>

<h1 align="center">Opswright</h1>

Opswright 是一款原生 Windows 桌面 SSH / SFTP 客户端，将终端、文件管理与 AI 助手集于一体。

## 截图

<table>
  <tr>
    <td width="50%" align="center">
      <img src="docs/terminal-ai-assistant.webp" alt="终端与 AI 助手" width="100%"><br>
      终端与 AI 助手
    </td>
    <td width="50%" align="center">
      <img src="docs/sftp-file-manager.webp" alt="SFTP 文件管理" width="100%"><br>
      SFTP 文件管理
    </td>
  </tr>
  <tr>
    <td width="50%" align="center">
      <img src="docs/system-monitor.webp" alt="系统监控" width="100%"><br>
      系统监控
    </td>
    <td width="50%" align="center">
      <img src="docs/ai-assistant-settings.webp" alt="AI 助手设置" width="100%"><br>
      AI 助手设置
    </td>
  </tr>
</table>

## 功能

- **终端**
  - 多标签、分屏与终端搜索，标签可拖动排序和重命名
  - 真彩色、宽字符、输入法，以及 tmux、vim、htop 鼠标操作
  - 十套内置配色方案并可导入 `.itermcolors`，支持关键字高亮、行号与时间戳，可调光标、行高和字宽
  - 多种输出编码，可按主机设置；Ctrl + 点击打开终端中的网址，Alt + 拖动进行矩形块选择
  - 输入时根据主机的 Shell 历史提示命令，按 → 或 End 采纳，并可列出文件夹中的文件来补全路径
  - 除 SSH 外还支持 Telnet、串口和本地终端（Windows 可选 PowerShell、CMD、Git Bash 与 WSL）
  - 同一主机的多个面板共享连接，断线自动重连，网络切换或从睡眠唤醒后立即重连并回到原来的文件夹

- **工作区**
  - 系统监控、进程占用、主机详情、Docker、快捷指令、历史指令、广播指令和会话日志都是可拖拽窗格，可停靠到任意窗格旁或其他标签页
  - 查看 Docker 容器与镜像的详情和资源占用，并直接启停容器
  - 广播指令与同步输入，会话日志可按文本或原始格式记录
  - 命令面板与可自定义的快捷键，可记忆窗口与分屏布局

- **文件管理**
  - SFTP 与终端并排，支持跟随终端目录，服务器没有 SFTP 时自动改用 SCP
  - 多标签内置编辑器，带目录树
  - 可通过 sudo 提权访问文件
  - 支持文件拖放和文件夹传输，上传保留权限和修改时间
  - 通过拖放或复制粘贴，在两台服务器之间直接复制文件和文件夹，数据经应用内存中转，不落本地磁盘
  - 传输队列支持暂停、续传和重试，传输期间保持电脑唤醒

- **AI 助手**
  - 每台主机独立的助手面板，支持执行命令、编辑文件，SSH、Telnet、串口和本地终端均可使用，对话按主机保存
  - 接入 AI 服务商，或使用自动检测到的本机 ACP 智能体（Claude Code、Codex、Grok、Gemini CLI 等），可同时添加多个，并选择各自支持的模型与思考等级；操作始终在远程主机上进行
  - 支持粘贴图片和文件作为附件
  - 询问、自动审核、完全允许三种权限模式，可配置审核模型、单轮工具调用上限和请求超时

- **智能体接入**
  - 通过开放 API、MCP 和 Skill 接入本机 AI 智能体
  - 查看主机、建立连接和执行命令，无需向智能体暴露已保存的凭据
  - 支持为 Claude Code、Codex 一键配置 Skill，接入功能默认关闭

- **主机与凭据**
  - 手动添加并分组主机，也可通过 `user@host:port` 快速连接
  - 支持密码、OpenSSH / PEM / PuTTY 密钥、OpenSSH 证书、SSH Agent 和无密码登录
  - 分组不限层级，可拖动整理，支持多选与测速
  - 凭据本地加密保存，支持生成密钥和安装公钥；主机密钥变化时核对指纹，并可管理已知主机

- **备份与恢复**
  - 密码加密备份主机、凭据、快捷指令和设置，支持跨设备恢复
  - 保存到本地、WebDAV、S3 兼容存储或 GitHub 私密 Gist
  - 支持手动与定时备份，可设置保留份数

- **网络连接**
  - 多级跳板机与 HTTP / SOCKS5 代理，AI 服务商和更新检查也可使用代理
  - 本地、远程和动态端口转发

- **日常体验**
  - 服务器状态监控
  - 浅色与深色主题，支持背景图和界面缩放
  - 可关闭到托盘，从托盘弹窗快速连接主机
  - 支持英文和简体中文

## 机密与主密码

请以普通桌面用户运行 Opswright,无需 root 或 `sudo`。保存和解锁凭据不需要任何额外配置:一个**主密码**把随机主密钥封存在 `master.enc` 中,主密钥再加密 `secrets.enc` 里 Opswright 保存的一切。

首次使用时 Opswright 会让你设置主密码。之后这台电脑会把它记在 Windows 凭据管理器里(由操作系统负责),以后打开无需再输入。手动锁定(快捷键、托盘、设置)后,本次运行内需要重新输入主密码;下次启动又会记住。空闲、锁屏与休眠只会隐藏窗口,不会要求密码。

把数据文件夹拷到另一台电脑或另一个系统用户时,`master.enc` 会跟着走,该电脑首次打开需要输入主密码,之后也会记住。Opswright 不会保存主密码:忘记后已保存的机密只能从备份找回,锁屏上的重置会删除它们。**设置 → 关于 → 机密存储**可以修改主密码,也可以让本机不再记住(下次重新要求输入)。

## 开发

基于 [mygo](https://github.com/egoist/mygo) 构建，需要 Go 1.27.3（可借助 `GOTOOLCHAIN` 自动下载），不需要 cgo。

```sh
# 运行
go run .

# 运行开发版 app
scripts/run-dev-app.sh

# 把数据放到单独的目录
OPSWRIGHT_DATA_DIR=/tmp/demo go run .

# 本地测试用 SSH 服务器
go run ./cmd/devsshd -addr 127.0.0.1:2222 -user demo -password demo -root /tmp/remote

# 本地测试用 HTTP / SOCKS 代理
go run ./cmd/devproxy -addr 127.0.0.1:3128

# 测试与检查
go test -race -count=1 ./...
go vet ./...
gofmt -l .

# 修改 locales/*.json 后重新生成多语言目录
GOFLAGS=-mod=mod go run github.com/egoist/mygo/cmd/mygo generate

# 打包到 dist/
go tool mygo build -platform windows/amd64 -o dist
```

连接 devsshd 测试时，新建主机 `127.0.0.1`，端口 `2222`，用户 `demo`，密码 `demo`。

开发约定见 [AGENTS.md](AGENTS.md)。

## 社区支持

- [LinuxDO](https://linux.do)

## 开源协议

Opswright 基于 [GNU Affero General Public License v3.0](LICENSE) 开源。
