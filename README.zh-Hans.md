[![Town OS](./banner.png)](https://town-os.github.io)

**主页：<https://town-os.github.io>** | [源码 (Gitea)](https://gitea.com/town-os/town-os) | [镜像 (GitHub)](https://github.com/town-os/town-os)

> **把你的云放进自己的储物间，任何人都能用。**

Town OS 是一个完全从 U 盘运行的自助式平台，能把任何一台电脑变成个人云服务器。它以容器方式管理自己的存储、网络与服务——无需安装。它是为所有人设计的，而不只是为专家：一个友好的界面会引导你完成一切，因此除非你自己想了解，否则不必知道这些东西是怎么运作的。

**GITHUB 只是镜像：** 源仓库位于 <https://gitea.com/town-os/town-os>。

> **中文翻译（简体）。** 本文件是 [README.md](README.md) 的简体中文译本，
> **英文原件为准**。繁体中文译本见 [README.zh-Hant.md](README.zh-Hant.md)。
> 仓库中的其他文档也有中文译本：
> [CLAUDE.zh-Hans.md](CLAUDE.zh-Hans.md)（构建与代码风格规则；繁体：[CLAUDE.zh-Hant.md](CLAUDE.zh-Hant.md)）与
> [DESIGN.zh-Hans.md](DESIGN.zh-Hans.md)（架构与功能规格说明；繁体：[DESIGN.zh-Hant.md](DESIGN.zh-Hant.md)）。
> 两者出现分歧时以英文原件为准，并应修正译文。代码标识符、文件路径、命令、
> 环境变量、make 目标名与 API 路径一律保留原文，不作翻译。

## 目录

- [为什么这很重要](#为什么这很重要)
- [已有与计划中的功能](#已有与计划中的功能)
- [可启动镜像（PC 与树莓派）](#可启动镜像pc-与树莓派)
- [征求意见](#征求意见)
- [前置条件](#前置条件)
- [开发](#开发)
- [Makefile 目标](#makefile-目标)
    - [测试](#测试)
    - [代码检查](#代码检查)
    - [本地 Registry](#本地-registry)
    - [本地 Gitea 服务器](#本地-gitea-服务器)
    - [构建](#构建)
    - [发布与推送](#发布与推送)
    - [Registry 认证](#registry-认证)
    - [Btrfs 管理](#btrfs-管理)
    - [清理](#清理)
    - [预检检查](#预检检查)
    - [SSH](#ssh)
    - [依赖检查](#依赖检查)
- [许可证](#许可证)

## 为什么这很重要

你的数据应当住在你自己家里，而不是别人的电脑上。云服务确实方便，但它带来的是月费、隐私上的取舍，以及一家公司随时可能更改条款、涨价或关停的风险。Town OS 给你同样的便利，而不必交出控制权。

使用它不需要你懂技术。插上 U 盘，开机，你就有了一套能用的系统。升级就是换一只 U 盘。重置就是重启。万一出了问题，你总能回到一个可用的状态——你不可能把自己锁在门外。

Town OS 的设计目标，是让任何人都能帮家人在家里跑起各种服务。给父母搭一台媒体服务器，让孩子的设备远离间谍软件，或者托管你自己的网站——全都不必向云服务商申请许可。

## 已有与计划中的功能

打包系统与存储、网络完全一体化，按需创建资源，包括通过 UPnP 开放端口、经由按包划分的网络控制器管理端口转发，或者建立隧道。

Town OS 运行自己的本地解析器（Rolodex），并由系统自身编排：每个已安装的包都会在一个私有 TLD 之下获得一个名称（`plex.default.home`），因此服务可以按名称而非端口号访问。同一个解析器也承担路由器级别的过滤——DNSBL 域名黑名单与 RBL 反向 IP 列表会按需向你选择的提供方区域发起查询（Spamhaus、SURBL、URIBL 等以一键添加的形式提供），另有一份你手工维护的本地黑名单。这就是你让孩子的平板、或整个家庭远离广告软件与间谍软件的方式，既不下载订阅源，也不会在你背后缓存任何东西。

网络可以切分为 WireGuard overlay。每个网络拥有自己的 TLD 与自己的 overlay 子网，而且这种划分是真实的：局域网上的笔记本电脑能解析每一个网络，而通过隧道接入某一个网络的手机只能解析那个网络与公共互联网，别无其他——同级网络的名称对它而言根本不存在。Peer 从 UI 登记（也可以从一个你只授予了"在某一个网络上登记设备"权限的账户登记），获得一份可直接导入的配置，并被告知它实际拨打的那个地址，因此同一 Wi-Fi 上的手机不会被要求绕经公网 IP 回环。登记带有 TTL 并会自行过期，因此被弃用的设备不会永远占着一个 overlay 地址。Networks 界面列出此刻谁在连接——设备、账户、overlay 地址、实时握手与传输量——并带一个断开按钮。

所有 HTTP 内容都按名称经 HTTPS 提供。Town OS 运行自己的证书颁发机构；每个包与每个 page 都为其 FQDN 获得一张叶子证书，而单一的共享 ingress 在 `:443` 上终止 TLS，并依据 SNI 选出正确的服务。从 `/tls/ca.crt` 取一次根证书，浏览器就不再抱怨了。同一张证书在局域网与 WireGuard 隧道中都有效，因为 ingress 在所有接口上监听，并按名称而非地址路由。

存储系统是与打包系统一并设计的，以支持升级，也支持临时卸载与日后恢复。存储按包做了独立分区，从而可以按成本与可用性优先级制定灾备策略。配额用于避免存储需求给用户带来意外。

你的文件有了自己的家。对象存储（gfeh）为每个网络运行一个守护进程，各自拥有自己的用户、自己的权限与自己的那片磁盘，并同时以四种方式发布同一批文件——一个 S3 端点、纯 HTTPS、一个 Google Drive 兼容视图，以及 IPFS——全部按名称提供，全部位于同一个 ingress 与同一张证书之后。用户是一棵树而不是一张平铺列表，因此你可以把某个分支连同其下的一切交给某个人；文件也可以作为链接分享出去，并在日后撤回。Object Storage 界面展示每个网络的分区、其中有谁、他们能访问什么，以及当前发布的每一个链接。搭建这台机器的人会被自动安置进 home 分区，因为一个还得自己给自己授权才能用的文件库，根本算不上文件库。

包可以向用户索取输入——类似 debconf——但是通过 UI 进行（可以看看截图）。这些是模板变量，可用于配置容器镜像与管理网络。问题是带类型的，而类型确实在干活：端口留空会自动分配，secret 会以 256 位熵生成而不是要你自己想一个，布尔值渲染为复选框，问题可以标记为可选从而让空答案意味着"不设置它"，一组高级选项还能用 `show_if` 收纳到一个复选框之后，这样对话框就不会是一堵字段墙。`oauth` 类型的问题取代了你过去手工运行的那个 shell 脚本：点击 Connect，在供应商自己的页面上授权，令牌就落进了字段里——而且 Town OS 里没有烘焙任何供应商清单，因为 URL 是由包自己指明的。

包还可以定义 Go `text/template` 文件模板，在安装时渲染进卷中，并可访问用户应答、包元数据与系统信息（主机名、各 IP）。模板在卷播种之后、服务启动之前应用，且已有文件绝不会被覆盖。包可以依赖其他包：依赖会自动安装，与父包共享一个私有容器网络从而按名称而非经由宿主机端口通信，能在安装时互相传递值（例如数据库的容器名与端口），并且能以双方共同选择加入的方式共享存储卷。[包仓库](https://github.com/town-os/default-packages) 里有更多信息。你也可以用一份私有仓库清单完全替换掉这些仓库——非常适合你的游戏伙伴、需要你支援的家人等等。这方面预计会有大量扩展。

所有服务都有充分的日志与监管。有一个舒适的 UI 来访问这些信息，其呈现方式意在让非技术用户也能安全地阅读。服务以树状展示——一个包与嵌套在其下的依赖——你可以一次性对整棵树执行操作，或在一个视图中读取树中每一个单元的合并日志。管理员与普通用户是分开的账户：如果你愿意，你可以帮父母跑一台 Plex（或类似的东西），也可以让他们远离间谍软件。在"管理员"与"普通用户"之间还有授权（grant）——勾一个框，就能让某个账户在某一个网络上登记设备，或者运行那个网络的文件库，仅此而已。授权没有解锁的东西默认保持关闭，因此明年新增的某项能力不会被悄悄交给已经存在的账户。

给机器做更新时，它会把过程摊开给你看。system controller 在开始启动之前就绑定它的端口，因此 UI 会实时流式展示进度——控制器、DNS、系统服务，然后每重启一个包就多一行——而不是对着一个死端口转圈。它为每一次进程化身打上一个 id，因此浏览器能区分"旧版本还在应答"与"新版本已经起来"，并知道这次更新究竟落地了没有。

整个界面都做了国际化。所有面向用户的字符串——后端错误信息与前端 UI 文本——都经由一个以 BCP 47 语言环境代码（例如 `en-US`、`de-DE`）为键的消息目录路由。**有 24 种语言在前后端都完整翻译**：阿拉伯语、孟加拉语、中文（简体与繁体）、丹麦语、荷兰语、英语、芬兰语、法语、德语、印地语、意大利语、日语、韩语、波兰语、葡萄牙语、俄语、梵语、西班牙语、瑞典语、泰语、土耳其语、乌克兰语与越南语。系统设置中的语言界面以母语文字呈现 21 种常用语言，并带一个可展开的、包含 89 个国家/地区代码的清单；没有对应目录的会显示出来但被禁用。

UI 从你的浏览器（`navigator.languages`）选择语言，把地区变体折叠到我们提供的语言上（`de-AT` 使用德语目录），并按文字消歧中文。明确的选择会按浏览器记住，因此这台机器的全局语言设置不再覆盖每个人所看到的内容——它只是 Town OS 没有对应目录的语言的回退值。

Windows 应用可以借助 Valve 的 Proton 兼容层与原生 Linux 容器并肩运行。带 `proton` 段的包定义会指明一个 Windows 应用镜像、一个提取目录与一个可执行文件路径；系统从 OCI 镜像拉取该应用，把它提取进一个持久卷，并在 Proton 运行器容器中运行它。运行器镜像通过系统级的 `proton_image` 设置配置。**Proton 支持是选择加入的**：用 `make PROTON_ENABLED=1 …`（或 `go build -tags proton`）重新构建才会把它编译进去。没有该标签时，包 YAML 中的 `proton:` 块会在安装时被拒绝，`proton_image` 设置不会被播种，设置 UI 会省略 Proton 卡片，发布流水线也不会构建或推送该运行器镜像。

内置的监控栈开箱即用地提供系统可观测性。Prometheus 采集指标，Node Exporter 报告主机级统计，两者都作为系统服务管理，无需手动配置。默认情况下仪表盘直接用 uPlot（约 35 KB）在 UI 中渲染——磁盘 I/O、网络、CPU 与内存——从而把约 771 MB 的 Grafana 镜像整个挡在机器之外。把 `monitoring_backend` 设置切换为 `grafana`，则会拉取并启动完整的 Grafana 栈，并预置一个数据源与两个仪表盘。

QEMU 虚拟机支持作为一等运行时与容器并肩运行。包只需带一个顶层 `vm:` 段——磁盘镜像、内存与 CPU 数——而不是 `image:`，即可选择它。控制平面服务从 URL 下载镜像，用 `qemu-img` 转换为 raw 格式，并缓存在 `vm-images` btrfs 子卷中。安装时，一个 systemd 服务单元以 KVM 加速、virtio 网络与用户态端口转发启动 `qemu-system-x86_64`。VM 镜像可以通过 API 与 UI 列出、上传与删除。

静态页面托管让你直接从 UI 发布 HTML 内容。支持三种来源类型：上传 tar 归档（默认）、从容器镜像中提取文件，或克隆一个 git 仓库。创建对话框中的下拉框选择来源类型，每个 page 都经由共享 ingress 在自己的域名上通过 HTTPS 提供服务，并使用本地 CA 签发的证书。page 与包一样归属于某个网络，因此一个站点可以发布给局域网上的所有人，也可以只发布给某一个 WireGuard 网络的 peer。archive 类型的 page 随时可以通过上传新归档来更新；git 与容器镜像类型的 page 可以按需重建以拉取最新内容。

看看这些[截图](./screenshots/)。这一切在今天的开发任务中都已经能跑起来。

详细的使用说明，包括打包系统、存储、pages 与 API 文档，请访问 **<https://town-os.github.io>**。

## 可启动镜像（PC 与树莓派）

本仓库存放 Town OS 软件本身。**可启动磁盘镜像**由独立的 [install 仓库](https://gitea.com/town-os/install) 构建，它同时也能在虚拟机中启动该镜像：

```bash
git clone https://gitea.com/town-os/install.git
cd install
make deps        # 一次性手动执行 —— 任何构建目标都不会安装软件包
make run         # 构建镜像并在虚拟机中启动它
```

镜像构建默认是本机架构的——不指定 `TARGET` 时，镜像架构与构建主机一致。用 `TARGET` 指定另一种：

| 命令                    | 结果                                                                        |
| -------------------------- | ----------------------------------------------------------------------------- |
| `make image TARGET=x86_64` | PC 镜像（UEFI/GRUB）。仅限 x86_64 主机——不存在 x86 模拟路径。     |
| `make image TARGET=aarch64`| 通用 aarch64 镜像（UEFI/GRUB），例如 Apple Silicon 虚拟机。                  |
| `make image TARGET=rpi`    | **原生启动的树莓派镜像。** 一个镜像同时覆盖 Pi 4/400/CM4 与 Pi 5/CM5。|

**树莓派。** `TARGET=rpi` 只支持 aarch64 且只支持 btrfs，并且经由树莓派自己的 GPU 引导程序与 `config.txt` 启动，而不是 GRUB。在 aarch64 主机上它原生构建；在 x86_64 主机上它在一台完整系统的 `qemu-system-aarch64` 虚拟机内交叉产出——是一整台被模拟的机器，而不是 `binfmt`/qemu-user，也不是交叉编译器，因此构建仍以原生 aarch64 代码运行。它能工作，而且很慢。

把生成的 `-rpi` 镜像刷写到 SD 卡、U 盘或 NVMe：

```bash
make flash RPI=1 USB_DEV=/dev/sdX   # 或者直接 dd town-os-<date>-aarch64-rpi.img
```

对于 **Pi 5 的 NVMe 启动**，还需把 EEPROM 启动顺序设置为包含 NVMe（`rpi-eeprom-config --edit` → `BOOT_ORDER=0xf416`；非 HAT+ 转接卡还需加上 `PCIE_PROBE=1`）；`dtparam=pciex1` 已经写在生成的 `config.txt` 中。两种主板的 USB 电流上限也已在 `config.txt` 中解除（Pi 5 上是 `usb_max_current_enable=1`，Pi 4 上是 `max_usb_current=1`），这一点很重要，因为总线供电的 USB SSD 与 NVMe 转接卡恰恰就是"从 U 盘启动"这类镜像所依赖的设备，而它们在固件默认值下会掉电。

刷写、串口控制台、发布版本与完整变量清单，请参见 [install 仓库的 README](https://gitea.com/town-os/install)。

## 征求意见

请试试开发版构建（在任意 Linux 上执行 `make dev`；详见下文），并把你希望有的功能提交为 [issue](https://gitea.com/town-os/town-os/issues)（需要 Gitea 账户；可以用 GitHub SSO）。我会尽力对所有可能性保持接纳与开放，所以请不要觉得你的想法太大或太疯狂。发出来就好。<3

## 前置条件

在一台全新机器上，最快的路径是 `make deps`：

```bash
make deps
```

它会在基于 Arch 与基于 Debian/Ubuntu 的发行版上安装全部宿主机依赖（Go 1.25、podman、runc、btrfs-progs、libsystemd 头文件、golangci-lint、bun、qemu、构建工具）。它还会在 `ui/` 中运行 `bun install`，使 UI 工具链（eslint、vite、vitest）为 `make lint` 与 `make test` 做好准备。可安全重复运行。

Town OS 在宿主机上所需内容的完整清单：

- Go 1.25+
- [Bun](https://bun.sh)（JS 运行时；`ui/` 的 devDeps 包含 eslint、vite 与 vitest，由 `make deps` 自动安装）
- Podman（以 root 运行，配合 `sudo`）与 `runc` 运行时
- QEMU（`qemu-system-x86_64`、`qemu-img`），用于 VM 包支持
- btrfs-progs（`mkfs.btrfs`）
- libsystemd（systemd 集成所需的开发头文件）
- golangci-lint
- Python 3（测试目标中用于端口分配）

如果你更愿意手工安装全部内容，下面的命令与 `make/deps.sh` 所做的一致。

### Ubuntu / Debian

```bash
sudo apt-get update
sudo apt-get install -y build-essential pkg-config ca-certificates libsystemd-dev \
    btrfs-progs podman runc python3 curl git unzip qemu-system-x86 qemu-utils
```

从 <https://go.dev/dl/> 安装 Go 1.25+，然后：

```bash
curl -fsSL https://bun.sh/install | bash
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sh -s -- -b $(go env GOPATH)/bin
(cd ui && bun install)
```

### Arch Linux / Manjaro

```bash
sudo pacman -Sy --needed base-devel pkgconf systemd btrfs-progs podman runc \
    python curl git unzip qemu-base qemu-img golangci-lint go
```

然后：

```bash
curl -fsSL https://bun.sh/install | bash
(cd ui && bun install)
```

可以选择创建一个 `.env` 文件，为**开发环境**提供仓库凭据：

```
TOWN_OS_REPO_USERNAME=<username>
TOWN_OS_REPO_PASSWORD=<password>
```

`make dev` 会用它们在后端设置默认仓库凭据。密码可以是来自 Gitea 或 GitHub 的 HTTP API key。若省略，则不设置默认凭据，此时只有公开仓库才能在不提供显式凭据的情况下添加。

集成测试（`make test-full`）**不**使用 `.env` 中的凭据。它们运行一个本地 Gitea 实例，使用写死的测试凭据（`town-os` / `town-os-test`），并且在仓库操作中从不联系 GitHub。

安装完前置条件后，执行一次 `make pull-images`，从 Docker Hub 获取所有容器镜像并保存到本检出目录的镜像缓存 `.cache/images/` 中。只有在你撞上速率限制时才需要 Docker Hub 凭据（通过 `.env` 中的 `DOCKER_USERNAME` / `DOCKER_PASSWORD`）。其余所有构建与测试目标都从该缓存加载镜像，从不联系 Docker Hub。若某个目标需要的缓存镜像缺失，`make pull-images` 会自动运行。

## 开发

> **在一台全新机器上，从这里开始：**
>
> - **`make deps`** —— 在 Arch 或 Ubuntu/Debian 上安装全部宿主机依赖（Go、podman、runc、btrfs-progs、libsystemd 头文件、golangci-lint、bun、qemu、构建工具）。可安全重复运行。
> - **`make help`** —— 打印一份分组的、面向用户的 make 目标清单。它也是默认目标，因此直接运行 `make` 也可以。

**在开发服务器运行的同时跑集成测试，可能仍存在一些未解决的问题。此事正在调查中。**

如果你只是想试用，请使用 `stable` 分支（默认分支）。如果你想要最新改动（可能并不稳妥），请使用 `main`。两个分支都会随着改动被认定稳定或被整合进仓库而向前滚动。

运行 `make dev` 会构建测试镜像、创建开发用 btrfs 卷、在 5309 端口启动后端容器，并启动带热重载的 Vite 开发服务器。运行起来之后，通过 `http://<hostname>:5173` 访问 UI。

宿主机上的 5309（后端 API）与 5173（Vite 开发服务器）端口必须可访问。

| 目标               | 说明                                                                                 |
| -------------------- | ------------------------------------------------------------------------------------- |
| `make dev`           | 启动完整开发环境（后端 + Vite 开发服务器）。                                 |
| `make dev-stop`      | 停止并移除本工作树的开发后端容器。                            |
| `make dev-stop-all`  | 停止宿主机上每一个 `town-os-dev-*` 容器，而不仅是本工作树的。             |
| `make dev-logs`      | 跟踪正在运行的开发容器内的 journalctl。                                           |
| `make btrfs-dev`     | 为开发环境创建一个全新的 50GB btrfs 环回卷。                          |
| `make dev-btrfs`     | 仅在尚未挂载时创建开发 btrfs 卷（由 `dev` 自动使用）。|
| `make clean-btrfs-dev` | 卸载、分离 loop 设备并移除开发 btrfs 卷。                            |
| `make clean-dev`     | 停止开发容器并拆除开发 btrfs 卷。会移除 `dev-data/`。             |

## Makefile 目标

所有目标都使用一个由工作目录路径推导出的唯一 `INSTANCE_ID`，因此多个检出可以并发运行而互不冲突。临时状态（端口文件、btrfs 卷、开发数据）位于 `/tmp/town-os-$(INSTANCE_ID)/`。

### 测试

| 目标                          | 说明                                                                                                                                          |
| ------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| `make test`                     | 运行代码检查、Go 单元测试与 JS 单元测试。                                                                                          |
| `make test-race`                | 运行代码检查，并在竞态检测器下跑 Go 单元测试。比 `test` 慢，且仅限 Go（bun 没有对应物）；它报告的是待分诊的发现，而非充当门禁。 |
| `make test-integration`         | 构建测试镜像，并在带 systemd 与 btrfs 的特权 podman 容器内运行 Go 集成测试。退出时清理 btrfs 环回。     |
| `make test-integration-build`   | 构建测试镜像并启动集成测试容器（已加载全部镜像），但不运行任何测试。适合为 `test-integration-rerun` 做准备。 |
| `make test-integration-rerun`   | 在已经运行的容器中重跑集成测试（来自先前的 `test-integration-build`）。跳过镜像构建。                              |
| `make test-ui-unit`             | 只运行 JS（bun）UI 单元测试。                                                                                                 |
| `make test-ui-integration`      | 针对一个后端容器运行 JS（bun）UI 集成测试。退出时清理 btrfs 环回。                                                     |
| `make test-ui-integration-local`| 针对本地运行的后端（无容器）运行 UI 集成测试。                                                                           |
| `make test-full`                | 依次运行 `test`、`test-integration` 与 `test-ui-integration`。使用信号陷阱保证清理。                                    |
| `make test-full-log`            | 运行 `test-full` 并把全部输出 tee 到 `/tmp/town-os/log/` 下带时间戳的日志文件。                                                                |
| `make auto-test`                | 监视 `.go`/`.js` 文件变化并自动重跑 `make test`。需要时会安装 [reflex](https://github.com/cespare/reflex)。          |
| `make auto-test-full`           | 监视文件变化并自动重跑 `make test-full`。需要时会安装 reflex。                                                       |

用 `TEST_RUN=<regex>` 过滤要运行哪些集成测试（例如 `make test-integration TEST_RUN=TestInstall`）。用 `TEST_TIMEOUT=<duration>` 覆盖默认的 60 分钟超时。

**一次测试运行绝不会与另一次、或与 `make dev` 冲突。** 测试容器以宿主机网络运行（这是刻意的——桥接网络的 DNS 在强制门户网络下会失效），因此它启动的每个系统服务都绑定在宿主机的网络命名空间中，`make dev` 启动的一切也是如此。因此测试框架为每次运行分配各自的临时端口给 rolodex、node-exporter、Prometheus、监控 UI 与 ingress，并分配各自的 WireGuard 盐值，使接口名、监听端口与 overlay 子网按检出与按角色各不相同。`make dev` 刻意不接受这些覆盖：它意在镜像一台真实机器，那里 DNS 在 `:53`、ingress 在 `:443`。

### 代码检查

| 目标      | 说明                       |
| ----------- | --------------------------------- |
| `make lint` | 运行 `go vet` 与 `golangci-lint`。 |

### 本地 Registry

集成测试使用一个本地 `registry:2` 容器，以避免 Docker Hub 的速率限制。当你运行 `make test-integration` 或 `make test-ui-integration` 时，构建会自动：

1. 发现测试包仓库引用的所有 `docker.io` 镜像（`discover-images` 工具）
2. 在随机端口上启动一个本地 registry
3. 从镜像缓存加载每个镜像并推送到本地 registry
4. 生成一个 `registries.conf`，把 `docker.io` 的拉取重定向到本地镜像源
5. 把该配置挂载进测试容器

这一切是透明的——无需修改任何代码。所有镜像都从检出目录的镜像缓存（`.cache/images/`）加载；测试流水线期间不会发生任何 docker.io 拉取。

| 目标                   | 说明                                               |
| ------------------------ | --------------------------------------------------------- |
| `make registry`          | 启动本地 registry 容器。                       |
| `make registry-populate` | 把发现到的 docker.io 镜像镜像化到本地 registry。 |
| `make registry-stop`     | 停止并移除本地 registry 容器。             |

每个工作目录都有自己的 registry 实例（通过 `INSTANCE_ID` 区分），因此并发的测试运行不会冲突。

### 本地 Gitea 服务器

集成测试还会使用一个本地 Gitea 实例，以避免直接从 GitHub 克隆测试包仓库。当你运行 `make test-integration` 或 `make test-ui-integration` 时，构建会自动：

1. 在随机端口上启动一个本地 Gitea 服务器
2. 创建一个管理员用户
3. 把测试包仓库作为裸克隆缓存在 `.cache/git-repos/` 中（后续运行会 fetch 以刷新）
4. 通过 go-git 把缓存的仓库推送进 Gitea（`populate-repos` 工具）
5. 把 `TOWN_OS_TEST_REPO_CORE_URL` 与 `TOWN_OS_TEST_REPO_EXTRAS_URL` 环境变量传入测试容器，使所有 git 克隆都命中本地 Gitea 实例

`.cache/git-repos/` 中的裸克隆缓存跨 Gitea 重启留存，因此只有第一次运行会访问 GitHub。后续运行只 fetch 更新并推送到新的 Gitea 实例。`make clean` 会移除该缓存。

这一切是透明的——无需修改任何代码。若未设置这些环境变量，测试会回退到 GitHub 的 URL。

| 目标                | 说明                                                |
| --------------------- | ---------------------------------------------------------- |
| `make gitea`          | 启动本地 Gitea 容器并创建管理员用户。 |
| `make gitea-populate` | 把测试仓库从 GitHub 迁移进本地 Gitea。       |
| `make gitea-stop`     | 停止并移除本地 Gitea 容器。             |

每个工作目录都有自己的 Gitea 实例（通过 `INSTANCE_ID` 区分），因此并发的测试运行不会冲突。

### 构建

| 目标                        | 说明                                                                                                            |
| ----------------------------- | ---------------------------------------------------------------------------------------------------------------------- |
| `make production-image`       | 构建生产基础镜像（用于集成测试）。                                                               |
| `make dev-production-image`   | 构建生产基础镜像（用于开发）。                                                                             |
| `make test-image`             | 构建测试容器镜像（包含集成测试二进制）。                                                     |
| `make dev-image`              | 构建开发容器镜像。                                                                                         |
| `make ui-integration-image`   | 构建 UI 集成测试容器镜像。                                                                         |
| `make build-networkcontroller`| 在本地构建网络控制器二进制（`town-os-networkcontroller`）。                                             |
| `make ui-image`               | 在本地把 UI 镜像构建为 `localhost/town-os-ui:<INSTANCE_ID>` 供测试使用（从不拉取 quay 上的 UI 镜像）。          |
| `make nc-image`               | 在本地构建网络控制器镜像供测试使用；`make nc-image-dev` 针对开发基础镜像做同样的事。          |
| `make ingress-image`          | 在本地构建 ingress 镜像供测试使用。                                                                             |
| `make pull-images`            | 从 Docker Hub 拉取所有容器镜像并保存到检出目录的镜像缓存。若有缓存镜像缺失会自动运行。 |

开发与集成使用各自独立的生产基础镜像与构建缓存，因此并发构建不会互相干扰。

### 发布与推送

所有发布镜像都推送到 `quay.io/town/`。所有推送标签都按架构分区：每台主机推送其本机架构，形式为 `rc.<date>-<arch>` / `rc.latest-<arch>`（候选发布）或 `release.<date>-<arch>` / `latest-<arch>`（正式发布），其中 `<arch>` 是 `uname -m` 的原始形式——`x86_64` 或 `aarch64`，而*不是* OCI 平台名 `amd64`/`arm64`。不带后缀的普通名称（`rc.latest`、`latest` 与日期标签）仅作为多架构 manifest 列表存在，由 `make manifest-rc` / `make manifest-release` 在每种架构都推送完成之后组装；普通名称绝不能作为单架构标签推送，因为它在另一种架构上会以 `exec format error` 失败。

在运行时，system controller 从同一个值推导出每一个同族镜像标签（UI、Rolodex、网络控制器、ingress）：若设置了 `TOWN_OS_TAG` 环境变量则取之，否则取 `rc.latest-<arch>`。不存在编译期的版本固定——install 构建系统通过在 system controller 的 systemd 单元上设置 `TOWN_OS_TAG` 来固定某个发布版本，而在没有覆盖时，机器始终跟踪 `rc.latest-<arch>`。

| 目标                      | 说明                                                                                                        |
| --------------------------- | ------------------------------------------------------------------------------------------------------------ |
| `make release-image`        | 构建发布用的 system controller 镜像（`quay.io/town/town`）。                                                   |
| `make release-ui-image`     | 构建发布用的 UI 镜像（`quay.io/town/ui`）。                                                                    |
| `make release-proton-image` | 构建发布用的 Proton 运行器镜像（`quay.io/town/proton`）。**需要 `PROTON_ENABLED=1`**；否则该目标不存在。 |
| `make release-nc-image`     | 构建发布用的网络控制器镜像（`quay.io/town/networkcontroller`）。                                     |
| `make release-ingress-image`| 构建发布用的 ingress 镜像（`quay.io/town/ingress`）。                                                          |
| `make release-build`        | 拉取镜像、运行 `test-full`，然后构建发布镜像。当 `PROTON_ENABLED=1` 时包含 Proton 运行器。    |
| `make push`                 | `push-rc` 的别名。                                                                                               |
| `make push-rc`              | 把所有镜像（system controller、UI、网络控制器；`PROTON_ENABLED=1` 时含 Proton）作为按架构的候选发布推送（`rc.<date>-<arch>` + `rc.latest-<arch>`）。 |
| `make manifest-rc`          | 从按架构的标签组装并推送普通的 `rc.<date>` / `rc.latest` 多架构 manifest 列表。在每种架构都推送完成之后运行一次。 |
| `make push-release`         | 运行 `release-build`，然后把所有镜像作为按架构的正式发布推送（`release.<date>-<arch>` + `latest-<arch>`）。        |
| `make manifest-release`     | 从按架构的标签组装并推送普通的 `release.<date>` / `latest` 多架构 manifest 列表。在每种架构都推送完成之后运行一次。 |
| `make push-ui-rc`           | 只把 UI 镜像作为按架构的候选发布推送（`rc.<date>-<arch>` + `rc.latest-<arch>`）。                  |
| `make push-ui-release`      | 只把 UI 镜像作为按架构的正式发布推送（`release.<date>-<arch>` + `latest-<arch>`）。                          |
| `make push-proton-rc`       | 只把 Proton 运行器镜像作为候选发布推送。**需要 `PROTON_ENABLED=1`**。                          |
| `make push-proton-release`  | 只把 Proton 运行器镜像作为正式发布推送。**需要 `PROTON_ENABLED=1`**。                                    |
| `make push-nc-rc`           | 只把网络控制器镜像作为按架构的候选发布推送（`rc.<date>-<arch>` + `rc.latest-<arch>`）。  |
| `make push-nc-release`      | 只把网络控制器镜像作为按架构的正式发布推送（`release.<date>-<arch>` + `latest-<arch>`）。          |
| `make push-ingress-rc`      | 只把 ingress 镜像作为按架构的候选发布推送（`rc.<date>-<arch>` + `rc.latest-<arch>`）。             |
| `make push-ingress-release` | 只把 ingress 镜像作为按架构的正式发布推送（`release.<date>-<arch>` + `latest-<arch>`）。                     |
| `make push-tag PUSH_TAG=x`  | 用自定义标签 `x` 构建并推送所有镜像。当 `PROTON_ENABLED=1` 时包含 Proton 运行器。               |

### Registry 认证

| 目标             | 说明                                                                                       |
| ------------------ | ------------------------------------------------------------------------------------------------- |
| `make docker-login`| 使用 `.env` 中的 `DOCKER_USERNAME` / `DOCKER_PASSWORD` 登录 Docker Hub。未设置则跳过。 |
| `make quay-login`  | 使用 `.env` 中的 `QUAY_USERNAME` / `QUAY_PASSWORD` 登录 Quay.io。未设置则跳过。        |

### Btrfs 管理

| 目标                 | 说明                                                                 |
| ---------------------- | --------------------------------------------------------------------------- |
| `make btrfs`           | 为集成测试创建一个 50GB btrfs 环回卷。                  |
| `make clean-btrfs`     | 卸载、分离 loop 设备并移除集成测试的 btrfs 卷。 |
| `make btrfs-dev`       | 为开发环境创建一个 50GB btrfs 环回卷。                |
| `make clean-btrfs-dev` | 卸载、分离 loop 设备并移除开发 btrfs 卷。              |
| `make dev-btrfs`       | 仅在尚未挂载时创建开发 btrfs 卷。              |

开发环境与集成测试环境使用各自独立的 btrfs 卷、容器镜像与构建缓存，因此它们可以并发运行而互不冲突。

### 清理

`make test-full` 在测试完成后会自动清理所有集成容器、registry、Gitea 与 btrfs 环回卷。一个 shell EXIT 陷阱确保即使被信号中断也会执行清理。每个集成测试目标（`test-integration`、`test-ui-integration`）也各自使用 EXIT 陷阱，以保证无论配方如何终止（成功、失败或被信号中断）btrfs 环回都会被清理。`clean-btrfs` 目标包含一道安全网，会扫描当前目录下由 btrfs 镜像支撑的孤立 loop 设备，以应对跟踪文件缺失的情况。

| 目标                   | 说明                                                                             |
| ------------------------ | --------------------------------------------------------------------------------------- |
| `make clean`             | 移除 `.cache/` 构建缓存目录。                                             |
| `make clean-dev`         | 停止所有开发容器，拆除开发 btrfs，移除 dev-data/dev-repos。                |
| `make clean-cache`       | 从临时状态目录中移除开发数据、开发仓库与开发 Rolodex 数据。    |
| `make clean-integration` | 移除集成测试容器（test、UI 后端、UI 运行器）并清理 btrfs。       |
| `make clean-btrfs`       | 卸载并移除集成测试的 btrfs 卷与孤立的 loop 设备。         |
| `make clean-image-cache` | 删除本检出目录的镜像缓存（`.cache/images/`）。                           |
| `make clean-containers`  | 移除任意工作目录/实例下的所有 town-os 与 preflight 容器。      |
| `make clean-all`         | 清理一切：所有容器、构建缓存、开发环境、集成测试与 btrfs。             |

### 预检检查

| 目标            | 说明                                                                                                    |
| ----------------- | -------------------------------------------------------------------------------------------------------------- |
| `make preflight-dev` | 校验开发环境：检查 podman、btrfs-progs、仓库凭据与桥接网络。 |

### SSH

| 目标     | 说明                                                                                   |
| ---------- | --------------------------------------------------------------------------------------------- |
| `make ssh` | SSH 登录到 `town-os.local` 上正在运行的 Town OS 设备（自动清除陈旧的主机密钥）。 |

### 依赖检查

这些通常不会被直接调用；它们作为其他目标的前置条件运行。

| 目标                     | 说明                                  |
| -------------------------- | -------------------------------------------- |
| `make check-go`            | 验证 `go` 可用。                    |
| `make check-bun`           | 验证 `bun` 可用。                   |
| `make check-podman`        | 验证 `podman` 可用。                |
| `make check-runc`          | 验证 `runc` 可用。                  |
| `make check-btrfs`         | 验证 `mkfs.btrfs` 可用。            |
| `make check-golangci-lint` | 验证 `golangci-lint` 可用。         |
| `make check-python3`       | 验证 `python3` 可用。               |
| `make check-libsystemd`    | 验证 libsystemd 开发头文件存在。 |

## 许可证

GNU Affero GPL 3.0
