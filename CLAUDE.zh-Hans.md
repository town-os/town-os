CLAUDE，未经我明确许可，不得编辑此文件。

> **本文件是 [CLAUDE.md](CLAUDE.md) 的简体中文译本。英文原件为准。**
> 繁体中文译本见 [CLAUDE.zh-Hant.md](CLAUDE.zh-Hant.md)；西班牙语译本见
> [CLAUDE.es-ES.md](CLAUDE.es-ES.md)（西班牙）与 [CLAUDE.es-MX.md](CLAUDE.es-MX.md)（墨西哥）。
> 两者出现分歧时，以英文原件为准，并应修正译文。代码标识符、文件路径、
> 命令、环境变量、API 路径与 YAML 键名一律保留原文，不作翻译。

**本文件只包含构建说明与代码风格。** 系统实际如何运作——架构、各子系统行为、
API 界面、启动顺序、设置项，以及维系这些内容的不变量——都在
[DESIGN.md](DESIGN.md)（中文译本见 [DESIGN.zh-Hans.md](DESIGN.zh-Hans.md)）中。
需要了解 Town OS **做什么**时读 DESIGN.md；需要了解**如何构建、如何测试、
如何在其中写代码**时读本文件。凡是改变行为的改动，DESIGN.md 都需要随之更新。

- **最重要**：
    - **使用 `make`，而非底层的编译/测试工具。** 绝不直接运行 `go build`、`go test`、`go vet`、`golangci-lint`、`bun test`、`vitest` 或任何等价命令。一律通过 make 目标执行，这样仓库的封装（清理陷阱、btrfs 生命周期、每次运行的实例 ID）才会生效。
    - **可随时运行的 make 目标**（快速、幂等、无远程副作用）：
      `make help`、`make lint`、`make check-*`（bun / go / podman / runc / btrfs / libsystemd / golangci-lint）。可自由使用它们来验证改动——无需事先询问。
    - **若某个 make 目标不在上述任一列表中，先询问。**
    - 任何情况下都绝不强制推送（force push）。
    - 需要推送时，只推送到 "origin"。
    - 推送前务必先执行 `git pull --rebase`，并解决所有合并冲突。
    - 绝不以任何方式碰 GPG。正常执行 `git commit` 即可。若签名失败，停下来询问用户。绝不杀掉 gpg-agent，绝不使用 `--no-gpg-sign`，绝不自行尝试修复 GPG。
    - 提交必须签名。
    - 绝不因任何理由摆弄 GPG agent。

- 当提供了参数时，确保调用函数中真正使用了这些参数

- **并发安全** —— `make test-full` 必须始终能够在同一仓库中同时运行而互不冲突。没有任何事情比这更重要。

- Go 程序中不应使用 context.TODO 与 context.Background。凡有可能，请使用带超时和取消的 context，确保不会有任何东西永久等待某个 context。

- 你所做的每件事都要加测试。**每一处行为改动都必须同时具备单元测试和集成测试。** 单元测试隔离验证逻辑；集成测试在测试容器内、使用真实的 systemd、btrfs 与 podman 端到端验证功能。若确实无法编写集成测试（例如纯 UI 改动），在提交信息中说明原因。

- 使用类型断言的结果前，一律检查断言是否成功

- **容器镜像中使用 CMD 而非 ENTRYPOINT** —— 所有 Containerfile 及内联的 Containerfile 字符串都必须使用 `CMD` 而非 `ENTRYPOINT`。这样 `podman run <image> <command>` 无需 `--entrypoint` 即可覆盖默认命令。适用于 systemcontroller 镜像、NC 镜像，以及任何动态生成的 Containerfile。

- **每个运行时容器镜像都必须自带系统 CA 证书包** —— 任何 Containerfile（或内联的 Containerfile 字符串），只要其最终阶段运行的 Town OS 代码会发起对外 HTTPS 调用，就必须安装 `ca-certificates`（debian/ubuntu：`apt-get install ca-certificates`；alpine：`apk add ca-certificates`），除非基础镜像已经提供（例如 `caddy`、`oven/bun`）。缺少 CA 证书包时，Go 的 TLS 栈会让每一次 HTTPS 调用都失败并报 `x509: certificate signed by unknown authority`，而后台轮询器中的失败在默认日志级别下是不可见的（参见 `fetchExternalIP` 静默丢弃 `ipinfo.io` 响应的情况）。新增任何 Containerfile 时，先确认最终镜像中存在 `/etc/ssl/certs/ca-certificates.crt`，再认为该镜像可以发布。

- **所有 `podman run --name` 都必须带 `--replace`** —— 全仓库无一例外。

- **make 流水线中所有 podman 都通过 `${SUDO}` 以 root 身份运行** —— `make/lib.sh` 中定义 `SUDO="sudo HOME=$HOME"`，并且 make 脚本（`build.sh`、`images.sh`、`test.sh`、`dev.sh`、`registry.sh`、`gitea.sh`、`lib.sh`）中的**每一处** `podman` 调用都必须写成 `${SUDO} podman`。root 与 rootless 的 podman 拥有**各自独立的镜像存储**：基础镜像被拉取/加载进 root 存储（`/var/lib/containers`），而 rootless 用户存储是空的。因此裸的（不带 `${SUDO}` 的）`podman` 调用会命中空的 rootless 存储，并在 `--pull=never` 下以 `image not known` 失败——即便 `${SUDO} podman image exists` 报告镜像存在（那是另一个存储）。向 make 脚本中添加任何 podman 命令时，务必加上 `${SUDO}` 前缀；绝不要在 rootless podman 下运行会构建/加载镜像的 make 目标，也不要为宿主机侧构建设置指向 rootless socket 的 `CONTAINER_HOST`（那会把 `${SUDO} podman` 路由到错误的存储）。唯一的例外是可用性探测（`check.sh`/`preflight.sh` 中的 `command -v podman`）以及 `deps.sh` 安装列表中作为软件包名出现的字面量 `podman`。

- **构建中不得硬编码公共 DNS；podman 构建使用 `--network=host`** —— make 流水线中的每一次 `podman build` 都以 `--network=host` 运行，使名称解析走宿主机的解析器（systemd-resolved）。容器网络下的构建会把宿主机的环回 stub 替换成公共解析器，而强制门户网络（咖啡馆、酒店）会阻断对 1.1.1.1/8.8.8.8 的直接查询——从而让 `bun install`、`apt-get`、`apk add` 无限期卡住。出于同样的原因，测试与开发所用的 NC 镜像是**在宿主机上**构建的（`nc-image` / `nc-image-dev` 目标 → `localhost/town-os-networkcontroller:<INSTANCE_ID>`，二进制从生产/开发基础镜像中提取，因此始终与 systemcontroller 匹配），再经由镜像缓存加载进容器——绝不在容器内用 `--dns` 构建。

- **所有测试套件的 `podman run` 容器都使用 `--net host`** —— 测试容器、UI 后端、UI 测试运行器、开发容器、registry 与 gitea 容器全部使用宿主机网络。registry 与 gitea 通过 `REGISTRY_HTTP_ADDR` / `GITEA__server__HTTP_PORT` 直接绑定各自实例的随机端口，而不是使用 `-p` 映射；gitea 的 SSH 被禁用（`DISABLE_SSH=true`），因此不会有任何东西尝试绑定宿主机的 22 端口。理由：桥接网络容器在强制门户网络下 DNS 会失效，而 registry（Docker Hub 回源拉取）与 gitea（仓库迁移）都会自行发起对外调用。唯一刻意保留的例外是 `preflight-dev` 的 nginx 容器，它的 `-p` 映射正是为了验证桥接网络可用。

- **镜像标签按架构分区** —— 每个推送的标签都带有 `uname -m` 原始形式的架构后缀（`<arch>` 为 `x86_64` 或 `aarch64`）。该标签后缀刻意区别于 OCI 平台名 `amd64`/`arm64`：Go 通过 `archTag()` 把 `runtime.GOARCH` 映射到该后缀，make 使用 `HOST_ARCH`（规范化为 `x86_64`/`aarch64`），shell 使用 `make/lib.sh` 中的 `host_arch_tag`。而普通的 `host_arch` / `runtime.GOARCH` 值仍保持 `amd64`/`arm64`，因为 podman 在 `podman pull --platform linux/<arch>` 和 `.Architecture` 比较时需要它们——绝不要把 `x86_64`/`aarch64` 喂给 `--platform`。`push-rc` 推送 `rc.<date>-<arch>` / `rc.latest-<arch>`；`push-release` 推送 `release.<date>-<arch>` / `latest-<arch>`——始终是执行推送的宿主机的本机架构。不带后缀的普通名称（`rc.latest`、`latest` 以及日期标签）**仅**作为多架构 manifest 列表存在，由 `manifest-rc` / `manifest-release` 在 `ARCHES`（`x86_64 aarch64`）中每个架构都推送完成之后组装；绝不要把普通名称作为单架构标签推送。当没有烘焙进标签时，运行时的回退值是 `main.go` 中的 `defaultVersionTag()`（`rc.latest-<arch>`，架构来自 `archTag()` 映射后的 GOARCH）。理由：从一台主机推送的单架构普通标签，在另一种架构上会以 `exec format error` 失败（或者更糟：在 `Restart=always` 下不断崩溃重启的同时，却让状态轮询测试虚假通过）。

- **普通便捷标签绝不可用于测试** —— 任何测试、测试框架、开发容器或夹具都不得引用*普通的*（无架构后缀的）`quay.io/town/*:rc.latest` 或 `:latest` 镜像（它们可能不存在，或是过期的多架构 manifest）。带架构后缀的形式**是**允许的，并且是默认选择。测试使用：宿主机对应架构的 rolodex rc 标签（`rc.latest-<arch>`，即 `rc.latest-x86_64` / `rc.latest-aarch64`）、本地构建的 UI 镜像（`make ui-image` → `localhost/town-os-ui:<INSTANCE_ID>`）、本地构建的 NC 镜像（`make nc-image`），以及在镜像从不会被拉取或运行的 mock 单元测试中使用的中性假标签（例如 `:testtag`）。

- **快速失败** —— 若任何 make 子任务，或由 make 子任务启动的脚本失败，立即停止。不要继续进入下一阶段。

- **绝不吞掉退出码** —— 运行 make/测试命令的脚本绝不能吞掉退出码。不要写 `|| rc=$?`，不要在测试调用上写 `|| true`。让 `set -e` 发挥作用。清理命令（podman rm、rm -f）不在此限。

- **测试中不得硬编码共享资源** —— 所有测试临时文件、socket、目录与端口都必须使用每次运行唯一的路径（`t.TempDir()`、`filepath.Join`、`findFreePort` 等）。绝不使用 `/tmp/foo.sock` 这样的固定路径。

- **运行上述允许的 make 目标无需事先询问；"需要许可"列表中的其他任何操作都需要明确同意。** 绝不直接调用 `go`、`go test`、`go vet`、`golangci-lint`、`bun test`、`vitest` 等——一律通过 make。

- **测试或构建代码中的任何东西都不得使用 tmpfs** —— 任何 make 目标、make 脚本或测试框架写出的文件，都不得位于 tmpfs（内存支撑的）文件系统上。这一条不可协商且绝对：它适用于 btrfs 环回后备镜像、容器/卷数据、归档、下载、端口文件、跟踪文件，以及每次运行产生的其他一切产物。原因是致命的，而非表面的：测试用 btrfs 文件系统是一个 50G 的环回文件，而由 tmpfs 支撑的 loop 设备在内存压力下会**使宿主机内核死锁**——tmpfs 页只能回收到 swap，但 loop 回写路径本身又需要分配内存才能把它们排空，于是一旦 tmpfs 占满内存，机器就会硬锁死，并由固件/看门狗重启（已在 Manjaro 上观察到：systemd 把 `/tmp` 挂载为大小为内存 50% 的 tmpfs，而 swap 几乎为零）。在常见开发发行版（Arch/Manjaro/Fedora）上 `/tmp` 就是 tmpfs，所以**不要假设 `/tmp` 由磁盘支撑**。任何会创建后备文件、loop 设备或较大写入目标的测试/构建代码，都必须先把其目录解析到真正由磁盘支撑的文件系统上（例如检查 `findmnt -no FSTYPE <dir>` 不是 `tmpfs`/`ramfs`，或放在 `/var/tmp` 这类已知位于磁盘的路径下），若无法做到则大声失败。向 make 脚本中添加任何新路径时，写入前先确认它不在 tmpfs 上。

- **临时状态位置** —— 每次运行的簿记数据（端口文件，`.disk`/`.loop`/`.mount` 跟踪文件，开发元数据）按实例限定在 `/tmp/town-os-$(INSTANCE_ID)/` 之下；但任何*承载数据*的产物——首先是 btrfs 环回后备镜像——都必须放在由磁盘支撑的路径上，绝不能放在 tmpfs 上（见上面的 no-tmpfs 规则）。在未先确认 `/tmp` 不是 tmpfs 之前，绝不要把环回/磁盘镜像、容器卷数据或大型下载放到 `/tmp`。

- **只在被告知时提交或推送** —— 除非用户明确要求，否则绝不运行 `git commit` 或 `git push`。绝不强制推送（`--force` 或 `--force-with-lease`）。

- systemcontroller 绝不应调用 os.Exit，除非该服务确实正在被终止——严重错误应以 fatal 级日志处理

- 请检查所有错误。任何代码的任何部分，都不得以任何理由用下划线忽略或跳过错误检查

- **务必检查 comma-ok 表达式的 `ok`。** 任何返回 `value, ok` 对的表达式——类型断言（`v, ok := x.(T)`）、map 索引（`v, ok := m[k]`）、channel 接收（`v, ok := <-ch`）——都必须先检查 `ok` 再使用 `value`；绝不用 `_` 丢弃它，也绝不假定断言/查找一定成功。优先使用 comma-ok 形式，而非单值类型断言 `v := x.(T)`（后者在类型不匹配时会 panic）：使用 `v, ok := x.(T)` 并显式处理 `!ok`。此规则同样适用于测试代码。（类型明确的 switch 分支——`switch v := x.(type)`——以及刻意的 `_ = m[k]` 成员写入，是仅有的例外。）

- 尽可能在 if 语句中使用内联错误语法（例如 `if err := foo(); err != nil {`）

- **测试服务使用随机高位端口** —— 启动网络服务（DNS、HTTP、gRPC 等）的集成测试必须通过 `findFreePort` 绑定到随机高位端口，绝不使用 53 或 80 这类知名端口。这可防止多个测试同时运行时发生冲突。

- **测试中的 DNS 绝不允许触碰宿主机。** 任何测试、测试框架，或由 make 测试目标启动的任何东西，都不得改动宿主机的名称解析，也不得占用宿主机的 DNS 端口。具体而言，一次测试运行绝不能：
    - 重写 `/etc/resolv.conf`（那是 `make/dev.sh` 中的 `redirect_host_dns`，只属于 `make dev`），
    - 写入 `/etc/systemd/resolved.conf.d/town-os.conf`，或以其他方式调用 `rolodex.ConfigureResolvedRouting`，
    - 向 `systemd-resolved` 发信号或重启它（`pkill -HUP systemd-resolved`），
    - 在宿主机网络命名空间中绑定 **`127.0.0.2:53`**，或任何 `:53`。

  测试容器刻意以 `--net host` 运行（桥接网络的 DNS 在强制门户网络下会失效），因此系统服务绑定的每一个端口都落在**宿主机**命名空间中。这正是为什么 `TOWN_OS_DNS_PORT` 会按次运行分配到 `$(STATE_DIR)/.dns-port` 并由 `system_port_env`（`make/lib.sh`）传入，也是为什么只要 `dnsPortIsDefault()` 为假，`main.go` 就跳过 resolved 路由配置——因为 resolved 的按域名服务器地址不携带端口，把 resolved 指向一个已被迁移端口的 rolodex 的 `DNSLoopback`，会让该 TLD 下的每一次查询都被黑洞吞掉。

  如果一次测试运行结束后 `127.0.0.2:53` 仍被占用，或宿主机上出现了 `town-os.conf` 配置片段，应将其视为**测试框架的缺陷，而非偶发的测试不稳定**：这意味着端口覆盖没有传到容器里，rolodex 回退到了默认值。用 `ss -lnup | grep 127.0.0.2` 与 `ls /etc/systemd/resolved.conf.d/` 验证——宿主机 `:53` 上唯一的监听者应当是机器自己的解析器，绝不是我们的。`make dev` 是唯一的例外，且由操作者主动选择，因为它本就意在镜像一台真实的机器。

- **绝不编写会向远端 Gitea 或 GitHub 推送的测试。**

- **当我让你做某件事时，不要争辩。**

- **在无关紧要时，测试的 git 操作应优先使用本地仓库而非远程仓库** —— 例如 populate-repos 应在存在本地同级目录时从该目录克隆，而不是从 GitHub 拉取。

- 请随时修复测试中所有可修复的警告

- 包变量应始终作为编译步骤的一部分被翻译。固定的包变量应始终有测试覆盖。

- 确保所有文件按 API 组织。它们应按子模块名称分层限定作用域。行数的参考指标大约为 500 行。


## 性能约定

- **使用 `strings.Builder` 构造字符串** —— 绝不用 `string(append([]byte(s), c))` 逐字符构造字符串。使用 `strings.Builder` 配合 `WriteByte`/`WriteString`，把 O(n²) 的分配降为 O(n)。参见 `src/packages/packages_compile.go`（`applyTemplate`、`applyTemplates`）。

- **已知大小时预分配切片** —— 当结果大小或其上界已知时（例如分页中的 `limit`），使用 `make([]T, 0, capacity)`。避免在热路径中先 `var items []T` 再无界 `append`。

- **用 `COUNT(*) OVER()` 实现单查询分页** —— 分页列表端点必须在 SELECT 列中使用 SQLite 窗口函数 `COUNT(*) OVER()`，而不是另跑一次 `COUNT(*)` 查询。在扫描每一行的同时读出总数。

- **为 WHERE 子句中使用的列建索引** —— 每一个用于 `WHERE` 过滤的 SQLite 列（尤其是 `created_at`、`success`、`account`）都必须有合适的索引。复合索引应与常见的过滤组合相匹配（例如为 `CountRecentErrors` 建立 `(success, created_at)`）。

- **缓存昂贵的重复查找** —— `RepositoryRoot.LoadPackages()` 的结果按仓库名缓存在一个 `sync.Map` 中，并在 `ForceRefresh()` 时失效。调用方必须使用 `cachedLoadPackages()`，而不是直接调用 `LoadPackages()`。同理，`GetInternalIP()` 把结果缓存在 `atomic.Value` 中，而不是每个请求都调用 `net.InterfaceAddrs()`。

- **直接查找优于全量扫描** —— 检查单个包时使用 `GetInstalledVersion(repo, name)`（直接读取 `installed/<repo>/<name>/`），而不是 `ListInstalled()` 加线性搜索。

- **相互独立的操作并行 I/O** —— `refreshSystemServices` 中的容器镜像拉取使用 goroutine 加信号量（最多 3 个并发），而不是顺序循环。使用 `sync.WaitGroup` + channel 信号量；不要引入 `errgroup` 依赖。

- **后台 goroutine 使用服务器作用域的 context** —— 后台 goroutine（pages 的 git clone、镜像提取）必须使用服务器作用域的 context（`s.ctx`），而不是 `context.Background()`，以便它们响应优雅关闭。它们**不得**使用 HTTP 请求的 context（该操作的生命周期必须长于请求）。

- **reconcile 中批量加载依赖** —— 所有包的依赖记录在进入 reconcile 循环之前一次性预加载到一个 map 中，而不是在循环内逐包加载。


## 开发前置条件

从源码构建 Town OS 需要：

- **Go 1.25+** —— 为 system controller 启用 CGO（链接 libsystemd）。
- **libsystemd-dev** —— systemd journal 与 dbus 绑定所需的 C 开发头文件，`go-systemd/v22` 依赖它。
- **Bun** —— 用于 UI 构建与测试的 JavaScript 运行时。
- **Podman** —— 以 root 运行（`sudo`），用于容器操作。
- **btrfs-progs** —— 提供 `mkfs.btrfs`，用于创建测试与开发用的 btrfs 卷。
- **golangci-lint** —— 用于 Go 代码检查。
- **QEMU** —— `qemu-system-x86_64` 用于运行 VM 包；`qemu-img` 用于把 VM 磁盘镜像转换为 raw 格式。

### 引导安装

`make deps` 会在一台全新的 Arch 或 Ubuntu/Debian 机器上安装全部宿主机依赖
（Go、podman、runc、btrfs-progs、libsystemd 头文件、golangci-lint、bun、qemu、
构建工具）。它由 `make/deps.sh` 实现，从 `/etc/os-release` 检测发行版，可安全重复运行。

`make help`（默认目标）会打印一份分组的、面向用户的 make 目标清单。
由 `make/help.sh` 实现。在 `make/include.mk` 中新增或重命名目标时，
请保持这两个脚本同步。

### 预检检查

Makefile 提供了 `preflight-dev` 目标，用于在运行测试或启动开发服务器之前验证开发环境。它检查：

- **podman** —— 验证 `podman` 命令在 PATH 中可用。
- **btrfs-progs** —— 验证 `mkfs.btrfs` 命令在 PATH 中可用。
- **仓库凭据** —— 验证已设置 `TOWN_OS_REPO_USERNAME` 与 `TOWN_OS_REPO_PASSWORD` 环境变量。
- **桥接网络** —— 启动一个带端口绑定的测试 nginx 容器，验证 podman 的 `-p` 标志工作正常。

每项检查在失败时打印描述性错误信息并以非零状态退出。所有检查通过后才会显示 "All preflight checks passed."。

### Ubuntu / Debian 安装

在 Ubuntu 或 Debian 系统上，使用以下命令安装系统依赖：

```
sudo apt-get install -y libsystemd-dev btrfs-progs podman runc qemu-system-x86 qemu-utils
```

Go、Bun 与 golangci-lint 需分别安装（参见各自上游文档）。

## 代码质量

### 错误处理

所有 Go 的错误返回值都必须被显式检查。`errcheck` linter 在全项目启用，并且不得使用空白标识符（`_ =`）丢弃错误。

在生产代码中，defer 函数里的清理错误通过命名返回值用 `errors.Join()` 与主错误合并（例如 `defer func() { err = errors.Join(err, f.Close()) }()`）。非关键的尽力而为操作应记录错误，而不是丢弃。

在测试代码中，清理错误依严重程度通过 `t.Errorf` 或 `t.Logf` 报告，或以 `//nolint:errcheck` 注解加理由注释显式抑制。

所有 `//nolint` 指令都必须带理由注释（由 `nolintlint` 强制）。

## 集成测试

### 本地 Docker Registry

集成测试针对一个本地 `registry:2` 容器运行，以避免 Docker Hub 的速率限制并确保可复现性。流程如下：

1. **镜像发现** —— `discover-images` 工具扫描所有测试包仓库中的 `docker.io` 镜像引用，包括主镜像与归档镜像。结果去重后写入 `.cache/.registry-images`。
2. **启动 registry** —— 在一个随机端口上启动 `registry:2` 容器。
3. **镜像镜像化** —— 每个发现的镜像从 Docker Hub 拉取，重新打上本地 registry 地址的标签，并推送到本地 registry（对 localhost 禁用 TLS 校验）。
4. **registry 配置** —— 生成一个 `registries.conf` 文件，把 `docker.io` 的拉取重定向到本地镜像源。该文件被挂载进测试容器的 `/etc/containers/registries.conf.d/`。
5. **透明运作** —— 无需修改任何代码；podman 会自动使用本地镜像源。对于未缓存的镜像，镜像源会回退到 Docker Hub。

每个工作目录都有自己的 registry 实例（通过 `INSTANCE_ID` 区分），因此并发的测试运行不会冲突。

### 本地 Gitea 服务器

集成测试使用本地 Gitea 实例，以避免 git 操作触及 GitHub 的速率限制。流程与本地 Docker registry 模式一致：

1. **启动服务器** —— 在随机端口上启动 `gitea/gitea:latest` 容器，安装向导预先锁定。自动创建一个管理员用户（`town-os`）。
2. **仓库迁移** —— `populate-repos` 工具使用 Gitea 迁移 API，把测试包仓库（`test-packages-core`、`test-packages-extras`）从 GitHub 迁移到本地 Gitea 实例。迁移是幂等的：已存在且非空的仓库会被跳过；因迁移失败而残留的空仓库会被删除并重试。
3. **透明运作** —— 测试通过环境变量（`TOWN_OS_TEST_REPO_CORE_URL`、`TOWN_OS_TEST_REPO_EXTRAS_URL`）获得本地 Gitea 的 URL。若未设置这些变量，测试会回退到默认的 GitHub URL。

每个工作目录都有自己的 Gitea 实例（通过 `INSTANCE_ID` 区分），因此并发的测试运行不会冲突。镜像发现在本地 Gitea 仓库可用时会从中读取。

### 容器清理

`test-full` 目标在集成测试完成后运行 `clean-integration` 与 `clean-btrfs`，确保即使测试失败，所有测试容器（test、registry、gitea、ui-backend、ui-integration）与 btrfs 环回挂载也会被拆除。`clean-dev` 目标在清理缓存前先删除所有 `town-os-dev` 容器。`clean-containers` 目标删除任意实例或工作目录下的所有 Town OS 容器（匹配 `town-os-*` 与 `preflight-test-*` 模式）。`clean-integration` 目标使用容错的容器删除方式以实现幂等清理。`clean-all` 目标使用 `clean-containers` 进行跨实例的全面清理。监控镜像会从镜像缓存预加载进集成测试容器。

### Btrfs 环回清理

测试目标（`test-integration`、`test-ui-integration`、`test-full`）使用 shell 的 EXIT 陷阱，保证无论测试成功、失败还是被信号中断，btrfs 清理都会执行。相关配方组织在 `make/` 下的 shell 脚本中。btrfs 卷的创建在 EXIT 陷阱注册之后、于测试脚本内部进行，确保即使创建或后续步骤失败，loop 设备也不会泄漏。

`clean-btrfs` 目标执行尽力而为的清理（不使用 `set -e`）：卸载 btrfs 文件系统，通过 `losetup -j` 查找该磁盘镜像文件对应的 loop 设备并分离，并删除状态跟踪文件（`town-os.disk`、`town-os.loop`、`town-os.mount`）。一道安全网会扫描所有活跃的 loop 设备（`losetup -a`），查找任何由当前目录下 btrfs 镜像文件支撑的设备，即使跟踪文件缺失也会分离这些孤立设备。

### 测试文件组织

集成测试文件按组件与子功能组织。每个文件聚焦一个特定领域：btrfs 操作、git 操作、仓库管理，以及 system controller 的各子系统。system controller 的测试进一步拆分为独立文件：归档、引导、文件系统、安装（mock 与真实 systemd）、多仓库场景、网络、包、pages、reconcile、仓库、设置、systemd 单元与卷。通用的测试初始化与辅助函数集中在一个专门的 helpers 文件中。

### 测试环境

集成测试在特权 podman 容器内运行，容器中带有 systemd、btrfs 与完整的测试二进制。该容器包含 podman 与 runc，用于运行包容器。测试会实际演练真实的 systemd 单元生命周期、btrfs 卷管理与容器操作。
