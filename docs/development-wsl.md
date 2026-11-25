# 本地开发指南 (WSL2, 仅 Windows 客户端)

本 fork (`lvbibir/opswright`) 在 WSL2 中开发, 只交付 Windows 客户端, 上游 `Licoy/sshyard` 保留为 `upstream` 远程用于跟进. 本文只写本机的环境与操作流程; 仓库规则 (测试范围、文案语言、代码风格等) 以 [AGENTS.md](../AGENTS.md) 为准.

## 环境现状

| 项 | 值 |
| --- | --- |
| Go | go1.27.4, 官方 tarball 安装于 `/usr/local/go` (go.mod 最低要求 1.27.3) |
| PATH | `/etc/profile.d/golang.sh`, 新开终端自动生效 |
| 依赖缓存 | `/root/go/pkg/mod` (GOPATH 默认 `/root/go`) |
| Git 远程 | `origin` = `lvbibir/opswright` (推送目标), `upstream` = `Licoy/sshyard` (跟进上游) |
| Windows 侧 | `/mnt/c/Users/lvbibir`, WSL interop 已启用 (可从 WSL 直接运行 .exe) |

不需要在 Windows 侧装 Go 或任何 C 工具链: 项目纯 Go、无 cgo, 在 WSL 里交叉编译即可.

## 从零安装 (换机器备忘)

```sh
V=go1.27.4
curl -fL -o /tmp/$V.linux-amd64.tar.gz "https://go.dev/dl/$V.linux-amd64.tar.gz"
sha256sum /tmp/$V.linux-amd64.tar.gz   # 与官网 https://go.dev/dl/ 公布的校验值比对
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf /tmp/$V.linux-amd64.tar.gz
sudo tee /etc/profile.d/golang.sh >/dev/null <<'EOF'
export PATH="/usr/local/go/bin:$HOME/go/bin:$PATH"
EOF
# 卸载: sudo rm -rf /usr/local/go /etc/profile.d/golang.sh ~/go
```

## 日常开发循环

```sh
cd /root/pi/sshyard  # 本机克隆目录名仍为 sshyard, 可自行 mv 改名

# 1. 首次或 go.mod / go.sum 变更后拉依赖 (约 11 秒)
go mod download

# 2. 逻辑测试 —— 无头运行, 只跑改动涉及的包 (范围规则见 AGENTS.md)
go test -race -count=1 ./internal/session/...                   # 换成本次改动的包
go test -race -count=1 ./internal/app -run '^TestAddHostFlow$'  # 单个用例示例

# 3. 出 Windows 版 (约 20 秒), 直接出到桌面, 构建完双击就能测
go tool mygo build -platform windows/amd64 -o /mnt/c/Users/lvbibir/Desktop
# 产物: /mnt/c/Users/lvbibir/Desktop/windows-amd64/Opswright.exe (GUI 子系统, 无控制台窗口)
# Windows 双击运行; 或 WSL 里直接执行 /mnt/c/Users/lvbibir/Desktop/windows-amd64/Opswright.exe

# 4. 改了 locales/*.json 后重新生成 catalog (mygo build 时也会自动检查)
GOFLAGS=-mod=mod go run github.com/egoist/mygo/cmd/mygo generate
```

提交前 (与 AGENTS.md 一致): `gofmt` 保持改动文件格式; `go vet` 只跑改动包; 测试只跑相关用例, 不跑全量.

## 本地测试用假服务器

```sh
# SSH 服务器: 支持密码/公钥认证、PTY、sftp、端口转发
go run ./cmd/devsshd -addr 127.0.0.1:2222 -user demo -password demo -root /tmp/remote
# 客户端里添加主机 127.0.0.1:2222, 用户 demo, 密码 demo

# HTTP/SOCKS5 代理 (测代理与动态转发)
go run ./cmd/devproxy -addr 127.0.0.1:3128
```

## 注意事项

**测试的边界.** 界面逻辑用 `ui.Tester` 无头驱动, 加上进程内假服务器 (`internal/sshtest`), 绝大多数逻辑与交互都能在 WSL 里测. 只有 Windows 专属的运行时行为 (ConPTY 本地终端、托盘、WSL 发行版探测等) 必须跑 exe 在 Windows 上实测. 不要拿 Linux 构建验证 Windows 行为; 本机有 WSLg, Linux 版 (`go run .`) 可用于快速冒烟, 但不代表 Windows 上的样子.

**数据目录隔离.**

- exe 默认把数据写到 Windows 用户配置目录 (`%APPDATA%\Opswright`), 与上游原版 (`%APPDATA%\SSHYard`) 互不影响.
- 便携模式: 在 exe 同目录放一个空的 `opswright_data` 文件夹 (Windows 与 Linux 都支持), 数据、缓存、日志就都留在那里; 删掉文件夹即回到默认目录.
- 或设环境变量 `OPSWRIGHT_DATA_DIR` (如 PowerShell: `$env:OPSWRIGHT_DATA_DIR = 'D:\dev\opswright-data'`), 设置后不再启用便携逻辑.

**安装器与更新.**

- 默认跳过 setup.exe 安装器 (构建日志有 "install NSIS" 提示): 需要时 `sudo apt install nsis` 再构建; 自用直接跑 exe, 不必装.
- 本 fork 不配置更新签名, 构建自动跳过更新清单 ("Not signing an update"), 不影响使用. 将来若要自更新: `go tool mygo keygen -o <目录>` 生成密钥对, 把输出的 `updates` 段写进 `mygo.json`, 构建时以 `MYGO_UPDATER_PRIVATE_KEY` 传入私钥. 注意 `mygo.json` 现有的 `publicKey` 是上游作者的密钥, 与本 fork 产物无关.

**上游同步.** 保持 fork 的改动少而集中 (品牌、配置、必要精简), 便于合并:

```sh
git fetch upstream
git merge upstream/main   # 或 git rebase upstream/main
```

提交信息保持 conventional commits (feat/fix/chore…), 与上游一致.

**CI.** 上游是私有仓库, 在 fork 上运行 Actions 消耗自己的分钟数; 纯文档改动会跳过 CI, 想彻底不跑可在 fork 的 Settings → Actions 禁用 (个人自用不需要 CI; 上游 `release.yml` 依赖作者签名密钥, 本 fork 用不了).

**安全与文案.** 不提交任何密钥 (`*.key` 已在 .gitignore); 用户可见文案必须中英双语齐全 (仅 `en` 与 `zh-Hans`) 并重新生成 catalog (见 AGENTS.md). 构建产物 (`/build/`, `/dist/`, `.mygo/` 等) 已在 .gitignore, 输出到仓库外或用 `-o` 指定即可, 不会弄脏工作区.
