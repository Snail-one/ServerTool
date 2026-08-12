# Linux CLI 安装与自更新标准

状态：正式规范  
适用范围：发布到 GitHub Releases 的 Linux 单文件 CLI 程序  
参考实现：ServerTool

## 1. 目标

所有项目应提供一致、可预期的安装与更新体验：

- 同一脚本同时支持首次安装、升级和损坏修复。
- 自动识别 Linux amd64、arm64。
- 版本、系统、架构和发布文件一一对应。
- 下载前判断是否需要更新，下载后必须校验完整性。
- 替换前必须验证新程序可以执行；失败不得破坏旧程序。
- CLI 统一提供 `update` 和 `--version` 参数。
- 安装、更新和构建脚本统一放在 `scripts/` 目录。

本文中的“必须”“不得”为强制要求，“建议”为推荐要求。

## 2. 标准参数

每个项目接入时必须明确以下参数：

| 参数 | 通用含义 | ServerTool 取值 |
| --- | --- | --- |
| `OWNER/REPOSITORY` | GitHub 仓库 | `Snail-one/ServerTool` |
| `PRODUCT_ID` | `--version` 第一列的稳定标识 | `snailtool` |
| `ASSET_PREFIX` | Release 二进制文件前缀 | `snailtool` |
| `COMMAND_NAME` | 安装后的命令名 | `snail` |
| `INSTALL_DIR` | 系统安装目录 | `/usr/local/sbin` |
| `VERSION_TAG` | 发布版本 | `v1.2.0` |
| `SCRIPT_URL` | 仓库安装脚本地址 | `https://raw.githubusercontent.com/Snail-one/ServerTool/main/scripts/install.sh` |

项目的程序名、命令名可以不同，但确定后不得在不同发布版本间随意变化。

## 3. 目录约定

```text
scripts/
├── install.sh                 # 首次安装、更新和修复
├── build_linux.sh             # Linux 本地构建
├── build_windows.ps1          # Windows 交叉构建
└── generate_release_notes.sh  # Release Notes 生成
```

自更新代码放在项目自身的源代码目录内，例如：

```text
internal/selfupdate/update.go
```

## 4. 版本标准

### 4.1 版本来源

正式版本必须来自 Git 标签，建议使用语义化版本：

```text
v主版本.次版本.修订版本
```

示例：

```text
v1.0.0
v1.3.2
v2.0.0-rc.1
```

用于文件名的版本只允许：

```text
A-Z a-z 0-9 . _ -
```

### 4.2 编译注入

源代码保留开发默认值：

```go
var Version = "dev"
```

正式构建时必须通过链接参数注入标签版本、提交和构建时间：

```bash
go build -trimpath \
  -ldflags="-s -w \
  -X example/internal/version.Version=${VERSION} \
  -X example/internal/version.Commit=${COMMIT} \
  -X example/internal/version.BuildDate=${BUILD_DATE}"
```

### 4.3 版本输出契约

每个程序必须支持：

```bash
command --version
```

第一行必须采用稳定、可解析的格式：

```text
<PRODUCT_ID> <VERSION_TAG>
```

示例：

```text
snailtool v1.2.0
```

后续行可以显示提交、构建时间、Go 版本和平台，但安装脚本只能依赖第一行。

## 5. Release 文件标准

### 5.1 二进制命名

```text
<ASSET_PREFIX>_<OS>_<ARCH>_<VERSION_TAG>
```

示例：

```text
snailtool_linux_amd64_v1.2.0
snailtool_linux_arm64_v1.2.0
```

### 5.2 校验文件命名

```text
checksums_<VERSION_TAG>.txt
```

示例：

```text
checksums_v1.2.0.txt
```

内容使用 `sha256sum` 标准格式：

```text
<SHA256>  snailtool_linux_amd64_v1.2.0
<SHA256>  snailtool_linux_arm64_v1.2.0
```

不得把校验文件自身写入校验文件。

## 6. 发布标准

发布流程必须按以下顺序执行：

1. 运行全部测试。
2. 从标签或手动输入取得 `VERSION_TAG`。
3. 校验版本字符是否合法。
4. 分架构编译，并通过链接参数注入版本信息。
5. 使用标准文件名上传构建产物。
6. 汇总所有架构产物并生成 `checksums_<VERSION_TAG>.txt`。
7. 创建 GitHub Release 并上传二进制和校验文件。
8. 生成 Release Notes。

推荐的 GitHub Actions 核心配置：

```yaml
- name: Build
  run: |
    VERSION="${{ github.ref_name }}"
    COMMIT="${GITHUB_SHA}"
    BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    LDFLAGS="-s -w -X example/internal/version.Version=${VERSION} -X example/internal/version.Commit=${COMMIT} -X example/internal/version.BuildDate=${BUILD_DATE}"
    go build -trimpath -ldflags="${LDFLAGS}" \
      -o "dist/<ASSET_PREFIX>_linux_<ARCH>_${VERSION}" ./cmd/<COMMAND>

- name: Generate checksums
  run: sha256sum <ASSET_PREFIX>_* > "checksums_${RELEASE_TAG}.txt"

- name: Publish release
  uses: softprops/action-gh-release@v3
  with:
    tag_name: ${{ env.RELEASE_TAG }}
    name: ${{ env.RELEASE_TAG }}
    generate_release_notes: true
    files: release-assets/*
```

个人直接向主分支提交时，建议额外通过 `git log <旧标签>..<新标签>` 生成 Markdown 提交列表，并作为 `body_path` 传给发布 Action。GitHub 原生 Release Notes 继续用于补充 PR、贡献者和完整变更链接。

## 7. 安装与更新流程

标准流程如下：

```text
启动脚本
  → 权限与平台预检
  → 查询目标 Release 版本
  → 计算当前架构对应的文件名
  → 下载 SHA-256 校验文件
  → 检测本地版本与本地文件哈希
      ├─ 版本相同且哈希相同：直接结束
      ├─ 版本相同但哈希不同：重新下载修复
      ├─ 版本不同：下载更新
      └─ 文件不存在或无法执行：下载安装
  → 显示二进制下载进度
  → 校验下载文件 SHA-256
  → 执行新文件的 --version
  → 写入同目录临时文件
  → 原子替换目标命令
  → 清理临时文件
```

### 7.1 启动前预检

安装脚本必须：

- 使用 `set -eu`。
- 要求 root 权限，或明确支持用户级安装。
- 只接受绝对安装目录。
- 拒绝包含路径分隔符的命令名。
- 拒绝包含非法字符的版本。
- 确认系统为 Linux。
- 把 `x86_64`、`amd64` 统一映射为 `amd64`。
- 把 `aarch64`、`arm64` 统一映射为 `arm64`。
- 检查 `curl` 或 `wget`。
- 检查 `sha256sum` 或 `shasum`。
- 使用 `mktemp -d` 创建独立临时目录，并通过 `trap` 清理。

### 7.2 获取目标版本

默认更新到最新正式 Release：

```text
GET https://api.github.com/repos/<OWNER>/<REPOSITORY>/releases/latest
```

从响应的 `tag_name` 获取版本。

脚本还必须支持指定版本：

```bash
sudo sh scripts/install.sh v1.2.0
```

指定版本时直接使用对应标签，不查询 latest。

### 7.3 获取校验值

必须先下载体积较小的校验文件，再决定是否下载二进制：

```text
https://github.com/<OWNER>/<REPOSITORY>/releases/download/<VERSION>/checksums_<VERSION>.txt
```

从校验文件中只读取当前系统和架构对应资产的 SHA-256。

### 7.4 判断安装、更新或修复

| 本地状态 | 处理方式 |
| --- | --- |
| 目标文件不存在 | 下载并安装 |
| 目标文件不可执行 | 下载并修复 |
| `--version` 无法解析 | 下载并修复 |
| 本地版本低于或不同于目标版本 | 下载并更新 |
| 本地版本与目标版本相同，SHA-256 相同 | 不下载二进制，直接成功退出 |
| 本地版本与目标版本相同，SHA-256 不同 | 警告文件异常，重新下载修复 |

版本相同不能作为跳过下载的唯一条件，必须同时验证本地文件 SHA-256。

### 7.5 下载要求

- Release 元数据和校验文件可以静默下载。
- 二进制必须显示进度。
- 必须跟随 HTTPS 重定向。
- 必须把 HTTP 非成功状态视为失败。
- 建议配置连接超时和最多三次重试。
- 下载必须写入临时目录，不得直接覆盖正式文件。

### 7.6 下载后验证

下载完成后必须依次验证：

1. 实际 SHA-256 与 Release 校验文件一致。
2. 文件具有可执行权限。
3. 执行 `<下载文件> --version` 成功。
4. 第一行版本与目标版本一致，建议作为强校验实施。

任何验证失败都必须停止，不得修改已安装程序。

### 7.7 原子替换

新程序必须先复制到目标目录中的临时文件：

```text
/usr/local/sbin/.<COMMAND_NAME>.new.<PID>
```

验证和复制成功后，通过同一文件系统内的 `mv` 替换：

```bash
mv -f "$STAGED_FILE" "$TARGET"
```

不得把网络下载输出直接写入正式目标路径。这样在下载、校验或复制失败时，旧程序仍然可用。

## 8. CLI 自更新标准

每个程序统一支持：

```bash
sudo <COMMAND_NAME> update
```

CLI 本身不重复实现 Release 解析和安装逻辑，只负责安全地获取仓库中的标准安装脚本，然后调用它。

推荐流程：

1. 检查更新所需权限。
2. 显示安装脚本的完整 HTTPS 下载地址，便于用户核对更新来源。
3. 通过 HTTPS 下载仓库 `main` 分支的 `scripts/install.sh`。
4. 设置 30 秒请求超时。
5. HTTP 状态必须为 200。
6. 限制脚本最大尺寸，建议不超过 1 MiB。
7. 检查内容以 `#!/bin/sh` 开头。
8. 写入权限为 `0600` 的临时文件。
9. 通过 `sh <临时文件>` 执行，并透传标准输入、输出和错误。
10. 执行结束后删除临时文件。
11. 安装脚本负责后续版本判断、校验、下载和替换。

直接读取 `main` 分支脚本可以立即获得最新修复逻辑，但也意味着仓库主分支是更新信任源。高安全项目建议改为读取固定标签中的脚本，并增加签名验证。

## 9. 输出标准

统一使用以下前缀：

```text
[INFO] 正常阶段和结果
[WARN] 可恢复异常或自动修复
[ERROR] 导致流程失败的错误
```

标准输出示例。

无需更新：

```text
[INFO] 正在查询最新版本...
[INFO] 当前版本：example v1.2.0
[INFO] 当前已是最新版本，文件校验通过，无需更新
```

正常更新：

```text
[INFO] 正在查询最新版本...
[INFO] 当前版本：example v1.1.0
[INFO] 正在下载 example_linux_amd64_v1.2.0...
############################ 100.0%
[INFO] SHA-256 校验通过
[INFO] 更新完成：/usr/local/sbin/example
[INFO] 当前版本：example v1.2.0
```

同版本损坏修复：

```text
[INFO] 当前版本：example v1.2.0
[WARN] 当前版本号相同，但文件校验失败，将重新下载修复
[INFO] 正在下载 example_linux_amd64_v1.2.0...
```

## 10. 错误处理标准

| 失败位置 | 必须行为 |
| --- | --- |
| Release API 不可用 | 报错退出，不修改旧程序 |
| 不支持的系统或架构 | 明确显示系统或架构并退出 |
| 找不到对应校验值 | 报错退出 |
| 二进制下载失败 | 清理临时文件，保留旧程序 |
| SHA-256 不一致 | 报错退出，保留旧程序 |
| 新文件不能执行 | 报错退出，保留旧程序 |
| 写入安装目录失败 | 报错退出，保留旧程序 |
| 原子替换失败 | 报错退出并清理暂存文件 |

退出码必须满足：成功为 `0`，失败为非 `0`。

## 11. 安全边界

本标准的基础安全保证包括：

- 所有远程请求使用 HTTPS。
- Release 文件使用 SHA-256 校验。
- 本地同版本程序也与 Release 哈希比对。
- 下载文件在替换前实际执行 `--version`。
- 临时文件权限受限并自动清理。
- 使用原子替换避免半写入文件。
- 对版本、命令名、目录和下载尺寸进行限制。

SHA-256 只能验证文件与 Release 中的校验记录一致，不能防御 GitHub 账号、仓库和 Release 同时被篡改。对供应链安全要求较高的项目，建议增加以下能力：

- 使用 Cosign、Minisign 或 GPG 对产物签名。
- 在安装程序中内置公钥并验证签名。
- 对 Actions 依赖使用完整提交 SHA，而不是浮动标签。
- 启用 GitHub 环境审批、分支保护和标签保护。

## 12. 兼容策略

迁移旧项目时，可以临时兼容无版本后缀的旧资产：

```text
<ASSET_PREFIX>_linux_<ARCH>
checksums.txt
```

标准脚本应优先请求新版文件名；新版校验文件不存在时才回退旧格式。所有新 Release 必须只使用版本化文件名。

## 13. 项目接入清单

复制 ServerTool 的参考实现后，至少修改：

- [ ] `REPOSITORY`
- [ ] 环境变量前缀，例如 `SERVERTOOL_`
- [ ] `PRODUCT_ID`
- [ ] `ASSET_PREFIX`
- [ ] `BINARY_NAME`
- [ ] 默认安装目录
- [ ] `InstallScriptURL`
- [ ] Go 版本包路径和 `-ldflags -X` 路径
- [ ] GitHub Actions 构建入口 `./cmd/<COMMAND>`
- [ ] Release 资产矩阵
- [ ] README 安装、更新和版本查询命令

## 14. 验收测试

每个接入项目发布前必须验证：

- [ ] amd64 首次安装成功。
- [ ] arm64 首次安装成功或完成交叉环境验证。
- [ ] 旧版本可以更新到最新版本。
- [ ] 当前版本且哈希一致时不重复下载二进制。
- [ ] 当前版本文件被修改后可以自动重新下载修复。
- [ ] 当前文件不可执行时可以修复。
- [ ] 校验文件缺失时拒绝安装。
- [ ] 下载文件哈希不符时拒绝替换。
- [ ] 下载文件无法执行时拒绝替换。
- [ ] 网络中断后旧程序仍可运行。
- [ ] 非 root 更新得到明确提示。
- [ ] 不支持的架构得到明确提示。
- [ ] `--version` 第一行符合解析契约。
- [ ] `update` 透传下载进度和错误。
- [ ] 所有临时文件在成功、失败和中断后均被清理。

## 15. ServerTool 参考文件

- 发布流程：`.github/workflows/release.yml`
- 安装与更新：`scripts/install.sh`
- CLI 自更新：`internal/selfupdate/update.go`
- Linux 构建：`scripts/build_linux.sh`
- Windows 交叉构建：`scripts/build_windows.ps1`
- 发布说明生成：`scripts/generate_release_notes.sh`

其他项目应复制流程和契约，而不是复制 ServerTool 的项目名、包路径或安装命令。
