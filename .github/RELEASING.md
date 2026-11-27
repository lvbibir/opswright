# 正式发布签名配置

本 fork 只交付 Windows 客户端，不签名、不公证 macOS 产物。在 GitHub 仓库
Settings → Secrets and variables → Actions → Repository secrets 中按需配置：

| Secret | 内容 |
| --- | --- |
| `MYGO_UPDATER_PRIVATE_KEY` | `mygo-update.key` 的完整内容，与 `mygo.json` 的 `updates.publicKey` 配对；同目录 `.pub` 是公钥。不配置时构建跳过更新清单签名，自用可直接跑 exe |

生成密钥对：`go tool mygo keygen -o <目录>`，把输出的公钥写进 `mygo.json` 的
`updates` 段，私钥作为 `MYGO_UPDATER_PRIVATE_KEY` 传入构建。

## 工作流行为

1. `release.yml` 先检查标签与 `mygo.json` 版本一致，已有正式发布会拒绝覆盖；
   同时按 `cliff.toml` 用 git-cliff 生成上一个正式标签以来的发布说明，并等待同一
   标签上的 `ci.yml` 通过；`bump.sh` 的版本号提交之前的提交已在 main 上通过完整
   CI 时（仍在运行则等它结束），标签上的 `ci.yml` 直接复用该结果，不再重复测试和构建
2. `go tool mygo build` 构建并（配置了私钥时）签署 Windows（amd64）产物
3. 构建时把发布说明写入更新清单，供应用内更新窗口显示；验证更新清单有签名且带说明
4. 所有平台构建成功且 CI 通过后，汇总全部安装包、更新归档、delta 与更新清单，
   生成 `SHA256SUMS`，在发布说明后追加下载链接，先建草稿上传全部资产，再自动发布为 latest

Windows amd64 在 `windows-latest` 运行器上原生构建。

Windows 的普通 CI、beta 与正式 Release 都额外提供
`opswright-<版本>-windows-amd64-portable.zip`；ZIP 直接归档 MyGo 构建的程序及
完整资源，排除安装器、安装脚本和更新产物；正式 Release 将其纳入 `SHA256SUMS`
并提供下载链接，解压到可写目录后运行 `Opswright.exe` 即可，无需安装；应用数据仍保存在
`%APPDATA%\Opswright`，已保存凭据仍绑定当前 Windows 用户，不代表数据可随 ZIP 跨设备迁移。

本地可运行 `python3 .github/windows-portable-test.py` 验证 ZIP 内容、无签名 beta 产物
及下载链接，Windows CI 在归档前也会执行这些检查。

任一平台构建或 CI 失败时不会创建 Release；修复或重跑 CI 的失败 job 后，重跑本工作流的
失败 job 即可；之前运行留下的草稿会被替换，已发布的版本不会被覆盖。

发布用 `./bump.sh -v <x.y.z> -p`：只修改 `mygo.json` 的版本，提交
`chore(release): bump version to <x.y.z>`（不进入发布说明），创建 `v<x.y.z>` 标签并推送
分支与标签；不加 `-p` 只在本地提交和打标签；`v<version>-beta-<yyyymmddhhmmss>` 继续只
运行 `ci.yml`，不会触发正式签名发布。

允许工作区存在其他已暂存、未暂存或未跟踪改动，版本提交只包含 `mygo.json`，其他文件及其
暂存状态保持不变；`mygo.json` 本身已有的其他编辑会随该文件一并提交。

本地可运行 `python3 .github/bump-test.py`，在临时 Git 仓库中验证版本提交、混合改动保留
和推送流程，推送目标仅为测试创建的本地裸仓库。
