# 首页「常用」与服务器体检开发方案

状态：待开发方案，本文描述目标行为，不代表功能已经实现。  
适用项目：ServerTool / snail_tool。  
首版平台：Linux amd64、arm64，面向直接运行在服务器上的程序。  
入口：首页「1 常用」→「1 服务器体检」。

## 1. 目标与范围

用户登录服务器后，通过一次体检了解资源压力、磁盘风险、失败服务和异常容器。结果先展示异常，再展示资源概览，支持重新检查与只读详情查看。

首版功能：

| 功能 | 首版交付内容 |
| --- | --- |
| 首页常用入口 | 新增「1 常用」，原菜单编号依次后移 |
| CPU 与负载 | 约 1 秒采样的 CPU 使用率，在线逻辑 CPU 数，1/5/15 分钟负载 |
| 内存与 Swap | 总内存、可用内存、使用率、Swap 总量与使用量 |
| 磁盘空间 | 本地文件系统容量、可用空间、使用率和挂载点 |
| inode | 各本地文件系统的 inode 总量、可用量和使用率 |
| systemd 服务 | 失败的 service 单元及其只读日志详情 |
| 容器 | Docker/Podman 可访问性、运行状态、健康状态、OOM 和退出状态 |
| systemd 日志 | journal 总占用，无法读取时保留明确说明 |

首版不包含持续监控、定时告警、历史数据库、远程服务器管理、自动修复、全盘目录扫描、容器日志排行、非交互 CLI 和用户配置文件。阈值先集中在代码中定义，后续再增加配置入口。

体检不安装软件、不修改配置、不启动或重启服务、不删除数据、不执行容器生命周期操作。当前交互程序仍按现有入口要求使用 sudo/root。

## 2. 菜单与交互

### 2.1 首页

```text
1 常用
2 容器管理
3 一键配置
4 SSH 管理
5 通用配置
6 开发环境
7 系统工具
8 清理配置
0/q 退出
```

原菜单状态徽标继续跟随对应功能；「常用」首版使用普通菜单项。首页不执行体检，不读取磁盘使用率或进行 CPU 采样，避免增加每次返回首页的耗时。

### 2.2 常用菜单

```text
ServerTool › 常用

1 服务器体检  -- 资源、服务与容器状态
0/q 返回
```

进入体检页面时执行一次采集，显示「正在检查服务器…」。完成后保留这次结果，只有选择「重新检查」才重新采集；进入详情、返回详情不重新采集。

### 2.3 体检页面

以下为展示示例，数值和计数不是实际服务器数据。

```text
ServerTool › 常用 › 服务器体检

检查时间：2026-09-30 20:00:00
检查耗时：1.8 秒
检查完成：7/7 类
发现：2 项严重，1 项提醒

[严重] /var 磁盘使用率 94%，可用空间 3.2 GiB
[严重] Docker 容器 web：unhealthy
[提醒] systemd 日志占用 2.6 GiB，建议检查保留策略

CPU       18%（本次约 1 秒采样），4 个逻辑 CPU
负载      0.62 / 0.48 / 0.35
内存      62%，可用 3.0 GiB
Swap      未配置
磁盘      1 个文件系统异常
inode     正常
服务      0 个失败
容器      1 个异常，0 个未配置健康检查
日志      2.6 GiB

1 重新检查
2 查看磁盘与 inode
3 查看服务详情
4 查看容器详情
5 查看日志占用
0/q 返回
```

复用 `internal/ui` 的路径标题、菜单、卡片和颜色函数。正常为绿色，提醒为黄色，严重为红色，无法检查和不适用为灰色。无色模式仍输出状态文字，不依靠颜色传达含义。

默认只显示前 10 条异常，随后显示「另有 N 项，请进入详情查看」。排序为严重、提醒，同等级按检查项顺序和对象名称排序。完整详情分页，每页 10 项。

## 3. 状态与汇总规则

采集状态与异常等级分别保存，避免把读取失败当作正常或严重故障。

| 采集状态 | 含义 | 示例 |
| --- | --- | --- |
| complete | 本项检查完成 | 成功读取磁盘数据 |
| partial | 部分数据可用 | 容器列表可读，但某个容器 inspect 失败 |
| unavailable | 无法完成检查 | 权限不足、命令超时、返回格式无法识别 |
| skipped | 不适用 | 未安装容器运行时、系统不使用 systemd |

| 异常等级 | 含义 |
| --- | --- |
| normal | 已采集指标未触发阈值 |
| info | 补充信息，例如未配置容器健康检查 |
| warning | 需要关注或进一步确认 |
| critical | 明确异常或资源接近耗尽 |

汇总按 CPU、内存、磁盘、inode、服务、容器、journal 七类检查统计完成度。异常条数按具体对象计算：一个文件系统的空间风险和 inode 风险可以各产生一条；同一个容器只计算一次，详情列出全部原因，等级取最高。

无异常且没有 partial/unavailable 时显示「已完成的检查未发现异常」；有缺失时显示「已完成的检查未发现异常，另有 N 类检查未完整完成」。不适用项目单独标注，不计为异常或采集失败。

## 4. 采集与判断规则

所有阈值都是首版产品默认值，不是适用于所有服务器的运维标准。比较使用原始数值，显示时才四舍五入。

### 4.1 CPU 与负载

读取 `/proc/stat` 的汇总 CPU 行两次，间隔默认 1 秒。取 user、nice、system、idle、iowait、irq、softirq、steal 的差值，guest 和 guest_nice 不重复累加。使用率定义为 `100 × (总差值 - idle差值 - iowait差值) / 总差值`；iowait 可以单独展示，CPU 使用率不是 IO 压力指标。

计数回退、总差值为零、必要字段缺失时，该指标标记为 unavailable，不伪造为 0%。读取 `/proc/stat` 中独立的 cpuN 行统计在线逻辑 CPU 数；两次计数不一致时提示采样期间 CPU 数变化。

读取 `/proc/loadavg` 的前三个数值。首版以 5 分钟负载除以在线逻辑 CPU 数作为提示依据，页面同时展示原始 1/5/15 分钟负载。

| 条件 | 等级与文案 |
| --- | --- |
| CPU 使用率 ≥ 90% | warning：本次 CPU 采样较高，建议重新检查 |
| 5 分钟负载 / 在线逻辑 CPU 数 ≥ 1 | warning：近期负载较高，建议查看进程和 IO |
| 其他情况 | normal |

CPU 单次高值不输出 critical，也不声称持续过载。负载不能直接等同于 CPU 使用率。上述内核字段含义参考 [Linux /proc 文档](https://docs.kernel.org/filesystems/proc.html)。

### 4.2 内存与 Swap

读取 `/proc/meminfo` 的 MemTotal、MemAvailable、SwapTotal、SwapFree。内存使用率定义为 `100 × (MemTotal - MemAvailable) / MemTotal`；单位转换明确按文件字段的 kB 转换为字节。

| 条件 | 等级 |
| --- | --- |
| MemAvailable / MemTotal ≤ 5% | critical |
| MemAvailable / MemTotal ≤ 10%，且未达到严重条件 | warning |
| 其他情况 | normal |

缺少 MemAvailable 时保留总量，使用率为 unavailable；首版不使用不可靠的替代公式。总量为零、数值越界或格式错误也标记为不可用。

SwapTotal 为零显示「未配置」，不告警。Swap 已使用只展示数值，不单独告警。首版不据此推断正在发生交换或内存抖动。

### 4.3 磁盘空间与 inode

为避免解析本地化命令文本，建议直接读取 `/proc/self/mountinfo`，对筛选后的挂载点调用 Linux `statfs`，通过 Go 标准库 `syscall.Statfs` 实现，不新增第三方依赖。文件放在 `_linux.go` 中；非 Linux 提供不可用实现，保持包可编译。

挂载路径需要解码 mountinfo 的转义序列，不能直接按空格拆分路径。只检查明确支持的本地文件系统，首版支持 ext2/ext3/ext4、xfs、btrfs、f2fs、vfat、exfat；新类型通过明确规则扩展。

排除 proc、sysfs、devtmpfs、tmpfs、cgroup、容器 overlay 等虚拟挂载，以及 NFS/CIFS/FUSE 等可能产生远程 IO 的挂载。保留排除原因供详情查看，不把未检查的文件系统报告为正常。若 `/` 不在支持范围，页面明确提示根文件系统未检查。

按文件系统设备标识与类型合并重复挂载，保留对应路径列表；btrfs 子卷共享空间时合并显示并注明共享。`/`、`/var`、`/home` 及容器数据目录若属于同一文件系统，只产生一次同类告警。容器数据目录只使用成功获取的实际配置，不假设固定为 `/var/lib/docker`。

空间使用率按普通进程可用空间计算，体现保留块的影响：

```text
已用字节 = (Blocks - Bfree) × Bsize
可用字节 = Bavail × Bsize
使用率 = 已用字节 / (已用字节 + 可用字节) × 100
inode 使用率 = (Files - Ffree) / Files × 100
```

详情分别展示总量、已用量、可用量，注明可用空间可能因保留块而小于总量减已用量。不强制将不同工具的整数百分比视为完全一致。

| 指标 | 提醒 | 严重 |
| --- | --- | --- |
| 空间使用率 | ≥ 80%，且 < 90% | ≥ 90% |
| inode 使用率 | ≥ 80%，且 < 90% | ≥ 90% |

Files 为零或文件系统不提供有效 inode 总量时，inode 标记为 skipped。挂载点消失或某个 statfs 失败时保留其他文件系统数据，该类检查为 partial。首版不执行 `du /` 或递归寻找大文件。

### 4.4 systemd 服务

先判断系统是否实际使用 systemd，仅存在 systemctl 命令不足以确认。不存在 systemd 时为 skipped；存在但无法连接服务管理器时为 unavailable。

采集命令：

```bash
systemctl --failed --type=service --no-legend --plain --no-pager
```

固定 `LC_ALL=C`，不从本地化描述猜测状态。列出的失败服务每个生成 warning，首版不根据服务名推断业务严重程度。无失败服务且采集成功才显示正常。

进入选中服务的详情时按需读取：

```text
systemctl show <unit> --property=Id,ActiveState,SubState,Result --no-pager
journalctl --unit=<unit> --lines=100 --no-pager --output=short-iso
```

单元名来源于采集结果，用独立参数传入命令；详情日志固定取最近 100 行，不进入持续跟随模式。保留服务的完整名称，禁止由用户输入拼接 shell 命令。

### 4.5 容器

沿用项目 Docker 优先、Podman 次之的顺序，并识别 docker 命令实际指向 Podman 的情况。体检使用带超时的只读探测，不直接调用目前没有超时的 `runtime.DetectAll()`，也不调用会引导安装的 `runtime.Ensure()`。

只检查本地上下文；检测到远程 Docker endpoint/context 时显示「远程运行时未纳入本机体检」，不混合远端容器和本机磁盘结果，不展示可能包含凭据的完整 endpoint。首版在 root 权限下检查 root 可见的容器，明确提示用户级 rootless Podman 容器不在覆盖范围。

Docker/Podman 各提供小型采集适配器：先获取容器 ID，再按固定大小批量 inspect。不要假设二者 JSON 字段完全相同。详情只保存名称、ID、State、Health、OOMKilled、ExitCode、RestartCount 等必要字段，不保存完整 inspect 中的环境变量和挂载配置。

| 状态 | 等级与处理 |
| --- | --- |
| unhealthy | critical，展示最近一次健康检查的有限长度信息 |
| dead | critical |
| 已停止且最近退出 OOMKilled=true | critical，注明最近一次退出被标记为 OOM |
| restarting | warning |
| exited 且 ExitCode ≠ 0 | warning，展示退出码，提示结合任务用途判断 |
| running、健康检查 healthy | normal |
| running、未配置健康检查 | info：运行中，健康状态未知 |
| 健康检查 starting | info：健康检查启动中 |
| exited 且 ExitCode=0、created、paused | info，列入详情，不作为异常 |
| 未识别状态 | 保留原始值，状态判断为 unavailable |

RestartCount 仅展示累计次数；没有历史差值时不能声称正在频繁重启。运行中的容器即使存在历史 OOM 标记，也只补充历史信息，不判定为当前 OOM 故障。

安装了 Docker 但 daemon 不可达时，输出 warning「Docker 服务不可达」并将容器数据标记为 unavailable；禁止显示「0 个异常容器」。二者并存时分别展示结果，不因 Docker 不可达而隐去 Podman。

检查期间容器被删除时仅记为 partial，不生成容器故障。Docker 状态采集接口参考 [Docker inspect 文档](https://docs.docker.com/reference/cli/docker/container/inspect/)。

### 4.6 systemd 日志

执行 `journalctl --disk-usage`，固定 `LC_ALL=C`。对明确支持的 bytes/K/M/G/T 文本形式做单位换算；格式无法识别时保留经清理的文本并标记数值 unavailable，不默认为 0。

默认 journal 占用达到 2 GiB 生成 warning「建议检查保留策略」，不生成 critical。该值是提示阈值，日志是否危及磁盘空间由磁盘检查决定。未使用 systemd 或 journalctl 不存在时为 skipped；权限不足、超时为 unavailable。

本页只展示占用和建议。清理日志、配置保留上限和容器日志轮转操作均不在首版体检流程中执行。

## 5. 代码组织

「常用」与现有 `internal/common` 的「通用配置」是不同菜单。新增 `internal/frequent` 作为常用入口，避免名称和职责混淆；体检放在该目录下，而不是继续使用先前讨论的系统工具入口。

建议结构如下，文件名可在实现时按实际复杂度合并，不为拆分而拆分：

```text
internal/frequent/
  frequent.go                 常用菜单，Run(view *ui.UI) error
  doctor/
    doctor.go                 体检流程、上下文、刷新与取消
    types.go                  结果、指标、异常与阈值
    collect_linux.go          /proc、挂载点与 statfs 采集
    collect_other.go          非 Linux 降级
    commands.go               超时命令执行与依赖注入
    services.go               systemd、journal 采集
    containers.go             Docker/Podman 适配
    evaluate.go               纯数据判断与汇总
    render.go                 概览、详情与分页
```

需要调整的现有文件：

| 文件 | 变更 |
| --- | --- |
| `internal/app/app.go` | 首页新增常用入口，switch 与展示编号同步调整 |
| `internal/app/app_test.go` | 菜单编号、徽标行识别及顺序断言同步调整 |
| `README.md` | 实现完成后更新功能列表、目录结构和入口说明 |
| `docs/CLI.md` | 实现完成后补充菜单入口及只读行为；首版不新增 CLI 命令 |

菜单使用 `shared.RunAction` 和 `shared.ErrReturnToMenu`。用户返回时不额外停留；无效或空输入不能启动任何检查之外的操作。

不要为了查看异常容器直接进入现有完整管理流程。首版实现只读容器详情与有限行日志查看；未来如提供管理入口，应作为用户明确选择的独立菜单动作。

## 6. 数据模型与接口

以下类型用于明确边界，属于建议接口，不是已存在的代码：

```go
type CheckState string // complete / partial / unavailable / skipped
type Severity string   // normal / info / warning / critical

type Finding struct {
    CheckID  string
    ObjectID string
    Severity Severity
    Summary  string
    Evidence []string
    Advice   string
}

type CheckResult struct {
    ID        string
    State     CheckState
    Reason    string
    Findings  []Finding
    StartedAt time.Time
    Duration  time.Duration
}

type Report struct {
    StartedAt time.Time
    FinishedAt time.Time
    Checks    []CheckResult
    // 实现时增加 CPU、内存、文件系统、服务、容器和 journal 的具体数据结构。
}

type Thresholds struct {
    CPUWarningPercent    float64
    LoadWarningRatio     float64
    MemoryWarningPercent float64 // 可用比例
    MemoryCriticalPercent float64
    DiskWarningPercent   float64
    DiskCriticalPercent  float64
    InodeWarningPercent  float64
    InodeCriticalPercent float64
    JournalWarningBytes  uint64
}

type CommandRunner interface {
    Output(ctx context.Context, name string, args ...string) ([]byte, error)
}
```

指标值与可用状态分别保存；缺失的 CPU 使用率、内存使用率等不能用零值代表成功。内部统一字节与原始浮点数，渲染时转换为 MiB/GiB 和百分比。

核心接口建议为 `Collect(ctx, dependencies, thresholds) Report`、`Evaluate(...)`、`Render(...)`、`Run(view *ui.UI) error`。读取文件、采样等待和执行命令支持注入，便于通过样本测试，不依赖测试机实际资源与服务状态。

单项采集失败进入 Report，不作为整个页面的致命错误。取消或 UI 输入错误通过 Run 返回，禁止吞掉用户取消继续刷新。

## 7. 执行、性能与边界

- 全次检查默认截止时间 15 秒；普通外部命令 3 秒，容器命令最多 5 秒，均不能超过剩余全局时间。
- CPU 等待使用可取消的 timer，不能用无法取消的固定 sleep。
- CPU 采样、主机指标、服务、容器与日志采集可以并发；最多 4 个工作任务。工作任务不直接输出 UI，汇总后按固定顺序展示。
- 外部命令使用 `exec.CommandContext` 与独立参数，不使用 `sh -c`；保留判断所需的 stdout、stderr 与退出原因。
- 命令输出设上限，例如每次 4 MiB；截断必须标记为 partial/unavailable。达到上限后继续排空输出或终止命令，不能因停止读取造成管道阻塞。
- 容器 inspect 每批最多 50 个；超过全局预算时保留已获取结果，并显示完成数量与未检查数量。
- mountinfo 和 meminfo 等普通文本设合理读取上限；详情不扫描日志全文，不执行全盘递归遍历。
- Ctrl+C 取消本次检查并终止正在运行的采集子进程，返回常用菜单。实现时局部使用信号上下文，退出检查后释放信号注册。
- 默认本地健康主机期望在 2～5 秒内完成；超时环境返回部分结果。此为性能验收目标，不是依赖系统调用绝不阻塞的承诺。
- 不支持的文件系统、运行环境和输出格式明确列出。首版不声称覆盖容器内运行时的宿主机资源或 rootless 用户容器。
- 输出清理控制字符和 ANSI 转义；健康检查与命令错误详情做长度限制，首版默认不展示完整环境变量、完整 inspect 或完整 endpoint。

## 8. 开发步骤

1. 新增常用菜单，调整首页编号和原有菜单测试；此阶段不触发任何体检采集。
2. 建立结果类型、阈值、可注入的命令执行器和纯判断函数。
3. 完成 CPU、内存、磁盘及 inode 采集，验证公式、边界值与挂载去重。
4. 完成 systemd 服务与 journal 采集，验证缺少命令、无 systemd 和读取失败。
5. 完成 Docker/Podman 采集，验证运行时并存、不可达、状态差异和检查期间对象消失。
6. 接入概览、分页详情、刷新和取消，验证每项失败不会中断其他检查。
7. 执行项目测试和跨架构构建，在隔离环境做人工验收，更新 README 与 CLI 文档。

## 9. 测试与验证

使用 `/proc`、mountinfo、systemd 输出和运行时 JSON 样本进行单元测试，不在测试中修改真实服务或创建真实磁盘故障。

重点测试：

| 类别 | 必须覆盖的场景 |
| --- | --- |
| CPU | 正常差值、guest 不重复计算、零差值、回退、字段缺失、取消采样 |
| 内存 | MemAvailable 计算、单位换算、无 Swap、缺少字段、5% 与 10% 边界 |
| 文件系统 | 转义路径、重复挂载、共享空间、无 inode、statfs 失败、80% 与 90% 边界 |
| 服务与日志 | 无 systemd、缺少命令、空失败列表、权限不足、超时、未知日志单位 |
| 容器 | 未安装、并存、daemon 不可达、健康状态缺失、OOM、非零退出、正常停止、对象消失 |
| 汇总 | 容器多原因只计一次、partial 不显示全正常、稳定排序、分页与截断提示 |
| UI 与取消 | 菜单编号、NO_COLOR、0/q/exit、无效输入、刷新、取消后不遗留采集进程 |

实现后的项目验证命令：

```bash
go test ./...
go vet ./...
```

交叉构建输出写到临时路径：

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /tmp/servertool-doctor-amd64 ./cmd/snail_tool
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /tmp/servertool-doctor-arm64 ./cmd/snail_tool
```

人工验收使用测试 VM 或隔离测试环境，不为验收破坏当前工作机。覆盖无容器运行时、Docker、Podman、二者并存、非 systemd 和部分检查失败场景。负载及容器异常以受控样本或临时测试服务验证；磁盘满和 inode 满优先用样本验证。

## 10. 首版验收清单

- [ ] 首页「1 常用」，原功能编号和对应处理一致，清理配置为第 8 项。
- [ ] 常用菜单第一项为服务器体检，面包屑和返回行为符合现有 UI。
- [ ] 首页与常用菜单不会自动执行体检。
- [ ] 进入体检后完成一次检查；刷新重新采集，详情返回复用已有结果。
- [ ] CPU、内存、空间、inode、服务、容器与 journal 七类都有完成或降级状态。
- [ ] 先展示异常，再展示概览，详情能查看完整对象列表。
- [ ] 所有告警包含依据，必要时包含建议；CPU 高值注明单次采样。
- [ ] 无健康检查、无 Swap、正常停止的容器不会被直接标为故障。
- [ ] 缺少依赖、超时和权限不足不会伪装成零值或正常。
- [ ] 容器不可达保留明确提醒，不混入远程容器或遗漏覆盖范围说明。
- [ ] 重复挂载不重复产生同类告警，不递归扫描全盘。
- [ ] 整个体检流程只执行采集与只读详情操作。
- [ ] NO_COLOR、TERM=dumb 与非终端输出可读，不含不必要的控制序列。
- [ ] Ctrl+C 取消后返回菜单，外部采集命令不继续运行。
- [ ] 单元测试、go vet、Linux amd64/arm64 构建通过。
- [ ] README 与 CLI 文档标注实际实现范围，不把后续规划写成已实现功能。

## 11. 后续扩展

首版稳定后，再按需求增加：容器日志占用排行、指定目录的按需空间分析、用户可调阈值、只读 `snail doctor` 和 JSON 导出、持续采样与历史对比。后续功能继续使用现有采集和判断层，避免在 CLI 与交互菜单中重复实现规则。
