# tinycs test — 本地评测命令设计

## 背景

`tinycs submit` 将代码推送到远程服务器评测，每次需要网络且有延迟。
tester 二进制（如 `tinynum-tester`）已支持本地运行：

```
tester -d <project-dir> [-s <stage-slug>]
```

`tinycs test` 命令封装这一能力，让学生在提交前本地快速验证。

---

## 命令设计

```
tinycs test                      # 测试当前关卡
tinycs test --stage <slug>       # 指定关卡
tinycs test --all                # 测试所有关卡
```

读取当前目录的 `tinycs.yml` 获取 `course` 和 `language`，与 `tinycs submit` 保持一致。

---

## tester 二进制管理

### 缓存路径

```
~/.tinycs/testers/<course>/<version>/tester
```

示例：`~/.tinycs/testers/tinynum/v0.4.0/tester`

### 版本信息来源：GitHub Releases

tester 每门课对应一个独立 GitHub 仓库（如 `tinycs-cn/tinynum-tester`），按 tag 发布。
版本管理完全依赖 GitHub Releases，**无需 tinycs-api 新增任何端点**。

- **查询最新版本**：

  ```
  GET https://api.github.com/repos/tinycs-cn/{course}-tester/releases/latest
  → { "tag_name": "v0.4.0", "assets": [{"name": "tinynum-tester-darwin-arm64", "browser_download_url": "..."}] }
  ```

- **下载 URL 规律**（直接构造，无需解析 assets）：

  ```
  https://github.com/tinycs-cn/{course}-tester/releases/download/{tag}/{course}-tester-{platform}
  ```

- **TTL**：本地缓存 meta.json 记录版本号，24h 内跳过 GitHub API 查询。

### 生命周期（`ensureTester(course) → (path, error)`）

```
1. 读取本地缓存元数据：~/.tinycs/testers/<course>/meta.json
   { "version": "v0.4.0", "cached_at": "..." }

2. 若缓存未超过 24h → 直接返回缓存路径

3. 否则查询 GitHub Releases latest API 获取最新 tag

4. 若本地版本 == 最新版本 → 更新 cached_at，返回缓存路径

5. 否则构造下载 URL → 下载二进制 → chmod +x → 更新 meta.json
```

### 平台适配

客户端按 `runtime.GOOS`/`runtime.GOARCH` 自动选择，无需服务端介入：

| GOOS   | GOARCH | 文件名后缀     | 运行方式   |
| ------ | ------ | -------------- | ---------- |
| darwin | arm64  | `darwin-arm64` | 原生二进制 |
| darwin | amd64  | `darwin-amd64` | 原生二进制 |
| linux  | amd64  | `linux-amd64`  | 原生二进制 |
| linux  | arm64  | `linux-arm64`  | 原生二进制 |

### Windows 支持（Docker 模式）

`tester-utils` 使用了 `Setpgid`、`syscall.Kill` 等 Unix-only API（进程组管理），
无法直接交叉编译为 Windows 二进制。

Windows 上改用 Docker 容器运行 tester，行为与服务端评测环境完全一致：

```bash
docker run --rm \
  -v "<projectDir>:/workspace" \
  ghcr.io/tinycs-cn/{course}-tester:<version> \
  -s <stage> -d /workspace
```

**实现方式**：`ensureTester` 在 `runtime.GOOS == "windows"` 时返回一个 `dockerRunner`
而非文件路径，`TestCommand` 统一通过 `testerRunner` 接口调用，无需感知差异。

**前提**：用户需安装 [Docker Desktop for Windows](https://www.docker.com/products/docker-desktop/)。
首次运行会拉取镜像（~50MB），后续版本更新与原生路径一样通过 GitHub Releases tag 驱动。

> Windows Docker 模式为独立 milestone，当前版本（v0.4.0）仅支持 macOS / Linux。

### tester release.yml 已支持多平台构建

tinynum-tester release.yml 已更新，构建 4 个平台的二进制
（Windows 不发布原生二进制，见上方 Windows Docker 模式说明）：

```yaml
- name: Build binaries
  run: |
    CGO_ENABLED=0 GOOS=linux  GOARCH=amd64 go build -ldflags="-s -w" -o tinynum-tester-linux-amd64  .
    CGO_ENABLED=0 GOOS=linux  GOARCH=arm64 go build -ldflags="-s -w" -o tinynum-tester-linux-arm64  .
    CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w" -o tinynum-tester-darwin-amd64 .
    CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o tinynum-tester-darwin-arm64 .

- name: Create GitHub Release
  uses: softprops/action-gh-release@v2
  with:
    files: tinynum-tester-*
    generate_release_notes: true
```

---

## 实现骨架

### 新增文件

**`internal/commands/test.go`**

```go
func TestCommand(args []string) error {
    flags := flag.NewFlagSet("test", flag.ContinueOnError)
    stage       := flags.String("stage", "", "指定评测关卡 (slug)")
    all         := flags.Bool("all", false, "测试所有关卡")
    testerPath  := flags.String("tester-path", "", "直接指定本地 tester 二进制路径（调试用）")
    if err := flags.Parse(args); err != nil { return err }

    cfg, err := config.Load()
    if err != nil { return fmt.Errorf("加载配置失败: %w", err) }
    token := cfg.GetToken()
    if token == "" {
        return errors.New("未登录，请先运行: tinycs login")
    }

    course, language, projectDir, err := resolveProject()
    if err != nil { return err }

    path := *testerPath
    if path == "" {
        path, err = ensureTester(course)
        if err != nil { return err }
    }

    cmdArgs := []string{"-d", projectDir}
    if *stage != "" {
        cmdArgs = append(cmdArgs, "-s", *stage)
    } else if !*all {
        // 默认：调 GET /v1/current-stage?course=&language= 获取当前关卡 slug
        // 顺序课程返回 StageItem（含 slug）；freeform 返回 400（需用户显式传 --stage）
        slug, err := resolveCurrentStageFromAPI(course, language, cfg)
        if err != nil { return err }
        cmdArgs = append(cmdArgs, "-s", slug)
    }

    cmd := exec.Command(path, cmdArgs...)
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    return cmd.Run()
}
```

**`internal/commands/tester_cache.go`**

```go
type testerMeta struct {
    Version  string    `json:"version"`
    CachedAt time.Time `json:"cached_at"`
}

const testerCacheTTL = 24 * time.Hour

func ensureTester(course string) (string, error) {
    home, _ := os.UserHomeDir()
    cacheDir   := filepath.Join(home, ".tinycs", "testers", course)
    metaPath   := filepath.Join(cacheDir, "meta.json")
    testerPath := filepath.Join(cacheDir, "tester")

    // 1. 读本地 meta，若 cached_at 未超过 TTL 直接返回
    // 2. GET https://api.github.com/repos/tinycs-cn/{course}-tester/releases/latest
    // 3. 若版本一致，更新 cached_at 返回
    // 4. 构造下载 URL，按 runtime.GOOS+runtime.GOARCH 选平台
    // 5. 下载 → os.WriteFile → os.Chmod(testerPath, 0755) → 更新 meta
}
```

### 注册命令

在 `cmd/tinycs/main.go` 中添加：

```go
case "test":
    err = commands.TestCommand(args[1:])
```

---

## 与 `tinycs submit` 集成（可选阶段二）

`submit` 前自动跑本地测试，失败时提示：

```
⚠ 本地测试未通过 (3/17)，仍要提交？(y/N)
```

通过 `--skip-test` flag 跳过。

---

## API 依赖说明

| 功能            | API 依赖                                              |
| --------------- | ----------------------------------------------------- |
| tester 版本管理 | 无（依赖 GitHub Releases，public 仓库，无需认证）     |
| 当前 stage 推断 | `GET /v1/current-stage?course=<slug>&language=<slug>` |

**stage 推断流程（单次调用）**：

```
GET /v1/current-stage?course=tinynum&language=python
→ 200: { slug: "storage-and-shape", ... }
→ 400: freeform 课程 → 提示加 --stage <slug>（tinycs stages 查看列表）
→ 404: 未注册该课程 → 提示先在网页端注册
```

服务端内部用 `GetActiveRepositoryByCourseAndLanguage(userID, courseSlug, languageSlug)` 完成 join，客户端无需持有 repo UUID。

---

## 实施步骤

| 步骤 | 内容                                                                             | 依赖                |
| ---- | -------------------------------------------------------------------------------- | ------------------- |
| 1    | ~~扩展 tester release.yml，构建 4 平台二进制（去除 Windows）~~ ✅ 已完成         | tinynum-tester repo |
| 2    | ~~实现 `ensureTester`（GitHub Releases 查询+缓存，macOS/Linux）~~ ✅ 已完成      | 步骤 1              |
| 3    | ~~tinycs-api 新增 `GET /v1/current-stage?course=&language=`~~ ✅ 已完成          | —                   |
| 4    | ~~实现 `TestCommand`~~ ✅ 已完成                                                 | 步骤 2、3           |
| 5    | ~~注册到 main.go~~ ✅ 已完成                                                     | 步骤 4              |
| 6    | submit 集成（可选）                                                              | 步骤 4              |
| 7    | Windows Docker 模式：`testerRunner` 接口 + `dockerRunner` 实现（独立 milestone） | 步骤 1              |

步骤 2-5 可在步骤 1 完成前用 `--tester-path <local>` flag 先联调本地 tester 二进制。
