# Town OS 设计

Town OS 如何运作：架构、各子系统的行为、API 界面，以及维系它们的不变量。
构建说明、测试规则与代码风格在 [CLAUDE.md](CLAUDE.md)（简体中文译本见
[CLAUDE.zh-CN.md](CLAUDE.zh-CN.md)）中。

> **本文件是 [DESIGN.md](DESIGN.md) 的简体中文译本。英文原件为准。**
> 繁体中文译本见 [DESIGN.zh-TW.md](DESIGN.zh-TW.md)；西班牙语译本见
> [DESIGN.es-ES.md](DESIGN.es-ES.md)（西班牙）与 [DESIGN.es-MX.md](DESIGN.es-MX.md)（墨西哥）；
> 日语译本见 [DESIGN.ja-JP.md](DESIGN.ja-JP.md)。
> 两者出现分歧时，以英文原件为准，并应修正译文。代码标识符、文件路径、
> 命令、环境变量、API 路径与 YAML 键名一律保留原文，不作翻译。

行为上的改动应当在做出该改动的同一个提交中写入本文件。仓库构建方式或测试方式
的改动则属于 CLAUDE.md。

## 架构不变量

这些规则约束的是设计，而非代码。违反其中任何一条都不会让构建或 linter 失败——
它产生的是一台能启动、但随后行为失常的机器，而且失常之处通常离改动很远。

- **存储层管理卷；gfeh 提供对象存储。** `src/storage` 只处理 btrfs 子卷与配额，别无其他——它完全不负责对象存储。对象、每个文件的元数据与权限、分层的用户/ACL 数据库、共享、按文件的 HTTP 暴露、联邦，以及每一种协议视图（S3、IPFS、Google Drive、纯 HTTP——以及 SMB/CIFS，gfehd 实现了它但 Town OS 不提供服务）都属于 gfeh，由它负责。绝不要向 `src/storage` 或 `/storage/*` 添加对象/blob/按文件的端点，也绝不要让 `storage.Storage` 或 `storage.Controller` 知道用户、权限或协议。参见 [Storage](#存储)。

- **Pages 功能始终启用** —— pages 子系统（通过 Caddy 托管静态站点）在启动时无条件初始化；不存在 `TOWN_OS_PAGES` 环境变量开关。正常启动时 pages manager 非 nil，因此 pages API 始终可用。处理器仍保留一个防御性的 nil-manager 保护，返回 "pages not configured"（由那些构建服务器时不带 `ServerConfig.PagesMgr` 的测试触发），但真实启动永远不会走到那里。

- **版本变更检测与单元重启** —— systemcontroller 通过比较运行中容器的镜像 SHA（取自 `/proc/1/cgroup` → `podman inspect`）与持久化在 `<btrfsPath>/town-os-version` 的版本文件来检测镜像升级。版本变更时：(1) 拉取所有容器镜像，(2) 重建 NC 镜像，(3) reconcile 重新生成所有 systemd 单元，(4) 内容发生变化的单元按顺序重启：先 NC 单元（它们拥有网络），再依赖服务，最后父服务/独立服务，(5) 对于单元发生变化的容器包，通过 `podman exec` 执行更新后命令（`post_update` 字段）。版本文件在 reconcile 成功之后写入。单元内容通过 `ReadUnit()` 在前后比对，以避免内容未变时的无谓重启。

- **网络控制器镜像是拉取的，而非启动时构建** —— NC 镜像是一个已发布的同族镜像（`quay.io/town/networkcontroller:<tag>`，标签来自 `resolveImageTag()`），与其他核心镜像一同拉取，就像 UI、rolodex 与 ingress 镜像一样。它**不会**在启动过程中用 `podman build` 构建；早先的启动时构建（`localhost/town-os-networkcontroller:local`，alpine 基础镜像，`--dns=8.8.8.8`）已经移除。`NC_IMAGE` 覆盖推导出的默认值，集成测试框架正是用它注入本地构建的镜像。拉取失败不是致命的：每个包的 NC 单元都带有一个 `ExecStartPre` 的 `--pull=never` 网络创建兜底，因此失败的拉取可以在下次启动时恢复。

- **所有监控服务都是系统服务** —— Prometheus、Node Exporter 与监控 UI 全部运行在系统服务命名空间下（`town-os-system--` 前缀），在 reconcile 之前直接由 `main.go` 启动。它们从不通过包仓库系统安装；不存在可安装的 "monitoring" 包。这三个服务是：`town-os-system--node-exporter.service`（宿主机网络，端口 9100）、`town-os-system--prometheus.service`（端口 9090，从 `{btrfsBase}/monitoring/` 绑定挂载配置与数据）、`town-os-system--monitoring-ui.service`（端口 5308）。监控 UI 服务运行的要么是 socat 转发器（uPlot 模式，默认），要么是 Grafana（grafana 模式），由 `monitoring_backend` 设置控制。Prometheus 配置直接写入磁盘。Prometheus、Grafana 与 uPlot 的 socat 转发器都通过设置了 `PackageUnitConfig.SystemServiceKey` 的 `systemd.GeneratePackageUnits` 生成，因此它们同样获得完整的网络控制器、socket 激活与私有 podman 网络——与普通包相同的管路，只是采用系统服务的命名。

- **宿主机卷的属主在 `HostVolumeMount` 上以声明方式指定，且不递归** —— 内部 uid 写死的容器镜像（Grafana 的 `472`、Prometheus 的 `65534` 等）需要写入其绑定挂载的宿主机路径，而绑定挂载会直接透传宿主机的属主信息，因此宿主机路径必须在容器启动前就属于该 uid:gid。我们使用绑定挂载（而不是命名的 podman 卷，那样 podman 会在首次创建时自动 chown），因为我们希望数据位于带配额的 btrfs 子卷上。`src/systemd/unit.go` 中的 `systemd.HostVolumeMount` 结构体带有可选的 `UID *uint32` 与 `GID *uint32` 字段；当两者都设置时，单元生成器会为该挂载点在 `ExecStartPre=/bin/mkdir -p` 各行之后、`podman run` 之前发出 **`ExecStartPre=/bin/chown <uid>:<gid> <hostpath>`**（不带 `-R`）。这是系统服务上宿主机绑定挂载卷属主的唯一声明式来源，取代了此前在 `GrafanaPackageConfig` 与 `PrometheusPackageConfig` 中手写的 `ExecStartPreExtra` chown 条目。

  chown 刻意不递归，这已经足够，原因是：
  1. **可写挂载**（`grafana-data` → `/var/lib/grafana`，`prometheus-data` → `/prometheus`）只需要顶层属主正确，容器就能在其中创建自己的子目录。容器进程以自己的 uid（472 或 65534）创建这些子项，因此它们本来就属主正确，永远不会漂移。无需递归。
  2. **只读挂载**（`grafana-provisioning` → `/etc/grafana/provisioning`）根本不声明 UID/GID，也不会产生 chown 行。只要宿主机权限是 0755/0644（`WriteGrafanaProvisioningFiles` 就是这样设置的），任何 uid 都能读取其内容，与属主是谁无关。

  `EnsureGrafanaStorage`（`src/monitoring/monitoring_ui.go`）现在只创建目录然后返回；它完全不做 chown。`WriteGrafanaProvisioningFiles` 以全局可读的权限写出数据源与仪表板的 YAML/JSON 文件，之后无需再修正属主。过去每次启动都遍历 `grafana-data` 的、基于 `filepath.WalkDir` 的进程内 chown 已经移除；由 systemd 发出的那一次 `chown` 系统调用就是权威的修正。uid/gid 常量仍留在各自的文件中（`monitoring_ui.go` 中的 `grafanaUID = 472` / `grafanaGID = 472`，`prometheus.go` 中的 `prometheusUID = 65534` / `prometheusGID = 65534`）；除非上游容器镜像也随之改变，否则不要改动它们。

- **网络状态目录必须与宿主机共享** —— `-network-state` 的默认值是 `/run/town-os`（`src/svc/systemcontroller/cmd/systemcontroller/main.go` 中的 `DefaultNetworkStatePath`）。systemcontroller 运行在容器内，却通过 `CONTAINER_HOST` 在宿主机上创建 NC 容器，因此绑定挂载的源路径（每个 NC 单元中的 `-v /run/town-os:/run/town-os:ro`）必须存在于宿主机文件系统上。install 仓库中的 systemcontroller systemd 单元必须绑定挂载 `/run/town-os:/run/town-os`，并确保在 systemcontroller 启动之前该宿主机目录已经存在（`ExecStartPre=/usr/bin/mkdir -p /run/town-os` 或 `RuntimeDirectory=town-os`）。没有这个挂载，systemcontroller 的 `os.MkdirAll` 与状态文件写入都会落在容器的 tmpfs 里，宿主机目录并不存在，NC 容器随即以 `Error: statfs /run/town-os: no such file or directory` 启动失败——进而拖垮 Prometheus、监控 UI 以及每一个带网络的包。绝不要把默认值设为 `/var/run/town-os`，或 `/var/run`、`/tmp` 之下的任何路径；该路径必须位于 `/run` 之下（或另一个与宿主机共享的绑定挂载中），并且在挂载两侧必须是同一个路径。

## System Controller 启动顺序

`src/svc/systemcontroller/cmd/systemcontroller/main.go` 中的 system controller 启动严格遵循以下顺序。标注 **(非致命)** 的步骤在失败时向 stderr 记录日志并继续；其余步骤失败即为致命，会中止启动。

启动过程是**可观测的**：在任何工作开始之前就先绑定 `:5309`，由一个最小化的启动状态桩（stub）承接并流式推送进度；完整的 Echo 路由器在最后被换入，全程不关闭监听套接字。进度以五个粗粒度阶段上报（`boot_controller`、`boot_dns`、`boot_services`、`restart_packages`、`ready`）——参见 [Boot Status and Refresh](#启动状态与刷新)。

1. **设置 `CONTAINER_HOST`** —— `setupPodmanEnv()` 设置 `CONTAINER_HOST=unix:///run/podman/podman.sock`，使后续每一次 `podman` 调用（以及子进程）都走宿主机的 podman socket，而不是 systemcontroller 容器隔离的存储。
2. **解析命令行标志与环境变量** —— `-db`、`-btrfs`、`-repo-dir`、`-network-state`、`-listen`。环境变量覆盖：`TOWN_OS_LISTEN`。
3. **用启动处理器绑定 `:5309`** —— `NewBootStatus()` + `NewRootHandler(NewBootHandler(bs))` 在任何启动工作之前立即绑定监听。在第 24 步的切换发生之前，该套接字只应答 `GET /status/ping`（503，附 `{booting, step, done, boot_id}`）与 `GET /boot-status`（SSE）；其余一律 403。
4. **阶段 `boot_controller`** —— 临时工作目录；创建 btrfs 基础目录与网络状态目录；清除旧部署遗留在 btrfs 根上的陈旧 `town-os.db`（`cleanupStaleRootDB`），并拒绝会重新创建它的 `-db` 路径（`validateDBPath`）——运行时数据库位于 `<btrfsBase>/data/db/system.db`，绝不在根目录。
5. **打开 SQLite 数据库** —— 设置了 `-db` 则持久化，否则使用临时文件。
6. **初始化账户 manager** —— 创建 accounts 表并迁移旧表（能力列转为 grants；`smb_nt_hash` 被丢弃）。随后 `PurgeLegacyServiceAccounts` **(非致命)** 在升级后的首次启动时，一次性移除对象存储守护进程旧的管理员账户及其存储的密码——参见 [No service accounts](#没有服务账户)。
7. **生成临时 JWT 签名密钥** —— 通过 `crypto/rand` 取 32 字节随机数，可用 `TOWN_OS_SIGNING_KEY` 覆盖。初始化会话 manager，它会清除此前所有会话（旧令牌在新密钥下无效）。
8. **初始化审计、设置、pages 与网络 manager** —— 设置项以默认值播种（`default_quota`、`max_archive_size`、`locale`、`dns_tld`、`dns_resolution_mode`、`peer_ttl` 等）；pages 始终初始化；网络 manager 拥有 WireGuard 网络表与 peer 表，**并播种 home 网络**，因此从此刻起它必然存在（参见 [The home network always exists](#home-网络始终存在)）。
9. **播种仓库** —— 若 `repositories.json` 不存在，写入默认仓库（若设置了 `TOWN_OS_TEST`/`DEBUG` 则写入测试仓库）。应用 `TOWN_OS_REPO_USERNAME`/`TOWN_OS_REPO_PASSWORD` 凭据。
10. **初始化仓库根并强制刷新** —— 通过 go-git 克隆/拉取所有已配置的仓库。
11. **初始化安装 manager、btrfs 存储、systemd manager**。
12. **解析镜像标签** —— `resolveImageTag()`：优先取 `TOWN_OS_TAG` 环境变量（由 install 构建系统设置），否则取 `rc.latest-<arch>`（`defaultVersionTag()`，架构由 `runtime.GOARCH` 经 `archTag()` 映射为 `x86_64`/`aarch64`）。不存在 `/town-os.tag` 文件，也没有编译期的 `Version` 固定值。每一个同族镜像标签（UI、rolodex、network controller、ingress）都由这一个值推导；推送标签是按架构分的，因此推导出的同族标签也是。
13. **推导 NC 镜像** —— `quay.io/town/networkcontroller:<tag>`，可通过 `NC_IMAGE` 覆盖。它是拉取的（第 17 步），从不构建。
14. **启动后台仓库刷新** —— goroutine 每 5 分钟轮询一次。
15. **阶段 `boot_dns`：写入 Rolodex 配置，内容变化则重启** **(非致命)** —— Rolodex 是由 systemd 管理的启动服务。systemcontroller 写出 `rolodex.yml`（幂等：若该文件比二进制更新且内容未变则跳过），并且仅在文件确实被写入时才重启服务。`resolution.mode` 来自 `dns_resolution_mode` 设置；存储值无法解析时回退到默认值，而不是渲染出一份 rolodex 会拒绝的配置。`forwarders:` 来自 `dns_local_forwarders` 设置：开启时，该列表在每次启动时从宿主机的解析器中发现，因此换了网络的机器无需操作者做任何事就能用上新的解析器（参见 [Local forwarders](#本地转发器)）。rolodex 容器以 `--net host` 运行，并直接把 DNS 绑定到 `127.0.0.2:{port}`。随后等待 DNS 就绪（TCP 连接轮询），并配置 systemd-resolved 把该 TLD 路由到 rolodex——**当 `TOWN_OS_DNS_PORT` 已把 rolodex 从 `:53` 迁走时，这一步被跳过**，因为 resolved 的按域名服务器地址不携带端口，那样会让该 TLD 下的每一次查询都被黑洞吞掉。
16. **读取监控后端并发现 btrfs 磁盘设备** —— `monitoring_backend`（默认 `uplot`）；`monitoring.BtrfsDevices(btrfsPath)` **(非致命)** 通过 `/monitoring/status` 暴露底层块设备。
17. **阶段 `boot_services`：拉取核心容器镜像** **(非致命)** —— NC 镜像、Prometheus、Node Exporter、UI 镜像，以及在选中该后端时的 Grafana，通过 `parallelEnsureImages` 并行拉取（镜像已加载时跳过拉取）。
18. **启动监控系统服务** **(全部非致命)** —— 先拆除上一版设计遗留的 NC/socket 监控单元（它们仍占用 `-p 9090`/`-p 5308`，会让新服务不断崩溃重启）。Node Exporter、Prometheus 与监控 UI 都以 `--net host` 运行；node-exporter 与 Prometheus 绑定环回地址，只有监控 UI 的 `:5308` 面向局域网。这三个端口都来自 `monitoringPortsFromEnv()`，其零值即为生产默认值（[System-service host ports](#系统服务的宿主机端口)）。随后安装每夜执行的 podman prune 定时器 **(非致命)**。
19. **确保本地 TLS CA 存在** **(非致命)** —— 在 reconcile 之前执行 `tls.EnsureCA(<btrfsPath>/tls)`，这样 reconcile 遍历已安装包时才能签发叶子证书。
20. **启动 ingress 与 pages 服务** **(非致命)** —— `ingressctl.Manager` 安装并启动 `town-os-system--ingress`（共享的 `:443` SNI + `:80` Host 路由器），仅当宿主机拥有全局 IPv6 时才启用双栈。pages 的 Caddy 服务随之启动。当 `INGRESS_IMAGE` 被显式设为空时（开发模式），两者都会跳过。
21. **Reconcile 对象存储** **(非致命)** —— `ReconcileGfeh` 确保每个网络有一个 gfeh 分区：`gfeh/<network>` 子卷（chown 给 uid 2000）、渲染出的 `gfehd.yaml`，以及 `town-os-system--gfeh-<network>` 单元，且仅在渲染内容发生变化时才重启。当 `GFEH_IMAGE` 被显式置空时整体跳过；当 ingress 被禁用时也跳过（四个 HTTP 视图只能经由它访问）。分区的*名称*会在稍后异步发布——见第 30 步。参见 [Object Storage (gfeh)](#对象存储gfeh)。
22. **检测版本变更** —— 将运行中容器的镜像 SHA（`/proc/1/cgroup` → `podman inspect`）与 `<btrfsPath>/town-os-version` 比较。为 reconcile 设置 `versionChanged`。
23. **Reconcile** —— 遍历所有已安装的包并恢复运行时状态：
    - 创建根 btrfs 子卷（`installed`、`uninstalled`、`archives`、`pages`、`vm-images`、`user`、`tls`、`gfeh`）。
    - 对每个已安装的包（每个 repo/name 取最新版本）：加载 YAML，用保存的应答编译，创建带配额的 btrfs 卷，从归档/git/proton 播种空卷，应用文件模板，签发该包的 TLS 叶子证书，写出网络状态文件（含解析后的 `fqdn`），生成并安装 systemd 单元（service + NC + socket），启动服务。
    - 若 `versionChanged`：重启内容发生变化的单元（先 NC，再依赖，最后服务），然后执行 `post_update` 命令。
    - Reconcile pages：确保子卷、符号链接与页面内容就位。
    随后把当前镜像 SHA 持久化到 `<btrfsPath>/town-os-version`。
24. **Reconcile DNS 与网络** —— 拨号 rolodex 的 gRPC socket（最多重试 30 秒）。`RebuildDNS` 清空并从零重建 rolodex，从而丢弃上一次崩溃运行留下的漂移；`RebuildNetworkDNS` 为非默认网络的包重新注册面向局域网的全局记录（以及 DANE pin）。随后 `ReconcileNetworks` 将 home 网络的 TLD 与 `dns_tld` 对齐，并拉起每一个已启用网络的 WireGuard 接口，同时传入 rolodex 客户端，使每个网络的 TLD 作用域都被认领——包括仅 DNS 的 home 作用域。全部非致命。之后对象存储会被**第二次** reconcile（幂等），这样本步骤拉起的网络无需等待重启即可获得自己的分区。
25. **编排 ingress** **(非致命)** —— 等待就绪，拨号其 gRPC socket，`RebuildIngress` 以声明式方式推送完整路由集（HTTP 包 + pages + 对象存储的视图与索引），与 `RebuildDNS` 是同一模型。它还会在同一遍中，从构建这些路由所用的同一站点集合渲染每个分区的索引页——路由不能在它所服务的字节存在之前就被编排（[The partition index](#分区索引页)）。
26. **启动 UI 容器** **(非致命)** —— `town-os-system--ui.service`；当 `UI_IMAGE` 被显式置空时跳过（开发模式，此时由 bun 提供 UI）。
27. **阶段 `restart_packages`：新鲜度阶段** —— 若上一个进程留下了刷新标记，则串行重启每一个已安装的包单元，并为每个包发出一条进度事件，让 UI 各渲染一行。崩溃遗留的陈旧标记是无害的。
28. **创建 HTTP 处理器** —— 把所有 manager 接入 `ServerConfig`，启动后台轮询器（每小时的外部 IP、DNS 漂移修复、过期 peer 回收），并配置 Echo 路由器的 CORS、失败即拒的 grant 白名单、鉴权与审计中间件。
29. **阶段 `ready`：切换根处理器** —— 在已经绑定的监听套接字上，把启动桩原子地替换为完整的 Echo 路由器，因此不会出现端口抖动，进行中的 `/boot-status` SSE 订阅者也能安然跨过这次交接。随后 `BootStatus.Done()` 关闭该事件流。**系统至此就绪。**
30. **发布对象存储的名称** **(非致命，后台)** —— `publishGfehNames` 等待至少一个分区的管理 socket 有应答，然后重新运行 DNS 与 ingress 重建，使每个分区 `/v1/names` 的输出变成 A 记录、TLSA pin、叶子证书 SAN 与 ingress vhost。它在切换**之后**、且以异步方式运行，因为 gfehd 在认证之前会轮询 `/status/ping`——而后者在第 29 步之前一直返回 503——所以在此处同步等待会让它所等待的这次启动自我死锁。若届时没有任何分区就绪，这些名称会由下一次 reconcile 发布。
31. **优雅关闭** —— 收到 SIGINT 时：取消 context，以 30 秒超时关闭 HTTP 服务器。所有后台 goroutine 通过 context 取消退出。

# Town OS 功能规格说明

Town OS 是面向家庭用户的自托管云平台。它完全从 U 盘在内存中运行，把系统的全部存储用于用户数据。打包、存储与网络是完全一体化的。一个 Web UI 为非技术用户提供管理界面。

## Git 库

所有内部 git 操作都使用纯 Go 库（`go-git/go-git/v5`），而不是调用 `git` 命令行。

### 客户端接口

`git.Client` 接口抽象了所有 git 操作：

- **Clone** —— 把仓库克隆到父目录下的一个具名子目录中。
- **Pull** —— 以 rebase 方式拉取。
- **Diff** —— 报告工作树是否存在未提交的改动。
- **Stash / StashApply** —— 暂存与重新应用未提交的改动。
- **Fetch** —— 从 origin 远端拉取。
- **Checkout** —— 检出分支、标签或提交哈希。
- **Init** —— 初始化新仓库。若父目录不存在则返回错误。
- **Add** —— 按 pathspec 暂存文件（支持用 `"."` 表示全部文件）。
- **Commit** —— 使用本地 git 用户配置创建提交（回退为 `Town OS <town-os@localhost>`）。
- **RevParse** —— 把一个引用解析为 SHA 哈希。
- **Run** —— 分发任意 git 子命令（`config`、`branch`、`rev-parse --abbrev-ref`、`log`、`init`、`status`）。

### 实现

`GoGitClient` 使用 `go-git` 实现该接口。它支持：

- URL 中内嵌的凭据（`scheme://user:pass@host/...`），会被提取并作为 `http.BasicAuth` 传递。
- 所有操作都支持基于 context 的超时与取消。
- 一个 `Home` 字段，可覆盖 HOME 目录以进行隔离操作。

### Mock 客户端

`MockClient` 提供线程安全的 mock 实现，用于单元测试。它记录所有方法调用及其参数，并支持按方法注入错误与返回值。

### 使用场景

- **包仓库**：克隆、拉取（对脏工作树前后配合 stash/apply）与 fetch，用于仓库刷新（通过 `GoGitClient`）。
- **卷播种**：在安装与 reconcile 期间把 git 仓库克隆进空卷（通过 `GoGitClient`）。
- **Pages**：克隆并更新静态站点仓库（通过 `GoGitClient`）。
- **Git 源重建**：更新已安装包的 git 卷并重启依赖它的服务（通过 `GoGitClient`）。

## 仓库管理

### 仓库模型

仓库由名称、URL 与可选凭据（用户名与密码）定义。它们存储在基础目录下的 `repositories.json` 文件中。若未配置任何仓库，则播种一个默认仓库。

### 仓库 API

- `POST /repository/add`（需要管理员）—— 添加新仓库。接受名称、URL 与可选的用户名/密码凭据。若未提供凭据，则使用系统默认凭据。仓库会通过 go-git 克隆，并触发一次刷新。
- `POST /repository/remove`（需要管理员）—— 按名称移除仓库并触发刷新。
- `POST /repository/move`（需要管理员）—— 改变仓库的优先级位置。接受名称与目标位置索引。
- `POST /repository/refresh`（需要管理员）—— 强制刷新所有仓库。返回任何刷新错误。
- `GET /repository`（需要鉴权）—— 列出所有仓库，支持搜索、排序与分页。每一项包含名称、URL、用户名，以及任何刷新错误。

### 仓库刷新

仓库会周期性刷新（默认间隔 5 分钟），通过 go-git 从 origin 拉取。刷新过程中对脏工作树前后使用 stash/apply。刷新错误按仓库跟踪，并通过列表接口与状态 ping 接口暴露。

## 包系统

### 包定义

包以 YAML 定义，结构如下：

- `image` —— 容器镜像引用（与 `vm` 互斥）。
- `vm` —— 虚拟机配置（与 `image` 互斥）。见下文 **VM 配置**。
- `proton` —— 用于 Windows 可执行文件的 Proton/Wine 运行器配置（与 `vm` 和 `command` 互斥）。见下文 **Proton 配置**。
- `entrypoint` —— 字符串列表，在 podman run 时替换镜像内置的 `ENTRYPOINT`。以 `podman run --entrypoint='["..."]'` 形式发出（JSON 数组，用单引号包裹，使 systemd 原样转发）。对于上游 ENTRYPOINT 是一个拒绝任意命令参数的包装脚本的镜像，这是必需的（例如 `matrixdotorg/synapse` 的 `/start.py` 把第一个参数解释为 "mode"，遇到任何未知值就报错——因此想用 `command: [sh, -c, "…"]` 的包必须同时设置 `entrypoint: [sh, -c]`，让 podman 彻底替换掉 `/start.py`）。仅限容器运行时；对 VM 包会被拒绝（`ErrEntrypointVMNotSupported`），对 Proton 包也会被拒绝（Proton 会自动生成自己的命令）。
- `command` —— 字符串列表，成为容器的 CMD（在 entrypoint **之后**传入的 argv）。仅限容器运行时；与 `proton` 互斥。包含空白或 shell 元字符的多词参数会在生成的单元文件中用单引号包裹，使 systemd 的 ExecStart 分词器把它们作为单个 argv 元素转发——一个串联的 `"a && exec b"` 字符串仍是一个参数，其中的 `&&` 会被转发给 `sh -c`（当 entrypoint 为 `[sh, -c]` 时），而不是被 systemd 拆开。
- `environment` —— 键值形式的环境变量（支持模板替换；仅限容器运行时）。
- `network` —— 外部与内部端口映射（支持模板替换）。
- `volumes` —— 具名卷，含挂载点、可选配额、可选归档来源、可选 git 播种 URL，以及可选的 UID/GID。
- `questions` —— 安装期间向用户呈现的具名问题。
- `notes` —— 带类型的元数据（URL、电话、邮箱），安装后展示。类型在编译期校验：URL 必须能解析为合法 URL，邮箱必须匹配 `user@domain.tld` 格式，电话号码必须是数字加可选的格式化字符。
- `description` —— 人类可读的包描述。
- `supplies` —— 该包提供的能力列表。
- `archives` —— 安装时用于填充卷的容器镜像归档列表（仅限容器运行时）。
- `templates` —— 具名文件模板，通过 Go text/template 渲染进卷中。每个模板指定目标卷、文件路径与模板内容。
- `post_update` —— 在 reconcile 期间检测到镜像 SHA 变化后，于运行中的容器内执行的 shell 命令列表（仅限容器运行时；VM 包不支持）。见下文 **更新后命令**。

### 运行时类型

每个包都有运行时类型：`container`（默认）或 `vm`。运行时由出现的顶层字段决定：`image`（或 `proton`）选择容器运行时（podman），`vm` 选择 VM 运行时（QEMU）。一个包必须恰好指定 `image`/`proton` 与 `vm` 之一；两者都指定或都不指定都是校验错误。Proton 包是容器包的一种特化形式——它们使用容器运行时，但会自动生成命令，并从另一个容器镜像中提取 Windows 应用文件。

### VM 配置

`vm` 段配置一台 QEMU 虚拟机：

- `image` —— VM 磁盘镜像 URL 或本地文件名（必填）。可以是指向远程镜像的 HTTP/HTTPS URL，也可以是引用 `vm-images` 子卷中已缓存镜像的文件名。支持 `@variable@` 模板替换。
- `memory` —— VM 内存，人类可读的字节字符串（例如 `2gb`、`512mb`）。默认 `1gb`。支持 `@variable@` 模板替换。
- `cpus` —— 虚拟 CPU 数量。默认 `1`。必须为非负数。

### Proton 配置

`proton` 段配置一个通过 Proton/Wine 兼容层运行的 Windows 应用：

- `app_image` —— 包含 Windows 应用文件的容器镜像引用（必填）。编译期会被规范化。支持 `@variable@` 模板替换。
- `app_directory` —— 容器内应用安装位置的绝对路径（必填，例如 `/app`）。支持 `@variable@` 模板替换。
- `volume` —— 应用文件将被提取到的、已定义的包卷名称（必填）。支持 `@variable@` 模板替换。
- `exe` —— 要运行的 Windows 可执行文件路径（必填，例如 `/app/myapp.exe`）。支持 `@variable@` 模板替换。
- `args` —— 传给可执行文件的可选命令行参数。每个元素都支持 `@variable@` 模板替换。

安装时，系统拉取 `app_image`，把 `app_directory` 提取到指定卷中，并自动生成容器命令 `proton run <exe> [args]`。用于运行该应用的容器镜像取自系统级的 `proton_image` 设置（默认 `quay.io/town/proton:latest`），可通过设置 `image` 按包覆盖。在 reconcile 期间，仅当目标卷为空时才会重复执行应用提取。

### 模板变量

模板替换使用 `@variable_name@` 语法。变量在包编译期被替换为问题的应答。替换适用于：环境变量值、网络端口名称与目标、卷挂载点、卷配额、卷归档引用、卷 git URL、VM 镜像 URL，以及 VM 内存值。另有两个内置变量可用：`@LOCAL_EXTERNAL_HOST@` 与 `@LOCAL_INTERNAL_HOST@`。

`@@` 序列是字面量 `@` 的转义。若要产生一个字面 `@` 紧跟一个模板变量，使用三个 `@`：`@@@variable@`。例如 `ssh://git@@@PACKAGE_DNS@:@sshport@` 解析为 `ssh://git@gitea.default.home:2222`。单独的 `@@` 解析为 `@`（例如 `admin@@example.com` → `admin@example.com`）。

注意：note 的编译使用单遍解析器（`ApplyTemplates`），它把上下文变量（`PACKAGE_DNS`、`LOCAL_EXTERNAL_HOST`、`LOCAL_INTERNAL_HOST`）与用户应答合并到一遍中处理，从而正确处理 `@@` 转义。其他字段（环境变量、端口、卷）使用按键解析器（`applyTemplate`），它在多遍处理中保留 `@@`，并在 `Compile` 结束时做最后一次 `@@` → `@` 解析。

### 问题（Questions）

问题在包安装期间提示用户。每个问题有 `query`（展示文本）、可选的 `type`（用于校验的输出类型）与可选的 `default` 默认值。问题名称必须以字母或数字开头，且只能包含字母、数字与下划线（例如 `port`、`dbpass`、`registration_secret`）。短横线、点号与其他标点会被拒绝；允许下划线，是因为问题名称会被用作 `@template@` 标记，而 `registration_secret` 这类多词标识符在真实包中很常见。

#### 输出类型

- **port** —— 校验过的端口号（1–65535）。当应答为空或为 `"auto"` 时，自动在 10000–60000 范围内生成一个可用的随机端口。
- **hostname** —— 小写字母数字加短横线。为空时自动生成 `<package-name>-<4位十六进制>`。
- **volume** —— 字母数字加短横线与下划线。
- **bytes** —— 人类可读的字节大小（`mb`、`gb`、`tb` 后缀）。
- **archive** —— 归档文件名。
- **duration** —— 时间长度（`s`、`m`、`h`、`d` 后缀）。
- **secret** —— 当应答为空或为 `"auto"` 时自动生成一个密码学安全的值。通过 `crypto/rand` 生成 32 字节，返回 64 字符的十六进制字符串（256 位熵）。适用于密码、加密密钥盐值等秘密值。用户可提供明确应答来覆盖。
- **boolean** —— 是/否选项，在安装问题对话框中渲染为**复选框**而非文本输入。校验使用 `strconv.ParseBool`，它恰好接受 yaml.v3（YAML 1.2）视为布尔的那些写法，外加 `1`/`0`/`t`/`f`，且不区分大小写；`yes`/`no` **不被**接受。应答会被规范化为字符串 `"true"` 或 `"false"`，因此 `@variable@` 替换与文件模板（`{{.Responses.key}}`）看到的始终是同一种规范形式，可以用 `{{if eq .Responses.key "true"}}` 判断。

  未勾选的复选框不会提交任何内容，而依赖包的布尔问题也常常不被其父包回答——若不处理，这两种情况都会触发 `Compile` 的空应答校验。因此 `autoGenerateResponses`（`controller_install_preview.go`）会把缺失或为空的布尔值解析为该问题的 `default`（规范化后），若未声明默认值则解析为 `"false"`。来自表单的显式 `"false"` 始终优先于 `default: "true"`，这样默认开启的选项才真的能被关掉；无法被 `strconv.ParseBool` 解析的 `default` 是包的缺陷，会让安装失败，而不是悄悄以关闭状态安装。

  包信息对话框把保存的布尔应答渲染为 Yes/No，而不是原始的 `"true"`/`"false"` 字符串；布尔问题在安装对话框中绕过"缓存值 + 清除按钮"的路径——已保存的应答只是预先勾选复选框，并且保持可直接编辑。

- **oauth** —— 通过在安装对话框中执行设备流（device flow）获得的令牌，而非手工输入。校验方式与 secret 相同（任意非空字符串），从不自动生成，并在包信息对话框中被掩码。安装对话框在文本框的位置渲染一个 **Connect** 按钮；来自上一次安装的缓存应答会渲染为"已连接"，因此重装不会把操作者再赶回供应商那里一趟。

#### OAuth 问题

有些应用需要一个只有其供应商才能签发的凭据——Plex 账户令牌、GitHub 个人令牌——而获取它的唯一办法一直是手工运行一个 shell 脚本，再把它打印的内容粘贴过来。`oauth` 问题改为直接在对话框中执行该流程。

**不存在供应商注册表**。问题自带一个 `oauth:` 块，其中写明供应商自己的 URL，因此任何提供设备式流程的供应商都可以被包使用，而无需改动 Town OS：

```yaml
questions:
  plextoken:
    query: "Plex account"
    type: oauth
    oauth:
      start: { method: POST, url: "https://plex.tv/api/v2/pins?strong=true", headers: { X-Plex-Client-Identifier: "{{client_id}}" } }
      extract: { id: id, code: code }
      approve: "https://app.plex.tv/auth#?clientID={{client_id}}&code={{code}}"
      poll: { url: "https://plex.tv/api/v2/pins/{{id}}", headers: { X-Plex-Client-Identifier: "{{client_id}}" } }
      token: authToken
      interval: 2s
      timeout: 10m
```

`start` 开启流程；`extract` 指定要从其响应中提取的 JSON 字段；`approve` 是浏览器打开的 URL；`poll` 会被反复执行，直到由 `token` 指定的 JSON 字段不再缺失或为 null——在协议层面，这恰恰就是"用户尚未授权"的样子。`{{...}}` 占位符针对提取出的值以及 `{{client_id}}` 解析，后者是控制器在每一步都发送的、每次流程随机生成的标识符（Plex 会把 pin 与它绑定）。提取出的 JSON 数字会被渲染为数字串，而不是 `1.234567e+06`——浮点格式化的 pin id 会让轮询 URL 返回 404，并永远卡在 "pending"。

该流程的实现位于 `src/packages/oauth.go`（模式定义与校验）与 `src/svc/systemcontroller/controller_oauth.go`（执行）。`POST /packages/oauth/start` 执行 start 步骤并返回 `{flow_id, approve_url, user_code, interval_ms}`；`POST /packages/oauth/poll` 执行一次轮询并返回 `pending`、带令牌的 `approved`，或 `expired`。两者都需要管理员权限。服务器只在令牌被兑取之前保留该流程——令牌被交给浏览器，浏览器再像其他任何应答一样把它作为该问题的答案提交，因此在服务端保留副本只会多出一处泄露点。

校验分为两半，把它们混为一谈就是缺陷。`ValidateOAuthSpec` 检查流程的*形状*（必填字段、可解析的时长、URL 的 host 中不含模板），这是安装包时 `Compile` 所执行的。`ValidateOAuthFlow` 是它再加上下面的地址策略，只在流程即将被*执行*时运行。安装发生在流程运行很久之后，而且发生在一台 `Compile` 无法看到其 `OAuthAllowPrivate` 设置的主机上——所以在编译期套用地址策略，会拒绝掉一个其自身流程刚刚成功过的安装。

**地址守卫是关键的承重结构。** 包指定任意 URL，而是*控制器*去拨号它们，因此没有守卫的话，一个包就能把它指向宿主机自己的网络。`packages.CheckOAuthAddr` 运行在 HTTP 客户端的 `DialContext` 中（每次重定向也会执行），拒绝环回、私有、链路本地、组播、未指定与 CGNAT 地址；URL 必须是 `https`。在拨号时而非解析时检查，正是它能防住 DNS 重绑定的原因。`ServerConfig.OAuthAllowPrivate` 放宽这一限制，它的存在仅仅是为了让测试能把流程指向 127.0.0.1 上的 `httptest` 服务器。

#### 可选问题

任何问题都可以设置 `optional: true`。其他所有问题都必须以非空值作答，这让包作者无法表达"应用确实可以没有"的设置项——SMTP 中继、API 密钥——除非编造一个占位默认值，然后指望操作者会覆盖它。

可选问题可以完全不出现在应答 map 中，或以空字符串作答；`Compile` 免除它的 `ErrMissingResponse` 与 `ErrEmptyResponse` 校验，并在其 `@variable@` 出现处替换为**空字符串**。空白应答同样跳过 `OutputType.Output`——后者的职责恰恰是为带类型的问题拒绝空值（空字符串不是合法端口）——因此 `optional` 与 `type` 可以组合：作答过的可选端口仍会按端口校验，而空白的则被编译为无。

有两个细节对正确性至关重要。`Compile` 是通过遍历收到的应答来做替换的，因此完全不出现在 map 中的问题会经过第二遍处理，用空字符串填充它的标记；没有这一遍，字面量 `@smtp_host@` 就会一路残留进容器的环境变量。另外，`autoGenerateResponses` 在进入类型分支之前就跳过可选问题：为它生成值会让"可选"失去意义，因为一个空白的可选 secret 否则就会变成一个随机的 256 位字符串，而应用会老老实实拿它去尝试认证。空白的可选问题若声明了 `default` 则回退到默认值，否则回退到空字符串。

`optional` 对布尔问题没有意义，因为布尔是复选框，永远会解析为它两个取值之一。

#### 条件问题（`show_if`）

问题可以带上 `show_if: <boolean_question>`，指向同一个包中的某个布尔问题。安装对话框在该复选框被勾选之前一直隐藏这个问题，因此包可以把一组高级选项——SMTP 中继、API 密钥——收纳到一个开关之后，而不是把所有字段一次性摊到操作者面前。

它不只是 UI 提示：编译器也遵守它。只要控制它的布尔值解析为 false，该条件问题就被编译为**空字符串**，并免除"必须作答且非空"的要求——完全等同于它被标为 `optional` 且留空——*无论那个仍然挂载着的字段提交了什么*。`questionHidden`（`src/packages/questions.go`）从提交的应答中读取控制值，若操作者从未碰过它则回退到该布尔问题声明的 `default`，并且解析时较为宽松，因为未勾选的复选框可能以 `"false"`、`"0"` 的形式到达，也可能根本不到达。`Compile` 对隐藏的问题强制使用空字符串并跳过 `Output()`，因此陈旧的值不会让一个操作者根本看不见的字段栽在类型校验上；完全不出现在应答 map 中的问题，其 `@marker@` 出现处同样会被填为空字符串。当布尔值为真时，非可选的条件问题照常为必填。

`ValidateShowIf` 会拒绝以下几种 `show_if`：指向不存在的问题（`ErrShowIfUnknown`）、指向类型不是 `boolean` 的问题（`ErrShowIfNotBool`）、指向问题自身（`ErrShowIfSelf`），或指向另一个本身就是条件问题的问题（`ErrShowIfChain`——不允许链式）。只有当控制其可见性的东西是一个纯粹的复选框时，条件问题才是自洽的。

### 编译

编译会校验所有应答、施加类型相关的校验、替换所有模板变量、规范化容器镜像 URL，并产出一个已解析的 `Package` 结构体。对于 VM 包，内存字符串会被解析为字节数，并应用 CPU 默认值。更新后命令会被去除首尾空白。校验错误会被收集起来一并返回。

**任何会进入 systemd unit 的值都不得携带控制字符。** unit 文件是按行组织的，而它的引号不跨行：无论被什么引号包裹，一条指令都在第一个裸换行处结束。因此携带换行的值并不是弄坏它自己那一行——换行之后的一切都会被当作同一个 `[Service]` 小节里的一条**新指令**解析，于是一个形如 `somevalue\nExecStartPre=/bin/sh -c '…'` 的环境变量值就往生成的 unit 里加进了一条 `ExecStartPre`。这跨越的是一条权限边界，而不只是产出了糟糕的输出：包作者本来就掌控镜像与命令，那是对*容器内部*运行什么的权力；而一条 systemd 指令是在 podman 被调用之前，以 **root 身份在宿主机上**运行的。

`packages.ValidateNoControlChars` 拒绝每一个 C0 控制字符与 DEL。**制表符是唯一的例外**——它是合法的空白，而 systemd 的分词器把它当作一个引号确实能够包住的分隔符。

这项检查运行**两次，且两趟都是承重的**：

- `InputPackage.Validate()` 覆盖作者写在 `environment`、`command` 与 `entrypoint` 中的字面量。它在 `Compile` 的*开头*运行，因此它看到的永远是替换之前的文本。
- 在 `Compile` 末尾对**已编译**的包做一趟扫描，覆盖替换之后的一切：环境变量值、command、entrypoint、卷挂载点与 `post_update`。这才是要紧的那一趟。YAML 里写作裸 `@marker@` 的值自身不含控制字符，因而通过 `Validate()`；换行是随*应答*到来的。而一个未声明 `type:` 的问题，根本没有别的东西会校验它——这就使应答这条路径成为真正带着调用方选定的字节抵达 unit 文件的那一条。

`systemd.quoteCommandArg` 作为兜底剥除同样的字符，因为 unit 生成没有错误返回值，而它是字节被写入 `/etc/systemd/system` 之前的最后一个点。它选择**丢弃**而非转义：systemd 确实会在引号内解析 C 风格转义，但当根本没有正当理由去投递这个字节时，把一条安全边界押在解析器的某个细节上不会带来任何收益。

先前能工作的东西没有一样被拒绝。多行的值本来就已经产出坏掉的 unit；改变之处在于它现在会在编译期大声失败，而不是悄无声息地生成一个无人检查的 unit。

### 更新后命令（Post-Update Commands）

`post_update` 字段是一组 shell 命令字符串，在 system controller 于 reconcile 期间检测到镜像 SHA 变化之后，于运行中的容器内执行。这使自动化迁移任务成为可能（例如 PostgreSQL 容器更新后执行 `pg_upgrade`）。

- **仅限容器** —— 对 VM 包，`post_update` 在校验期被拒绝（`ErrPostUpdateVMNotSupported`）。
- **模板替换** —— 每条命令都支持来自问题应答的 `@variable@` 替换，与环境变量和网络字段一致。
- **空白裁剪** —— 每条命令在编译期被去除首尾空白。空命令或仅含空白的命令在校验期被拒绝。
- **执行触发条件** —— 只有当 `ReconcileConfig.VersionChanged` 为真**且**该包的 systemd 单元内容与先前安装的单元不同时，命令才会执行。任一条件不满足，就不会执行任何命令。
- **执行顺序** —— 命令在所有版本变更引发的重启完成之后按顺序执行（先 NC 单元，再依赖，再服务，最后才是更新后命令）。在同一个包内，命令按列表顺序执行。
- **执行方式** —— 每条命令通过 `podman exec <container-name> sh -c '<command>'` 执行，超时为 5 分钟。`ReconcileConfig` 上的 `PostUpdateExec` 函数提供执行机制；为 nil 时禁用更新后命令的执行。
- **非致命** —— 命令失败会被记录，但不会中止 reconcile，也不会阻止后续命令执行。

包 YAML 示例：

```yaml
image: postgres:16
post_update:
  - "pg_upgrade --check"
  - "pg_upgrade"
  - "vacuumdb --all --analyze-in-stages"
```

### 文件模板

模板是包 YAML 中的具名对象，含三个字段：`volume`（目标卷名）、`path`（卷内文件路径）与 `content`（Go text/template 字符串）。

模板数据上下文提供四个命名空间：

- `.Responses.key` —— 问题应答值（以问题名为键）。
- `.Package.Name`、`.Package.Version`、`.Package.Repo`、`.Package.Image`、`.Package.Description` —— 包元数据。
- `.System.Hostname`、`.System.ExternalIP`、`.System.InternalIP` —— 系统级信息。
- `.Dep.KEY.Host` 与 `.Dep.KEY.Ports` —— 已安装依赖的运行时坐标，键名与父包 YAML 在 `dependencies:` 下声明的 dep key 相同。`Host` 是 podman 容器名（可通过共享网络上的 podman DNS 解析）；`Ports` 是 `map[string]string`，同时以数字容器端口（例如 `"5432"`）和该依赖网络条目上声明的任意语义名（小写，例如 `"sql"`）为键。用 `{{index .Dep.db.Ports "sql"}}` 访问具名端口。对于没有依赖的包，该 map 为 nil；对不存在的依赖使用 `{{.Dep.db.Host}}` 会渲染出 `<no value>`（与其他任何缺失的 map 键一样），而对 nil 的 `Ports` 使用 `index` 会刻意报错，从而让配置有误的模板大声失败。

`volume` 与 `path` 字段支持 `@variable@` 替换（与环境变量、网络和卷字段使用的是同一机制）。`content` 字段使用 Go `text/template` 语法，如 `{{.Responses.key}}`、`{{.Package.Name}}`、`{{.Dep.KEY.Host}}` 等。`content` 内部**不**支持 `@dep_*@` 标记形式——请改用 Go 模板的 `.Dep` 命名空间；`@dep_*@` 在 `environment:` 值与依赖的 `responses:` 块中仍是正确形式。

模板在卷播种（归档、git 克隆）**以及所有依赖安装完成之后**才应用，因此渲染父包内容时 `.Dep` 已经填充完毕。reconcile 期间模板会被重新渲染，但已存在的文件绝不会被覆盖，从而保留来自归档上传或先前运行的数据；依赖 map 会从持久化的依赖记录中重建，因此当 reconcile 确实需要写出一个缺失的模板时，`.Dep` 仍然可以解析。

校验强制要求：模板名称遵循卷命名约定（字母数字加点、短横线与下划线），路径必须是相对路径且不含目录穿越，`volume` 必须引用一个已定义的包卷（除非该字段中含有模板变量），并且 `content` 必须能解析为合法的 Go `text/template`。

### 镜像规范化

容器镜像引用在编译期被规范化：
- 单一名称（`nginx`）变为 `docker.io/library/nginx:latest`。
- 两段式（`user/app`）变为 `docker.io/user/app:latest`。
- 完整引用被保留；若不含标签则追加 `:latest`。

### 应答持久化

应答按版本保存在 `responses/<repo>/<pkg>/<version>.json`。另有一份 `last` 副本保存在 `responses/last/<repo>/<pkg>.json`，供升级以及从已卸载卷重新安装时复用。安装成功后 last 应答会被清除。

有两个 API 端点管理 last 应答：

- `POST /packages/last-responses`（需要管理员）—— 取回某个包缓存的 last 应答（按 repo 与 name）。
- `POST /packages/clear-last-responses`（需要管理员）—— 删除缓存的 last 应答文件。

### 安装问题 UI

用户安装包时，问题对话框会加载已有应答（来自当前安装），若不存在则加载缓存的 last 应答（来自上一次卸载）。当前应答优先于 last 应答。

**缓存应答**以只读的样式化容器展示，背景色较淡，显示已保存的值（密码显示为 `********`）。一个隐藏的表单输入保留该值以便提交。每个缓存字段都有一个清除按钮（X 图标）并带工具提示（"Clear to enter a new value"），点击后把只读展示替换为可编辑输入框。清除按钮采用 ghost 样式，悬停时变红。

**默认值**在没有缓存值时以两种方式呈现：作为输入框的占位文本（例如 "Default: 8080"），以及作为输入框下方的浅色辅助文本，其中的值用等宽字体。当未定义默认值时，会展示与类型相关的占位文本：端口为 "Auto-assigned if empty"，主机名为 "Auto-generated if empty"，时长为 "e.g. 30s, 5m, 2h, 1d"。

来自服务器的**校验错误**按字段以红色文本显示在输入框下方，并且输入框会带上红色边框。

**尺寸与分页。** 对话框高度上限为视口高度（减去边距），并以 flex 列布局，因此页眉与页脚保持固定，而问题区域滚动——否则基础 `DialogContent` 的 `overflow-hidden` 会让问题很多的包溢出部分无法触及。问题**每页 5 个**分页展示，配有 Previous/Next 控件，在最后一页让位给 Install 按钮。每一页都保持挂载状态（非活动页为 `display:none`），这样非受控的表单输入会保留已输入的值并且仍会被提交；卸载某一页会悄无声息地丢掉该页上的答案。字段错误会跳转到承载它的那一页，因此校验错误绝不会被藏在分页器背后。分页器复用既有的 `datatable.next`/`previous` 字符串与一个数字页码计数，因此不会新增翻译键。

用 `show_if` 声明的**条件问题**在其控制复选框被勾选之前保持隐藏（参见 [Conditional questions](#条件问题show_if)）。

**OAuth 问题**依据每个问题单一的状态渲染——`idle`、`starting`、`waiting`、`connected`、`error`——该状态由缓存应答播种，而不是由"某处是否存在令牌"决定。过去，来自上一次安装的缓存令牌会让该字段在任何事情发生之前就显示为已连接，并且在一次失败的重连过程中继续如此显示，于是绿色的 Connected 徽标压在红色错误之上。现在令牌只被用于一个判断（Connect 还是 Reconnect），此外它仅仅是隐藏输入所提交的内容：一次失败的重连让操作者仍保有原先的令牌，但不会有任何东西声称这次失败的尝试成功了；正在进行中的重连不会显示为已连接；而一次不携带令牌的授权是错误，而不是会安装一个空凭据的静默成功。

### 包信息对话框

包信息对话框以带标签的列表展示 notes。notes 按其类型渲染：URL 类型是可点击的超链接，在新标签页打开（`target="_blank"`）；email 类型是 `mailto:` 链接，打开用户的邮件客户端；phone 类型是 `tel:` 链接。无类型的 note 渲染为不带链接的纯代码块。

### 包清单 API

`POST /packages/manifest`（需要鉴权）返回某个包的原始 YAML 定义。接受 repo、name 与 version。以 `Content-Type: text/x-yaml; charset=utf-8` 返回文件内容。若包文件不存在则返回 404。

### 包操作下拉菜单

在包列表 UI 中，每一行包都有一个 `...` 下拉菜单（平铺视图与按仓库分组视图都有）。下拉菜单包含：

- **Info**（仅已安装的包）—— 打开包信息对话框，展示问题、应答与编译后的 notes。
- **Manifest** —— 打开一个对话框展示原始 YAML 包定义，并附带复制按钮。
- **Version/Repository** —— 以禁用项的形式展示版本与仓库名。
- **Uninstall**（仅已安装的包）—— 触发卸载确认对话框。

### 精选包（Featured Packages）

每个仓库都可以包含一个 `featured.json` 文件，内含一个包名的 JSON 数组。它们由 `LoadFeatured` 加载，并随包列表一起在 `RepoPackageGroup` 中返回。平铺的包列表 API 会为每一项设置 `featured` 布尔值。分组的包列表 API 即使在搜索过滤缩减了包列表时，也会在每个分组上保留 `Featured` 数组。

- `GET /packages`（需要鉴权）—— 列出包，支持搜索、排序、分页，以及可选的 `featured_only` 与 `installed_only` 过滤。
- `GET /packages/featured`（需要鉴权）—— 列出所有仓库中的精选包。
- `GET /packages/by-repo`（需要鉴权）—— 按仓库分组列出包。接受 `search` 与 `featured_only` 查询参数。

#### 精选包过滤器

平铺的包列表 API（`GET /packages`）与分组的包列表 API（`GET /packages/by-repo`）都接受 `featured_only` 查询参数。设为 `"true"` 时只返回被标记为精选的包。该过滤与 `installed_only` 取交集——两者可同时生效。在 UI 中，一个 "Featured only" 复选框切换该过滤。精选过滤的默认状态是 `true`（首次访问时只显示精选包）。过滤偏好（`pkg_group_by_repo`、`pkg_installed_only`、`pkg_featured_only`）持久化在 `localStorage` 中。

### 已安装包过滤器

平铺的包列表 API（`GET /packages`）接受 `installed_only` 查询参数。设为 `"true"` 时只返回已安装的包。过滤在服务端于搜索、排序与分页之前施加，确保页数与偏移量正确。在 UI 中，一个 "Installed only" 复选框切换该过滤并把分页重置到第一页。

### 包的安装与卸载

#### 安装 API

`POST /packages/install`（需要管理员）安装一个包。接受 repo、name、version、responses 与可选标志：

- `reuse_volumes` —— 复用先前已卸载版本的卷。
- `import_from_version` —— 从指定的先前版本导入卷。
- `skip_response_reuse` —— 不从先前安装自动填充答案。

安装过程会：从仓库中的包文件创建硬链接到 installed 目录，持久化应答，创建带配额与可选 UID/GID 的卷，从归档与 git 播种卷（仅限容器运行时），应用文件模板，生成 systemd 单元文件，写出网络状态文件，安装并启动 systemd 单元，并在成功后清除 last 应答。last 应答在安装前保存，以便卸载时恢复。对于 VM 包，VM 磁盘镜像会在生成单元之前被下载并转换为 raw 格式（若为远程 URL）；卷播种（归档、git 克隆）则被跳过。

#### 卸载 API

`POST /packages/uninstall`（需要管理员）卸载一个包。接受 repo、name、version 与可选标志：

- `purge_volumes` —— 立即删除所有相关卷。

不做清除时，卷会从 `installed/` 前缀移动到 `uninstalled/` 前缀。网络状态文件被删除，systemd 单元被停止、禁用并卸载。

**依赖级联。** 卸载父包会递归卸载它拥有的每一个依赖。级联读取父包持久化的依赖记录（`LoadDependencies`），并深度优先遍历每个子项，在每一层都重复查找，因此嵌套的子依赖（`parent--dep--child--dep--grandchild`）也会被移除。对每个依赖，级联会注销其 DNS 记录、卸载其 systemd 单元（service + NC + socket）、删除其网络状态文件、调用 `inst.Uninstall` 丢弃安装记录，并根据 `purge_volumes` 是否设置，要么清除其卷，要么把它们移动到 `uninstalled/` 前缀。级联在 `uninstallDependencies`（`src/svc/systemcontroller/controller_install_dependencies.go`）中实现，并在父包自身卸载完成之后运行。不存在引用计数：每个依赖恰好属于一个父包（其安装记录位于 `installed/<repo>/<parent--dep--key>/`），因此在两个父包下分别安装的"共享"依赖其实有两份独立记录，卸载其中一个父包只会移除它自己的那一份。

#### 已安装包信息

`POST /packages/installed/info`（需要鉴权）返回某个已安装包的问题、应答、编译后的 notes 与 note 类型。

**非管理员只拿得到 notes，别无其他。** 该路由保持 `requireAuth`，因为仪表盘会为每个账户渲染每个已安装服务的 notes——那正是 notes 的用途——但 `type: secret` 的问题的答案是生成的凭据，`type: oauth` 的答案是供应商令牌，因此把完整的应答 map 返回给任何有登录权的人，就等于把每个包的凭据都交出去。问题本身也被扣下：一个问题的 `query` 无害，但把它与一份被涂黑的应答 map 配对，只会告诉对方有什么东西被藏起来了；而唯一渲染问题的界面是仅限管理员的安装对话框。仅仅丢掉这个 map 还不够——note 正是由这些答案编译出来的，因此 `redactSecretsInNotes` 会掩码任何被 note 引用到的 secret 或 oauth 答案，且按值匹配，这样从不引用它们的 note 会被完整保留。短于六个字符的答案不作处理：两个字符的 secret 并不是任何人选择的凭据，而掩码它的每一次出现只会把无关的 note 文本撕得粉碎。

#### 包版本

`POST /packages/versions`（需要鉴权）按名称列出某个包的可用版本。

#### 包问题

有两个端点用于获取包的问题：

- `POST /packages/questions`（需要管理员）—— 按包名获取问题（最新版本）。
- `POST /packages/questions/identity`（需要管理员）—— 按 repo、name 与 version 获取问题。

### 时区处理

UI 维护一份常见 IANA 时区名称的静态副本，并提供 `getTimezoneOffsetMinutes()` 工具函数，使用浏览器的 `Intl` API 在客户端计算 UTC 偏移。服务器通过状态 ping 响应暴露本机系统的 UTC 偏移分钟数。

### 安装预览

- `POST /packages/install-preview`（需要鉴权）—— 预览安装某个包会创建些什么。接受 repo、name 与 version。返回 repo、name、version、description、image、卷、端口、升级信息、运行时类型，以及该包是否含有问题。对于 VM 包，预览还包含 VM 配置（镜像 URL、人类可读的内存量与 CPU 数）。

### 子包

- `POST /packages/children`（需要鉴权）—— 列出给定 repo 与包名下的子包名称。

### 已卸载卷列表

- `POST /packages/uninstalled-volumes`（需要鉴权）—— 检查某个包是否有上次卸载遗留的卷。返回是否存在已卸载的卷、已卸载版本列表与已安装版本列表。

### 已安装包管理

- `GET /packages/installed`（需要鉴权）—— 列出所有已安装的包，支持搜索、排序与分页。
- `POST /packages/responses`（需要管理员）—— 按 repo、name 与 version 获取某个已安装包保存的应答。
- `POST /packages/purge-volumes`（需要管理员）—— 永久删除某个已安装包的卷。

### 包的启用/禁用

- `POST /packages/disable`（需要管理员）—— 禁用一个包。设置 disabled 标志并停止所有相关的 systemd 服务。
- `POST /packages/enable`（需要管理员）—— 重新启用一个被禁用的包。清除 disabled 标志并启动所有相关的 systemd 服务。

除核心的 `Install`、`Uninstall`、`ListInstalled` 与 `GetResponses` 方法外，`Installer` 接口还支持 `SetDisabled`、`IsDisabled` 与 `IsPackageChanged`。

### 已卸载卷管理

- `POST /packages/purge-uninstalled-volumes`（需要管理员）—— 永久删除某个包所有已卸载的卷。

## 存储

存储使用带配额约束的 btrfs 子卷。

### 关注点分离：卷 vs. 对象存储

**存储层管理卷。gfeh 提供对象存储。存储层完全不处理对象存储——gfeh 才是负责方。**

`src/storage` 创建、调整大小、重命名、快照和删除 btrfs 子卷，并报告磁盘使用情况。这就是它的全部职责。它绝不能知道什么是对象、桶（bucket）、键、文件句柄、内容标识符（CID）、ACL、共享或协议视图。对存储层而言，子卷就是一片带配额的、不透明的字节场地。

gfeh（`gitea.com/town-os/gfeh`，一个以 `town-os-system--gfeh` 形式发布的 Rust 系统服务）拥有这条线以上的一切：对象命名空间、每个文件的元数据与权限、分层的用户/ACL 数据库、共享、按文件的 HTTP 暴露、向外部服务的联邦，以及每一种协议视图（S3、IPFS、Google Drive、纯 HTTP；SMB/CIFS 在 gfehd 中存在，但 [Town OS 不提供该服务](#不提供-smb-视图)）。它使用存储层，纯粹是为了给自己分区所在的子卷做置备与扩容，之后便在绑定挂载的子树上自行进行直接 I/O。

改动任何一侧时都必须遵守的后果：

- **不要**向 `src/storage` 或 `/storage/*` API 添加对象、blob、键值或按文件的端点。若某个功能需要寻址单个文件，它属于 gfeh。既有的 `upload-archive`/`download-archive` 端点是用于卷播种的 tar 传输通道，不是对象 API，也不得朝那个方向生长。
- **不要**让 `storage.Storage` 或 `storage.Controller` 知道用户、权限或协议。配额是存储层强制的唯一策略。
- gfeh 分区位于保留的 `gfeh/` 子卷前缀之下。它们是**在进程内**通过 `storage.Storage` 的 `CreateFilesystem`/`ModifyFilesystem` 置备的，而不是通过 `/storage/*` HTTP API：`createFilesystem` 会无条件地把每一个提交的名称改写为 `user/<name>`（`controller_storage.go`），因此那条路由不可能产出任何其他前缀下的卷。分区置备因此需要自己的 `/gfeh/partitions/*` 处理器，这也把保留前缀的强制、配额策略与审计日志集中在一处，而不是在 gfeh 中重复一遍。

- **gfeh 依赖一份成文的契约，而这里的改动可能破坏它。** gfeh 仓库中的 `TOWNOS_CONTRACT.md` 列出了 gfeh 依赖 Town OS 的每一条路由、行为与不变量——`user/` 改写、保留前缀规则、`/gfeh/partitions/*` 的状态码、无法区分的鉴权失败，以及空 `Account.Networks` 的失败即拒含义——并锁定了它据以验证的 Town OS 版本。gfeh 模拟该契约，使其测试无需 root、systemd、podman 或 btrfs 即可运行。

  **改动 `src/storage`、`src/account` 或 system controller 的路由时，请在 gfeh 检出目录中重新运行 `make check-townos-sync`。** 漂移的模拟器会让 gfeh 拿到一份全绿的测试套件和一个坏掉的部署。模拟器与契约文档要一起对齐；绝不能只改其一。

这套集成在 Town OS 一侧的内容——分区路由、按网络划分的守护进程、管理 socket，以及名称如何进入 DNS 与 ingress——见 [Object Storage (gfeh)](#对象存储gfeh)。

### 文件系统操作

`Storage` 接口提供：

- **CreateFilesystem** —— 创建带可选配额的新 btrfs 子卷。
- **ModifyFilesystem** —— 修改卷的名称和/或配额。
- **RemoveFilesystem** —— 删除卷。
- **ListFilesystems** —— 列出卷，支持按前缀与状态（`user`、`installed`、`uninstalled`）过滤、排序、分页与搜索。当找不到 btrfs 挂载点时返回空列表（而非错误）。
- **RenameFilesystem** —— 重命名卷。
- **SnapshotFilesystem** —— 创建 btrfs 快照。
- **DiskUsage** —— 报告磁盘使用统计。

配额在 btrfs qgroup 层面强制执行。配额为 0 表示不限。

### 存储 API

- `POST /storage/create`（需要鉴权）—— 以名称与可选配额创建新的用户文件系统。
- `POST /storage`（需要鉴权）—— 列出文件系统，支持按前缀与状态过滤、排序、分页与搜索。
- `POST /storage/modify`（需要鉴权）—— 修改卷的名称和/或配额。只有用户文件系统允许重命名；包卷不能重命名。
- `POST /storage/remove`（需要鉴权）—— 删除用户文件系统。
- `POST /storage/package-volumes`（需要鉴权）—— 按包分组列出包卷，可选择是否包含已卸载的卷。
- `POST /storage/remove-package-volume`（需要管理员）—— 按内部名称删除某个具体的包卷。
- `POST /storage/remove-package-volume-group`（需要管理员）—— 存储树中非叶节点删除按钮背后的级联删除。`repo` 与 `name` 必填；`version` 为空则针对该包的所有已安装版本。**在移除任何子卷之前，目标包依赖树中的每一个 systemd 单元都会被停止**，因此仍然打开着某个卷的 podman 容器不可能与 btrfs 删除发生竞争。`include_uninstalled` 会额外清扫匹配的 `uninstalled/` 子树（与驱动卷列表的那个 "Show uninstalled" 开关相连）。
- `POST /storage/upload-archive`（需要管理员）—— 上传归档并解包进某个卷。
- `POST /storage/download-archive`（需要管理员）—— 把卷作为压缩归档下载。

### 卷命名空间

- **用户卷** —— 磁盘上为 `user/<name>`。`user/` 前缀由 create、remove、modify 与 list 处理器透明地添加，并在 API 响应中剥离，因此 API 使用方只看到裸名称。`user` 根子卷在启动时由 reconcile 创建。
- **已安装包卷** —— `installed/<repo>/<name>/<version>/<volname>`。
- **已卸载包卷** —— `uninstalled/<repo>/<name>/<version>/<volname>`。
- **归档存储** —— `archives/` 前缀（系统管理）。
- **VM 镜像** —— `vm-images/` 子卷（系统管理）。存放缓存的 raw 格式 VM 磁盘镜像。
- **对象存储分区** —— `gfeh/<network>`，每个 Town OS 网络一个，属主为 uid/gid 2000。属保留区：`/storage/create` 无法产出它（该路由把每个名称都改写为 `user/<name>`），因此它们通过 [`/gfeh/partitions/*`](#协议一分区置备gfehpartitions) 置备。

所有前缀根名称（`installed`、`uninstalled`、`archives`、`pages`、`vm-images`、`user`、`gfeh`）都是保留的，用户不能直接创建、修改或删除它们。归档的上传与下载在遇到不带内部前缀的子卷名称时，会通过添加 `user/` 前缀来解析。

**除非前缀之后的名称无法爬回上层，否则前缀不构成边界。** `filepath.Join` 会折叠 `..`，因此提交给一个会添加 `user/` 前缀的处理器的 `../gfeh/home`，会变成 `user/../gfeh/home`，从而寻址到另一个网络的对象存储分区——而且它也能溜过保留名称检查，因为该检查匹配的是一个此时穿越尚未产生的前导前缀。因此 `storage.ValidateFilesystemName`（不允许前导斜杠、不允许空字节、不允许空的或 `.`/`..` 的路径分量，并限制字符集）在 `ModifyFilesystem` 中被施加于**两个**名称——只校验重命名目标，会让调用方把别人的子卷挪进自己的命名空间——同时也施加于 `RemoveFilesystem`，后者过去完全不做校验，而它偏偏是破坏性的那一个。`/storage/*` 的处理器在添加 `user/` 前缀**之前**校验提交的名称，这正是保留名称检查名副其实的原因。这些路由是 `requireAuth` 而非 `requireAdmin`，因此这个问题此前对机器上任何普通账户都是可达的。

**list** 的前缀被刻意豁免：`nest/` 是调用方索取 `nest` 之下全部内容的方式，没有任何东西会把它拼接进文件系统路径（存储层从自身基准目录列举，并把它当作字符串过滤器使用），而 `user/` 是无条件添加的，因此带穿越的前缀只会匹配不到任何东西，而不是够到任何东西。

### 归档格式检测

归档的压缩格式通过检查上传流开头的魔数字节来检测。前 6 个字节经由带缓冲的 reader 预读，并与已知签名比对：

- **gzip** —— `0x1f 0x8b`
- **bzip2** —— `0x42 0x5a 0x68`（`BZh`）
- **xz** —— `0xfd 0x37 0x7a 0x58 0x5a 0x00`（`\xfd7zXZ\x00`）

无法识别的签名会被立即拒绝。文件扩展名也会被独立校验以确认格式。

### 归档流校验

格式检测之后，解压后的流通过 `io.TeeReader` 被校验为 tar 归档。tee 的一路喂给 Go 的 `archive/tar` reader 以校验 tar 头；另一路喂给真正的 `tar -xf` 解包进程。若校验发现 tar 流非法，解包会被中断。解压在可用时使用并行实现：gzip 用 `pigz`，bzip2 用 `lbzip2`，xz 用 `xz`。

### 归档上传

`POST /storage/upload-archive`（需要管理员）接受一个 multipart 表单：

- `subvolume`（必填）—— 目标子卷路径。
- `archive`（必填）—— 归档文件。支持格式：`.tar`、`.tar.gz`/`.tgz`、`.tar.bz2`/`.tbz2`、`.tar.xz`/`.txz`。
- `subpath`（可选）—— 卷内用于解包的相对路径；按需创建。
- `stop_service`（可选）—— 解包前停止、完成后重启的 systemd 单元名。

归档直接以流式处理，不落临时文件。解包后会校验路径穿越（解析符号链接）。最大上传大小默认 1 GB（`max_archive_size` 设置）。解包超时默认 600 秒（`archive_unpack_timeout` 设置）。

### 归档下载

`POST /storage/download-archive`（需要管理员）接受一个 JSON 请求体：

- `subvolume`（必填）—— 源子卷路径。
- `paths`（可选）—— 子卷内要包含的具体路径数组。
- `stop_service`（可选）—— 归档期间停止、完成后重启的 systemd 单元名。
- `format`（可选）—— 压缩格式：`tar.gz`（默认）、`tar.bz2` 或 `tar.xz`。
- `filename`（可选）—— 下载文件的自定义基础名。服务器会清洗该值（去除路径分隔符与控制字符），移除任何已有的归档扩展名以防重复，并为所选格式追加相应扩展名。未提供或清洗后为空字符串时默认为 `download`。

返回所请求格式的流式归档。压缩分别使用 `pigz`、`lbzip2` 或 `xz`。Content-Type 与 Content-Disposition 的 filename 头会与所选格式和自定义文件名保持一致。提供 `paths` 时只包含匹配的路径。

### 从容器镜像自动归档

包定义中可以包含引用容器镜像的 `archives` 段。在安装与 reconcile 期间，空卷会通过拉取镜像、创建临时容器并把指定目录复制进卷的方式来填充。

### Git 卷播种

卷可以指定带仓库 URL 的 `git` 字段。在安装与 reconcile 期间，空卷会通过克隆该仓库来播种（超时 5 分钟）。该 URL 可以引用模板变量，使用户能通过问题应答覆盖仓库地址。已有数据绝不会被覆盖。克隆失败会被记录并跳过（非致命）。

### Git 源重建

`POST /packages/rebuild-git`（需要管理员）更新某个已安装包的 git 播种卷。它通过 go-git 为每个 git 卷拉取最新改动，然后重启依赖它的服务。需要包的 repo、name 与 version。重建前会针对已保存的应答重新求值模板变量。

### VM 镜像管理

VM 包需要 raw 格式的磁盘镜像。远程镜像会被下载并用 `qemu-img convert -O raw` 转换；转换后的 `.raw` 文件缓存在 `vm-images` 子卷中。后续安装复用该缓存镜像。本地镜像引用直接从 `vm-images` 子卷解析。

- `GET /vm-images`（需要鉴权）—— 列出已缓存的 VM 磁盘镜像。为每个镜像返回名称与文件大小。
- `POST /vm-images/upload`（需要管理员）—— 从 URL 下载 VM 镜像并转换为 raw 格式。接受一个 URL 与可选名称。名称默认取 URL 的文件名并加 `.raw` 扩展名。下载超时为 30 分钟。转换后的镜像存入 `vm-images` 子卷。
- `POST /vm-images/delete`（需要管理员）—— 按名称移除已缓存的 VM 镜像。

### 展示名称剥离

已安装与已卸载包卷的 API 响应会剥离路径中的前导仓库段（例如 `default/nginx/2.0/data` 变为 `nginx/2.0/data`）。完整的磁盘路径保留在 `internal_name` 字段中，供需要它的操作使用（例如在归档操作中推导用于停止/启动的 systemd 服务名）。

### 存储 UI

存储管理界面分为两部分：

**用户文件系统** —— 一个可分页、可排序、可搜索的数据表。每行都有 Modify（名称与配额）与 Delete 按钮。创建对话框会用系统 `default_quota` 设置预填配额字段。

**包卷** —— 一棵按包组织的层级树。每个包是一个可折叠的树标题，显示：卷总数、版本数、聚合配额与安装状态徽标。当一个包有多个版本时，会显示版本子标题，并带上按版本的配额与状态。当 "Show uninstalled volumes" 开关激活时，已卸载的卷也会包含进来。

每一行叶子卷都显示配额与状态，并提供三个操作：

- **Download**（图标按钮）—— 打开一个对话框，含可选的文件名字段（下载文件的基础名；归档扩展名会自动追加）、压缩格式选择器（gzip、bzip2、xz）、可选的逗号分隔路径过滤，以及一个在下载期间停止依赖服务的复选框。使用 File System Access API 做流式保存，并回退到 blob 下载。
- **Upload**（图标按钮）—— 打开一个对话框以选择归档文件（`.tar`、`.tar.gz`、`.tgz`、`.tar.bz2`、`.tbz2`、`.tar.xz`、`.txz`），含可选的解压子路径，以及一个在上传期间停止依赖服务的复选框。
- **Modify**（按钮）—— 打开一个对话框，显示卷名、状态与关联的服务名，并提供一个修改配额的字段。对包卷而言名称字段不可编辑。

## Pages

Pages 是静态站点托管功能，支持三种内容来源类型：归档上传、容器镜像与 git 仓库。用户指定一个域名或子域名，系统通过一个 Caddy 容器提供内容服务。更新通过重建或重新上传手动触发。

### 数据模型

每个 page 站点包含：唯一名称（主键）、来源类型（`archive`、`container_image` 或 `git`；默认 `archive`）、仓库 URL（git 必填）、分支（默认 `main`）、容器镜像引用（container_image 必填）、镜像目录（container_image 必填）、域名（默认取 page 名称）、状态（`pending`、`active` 或 `error`）、一个**网络**，以及创建/更新时间戳。Pages 存储在一张 SQLite 表中。

`Network` 是该 page 的发布网络，与包的安装网络完全一致：它决定 page 的主机名、叶子证书 SAN、DANE TLSA 属主与 ingress vhost 都在哪个 TLD 之下命名，也决定谁能解析这个 page。为空——即零值与数据库默认值——表示默认/home 网络，与包的 `Installer.LoadNetwork` 是同一约定。参见 [Pages are network-scoped too](#pages-同样是按网络限定作用域的)。它在创建时被接受，也是部分更新字段之一。

Pages 内容存储在 `pages/` 前缀下的 btrfs 子卷中。每个 page 获得一个位于 `pages/{name}` 的子卷，以及一个位于 `pages-webroot/{name}`、指向 `/data/pages/{name}` 的符号链接。`pages` 前缀是保留的，不能通过通用存储 API 重命名或删除。

### Pages API

所有变更端点都需要管理员鉴权；列表端点需要普通鉴权。

- `POST /pages/create`（需要管理员）—— 创建新 page。接受名称、来源类型、仓库 URL、分支、域名、容器镜像与镜像目录。来源类型默认为 `archive`。校验随来源类型而变：git 需要仓库 URL；container image 需要镜像与镜像目录两者。会创建 btrfs 子卷与 webroot 符号链接。git 与 container image 类型的 page 以异步方式置备（克隆或镜像提取）；状态在成功时从 `pending` 转为 `active`，失败时转为 `error`。archive 类型的 page 会保持 `pending` 状态，直到通过 `/pages/upload` 上传内容。未提供域名时默认取 page 名称。
- `POST /pages/upload`（需要管理员）—— 为 archive 类型的 page 上传内容。接受含 `name` 与 `archive` 文件的 multipart 表单。仅对来源类型为 `archive` 的 page 有效；其他来源类型返回 400。使用与存储归档上传相同的魔数格式检测、扩展名校验与流校验。直接解包进该 page 的 btrfs 子卷。成功时状态置为 `active`，失败时置为 `error`。
- `POST /pages/update`（需要管理员）—— 对 page 的仓库 URL、分支、域名、来源类型、容器镜像或镜像目录做部分更新。只有提供了的字段才会被修改。
- `POST /pages/remove`（需要管理员）—— 从数据库中删除 page，移除 webroot 符号链接，并删除 btrfs 子卷。
- `POST /pages/rebuild`（需要管理员）—— 行为随来源类型而变：git 类型拉取最新改动（若缺少 `.git` 则重新克隆）；container image 类型通过 podman 从镜像重新提取；archive 类型返回 400（请改用 `/pages/upload` 重新上传）。
- `GET /pages`（需要鉴权）—— 列出所有 page，支持排序、搜索与分页。可按名称、仓库 URL、分支、域名、来源类型、状态与时间戳排序。

### Pages UI

Pages 管理界面展示一个可分页、可排序、可搜索的数据表，列包括名称、域名、来源类型、仓库 URL、分支与状态。来源类型以徽标展示。状态以带颜色的徽标展示（active 为默认色，error 为红色，pending 为次要色并带旋转的加载图标与 "Provisioning..." 文字）。

创建对话框顶部有一个来源类型下拉框（Archive Upload / Container Image / Git Repository，默认 Archive Upload）。字段随所选来源类型动态变化：git 显示仓库 URL 与分支；container image 显示镜像引用与镜像目录；archive 显示一个可选的文件上传输入。对于 git 与 container image 类型的 page，提交表单会触发置备：所有输入被禁用，提交按钮显示带 "Provisioning..." 文字的加载动画，且对话框不能被关闭。UI 每 2 秒轮询一次 page 状态，最多轮询 60 秒。对于选择了文件的 archive 类型 page，上传在创建之后同步进行。

每行的操作随来源类型而变：archive 类型显示 Upload 按钮；git 与 container image 类型显示 Rebuild 按钮（带确认）。所有 page 都有 Edit 与 Delete 操作。编辑对话框显示与该 page 来源类型相符的字段。

## 对象存储（gfeh）

gfeh 是 [关注点分离](#关注点分离卷-vs-对象存储) 中所述分工的对象存储那一半：`src/storage` 拥有 btrfs 子卷与配额，gfeh 拥有对象、按文件的权限、用户/ACL 森林、共享，以及每一种协议视图。本节讲的是这条边界在 Town OS 一侧的内容——守护进程如何部署，以及跨越这条边界的每一种协议。

`gfehd` 是一个发布到 crates.io 的 Rust 二进制，在此打包为 `quay.io/town/gfeh`（`Containerfile.gfeh`），因为 gfeh 自己的仓库并不提供镜像。它是**每个分区一个进程**，而不是单一的多租户守护进程。

### 部署形态：每个网络一个分区

一个**分区**由一个 btrfs 子卷、一个 `gfehd` 进程、一个管理 socket，以及**它自己的一套用户**构成。每个 Town OS 网络恰好有一个分区，因此对象存储的命名空间是被划分 DNS 与 WireGuard 的同一条边界所划分的：`office` 分区中的一个主体（principal）、一项授权和一个暴露，在 `home` 中毫无意义。

| 内容 | 位置 |
|---|---|
| 分区数据 | `<btrfsBase>/gfeh/<network>` → 容器内 `/data/<network>` |
| 配置 | `<btrfsBase>/gfeh-control/<network>/gfehd.yaml` → `/etc/gfeh/gfehd.yaml` |
| 管理 socket | `<btrfsBase>/gfeh-control/<network>/run/admin.sock` → `/run/gfeh/admin.sock` |
| 单元 | `town-os-system--gfeh-<network>.service` |

路径辅助函数位于 `src/gfeh/layout.go`——`PartitionVolume`、`ConfigPath`、`SocketPath`、`ServiceKey`、`NetworkFromKey`——它们是唯一拼装这些字符串的地方。

socket 位于 btrfs 上，因为那是 gfehd 容器与 systemcontroller 容器都能看到的唯一文件系统；`ingressctl` 为其 gRPC socket 用的是同一招。gfehd 以 **uid/gid 2000** 运行（`gfeh.UID`/`gfeh.GID`），而绑定挂载会直接透传宿主机属主信息，因此分区子卷在创建时就被 chown 给该 uid——这也是 `storage.Filesystem` 带有可选 `UID`/`GID`、`storage.Controller` 带有 `Chown` 的原因。不递归，理由与 `HostVolumeMount` 的 chown 相同：守护进程以自己的 uid 创建自己的子项，因此它们本来就属主正确，永远不会漂移。

**端口。** 四个 HTTP 视图在**每个分区上都绑定固定且相同的容器端口**——s3 9000、http 9001、drive 9002、ipfs 9003——并且**不发布任何宿主机端口**。这样做之所以安全，恰恰是因为它们不发布宿主机端口：每个容器有自己的网络命名空间，ingress 通过容器名访问它，就像访问一个包一样。两个分区都在 9000 上提供 S3 服务也不会冲突，在并发的 `make test-full` 下同样如此。

**没有任何分区发布宿主机端口**，因为 SMB——唯一需要宿主机端口的视图，既不是 HTTP，也无法置于 ingress 之后——是[不提供服务的](#不提供-smb-视图)。`DefaultSMBPortBase`（`4450`）与 `GFEH_SMB_PORT_BASE` 保留但未被使用，这样即使该视图将来回归，测试框架的设置也不会造成危害。

### 协议一：分区置备（`/gfeh/partitions/*`）

这四条路由之所以存在，是因为 `createFilesystem` 会无条件把每个提交的名称改写为 `user/<name>`，因此 `/storage/create` **不可能**产出 `gfeh/` 前缀下的卷。它们在 `TOWNOS_CONTRACT.md` 中被声明，而 gfeh 的 Rust 客户端解析的正是这些确切形状，因此**这里的改动是契约变更，不是重构**。在 gfeh 检出目录中执行 `make check-townos-sync` 是捕捉漂移的手段；`controller_gfeh_partitions_test.go` 则在本侧固定这些线上形状。

| 路由 | 鉴权 | 请求 | 响应 |
|---|---|---|---|
| `POST /gfeh/partitions/create` | 管理员 | `{name, quota}`，name **不带**前缀 | `Filesystem` `{name:"gfeh/<n>", quota}` |
| `POST /gfeh/partitions/modify` | 管理员 | `{name, quota}` | `Filesystem` |
| `POST /gfeh/partitions/remove` | 管理员 | `{name}` | 200，空体 |
| `POST /gfeh/partitions` | 鉴权 | 无请求体 | `Filesystem` 的**纯 JSON 数组** |

有两个细节是承重的：

- **列表返回的是裸数组，而非 `PageResult`。** Town OS 其他所有列表端点都分页；这一个不能，因为 gfeh 的 `list_partitions()` 直接反序列化为 `Vec<Filesystem>`，而带分页信封的响应在 Rust 侧无法解码。
- **前缀是不对称的。** 请求携带裸名称，响应携带 `gfeh/<name>`。该前缀是 Town OS 的命名空间产物，并非分区身份的一部分；gfeh 的 `Partition::from_volume` 在接收时会把它剥掉。

gfeh 客户端会据以分支的状态码：**409** 已存在（它的置备逻辑是"创建或扩容"，并靠这个状态码区分二者——否则，一个除首次启动外每次启动时分区都已存在的守护进程，就只能成功启动那一次），**404** 不存在，**400** 名称非法，**403** 非管理员。含路径分隔符的名称在这条边界上被拒绝，因为 gfehd 在它自己那侧也会拒绝；对"什么是合法分区名"意见不一，会让 `../user/something` 寻址到对象存储根目录之外的卷。

处理器在进程内调用 `storage.Storage`，绝不走 `/storage/*`，因此保留前缀的强制、配额策略与审计日志都留在同一处。这些路由**不在** `grantRoutes` 中——置备一棵权限树的根不是某项授权所能买到的东西，因此持有授权的账户会在任何处理器运行之前就被全局白名单拒绝。

### 协议二：管理 socket（`/v1/*`）

每个守护进程的管理界面都是**仅在 Unix socket 上**的 JSON-over-HTTP——绝不使用端口。没有令牌，也没有认证：socket 上的文件系统权限就是访问控制，因此能够访问它就已经意味着在这台机器上拥有 root。`src/gfeh/client.go`（`UnixClient`）是 Go 侧实现；它把 `DialContext` 固定到该 socket，并使用一个假的 `http://gfeh` 权威名。

| 调用 | 方法 + 路径 | 用途 |
|---|---|---|
| `Health` | `GET /v1/health` | 存活性；同时也是就绪探针 |
| `Names` | `GET /v1/names` | 该分区希望发布的名称 |
| `ListPrincipals` / `CreatePrincipal` / `DeletePrincipal` | `GET`/`POST` `/v1/principals`，`DELETE /v1/principals/<name>` | 该分区的用户森林 |
| `ListGrants` / `CreateGrant` / `RevokeGrant` | `GET /v1/grants?principal=`，`POST /v1/grants`，`DELETE /v1/grants/<id>` | ACL |
| `ListExposures` / `WithdrawExposure` | `GET /v1/exposures`，`DELETE /v1/exposures/<token>` | 已发布的 `/f/<token>` 链接 |

gfehd 把内部错误映射到 HTTP 状态码（404/409/400），而 `StatusError.Unwrap` 再把它们映射回 Go 的哨兵错误，因此 `errors.Is` 可用。

添加用户是 `POST /v1/principals {name, parent, ceiling}`——**没有密码**，这正是 UI 从不索要密码的原因。ceiling 遵循 gfeh 的投影规则：Town OS 管理员为 `all`，其他为读/写。授权会被 gfehd 收敛到该主体的 ceiling 之内，因此 UI 展示的是*返回回来*的权限，而不是发送出去的权限：管理员必须能看到一项授权被收窄了。

### 协议三：名称——gfeh 回答，Town OS 组装

**gfeh 从不注册 DNS 记录或 ingress 路由。** `RebuildDNS` 调用 `TeardownTLD`，`RebuildIngress` 用完整的推导集合调用 `SetRoutes`——两者都会摧毁外来状态——因此 gfeh 直接注册的任何东西，都只能存活到下一次 reconcile。取而代之的是，`GET /v1/names` 返回带视图与端口的**标签**（`s3.<partition>`），由 Town OS 组装出区域。因此这些名称是在每次重建时被*询问*的，而不是被推送一次。

`gfehFQDN(label, tld)`（`gfeh_tls.go`）把标签在网络的 TLD 之下限定，并且是 A 记录、叶子证书 SAN、DANE TLSA 属主与 ingress vhost 都必须一致同意的那一个字符串——与 `packageFQDN` 和 `pageFQDN` 所维护的是同一条不变量。它**总是**限定：它不查询 `isPublicFQDN`，因为每个 gfeh 标签本身就含有一个点（`s3.gfeh`），而那个判定会把任何这样的名称读作公共 FQDN，结果会让每个名称都不被限定，并为一个无人拥有的域名申请 ACME 证书。

**它同时也是标签从一根线上的字符串变成 vhost、DNS 记录和文件系统路径的那个咽喉点**，因此 `gfeh.ValidateLabel` 只在这里施加，别无他处。ingress 的 vhost 被写作 `https://<hostname> {` 且不加引号，因此一个携带换行与花括号的标签会闭合这个块并另开一个——而 Caddy 不会只拒绝那一个坏 vhost，它会拒绝整份配置，并把机器上的每一个名称一起拖下水。校验不通过的标签产出空字符串，而每个调用方本来就会丢弃空的 FQDN，因此畸形的名称贡献的是"没有记录、没有路由、没有证书、没有目录"，而不是一个坏掉的。长度（`gfeh.NameMaxLen`）是在**组装后**的名称上检查的，而非只检查标签：在限长之内的标签，在很长的 TLD 之下限定后仍可能超限，而 DNS 承载不了的名称，证书与 vhost 同样不该声称拥有。

发布方式与包和 pages 完全一致：

- **双栖 DNS** —— 非默认网络的分区会获得一条位于本机 overlay IP 的作用域 A 记录（服务于该网络的 WireGuard peer），*以及*一条位于局域网 IP 的全局 A 记录，二者分别由 `RebuildDNS` 与 `RebuildNetworkDNS` 中的合并逻辑写入。DANE TLSA 在两侧都会被固定。
- **TLS** —— 每个名称一张本地 CA 签发的叶子证书，并把本机在该网络上的 overlay IP 作为 SAN 带上，使 peer 能够直接用 WireGuard 裸地址拨号。
- **Ingress** —— 每个 HTTP 视图一个 vhost，后端为共享的 `town-os-ingress` podman 网络上的 `<container>:<port>`。`dedupeIngressRoutes` 以"先到先得"的方式守护路由集合，因为 Caddy 会因为一个重复的 vhost 而拒绝整份配置。

`IsHTTPView` 把守最后这一步，并且**未知**视图被当作非 HTTP 处理：为不讲 HTTP 的东西建 vhost，会先接受 TLS 握手然后失败，这比完全没有路由更糟。（非 HTTP 的视图会贡献一条 DNS 记录而没有 ingress 路由；当前提供服务的四个视图全是 HTTP。）

### 分区索引页

gfeh 提供的每一个视图都应答某种**协议**，没有一个应答浏览器：HTTP 视图恰好只有一条路由 `/f/{token}`，因此它的根是 404；S3 对任何它无法解析为操作的请求返回 XML 错误；Drive 与 IPFS 应答各自的 API。于是，任何人拿到一个新名称后会做的那一件事——打开它——报告的是对象存储坏了，而事实上那里从来就没有可看的东西。

每个分区在 **`gfeh.<tld>`** 发布一个静态索引页——即 `gfeh.IndexLabel`，它取自 `VolumePrefix` 而不是把字符串 `"gfeh"` 再写一遍，因为索引必须落在它所索引的那些视图标签的父节点上。不需要学习任何新名称：视图本来就是 `s3.gfeh`、`http.gfeh`、`drive.gfeh`、`ipfs.gfeh`。

- **它由 `collectGfehSites` 作为一个普通的 `GfehSite` 贡献出来**，这正是要点所在：它从为视图推导全部六项内容的同一段代码那里继承 A 与 AAAA 记录、作用域 overlay 记录、DANE pin、叶子证书 SAN 与 ingress 路由，因此 vhost 与证书不可能由不同的字符串拼出。只有当该分区至少有一个由 ingress 承载的视图时才会添加它——一个什么都不可浏览的分区的索引页，只会是一个名称、一张证书和一条路由，只为渲染一句"这里没什么可看"。
- **它由 pages 容器提供服务，而不是 gfehd。** 静态 HTML 不需要自己的服务器，而把它作为 Caddy 的 `respond` 响应体内联发出，会把生成的标记放进配置文件里，那里一个转义错误就会让 Caddy 拒绝一切。
- **内容位于它自己的 `gfeh-index/` 根之下**，与 `gfeh/` 平级，理由和 `gfeh-control/` 相同：`pages/` 之下的一切都是一个 page，由一行记录拥有，并被 pages 的 reconcile 清扫。webroot 是两者唯一共享的东西，因为那是容器实际提供服务的目录。`ViewIndex` 刻意**不在** `HTTPViews` 中，因此 `IsHTTPView` 不接受它——那个判定回答的是"这是否是 gfehd 上报的、ingress 可以承载的视图"，而索引页既非 gfehd 上报，也非它提供服务。
- **`pruneStalePageSymlinks` 合并了 `gfehIndexHostnames`。** 索引页不是 page，因此若无此举，第一次 `reconcilePages` 就会删除每一个索引链接——而一台有对象存储却没有 pages 的机器，每一轮都会撞上这种最激进的情况。有效集合仅从**网络集合**推导，绝不去询问守护进程，这样仅仅是启动较慢的分区不会被剪掉自己的索引：可以删除什么，必须能由 Town OS 自己拥有的状态判定。
- **索引页由 `reconcileGfehIndexes` 渲染，调用点在 `RebuildIngress`**，而不是 `ReconcileGfeh`。这个位置是承重的：ingress 重建会在启动时、每小时的 reconcile 中、包与 page 的增删改时运行，尤其是在 `publishGfehNames` 中——那是冷启动时第一次真正有守护进程在应答的时机，因为 gfehd 会轮询 `/status/ping`，而后者在处理器切换之前一直是 503。从 gfeh 的 reconcile 中写出的索引页，会在守护进程还说不出自己提供什么之前就被写出，并一直陈旧到下一个小时。

索引页**只**承载视图，而它们本来就在 DNS 中。不含暴露、主体、授权或配额：它在没有任何认证的情况下提供服务，而每一个已发布的 `/f/<token>` 链接都是一个持有即用的凭据——恰恰是无认证页面绝不能枚举的东西。

### 协议四：UI 代理（`/gfeh/*`）

管理 socket 未经认证且不可通过网络访问，因此由 Town OS 代理它。这些路由刻意**与那四条契约路由分开**，以便 `check-townos-sync` 始终精确匹配契约所声明的内容。

| 路由 | 鉴权 |
|---|---|
| `GET /gfeh` | 鉴权 —— 分区及其网络、TLD、配额、单元状态与 `/v1/names` 输出 |
| `GET /gfeh/principals?network=` | 鉴权 |
| `POST /gfeh/principals/add` / `remove` | `requireObjectStorage`（管理员或 `gfeh` 授权） |
| `GET /gfeh/grants?network=&principal=` | 鉴权 |
| `POST /gfeh/grants/add` / `revoke` | `requireObjectStorage` |
| `GET /gfeh/exposures?network=` | 鉴权 |
| `POST /gfeh/exposures/withdraw` | `requireObjectStorage` |

四个 `GET` 不计入审计；五个变更操作带有审计键。在未配置任何分区时，`GET /gfeh` 会报告"对象存储未配置"，而不是报错。

**其中每一条——包括读操作——都由 `requireNetworkScope` 限制在调用者自己的网络内**，因为"哪个网络"存在于只有处理器才解析过的请求体或查询参数里。一个受限账户列出另一个网络的主体或已发布链接，恰恰就是作用域机制要防止的泄露；而读操作是 `requireAuth`，因此上游没有任何东西会阻止它。`GET /gfeh` 不指定网络（它就是要枚举网络），因此它改为过滤行——依据同一个 `Restricted()` 判定，因为拿一个普通账户去和它空的作用域做过滤，会让每个分区对每个普通账户都不可见，而不是限制住任何人。

**`gfehClientFor` 内部的顺序是承重的：先形状，再权限，最后存在性。** 空网络对所有人都是 400（打字错误不是权限问题）；越界的网络在任何分区查找**之前**就返回 403；只有在这之后，缺失的注册表才配得上 503，未知网络才配得上 404。若把查找放在前面，一个本就无权询问的调用者就能得知那个分区是否存在、其守护进程是否在运行，而且得到的是另一种形式的*成功*拒绝——于是没有任何记录表明一个受限账户曾伸手到自己作用域之外。

### 没有服务账户

早先的版本创建了一个专用的管理员账户 `gfeh`，其密码存放在 `gfeh_service_password` 设置中，以便守护进程能向控制平面认证。**那已经没有了。** Town OS 在守护进程启动之前就自行置备每个分区的子卷与配额，并通过管理 socket 创建主体，因此那份凭据什么也没买到——代价却是一个*无人创建的、处于启用状态的管理员账户*，堂而皇之地出现在每台机器的用户列表里，权限足以卸载一切，并且让每一个"这台机器有管理员吗"的问题都被迫变成"有*人类*管理员吗"。

`hasEnabledAdmin`（`src/svc/systemcontroller/admin_presence.go`）现在就是那个朴素的问题，由 `/status/ping` 中的初始化标志与 `POST /account/create` 的引导分支共享，因此两者永远不会各执一词——一台机器如果一处说"已初始化"而另一处不这么说，那就是一台谁也进不去的机器。

`account.PurgeLegacyServiceAccounts` 在升级后的首次启动时删除该行与存储的密码，并报告它是否真的删除了什么，这样机器只会说一次，而不是每次启动都记一条日志。它刻意使用原始 SQL：`Manager` 没有 `Delete`，而"删除账户"这项能力不该作为一次清理的副作用被引入。

`gfehd.yaml` 中留下的是 `credentials:` 与 `drive.tokens:`——那是**终端用户向 gfeh 的各视图认证**用的，绝不是 Town OS 的登录凭据。`town_os:` 块仍然存在于配置模式中（gfehd 的 YAML 被精确镜像），但 Town OS 不会向其中渲染任何账户。

### 不提供 SMB 视图

SMB **不提供服务**。它是唯一无法置于 ingress 之后的视图，也是唯一需要自己那份凭据的：一个 NT 哈希（`MD4(UTF16LE(password))`），它无法从存储的密码哈希推导出来，因此每个想要共享的用户都得额外背一个密码。Town OS 的账户没有这样的密码，因此 gfehd 无人可认证——而在局域网上开一个无认证的共享，不是可以退而求其次的选项。

后果：没有任何分区声明 `smb:` 块，也不为它分配宿主机端口（保留 `SMBPortBase` 仅仅是为了让测试框架的 `GFEH_SMB_PORT_BASE` 保持接线状态），`Account.SMBNTHash` 与 `src/account/smb_credential.go` 已被移除，`smb_nt_hash` 列由 `migrateLegacyAccountColumns` 丢弃——NT 哈希不加盐、没有工作因子，对任何仍在讲 NTLM 的东西而言等价于密码明文，因此为一个无人提供服务的视图把它静置在磁盘上，是两头最坏的组合。其余四个视图不受影响。

### 配置文件

`src/gfeh/config.go` **精确**镜像 gfehd 的 YAML。gfehd 的每一个配置结构体都是 `#[serde(deny_unknown_fields)]`，因此多出来的键不会被忽略——它是一个硬性的启动失败。顶层字段：`data_dir`、`partition`、`network`（一个**指针**：缺失表示默认分区，而空字符串是另一种、非法的请求）、`admin_socket`、五个可选的视图块、`credentials` 与 `town_os`。Town OS 渲染五个视图中的四个，既不渲染 `smb:` 块，也不渲染 `town_os:` 账户。文件以 `0640` 写入 `<btrfsBase>/gfeh-control/<network>/` 之下，并对 gfeh 的 gid 组可读，因为守护进程以 uid 2000 运行且必须读取它。

### 启动与 reconcile

`ReconcileGfeh` 在启动时于 **ingress 与 pages 之后**、**`Reconcile` 之前**运行——那时 TLS CA 与存储已经就位，而这些名称必须能供后面的 `RebuildDNS`/`RebuildIngress` 调用使用。它在 **`ReconcileNetworks` 之后再运行一次**，该操作是幂等的（未发生变化的分区会被放过，而不是被重启），并覆盖本次 reconcile 新建出的任何网络。它也会被 `/networks/create`、`/networks/remove`、`/networks/enable` 与 `/networks/disable` 调用，因此运行时新增的网络也能获得分区。全过程非致命。

对每个网络，它确保子卷存在（带 UID/GID）、渲染配置，并**仅在渲染内容发生变化时**才安装并重启单元（即 reconcile 已在使用的 `ReadUnit` 差异惯用法）。`pruneGfehPartitions` 移除已不存在网络所对应的单元。

**按分区的等待已经取消，而它的缺席是承重的。** `reconcileGfehPartition` 启动单元后就到此为止；某个守护进程是否在应答，由 `GfehReadyNetworks` 和名称收集器分别去问，而这两者本来就把沉默的分区当作"什么也没贡献"，而不是当作失败。那个等待过去位于循环内部，每个分区一次——包括它其实什么也没做的那些分区，因为除 home 之外的任何网络，`ensureFirstUserPrincipal` 在第一行就返回了。在一个带截止时间的 context 上，这不只是慢：第一个永远不应答的守护进程会在 `WaitForReady` 中耗尽全部剩余预算，于是它之后的每个分区都在一个已过期的 context 上尝试 `Start`，而 `pruneGfehPartitions` 根本没机会运行。一个死掉的守护进程按网络名的排序顺序，把对象存储的其余部分一起拖垮了。

唯一保留下来的等待是 reconcile 最末尾的 `seatGfehFounder`：它只等待 **home** 分区，上限为 `gfehFounderWaitBudget`（10 秒，测试中可按配置覆盖），随后为机器安置第一个账户。因为它在最后，超时只会拖延已经完成的工作；仍在冷启动的守护进程会在下一轮被安置，而启动流程紧接着 `ReconcileNetworks` 就会跑下一轮。出于同样的理由，`GfehReadyNetworks` 通过 `context.WithoutCancel` 为每次健康探测给出各自的预算，而不是去消耗调用方剩下的那点时间——否则一个已耗尽的截止时间会让所有分区同时显得已经死亡。取消仍然被遵守；那属于关机。

**对象存储没有开关设置。** 存文件正是这台机器存在的目的，所以它像 DNS 和 ingress 一样运行——作为 Town OS 之所以是 Town OS 的一部分，而不是一个需要被启用的功能。一个开关只会带来"某人正在排查文件去哪了，却发现它处在关闭位置"的机会；想让守护进程停下的管理员，可以像对待其他任何系统服务一样，在服务面板里停止它们。升级后的机器设置表中若残留 `object_storage_enabled` 行，没有任何东西会读取它。

余下的逃生舱口关乎*构建*，而非策略：它以 ingress 为前提（当 `INGRESS_IMAGE` 为空时，四个 HTTP 视图对任何人都不可达，因此启动分区只会发布无人提供服务的名称），而显式置空的 `GFEH_IMAGE` 会完全跳过对象存储（开发模式）——与 `UI_IMAGE` 和 `INGRESS_IMAGE` 使用同样的 `LookupEnv` 约定，因为 `Getenv` 会让空值意味着"使用默认值"，从而根本没有关闭开关。

**第一个账户被安置在 home 分区中。** `ensureFirstUserPrincipal` 以本机最早创建的账户命名创建一个主体（按 `CreatedAt`，以用户名作为平局裁决，这样创始账户不会因 map 迭代顺序而在两次 reconcile 之间发生变化），并使用 `gfeh.CeilingForAccount(admin)`。森林为空的分区谁也服务不了：操作者打开 Users 标签页，什么也看不到，还得自己琢磨出"我自己的账户不在里面"。**仅限 home**——每台机器都有这个分区，而后来添加的网络属于被授予其上权限的人，把创始账户安置进去等于把别人创建的命名空间交给他。幂等性由 gfehd 保证，它对已存在的主体返回 409。

**名称在处理器切换之后才发布。** `publishGfehNames` 在后台运行：gfehd 轮询 `/status/ping`，而后者**在完整路由器就位之前一直返回 503**（[Boot Status](#启动状态与刷新)），因此分区在启动基本完成之前无法完成自己的启动。在此处同步等待会让它所等待的这次启动自我死锁。若届时没有任何分区就绪，这些名称就交由下一次 reconcile 发布。

分区会在 `collectSystemServices()` 中注册，因此 `POST /system-services/refresh` 会重新拉取并重启它们——正是这一处遗漏曾让 ingress 悄悄停留在旧版本上。

### 版本耦合

**Town OS 固定的是 gfehd 的下限，而这是下限而非偏好。** `Containerfile.gfeh` 依据 `GFEH_VERSION` 从 crates.io 构建（可覆盖，或将 `GFEH_LATEST` 置为非空以采用 crates.io 当前的最新版——与 install 仓库中的 `TTYFORCE_LATEST` 是同一形状）。当前下限是 **0.1.2**。

这两种失败对 `make test` 都不可见——单元测试与集成测试套件都用一个**假的 gfehd** 顶替，因此把版本固定到下限之下换来的是一套全绿的测试和一台对象存储悄然死亡的机器。当 Town OS 开始依赖守护进程的新行为时，请提高这个固定版本，并让镜像构建在该版本尚未发布时大声失败。

### UI

`/dashboard/objects`（导航项 `nav.objects`，"Object Storage"）。顶部是网络选择器，其下是 `?tab=` 子标签页，每个对应 `ui/src/routes/objects/` 下的一个文件：**Overview**（按分区的状态、配额，以及已发布的名称，并标明每个名称是经由 ingress 访问还是直接拨号）、**Users**（主体与 ceiling；添加时会投影一个 Town OS 账户）、**Grants**，以及 **Links**（暴露，可撤回）。读操作是 `requireAuth`，因此该标签页不限管理员；变更控件需要管理员或 `gfeh` 授权，并且无论哪种都只限于调用者自己的网络。

该界面上有两个细节，其存在是为了防止读者据一个用不了的数字或令牌采取行动：

- **对 HTTP 视图，Overview 的 Port 列是空的。** gfehd 为这类视图上报的端口是 ingress 代理到的*容器侧后端端口*，从读者所在的任何位置都不可达——在 "Ingress (HTTPS)" 旁边印出 `9000`，只会招致有人去拨 `s3.gfeh.home:9000`，然后断定这个功能坏了。SMB 保留它的数字，因为那本会是一个真实的宿主机端口。
- **Links 标签页渲染的是完整 URL，且由服务端组装。** `GfehExposureView.URL` 由 `gfehPublishedLinkBase` 构建——`https://<http-view-fqdn>/f/`——它来自为 ingress vhost 与叶子证书 SAN 命名的同一个收集器，因此已发布的链接在构造上就是 ingress 会路由、证书也覆盖的名称。它不在浏览器里组装，是因为 UI 将不得不知道四件服务器早已掌握的事实：提供服务的名称是 *http 视图的*，而不是分区的或本机的；它是在该分区自己网络的 TLD 之下限定的，而非全局 TLD；路由是 `/f/<token>`；以及上报的端口绝不能出现。当分区不提供任何 HTTP 视图时该字段为空——这是诚实的答案，因为那时确实没有任何东西在服务那个令牌——而被禁用的暴露渲染为纯文本，而不是一个可点击的 404。

**这个界面是管理对象存储的唯一场所。** 服务界面上没有对象存储专区：一个分区**就是**一个系统服务——各自一个 `town-os-system--gfeh-<network>` 单元——因此它本来就是该界面 System Services 表中的一行，`Object Storage (<network>)`，带有与其他系统服务相同的状态徽标和相同的启动/停止/重启/日志操作。此前旁边那块面板重复了这一行，并且独立于它轮询，于是同一个单元在两个层级上有两套可能互相矛盾的控件；它还会无条件渲染，而表格却要等自己的轮询返回后才显示，这就使得首次绘制时对象存储孤零零地排在界面顶部，片刻之后系统服务才插进它上方。服务界面上的 `?expand=objects` 会展开 System Services，那一行就在那里。

## 服务

### 服务单元过滤

systemd 单元查询在 dbus 层面就被限定为 `town-os-package--*` 模式，只获取 Town OS 的包单元，而不是系统上的全部单元。系统服务单元（`town-os-system--*`）通过 `IsSystemServiceUnit()` 单独识别。结果集进一步排除网络控制器（`-network.service`）、uPnP 助手（`-upnp.service`）与端口转发（`-fwd-`）。网络控制器单元在内部保留以供故障检测，但不出现在面向用户的列表中。

### 服务描述富化

包描述以批量方式加载，每个仓库调用一次 `LoadPackages`，而不是逐包读取 YAML。描述通过由每个包身份构造出的预期单元名与服务单元匹配。

### 服务单元生成

systemd 服务单元依据包的运行时类型以不同方式生成。

**容器包**生成基于 podman 的单元，启动用 `podman run`，停止用 `podman stop`，其中包含端口映射（`-p`）、环境变量（`-e`）与卷挂载（`-v`）。

**VM 包**生成基于 QEMU 的单元，使用 `qemu-system-x86_64` 并带上：

- `-m {MB}` —— 以兆字节为单位的内存（由编译出的字节值换算）。
- `-smp {cpus}` —— 虚拟 CPU 数量。
- `-nographic` —— 无头运行（无显示输出）。
- `-enable-kvm` —— KVM 硬件加速。
- `-drive file={image},format=raw,if=virtio` —— 以 virtio 块设备形式挂载 raw 磁盘镜像。
- `-netdev user,id=net0`，并为每个端口映射带上 `hostfwd=tcp::{external}-:{internal}` —— QEMU 用户态网络加宿主机到客户机的端口转发。
- `-device virtio-net-pci,netdev=net0` —— 半虚拟化网络设备。

VM 单元还会在启动前与停止后的钩子中通过 `firewall-cmd` 管理防火墙端口，并与 socket 单元协调以避免端口冲突。

### 服务单元 API

- `GET /systemd/units`（localhost 或需要鉴权）—— 平铺列出所有包服务单元。返回的单元状态附带包标识符、包描述与网络控制器故障标志。
- `GET /systemd/units-tree`（localhost 或需要鉴权）—— 同样的数据，但组织成依赖树：根包在顶层，依赖递归嵌套在其父包之下（形状与 `/storage/package-volumes` 一致）。每个节点除了面向人的 `package_identifier` 之外，还带有 `repo`/`name`/`version`（原始有效名，可能含 `--dep--`），以及与平铺端点相同的状态字段，因此客户端无需二次请求即可富化行数据。**搜索与分页只作用于根节点**——依赖后代不计入分页，因此即便在分页边界上，一棵树也总是带着完整子树返回。
- `POST /systemd/status`（需要管理员）—— 改变某个服务单元的状态。接受单元名与动作（start、stop、restart、enable、disable）。
- `POST /systemd/status/tree`（需要管理员）—— 对某个根包的整棵依赖树施加一个动作。接受 `repo`、`name`（原始有效名，因此来自安装 API 的值可以原样回传）、`version` 与 `action`。只允许 `start`、`stop` 与 `restart`——`enable`/`disable` 会被拒绝——并且拒绝停止 system controller 自身的单元。**遍历顺序取决于动作**：单元以叶子优先的顺序收集（这是启动与重启的自然顺序），而对 stop 则把顺序反转，使根节点先于其后代停下。

### 服务管理 UI

服务界面展示一个已安装包 systemd 单元的分页数据表。每行显示包标识符、描述、活动状态、子状态与一个操作下拉菜单。

#### 服务操作

每个服务的操作下拉菜单提供：

- **Start** —— 启动服务（带确认）。
- **Stop** —— 停止服务（带确认；对 system controller 自身禁用）。
- **Restart** —— 重启服务（带确认）。
- **Service Logs** —— 打开该服务单元的日志查看器。
- **Network Logs** —— 打开该服务的网络控制器单元的日志查看器（单元名加 `-network.service` 后缀）。

### 高级日志

服务表下方的 "Advanced Logs" 按钮会打开一个模态框，其中包含：

- **Controller Logs** —— 查看 `town-os-systemcontroller.service` 的日志。
- **System Logs** —— 查看系统级日志（所有单元）。
- **Journal Errors** —— 查看按优先级 3 过滤的系统日志（错误及以上，等价于 `journalctl -p 3`）。
- **自定义服务名** —— 一个文本输入框，可查看任意 systemd 单元的日志。

### 日志查看器

日志查看器对话框提供：

- 动态标题，依上下文显示单元名、"System Logs" 或 "Journal Errors"。
- 状态徽标，显示该单元的活动状态与子状态（在查看具体单元时）。
- 实时搜索，带防抖过滤（300 毫秒）。
- 按日期与小时的时间范围过滤。
- 跟随模式开关，可持续追踪日志并自动滚动（当搜索或时间过滤生效时自动禁用）。
- 初始滚动到底部：查看器打开后，一旦条目加载完毕，日志容器就滚动到末尾。滚动到底部的 effect 以 `journalEntries.length > 0` 为条件，因此它不会在条目到达之前的空首次渲染中被消耗掉；随后一个 `requestAnimationFrame` 会在布局稳定后重新钉住 scrollTop，以防展开的树在提交与绘制之间变高。
- 树状视图开关，按分钟分组条目。树状视图是默认视图，且每个分钟分组**默认展开**。展开状态的 map 只存储显式的折叠：未定义的条目被视为展开，因此首次切换是折叠而非展开。
- 一键复制全部已显示的日志条目。
- 日志输出中的 ANSI 颜色码渲染。
- 结构化字段高亮（`name=value` 键值对）。

### 日志 API

有两类端点提供日志数据：

- `GET /systemd/logs`（localhost 或需要管理员）—— 通过 Server-Sent Events 流式推送历史日志条目。`unit` 查询参数选择服务；为空或为 `__system__` 时返回系统级日志。
- `GET /systemd/logs/tail`（localhost 或需要管理员）—— 返回一页 JSON 格式的日志条目。支持参数：`unit`、`lines`（默认 100）、`before`/`after`（游标分页）、`grep`（不区分大小写的搜索）、`since`/`until`（Unix 时间戳）与 `priority`（syslog 严重级别过滤，0 表示不过滤）。
- `GET /systemd/logs/tree` 与 `GET /systemd/logs/tree/tail`（localhost 或需要管理员）—— 按树限定的对应端点。它们不接受 `unit`，而是接受 `repo`、`name` 与 `version`（全部必填），并覆盖该包依赖树中的**每一个** systemd 单元，因此父包的日志与其依赖的日志会在同一个视图中交织。除此之外，重放与分页语义与 `/systemd/logs` 和 `/systemd/logs/tail` 一致。

## 账户管理

### 账户模型

每个账户包含：用户名（主键）、密码哈希（绝不在 JSON 中暴露）、邮箱、电话、真实姓名、管理员标志、禁用标志、一个**授权集合**、一个网络作用域，以及创建/更新时间戳。账户存储在一张 SQLite 表中。

**不存在账户"种类"这一概念**。一个账户要么是管理员（在每个网络上持有全部授权），要么不是；而非管理员持有的就是那些被打开的授权。`Account.Restricted()`——即持有至少一项授权的非管理员——是推导出来的，从不存储。

**不存在服务账户。** 早先的版本给对象存储守护进程配了自己的管理员账户；它已经没有了，`account.PurgeLegacyServiceAccounts` 会在升级后的首次启动时删除它（及其存储的密码）。参见 [No service accounts](#没有服务账户)。

### 校验规则

- **密码** —— 最少 8 个字符，且只能是可打印 ASCII（`0x21`–`0x7E`，不含空格）。高位字节与控制字节在创建时即被拒绝（`ErrPasswordInvalidChars`），而不是去指望通往 bcrypt 路径上的每一层——HTTP Basic 认证、JSON、URL 编码、数据库的 latin1 列——都能一模一样地往返编码。
- **邮箱** —— 标准邮箱格式（`user@domain.tld`）。
- **电话** —— 数字加可选的格式化字符（`+`、空格、短横线、圆括号）。
- **联系信息** —— 邮箱、电话与真实姓名全部必填（非空）。
- **授权** —— 每个名称都必须在 `account.AllGrants` 中（`ErrInvalidGrant`）；管理员不得显式持有任何授权（`ErrGrantsAdmin`——它本来就全部持有，因此存储的子集只可能与之矛盾）；持有任何授权的账户必须至少限定到一个网络（`ErrGrantsNoNetworks`）。
- **网络作用域** —— 每一项都必须是合法的网络名（`ErrInvalidNetworkName`）。空列表绝不会被读作"任意网络"。

### 授权（Grants）

**授权**是非管理员账户可以持有的具名能力。目前有两个：

| 授权 | 常量 | 可换来 |
|---|---|---|
| `wireguard` | `account.GrantWireGuard` | 在该账户的网络上登记与刷新 WireGuard peer |
| `gfeh` | `account.GrantGfeh` | 管理这些网络所拥有的对象存储——主体、它们的授权、已发布的链接 |

`account.AllGrants` 就是注册表：不在其中的授权无法被存储，这正是阻止 API 请求中的一个拼写错误变成一项永远悄悄匹配不到任何东西的权限的机制。新增一项能力就是在那里加一条，再加上它在 `grantRoutes` 中的路由——不需要新列、不需要新迁移、不需要新的 `UpdateFields` 指针。UI 从镜像文件 `ui/src/lib/grants.js` 渲染复选框，因此新增授权也不需要新的标记代码。

两者是**独立的**。持有 `wireguard` 在对象存储中什么也换不到，持有 `gfeh` 也换不到 peer 登记能力；一个账户可以两者兼有。`Account.HasGrant` 回答"这个调用者到底能不能做这件事"，而 `Account.MayAdministerNetwork` 回答"在哪个网络上"——二者绝不互相替代。

#### 强制分三层，而分层组合正是要点

1. **`grantAllowlist`** 是一个*全局的*、失败即拒的中间件。明天新增的路由，在有人把它列入 `grantRoutes`（`src/svc/systemcontroller/controller_auth.go`，以 `"METHOD PATH"` 为键）之前，对受限账户默认是拒绝的。没有有效令牌的请求、来自管理员的请求，以及来自不持有任何授权的普通账户的请求，都会直接穿过它交给路由自身的鉴权——授权是给那些为行使它而存在的账户*叠加的*权限，因此这一层只约束这类账户。
2. **路由自身的中间件** —— `requirePeerEnroll`（`wireguard` 授权）与 `requireObjectStorage`（`gfeh` 授权），两者都由 `requireGrant` 构建，后者放行管理员，因为管理员持有全部授权。读操作仍是 `requireAuth`。
3. **`requireNetworkScope`**，位于处理器内部，因为网络存在于请求体或查询参数中，只有处理器才解析过它。它做的是**限制**，而不是授予；并且它只限制 `Restricted()` 账户——普通账户不持有任何授权，因此也没有作用域，而空作用域会拒绝一切网络，所以把它施加于普通账户会让那些刻意保持 `requireAuth` 的路由上的每一次读操作都变成 403。

`grantRoutes` 就是授权所能换来的全部：

```
wireguard: GET  /networks/peers   POST /networks/peers/add   POST /networks/peers/refresh
gfeh:      GET  /gfeh             GET  /gfeh/principals      POST /gfeh/principals/add
           POST /gfeh/principals/remove                      GET  /gfeh/grants
           POST /gfeh/grants/add  POST /gfeh/grants/revoke   GET  /gfeh/exposures
           POST /gfeh/exposures/withdraw
```

外加 `grantCommonRoutes`，任何持有授权的账户无论持有哪一项都可访问：`POST /account/authenticate`、`GET /account/me`、`GET /networks`、`GET /dns/services`、`GET /tls/ca.crt` 与 `GET /status/ping`。没有它们，任何授权都无法使用——你不先登录就无法行使任何授权——因此它们是共用的，而不是被复制进每一项授权。

`GET /status/ping` 出现在那个列表上还有第二个理由：它是**公开的**，注册时完全没有鉴权中间件，因此匿名的陌生人也能拿到 200。由于白名单是全局且失败即拒的，遗漏它就意味着一个有效令牌会把那个 200 变成 403——认证反而让调用者严格地比什么都不出示更糟。它同时还是仪表盘 60 秒一次的会话心跳，以及整个状态面板的数据来源，因此持有 `gfeh` 的账户本可以访问每一条 `/gfeh` 路由，却仍然得不到一个可用的页面。同时再授予 `wireguard` 也无济于事：ping 不与任何一项授权挂钩。

请注意刻意**缺席**的内容：`/gfeh/partitions/*` 保持 `requireAdmin`（置备一个分区就是创建一棵权限树的根并分配一个 btrfs 子卷；`TOWNOS_CONTRACT.md` 把它保留给管理员，而 gfeh 的客户端会依据 403 分支），以及 `GET /networks/peers/connected`，它聚合了所有网络上每个账户的 peer 与观测到的源地址。

与创建后不可变的 `Admin` 不同，授权是可变的；而 `account.Manager.CreateGranted` 是独立于 `Create` 的方法，这样那些不变量（持有授权者绝不是管理员，且始终有非空作用域）就在创建时于一处被强制，而不是从一个被加宽的位置参数签名里拼凑出来。

#### 从旧列迁移

早先的版本为每项能力保留一个布尔列。`legacyGrantColumns`（`src/account/sqlite.go`）把每一列映射到它将成为的授权，`migrateLegacyAccountColumns` 负责搬运并删除该列：

| 旧列 | 变为 |
|---|---|
| `wireguard` | `wireguard` |
| `object_storage` | `gfeh` |
| `network_only`（一个把两者合成一个标志的中间态模式） | 两者 |

**一列，一项授权。** 原本能登记 peer 的账户仍然能，原本不能的也不会悄悄获得这项能力——在升级过程中扩大权限是不可撤回的方向，因为账户保留着它的密码，而界面上没有任何东西会说它的权限变大了。`smb_nt_hash` 被直接丢弃（参见 [No SMB view](#不提供-smb-视图)）。

### 每个账户都属于 home 网络

`Manager.Create`——**第一个**账户与每个普通账户所走的路径——写入 `networks: ["home"]`。`CreateGranted` 不会把它并进去：在那条路径上，管理员选定的作用域恰恰就是该账户可以触及的网络，把 `home` 折进去会扩大一个本应限定在 `office` 的门户账户的范围。

这样做是安全的，因为对于不持有授权的账户，作用域是**成员身份，而非限制**：`Restricted()` 为假，因此上面各层都不会去查询它。而且它绝不可能指向一个不存在的网络——参见 [The home network always exists](#home-网络始终存在)。

### 账户 API

- `POST /account/create` —— 创建新账户。在引导模式下（不存在处于启用状态的管理员账户）允许未认证访问；否则需要管理员认证。非空的 `grants` 数组会转由 `CreateGranted` 处理并使用所提供的 `networks`；否则账户通过 `Create` 创建并加入 home 网络。用户名重复的错误会返回通用失败信息，以防用户枚举。
- `POST /account` —— 按用户名获取账户（需要鉴权）。
- `GET /account` —— 列出所有账户，支持分页与搜索（需要鉴权）。
- `POST /account/update` —— 更新账户字段（需要鉴权）。被更新的用户名来自**请求体**，因此编辑他人账户仅限管理员：没有这项检查，任何已认证账户都能 POST `{"username":"admin","fields":{"password":"..."}}` 从而接管这台机器——控制器驱动着宿主机的 podman socket，所以那就是 root。普通账户仍可编辑自己的联系方式与密码，这正是该路由没有直接设为 `requireAdmin` 的原因。管理员身份在账户创建后不可更改；授权与网络作用域可以更改，但**只能由管理员更改，即便是改你自己的账户也一样**——否则普通用户就能给自己授予 `gfeh` 从而闯进某个分区，或授予 `wireguard` 从而在 overlay 上登记一个 peer。`networks` 为 nil 时保持已存储的作用域不变；非 nil 时整体替换。`validateGrantResult` 检查更新*之后*该行的状态，因此给管理员授予授权、把持有授权者提升为管理员，以及把作用域从授权之下清空，这三种情况都会被捕获。
- `POST /account/disable` —— 禁用账户，阻止其认证（需要管理员）。同时撤销该账户的活动会话。让禁用生效的并不是这一步——`SessionManager.Validate` 本身就会拒绝被禁用账户的令牌，因此这项保证并不依赖撤销是否成功——它的作用是：若该账户日后被重新启用，禁用之前签发的令牌不会重新生效，而那并不是管理员在撤销某人访问权之后所说的"启用"的含义。
- `POST /account/enable` —— 重新启用被禁用的账户（需要管理员）。

### 账户管理 UI

用户管理界面（`/dashboard/users`）展示一个可分页、可排序、可搜索的账户数据表。每行显示用户名、邮箱、电话、真实姓名、管理员/用户角色徽标与启用/禁用状态。每行的操作包括一个 Edit 按钮（打开对话框以更新密码、邮箱、电话、真实姓名、**授权复选框**与网络作用域选择器）以及一个带确认的启用/禁用开关。有一个链接可跳转到专门的创建用户页面（`/dashboard/users/create`），其注册表单带有同样的控件。两个表单都从 `ui/src/lib/grants.js` 渲染复选框，并拒绝在未选择任何网络的情况下授予任何权限。

### 会话管理

会话使用 JWT 令牌（HS256），claim 包含会话 ID（UUID）、用户名与签发时间戳。签名密钥是临时的：每次服务启动时通过 `crypto/rand` 生成 32 字节随机数，绝不落盘。`InitSessionManager` 在启动时运行，会清除所有已存在的会话（`DELETE FROM sessions`），因为旧令牌在新密钥下无效。`TOWN_OS_SIGNING_KEY` 环境变量可覆盖生成的密钥。会话在最后一次使用后 7 天过期。一个后台清理任务定期移除过期会话。

**被禁用账户的令牌一到就是死的。** `Validate` 检查 `Disabled` 并拒绝，因为登录之后的每一个请求都仅由该函数授权：没有这项检查，禁用一个账户只是阻止它*再次*登录，而它已经持有的令牌在整个会话生命周期内仍然有效，并且会因被使用而自我续期。

**没有会话管理器意味着不提供服务，而不是敞开服务。** 这台机器上的每一个授权判定，过去都由同一个 nil 推导而来：`requireAuth`、`requireAdmin`、`requireGrant`、`revokeSession`、`requireNetworkScope` 与 `callerIsAdmin` 都把 `GetSessionManager() == nil` 读作"认证根本没有配置，那就放行吧"。这就让*没有人可供认证*与*所有人都被授权*成了同一个状态——整个授权面距离把 `POST /account/create` 与 `POST /packages/install` 端给一个匿名调用者，只差一个未设置的字段；而这台控制器是以 root 身份驱动宿主机 podman socket 的，类型系统里没有任何东西点出这一点，真发生了也不会有任何错误。

现在这个条件是 **`ServerConfig.AuthDisabled`：明说，而非推断**。在认证启用的情况下缺少会话管理器属于配置错误，`NewHandler` 会返回 `ErrAuthNotConfigured` 而不是一个 handler ——在构造期拒绝，而不是每个请求各拒绝一次，因为一台启动之后对每条需鉴权路由都回 500 的机器是一场令人困惑的故障，而一台拒绝启动的机器会在日志里把问题说明白一次，趁它还能被修好。中间件同样拒绝这一状态，因此由任何其他路径拼装出来的 handler 集合也是关闭的。

`InitTestServer` 会在——且仅在——配置未安装会话管理器时设置 `AuthDisabled`。这正是让那约 230 处从不构造会话管理器的测试调用点原封不动继续工作的原因；而*确实*构造了会话管理器的测试，其鉴权仍旧被强制执行——在那里关掉它会把整个测试套件里的每一条授权断言变成同义反复。

`callerIsAdmin` 是答案本身发生改变（而非仅仅换了位置）的那一处：对无法识别身份的调用者，它现在返回 **false**，而过去返回 true。抵达它的每一条路由都位于 `requireAuth` 或 `requireAdmin` 之后，而这两者现在都会直接拒绝该状态，因此实践中它不可达——但一个做脱敏的辅助函数，不是可以凭这一点去慷慨的地方。

`SessionManager` 接口提供：`Create`、`Validate`、`Revoke`、`RevokeAllForUser`、`Cleanup`、`List`、`GetUsername`、`HasActiveAdminSessions` 与 `StartCleanup`。

会话 API 端点：

- `POST /account/authenticate` —— 用户名/密码登录（公开）。返回 JWT 令牌与账户对象。所有认证失败（密码错误、用户不存在、账户被禁用）都返回同一个通用的 "invalid credentials" 错误，以防用户枚举。
- `GET /account/sessions` —— 列出当前已认证用户的会话（需要鉴权）。
- `GET /account/me` —— 获取当前已认证用户的用户名（需要鉴权）。
- `POST /account/session/revoke` —— 按 ID 撤销特定会话（需要鉴权）。

### 审计日志

所有管理操作都被记入审计日志。每条记录包含：自增 ID、账户（用户名）、动作描述、请求路径、经过清洗的详情（凭据被掩码）、成功标志、错误信息与创建时间戳。

**清洗器做的是掩码而非删除**，它把凭据的值替换为 `[REDACTED]` 并保留键名。审计的阅读者应当能够看出某个字段存在过但被扣下了，而不是根本无法把它与一个从未携带该字段的请求区分开。它以不区分大小写的方式，把 `auditRedactedKeys` 与整个键名、以及键名最后一个下划线之后的后缀做匹配，因此 `smtp_password` 会被捕获，而不需要一条同样会吞掉无害名称的子串规则；它还会同时递归进数组与 map。包安装的 `responses` map 被视为**不透明**并整体掩码：它的键属于包作者，因此没有可供匹配的词汇表，而它的值恰恰就是生成的 `type: secret` 与 `type: oauth` 答案——日志绝不能变成它们的副本。裸的 `key` 刻意**不在**列表上——否则后缀规则会捕获 `public_key`，而 `POST /networks/peers/add` 正携带该字段，且 WireGuard 公钥在构造上就是公开的，同时它又是唯一能说明"登记的是哪台设备"的字段。

被跟踪的动作包括：创建/修改/移除文件系统，添加/移除/移动/刷新仓库，安装/卸载包，清除卷，禁用/启用包，设置单元状态，创建/更新/禁用账户，认证，撤销会话，更新设置，忽略升级提示，上传/下载归档，创建/更新/移除/重建 page，上传/删除 VM 镜像。

只读端点被明确排除在审计日志之外。被排除的路径包括根路径（`/`）、所有 GET 列表/查询端点、信息端点（`/packages/installed/info`）、应答获取（`/packages/last-responses`、`/packages/responses`）、安装预览（`/packages/install-preview`）、版本/问题查询、时区列表、pages 列表端点、状态 ping、系统服务列表（`/system-services`）、审计日志查询、设置读取，以及日志流式端点。

- `POST /audit/log`（localhost 或需要管理员）—— 查询审计日志，支持基于游标或偏移的分页、按账户过滤、排序与搜索。

### 设置管理

键值形式的设置存储在 SQLite 中。默认设置包括 `default_quota`（50 GB）、`max_archive_size`（1 GB）、`archive_unpack_timeout`（600 秒）、`locale`（en-US）、`dns_tld`（home）、`dns_resolution_mode`（auto）、`dns_local_forwarders`（false）、`peer_ttl`（7200 秒）与 `gfeh_partition_quota`（0）。`proton_image` 只在带 `proton` 构建标签的构建中注册。完整表格见 [Settings](#设置项)。

- `GET /settings` —— 获取所有设置（需要管理员）。
- `POST /settings/get` —— 按键获取特定设置（需要管理员）。
- `POST /settings/set` —— 设置某项设置的值（需要管理员，计入审计）。字节值类设置（`default_quota`、`max_archive_size`）接受人类可读的字符串（例如 "500GB"、"10MB"），它们会被解析并以数值字节数存储。

**每一个账户管理器的每个方法都接收 context，而 `dbTimeout` 是一个上限，而非全部。** 它们过去每次查询都自开一个根 context（`account.dbCtx`，现已移除），这意味着调用方的取消停在管理器边界上：一个被放弃的 HTTP 请求仍在继续干活，优雅关闭也打断不了一次查询。这在此处比在别处更要紧，因为 `OpenDB` 设置了 `SetMaxOpenConns(1)` —— SQLite 只允许一个写者，于是每一次查询都被串行化在同一条连接之后，一次慢查询会把其他每一个调用方都卡在一段无法打断的 30 秒等待之后。

`account.queryCtx` 改为从调用方派生：带有更短 deadline 的调用方保留自己的 deadline，没有 deadline 的调用方仍旧不会永远挂住，而被取消的调用方会终止查询，而不是任由它跑完自己的钟。nil context 被读作 `context.Background()` 而不是 panic ——管理器不是因为调用方漏传一个参数就把整台机器带下去的那一层，而直接构造 handler 的测试会让服务端 context 保持为 nil。

Handler 传 `c.Request().Context()`；后台 goroutine 传服务端作用域的 context，绝不传请求的 —— 因为该操作必须比触发它的那个请求活得更久。

**`getLocale()` 是唯一一处刻意的例外**，它使用服务端 context 而不是接收一个。它被约 55 处调用，几乎全都在构造错误消息；而请求 context 本来也是错误的界限：它唯一已被取消的情形是客户端挂断了，那时这条消息无论如何都不会被送达。

六个全部完成 —— `SettingsManager`、`AuditManager`、`PagesManager`、`NetworkManager`、`SessionManager` 与 `Manager` —— 连同 `OpenDB`，`dbCtx` 已经消失。

有两个方法取**服务端** context 而非调用方的，且两者都是刻意为之。`AuditManager.LogEntry` 由 `auditMiddleware` 在 handler 返回*之后*调用，用以记录它做了什么：传请求 context 会让一个中途挂断的客户端取消掉那条记录该请求的写入，于是最少被记录下来的动作，恰好就是有人中途断开的那些。`NetworkManager.ReapExpiredPeers` 是后台的 peer 清扫，它只完成一半会留下活的 WireGuard 设备仍在携带的 peer。

`Manager.Authenticate` 接收的 context 约束它前后的两次查询，但**不**约束夹在中间的 argon2id 哈希 —— argon2 没有取消机制，而限制并发哈希（每次 64 MiB）的是 `loginGate`。

### 设置 UI

系统设置界面为所有系统级设置提供管理员可配置的控件。每项设置都展示在一个带边框的区块中，含标题、以人类可读格式显示当前值的说明，以及一个带数字输入框、单位选择器与保存按钮的表单。

- **Default Volume Quota** —— 可按 GB、MB 或字节配置。设为零时显示 "0 (no quota)"。
- **Max Archive Size** —— 可按 GB、MB 或字节配置。控制归档上传所允许的最大文件大小。
- **Archive Unpack Timeout** —— 可按秒、分钟或小时配置。控制解包上传归档所允许的最长时间。
- **Language** —— 一个下拉框，以母语文字显示常用语言。可展开区域会显示扩展语言环境。未填充的语言环境带星号显示并被禁用。
- **Proton Image** —— 一个可编辑文本输入框，用于填写 Proton 运行器容器镜像引用（例如 `quay.io/town/proton:latest`）。
- **Local DNS Forwarders** —— 一个由 `dns_local_forwarders` 支撑的开关。其下方显示 rolodex *实际*正在转发到的地址，这些地址读自 `GET /dns/status` 而非由设置推断；当发现过程没有找到任何可用地址时，面板会说明仍在使用公共转发器，而那正是"开关显示为开、却什么也没变"的那唯一一种情形。参见 [Local forwarders](#本地转发器)。

当前值会被分解为最合适的单位来显示（例如 1073741824 字节显示为 "1 GB"，120 秒显示为 "2 minutes"）。输入校验会拒绝负数与非数字值。

## 包升级

### 升级检测

升级系统把已安装的包版本与已配置仓库中的最新可用版本作比较。当存在更新的版本，或检测到本地修改时，该包会被标记为可升级。

- `GET /packages/upgrades`（需要鉴权）—— 列出可用升级。每一项包含 repo、name、已安装版本、最新版本与一个 changed 标志。
- `POST /packages/upgrades/dismiss`（需要管理员）—— 把当前的升级标记为已忽略。计算当前升级集合的 SHA256 哈希并存入 `dismissed_upgrades_hash` 设置。

状态 ping 响应中包含 `upgrades_available`（数量）与 `upgrades_dismissed`（布尔值，哈希匹配时为真）。

## 网络

### UPnP 端口映射

`upnp.Manager` 接口提供 `AddPortMapping` 与 `RemovePortMapping`，用于经由 UPnP/IGD 在本地网络网关上管理 TCP 端口转发。其实现通过 SSDP 发现 Internet 网关设备，并使用 WANIPConnection2 的 SOAP 方法。本机 IP 通过连接一个外部地址（8.8.8.8:80 UDP）来探测。

### 网络控制器

网络控制器管理按包划分的端口转发与 UPnP 映射。每个有网络需求的包都有一个 JSON 状态文件，指明端口的外部/内部映射、UPnP 标志与转发标志。

- **Socat 转发**（当 `forward=true` 时）—— 运行 `socat TCP-LISTEN:{externalPort},fork,reuseaddr TCP:127.0.0.1:{internalPort}` 来转发流量。
- **UPnP 映射**（当 `upnp=true` 时）—— 在网关上映射端口。当 `forward=true` 时映射外部到外部（由 socat 监听）；当 `forward=false` 时映射外部到内部（由 podman 网桥处理）。
- **Reconcile** —— 通过 fsnotify 监视状态文件，按需停止/启动转发器与映射。
- **续期** —— UPnP 映射每 10 分钟续期一次，TTL 为 1800 秒。
- **关闭** —— context 被取消时移除所有 UPnP 映射并杀掉所有 socat 进程。

### 依赖共享网络

包的依赖共享父包的 podman 网络。这让同一依赖树中的容器可以直接通过容器名互相通信（借助共享网络上 podman 内置的 DNS），而不必经由宿主机端口转发。

- **幂等的网络创建** —— 无论是否存在网络控制器（NC），每个服务单元都包含 `ExecStartPre=-/usr/bin/podman network create {network}`。这是一道启动顺序上的安全网：若 NC 尚未创建该网络（例如镜像未构建、systemd 竞态），服务仍然能启动。NC 也会创建该网络——谁先到谁生效，另一个成为空操作。
- **网络归属** —— 父包拥有该 podman 网络（`town-os-net--{repo}-{name}-{version}`）。NC 在 `ExecStartPre` 中创建它，并在 `ExecStopPost` 中移除它（`podman network rm -f`）。
- **依赖加入父网络** —— 依赖的服务单元使用 `--net {parent-network}` 而不是创建自己的网络。它们在 `ExecStartPre` 中幂等地创建该网络（以防它们先于父包启动），但从不移除它。
- **没有端口的独立包**沿用原有模式：在 `ExecStartPre` 中先 `podman network rm -f` 再 `podman network create`，并在 `ExecStopPost` 中 `podman network rm -f`。只有既没有 NC 也没有父 NC 的独立包才会在 `create` 之前执行 `rm -f`。
- **带依赖的父包**在 `ExecStartPre` 中**不**先 `rm -f` 再 `create`，因为依赖可能已经在该网络上运行（借助 `Before=` 排序，它们先启动）。

### 依赖的 systemd 排序

依赖的 systemd 单元带有排序指令，确保相对于父包的启停顺序正确：

- **依赖单元**：`PartOf={parent-service}`（停止父包会级联到依赖）与 `Before={parent-service}`（依赖先于父包启动、后于父包停止）。
- **父单元**：`Wants={dep1} {dep2} ...` 与 `After={dep1} {dep2} ...`（父包需要依赖，并在启动前等待它们）。
- **网络控制器**：既有的针对 NC 的 `Wants=` 会与依赖的 `Wants=` 目标合并。

这些通过 `PackageUnitConfig` 的字段配置：`ParentNetwork`、`ParentUnitName`（用于依赖）与 `DependencyUnitNames`（用于父包）。reconcile 会从依赖记录与 `ParentName()` 计算出它们。

### 依赖环境变量

父包会获得用于在共享网络上访问其依赖的环境变量：

- `TOWNOS_DEP_{KEY}_HOST` —— 该依赖的 podman 容器名（可通过共享网络上的 podman DNS 解析）。
- `TOWNOS_DEP_{KEY}_PORT_{containerPort}` —— 容器侧端口号（由于父包与依赖在同一网络上，无需宿主机端口映射）。
- `TOWNOS_DEP_{KEY}_PORT_{NAME}` —— 当依赖在 `network.external` / `network.internal` 中声明了语义端口名时（见下文 **具名端口**），会在数字形式之外额外发出这一形式。名称会被转为大写，因此依赖中的 `sql` 在父包上变成 `TOWNOS_DEP_DB_PORT_SQL`。数字形式与具名形式并存，且始终携带相同的值。

### 依赖模板变量

除上述运行时环境变量之外，依赖的主机与端口值在包编译期也可作为 `@variable@` 模板标记使用。这让父包能在编译期于其 `environment` 字段值中引用依赖，也让**同级依赖**能在 `dependencies.<key>.responses` 块中互相引用。

- `@dep_KEY_host@` —— 解析为该依赖的 podman 容器名（可通过共享网络上的 podman DNS 解析）。
- `@dep_KEY_port_N@` —— 解析为该依赖的数字容器端口 N。
- `@dep_KEY_port_NAME@` —— 解析为该依赖以语义名 `NAME` 标记的容器端口（见下文 **具名端口**）。模板中为小写；与环境变量后缀不区分大小写地匹配。对同一个端口，它与 `@dep_KEY_port_N@` 并存。

模板键由 `TOWNOS_DEP_*` 运行时环境变量名推导而来：去掉 `TOWNOS_` 前缀并把其余部分转为小写。例如 `TOWNOS_DEP_DB_HOST` 变成模板键 `dep_db_host`，`TOWNOS_DEP_DB_PORT_5432` 变成 `dep_db_port_5432`。

`@dep_*@` 形式只在本来就会执行 `@variable@` 替换的地方生效——`environment` 值与依赖的 `responses`。在文件模板的 `content` 内部，请改用 Go 模板的 `.Dep` 命名空间（见上文 **文件模板**）：`{{.Dep.KEY.Host}}` 与 `{{index .Dep.KEY.Ports "sql"}}` 携带相同的值。`.Dep` 由同一套 `TOWNOS_DEP_*` 计算填充，并把每个端口同时以其数字键（`"5432"`）和（若声明过）其小写语义名（`"sql"`）暴露出来。

在**父包**一侧，这些变量在依赖安装完成、其容器名与端口已知之后才被解析。它们在单元生成期间被应用到父包的环境变量值上。reconcile 也会重建依赖环境变量，使 systemd 单元在重启与版本变更之后仍然正确。

在**依赖**一侧（即 `dependencies.<key>.responses` 下声明的、引用另一个同级键的应答），解析发生在 `installDependencies` 期间，通过一次拓扑排序完成：

- `src/svc/systemcontroller/controller_install_dependencies.go` 中的 `orderDependencies` 解析每个同级依赖的 `Responses` 中的 `@dep_KEY_host@` / `@dep_KEY_port_N@` 标记并构建 DAG。没有引用的同级依赖先运行；有引用的同级依赖在它所指名的同级之后运行。在同样就绪的依赖之间，以字母序打破平局以保证确定性（Go 的 map 迭代是随机的，因此排序对可复现性是必需的）。
- 同级依赖之间的环是硬错误，会在任何依赖被置备之前中止安装。
- 对该顺序中的每个依赖，都会在 `depIP.CompileWithContext` 运行**之前**对其 `Responses` 调用 `applyDepTemplates`，把 `@dep_OTHER_*@` 标记替换为已安装同级所累积的容器名/端口值。若没有这次预编译替换，依赖 YAML 中带类型的问题（例如 `type: port`，或任何其 `Output` 会执行 `strconv.ParseUint` 的类型）就会以 `ErrInvalidResponseType` 拒绝那个字面占位符，导致安装中途中止，并在磁盘上留下一个装了一半的父包。
- 自引用（依赖 X 引用 `@dep_X_host@`）会被忽略，而不是当作环。对未被声明为同级键的名称的引用，会被视为外部模板变量并在排序时忽略。
- 安装处理器通过 SSE 流式推送错误，并从 HTTP 处理器返回 `nil`，因此无论安装是否真的完成，审计日志都始终记录 `success=true`。这意味着部分安装失败（装了一半的依赖树、`installed/<repo>/<parent>/<version>/` 之下的孤立 btrfs 卷）只在 SSE 流与 systemd 单元列表中可见——在 `/audit/log` 中看不到。

示例：一个带依赖键 `db`（一个暴露 5432 端口的 Postgres 容器）的包，可以在其 environment 段中使用 `@dep_db_host@` 与 `@dep_db_port_5432@`，而不必硬编码 `127.0.0.1`：

```yaml
environment:
  DB_HOST: "@dep_db_host@"
  DB_PORT: "@dep_db_port_5432@"
```

同级互相引用的示例（jitsi 的形态）：`jitsi` 依赖 `prosody`、`jicofo` 与 `jvb`。`jicofo` 与 `jvb` 各自都需要 prosody 的容器名与内部 XMPP 端口，因此父包 YAML 通过每个引用方依赖的 `responses` 块把它们串起来。`orderDependencies` 先安装 `prosody`，然后是 `jicofo` 与 `jvb`（这两者之间按字母序），每个都已把占位符替换为 prosody 的具体容器名与端口 5222：

```yaml
dependencies:
  prosody:
    package: prosody
  jicofo:
    package: jicofo
    responses:
      xmpphost: "@dep_prosody_host@"
      xmppport: "@dep_prosody_port_5222@"
  jvb:
    package: jvb
    responses:
      xmpphost: "@dep_prosody_host@"
      xmppport: "@dep_prosody_port_5222@"
```

### 依赖共享卷

同一依赖树中的包可以通过双方共同选择加入的方式共享 btrfs 子卷。依赖的作者把某个卷标记为 `shareable: true`；父包的作者随后声明一个 `expose:` 块（把该依赖的卷挂载进父包的容器），或者在另一个依赖上声明 `consume:` 块（把一个同级的卷挂载进另一个同级的容器）。没有 `shareable: true` 的卷不能被跨包挂载——安装/reconcile 环节会拒绝对非共享卷的任何引用。

这套接线是既有 `HostVolumeMount` 基础设施之上的一层薄封装：安装路径把每个 `expose`/`consume` 条目解析为一个指向生产方依赖在磁盘上的 btrfs 子卷的 podman `-v <hostpath>:<containerpath>:<options>` 标志。reconcile 在每次启动时从父包持久化的 YAML 重建同样的标志，而 `installUnitIfChanged` 的内容差异比对会自动捕捉变化——不需要特殊的重启钩子。

**依赖侧选择加入。** 依赖按卷声明 `shareable: true`：

```yaml
# radarr/1.0.yaml
volumes:
  movies:
    mountpoint: /movies
    quota: "@moviesize@"
    shareable: true     # 选择加入：父包或同级可以挂载它
  config:
    mountpoint: /config  # 非共享；若有父包尝试 expose 它则被拒绝
```

**父包 → 依赖（`expose:`）。** 父包的 `dependencies.<key>.expose:` map 指明要绑定挂载进父包容器的依赖卷。每一项接受一个容器路径与可选的 `readonly` 标志（默认 `true`，因为父包通常只是消费依赖的产出）：

```yaml
# plex/1.0.yaml
dependencies:
  radarr:
    package: radarr
    expose:
      movies:                  # radarr YAML 中的卷名
        path: /data/movies     # Plex 容器内的路径
        readonly: true
  sonarr:
    package: sonarr
    expose:
      tv:
        path: /data/tv
        readonly: true
```

**同级 → 同级（`consume:`）。** `dependencies.<key>.consume:` 列表把一个同级依赖的卷挂载进**本**依赖的容器。每一项接受 `from:`（同一父包 `dependencies:` map 中的同级依赖键）、`volume:`（同级 YAML 中的卷名）、`path:`（消费方依赖上的容器路径），以及可选的 `readonly`（默认 `false`，因为同级之间的共享通常需要可写——例如某个 *arr 要往下载客户端的 `/downloads` 里导入）：

```yaml
# media/1.0.yaml —— 把下载客户端与各 arr 接线起来的父包
dependencies:
  qbittorrent:
    package: qbittorrent
  radarr:
    package: radarr
    consume:
      - from: qbittorrent
        volume: downloads
        path: /downloads
  sonarr:
    package: sonarr
    consume:
      - from: qbittorrent
        volume: downloads
        path: /downloads
```

**拓扑安装顺序。** `consume.from` 引用会与既有的 `@dep_KEY_*@` 应答引用一起，为 `orderDependencies` 构建的安装期 DAG 添加边。消费同级 A 的依赖 B 会严格在 A 之后安装，这样 B 的容器启动时 A 的 btrfs 子卷已经存在。consume 边之间的环（A 消费 B，B 消费 A）是硬错误，会在任何依赖被置备之前中止安装。自消费（`from:` 等于该依赖自身的键）在校验期即被拒绝。

**校验。** 编译期校验会拒绝：相对路径或含穿越的挂载路径、指向同一 `dependencies:` map 中未声明键的 `consume.from`、自消费，以及同一依赖内重复的 consume 路径。跨包校验（生产方对应卷上是否有 `shareable: true`）发生在安装/reconcile 时加载生产方 YAML 的那一刻——expose 或 consume 了非共享卷的父包会以 `volume %q is not marked shareable on %s` 安装失败。

**模板路径替换。** `expose.<volname>.path` 与 `consume[].path` 与普通卷挂载点一样参与 `@question@` 替换。`consume.from` 与 `consume.volume`（以及 `expose` 的 map 键）是标识符而非数据，不会被替换。

**权限注意事项——绑定挂载会透传 UID/GID。** 依赖在宿主机上的 btrfs 子卷，属主是依赖容器创建它时所用的那个 uid:gid。若依赖以 1000:1000 运行（linuxserver/* 的默认值），而消费方的父包或同级以不同的 uid 运行，消费方在读或写时会得到 EACCES。修复之道在包的 YAML 中，而不在平台里：让共享卷的各包之间的 `PUID`/`PGID` 问题默认值保持一致。`HostVolumeMount.UID`/`GID` 的 chown 行刻意不递归，并且只在依赖作者在可写挂载上显式设置了它们时才生效；共享卷解析器从不自动 chown。

**模板命名空间。** 依赖的可共享卷也会在文件模板的 `.Dep` 命名空间中以 `.Dep.<key>.Volumes.<volname>` 暴露（其值是该卷在依赖容器内的挂载点）。这与 `.Dep.<key>.Ports` 是平行的。非共享卷被刻意排除在该 map 之外，因此文件模板无法触及依赖作者未选择暴露的数据。

**卸载顺序。** 既有的 `Before=`/`PartOf=` 指令已经保证父包先于依赖停止、依赖先于其生产方停止，因此当父包被卸载（级联卸载其依赖）时，消费方的容器在生产方的卷被触碰之前就已经消失。不需要新的卸载逻辑。

**范围之外。** 一个依赖恰好属于一个父包（既有不变量）；共享卷并不会让依赖变成多租户的。反方向共享（父包的卷 → 依赖）在 v1 中不支持；若将来确有需要，模式仍是可扩展的。系统服务（`town-os-system--*`）不具备此功能——`GenerateSystemServiceUnit` 不查询 `expose`/`consume`。

### 具名端口

依赖端口的引用可以使用语义名，而不是容器端口号。依赖在 `network.external` / `network.internal` 中把名称声明为 YAML 键；父包则通过 `@dep_KEY_port_NAME@` 引用同一个端口。这让原始端口号只存在于唯一一处（拥有它的那个依赖），并让父包谈论角色（`sql`、`http`、`admin`）而不是协议琐事。

**规范形态。** 端口号由依赖拥有——理想情况下作为 `type: port` 问题的默认值，这样自动生成与覆盖都能正常工作：

```yaml
# 依赖：named-db/1.0.yaml
environment:
  PGPORT: "@port@"
network:
  internal:
    sql: "@port@"
questions:
  port:
    query: "What port should PostgreSQL listen on?"
    type: port
    default: "5432"
```

```yaml
# 父包：named-parent/1.0.yaml
environment:
  DB_HOST: "@dep_db_host@"
  DB_PORT: "@dep_db_port_sql@"   # 父包中任何地方都没有 "5432"
dependencies:
  db:
    package: named-db
```

**Map 模式。** `network.external` 或 `network.internal` 中的端口条目，其 YAML 键要么是：

- 一个数字端口字符串（旧形式）：`"5432": "5432"` → 宿主机端口 5432 → 容器端口 5432。不记录任何名称。
- 一个匹配 `PortNameRegexp`（`^[a-zA-Z][a-zA-Z0-9_]*$`）的语义名：`sql: "5432"` → 容器端口（值）同时兼作宿主机端口，且名称 `sql` 被存入 `PackageNetwork.{External,Internal}Names[containerPort]`。名称必须以字母开头（以避免与数字解析产生歧义），并可包含字母数字与下划线。

两种形式可以并存于同一个 map 中；解析器依据键来分支。一个名称映射到两个不同的容器端口，或两个名称映射到同一个容器端口，都是编译期错误。编译后的 `Package` 类型在既有的 `PortMap` 之外新增两个可选的 `PortNameMap` 字段；只关心数字端口的消费方（单元生成、网络状态序列化）不受任何影响。

**环境变量与模板的发出。** 对编译后依赖中的每一个端口，安装器都会发出 `TOWNOS_DEP_<KEY>_PORT_<N>=<N>`（始终发出）。若该端口有名称，它还会额外发出值相同的 `TOWNOS_DEP_<KEY>_PORT_<UPPER_NAME>=<N>`。模板解析器去掉 `TOWNOS_` 前缀并把其余部分转为小写，因此 `@dep_db_port_5432@` 与 `@dep_db_port_sql@` 解析为同一个值。`controller_install_dependencies.go` 中的 `depKeyRefRegex` 接受两种形式；同级依赖的拓扑排序在构建 DAG 时也能识别具名引用。

**向后兼容。** 使用数字形式的既有包继续原样工作——不强制迁移。父包可以在同一个文件中对同一个依赖混用数字与具名引用。reconcile 在启动期间重建两种形式，因此仍然存在的既有安装绝不会退化。

**何时使用名称。** 只要父包引用了依赖的端口就该用。名称是父包唯一可以援引的事实；端口号归依赖所有。优先对内部端口使用名称（父包与依赖之间的流量正是走共享 podman 网络的），外部具名端口虽然允许但不常见，因为父包通常不会通过宿主机绑定去拨号依赖。

## 网络（WireGuard Overlay）

一个**网络**是一个具名的 WireGuard overlay，与一个 DNS TLD 配对。包安装进网络；peer 加入网络；而 TLD 决定了谁能解析什么（参见 [Network TLDs, Dual-Home, and Split-Horizon Resolution](#网络-tld双栖与分离视界解析)）。

### 网络模型

`account.Network`（`src/account/network.go`）携带：`Name`、`TLD`、`Subnet`、`Address`（本机自己的 overlay 地址，永远是 `.1` 主机位）、`PublicKey`、`PrivateKey`（绝不序列化）、`ListenPort`、`Enabled` 与时间戳。名称必须是 DNS 标签安全的（`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`，最长 32 字符），因为它们会被复用为 WireGuard 接口后缀与 systemd 单元名。

`Enabled` 只控制*传输层*：为假时不拉起 WireGuard 接口，切断远程访问，而本地 DNS 解析与容器本身继续运行。

### home 网络始终存在

`DefaultNetworkName` 是 `home`，它由 **`account.InitNetworkManager` 播种**，与建表同时进行——而不是由启动时的 reconcile 播种。因此从数据库存在的那一刻起它就在：在控制器启动之前、在每一个测试服务器中，以及在这台机器服务的第一个请求时。`account.DefaultNetwork()` 是那一行的规范定义。

这很重要，因为下游的一切都是假定它存在而写的：第一个账户被限定到它（[每个账户都属于 home 网络](#每个账户都属于-home-网络)），默认 TLD 就是它的 TLD，而 gfeh 会给它一个分区并把创始账户安置在那里。如果它需要先被创建，就会存在一个窗口期，其间上述一切都不成立——这正是过去对象存储在首次启动时一直死着、直到后来某次重启碰巧发现网络已经存在的原因。

它**不能被移除**（`ErrNetworkProtected`，且 `POST /networks/remove` 会拒绝），也不能被创建第二次——对 `home` 调用 `POST /networks/create` 会因 TLD 冲突检查而得到 409。

它是**仅 DNS 的**：`applyNetworkTransport` 不给它 WireGuard 接口、不给 overlay 子网、也不给 peer，因此它永远不可能有隧道设备。所播种的那一行因此**完全不携带传输字段**——空子网、无密钥对、端口 0。这是事实而非占位符；推导出的子网与密钥会是没有任何东西读取的字段。

**在它上面登记 peer 会被拒绝——这是刻意的，而且在两层上都拒绝。** `POST /networks/peers/add` 对 `home` 返回 400，而 `NetworkManager.AddPeer` 无论被谁调用都返回 `ErrNetworkDNSOnly`。这很重要，因为[每个账户都属于 home 网络](#每个账户都属于-home-网络)：如果在那里的登记被接受，仅凭成员身份就等于拿到了一条上隧道的路，而存下来的 peer 描述的是一条并不存在、也永远不会存在的隧道。peer 是在真实 overlay 上动态创建的，所以想要隧道的调用者要指名一个。

这条拒绝过去是**偶然的**：没有任何地方检查网络，处理器一路落到 `netip.ParsePrefix`，对播种行的空 `Subnet` 解析失败，最后以 **500** 冒出来。那读起来像是机器坏了而不是一次拒绝，它没有告诉调用者任何原因，而且只要有什么东西往那一行写入了子网，它就会不再拒绝。这道守卫按名字进行，位于服务端生成密钥对之前，后面还跟着一道针对"无传输行"的检查，以覆盖子网因其他原因为空的网络。

**它的 TLD 来自 `dns_tld`，并由控制器保持二者同步。** 播种时无法知道该值（account 包没有设置 manager），因此该行以裸默认值出现，再由 `ensureDefaultNetwork` 在启动时对齐，且仅在两者不一致时才写入。`POST /dns/tld` 在写入设置的同时也会重新指向它。两者都经由 `NetworkManager.SetTLD`，它的存在正是为此。搞错它不是外观问题：`applyNetworkTransport` 会把 `n.TLD` 交给 `rolodex.EnsureNetworkScope`，由后者决定 home 作用域拥有哪个区域。

### 编址与接口

- **子网** —— `wireguard.SubnetForNetwork(seed, name)` 从一个机器身份种子与网络名推导出确定性的 `/24`。以机器身份为键意味着两台都对外提供 peer 服务的 Town OS 机器会选出不同的子网，因此同时加入两者的设备永远不会遇到冲突。子网取自 `10.64.0.0/10`，以避开消费级路由器常发的 `10.0`/`10.1` 段。种子是 `networkIPAMSeed()`：systemd machine-id，其次是主机名，再次是一个常量，因此推导永不失败——并把实例盐折入其中。
- **接口名** —— `wireguard.InterfaceName(salt, name)` 是 `"town"` 加上加盐网络名 SHA-256 的 4 位十六进制：与创建顺序无关、与网络数量无关，并且在内核 15 字符的限制之内。wg-quick 从配置文件名推导接口名，因此配置被写作 `<InterfaceName>.conf`。`systemcontroller.NetworkInterfaceName(name)` 是集成测试所使用的、已应用盐值的形式，这样测试就绝不会去断言一个根本没被创建的设备。
- **监听端口** —— `wireguard.ListenPortForName(salt, name)` 以加盐名称的哈希为偏移，从 `DefaultListenPortBase`（51820）起算，并在遇到另一个网络已占用的端口时向前探测。

#### 实例盐

WireGuard 的接口名、它的 UDP 监听端口与它的 overlay 子网都是**命名空间全局的**，而测试容器与开发容器都以 `--net host` 运行（这是刻意的——桥接网络的 DNS 在强制门户网络下会失效）。没有盐值时，一台 `make test-full` 的机器与一台 `make dev` 的机器会为同一个网络名推导出*相同*的接口名与监听端口：后启动的那个无法创建自己的设备，它的 overlay 直接是死的。两个并发的测试工作树也会以同样方式冲突——IRON RULE。

`TOWN_OS_WG_SALT`（`EnvWireGuardSalt`）被读取一次到 `wireGuardSalt` 中。测试框架通过 `make/lib.sh` 中的 `wireguard_salt` 把它设为 `<role>-<INSTANCE_ID>`——role 在同一个检出中区分测试机与开发机，`INSTANCE_ID` 区分不同检出，两部分缺一不可。对于给定的 role 与检出它是稳定的，这一点对开发模式很重要，因为开发模式的数据库跨运行留存，否则其中存储的子网会指向以上一次盐值命名的设备。**真实机器不设置任何值，并保持历史上不加盐的名称**；空盐值会让每一次推导原样返回。

**podman 的默认子网池必须避开 `10.64.0.0/10`。** 运行时镜像写出的 `/etc/containers/containers.conf` 中含 `default_subnet_pools = [{"base" = "172.16.0.0/12", "size" = 24}]`，正是因为 podman 的默认值（10.89/16、10.90/15、10.96/11 等）全都落在 overlay 范围之内：范围内的 `/24` 会因与 overlay 路由冲突而被跳过，池子在负载下耗尽并报 "could not find free subnet from subnet pools"，包的容器网络随之停止工作。不要删除该文件，也不要把池子重新扩回 `10.64.0.0/10`。

`wireguard` 包**自身不做任何接口控制**。它生成密钥对并渲染 wg-quick 风格的配置；由 systemcontroller 把渲染好的配置写入与宿主机共享的网络状态目录，再由一个生成的 systemd 单元拉起或关闭内核接口。这正是让 systemcontroller 容器免于依赖宿主机网络命名空间的原因。

**`applyNetworkTransport` 中的顺序很重要。** 必须在接口已启动、overlay 地址已分配、链路处于 UP 状态并被路由覆盖*之后*，才去编排 rolodex——已分配不等于可用。先编排它，就是在要求 rolodex 绑定一个宿主机尚不具备的地址；绑定会以 `EADDRNOTAVAIL` 失败，而该监听器会永久死亡，因为 rolodex 在 spawn 时就登记了监听器，那具"尸体"随后会挡住每一次重新声明。

### Peer

`account.NetworkPeer` 携带 `Network`、`PublicKey`、`Name`、`AllowedIP`、`Endpoint`、`Rolodex`、`CreatedBy`、`ExpiresAt` 与 `CreatedAt`。

- **`Rolodex`** 标记那些在其 overlay 地址上运行 rolodex DNS 服务器的 peer。本机随后把该地址注册为按 TLD 的转发器，于是共享 TLD 之下、在该 peer 上具有权威性的名称便可跨 overlay 解析。手机与笔记本电脑保持它为 false。
- **`CreatedBy`** 是归属键：持有 `wireguard` 授权的账户只能刷新自己创建的 peer，因此受限账户无法让别人的 peer 一直存活。
- **`Endpoint`** 取自**登记客户端所拨打的地址**（其 `peers/add` 请求的 `Host` 头），而不是取自本机对自身的认知。本机的公网 IP（来自 ipinfo.io）或局域网地址在 NAT、端口转发或中继之后是不可达的——同一 Wi-Fi 上的手机无法回环到公网 IP，更完全无法路由到私有局域网地址，于是该 peer 会向着虚空握手，而在用户看来这就像 DNS 坏了。被拨打的地址在构造上就是可达的：请求正是经由它到达的。若没有可拨打的地址（例如环回登记），则**省略** endpoint，而不是设成一个不可能工作的值。

### Peer 登记的 TTL 与回收器

登记不会永久有效。`peer_ttl` 设置（单位秒，默认 `7200`）决定一次登记的有效时长。长期存活的客户端会在其到期前通过 `POST /networks/peers/refresh` 刷新自己的 peer；被弃用设备的 peer 则自行过期，因此只增不减的 `peers/add` 端点不会悄悄堆积死 peer 并烧掉 overlay 地址。`ExpiresAt` 为 nil 表示该 peer 永不过期——例如 rolodex 服务器与运维手动添加的设备这类永久 peer。

过期时间始终由**服务端计算**为 `now + peer_ttl`；调用方从不选择它。一个后台回收 goroutine 调用 `ReapExpiredPeers`，随后为每个受影响的网络重新渲染一次传输配置，使运行中的 WireGuard 设备与 rolodex 转发器丢弃被回收的 peer。它是尽力而为且幂等的：持久化的 peer 集合才是事实来源，一次失败的重新渲染会由下一次滴答或启动时的 reconcile 修复。`peerReapInterval` 是 TTL 的四分之一，并被约束在 `[1m, 15m]`，因此失效的 peer 最多在过期后残留约 TTL/4，而过小或过大的 TTL 都不会导致病态的扫描频率。

### 已连接 Peer

`GET /networks/peers/connected` 把持久化的行与每条隧道的实时内核状态联接起来。持久化的那一半（名称、账户、overlay 地址、过期时间）回答"谁被允许接入"；`wg show <iface> dump` 的那一半（握手、观测到的 endpoint、传输量）回答"此刻谁真的在线"——任何一半单独都不是完整的问题，这正是存在 `ConnectedPeerView` 而不是复用 `account.NetworkPeer` 的原因。

解析逻辑位于纯函数 `wireguard.ParseDump` 中。dump 的**第一行**描述的是接口本身，会被刻意跳过；把它当作 peer 会凭空造出一个持有接口自身密钥的幽灵。`wg` 的 `(none)` 与 `off` 占位符会被解码，而不是作为字面字符串原样透传。

**连接与否，取决于握手是否落在 WireGuard 180 秒的 `REJECT_AFTER_TIME` 窗口内**（`HandshakeStaleAfter`）——那是该协议所能提供的唯一存活性信号。协议没有会话拆除，因此走开的 peer 与仅仅空闲的 peer 在其握手过期之前无法区分。*从未*握手过的 peer 保留 nil 时间戳而不是纪元时间，因为"从未建立"与"离线一天"是关于一台设备的两个不同事实。

systemcontroller 以 `--net host` 运行，因此它本来就与 wg-quick 创建设备的命名空间相同；运行时镜像仅为 `wg` 这一个二进制而附带 `wireguard-tools`（wg-quick 仍在宿主机上、由生成的单元运行）。接口不存在不是错误——被禁用的网络，或传输尚未拉起的网络，本来就没有活跃 peer，而它持久化的行仍然必须渲染出来——而 dump 失败会退化为只显示持久化的行，而不是把面板整个清空。`home` 网络被完全排除：它没有传输层，把它包含进来只会在一个讲"谁通过隧道接入"的面板里放一行永远断连的记录。

**断开连接复用 `POST /networks/peers/remove`**，而不是新增端点。WireGuard 没有可以杀掉的会话，因此移除该 peer 就是唯一存在的强制终止手段。

### 网络 API

- `GET /networks`（需要鉴权）—— 列出所有网络，附带 peer 数量、推导出的接口名与运行状态。私钥绝不暴露。
- `POST /networks/create`（需要管理员）—— 创建网络。接受名称与可选 TLD（默认取名称）。推导子网、生成密钥对、分配监听端口，并返回创建出的网络。名称或 TLD 已被占用时返回 409——包括始终存在的 `home`。
- `POST /networks/remove`（需要管理员）—— 按名称删除网络。home 网络不能被移除。
- `POST /networks/enable` / `POST /networks/disable`（需要管理员）—— 拉起或关闭 overlay 接口。
- `GET /networks/peers?network=<name>`（需要鉴权，并受 `requireNetworkScope` 限制）—— 列出某个网络上已登记的 peer。该路由在 `wireguard` 授权的白名单上，因此受限账户可以访问它；而 peer 列表会指明设备、登记它们的账户以及它们的 overlay 地址——授权是对调用者自己网络的权限，而读操作恰恰是最容易忘记这一点的地方。
- `GET /networks/peers/connected`（**需要管理员**）—— 所有 WireGuard 网络上的每一个 peer，联接实时隧道状态。它刻意比 `requireAuth` 的同类更严，并且不在 `grantRoutes` 中。
- `POST /networks/peers/add`（`requirePeerEnroll`：管理员或 `wireguard` 授权，且限定在调用者的网络内）—— 登记一个 peer。当 `public_key` 为空时，服务器生成密钥对并返回私钥以及一份可直接导入的设备配置。接受可选的 `endpoint` 与一个 `rolodex` 标志。**home 网络是 400** —— 它是仅 DNS 的，不承载 peer，无论谁来请求（参见[home 网络永远存在](#home-网络始终存在)）。
- `POST /networks/peers/refresh`（`requirePeerEnroll`，且只能针对调用者自己登记的 peer）—— 把某个 peer 的 TTL 延长 `peer_ttl` 并返回新的过期时间，使客户端能在 TTL 到期之前从容安排下一次心跳。
- `POST /networks/peers/remove`（需要管理员）—— 按公钥移除 peer。

### 网络 UI

`/dashboard/networks` 列出各网络，并提供创建/移除/启用/禁用操作以及按网络的 peer 登记功能。第二块 **Connected Peers** 面板逐项列出所有 WireGuard 网络上的每一个 peer——设备、登记它的账户、它的 overlay 地址、它正在拨打的 endpoint、实时握手与传输状态，以及它的登记过期时间——并为每一行提供 Disconnect 操作。

## TLS 与本地 CA

Town OS 运行自己的 X.509 证书颁发机构，因此包与 page 的流量可以按名称经 HTTPS 提供服务，在局域网上既不需要公共 CA，也不依赖 ACME。

- **CA**（`src/tls/ca.go`）是位于 btrfs `tls` 子卷下的一对 ECDSA P-256 密钥（`ca.crt`、`ca.key`），有效期 10 年，因此可以跨重启存活。`EnsureCA` 加载已有 CA，或按需生成一个；证书全局可读，而私钥仅属主可读且绝不能被提供出去。CA 失败是非致命的——系统会在没有 HTTPS 的情况下启动，而不是干脆不启动。
- **叶子证书**（`src/tls/leaf.go`）按包、按 page 签发，写作同一目录中的 `cert.pem`/`key.pem`，因此消费方只需要一条挂载路径。`IssueLeaf` 是**幂等的**：当已有证书恰好覆盖所请求的 SAN 集合且仍然有效时，它不碰磁盘直接返回，这正是让 reconcile 每次启动都调用它而不会搅动证书文件的原因。主机名可以是 DNS 名或 IP 字面量；任何能解析为 IP 的进入 `IPAddresses`，其余进入 `DNSNames`。
- **`GET /tls/ca.crt`** 是**公开的**（并位于 `grantCommonRoutes` 中），因此任何客户端——浏览器，或通过 overlay 加入的手机——都能取得根证书并信任这台机器。

包的叶子证书 SAN 集合与它的 A 记录、DANE TLSA 属主和 ingress vhost 由同一个 FQDN 推导；参见 [The package FQDN is one string](#包的-fqdn-是同一个字符串--a-记录叶子证书-santlsa-属主ingress-vhost)。叶子证书还会带上本机在安装网络上的 overlay IP，因此 peer 可以用 WireGuard 裸地址访问该包，而不只是靠名称。

## Ingress

ingress 是共享的 Host 路由器：一个 sidecar，监管一个 Caddy 子进程，并暴露一套由 systemcontroller 编排的 gRPC 管理 API，方式与编排 rolodex 相同。它在内存中持有期望的路由集合，每次变更时渲染一份 Caddyfile，并零停机地重载 Caddy。

- **`src/ingress`** 是容器内的服务（`Server`、`renderCaddyfile`、gRPC 客户端与 `town-os-ingress` 二进制）。它以 `CGO_ENABLED=0` 构建。
- **`src/ingress/ingressctl`** 是 systemcontroller 一侧的生命周期控制器：它生成、安装并重启 `town-os-system--ingress` 单元，并暴露 systemcontroller 所拨号的 gRPC socket 路径。它之所以是一个独立的包，正是为了让无 CGO 的 ingress 二进制永远不会导入 `src/systemd`（后者经由 sdjournal 引入 cgo）。

### 路由

- **`:443`** —— 每条路由一个 `https://<hostname>` vhost，用该路由以文件方式固定的本地 CA 叶子证书终止 TLS（若是公共 FQDN 则用显式的 ACME 签发者），并反向代理到共享的 `town-os-ingress` podman 网络上的后端容器。
- **`:80`** —— 按 Host 路由：pages（`ServeHttp`）直接以明文 HTTP 提供服务（静态内容，不含敏感信息），包则获得 HTTP→HTTPS 重定向以保持仅 HTTPS，而任何未被路由匹配的 host 落到默认后端——Town OS UI，因此在 UI 不再霸占宿主机 `:80` 之后，裸 IP 登录（`http://<box-ip>/`）仍然可用。
- **尚未签发叶子证书**的路由（非 ACME、证书目录为空）在 HTTPS 上被跳过，因此置备到一半的条目绝不会让 Caddy 拒绝整份配置；page 仍会获得它的 `:80` vhost，那不需要证书。包只有在 HTTPS 目标真正存在之后才会被重定向，因此不会有任何东西重定向到尚未置备的证书上。

### 渲染

输出**按主机名排序**，因此跨多次 reconcile 渲染出的字节是确定性的——这正是让监管进程对内容未变的重载做空操作的前提。全局配置为 `auto_https off`（证书由 Town OS 管理）与 `protocols h1 h2`（ingress 只发布 TCP，因此基于 UDP 的 H3/QUIC 不可达）。Caddy 的管理 API 被刻意**保持启用**在其默认的容器本地 `localhost:2019` 上：监管进程用 `caddy reload` 编排新路由，而该命令正是与那个端点通信，因此 `admin off` 会让首次启动之后的每一次路由更新都失效。

ingress 是**与网络接口无关的**：它以 `-p 443:443` / `-p 80:80` 发布且不指定宿主机 IP，其 Caddyfile 中**没有 `bind` 指令**，因此 Caddy 在所有接口上监听，并纯粹依据 SNI/Host 选择 vhost。局域网客户端与 overlay peer 命中同一个监听器、SNI 选中同一个 vhost、拿到同一张本地 CA 叶子证书，并被代理到同一个容器。不要添加 `bind` 指令，也不要添加按网络的监听器。

生产环境绑定 443/80；集成测试传入临时端口（渲染为 `host:PORT`），因此 `make test-full` 绝不会在特权端口上冲突。启动流程通过 `RebuildIngress` 以声明式方式编排完整路由集，与 `RebuildDNS` 是同一种推送模型；包与 page 的增删改则通过同一套 gRPC API 编排增量变更。

## 启动状态与刷新

`:5309` 在任何启动工作开始之前就被绑定，因此 UI 可以观看一次启动——包括一次自我更新——的推进过程，而不是去轮询一个死掉的端口。

### 启动桩

`NewBootHandler` 是一个纯粹的 `http.ServeMux`（这是刻意的，这样它永远不可能意外挂载一条真正的 API 路由），只提供三样东西：

- `GET /status/ping` → `{booting, step, done, error, boot_id}`。它在**启动过程中返回 503**，完成后返回 200，因此外部就绪探针——测试容器的 `wait_for_url`、编排系统的健康检查——不会把这个桩当成"服务就绪"而开始猛击一个只启动了一半的控制器。JSON 响应体中仍然携带进度字段，因此 UI 能区分"正在启动"与"完全宕机"。
- `GET /boot-status` → 进度事件的 SSE 流。
- 其余一切 → **403**，而不是 404：该路由在完整处理器中是存在的，只是在切换之前不可用。

`RootHandler.Swap` 在启动末尾把这个桩原子地替换为完整的 Echo 路由器。监听套接字从不关闭，因此不会出现端口抖动，而已经被分发的 SSE 处理器持有各自的 writer，可以跨越这次切换继续流式输出。

### 进度阶段

五个粗粒度阶段，刻意做得少而面向用户——观看自我更新的人想知道的是"控制器"、"DNS"、"系统服务"还是"我的包"卡住了，而不是哪个内部构造函数正在运行：

`boot_controller` → `boot_dns` → `boot_services` → `restart_packages` → `ready`

新鲜度阶段会为每个已安装的包额外发出一个事件，前缀为 `restarting_`（`PackageStepPrefix`）；UI 去掉前缀后把每个渲染成独立一行，权重与粗粒度阶段相同，这样装了很多包的机器展示的是真实进度，而不是一根卡住的进度条。这些按包生成的名称刻意不符合固定阶段所强制的 `[a-z0-9_]+` 形状——它们是动态值。

阶段字面量在 `main.go` 中以 `bs.Step("...")` 调用的形式重复书写，而不是引用常量，因为 `TestBootStepsFrontendInSyncWithBackend` 会从 `main.go` 中解析它们，以证明前端的列表与之一致。**请保持两者同步**；一旦漂移，该测试会大声失败。

### 广播语义

`BootStatus` 可安全并发使用，并且**绝不阻塞启动**。`Subscribe` 会先把历史事件重放给新订阅者（因此迟到的订阅者不会错过任何东西），并把缓冲区大小设为足以容纳完整重放加上余量；若启动已经结束，它会在重放之后立即关闭 channel，使 `for range` 的消费者退出。`publish` 以非阻塞方式发送——缓冲区被填满的订阅者会被丢弃并关闭，其客户端会重连并再次获得历史重放。任何事件都不可能出现在 `Done` 之后。

### 进程身份与刷新

`boot_id` 是每次 systemcontroller 启动时重新生成的随机 UUID，由桩与完整路由器的 `/status/ping` **双方**上报（甚至在未认证的最小 ping 响应中也会携带，因为浏览器在重启期间会短暂地没有令牌）。在请求刷新之前捕获了该 id 的客户端，可以据此区分"旧进程仍在应答"（id 相同）与"新进程已经起来"（id 不同）——否则二者无从区分，因为两者都对 ping 返回 200，且启动完成后都会对 `/boot-status` 返回 404。这正是 UI 的 Refresh Core Services 流程能够观看自己的后继进程的原因。

`/boot-status` 因同样的理由被排除在审计日志之外：跨越处理器切换而保持流打开的 UI，其下一个请求会落到完整路由器上并得到 404。那是这条流预期中的结束，而不是一次运维操作——审计它会在每一次成功的刷新中记下一行失败动作，并把仪表盘上那颗红色的失败计数徽标撑大。

`POST /system-services/refresh`（管理员）按依赖顺序拉取每个系统服务镜像——先是 systemcontroller 镜像（版本锚点，这样它在最后自我重启时，刚拉取的镜像已经在本地），然后是 rolodex（本机的 DNS，其余拉取可能需要它来解析各自的 registry），最后是其余镜像并行拉取（最多 3 个并发）——并留下一个标记，供下一个进程的新鲜度阶段消费以重启已安装的包。

## DNS 管理（Rolodex）

Town OS 内置一个由 `rolodex-dns` 容器驱动的本地 DNS 解析器。rolodex 服务器为已安装的包管理区域文件与记录，并通过 gRPC Unix socket 接口提供本地名称解析。

### Rolodex Manager

rolodex 本身是由 systemd 安装与监管的启动服务——systemcontroller 不在容器层面安装、启动、停止或重启它。取而代之，`rolodex.Manager` 负责：

- **`WriteConfig`** —— 把 `rolodex.yml` 写入 `DataDir`。幂等：当该文件存在、比 systemcontroller 二进制更新、且内容已与预期一致时跳过写入。返回一个布尔值表示文件是否被写入（以便调用方决定是否重启 systemd 单元）。
- **`WaitForDNSReady`** —— 通过 TCP 轮询 `DNSLoopback:{port}`，直到它接受连接或超过 30 秒截止时间。在启动时、任何依赖 DNS 的操作（例如镜像拉取）之前调用。
- **`SystemServices`** —— 返回 rolodex 系统服务的元数据（key、显示名、镜像、端口、单元名），使它与其他系统服务一同出现在状态响应与 UI 中。
- **`Status`** —— 查询 systemd 单元状态以报告 rolodex 是否在运行。

rolodex 容器以 `--net host` 运行，并把 DNS 绑定到 `DNSLoopback`（`127.0.0.2`）的配置端口上（默认 `53`，测试中可通过 `DNSPort` 覆盖）。镜像标签由 system controller 的发布标签推导（`quay.io/town/rolodex:<tag>`），可通过 `ROLODEX_IMAGE` 环境变量覆盖。

**解析模式。** `rolodex.yml` 通过 `Config.ResolutionMode` 显式固定 `resolution.mode`，默认为 **`auto`**（`DefaultResolutionMode`）——即 rolodex 自己的分层回退链：先从根服务器迭代，然后 DoH/DoT，然后 `forwarders:` 列表，最后是 :53 上的公共解析器，并粘住最后一次成功的那一层。该模式被显式写出而不是留给 rolodex 的默认值，这样当上游改变其默认值时 Town OS 的行为不会随之移动。转发相关的集成测试会主动选用 `ResolutionModeForward`，并把 forwarders 指向一个本地桩。

**不要把裸 `recursive` 作为默认值。** 它*没有*回退，而且 rolodex 的迭代解析器（`src/resolver.rs`）对每个域名服务器只发送**一个不重传的 UDP 数据报，截止时间 1500 毫秒**；当当前委派集合中的每个服务器都失败时，`resolve()` 报错，而 `iterative_query` 会把*任何*错误都转换成 SERVFAIL。因此一个丢包就会让一次查询 SERVFAIL；而在过滤或劫持出站 :53 的网络上（酒店、强制门户、某些 ISP），*每一个*外部名称都会 SERVFAIL。`auto` 在网络允许的地方保留递归带来的隐私性，在不允许的地方则降级而不是失败。相关：rolodex 的委派缓存与否定缓存落在 `ce44bb5`，而该提交**不在任何已发布的标签中**——在发布版本包含它之前，recursive 模式会为每一个未缓存的名称与每一次 NXDOMAIN 重新从根开始走一遍（实测：冷公共名称 0.6–1.9 秒，RFC1918 PTR 为 2.7 秒）。

该模式可由运维在运行时通过 `dns_resolution_mode` 设置配置（`auto` | `recursive` | `forward`；由 `ValidateDNSResolutionMode` 校验，因此无法解析的值绝不可能到达 `rolodex.yml` 并把 DNS 弄砸）。`main.go` 在启动时把它读入 `rolodex.Config`；通过 `POST /settings/set` 的更改会运行 `Controller.RefreshDNSResolutionMode`，后者调用 **`Manager.RewriteConfig()`** 并重启 rolodex 单元。`RewriteConfig` 之所以存在，正是因为 `WriteConfig` 拒绝覆盖比 systemcontroller 二进制更新的 `rolodex.yml`（它把那视为被手工编辑过）——而上一次启动写出的文件*总是*满足该条件，因此对于运维发起的更改 `WriteConfig` 会静默地什么也不做。启动时用 `WriteConfig`，运行时更改用 `RewriteConfig`。

### 本地转发器

Town OS 默认写出的 `forwarders:` 列表是 `DefaultForwarders`——公共解析器。在阻断外部 DNS 的网络上（酒店、强制门户、只允许向自家服务器发出 `:53` 的 ISP），这些恰恰就是被丢弃的地址，因此 `auto` 的转发器层——那一层是在根服务器与加密上游都已失败*之后*才到达的，而那正是此种情形——无处可退。而该网络通过 DHCP 下发的解析器确实仍然会应答。

`dns_local_forwarders` 设置（默认 `false`，由 `ValidateBool` 校验）把转发器列表替换为本机自身网络配置所指向的解析器。它**不是一种解析模式**：它改变的是本地那一层*持有哪些*地址，而是否会去查询那一层仍由模式决定——在 `auto` 中它是最后手段，在 `forward` 中它是唯一上游，在 `recursive` 中它根本不被使用。因此打开它绝不能改变模式。

**默认关闭，而这个方向才是要紧的。** 本地解析器会看到这个家庭查询的每一个名称，而那正是从根解析所要避免的事情。这是运维在知情下做出的权衡，而不是机器在网络第一次出问题时替他做的决定。

发现逻辑位于 `src/rolodex/hostdns.go`。`HostResolversFrom` 按顺序读取 `hostResolvConfPaths`——**先**读 `/run/systemd/resolve/resolv.conf`，再读 `/etc/resolv.conf`——胜出的是第一个产出可用地址的文件，而不仅仅是第一个存在的文件。这个顺序是承重的：在使用 resolved 的机器上，`/etc/resolv.conf` 里是那个 stub（`127.0.0.53`），会因是环回地址而被丢弃，因此若发现逻辑止步于第一个*可读*文件，恰恰会在这项功能所服务的那些机器上一无所获。上游文件在容器内部可达，是因为 systemcontroller 单元绑定挂载了 `-v /run/systemd:/run/systemd`；丢掉这个挂载会让发现悄悄退化。环回、未指定、组播与链路本地地址全部被丢弃——转发到 resolved 的 stub 或转发到 rolodex 自己的 `DNSLoopback` 监听器都是查询环路而非上游，而链路本地地址在缺少 `resolv.conf` 行不携带的 zone 时毫无意义。

**一无所获的发现会保留已经配置好的转发器。** `Manager.forwarders()` 依次回退到 `Config.Forwarders`，再到 `DefaultForwarders`，因此打开这个开关绝不会让本地那一层指向空无一物——那会比它本要替换掉的公共默认值严格更糟。

`main.go` 在启动时把该设置读入 `rolodex.Config`（无法解析的存储值被读作关闭——这是安全方向），因此换了网络的机器会在下一次启动时用上新的解析器，无需运维操作。通过 `POST /settings/set` 的更改会运行 `Controller.RefreshDNSLocalForwarders`，与解析模式不同的是，它在标志未变化时**不会**短路返回：在它已经打开的情况下，被发现的地址本身可能已经变了，而重新渲染正是让这一变化到达 rolodex 的途径。`RewriteConfig` 仍会报告字节是否真的变了，因此渲染结果相同就不会产生重启。

`GET /dns/status` **同时**报告 `local_forwarders`（运维要求的）与 `forwarders`（`rolodex.yml` 实际持有的）。它们只在一种情况下不一致——发现没有找到任何可用地址、于是保留了公共默认值——而那正是"开关显示为开、却什么也没变"的那唯一一种情形，因此只显示标志的 UI 会展示一项并未生效的设置。设置界面正因如此才渲染生效中的列表，并在它为空时明确说明。

**测试与开发中 rolodex 镜像按架构拉取** —— make 测试框架拉取宿主机对应架构的 rc 标签 `quay.io/town/rolodex:rc.latest-<arch>`（其中 `<arch>` 是 `uname -m` 的原始形式 `x86_64`/`aarch64`），而**不是**不带架构后缀的普通 `rc.latest`。Town OS 内部的镜像拉取默认走 rc 通道，因此测试框架、开发环境与运行时都跟踪 `rc.latest-<arch>`。rolodex 从每台主机本机推送按架构的标签（rolodex-dns 仓库中的 `make push-rc` / `make push-release`），因此任何架构的测试主机都不需要多架构 manifest 组装；*普通的* `rc.latest`（无架构后缀）是单架构 manifest，在另一种架构上会以 `exec format error` 崩溃重启——只有带后缀的 `rc.latest-<arch>` 可以安全地直接拉取。Makefile 计算 `HOST_ARCH`（规范化为 `x86_64`/`aarch64`）并把 `ROLODEX_IMAGE_TAG` 默认设为 `rc.latest-$(HOST_ARCH)`；`ROLODEX_IMAGE` 由它推导，并经由环境变量注入测试/开发容器。可用 `make ROLODEX_IMAGE_TAG=<tag> ...`（例如用 `latest-$(HOST_ARCH)` 取已发布的 rolodex）或 `ROLODEX_IMAGE` 环境变量覆盖。生产/运行时行为与之一致——除非设置了 `ROLODEX_IMAGE`，否则 systemcontroller 从自己的发布标签推导（并通过 `defaultVersionTag()` 回退到 `rc.latest-<arch>`）；测试与开发框架总是会设置它。开发容器中烘焙的 rolodex 单元（`integration/testdata/town-os-system--rolodex.service`）使用 `@ROLODEX_IMAGE@` 占位符，在镜像构建时经由 `integration/testdata/Containerfile.dev` 中的 `ROLODEX_IMAGE` 构建参数替换（该参数为空时构建失败），因此烘焙的单元始终与测试框架加载的镜像一致。

### 网络 TLD、双栖与分离视界解析

每个网络都拥有一个 TLD，在 rolodex 中注册为一个 `home_domain` 即该 TLD 的网络作用域
（`rolodex.EnsureNetworkScope`，由 `controller_networks_reconcile.go` 中的
`applyNetworkTransport` 调用）。拥有该 TLD 正是**划分**它的机制：rolodex 会对加入
*其他*作用域的任何 WireGuard peer 隐藏本作用域的 TLD。默认/home 网络
（`account.DefaultNetworkName`，TLD 取自 `dns_tld` 设置，默认 `home`）以**仅 DNS**
作用域的形式拥有 `home.`——它不获得 WireGuard 接口、overlay 子网或 peer 关联，因此
永远不会有源 IP 被绑定到 home 作用域。`.home` 因此只在局域网内有效、对每一个
WireGuard peer 都隐藏，同时在局域网上完全可解析。

**双栖。** 安装进非默认网络的包会被发布两次
（`registerScopedPackageDNS`）：

- 一条位于本机 **overlay IP** 的、该网络 TLD 之下的**作用域** A 记录——按源 IP 提供给
  WireGuard overlay peer（`AddScopedRecord`）；以及
- 同一个 FQDN 位于本机 **局域网 IP** 的一条**全局** A 记录
  （`RegisterPackageDNS`）——提供给环回/局域网客户端。

每一侧都得到一个它真正能路由到的地址。该网络 TLD 不会发布全局权威区域：裸的全局 A
记录在局域网上无需区域即可解析，而 rolodex 的 **局域网→归属作用域回退**（rolodex-dns
解析步骤 5）会把作用域所拥有的 TLD 视为对局域网来源具有权威性——因此该网络 TLD 之下
未匹配到的名称会从局域网侧得到一个权威 NXDOMAIN，而不是把这个私有 TLD 泄漏到上游。
默认网络的包只留在全局 home 区域中（`registerPackageDNS`）；非默认网络的包绝不能出现
在那里（这正是最初那个"解析成 `.home`"的缺陷）。

**分离视界小结。** 局域网客户端（无 WireGuard）能解析**每一个**网络的 TLD（`.home`，
以及每个 WireGuard 网络的 TLD）加上公共互联网。加入某一个网络的 WireGuard peer **只能**
解析那个网络的 TLD 加上公共互联网——同级网络的 TLD 与 `.home` 都返回 NXDOMAIN。局域网
视图从不被划分；被划分的只有 overlay peer。`RebuildNetworkDNS`（`reconcile.go`，启动时
调用）为每个非默认网络的包重新注册面向局域网的全局记录，因此已安装的包在重启之后仍能
在局域网上解析；作用域记录则在 rolodex 中独立留存。启动时的网络 reconcile 会被传入
rolodex 客户端，因此即便是冷启动，home 作用域（以及每个网络作用域）也会被建立起来。

### 包的 FQDN 是同一个字符串 —— A 记录、叶子证书 SAN、TLSA 属主、ingress vhost

**包的 DNS 名称始终由其*安装网络的* TLD 推导，绝不来自全局 `dns_tld` 设置。**
`packageFQDN(repo, name, tld)`（`src/svc/systemcontroller/controller_tls.go`）是唯一的
事实来源，而 TLD 来自 `networkTLDValue(nm, settingsMgr, network)`（它只在默认网络时才
回退到 `dns_tld`）。有四样东西必须以完全相同的方式为一个包命名，其中任何一处不一致都
会悄无声息地破坏服务：

1. 它的 **A 记录**，2. 它的**叶子证书 SAN**，3. 它的 **DANE TLSA 属主**，
以及 4. 它在共享 `:443` 上的 **ingress vhost**。

**三个发布方现在都经由同一个校验器来组装这个名称。** 一个包、一个 page、一个对象存储
分区，各自都会在某个网络的 TLD 之下拿到一个名称，而过去它们各自组装各自的——彼此对
"什么才算合法名称"并不一致。`gfehFQDN` 会规范化标签、按严格的 LDH 规则校验每一个以点
分隔的分量，并拒绝限定之后超过 253 字符的名称；`packageFQDN` 是裸拼接，两项检查都没有；
`pageFQDN` 除了去除首尾空白之外什么也不检查。`qualifyPublishedName`
（`src/svc/systemcontroller/published_name.go`）现在是唯一的组装者，把 gfeh 的规则施加
于三者；而 `validatePublishedName` 是它不做限定的那一半，供那些必须被检查、却不该被组装
的名称使用。未通过的名称会被**丢弃** —— 每个收集器本来就会跳过空的 FQDN，因此它贡献的
是"没有记录、没有路由、没有证书、没有目录"，而不是给这四样东西各一个坏掉的 —— 并且这条
拒绝记录在 **Error** 级别，因为 `LOG_LEVEL` 默认就是 `error`，而一个悄无声息地不再解析的
服务，绝不能只有把日志级别调高才能被发现。

**page 的 domain 在 API 处就被校验，而不只是在组装时。** 对 page 而言这个名称还是第
*五*样东西：它在磁盘上的子卷与 webroot 符号链接，因为 pages 的 Caddy 以 `/srv/<host>`
为根。`ValidatePageDomain` 在 `POST /pages/create` 与 `POST /pages/update` 两处都会运行，
返回 400。要紧的是 update 这条路由：create 是被顺带覆盖到的——`CreateFilesystem` 会运行
`storage.ValidateFilesystemName`，handler 在抵达符号链接那段代码之前就已回滚；而
`migratePageDir` 只是把 `RenameFilesystem` 的失败记进日志，然后照样继续执行
`RemovePageSymlink` / `EnsurePageSymlink`。

微妙之处在于：**公共 FQDN 豁免于限定，但不豁免于校验**。`isPublicFQDN` 会把任何含点且不
以该 TLD 结尾的名称读作运营者自己的域名，按原样经 ACME 提供服务——这对 `blog.example.com`
是正确的，同时也正是 `../escape.example.com`、`site.example.com/../../etc` 与
`site.example.com other.example.com` 未经检查就抵达 `filepath.Join` 和 Caddyfile 的途径。
"那是运营者的域名"是不该把它组装到本机 TLD 之下的理由；它从来都不是不去检查它的理由。

为防止它们漂移，FQDN 只被计算**一次**——在 `applyPackageTLS` 中，与签发叶子证书同一行——
并作为 `PackageNetworkState.FQDN` 持久化（按包的网络状态 JSON 中的 `fqdn`）。ingress 路由
构建器（`collectPackageIngressSites`）读取该字段而不是重新拼装名称，因此 vhost 在构造上
就是证书有效的那个名称。`reconcileWriteNetworkState` **从其调用方**取得 TLD
（`reconcilePackage`，它已从安装网络解析出该值）；它绝不能自行调用 `reconcileDNSTLD`。
那样做过去是一个真实的缺陷：每次启动都会以 SAN `<pkg>.<repo>.home` 重新签发一个
`fart` 网络包的叶子证书，覆盖掉正确的 `.fart` SAN，同时 ingress 渲染出一个无人拨打的
`<pkg>.<repo>.home` vhost——于是该包在局域网上可以解析，却从未被真正提供服务。空的
`fqdn`（升级前的状态文件，或非 HTTP 的包）会回退到全局 TLD，并在下一次 reconcile 时自愈。

**ingress 与网络接口无关，也不需要按网络绑定。** 它以 `-p 443:443` / `-p 80:80` 发布且
不指定宿主机 IP（即 `0.0.0.0`，因此局域网 + WireGuard + 环回都能到达），其 Caddyfile 中
**没有 `bind` 指令**，因此 Caddy 在所有接口上监听，并纯粹依据 **SNI/Host** 选择 vhost。
后端通过共享的 `town-os-ingress` podman 网络上的容器名访问，而每一个由 HTTP 前置的包，
无论其 WireGuard 网络是什么，都会加入该网络。因此局域网客户端与 overlay peer 命中同一个
监听器、SNI 选中同一个 vhost、拿到同一张本地 CA 叶子证书，并被代理到同一个容器。没有
任何东西把监听套接字绑定到 overlay IP——`BindOverlayAddress` 是 rolodex 的 *DNS 作用域
关联*，不是套接字绑定。不要给 ingress 添加 `bind` 指令或按网络的监听器。

包的叶子证书还会把本机在该网络上的 **overlay IP** 作为 SAN 带上
（`networkOverlayIPValue`），因此 peer 可以用 WireGuard 裸地址访问该包
（`https://10.65.0.1`），而不只是靠名称。对默认网络（它没有 WireGuard 传输层）该值为空，
这使得默认网络的叶子证书不会在每次 reconcile 时被搅动。

网络包的 DANE TLSA 与其 A 记录一样是**双栖的**：`RebuildNetworkDNS` 注册一个全局 pin
（经由局域网→归属作用域回退提供给局域网来源）*以及*一个作用域 pin（提供给 overlay
peer，它们的查询永远看不到全局记录）。仅靠安装流程只会写出作用域那一半，而且跨重启时
两半都不会被重新发布。

### Pages 同样是按网络限定作用域的

page 携带一个 `network`（`PageSite.Network` 列；`""` 表示默认/home 网络，与包的
`Installer.LoadNetwork` 是同一约定），并获得**与包完全相同的待遇**：它的名称来自该网络
的 TLD，它是双栖的（作用域 overlay 记录 + 全局局域网记录），它的叶子证书携带该网络的
FQDN 加上本机的 overlay IP，它的 DANE TLSA 在该网络 TLD 之下被固定（全局 + 作用域），
并且它对*其他每一个*网络的 peer 都是隐藏的。`pageFQDN`（`pages_tls.go`）是
`packageFQDN` 在 page 一侧的孪生体，`pageNetworkTLD` 则对应 `networkTLDValue`。

page 特有的一处曲折：page 的 FQDN **同时还命名着它在磁盘上的 btrfs
子卷与它的 webroot 符号链接**（pages 的 Caddy 以 `/srv/<host>` 为根）。因此 FQDN 不只是
一个标签——弄错它，内容就会从 ingress 所服务的那个名称底下移走。三条推论：

- `reconcilePages` 用 `pageFQDN` 构建它的 `valid` 集合，因为该集合驱动
  `pruneStalePageSymlinks`——在那里把一个 `fart` 网络的 page 命名为 `blog.home`，既会
  错过它真正的 `blog.fart` 目录，*又会*把仍在使用的符号链接剪掉。
- 改变 page 的**网络**会重命名它的子卷/符号链接（`migratePageDir`），这与 `dns_tld`
  变更对默认网络 page 所做的完全一样。
- `migratePageDirsForTLD`（`dns_tld` 变更的处理器）**跳过非默认网络的 page**——它们并非
  在全局 TLD 之下命名，因此重命名它们会弄坏一个本来正常工作的 page。

pages 仍由 ingress 之后那个唯一共享的 `town-os-system--pages` 容器提供服务；网络只是
命名/DNS/证书层面的关切，不涉及按网络的容器或 podman 管路。

### DNS API

- `GET /dns/status`（需要鉴权）—— 返回 DNS 状态，包括启用标志、运行状态、TLD、记录数量、`local_forwarders`（转发器列表是否取自宿主机自身的解析器），以及 `forwarders`（`rolodex.yml` 实际持有的地址——参见 [Local forwarders](#本地转发器)）。
- `GET /dns/records`（需要鉴权）—— 列出所有 DNS 记录。
- `POST /dns/records/add`（需要管理员）—— 添加 DNS 记录。接受名称、记录类型、值与 TTL。
- `POST /dns/records/remove`（需要管理员）—— 按名称与类型移除 DNS 记录。
- `GET /dns/tld`（需要鉴权）—— 获取当前顶级域。
- `POST /dns/tld`（需要管理员）—— 设置 TLD。更改现有 TLD 并重新注册所有已安装的包。
- `POST /dns/setup`（需要管理员）—— 初始化 DNS 并注册所有已安装的包。
- `GET /dns/rbl`（需要鉴权）—— 获取 RBL（Realtime Blackhole List，反向 IP）配置：全局启用标志、各提供方区域及其**已解析为实际生效值**的拒绝码、列表级的 `refusal_cooldown_secs`，以及 `rotated_out`（当前因拒绝查询而被轮换出去的提供方，附带拒绝码与剩余秒数）。参见 [Refusal codes](#拒绝码提供方说别再问了不等于说这个被列入了)。
- `POST /dns/rbl`（需要管理员）—— 替换 RBL 配置。接受一个启用标志、一个列表级的 `refusal_cooldown_secs`，以及一组 `{zone, enabled, refusal_codes, refusal_cooldown_secs}` 提供方。区域会被校验为完全限定主机名，并转小写、去空白、去重；拒绝码由 `ValidateRefusalCodes` 校验（IPv4 地址或 `address/prefix`，按前缀掩码，`"none"` 只能单独出现，不允许重复）。
- `GET /dns/dnsbl`（需要鉴权）—— 获取 DNSBL（域名黑名单，正向名称）配置，形状与 `/dns/rbl` 相同。
- `POST /dns/dnsbl`（需要管理员）—— 替换 DNSBL 配置（形状与校验同 `/dns/rbl`；其拒绝冷却时间与 RBL 的相互独立）。
- `GET /dns/rbl/local`（需要鉴权）—— 列出本地 RBL 黑名单条目（`{name, reason}`）。
- `POST /dns/rbl/local/add`（需要管理员）—— 添加本地 RBL 条目。接受一个名称（域名或 IP）与可选原因。名称会被校验（域名或 IP）、转小写并去空白。
- `POST /dns/rbl/local/remove`（需要管理员）—— 按名称移除本地 RBL 条目。
- `GET /dns/dnsbl/allowlist`（需要鉴权）—— 列出 DNSBL 白名单条目（`{name, reason}`）。
- `POST /dns/dnsbl/allowlist/add`（需要管理员）—— 把某个名称从基于名称的黑名单检查中豁免。接受一个名称与可选原因。名称会被转小写、去空白，并且**只校验为域名**——IP 字面量会被拒绝（`ValidateDnsblAllowlistName`），因为白名单匹配的是名称及其子域，永远不可能匹配到一个地址。
- `POST /dns/dnsbl/allowlist/remove`（需要管理员）—— 按名称移除白名单条目。名称会被规范化但不会重新校验，因此早于某次校验规则变更的条目仍然可以被移除。
- `GET /dns/services`（需要鉴权）—— 列出已安装的包服务及其发布状态（是否在 DNS 区域中）（`{repo, name, version, fqdn, domains, published}`），按 repo/name 去重。
- `POST /dns/services/set`（需要管理员）—— 在 DNS 区域中发布或取消发布某个包服务。接受 `{repo, name, published}`。持久化该选择并立即注册/注销记录。

DNS 的只读端点（`/dns/status`、`/dns/records`、`/dns/rbl/local`、`/dns/dnsbl/allowlist`、`/dns/services`、`GET /dns/tld`、`GET /dns/rbl`、`GET /dns/dnsbl`）被排除在审计日志之外。白名单的*写*操作会被审计（把一个名称从所有黑名单中豁免是一项需要问责的变更）；与它们所对应的黑名单写操作一样，它们在 `account.RouteActions` 中没有具名动作——由路径本身标识它们。

### RBL / DNSBL 黑名单

Rolodex（0.2.4+）提供三种互补的垃圾/恶意/广告拦截机制，外加（0.4.3+）一种撤销机制与一种"不相信拒绝了查询的提供方"的机制，全部通过 DNS API 与 `rolodex.Client` 封装暴露（`SetRblConfig`/`GetRblConfig`、`SetDnsblConfig`/`GetDnsblConfig`、`AddLocalRblEntry`/`RemoveLocalRblEntry`/`ListLocalRblEntries`、`AddDnsblAllowlistEntry`/`RemoveDnsblAllowlistEntry`/`ListDnsblAllowlistEntries`）。全部由 **rolodex 按需查询**——Town OS 从不下载、解析或预缓存黑名单订阅源。

注意该封装的两个 `Set*` 方法把列表级的拒绝冷却时间作为末位参数（`SetRblConfig(ctx, enabled, providers, refusalCooldownSecs)`）；它们映射到上游的 `Set*ConfigWithRefusalCooldown`，因为上游那些保持参数个数不变的写法是为了外部 API 兼容性而存在的，而内部封装并不需要这一点。

- **RBL**（Realtime Blackhole List）—— 反向 IP 黑名单区域，按需以反转后的 IP 对某个区域发起查询（例如 `zen.spamhaus.org`）。用于检查反向 DNS 查询中出现的 IP。通过 `/dns/rbl` 配置为一组 `{zone, enabled, refusal_codes, refusal_cooldown_secs}` 提供方，外加一个全局启用标志与一个列表级的 `refusal_cooldown_secs`。
- **DNSBL**（域名黑名单）—— 域名黑名单区域，按需通过把被查询的域名前置到该区域来发起查询（例如 `googleadservices.com` + `dbl.spamhaus.org`）。DNSBL 的命中优先于转发/迭代得到的答案。通过 `/dns/dnsbl` 配置，形状与 RBL 相同，并有自己独立的冷却时间。
- **本地 RBL 条目** —— 一份由数据库支撑的名称/IP 列表，通过 `/dns/rbl/local*` 手动管理，在外部提供方之前被检查。**域名**类型的本地条目会以 `NXDOMAIN` 阻断该域名的正向 A/AAAA 查询，并立即生效（rolodex 在添加时更新内存缓存）。
- **DNSBL 白名单**（rolodex 0.4.3+）—— 运维应对第三方订阅源误报的逃生舱口，通过 `/dns/dnsbl/allowlist*` 管理。一个条目覆盖该名称**以及它之下的每一个名称**，因此把 `vendor.example` 加入白名单也会豁免 `cdn.vendor.example`。它会**短路整个基于名称的检查**，优先于已配置的 DNSBL 提供方以及任何匹配的本地 RBL 条目，并且它在提供方查询*之前*运行，因此被豁免的名称永远不会发出那次查询。同样由数据库支撑并带内存缓存，因此立即生效。

  没有它，面对一个把家庭所需名称列入黑名单的订阅源，唯一的补救办法就是禁用整个提供方。请注意它与本地黑名单的不对称：白名单条目**只能是名称**，绝不能是 IP，因为它所短路的正是基于名称的那次检查。基于 IP 的 RBL 路径不受它影响。

  **版本下限：** 较老的 rolodex 会以 gRPC `Unimplemented` 应答这三个白名单 RPC，表现为 500。`make test` 与 mock 的集成测试都发现不了这一点——`TestRolodexDnsblAllowlistRoundtripReal` 才是证明所固定镜像足够新的那个测试。

#### 拒绝码：提供方说"别再问了"不等于说"这个被列入了"

DNSxL 对"命中黑名单"与"对查询者的抱怨"返回的是**同一种记录**——`127.0.0.0/8` 之下的一条 `A`——因此区分二者的只有地址本身。`127.0.0.2` 表示该名称被列入；`127.255.255.254` 表示该查询是经由公共解析器到达的，而 `127.255.255.255` 表示查询者已超出限额。若把第二类读作命中，那么对该提供方检查过的**每一个**名称都会变成 `NXDOMAIN`：黑名单不再是黑名单，而成了一次故障。Spamhaus 公布的免费使用限额，家用机器可能在毫无察觉的情况下越过，而越过时的症状就是整个网络一片漆黑——那看上去像 DNS 坏了，而不像限流。

Rolodex 能识别这些码，并在遇到拒绝时**把该提供方从查询轮换中移出一段冷却时间**，而不是相信它。Town OS 把两半都暴露出来：

- **`refusal_codes`**，按提供方配置，两个列表都支持。每一项是一个 IPv4 地址或 `address/prefix`——之所以支持前缀，是因为提供方公布的是整段范围，而 Spamhaus 把整个 `127.255.255.0/24` 保留给错误码并会随时间往里添加新码，因此把今天的三个枚举出来，会导致明天的第四个被悄悄读作命中。
- **`refusal_cooldown_secs`**，按提供方与按列表配置。提供方的 `0` 表示沿用列表值；列表的 `0` 表示使用 rolodex 内置的默认值（3600）。
- **`rotated_out`**，出现在 `GET` 中，报告当前哪些提供方没有被询问、各自以什么码拒绝、以及剩余多少秒。这是运维可见的那一半：没有它，某个黑名单不再被查询的唯一信号，就是它不再拦截东西了。

**`ValidateRefusalCodes`（`controller_dns_validate.go`）精确镜像 rolodex 的 `resolve_refusal_codes`**，因为该列表是被原样透传的，而对某一项的含义各执一词会比根本不校验更糟。三种情形：

- **为空** ⇒ rolodex 代入它内置的集合，因此在这一切存在之前写下的配置无需编辑就能获得安全的读法；
- **恰好是 `"none"`** ⇒ 关闭检测，供那些真实命中码与内置码冲突的私有黑名单使用；
- **其他任何值** ⇒ 恰好就是这些码，且刻意**不**并入内置码。

`"none"` 与真实码混用会被拒绝——一个既要关闭检测又指名了要检测哪些码的列表，没有可选的读法。码会按其前缀掩码，且 **`/32` 渲染为裸地址**，与 rolodex 的 `Display` 一致：读回来与刚提交的不一样的码，看上去就像机器改写了运维的输入。

**`GET` 报告的是已解析的码**，因此一个没有指名任何码的提供方读回来会带着内置集合——这正是要点，因为运维必须能看到机器实际在拿什么做匹配。这也意味着**客户端绝不能在下一次保存时把它原样回传**：那样做会把今天的列表冻结进存储的配置，此后 rolodex 新增的码就会开始被读作命中——正是这一机制要防止的失败，只不过在上一层被重新引入。`BlocklistsTab.jsx` 中的 `toWire` 会把已解析的内置集合收拢回一个缺省字段，而 UI 保留一份内置列表的副本（`BUILTIN_REFUSAL_CODES`）只为一个用途：决定设置对话框打开时选中哪个单选项。若那份副本漂移，对话框会打开在 "Custom" 并预填当前生效的码——那是外观上的错误默认值，而不是错误的配置，因为除非运维保存，否则什么也不会改变。

**版本下限：** 早于拒绝码处理的 rolodex 会接受这些字段——proto3 忽略未知字段——却什么也不存储。mock 测试无法把这与成功区分开，因为 mock 会把递给它的东西原样回传。`TestRolodexRblRefusalCodesRoundtripReal` 及其 DNSBL 孪生测试断言：**空的**已配置列表读回来必须是*已解析*的，而这正是老镜像通不过的断言。

**不存在订阅源摄取/预缓存**：提供方区域就是配置的单位；UI 提供一份精选的知名 DNSBL/RBL 区域列表作为一键快捷添加，但用户可以添加任何区域。提供方区域的写入会替换整份配置（经校验、转小写、去重）。

**快捷添加列表是一种背书，并据此标准精选**（`ui/src/routes/dns/BlocklistsTab.jsx` 中的 `DNSBL_SUGGESTIONS` / `RBL_SUGGESTIONS`）。一个区域只有在家用机器开箱即可使用时才应出现在那里：仍在运营、免费，并且无需注册步骤即可应答一个自递归的解析器。当前的 DNSBL 有 Spamhaus DBL、SURBL、URIBL、NordSpam DBL、Spam Eating Monkey；RBL 有 Spamhaus ZEN、SpamCop、PSBL。

有三个被刻意**排除在外**，而 `TestBlocklistsTab` 的"不提供已停运或需注册的区域"用例保证它们一直如此：`dnsbl.sorbs.net` 已于 2024-06-05 停运且其区域被清空，因此它是一个读起来像保护的永久空操作；`b.barracudacentral.org` 要求先注册查询方 IP，未注册的机器可能应答一阵子然后被切断；UCEPROTECT 的 2/3 级会列出整个 ASN，因此一个坏邻居就能封掉一整家 ISP。这三者都是*静默*失败——运维看到一个已配置的区域，就假定它在工作。

另请注意，RBL（反向 IP）区域只在反向 DNS 查询中出现 IP 时才被查询，而普通浏览几乎不产生这类查询。真正影响浏览的是 DNSBL（域名）区域，而它们是针对邮件中的垃圾 URL 调优的，而非针对广告或追踪器——广告/追踪器拦截属于订阅源的领域，而那[被刻意排除在范围之外](#rbl--dnsbl-黑名单)。

### 按服务的 DNS 发布

发布是选择退出制：每个已安装的包服务都会被发布到 DNS 区域中，除非它的 `repo/name` 键出现在 `dns_excluded_services` 设置里（一个 JSON 数组）。`/dns/services/set` 切换其成员身份并立即注册/注销记录；`RebuildDNS` 与 `ReconcileDNS` 会过滤被排除的服务（经由 `filterExcludedDNSInfo` + `loadDNSExcludedServices`），因此该选择在重启与 reconcile 之后仍然有效。未发布的服务照常运行，但无法按名称解析。

### DNS 管理 UI

DNS 管理界面在四个可深链的子标签页（`?tab=`）之上显示 DNS 状态（启用、运行中、TLD、记录数量）：

- **Records** —— DNS 记录表，配有用于添加记录（类型：A、AAAA、CNAME、MX、TXT、SRV、PTR）、移除记录、更改 TLD 与初始化 DNS 的对话框。
- **Blocklists** —— DNSBL 与 RBL 的提供方区域区块（全局启用开关、按区域的启用/移除、按区域的拒绝码设置、建议区域快捷添加、自定义区域添加——全部为按需查询），外加一张手动本地条目表（添加/移除）。每个区块的开头会列出当前因拒绝查询而被退避的提供方（如果有的话）。没有订阅源，没有"应用"按钮，什么也不缓存。
- **Allow Lists**（`?tab=allowlists`，`ui/src/routes/dns/AllowListsTab.jsx`）—— DNSBL 白名单：一张带原因的豁免域名表，以及添加与移除。读操作是 `requireAuth`，因此该标签页不限管理员；添加/移除控件仅限管理员。它是一个平级标签页而非 Blocklists 上的一张卡片，因为当某个东西无法访问时，运维是按名称去找豁免项的，而不是在滚动浏览提供方区域时顺便发现它。
- **Services** —— 已安装的包服务，配有发布开关（在 DNS 区域中发布/取消发布）。

## 状态端点

`GET /status/ping`（公开）返回系统状态，包括：文件系统数量（user、installed、uninstalled）、仓库与包的数量、已安装包数量、账户与管理员数量、服务单元数量（总数、活动、失败）、系统服务单元数量（总数、活动、失败）、近期审计错误（最近 5 分钟）、初始化状态（`needs_setup` 仅在不存在处于启用状态的管理员账户时为真；只要存在管理员，无论会话状态如何都会显示登录页）、外部 IP（每小时从 ipinfo.io 获取）、内部 IP（第一个非环回 IPv4 地址）、磁盘使用统计、升级可用性、服务器 UTC 时区偏移的分钟数、当前语言环境、`proton_enabled`（本次构建是否带 `proton` 构建标签）、`boot_id`，以及在提供了有效令牌时的已认证用户名。

服务单元数量被拆为两个字段：`units` 只统计包服务单元（匹配 `town-os-package--*` 的），而 `system_services` 统计系统服务单元（匹配 `town-os-system--*` 的）。已卸载包遗留的 systemd 单元会被排除在包计数之外。已安装包列表通过由每个包身份构造出的预期单元名，与发现到的 systemd 单元交叉比对。

该处理器只列举一次账户（用于 `needs_setup`、总数与管理员计数），并且卷计数使用 `FilesystemNames` 而非 `ListFilesystems`——后者每个子卷都要执行一次 `btrfs qgroup show` 加一次 rootid 查找，在约 30 个子卷的规模下，为了一个 ping 根本不会读取的配额，要花掉这次 ping 大约一秒的延迟预算。

来自非 localhost 来源的未认证请求会收到一个最小响应，只含 `status`、`needs_setup` 与 `boot_id`。`boot_id` 即便在那里也会携带，因为刷新流程会跨控制器重启轮询 ping，而在此期间浏览器会短暂地没有令牌；它是每个进程随机生成的 UUID，不泄露任何系统信息。已认证请求以及所有来自 localhost 的请求都会收到包含上述全部字段的完整响应，另加 `repository_errors`（一个仓库名到错误字符串的 map，跟踪按仓库的刷新失败）。

当控制器仍在启动过程中时，该路径改由启动桩提供服务，并返回 **503** 与 `{booting, step, done, error, boot_id}`——参见 [Boot Status and Refresh](#启动状态与刷新)。

### 外部 IP 轮询

system controller 从 `https://ipinfo.io/json` 获取服务器的公网（外部）IP 地址。该轮询器在 HTTP 处理器创建时（`NewHandler`）以及 Unix socket 服务器启动时自动开启。它在启动时立即获取一次 IP，随后每 1 小时轮询一次。每次获取有 10 秒的 HTTP 超时。结果被缓存在一个原子值中，并作为 `external_ip` 包含在已认证的 ping 响应里。获取失败以 debug 级别记录，不影响系统其余部分；当尚未获取到任何 IP 时，该字段会从响应中省略。

## 监控

一套集成的 Prometheus + Node Exporter 监控栈提供系统指标。`monitoring.Manager` 把这套栈作为由 systemd 监管、带 `Restart=always` 的 podman 容器（系统服务）来管理，使用 `town-os-system--` 命名前缀。仪表盘前端可通过 `monitoring_backend` 设置配置。

### 监控端口

端口 **5308** 是专用的监控仪表盘端口（`TOWN_OS_MONITORING_PORT` 可迁移它；两个环回端口同理，分别由 `TOWN_OS_PROMETHEUS_PORT` 与 `TOWN_OS_NODE_EXPORTER_PORT` 控制——参见 [System-service host ports](#系统服务的宿主机端口)）。这些端口以单个 `monitoring.Ports` 值传达给三个服务，其空字段由 `withDefaults()` 填充，因此默认值逻辑只存在于一处。当前生效的后端决定了在仪表盘端口上监听的是什么：

- **uPlot 模式**（默认）：一个 socat 转发器（`socat TCP-LISTEN:5308,fork,reuseaddr TCP:localhost:9090`）把 Prometheus 的 HTTP API 暴露在 5308 端口上。React UI 直接查询 Prometheus 的 `/api/v1/query_range` 并用 uPlot 渲染图表。
- **Grafana 模式**：Grafana 直接监听 5308 端口（经由 podman 端口映射）。React UI 内嵌一个 Grafana iframe。

**不存在**经由 systemcontroller（5309 端口）的反向代理。浏览器就所有监控数据直接与 5308 端口通信。

### 监控后端设置

`monitoring_backend` 系统设置控制使用哪个仪表盘前端：

- `"uplot"`（默认）—— 在 React UI 中用 uPlot（约 35 KB）渲染的轻量内置图表。经由 socat 转发器在 5308 端口查询 Prometheus。不会拉取或启动 Grafana，首次启动可省下约 771 MB。
- `"grafana"` —— 完整的 Grafana 仪表盘。Grafana 容器镜像会被拉取并在 5308 端口启动。预置了一个 Prometheus 数据源以及注册表中的每一个仪表盘。

更改该设置会立即生效：切换到 `"grafana"` 会拉取 Grafana 镜像并启动容器（同时停止 socat 转发器）；切换到 `"uplot"` 会停止 Grafana 并启动 socat 转发器。

### 监控容器

- **Node Exporter**（`quay.io/prometheus/node-exporter:latest`，宿主机端口 9100）—— 采集宿主机系统指标。以宿主机 PID 命名空间、`SYS_TIME` 能力，以及把宿主机根文件系统只读绑定挂载到 `/host` 的方式运行。其 systemd 单元传入 `--collector.diskstats.device-exclude=^(ram|fd)\d+$`（即 `monitoring.DiskstatsDeviceExclude` 常量）以覆盖 node_exporter 的上游默认值（`^(ram|loop|fd|(h|s|v|xv)d[a-z]|nvme\d+n\d+p)\d+$`），后者会过滤掉分区（`sda3`、`nvme0n1p3`）与 loop 设备——而那恰恰就是 `monitoring.BtrfsDevices` 为支撑 `/town-os` 的 btrfs 文件系统所报告的设备形态。没有这项覆盖，Disk I/O 仪表盘的查询会静默地返回零个序列，面板渲染为空。除非你同时把 Disk I/O 查询迁离 `node_disk_*`，否则不要移除或放宽该标志。回归覆盖：`TestNodeExporterUnitConfigDiskstatsExcludeAllowsRealDevices` 固定该标志与正则，而 `TestMonitoringNodeExporterEmitsDiskMetricsForFilteredDevices` 启动一个真实的 node_exporter 容器，确认它至少为一个被上游默认值排除的设备发出 `node_disk_read_bytes_total`。
- **Prometheus**（`quay.io/prometheus/prometheus:latest`，宿主机端口 9090）—— 以 15 秒间隔抓取 Node Exporter、它自身、rolodex（job `rolodex`）与 system controller（job `systemcontroller`，参见 [System Controller Metrics](#system-controller-指标)）。那两个可选 job 在其地址未设置时会被省略，而不是指向一个猜测的默认值，因为无人配置过的目标会永久处于 down 状态，读起来像一个坏掉的服务而非一个缺席的服务。数据以 30 天保留期存放在持久化数据目录中。配置与数据卷从监控数据目录绑定挂载。该 systemd 单元包含 `ExecStartPre` 的 mkdir 指令，以便在启动时预先创建卷目录。
- **Grafana**（`docker.io/grafana/grafana:latest`，宿主机端口 5308）—— 可选的仪表盘 UI，仅当 `monitoring_backend` 为 `"grafana"` 时启动。使用浅色主题（`GF_USERS_DEFAULT_THEME=light`）。匿名浏览以 Viewer 角色启用，允许 iframe 内嵌。该 systemd 单元包含 `ExecStartPre` 的 mkdir 指令以在启动时预先创建卷目录。预置了一个 Prometheus 数据源以及 [Dashboards](#仪表盘) 中描述的那些仪表盘；它们是如何被放到位的，参见 [Dashboard Provisioning](#仪表盘置备)。
- **Socat 转发器** —— 即 uPlot 形态下的 `monitoring-ui` 单元（`town-os-system--monitoring-ui.service`），仅当 `monitoring_backend` 为 `"uplot"` 时启动。把 5308 端口转发到 9090 端口的 Prometheus。它使用的是与 Grafana *相同的单元 key*，而不是第二个单元：两者是同一个服务的两种可选实体，正因如此，切换后端才是一次单元重写加重启，而不是一对可能让两者都在运行或都不运行的启停调用。

### 仪表盘

共有两个仪表盘，而且**两种后端都从同一批查询渲染出同样的这两个**。它们之所以分开而不是并成一长页，是因为它们回答不同的问题：System 是运维在机器发卡时看的，DNS 是他们在某个名称无法解析时打开的。把八个 DNS 面板折进 overview，只会把那四个主机面板——人们打开它的理由——埋掉。

**System**（Grafana uid `town-os-overview`，"Town OS Overview"）—— 四个面板：

1. **Disk I/O (/town-os)** —— 在支撑该 btrfs 文件系统的各块设备上求和的读/写吞吐，因此无论文件系统跨越多少设备，面板都只显示一条 Read 线与一条 Write 线。设备正则由 `monitoring.BtrfsDevices` 代入；空列表会解析为 `NoBtrfsDevicesSentinel`，它匹配不到任何东西，因此面板渲染为空，而不是悄悄把宿主机上每一块磁盘加总起来。
2. **Network (External)** —— 每个物理设备的收/发比特每秒（排除 `lo`、veth、podman、cni、tailscale、网桥与 docker），并与 `node_network_up == 1` 联接，因此曾经存在但现已 down 的接口不会拉出一条条平直的零线把图例挤出屏幕。
3. **CPU Usage** —— 按模式（user、system、iowait、irq、softirq、steal、nice）堆叠，并叠加一条 Total 线，0–100%。
4. **Memory Usage** —— 总量、已用、可用。

**DNS**（Grafana uid `town-os-dns`，"Town OS DNS"）—— 基于 `rolodex` 抓取 job 的八个面板：

1. **DNS Queries by Response Code** —— `rate(rolodex_dns_queries_total)` 按 `rcode` 求和，堆叠。这个拆分本身就是面板，而不是一个下钻视图，因为单纯的查询计数无法区分繁忙的解析器与对一切都 SERVFAIL 的解析器——它们是同一条线。
2. **Query Latency** —— 由 `rolodex_dns_query_duration_seconds_bucket` 得出的 p50/p95/p99。这些桶在 `histogram_quantile` *之前*先按 `le` 求和，因为原始序列带有 `proto` 标签，不聚合就分位会画出每种传输方式一条线，而不是全机范围的延迟。
3. **Answers by Source** —— 由哪个解析阶段作答（cache、local、scoped，或某个上游层级），堆叠。这个面板说明的是本机在自问自答，还是在转发。
4. **Cache Hit Ratio** —— 命中加否定命中占全部查找的比例，0–100%。被缓存的 NXDOMAIN 算作命中：它与一次正向命中同样省下了一次上游往返。分母刻意不做钳制，因此空闲的机器会让线断开，而不是为一个从没被问过任何东西的缓存画出一个自信的 0%。
5. **Cache Entries** —— 正向、否定与黑名单缓存的条目数。
6. **Blocklist Activity** —— 按种类的拦截数、被白名单豁免数，以及**被拒绝数**。拒绝与拦截共处一个面板是刻意的：提供方回答"别再问了"而不是"这个被列入了"，正是悄悄把黑名单变成一次故障的原因（[Refusal codes](#拒绝码提供方说别再问了不等于说这个被列入了)），而它只有与被它取代的拦截率并列时才显得反常。
7. **Upstream Tier Outcomes** —— 每一层的成功与失败次数，以及耗尽了所有层级的查询数。
8. **DNS Traffic** —— 线上收/发字节数。

每一条 DNS 查询都带有由 `monitoring.RolodexJobName` 构建的 `{job="rolodex"}` 选择器，因此抓取配置发出的标签与仪表盘所选择的标签不可能漂移开——不一致在任何地方都不是错误，它表现为一台 DNS 明明正常的机器上八个空空如也的面板。

两个前端是用不同语言写的、渲染同一个仪表盘的两套独立代码，而它们**唯一**的差别是速率窗口：Grafana 按面板展开 `$__rate_interval`，而 uPlot 前端没有宏展开，因此它固定使用 `RATE_INTERVAL`（`5m`）。宏若泄漏到 uPlot 一侧，就是一个 Prometheus 解析错误，会把整个标签页变成空白。

有三个测试把两侧绑在一起，因为再没有别的东西连接它们：

- `TestRolodexDashboardMirroredInFrontendQueries` 从 Go 测试中读取 `ui/src/components/monitoring/queries.js`，若任一侧提到了另一侧没有的 rolodex 指标族则失败——与 `TestBootStepsFrontendInSyncWithBackend` 对启动阶段所用的是同一种防漂移手段。
- rolodex 抓取集成测试断言**所固定的 rolodex 镜像确实导出了** `monitoring.RolodexDashboardMetrics()` 中的每一个指标族，并以 `# TYPE` 行匹配，这样名称是另一个前缀的指标族就无法为一个缺失的族背书。面板提到守护进程并不发出的指标族时，会渲染出一张空图，而那与一个空闲的解析器无法区分。
- `TestDashboardQueriesParseInPrometheus` 把每个仪表盘的每一条表达式送到一个真实的 Prometheus 面前。JSON 内部畸形的 PromQL 在任何地方都不是语法错误：文件被成功置备，仪表盘被加载，面板画出坐标轴，然后永远显示 "No data"。

### 仪表盘置备

`monitoring.GrafanaDashboards(diskDevices)`（`src/monitoring/dashboard.go`）就是那份注册表——每个仪表盘的文件名、uid、标题与渲染出的 JSON——而 `WriteGrafanaProvisioningFiles` 遍历它。新增一个仪表盘就是在那里加一条，仅此而已：置备器（`GrafanaDashboardProviderYAML`）指向的是 `dashboard-json` **目录**，因此其中的每个文件都会被拾取。在注册表存在之前，文件写入器就是事实上的清单，这意味着要添加第二个仪表盘，就只能去改一段与仪表盘毫不相干的代码。

那些 uid 是常量（`OverviewDashboardUID`、`DNSDashboardUID`），因为 Web UI 会深链它们。漂移的 uid 在任何地方都不会产生错误——Grafana 只会在 iframe 里呈现一个 "dashboard not found" 页面。

DNS 仪表盘是**由面板规格构建并序列化**出来的（`src/monitoring/dashboard_dns.go`），而不是像仍然如此的旧 overview 仪表盘那样拼接进一份 JSON 模板。仪表盘中畸形的 JSON 代价不是少一个面板；它会让置备失败，于是该仪表盘根本不会出现。面板的 target 携带对象形式的数据源引用（`{"type":"prometheus","uid":GrafanaDatasourceUID}`）——Grafana 13+ 无法解析 target 中的旧字符串形式，会在不报任何错误的情况下渲染 "No data"。

### 生命周期

Prometheus 与 Node Exporter 在启动时总是被启动。监控后端设置决定另外启动的是 Grafana 还是 socat 转发器。启动失败是非致命的；系统会在没有监控的情况下继续运行。重启由 systemd 的 `Restart=always` 策略处理。`Stop()` 方法是空操作，因为系统服务跨控制器重启而留存。

### 监控 API

- `GET /monitoring/status`（需要鉴权）—— 返回 `backend`（`"uplot"` 或 `"grafana"`）、每个服务的运行标志（`prometheus`、`node_exporter`、`monitoring_ui`，以及仅在 Grafana 模式下的 `grafana`），以及 `disk_devices`：支撑该 btrfs 文件系统的内核设备基名，前端会把它代入 Disk I/O 查询。`disk_devices` 为空表示发现失败，面板会回退到一个匹配不到任何东西的正则。当监控未配置时返回 `{"status": "disabled"}`。按服务的镜像与单元元数据不在此处——那是 `GET /system-services`。
- `GET /metrics`（localhost 或需要管理员）—— system controller 自身的 Prometheus 端点。参见 [System Controller Metrics](#system-controller-指标)。

### System Controller 指标

控制器把自身状态以 Prometheus 文本展示格式导出在**它已有的监听器**上（`:5309`，`MetricsPath = "/metrics"`），而不是自己的端口上。这是刻意的：该端点因此搭载在测试框架已经会用 `TOWN_OS_LISTEN` 迁移的那个监听器上，于是不需要向 `SYSTEM_PORT_FILES` 再添加宿主机端口，`make test-full` 与 `make dev` 也不可能在它上面冲突——IRON RULE。

它是 **localhost 或管理员**可访问的，而非公开。这次抓取聚合了账户数量、磁盘使用与哪些服务已经宕掉：那是一张"攻击什么、以及机器何时最无力抵抗"的地图。Prometheus 以 `--net host` 运行，因此它无需 podman 网络跳转即可到达环回地址，与 node-exporter 目标完全一样。

`src/metrics` 用几百行渲染该格式，而不是依赖 `prometheus/client_golang`，理由与当初把 `errgroup` 挡在门外相同。那个库的价值在于它的注册表、collector 接口与直方图机制——而这里一样都没用到，因为此处的每个值要么是进程生命周期内的计数，要么是每次抓取时从某个 manager 读取的——与此同时它的传递依赖树（`prometheus/common`、`procfs`、protobuf）却是实打实的，并会落进一个从内存启动的镜像里。

**标签值的转义是承重的，而非防御性的。** 标签值携带运维输入（仓库名、包名、systemd 单元名）。一个未转义的引号毁掉的不是一行——它会让 Prometheus 拒绝*整次*抓取，于是一个名字古怪的包就能悄悄把全部监控搞垮。

导出的内容：

| 指标 | 类型 | 说明 |
|---|---|---|
| `townos_up` | gauge | 服务期间恒为 1；不服务时不存在 |
| `townos_start_time_seconds` | gauge | 运行时长为 `time() - 此值`，以抓取方的时钟计 |
| `townos_package_units{state}` | gauge | `active`/`failed`/`inactive`，仅限已安装的包 |
| `townos_system_units{state}` | gauge | `town-os-system--*`，排除 NC 与 socket 单元 |
| `townos_package_unit_active{unit}` | gauge | 按单元的 1/0，因此运维能看出*哪个*服务宕了 |
| `townos_system_unit_active{unit}` | gauge | 系统服务同理 |
| `townos_packages_installed` / `townos_packages_available` | gauge | 清单数量 |
| `townos_repositories` / `townos_repository_errors` | gauge | 错误只计数，不按名称打标签 |
| `townos_upgrades_available` | gauge | |
| `townos_accounts{kind}` | gauge | `admin`/`user`/`disabled` |
| `townos_accounts_granted` | gauge | 持有至少一项授权的非管理员 |
| `townos_filesystems{state}` | gauge | `user`/`installed`/`uninstalled` |
| `townos_disk_total_bytes` / `_used_bytes` / `_available_bytes` | gauge | |
| `townos_audit_recent_errors` | gauge | 与仪表盘上那颗红色徽标所显示的是同一个数字 |
| `townos_audit_events_total{result}` | counter | `success`/`failure`，由 `auditMiddleware` 递增 |
| `townos_http_requests_total{method,status}` | counter | status 是一个**类别**（`2xx` 等），绝不是精确状态码 |

这里有几个选择本身就是要点，而非无关紧要：

- **一次抓取绝不会整体失败。** 每个 collector 都容忍 nil 的 manager，并在出错时记录日志后跳过。因为某一个子系统生病就返回 500，会让其余每一个指标恰恰在最需要它们的时刻消失，于是机器读起来是彻底死了而不是部分降级——而且启动过程中的抓取本就该报告哪些已经起来了。
- **值为零的桶仍然会被发出。** 在零时消失的 gauge 与机器停止上报的 gauge 无法区分，因此"没有失败的单元"看起来会与"单元采集坏了"一模一样。
- **状态按类别分桶。** 每一个不同的状态码都会成为一个永久序列，而一个在数十条路由上返回 400/401/403/404/409/422 的控制平面会迅速把序列数乘开，只为回答一个没人会对家用机器提出的问题。精确状态码本来就在审计日志与请求日志里。
- **计数器在内存中且按进程计。** 跨重启存活的计数器描述的是这台机器的历史而非本进程的历史，而 Prometheus 本来就理解计数器重置。这也让一次抓取——以及为它供数的审计中间件——完全不碰数据库。
- **`/metrics` 被排除在审计日志之外**，也被排除在它自己的请求计数器之外。否则 15 秒一次的抓取每天会写下约 5,700 行审计记录，而它们描述的不是任何运维做过的事，并且会主导它所服务的那个计数器。
- **`metricsMiddleware` 注册在三者的最外层**（在审计与授权白名单之前），这样被任一道门拒绝的请求仍会被计数——一个无法解释的 403 恰恰是这个计数器要暴露的东西。它从返回的 error 中取状态码，因为返回 error 的处理器此时还没有写出自己的状态码。

**抓取目标在任何地方都不会被重新拼装。** `MetricsScrapeTarget(listenAddr)` 从服务器绑定所用的同一个字符串推导它，而 `main.go` 把结果交给 `monitoring.Ports.ControllerMetrics`——与 `PackageNetworkState.FQDN` 和 `Manager.MetricsAddr()` 存在的理由相同，都是唯一事实来源。通配绑定（`:5309`、`0.0.0.0:5309`、`[::]:5309`）会被改写为 `localhost`，因为通配符不是任何东西能连接的地址；而显式指定的主机会被原样保留，因为改写它会把抓取指向控制器刻意不在的地址。结果为空时会省略该 job，而不是指向一个猜测。当 `TOWN_OS_TLS` 开启时，`ControllerMetricsScheme` 为 `https`，该 job 还会携带 `insecure_skip_verify`——叶子证书由本机自己的 CA 签发，Prometheus 没有理由信任它，也没有干净的途径拿到它，而这次抓取是宿主机命名空间内的环回通信，因此不可能有别的东西冒充它应答。

### 监控 UI

侧边栏导航中的监控标签打开一个仪表盘页面，页面上带有 **System / DNS 子标签页**，与其他每一个带子标签页的界面一样可通过 `?tab=system|dns` 深链，因此有人在故障期间正在看的仪表盘能挺过一次刷新，也可以被分享成链接。未知的 `?tab=` 值会回退到 System，而不是什么也不渲染。这份标签页清单是同一个数组，既持有要挂载的 uPlot 组件，也持有展示相同面板的 Grafana uid，因此不可能出现某个标签页在一种后端有、在另一种没有的情况。

渲染方式取决于状态响应中的 `backend` 字段：

- **uPlot 模式**：在 React 中用 uPlot 直接渲染面板，在 5308 端口查询 Prometheus。System 网格把自己钉在视口内（四个面板，每行两个）；DNS 网格**不这样做**——八个面板挤进一屏后每个只剩约 100px 的画布高度，到那个程度延迟图就只是装饰了，因此面板采用固定高度，页面滚动。
- **Grafana 模式**：一个内嵌的 Grafana iframe，指向 5308 端口，使用 kiosk 模式与浅色主题。切换标签页会把该框架重新指向另一个仪表盘的 uid，并且 iframe 以该 uid 作为 key，因此框架是被*替换*而不是在其中导航——Grafana 有自己的历史记录，而在活动框架上替换 src 会让浏览器的后退按钮在多个仪表盘之间来回走，而不是离开该页面。

两种后端的面板标题完全一致：切换后端的运维不该还要去琢磨哪个面板变成了哪个。它们是硬编码的英文——本界面不含任何 `t()` 调用，而且 Grafana 的面板标题无论如何都无法翻译，因为它存在于被置备的 JSON 之中。

当所需服务未在运行时，会改为显示一条警告横幅与占位信息。

## UI 容器

system controller 通过 `ui.Manager` 把一个独立的 UI 容器（`quay.io/town/ui`）作为系统服务管理。镜像标签由 system controller 的发布标签推导（`quay.io/town/ui:<tag>`），可通过 `UI_IMAGE` 环境变量覆盖。启动失败是非致命的；系统会在没有 UI 容器的情况下继续运行。

## Web UI 布局
### 仪表盘服务面板

仪表盘首页在统计卡片网格之上显示一个全宽的已安装服务面板。该面板列出从 `GET /systemd/units` 获取的所有包服务单元。每一行服务显示：

- 一个状态图标：活动为绿色对勾圆圈，失败为红色叉号圆圈，未激活为灰色圆圈。
- 包名（从 `package_identifier` 字段解析）。
- 以文本形式呈现的活动状态。
- 包描述（若有）。
- 来自 `POST /packages/installed/info` 的编译后 notes，内联渲染并带有类型感知的链接（URL、邮箱、电话）。

点击服务行——状态图标或包名皆可——会跳转到 `/dashboard/system?search=<package_identifier>`，即该服务在服务页面上自己那一行。服务页面用 `?search=` 的值预填过滤框，并把该词传给 `GET /systemd/units-tree`；该搜索匹配根节点自身的字段，因此页面打开时只显示这一个包及其依赖子树，而不是整个列表。这个词只是初始值，不是锁定：清空或修改过滤框会重新展开列表。链接携带的始终是原始的 `package_identifier`，绝不是美化后的 `display_identifier`——后者不是树搜索能匹配的词，用它构造的链接会落在空树上。当没有安装任何服务时该面板隐藏。notes 每个服务只获取一次并被缓存。

### 布局

仪表盘采用双栏布局：左侧是吸顶的侧边栏，右侧是带吸顶顶栏的内容区。

**侧边栏** —— 一个 256px 宽（`w-56`）的纵向面板，顶部是灰色横幅中的 Town OS 徽标与品牌文字，其下是纵向堆叠的导航按钮（每个带图标与标签）。当前路由使用 `variant="secondary"`，非当前路由使用 `variant="ghost"`。

**顶部状态栏** —— 一条右对齐的横条，显示：连接状态胶囊（加载中/离线/在线）、系统服务失败计数（当 `system_services.failed > 0` 时显示红色胶囊徽标并链接到 `/dashboard/system?expand=system`）、带管理员徽标的登录用户名，以及登出按钮。

## 系统服务

系统服务是由 systemd 管理的基础设施容器（区别于用户安装的包服务）。它们使用 `town-os-system--` 单元名前缀。

这一集合是：rolodex、ingress、pages、UI、node-exporter、Prometheus、监控 UI（socat 转发器或 Grafana），以及**每个网络一个 gfeh 分区**（`town-os-system--gfeh-<network>`）。该清单中的每一项都必须在 `collectSystemServices()` 中注册，这样 `POST /system-services/refresh` 才会重新拉取并重启它——那里的遗漏是不可见的，直到某次升级悄悄把该服务留在旧镜像上。

### 系统服务单元生成

`GenerateSystemServiceUnit` 产出基于 podman、带 `Restart=always` 的 systemd 单元。单元配置支持一个 `VolumeDirs` 字段，列出需要通过 `ExecStartPre=/bin/mkdir -p <dir>` 行预先创建的宿主机目录，以防容器在重启后、system controller 尚未运行之前启动时挂载失败。

### 系统服务 API

- `GET /system-services`（localhost 或需要鉴权）—— 列出系统服务及其实时单元状态。每一项包含 key、显示名、镜像、端口与 systemd 单元状态字段。当监控未配置时返回空列表。不计入审计日志。
- `POST /system-services/status`（需要管理员）—— 改变某个系统服务的状态。接受 key 与动作（`start`、`stop`、`restart`）。`enable` 与 `disable` 动作会被拒绝。
- `POST /system-services/refresh`（需要管理员）—— 刷新系统服务状态。

## Web UI 生产镜像

一个独立的 UI 容器镜像（`quay.io/town/ui`）由 `Containerfile.ui` 构建。它采用两阶段构建：`oven/bun:latest` 构建 UI 静态文件，然后 `docker.io/library/caddy:latest` 在 80 端口以 SPA 路由方式（`try_files {path} /index.html`）提供它们。UI 经由共享的 ingress 访问，而不是直接霸占宿主机的 `:80`——它是 ingress 对任何未被路由匹配的 host 的默认 `:80` 后端，因此裸 IP 登录仍然可用。

**缓存头是承重的**（`Caddyfile.ui`）。`/assets/*` 之下的一切都由 Vite 加了指纹，因此一个资源 URL 永远精确对应某一次构建，并以 `public, max-age=31536000, immutable` 提供。`index.html` 是 Vite **不**加指纹的那一个文件，而它正是指明当前 bundle 的那个；若提供时完全不带 `Cache-Control`，浏览器可能会施加启发式新鲜度（RFC 9111 §4.2.2）并在不重新验证的情况下复用其缓存副本，于是升级后的机器会继续发放上一个版本的 `index.html`，而它指向的是上一个版本的 bundle。症状是一次看上去根本没发生过的升级——新功能渲染得就像 UI 从未听说过它们。所有非资源路径都是由 `try_files` 解析到 `index.html` 的 SPA 路由，因此 `no-cache` 规则被写成覆盖它们全部（`@html not path /assets/*`）。

`make release-ui` 以 `--no-cache` 构建，因此 `push-rc` 总是发布新鲜构建的 UI 资源，而不是层缓存中的 bundle。

**测试从不拉取 quay 上的 UI 镜像** —— `ui-image` make 目标在本地把 `Containerfile.ui` 构建为 `localhost/town-os-ui:<INSTANCE_ID>`（始终与宿主机架构及仓库内的 UI 源码一致），保存到镜像缓存，测试框架再把它加载进测试容器并经由 `UI_IMAGE` 环境变量注入。`test-integration-build` 与 `test-ui-integration` 依赖 `ui-image`。quay.io/town/ui 的标签只用于生产/发布推送。`integration/systemcontroller_ui_test.go` 中的 `uiTestImage` 在 `UI_IMAGE` 未设置时跳过其测试，而不是回退到某个 quay 标签。

## Proton 运行器镜像

Proton 运行器镜像（`quay.io/town/proton`）由 `Containerfile.proton` 构建。它采用两阶段构建：下载阶段获取 GE-Proton 发行版压缩包（通过 `GE_PROTON_VERSION` 构建参数固定版本），运行时阶段安装 Wine/Proton 依赖（64 位 + 32 位）、用于无头运行的 Xvfb，以及位于 `/usr/local/bin/proton` 的包装脚本，该脚本会先启动虚拟帧缓冲并配置 Proton 环境，再执行应用。

make 流水线提供：`release-proton-image`（构建）、`push-proton-rc`（推送按架构的候选发布标签 `rc.<date>-<arch>` + `rc.latest-<arch>`），以及 `push-proton-release`（推送按架构的发布标签 `release.<date>-<arch>` + `latest-<arch>`）。当 `PROTON_ENABLED=1` 时，proton 镜像也包含在完整的 `push-rc` / `push-release` 流程以及 `manifest-rc` / `manifest-release` 的组装之中。

## Web UI API 客户端

浏览器在运行时从 `window.location` 确定 API 基础 URL，使用当前协议与主机名加上 5309 端口（例如 `https://myhost:5309`）。不涉及任何服务端代理；浏览器直接与 system controller API 通信。

设置了 `VITE_API_URL` 环境变量时，它会覆盖浏览器推导出的 URL。这在 API 服务器运行于不同主机或端口的开发过程中很有用。

监控仪表盘从当前主机名推导其监控端口 URL（5308 端口）。当设置了 `VITE_API_URL` 时，主机名从中提取；否则使用 `window.location.hostname`。

## Web UI 无障碍

所有对话框组件都包含一个 `DialogDescription` 元素，简要描述该对话框的用途。这满足了 Radix UI 对屏幕阅读器的无障碍要求，并消除 `aria-describedby` 警告。这些描述放在对话框头部标题之后，对所有用户可见。

## 国际化

所有面向用户的字符串（UI 标签、错误信息、toast 通知、审计日志动作描述）都通过消息目录模式实现可翻译。

### 后端

`i18n` 包提供一个 `T(locale, key, args...)` 函数来解析翻译键。回退链是：请求的语言环境，然后 `en-US`，最后是原始键字符串。当提供了 `args` 时会施加 `fmt.Sprintf` 格式化。消息键使用点分命名空间（例如 `auth.login_failed`、`pages.toast_provisioned`）。

### 已填充的目录

后端目录在 `src/i18n` 中按语言环境一个文件（`de_de.go`、`zh_cn.go` 等）；前端镜像位于 `ui/src/i18n`（`de-DE.js`、`zh-CN.js` 等）。两侧保持同步——每一个已填充的后端目录都有一个前端孪生体。

`PopulatedLocales()` 是权威清单（48 项）：`en-US`、`ar-AE`、`ar-EG`、`ar-SA`、`bn-BD`、`bn-IN`、`cs-CZ`、`da-DK`、`de-AT`、`de-CH`、`de-DE`、`en-AU`、`en-CA`、`en-GB`、`en-IN`、`en-NZ`、`en-ZA`、`es-AR`、`es-ES`、`es-MX`、`fi-FI`、`fr-BE`、`fr-CA`、`fr-CH`、`fr-FR`、`hi-IN`、`hr-HR`、`hu-HU`、`it-IT`、`ja-JP`、`ko-KR`、`nl-BE`、`nl-NL`、`pl-PL`、`pt-BR`、`pt-PT`、`ro-RO`、`ru-RU`、`sa-IN`、`sk-SK`、`sl-SI`、`sv-SE`、`th-TH`、`tr-TR`、`uk-UA`、`vi-VN`、`zh-CN`、`zh-TW`。不在其中的一律回退到英语。`IsPopulated(code)` 是 UI 用来在语言选择器中禁用未填充条目的依据。

这份清单是**从目录映射派生出来的，而不是手写出来的**：`buildPopulatedLocales()` 在 init 时读取 `catalogs` 的键，将其排序，并把 `en-US` 钉在最前面；`IsPopulated` 则直接对 `catalogs` 做索引。它过去是一份手工维护的切片字面量，只有一种失败模式，而那种失败是无声的——一个已在 `catalogs` 中注册、却在字面量里被遗漏的目录，被翻译了、被发布了，却从未在选择器中被提供出来。`PopulatedLocales()` 返回一个克隆，因为这份清单如今是包级状态，而不再是每次调用都新建的字面量；调用方对结果做排序或截断，不能因此扰动下一个调用方看到的内容。

### 国家变体

一个目录属于两种类型之一，其差别在于文件是怎么写的，而不在于它是怎么被选中的——两种类型都算已填充，也都出现在选择器中。

**语言目录**是一份翻译，完整写出：`de_de.go`、`cs_cz.go`、`ja_jp.go`。

**国家目录**由 `derive(base, overrides)`（`src/i18n/derive.go`，前端镜像为 `ui/src/i18n/derive.js`）构建：取它所属语言的目录，再加上该国家确实说得不一样的那些字符串。奥地利德语就是德语；`de_at.go` 回答的问题不是"这句话德语怎么说"，而是"这些句子里，哪一句奥地利人不会那样写"。把 `de-DE` 复制进 `de_at.go` 再改上四行，将意味着下一个加进 `de-DE` 的消息键会悄无声息地以英文抵达奥地利，而对一条德语字符串的修正得在三个文件里被找出来并重复一遍。继承基础目录、只列出分歧之处，让变体默认就是对的：一个新键在其基础语言拥有它的那一刻，就落到了每一处。

有十八个语言环境是这样派生出来的：

| 基础 | 由它派生 |
| --- | --- |
| `en-US` | `en-CA`、`en-GB` |
| `en-GB` | `en-AU`、`en-IN`、`en-NZ`、`en-ZA` |
| `de-DE` | `de-AT`、`de-CH` |
| `fr-FR` | `fr-BE`、`fr-CA`、`fr-CH` |
| `es-ES` → `es-latam` | `es-AR`、`es-MX` |
| `pt-BR` | `pt-PT` |
| `nl-NL` | `nl-BE` |
| `ar-SA` | `ar-AE`、`ar-EG` |
| `bn-BD` | `bn-IN` |

`es-latam`（`src/i18n/es_latam.go`、`ui/src/i18n/es-latam.js`）是唯一的中间层：它承载所有美洲变体共有的、相对于半岛西班牙语的分歧——`inválido` 而非 `no válido`，`agregar` 而非 `añadir`，直引号而非 `« »`——`es-AR` 与 `es-MX` 都建立在它之上。**它没有注册进 `catalogs`，也不可被选中**，因为它是一个共享片段，而不是任何人真正生活的地方；把它公布出去，等于提供一个并不存在的国家代码。

有些覆盖映射很小，还有几个（后端的 `en-CA`、`de-CH`，以及 `es-MX`）是空的。对一块技术性的控制面板来说，这是诚实的答案——加拿大英语保留美式的 `-ize` 拼写，而 `de_de.go` 里没有任何一条消息含有 `ß`，瑞士的 `ss` 规则无处可施（前端的 `de-CH.js` 确实带有真实的覆盖，因为 `de-DE.js` 用了 `ß`）。一份空的覆盖映射仍然标记出：这个语言环境是被慎重审阅过的，而不是被遗忘的。

这套方案由两侧的测试（`src/i18n/derive_test.go`、`ui/src/i18n/derive.test.js`）守住：每一个覆盖键都必须存在于其基础目录中，每一条覆盖都必须与它所替换的基础字符串确有不同，每一个派生目录都必须带有其基础目录的完整键集，并且每一个派生目录都必须列在测试的 `variants()` 表里——因此一个国家目录不可能在这些规则未施加于它的情况下被发布。

**每个语言环境代码都带有地区子标签**，`TestLocaleCodesAreRegionQualified` 维持这一点。苏美尔语（`sux`）曾是唯一的例外——一个裸的 ISO 639-3 代码——它已被移除。移除它是因为它的文字而非它的形状：楔形文字位于 `U+12000`–`U+1254F`，几乎没有任何系统自带能显示它的字体，因此在任何没有 Noto Sans Cuneiform 的机器上，该语言环境的每一个字符串都会画成替换方块。目录中括号里携带的罗马化转写却留了下来，这让情况比全空更糟——一堆窟窿周围散落着拉丁字母片段与标点。要诚实地渲染它，就意味着自带一份 webfont（该目录用到 45 个不同码位，但整套字体有 462K，做子集化又需要构建主机上有 `fonttools`），并添加 UI 完全没有的 `@font-face` 机制——为一门没有使用者的语言配备这么多装置，实在过重。

### 语言环境清单

全程使用 BCP 47 语言环境代码。提供了两份精选清单：

- **CommonLanguages**（21 项）—— 阿拉伯语（ar-SA）、孟加拉语（bn-BD）、德语（de-DE）、英语（en-US）、西班牙语（es-ES）、法语（fr-FR）、印地语（hi-IN）、意大利语（it-IT）、日语（ja-JP）、韩语（ko-KR）、荷兰语（nl-NL）、波兰语（pl-PL）、葡萄牙语（pt-BR）、俄语（ru-RU）、梵语（sa-IN）、瑞典语（sv-SE）、泰语（th-TH）、土耳其语（tr-TR）、乌克兰语（uk-UA）、越南语（vi-VN）、中文（zh-CN）。每一项都包含母语文字名称与英文名称。
- **ExtendedLocales**（89 项）—— 国家/地区特定语言环境变体的完整清单（例如 de-AT、en-GB、es-MX、fr-CA、pt-PT、zh-TW）。

### 前端

一个 React 上下文提供者（`I18nProvider`）包裹整个应用，并暴露一个 `useI18n()` hook，返回 `{ locale, setLocale, syncServerLocale, t }`。`t` 函数以与后端相同的回退链，针对前端目录解析键。参数插值使用 `{name}` 占位符（例如 `t('greeting', { name: 'Alice' })`）。

与之并列还导出了 `translateIn(locale, key, params)`：它在指定的语言环境中翻译，而不是在当前生效的语言环境中，回退链相同。它的存在是为了那条确认语言已切换的消息：`t` 闭包捕获的是调用它的那次渲染所用的语言环境，因此从语言表单里发出的确认消息会写在**正在离开**的那门语言中——而这恰恰是页面上唯一一条以"该语言已不再使用"为内容的消息。

### 语言环境的检测、存储与同步

UI **首先从浏览器**选择语言，而不是从全局设置。加载时它读取 `navigator.languages`，并把这些有序偏好与已发布的目录做匹配。匹配不区分大小写，并会先在所有偏好上尝试精确标签，然后才按以下顺序回退：

1. **精确匹配。** `de-CH` 如今自带目录，因此 `de-CH` 解析为 `de-CH`，而不是折叠到 `de-DE`。
2. **中文按文字/地区消歧。** `zh-Hant` 或 `TW`/`HK`/`MO` 地区 → `zh-TW`，否则 `zh-CN`。文字是比任何默认值都更强的信号，因此这一条排在下面两条之前。
3. **有名有姓的地区默认值。** 那些不自带目录、却读某个变体而非其语言默认值的国家：西班牙语的拉丁美洲 → `es-MX`，葡语非洲与东帝汶 → `pt-PT`，爱尔兰、非洲以及南亚与东南亚的英语 → `en-GB`。没有这一条，`es-CO` 会拿到半岛西班牙语，`en-IE` 会拿到美式英语。
4. **有名有姓的语言默认值。** `ar` → `ar-SA`，`bn` → `bn-BD`，`de` → `de-DE`，`en` → `en-US`，`es` → `es-ES`，`fr` → `fr-FR`，`nl` → `nl-NL`，`pt` → `pt-BR`。
5. **任何共享主子标签的目录。**

第 3、4 步之所以存在，是因为回退过去只有第 5 步，而那只在每种语言恰好有一个目录时才是对的。如今有八种语言不止一个目录：一个浏览器要一个光秃秃的 `en`，或者要 `en-PH`，否则就会落到 `catalogs` 对象中最先声明的那一份英语上，于是答案成了导入顺序的属性，而不是任何人做出的决定。

优先级从高到低：

1. 明确的选择，**按浏览器**持久化在 `localStorage` 中——*已钉住*
2. 浏览器检测到并匹配上已发布目录的语言——*已钉住*
3. 服务器的全局 `locale` 设置，稍后经由 `syncServerLocale` 应用——*未钉住*

一旦语言环境被钉住，`syncServerLocale` 就是空操作。这正是这一拆分的意义所在：过去 60 秒一次的状态 ping 会调用 `setLocale`，从而在每次轮询时把管理员的全局 `locale` 设置强加到每一个浏览器上。`locale` 设置（系统级，默认 `en-US`，仍在 ping 响应中上报）如今只是 Town OS 未提供目录的语言的回退值。

### 语言环境 API

- `GET /locales`（需要鉴权）—— 返回当前语言环境、已填充语言环境清单、常用语言与扩展语言环境。不计入审计日志。

### 设置 UI

系统设置页面包含一个语言选择器。常用语言以母语文字名称显示在下拉框中。一个可展开区域会显示扩展语言环境清单。未填充的语言环境（即没有翻译目录的）会带星号后缀显示，并在选择器中被禁用，从而无法被选中。

选择器默认选中**页面当前实际渲染所用的语言环境**——即 `useI18n()` 持有的那一个——而不是 `GET /locales` 返回的 `current`。二者在通常情况下就并不一致：语言环境由浏览器选定并被固定，而全局 `locale` 设置仍停留在默认的 `en-US`（参见[语言环境的检测、存储与同步](#语言环境的检测存储与同步)）。预选 `current` 会让这个控件在一个并非英文的页面上显示 "English"。当前语言环境若是国家变体，它位于默认折叠的扩展清单中，因此加载时会展开该清单，以免下拉框停留在一个可见选项里根本不存在的值上；再次折叠时也会保留该条目，理由相同。`current` 只作为回退使用——仅当服务端并不提供当前语言环境时。

保存时会同时与这两者比较。只匹配其中之一仍然有事可做：与服务端一致但与页面不一致，意味着切换页面语言（调用 `setLocale`，并为该浏览器固定此选择），而不写入设置；与页面一致但与服务端不一致，意味着写入设置。只有当所选项与两者都一致时，才真的无事可做。成功提示用 `translateIn` 写在刚刚选定的那门语言中，因为它背后的界面已经切换过去了；而"无事可做"提示仍用屏幕上当前的语言，因为什么都没有改变。此前仅与 `current` 比较，使得正在显示的那门语言无法被选中——对它按下保存只会提示"无事可做"，因此要切回英文，必须先保存第三种语言。

## System Controller 配置

### 启动顺序

逐步的权威启动顺序位于 [System Controller Boot Sequence](#system-controller-启动顺序)。概括如下：

1. `setupPodmanEnv()` 把 `CONTAINER_HOST` 指向宿主机的 podman socket。
2. 解析标志，随后立即以启动状态桩绑定 `:5309`。
3. 创建目录、清理陈旧的根 DB、打开数据库，以及账户（外加旧服务账户清除）、会话、审计、设置、pages 与网络 manager——最后一个会播种 home 网络。
4. 播种仓库、强制刷新仓库根。
5. 安装 manager、btrfs 存储、systemd manager；解析镜像标签。
6. 写入 Rolodex 配置并等待就绪（rolodex 本身由 systemd 监管）。
7. 拉取核心镜像（NC、监控、UI）并启动监控系统服务。
8. 本地 TLS CA、ingress 与 pages 服务。
9. Reconcile 对象存储（每个网络一个 gfeh 分区）。
10. 检测版本变更、reconcile、执行更新后命令。
11. 重建 DNS、reconcile 网络、第二次（幂等的）对象存储 reconcile、编排 ingress、启动 UI 容器。
12. 新鲜度阶段（刷新之后按包重启）。
13. 构建处理器，并把启动桩原子地切换为完整路由器。
14. 一旦有分区应答，就在后台发布对象存储的名称。

监控、Rolodex 配置、核心镜像拉取、TLS CA、ingress、pages 服务、对象存储、网络 reconcile 与 UI 容器的启动失败都是非致命的；系统会在没有它们的情况下继续运行。所有容器镜像拉取都使用 `ensureImage` 助手，它在拉取前先检查 `podman image exists`，从而避免在镜像已预加载的测试/开发环境中重复拉取。非必要服务的拉取失败会记录到 stderr 且不阻止启动，使系统即便在网络暂时不可用时也能启动。

### 版本标签检测

system controller 为每一个同族服务（UI、Rolodex、网络控制器、ingress）推导出匹配的镜像标签，全部来自 `resolveImageTag()` 解析出的同一个标签：若设置了 `TOWN_OS_TAG` 环境变量则取之，否则取 `rc.latest-<arch>`（`defaultVersionTag()`，架构由 `runtime.GOARCH` 经 `archTag()` 映射为 `x86_64`/`aarch64`）。不存在编译期的 `Version` 固定值，也不存在 `/town-os.tag` 文件——两者都被移除了，因为其中任何一处的陈旧值都会在控制器已经前进之后，仍悄悄把每个同族镜像按住在旧标签上。install 构建系统通过在 systemcontroller 的 systemd 单元上设置 `TOWN_OS_TAG` 来固定某个具体标签（`../install/make/install.sh` 从 `CONTROLLER_IMAGE` 推导它）；没有覆盖时，整个机群始终跟踪 `rc.latest-<arch>`。该标签用于构造诸如 `quay.io/town/ui:<tag>` 与 `quay.io/town/rolodex:<tag>` 的镜像引用；推送的标签是按架构的，因此每一个推导出的同族标签都带有架构后缀。

### 错误格式

所有 API 错误都以 RFC 9457 Problem Detail 对象返回（含 type、title、status 与 detail 字段的结构化 JSON）。一个自定义的 `ProblemDetailHTTPErrorHandler` 被设为 Echo 的错误处理器。

### 请求日志

Echo 的 `RequestLogger()` 中间件全局启用，把所有 HTTP 请求记录到 stderr。详略程度由 `LOG_LEVEL` 环境变量控制。

### 登录限流

`POST /account/authenticate` 是公开的，而每一次尝试都要付出一次 64 MiB 的 argon2id 哈希。对密码哈希而言那是恰当的代价，但让未认证的调用者无限制地安排这种代价就是错的：几百个并发尝试就是几十 GB 的分配，而这台机器的整个设计要点就是从内存运行，其失败方式不是登录变慢——而是 OOM killer 把控制器带走。

两道相互独立的限制，因为它们回答不同的问题。`loginLimiter` 在一个时间窗内限制**每个来源的尝试次数**（5 分钟 20 次），这是让在线密码猜测变得不可行的机制，并且它按来源地址分键，因此一个滥用的客户端无法把这个家庭锁在门外。`loginGate` 限制跨所有来源的**并发哈希数**（4 个，把 argon2 的峰值内存约束在四分之一 GB 附近），而这是仅靠按来源限流做不到的。两者都在内存中且按进程计：它们保护的是本进程的内存与 CPU，而持久化它们会让一次失败的登录变成一次数据库写入。

两者都在哈希**之前**检查，而不是之后——要防御的代价正是哈希本身，因此一次仍然做了哈希的拒绝，等于为它所拒绝的攻击付了钱。gate 的名额通过闭包内部的 `defer` 释放，而不是在调用之后释放，因为被 panic 泄漏掉的名额会在进程余生中消失，四个这样的名额就能让这台机器上的每一次登录卡死到重启为止。一次被证实正确的密码会清空该来源的时间窗，因此处在同一个 NAT 地址之后的一个家庭，不会因正常使用而走进锁定状态。

### CORS

在 `DEBUG` 模式下允许所有来源。否则，允许来自同一主机名的跨端口请求（例如 80 端口上的浏览器与 5309 端口上的 API 通信），**但前提是 Host 头已被核对为这台机器可以合法被称呼的名称之一**。允许的方法：GET、HEAD、POST、PUT、PATCH、DELETE、OPTIONS。允许携带凭据，最大存活时间 3600 秒。

这项检查之所以重要，是因为旧规则——"Origin 的主机名等于 Host 头的主机名"——比较的是两个都来自同一个攻击者选定 URL 的值。把 `box.evil.example` 指向这台机器的局域网地址，浏览器就会发送 `Origin: http://box.evil.example` 与 `Host: box.evil.example:5309`，二者匹配。那正是 DNS 重绑定的形态，而在 `AllowCredentials` 之下，它把引导窗口（在不存在启用的管理员时 `POST /account/create` 会以未认证方式应答）交到了一个顺路访问的网页手里。

因此 `originAllowed` 要求 Host 头指名这台机器：它自己的主机名、`<hostname>.local`、`<hostname>.<dns_tld>`、它所应答的环回与局域网地址，或运维在 `AllowedHosts` 中配置的任何名称。这些形式是**逐一枚举的，而不是按后缀匹配**——像"任何第一个标签是该主机名的名称"这样的规则会接受 `townos.evil.example`，而攻击者只需去注册它即可。IP 字面量单独即可被接受：地址无法被 DNS 别名化，因此 `http://192.168.1.10/` 访问 `http://192.168.1.10:5309` 在构造上就是同一台机器，而这也是实际中最常见的用法。

**私有网络访问（PNA）只对 CORS 会接受的来源作答。** `Access-Control-Allow-Private-Network` 头此前是无条件回显的，那等于把浏览器"可以访问私有地址"的许可交给互联网上的每一个来源——而那正是 PNA 在 CORS 之上要额外提供的唯一保护。它的中间件注册在 CORS 中间件**之前**，因此在预检请求上它仍然会运行——预检由 CORS 自己应答，不会继续调用后面的链条。

### 优雅关闭

SIGINT 触发 context 取消。HTTP 服务器关闭，所有后台 goroutine 经由 context 通道退出。Rolodex 由 systemd 监管，不由 systemcontroller 停止。

### 命令行标志

- `-db <path>` —— SQLite 数据库路径（默认为临时文件）。
- `-btrfs <path>` —— btrfs 子卷操作的基础路径。
- `-repo-dir <path>` —— git 仓库的基础目录（默认为临时目录）。
- `-network-state <path>` —— 按包的网络状态文件所在目录（默认 `/run/town-os`，即 `DefaultNetworkStatePath`；它必须是 systemcontroller 容器与宿主机共享的路径——绝不能是 `/var/run/...` 或 `/tmp`）。
- `-listen <addr>` —— HTTP 监听地址（默认 `:5309`）。

网络控制器镜像同样不是标志；它由解析出的镜像标签推导，并可用 `NC_IMAGE` 覆盖。

### 环境变量

- `CONTAINER_HOST` —— 宿主机 podman 守护进程的 unix socket URL。启动时自动设为 `unix:///run/podman/podman.sock`（参见 `HostPodmanSocket`）。每一次 `podman` 调用——包括 systemcontroller fork 出的子进程——都从进程环境继承它，并走宿主机 socket，而不是 systemcontroller 容器隔离的 podman 存储。install 仓库中的 systemd 单元也应设置 `Environment=CONTAINER_HOST=...` 以便在 `systemctl` 输出中可见，但 `setupPodmanEnv()` 的调用才是运行时的事实来源。
- `TOWN_OS_LISTEN` —— 覆盖 `-listen` 标志。
- `TOWN_OS_SIGNING_KEY` —— 覆盖临时的 JWT 签名密钥（参见会话管理）。
- `TOWN_OS_TLS` —— 让控制平面自己的监听器（`:5309`）以 HTTPS 提供服务，由本机的本地 CA 终止，其叶子证书的签发方式与包的完全一致。**默认关闭，而这是次序问题而非折中**：没有拿到本机 CA 的浏览器无法对一张不受信任的证书完成 XHR，而与页面导航不同的是，这里没有可以点击通过的中间页——UI 会直接停止工作，而且无从抵达那个解释原因的界面。今天 UI 也是通过明文 HTTP 提供的（它是 ingress 的默认 `:80` 后端），因此没有先安装 CA 就打开它的机器，会从"未加密"直接变成"宕机"。运维应先安装 CA（`GET /tls/ca.crt`，公开），再设置本项。接受 `1`/`true`/`yes`/`on`。它在监听器绑定**之前**解析，因此以 HTTP 开始的启动状态流绝不会在其客户端脚下变成 HTTPS；并且失败时是**致命的**，而不是回退到明文：一个要求了 TLS 却悄悄得到明文的运维，处境比一台拒绝启动并说明原因的机器更糟。
- `TOWN_OS_TLS_CERT` / `TOWN_OS_TLS_KEY` —— 运维自备的证书与私钥，适用于前置名称已经拥有公共受信证书的机器。**同时**设置两者即可自行启用 TLS，且不会查询本地 CA；只设置其中一个则什么也不会发生。
- `TOWN_OS_TLS_SANS` —— 为生成的叶子证书追加的名称或 IP，逗号分隔，适用于通过控制器无法推导的名称访问的机器（CNAME，或路由器分配的 DHCP 名称）。
- `TOWN_OS_TEST` —— 若设置，则使用测试仓库而非生产默认仓库。
- `DEBUG` —— 若设置，则允许所有 CORS 来源，并把测试仓库前置到默认仓库之前。
- `LOG_LEVEL` —— 日志级别：`debug`、`info`、`warn`、`error`（默认 `error`）。
- `TOWN_OS_REPO_USERNAME` / `TOWN_OS_REPO_PASSWORD` —— 首次初始化时应用到所有仓库的仓库凭据。
- `TOWN_OS_TAG` —— 固定每个同族镜像所推导自的镜像标签（参见 [Version Tag Detection](#版本标签检测)）。由 install 构建系统在 systemcontroller 的 systemd 单元上设置。
- `ROLODEX_IMAGE` —— 覆盖 Rolodex 容器镜像（默认 `quay.io/town/rolodex:<tag>`）。
- `UI_IMAGE` —— 覆盖 UI 容器镜像（默认 `quay.io/town/ui:<tag>`）。把它设为**空字符串**（显式存在但为空）会完全跳过 UI 容器——开发模式，此时由 bun 提供 UI。
- `NC_IMAGE` —— 覆盖网络控制器镜像（默认 `quay.io/town/networkcontroller:<tag>`）。集成测试框架用它注入本地构建的 NC。
- `INGRESS_IMAGE` —— 覆盖 ingress 镜像（默认 `quay.io/town/ingress:<tag>`）。把它设为空字符串会跳过 ingress 与 pages 服务——开发模式。
- `GFEH_IMAGE` —— 覆盖对象存储镜像（默认 `quay.io/town/gfeh:<tag>`）。把它设为**空字符串**会完全跳过对象存储——开发模式。当 ingress 被禁用时对象存储同样会被跳过，因为四个 HTTP 视图只能经由它访问。
- `GFEH_SMB_PORT_BASE` —— 覆盖 SMB 监听器本会起始的宿主机端口（默认 `4450`）。这是遗留项：[没有任何分区提供 SMB 服务](#不提供-smb-视图)，因此不会分配宿主机端口。保留接线是为了让测试框架的设置保持无害。
- `TOWN_OS_WG_SALT` —— 实例盐，用于把本机的 WireGuard 接口名、监听端口与 overlay 子网与共享同一网络命名空间的另一个 Town OS 区分开。真实机器不设置它；由测试与开发框架设置。参见 [The instance salt](#实例盐)。

#### 系统服务的宿主机端口

每个系统服务都以 `--net host` 运行，因此这些端口全都绑定在控制器所处的那个网络命名空间中——即*宿主机*命名空间，在集成测试框架内部也是如此（其容器同样刻意以 `--net host` 运行，以便在桥接 DNS 失效的强制门户网络下构建仍能工作）。因此一台 `make test-full` 的机器与一台 `make dev` 的机器会争夺这里的每一个端口，并在 `Restart=always` 之下永远互相把对方拖入崩溃重启。

下列每一项各自迁移其中一个端口，并且**默认为生产端口**，因此未设置任何环境变量时会精确复现今天的启动行为。`make/lib.sh` 的 `system_port_env` 按次运行把它们分配到 `SYSTEM_PORT_FILES` 并传给测试容器——IRON RULE。`make dev` 刻意**一个都不设置**：dev 镜像的是真实机器，那里 `redirect_host_dns` 需要 rolodex 在 `:53` 上，浏览器需要 ingress 在 `:443` 上。无法解析的值会在 stderr 上报告并回退到默认值，因为打字错误否则看起来会与根本没设置一模一样。

- `TOWN_OS_DNS_PORT` —— rolodex 提供 DNS 服务的端口（默认 `53`，位于 `DNSLoopback`）。**当它为非默认值时，systemd-resolved 的路由配置会被完全跳过**：resolved 的按域名服务器地址不携带端口，因此把 resolved 指向 `DNSLoopback` 只会悄悄黑洞掉该 `.tld` 之下的每一次查询，而不是把它们留给正常的解析路径。
- `TOWN_OS_ROLODEX_METRICS_PORT` —— rolodex 提供其 Prometheus `/metrics` 端点的端口，同样位于 `DNSLoopback`（默认 `9153`）。它与 DNS 端口是彼此独立的监听器，需要各自的覆盖项；`rolodex.Manager.MetricsAddr()` 是 `rolodex.yml` 与 Prometheus 抓取目标共同构建自的那一个字符串，因此迁移它会同时移动两者。
- `TOWN_OS_NODE_EXPORTER_PORT` —— node-exporter 的环回指标端口（默认 `9100`）。
- `TOWN_OS_PROMETHEUS_PORT` —— Prometheus 的环回 HTTP API 端口（默认 `9090`）。
- `TOWN_OS_MONITORING_PORT` —— 唯一面向局域网的监控端口（默认 `5308`）。
- `INGRESS_HTTPS_PORT` / `INGRESS_HTTP_PORT` —— ingress 发布的端口（默认 `443` / `80`）。

## 设置项

| 键                      | 默认值                          | 说明                                     |
| ------------------------ | -------------------------------- | ----------------------------------------------- |
| `default_quota`          | `53687091200`                    | 默认卷配额，单位字节（50 GB）           |
| `max_archive_size`       | `1073741824`                     | 最大上传大小，单位字节（1 GB）             |
| `archive_unpack_timeout` | `600`                            | 解包超时，单位秒（10 分钟）              |
| `locale`                 | `en-US`                          | BCP 47 语言环境代码（系统级回退值）       |
| `dns_tld`                | `home`                           | 包 DNS 记录的默认顶级域|
| `dns_resolution_mode`    | `auto`                           | Rolodex 上游解析方式：`auto`、`recursive` 或 `forward` |
| `dns_local_forwarders`   | `false`                          | 从本机所在网络下发的解析器取转发器列表，而不是使用公共默认值 |
| `peer_ttl`               | `7200`                           | WireGuard peer 登记有效期，单位秒（2 小时） |
| `gfeh_partition_quota`   | `0`                              | 每个对象存储分区的配额，单位字节（0 = 不限） |
| `proton_image`           | `quay.io/town/proton:latest`     | Proton 运行器镜像——**仅在 `proton` 构建标签下注册** |

`DefaultSettings`（`src/account/settings.go`）在首次初始化时被播种，且已有的值绝不会被覆盖。

有几个键是**只读取、从不播种**的——在有东西写入之前它们没有对应的行，
其默认值位于读取处，作为空字符串的回退。不要以为把它们加入 `DefaultSettings`
不会带来其他影响：被播种的行与运维的选择无法区分，而对黑名单配置而言，
这正是"从未配置过，别动它"与"被显式设为空，推送它"之间的差别
（[RBL / DNSBL Blocklists](#rbl--dnsbl-黑名单)）。

| 键 | 缺失时的默认值 | 由谁写入 |
| --- | --- | --- |
| `monitoring_backend`     | `uplot` | `POST /settings/set` |
| `dns_rbl_config` / `dns_dnsbl_config` | 未配置（与"空"不是一回事） | `POST /dns/rbl`、`POST /dns/dnsbl` |
| `dns_excluded_services`  | 空列表（发布是选择退出制） | `POST /dns/services/set` |
| `dismissed_upgrades_hash` | 不存在（未忽略任何升级） | `POST /packages/upgrades/dismiss` |

**不存在 `object_storage_enabled`，也不存在服务账户密码。** 对象存储不是一个可以打开的功能（[Boot and reconcile](#启动与-reconcile)），而守护进程也不持有任何 Town OS 凭据（[No service accounts](#没有服务账户)）。升级后的机器上若残留这两者中任何一行，都不会被任何东西读取。

`proton_image` 不在基础 map 中：`src/account/settings_proton.go` 带 `//go:build proton`，并在 `init()` 中注册该默认值，因此不带该标签的构建没有 Proton 设置、没有 Proton 安装路径，并在状态 ping 中报告 `proton_enabled: false`。之所以采用构建标签门控的注册方式而不是导出一个 `Register` 函数，是为了不让任何调用方对 `DefaultSettings` 产生调用顺序上的依赖。
