# tinycs-cli

[![CI](https://github.com/tinycs-cn/tinycs-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/tinycs-cn/tinycs-cli/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/tinycs-cn/tinycs-cli?logo=github)](https://github.com/tinycs-cn/tinycs-cli/releases/latest)

Tinycs 平台命令行工具，用于提交课程代码并获取实时评测结果。

## 安装

### macOS / Linux（推荐）

```bash
curl -fsSL https://github.com/tinycs-cn/tinycs-cli/releases/latest/download/install.sh | sh
```

### Windows（PowerShell）

```powershell
irm https://github.com/tinycs-cn/tinycs-cli/releases/latest/download/tinycs-windows-amd64.exe -OutFile "$env:USERPROFILE\tinycs.exe"
```

> 下载后将 `tinycs.exe` 所在目录加入系统 PATH，或移动到已在 PATH 中的目录。

### 从源码构建（需 Go 1.23+，三端通用）

```bash
go install github.com/tinycs-cn/tinycs-cli/cmd/tinycs@latest
```

安装完成后验证：

```bash
tinycs version
```

## 快速上手

### 1. 登录

```bash
# 浏览器授权登录（推荐）
tinycs login

# 或使用 Token 登录
tinycs login --token tcs_xxx
```

### 2. 进入课程目录

课程目录中需要有 `tinycs.yml` 文件：

```yaml
course: tinydsa
language: python
```

### 3. 提交代码

```bash
tinycs submit
```

提交后 CLI 会实时输出评测日志，评测通过时自动解锁下一关。

## 命令参考

### `tinycs login`

| 选项              | 说明                                |
| ----------------- | ----------------------------------- |
| `--token <token>` | 使用 API Token 登录，跳过浏览器授权 |

### `tinycs submit`

| 选项              | 说明                   |
| ----------------- | ---------------------- |
| `--stage <slug>`  | 指定评测关卡           |
| `--dry-run`       | 仅预览打包文件，不上传 |
| `--message <msg>` | 自定义提交备注         |

### `tinycs stages`

列出当前课程的所有关卡序号、Slug 和名称。

```bash
tinycs stages
```

### `tinycs version`

显示版本号和构建信息。

## 文件排除规则

提交基于 `git push`，排除规则与 git 一致：

- `.gitignore` 中列出的文件自动排除
- `node_modules/`、`__pycache__/`、`.venv/`、`target/` 等常见构建目录若未在 `.gitignore` 中，CLI 会在提交前报错提示添加

提交前会自动检查：

- **二进制/依赖包/编译产物** — 检测到后报错，需加入 `.gitignore`
- **总文件大小超过 20 MB** — 超限后报错，需清理大文件

使用 `--dry-run` 可预览将执行的 git 操作。

## 环境变量

| 变量           | 说明                                   |
| -------------- | -------------------------------------- |
| `TINYCS_TOKEN` | API Token，优先于配置文件（适用于 CI） |
| `NO_COLOR`     | 设置后禁用彩色输出                     |

## 配置文件

登录后凭证保存在 `~/.tinycs/config.yml`（权限 `0600`）。

## 发布流程（维护者）

1. 确认 `main` 上 CI 全绿
2. 打 tag 并推送：

   ```bash
   git tag -a v0.1.0 -m "v0.1.0"
   git push origin v0.1.0
   ```

3. `.github/workflows/release.yml` 会自动跑 GoReleaser，产出多平台二进制 + checksums，发到 GitHub Releases

4. 学员 starter 的 `tinycs-submit.yml` 默认拉 `releases/latest/download/tinycs-linux-amd64`，发布后立刻生效

**本地预演**（无需打 tag、不会 push）：

```bash
brew install goreleaser
make snapshot         # 产出到 dist/
make release-check    # 仅校验配置语法
```
