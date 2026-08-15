[![Town OS](./banner.png)](https://town-os.github.io)

**主頁：<https://town-os.github.io>** | [原始碼 (Gitea)](https://gitea.com/town-os/town-os) | [鏡像 (GitHub)](https://github.com/town-os/town-os)

> **把你的雲放進自己的儲物間，任何人都能用。**

Town OS 是一個完全從 U 盤執行的自助式平台，能把任何一台電腦變成個人雲伺服器。它以容器方式管理自己的儲存、網路與服務——無需安裝。它是為所有人設計的，而不只是為專家：一個友好的介面會引導你完成一切，因此除非你自己想了解，否則不必知道這些東西是怎麼運作的。

**GITHUB 只是鏡像：** 源倉庫位於 <https://gitea.com/town-os/town-os>。

> **中文翻譯（繁體）。** 本文件是 [README.md](README.md) 的繁體中文譯本，
> **英文原件為準**。簡體中文譯本見 [README.zh-CN.md](README.zh-CN.md)。
> 倉庫中的其他文件也有中文譯本：
> [CLAUDE.zh-TW.md](CLAUDE.zh-TW.md)（建置與程式碼風格規則；簡體：[CLAUDE.zh-CN.md](CLAUDE.zh-CN.md)）與
> [DESIGN.zh-TW.md](DESIGN.zh-TW.md)（架構與功能規格說明；簡體：[DESIGN.zh-CN.md](DESIGN.zh-CN.md)）。
> 另有西班牙語譯本，分西班牙與墨西哥兩種地區變體：
> [README.es-ES.md](README.es-ES.md)、[CLAUDE.es-ES.md](CLAUDE.es-ES.md)、[DESIGN.es-ES.md](DESIGN.es-ES.md)；
> [README.es-MX.md](README.es-MX.md)、[CLAUDE.es-MX.md](CLAUDE.es-MX.md)、[DESIGN.es-MX.md](DESIGN.es-MX.md)。
> 另有日語譯本：
> [README.ja-JP.md](README.ja-JP.md)、[CLAUDE.ja-JP.md](CLAUDE.ja-JP.md)、[DESIGN.ja-JP.md](DESIGN.ja-JP.md)。
> 兩者出現分歧時以英文原件為準，並應修正譯文。程式碼識別符號、檔案路徑、命令、
> 環境變數、make 目標名與 API 路徑一律保留原文，不作翻譯。

## 目錄

- [為什麼這很重要](#為什麼這很重要)
- [已有與計劃中的功能](#已有與計劃中的功能)
- [可啟動鏡像（PC 與樹莓派）](#可啟動鏡像pc-與樹莓派)
- [徵求意見](#徵求意見)
- [前置條件](#前置條件)
- [開發](#開發)
- [Makefile 目標](#makefile-目標)
    - [測試](#測試)
    - [程式碼檢查](#程式碼檢查)
    - [本地 Registry](#本地-registry)
    - [本地 Gitea 伺服器](#本地-gitea-伺服器)
    - [構建](#構建)
    - [釋出與推送](#釋出與推送)
    - [Registry 認證](#registry-認證)
    - [Btrfs 管理](#btrfs-管理)
    - [清理](#清理)
    - [預檢檢查](#預檢檢查)
    - [SSH](#ssh)
    - [依賴檢查](#依賴檢查)
- [許可證](#許可證)

## 為什麼這很重要

你的資料應當住在你自己家裡，而不是別人的電腦上。雲服務確實方便，但它帶來的是月費、隱私上的取捨，以及一家公司隨時可能更改條款、漲價或關停的風險。Town OS 給你同樣的便利，而不必交出控制權。

使用它不需要你懂技術。插上 U 盤，開機，你就有了一套能用的系統。升級就是換一隻 U 盤。重置就是重啟。萬一出了問題，你總能回到一個可用的狀態——你不可能把自己鎖在門外。

Town OS 的設計目標，是讓任何人都能幫家人在家裡跑起各種服務。給父母搭一台媒體伺服器，讓孩子的裝置遠離間諜軟體，或者託管你自己的網站——全都不必向雲服務商申請許可。

## 已有與計劃中的功能

打包系統與儲存、網路完全一體化，按需建立資源，包括通過 UPnP 開放埠、經由按包劃分的網路控制器管理埠轉發，或者建立隧道。

Town OS 執行自己的本地解析器（Rolodex），並由系統自身編排：每個已安裝的包都會在一個私有 TLD 之下獲得一個名稱（`plex.default.home`），因此服務可以按名稱而非埠號訪問。同一個解析器也承擔路由器級別的過濾——DNSBL 域名黑名單與 RBL 反向 IP 列表會按需向你選擇的提供方區域發起查詢（Spamhaus、SURBL、URIBL 等以一鍵新增的形式提供），另有一份你手工維護的本地黑名單。這就是你讓孩子的平板、或整個家庭遠離廣告軟體與間諜軟體的方式，既不下載訂閱源，也不會在你背後快取任何東西。

網路可以切分為 WireGuard overlay。每個網路擁有自己的 TLD 與自己的 overlay 子網，而且這種劃分是真實的：區域網上的筆記型電腦能解析每一個網路，而通過隧道接入某一個網路的手機只能解析那個網路與公共網際網路，別無其他——同級網路的名稱對它而言根本不存在。Peer 從 UI 登記（也可以從一個你只授予了"在某一個網路上登記裝置"權限的帳戶登記），獲得一份可直接匯入的配置，並被告知它實際撥打的那個地址，因此同一 Wi-Fi 上的手機不會被要求繞經公網 IP 迴環。登記帶有 TTL 並會自行過期，因此被棄用的裝置不會永遠佔著一個 overlay 地址。Networks 介面列出此刻誰在連線——裝置、帳戶、overlay 地址、即時握手與傳輸量——並帶一個斷開按鈕。

所有 HTTP 內容都按名稱經 HTTPS 提供。Town OS 執行自己的證書頒發機構；每個包與每個 page 都為其 FQDN 獲得一張葉子證書，而單一的共享 ingress 在 `:443` 上終止 TLS，並依據 SNI 選出正確的服務。從 `/tls/ca.crt` 取一次根證書，瀏覽器就不再抱怨了。同一張證書在區域網與 WireGuard 隧道中都有效，因為 ingress 在所有介面上監聽，並按名稱而非地址路由。

儲存系統是與打包系統一併設計的，以支援升級，也支援臨時解除安裝與日後恢復。儲存按包做了獨立分割槽，從而可以按成本與可用性優先順序制定災備策略。配額用於避免儲存需求給使用者帶來意外。

你的檔案有了自己的家。物件儲存（gfeh）為每個網路執行一個守護程序，各自擁有自己的使用者、自己的權限與自己的那片磁碟，並同時以四種方式釋出同一批檔案——一個 S3 端點、純 HTTPS、一個 Google Drive 相容檢視，以及 IPFS——全部按名稱提供，全部位於同一個 ingress 與同一張證書之後。使用者是一棵樹而不是一張平鋪列表，因此你可以把某個分支連同其下的一切交給某個人；檔案也可以作為連結分享出去，並在日後撤回。Object Storage 介面展示每個網路的分割槽、其中有誰、他們能訪問什麼，以及當前釋出的每一個連結。搭建這台機器的人會被自動安置進 home 分割槽，因為一個還得自己給自己授權才能用的檔案庫，根本算不上檔案庫。

包可以向用戶索取輸入——類似 debconf——但是通過 UI 進行（可以看看截圖）。這些是模板變數，可用於配置容器鏡像與管理網路。問題是帶型別的，而型別確實在幹活：埠留空會自動分配，secret 會以 256 位熵生成而不是要你自己想一個，布林值渲染為核取方塊，問題可以標記為可選從而讓空答案意味著"不設定它"，一組進階選項還能用 `show_if` 收納到一個核取方塊之後，這樣對話方塊就不會是一堵欄位牆。`oauth` 型別的問題取代了你過去手工執行的那個 shell 指令碼：點選 Connect，在供應商自己的頁面上授權，令牌就落進了欄位裡——而且 Town OS 裡沒有烘焙任何供應商清單，因為 URL 是由包自己指明的。

包還可以定義 Go `text/template` 檔案模板，在安裝時渲染進卷中，並可訪問使用者應答、包後設資料與系統資訊（主機名、各 IP）。模板在卷播種之後、服務啟動之前應用，且已有檔案絕不會被覆蓋。包可以依賴其他包：依賴會自動安裝，與父包共享一個私有容器網路從而按名稱而非經由宿主機埠通訊，能在安裝時互相傳遞值（例如資料庫的容器名與埠），並且能以雙方共同選擇加入的方式共享儲存卷。[包倉庫](https://github.com/town-os/default-packages) 裡有更多資訊。你也可以用一份私有倉庫清單完全替換掉這些倉庫——非常適合你的遊戲夥伴、需要你支援的家人等等。這方面預計會有大量擴充。

所有服務都有充分的日誌與監管。有一個舒適的 UI 來訪問這些資訊，其呈現方式意在讓非技術使用者也能安全地閱讀。服務以樹狀展示——一個包與巢狀在其下的依賴——你可以一次性對整棵樹執行操作，或在一個檢視中讀取樹中每一個單元的合併日誌。管理員與普通使用者是分開的帳戶：如果你願意，你可以幫父母跑一台 Plex（或類似的東西），也可以讓他們遠離間諜軟體。在"管理員"與"普通使用者"之間還有授權（grant）——勾一個框，就能讓某個帳戶在某一個網路上登記裝置，或者執行那個網路的檔案庫，僅此而已。授權沒有解鎖的東西預設保持關閉，因此明年新增的某項能力不會被悄悄交給已經存在的帳戶。

給機器做更新時，它會把過程攤開給你看。system controller 在開始啟動之前就繫結它的埠，因此 UI 會即時流式展示進度——控制器、DNS、系統服務，然後每重啟一個包就多一行——而不是對著一個死埠轉圈。它為每一次程序化身打上一個 id，因此瀏覽器能區分"舊版本還在應答"與"新版本已經起來"，並知道這次更新究竟落地了沒有。

整個介面都做了國際化。所有面向使用者的字串——後端錯誤訊息與前端 UI 文字——都經由一個以 BCP 47 語言環境代碼（例如 `en-US`、`de-DE`）為鍵的訊息目錄路由。**有 30 種語言在前後端都完整翻譯**：阿拉伯語、孟加拉語、中文（簡體與繁體）、克羅埃西亞語、捷克語、丹麥語、荷蘭語、英語、芬蘭語、法語、德語、印地語、匈牙利語、義大利語、日語、韓語、波蘭語、葡萄牙語、羅馬尼亞語、俄語、梵語、斯洛伐克語、斯洛維尼亞語、西班牙語、瑞典語、泰語、土耳其語、烏克蘭語與越南語。

在這些語言之上還有 **18 種國家變體**——`en-GB`、`en-AU`、`en-CA`、`en-IN`、`en-NZ`、`en-ZA`、`de-AT`、`de-CH`、`fr-BE`、`fr-CA`、`fr-CH`、`es-AR`、`es-MX`、`pt-PT`、`nl-BE`、`ar-AE`、`ar-EG`、`bn-IN`——合計 **48 個可選的語言環境**。變體並不是第二份翻譯：它繼承所屬語言的目錄，只列出該國家確實說得不一樣的那些字串，因此一則新訊息在其基礎語言拿到它的那一刻就抵達每一個國家，而對一條法語字串的修正只需做一次，而不是在四個檔案裡各做一遍。其中有幾份覆寫清單坦然就是空的。加拿大英語保留美式的 *-ize* 拼寫；阿根廷西班牙語以 *voseo* 聞名——但 voseo 替換的是親近的 *tú*，而本目錄全程以 *usted* 稱呼讀者，這在布宜諾斯艾利斯與在馬德里讀起來並無二致。空清單仍然意味著已審閱，而不是被遺忘。系統設定中的語言介面以母語文字呈現 21 種常用語言，並帶一個可展開的、包含 89 個國家/地區代碼的清單；沒有對應目錄的會顯示出來但被停用。

UI 從你的瀏覽器（`navigator.languages`）選擇語言。精確匹配優先，因此 `de-CH` 得到的是瑞士德語，而不是被摺疊到德國的那一份。中文按文字消歧。對於我們沒有提供任何目錄的國家，語言會落到它實際閱讀的那一份——`es-CO` 落到墨西哥西班牙語而不是半島西班牙語，`en-IE` 落到英式而不是美式——而一個光禿禿的 `en` 或 `pt` 會解析到一個有名有姓的預設值，而不是碰巧最先載入的那個變體。明確的選擇會按瀏覽器記住，因此這台機器的全域語言設定不再覆蓋每個人所看到的內容——它只是 Town OS 沒有對應目錄的語言的回退值。

Windows 應用可以藉助 Valve 的 Proton 相容層與原生 Linux 容器並肩執行。帶 `proton` 段的包定義會指明一個 Windows 應用鏡像、一個提取目錄與一個執行檔路徑；系統從 OCI 鏡像拉取該應用，把它提取進一個持久卷，並在 Proton 執行器容器中執行它。執行器鏡像通過系統級的 `proton_image` 設定配置。**Proton 支援是選擇加入的**：用 `make PROTON_ENABLED=1 …`（或 `go build -tags proton`）重新構建才會把它編譯進去。沒有該標籤時，包 YAML 中的 `proton:` 塊會在安裝時被拒絕，`proton_image` 設定不會被播種，設定 UI 會省略 Proton 卡片，釋出流水線也不會構建或推送該執行器鏡像。

內建的監控棧開箱即用地提供系統可觀測性。Prometheus 採集指標，Node Exporter 報告主機級統計，兩者都作為系統服務管理，無需手動配置。預設情況下儀表盤直接用 uPlot（約 35 KB）在 UI 中渲染——磁碟 I/O、網路、CPU 與記憶體——從而把約 771 MB 的 Grafana 鏡像整個擋在機器之外。把 `monitoring_backend` 設定切換為 `grafana`，則會拉取並啟動完整的 Grafana 棧，並預置一個數據源與兩個儀表盤。

QEMU 虛擬機器支援作為一等執行時與容器並肩執行。包只需帶一個頂層 `vm:` 段——磁碟鏡像、記憶體與 CPU 數——而不是 `image:`，即可選擇它。控制平面服務從 URL 下載鏡像，用 `qemu-img` 轉換為 raw 格式，並快取在 `vm-images` btrfs 子卷中。安裝時，一個 systemd 服務單元以 KVM 加速、virtio 網路與使用者態埠轉發啟動 `qemu-system-x86_64`。VM 鏡像可以通過 API 與 UI 列出、上傳與刪除。

靜態頁面託管讓你直接從 UI 釋出 HTML 內容。支援三種來源型別：上傳 tar 歸檔（預設）、從容器鏡像中提取檔案，或克隆一個 git 倉庫。建立對話方塊中的下拉框選擇來源型別，每個 page 都經由共享 ingress 在自己的域名上通過 HTTPS 提供服務，並使用本地 CA 簽發的證書。page 與包一樣歸屬於某個網路，因此一個站點可以釋出給區域網上的所有人，也可以只發布給某一個 WireGuard 網路的 peer。archive 型別的 page 隨時可以通過上傳新歸檔來更新；git 與容器鏡像型別的 page 可以按需重建以拉取最新內容。

看看這些[截圖](./screenshots/)。這一切在今天的開發任務中都已經能跑起來。

詳細的使用說明，包括打包系統、儲存、pages 與 API 文件，請訪問 **<https://town-os.github.io>**。

## 可啟動鏡像（PC 與樹莓派）

本倉庫存放 Town OS 軟體本身。**可啟動磁碟鏡像**由獨立的 [install 倉庫](https://gitea.com/town-os/install) 構建，它同時也能在虛擬機器中啟動該鏡像：

```bash
git clone https://gitea.com/town-os/install.git
cd install
make deps        # 一次性手動執行 —— 任何構建目標都不會安裝軟體包
make run         # 構建鏡像並在虛擬機器中啟動它
```

鏡像構建預設是本機架構的——不指定 `TARGET` 時，鏡像架構與構建主機一致。用 `TARGET` 指定另一種：

| 命令                    | 結果                                                                        |
| -------------------------- | ----------------------------------------------------------------------------- |
| `make image TARGET=x86_64` | PC 鏡像（UEFI/GRUB）。僅限 x86_64 主機——不存在 x86 模擬路徑。     |
| `make image TARGET=aarch64`| 通用 aarch64 鏡像（UEFI/GRUB），例如 Apple Silicon 虛擬機器。                  |
| `make image TARGET=rpi`    | **原生啟動的樹莓派鏡像。** 一個鏡像同時覆蓋 Pi 4/400/CM4 與 Pi 5/CM5。|

**樹莓派。** `TARGET=rpi` 只支援 aarch64 且只支援 btrfs，並且經由樹莓派自己的 GPU 載入程式與 `config.txt` 啟動，而不是 GRUB。在 aarch64 主機上它原生構建；在 x86_64 主機上它在一台完整系統的 `qemu-system-aarch64` 虛擬機器內交叉產出——是一整台被模擬的機器，而不是 `binfmt`/qemu-user，也不是交叉編譯器，因此構建仍以原生 aarch64 程式碼執行。它能工作，而且很慢。

把生成的 `-rpi` 鏡像刷寫到 SD 卡、U 盤或 NVMe：

```bash
make flash RPI=1 USB_DEV=/dev/sdX   # 或者直接 dd town-os-<date>-aarch64-rpi.img
```

對於 **Pi 5 的 NVMe 啟動**，還需把 EEPROM 啟動順序設定為包含 NVMe（`rpi-eeprom-config --edit` → `BOOT_ORDER=0xf416`；非 HAT+ 轉接卡還需加上 `PCIE_PROBE=1`）；`dtparam=pciex1` 已經寫在生成的 `config.txt` 中。兩種主機板的 USB 電流上限也已在 `config.txt` 中解除（Pi 5 上是 `usb_max_current_enable=1`，Pi 4 上是 `max_usb_current=1`），這一點很重要，因為匯流排供電的 USB SSD 與 NVMe 轉接卡恰恰就是"從 U 盤啟動"這類鏡像所依賴的裝置，而它們在韌體預設值下會掉電。

刷寫、序列埠控制台、釋出版本與完整變數清單，請參見 [install 倉庫的 README](https://gitea.com/town-os/install)。

## 徵求意見

請試試開發版構建（在任意 Linux 上執行 `make dev`；詳見下文），並把你希望有的功能提交為 [issue](https://gitea.com/town-os/town-os/issues)（需要 Gitea 帳戶；可以用 GitHub SSO）。我會盡力對所有可能性保持接納與開放，所以請不要覺得你的想法太大或太瘋狂。發出來就好。<3

## 前置條件

在一台全新機器上，最快的路徑是 `make deps`：

```bash
make deps
```

它會在基於 Arch 與基於 Debian/Ubuntu 的發行版上安裝全部宿主機依賴（Go 1.25、podman、runc、btrfs-progs、libsystemd 標頭檔案、golangci-lint、bun、qemu、構建工具）。它還會在 `ui/` 中執行 `bun install`，使 UI 工具鏈（eslint、vite、vitest）為 `make lint` 與 `make test` 做好準備。可安全重複執行。

Town OS 在宿主機上所需內容的完整清單：

- Go 1.25+
- [Bun](https://bun.sh)（JS 執行時；`ui/` 的 devDeps 包含 eslint、vite 與 vitest，由 `make deps` 自動安裝）
- Podman（以 root 執行，配合 `sudo`）與 `runc` 執行時
- QEMU（`qemu-system-x86_64`、`qemu-img`），用於 VM 包支援
- btrfs-progs（`mkfs.btrfs`）
- libsystemd（systemd 整合所需的開發標頭檔案）
- golangci-lint
- Python 3（測試目標中用於埠分配）

如果你更願意手工安裝全部內容，下面的命令與 `make/deps.sh` 所做的一致。

### Ubuntu / Debian

```bash
sudo apt-get update
sudo apt-get install -y build-essential pkg-config ca-certificates libsystemd-dev \
    btrfs-progs podman runc python3 curl git unzip qemu-system-x86 qemu-utils
```

從 <https://go.dev/dl/> 安裝 Go 1.25+，然後：

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

然後：

```bash
curl -fsSL https://bun.sh/install | bash
(cd ui && bun install)
```

可以選擇建立一個 `.env` 檔案，為**開發環境**提供倉庫憑據：

```
TOWN_OS_REPO_USERNAME=<username>
TOWN_OS_REPO_PASSWORD=<password>
```

`make dev` 會用它們在後端設定預設倉庫憑據。密碼可以是來自 Gitea 或 GitHub 的 HTTP API key。若省略，則不設定預設憑據，此時只有公開倉庫才能在不提供顯式憑據的情況下新增。

整合測試（`make test-full`）**不**使用 `.env` 中的憑據。它們執行一個本地 Gitea 實例，使用寫死的測試憑據（`town-os` / `town-os-test`），並且在倉庫操作中從不聯絡 GitHub。

安裝完前置條件後，執行一次 `make pull-images`，從 Docker Hub 獲取所有容器鏡像並儲存到本檢出目錄的鏡像快取 `.cache/images/` 中。只有在你撞上速率限制時才需要 Docker Hub 憑據（通過 `.env` 中的 `DOCKER_USERNAME` / `DOCKER_PASSWORD`）。其餘所有構建與測試目標都從該快取載入鏡像，從不聯絡 Docker Hub。若某個目標需要的快取鏡像缺失，`make pull-images` 會自動執行。

## 開發

> **在一台全新機器上，從這裡開始：**
>
> - **`make deps`** —— 在 Arch 或 Ubuntu/Debian 上安裝全部宿主機依賴（Go、podman、runc、btrfs-progs、libsystemd 標頭檔案、golangci-lint、bun、qemu、構建工具）。可安全重複執行。
> - **`make help`** —— 列印一份分組的、面向使用者的 make 目標清單。它也是預設目標，因此直接執行 `make` 也可以。

**在開發伺服器執行的同時跑整合測試，可能仍存在一些未解決的問題。此事正在調查中。**

如果你只是想試用，請使用 `stable` 分支（預設分支）。如果你想要最新改動（可能並不穩妥），請使用 `main`。兩個分支都會隨著改動被認定穩定或被整合進倉庫而向前滾動。

執行 `make dev` 會構建測試鏡像、建立開發用 btrfs 卷、在 5309 埠啟動後端容器，並啟動帶熱重載的 Vite 開發伺服器。執行起來之後，通過 `http://<hostname>:5173` 訪問 UI。

宿主機上的 5309（後端 API）與 5173（Vite 開發伺服器）埠必須可訪問。

| 目標               | 說明                                                                                 |
| -------------------- | ------------------------------------------------------------------------------------- |
| `make dev`           | 啟動完整開發環境（後端 + Vite 開發伺服器）。                                 |
| `make dev-stop`      | 停止並移除本工作樹的開發後端容器。                            |
| `make dev-stop-all`  | 停止宿主機上每一個 `town-os-dev-*` 容器，而不僅是本工作樹的。             |
| `make dev-logs`      | 跟蹤正在執行的開發容器內的 journalctl。                                           |
| `make btrfs-dev`     | 為開發環境建立一個全新的 50GB btrfs 環回捲。                          |
| `make dev-btrfs`     | 僅在尚未掛載時建立開發 btrfs 卷（由 `dev` 自動使用）。|
| `make clean-btrfs-dev` | 解除安裝、分離 loop 裝置並移除開發 btrfs 卷。                            |
| `make clean-dev`     | 停止開發容器並拆除開發 btrfs 卷。會移除 `dev-data/`。             |

## Makefile 目標

所有目標都使用一個由工作目錄路徑推匯出的唯一 `INSTANCE_ID`，因此多個檢出可以併發執行而互不衝突。臨時狀態（埠檔案、btrfs 卷、開發資料）位於 `/tmp/town-os-$(INSTANCE_ID)/`。

### 測試

| 目標                          | 說明                                                                                                                                          |
| ------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| `make test`                     | 執行程式碼檢查、Go 單元測試與 JS 單元測試。                                                                                          |
| `make test-race`                | 執行程式碼檢查，並在競態檢測器下跑 Go 單元測試。比 `test` 慢，且僅限 Go（bun 沒有對應物）；它報告的是待分診的發現，而非充當門禁。 |
| `make test-integration`         | 構建測試鏡像，並在帶 systemd 與 btrfs 的特權 podman 容器內執行 Go 整合測試。退出時清理 btrfs 環回。     |
| `make test-integration-build`   | 構建測試鏡像並啟動整合測試容器（已載入全部鏡像），但不執行任何測試。適合為 `test-integration-rerun` 做準備。 |
| `make test-integration-rerun`   | 在已經執行的容器中重跑整合測試（來自先前的 `test-integration-build`）。跳過鏡像構建。                              |
| `make test-ui-unit`             | 只執行 JS（bun）UI 單元測試。                                                                                                 |
| `make test-ui-integration`      | 針對一個後端容器執行 JS（bun）UI 整合測試。退出時清理 btrfs 環回。                                                     |
| `make test-ui-integration-local`| 針對本地執行的後端（無容器）執行 UI 整合測試。                                                                           |
| `make test-full`                | 依次執行 `test`、`test-integration` 與 `test-ui-integration`。使用訊號陷阱保證清理。                                    |
| `make test-full-log`            | 執行 `test-full` 並把全部輸出 tee 到 `/tmp/town-os/log/` 下帶時間戳的日誌檔案。                                                                |
| `make auto-test`                | 監視 `.go`/`.js` 檔案變化並自動重跑 `make test`。需要時會安裝 [reflex](https://github.com/cespare/reflex)。          |
| `make auto-test-full`           | 監視檔案變化並自動重跑 `make test-full`。需要時會安裝 reflex。                                                       |

用 `TEST_RUN=<regex>` 過濾要執行哪些整合測試（例如 `make test-integration TEST_RUN=TestInstall`）。用 `TEST_TIMEOUT=<duration>` 覆蓋預設的 60 分鐘超時。

**一次測試執行絕不會與另一次、或與 `make dev` 衝突。** 測試容器以宿主機網路執行（這是刻意的——橋接網路的 DNS 在強制門戶網路下會失效），因此它啟動的每個系統服務都繫結在宿主機的網路名稱空間中，`make dev` 啟動的一切也是如此。因此測試框架為每次執行分配各自的臨時埠給 rolodex、node-exporter、Prometheus、監控 UI 與 ingress，並分配各自的 WireGuard 鹽值，使介面名、監聽埠與 overlay 子網按檢出與按角色各不相同。`make dev` 刻意不接受這些覆蓋：它意在鏡像一台真實機器，那裡 DNS 在 `:53`、ingress 在 `:443`。

### 程式碼檢查

| 目標      | 說明                       |
| ----------- | --------------------------------- |
| `make lint` | 執行 `go vet` 與 `golangci-lint`。 |

### 本地 Registry

整合測試使用一個本地 `registry:2` 容器，以避免 Docker Hub 的速率限制。當你執行 `make test-integration` 或 `make test-ui-integration` 時，構建會自動：

1. 發現測試包倉庫引用的所有 `docker.io` 鏡像（`discover-images` 工具）
2. 在隨機埠上啟動一個本地 registry
3. 從鏡像快取載入每個鏡像並推送到本地 registry
4. 生成一個 `registries.conf`，把 `docker.io` 的拉取重定向到本地鏡像源
5. 把該配置掛載進測試容器

這一切是透明的——無需修改任何程式碼。所有鏡像都從檢出目錄的鏡像快取（`.cache/images/`）載入；測試流水線期間不會發生任何 docker.io 拉取。

| 目標                   | 說明                                               |
| ------------------------ | --------------------------------------------------------- |
| `make registry`          | 啟動本地 registry 容器。                       |
| `make registry-populate` | 把發現到的 docker.io 鏡像鏡像化到本地 registry。 |
| `make registry-stop`     | 停止並移除本地 registry 容器。             |

每個工作目錄都有自己的 registry 實例（通過 `INSTANCE_ID` 區分），因此併發的測試執行不會衝突。

### 本地 Gitea 伺服器

整合測試還會使用一個本地 Gitea 實例，以避免直接從 GitHub 克隆測試包倉庫。當你執行 `make test-integration` 或 `make test-ui-integration` 時，構建會自動：

1. 在隨機埠上啟動一個本地 Gitea 伺服器
2. 建立一個管理員使用者
3. 把測試包倉庫作為裸克隆快取在 `.cache/git-repos/` 中（後續執行會 fetch 以重新整理）
4. 通過 go-git 把快取的倉庫推送進 Gitea（`populate-repos` 工具）
5. 把 `TOWN_OS_TEST_REPO_CORE_URL` 與 `TOWN_OS_TEST_REPO_EXTRAS_URL` 環境變數傳入測試容器，使所有 git 克隆都命中本地 Gitea 實例

`.cache/git-repos/` 中的裸克隆快取跨 Gitea 重啟留存，因此只有第一次執行會訪問 GitHub。後續執行只 fetch 更新並推送到新的 Gitea 實例。`make clean` 會移除該快取。

這一切是透明的——無需修改任何程式碼。若未設定這些環境變數，測試會回退到 GitHub 的 URL。

| 目標                | 說明                                                |
| --------------------- | ---------------------------------------------------------- |
| `make gitea`          | 啟動本地 Gitea 容器並建立管理員使用者。 |
| `make gitea-populate` | 把測試倉庫從 GitHub 遷移進本地 Gitea。       |
| `make gitea-stop`     | 停止並移除本地 Gitea 容器。             |

每個工作目錄都有自己的 Gitea 實例（通過 `INSTANCE_ID` 區分），因此併發的測試執行不會衝突。

### 構建

| 目標                        | 說明                                                                                                            |
| ----------------------------- | ---------------------------------------------------------------------------------------------------------------------- |
| `make production-image`       | 構建生產基礎鏡像（用於整合測試）。                                                               |
| `make dev-production-image`   | 構建生產基礎鏡像（用於開發）。                                                                             |
| `make test-image`             | 構建測試容器鏡像（包含整合測試二進位制）。                                                     |
| `make dev-image`              | 構建開發容器鏡像。                                                                                         |
| `make ui-integration-image`   | 構建 UI 整合測試容器鏡像。                                                                         |
| `make build-networkcontroller`| 在本地構建網路控制器二進位制（`town-os-networkcontroller`）。                                             |
| `make ui-image`               | 在本地把 UI 鏡像構建為 `localhost/town-os-ui:<INSTANCE_ID>` 供測試使用（從不拉取 quay 上的 UI 鏡像）。          |
| `make nc-image`               | 在本地構建網路控制器鏡像供測試使用；`make nc-image-dev` 針對開發基礎鏡像做同樣的事。          |
| `make ingress-image`          | 在本地構建 ingress 鏡像供測試使用。                                                                             |
| `make gfeh-image`             | 在本地把物件儲存（gfeh）鏡像構建為 `localhost/town-os-gfeh:<INSTANCE_ID>` 供測試使用。會拉取 Rust 工具鏈鏡像；該鏡像約 1.5G，且只有此目標需要它，因此刻意不放進 `BASE_IMAGES`。 |
| `make pull-images`            | 從 Docker Hub 拉取所有容器鏡像並儲存到檢出目錄的鏡像快取。若有快取鏡像缺失會自動執行。 |

開發與整合使用各自獨立的生產基礎鏡像與構建快取，因此併發構建不會互相干擾。

**交叉構建會按目標架構暫存其基礎鏡像。** podman 的儲存中每個 `name:tag` 只有一份鏡像，容不下兩種架構，因此交叉構建會把某個基礎鏡像重新指向目標架構，把宿主機架構的那一份擠掉。鏡像快取的 tar 按架構區分，所以被擠掉的那一份可以透過本地載入回來，而不必走網路拉取。`BASE_IMAGES_RUNTIME`——即可交叉構建的 Containerfile 用裸 `FROM` 命名的那些基礎鏡像，也就是會隨產物一起釋出的階段——按目標架構暫存；其餘都是工具鏈鏡像，每個交叉 Containerfile 都用 `--platform=$BUILDPLATFORM` 把它們釘在宿主機上，因為它們執行在宿主機並執行交叉編譯，所以在任何 `TARGET` 下都保持宿主機架構。每個構建分支自己暫存所需的基礎鏡像，而不依賴全域的統一處理。

### 釋出與推送

所有釋出鏡像都推送到 `quay.io/town/`。所有推送標籤都按架構分割槽：每台主機推送其本機架構，形式為 `rc.<date>-<arch>` / `rc.latest-<arch>`（候選釋出）、`release.<date>-<arch>` / `latest-<arch>`（正式釋出），或 `<tag>-<arch>`（透過 `make push-tag PUSH_TAG=<tag>` 使用自訂標籤），其中 `<arch>` 是 `uname -m` 的原始形式——`x86_64` 或 `aarch64`，而*不是* OCI 平台名 `amd64`/`arm64`。不帶字尾的普通名稱（`rc.latest`、`latest`、日期標籤，以及自訂標籤）僅作為多架構 manifest 列表存在，由 `make manifest-rc` / `make manifest-release` / `make manifest-tag` 在每種架構都推送完成之後組裝；普通名稱絕不能作為單架構標籤推送，因為它在另一種架構上會以 `exec format error` 失敗。在對應的 manifest 目標執行之前，普通名稱並不存在——這才是誠實的狀態，好過讓它解析到最後推送的那種架構。

**交叉釋出在本機執行測試，並跳過它無法交叉構建的東西。** `release-build` 透過 `make release-test` 來執行測試階段，後者只為這一次遞迴清空 `TARGET`：`test-full` 會構建整合測試的框架並在本機*執行*它，因此這些分支會直接拒絕異架的 `TARGET`。測試驗證的是執行它的這台機器上的原始碼；釋出中屬於交叉的部分是隨後構建的產物。Proton 執行器按其構造就是 x86_64 的——GE-Proton 提供的是 x86_64 的 Wine，鏡像還加入 i386 multiarch 以執行 32 位元 Windows 可執行檔，因此根本不存在可供交叉編譯的*目標*。所以非 x86_64 的釋出會把它從聚合目標中剔除，推送分支跳過為它打標籤，其 manifest 列表也僅基於 x86_64 組裝，而不是遍歷 `ARCHES` 的每一項——對一個完全按設計行事的鏡像，應當跳過而不是失敗。

在執行時，system controller 從同一個值推匯出每一個同族鏡像標籤（UI、Rolodex、網路控制器、ingress）：若設定了 `TOWN_OS_TAG` 環境變數則取之，否則取 `rc.latest-<arch>`。不存在編譯期的版本固定——install 構建系統通過在 system controller 的 systemd 單元上設定 `TOWN_OS_TAG` 來固定某個釋出版本，而在沒有覆蓋時，機器始終跟蹤 `rc.latest-<arch>`。

| 目標                      | 說明                                                                                                        |
| --------------------------- | ------------------------------------------------------------------------------------------------------------ |
| `make release-image`        | 構建釋出用的 system controller 鏡像（`quay.io/town/town`）。                                                   |
| `make release-ui-image`     | 構建釋出用的 UI 鏡像（`quay.io/town/ui`）。                                                                    |
| `make release-proton-image` | 構建釋出用的 Proton 執行器鏡像（`quay.io/town/proton`）。**需要 `PROTON_ENABLED=1`**；否則該目標不存在。僅限 x86_64——它會拒絕任何其他 `TARGET`。 |
| `make release-nc-image`     | 構建釋出用的網路控制器鏡像（`quay.io/town/networkcontroller`）。                                     |
| `make release-ingress-image`| 構建釋出用的 ingress 鏡像（`quay.io/town/ingress`）。                                                          |
| `make release-gfeh-image`   | 構建釋出用的物件儲存鏡像（`quay.io/town/gfeh`）。                                                                |
| `make release-test`         | 在本機執行 `test-full`，並為這一次遞迴清空 `TARGET`。`release-build` 依賴它而不是直接依賴 `test-full`，這樣交叉釋出也仍然能夠測試。 |
| `make release-build`        | 拉取鏡像、執行 `release-test`，然後構建釋出鏡像。當 `PROTON_ENABLED=1` *且*釋出目標為 x86_64 時包含 Proton 執行器。    |
| `make push`                 | `push-rc` 的別名。                                                                                               |
| `make push-rc`              | 把所有鏡像（system controller、UI、網路控制器、ingress、物件儲存；`PROTON_ENABLED=1` 時含 Proton）作為按架構的候選釋出推送（`rc.<date>-<arch>` + `rc.latest-<arch>`）。 |
| `make manifest-rc`          | 從按架構的標籤組裝並推送普通的 `rc.<date>` / `rc.latest` 多架構 manifest 列表。在每種架構都推送完成之後執行一次。 |
| `make push-release`         | 執行 `release-build`，然後把所有鏡像作為按架構的正式釋出推送（`release.<date>-<arch>` + `latest-<arch>`）。        |
| `make manifest-release`     | 從按架構的標籤組裝並推送普通的 `release.<date>` / `latest` 多架構 manifest 列表。在每種架構都推送完成之後執行一次。 |
| `make push-ui-rc`           | 只把 UI 鏡像作為按架構的候選釋出推送（`rc.<date>-<arch>` + `rc.latest-<arch>`）。                  |
| `make push-ui-release`      | 只把 UI 鏡像作為按架構的正式釋出推送（`release.<date>-<arch>` + `latest-<arch>`）。                          |
| `make push-proton-rc`       | 只把 Proton 執行器鏡像作為候選釋出推送。**需要 `PROTON_ENABLED=1`**。                          |
| `make push-proton-release`  | 只把 Proton 執行器鏡像作為正式釋出推送。**需要 `PROTON_ENABLED=1`**。                                    |
| `make push-nc-rc`           | 只把網路控制器鏡像作為按架構的候選釋出推送（`rc.<date>-<arch>` + `rc.latest-<arch>`）。  |
| `make push-nc-release`      | 只把網路控制器鏡像作為按架構的正式釋出推送（`release.<date>-<arch>` + `latest-<arch>`）。          |
| `make push-ingress-rc`      | 只把 ingress 鏡像作為按架構的候選釋出推送（`rc.<date>-<arch>` + `rc.latest-<arch>`）。             |
| `make push-ingress-release` | 只把 ingress 鏡像作為按架構的正式釋出推送（`release.<date>-<arch>` + `latest-<arch>`）。                     |
| `make push-gfeh-rc`         | 只把物件儲存鏡像作為按架構的候選釋出推送（`rc.<date>-<arch>` + `rc.latest-<arch>`）。                          |
| `make push-gfeh-release`    | 只把物件儲存鏡像作為按架構的正式釋出推送（`release.<date>-<arch>` + `latest-<arch>`）。                        |
| `make push-tag PUSH_TAG=x`  | 用自訂標籤構建並推送所有鏡像，按架構推送（`x-<arch>`），與 `push-rc` 的做法完全一致。當 `PROTON_ENABLED=1` 時包含 Proton 執行器。 |
| `make manifest-tag PUSH_TAG=x` | 從按架構的標籤組裝並推送普通名稱 `x` 的多架構 manifest 列表。在每種架構都推送完成之後執行一次。          |

### Registry 認證

| 目標             | 說明                                                                                       |
| ------------------ | ------------------------------------------------------------------------------------------------- |
| `make docker-login`| 使用 `.env` 中的 `DOCKER_USERNAME` / `DOCKER_PASSWORD` 登入 Docker Hub。未設定則跳過。 |
| `make quay-login`  | 使用 `.env` 中的 `QUAY_USERNAME` / `QUAY_PASSWORD` 登入 Quay.io。未設定則跳過。        |

### Btrfs 管理

| 目標                 | 說明                                                                 |
| ---------------------- | --------------------------------------------------------------------------- |
| `make btrfs`           | 為整合測試建立一個 50GB btrfs 環回捲。                  |
| `make clean-btrfs`     | 解除安裝、分離 loop 裝置並移除整合測試的 btrfs 卷。 |
| `make btrfs-dev`       | 為開發環境建立一個 50GB btrfs 環回捲。                |
| `make clean-btrfs-dev` | 解除安裝、分離 loop 裝置並移除開發 btrfs 卷。              |
| `make dev-btrfs`       | 僅在尚未掛載時建立開發 btrfs 卷。              |

開發環境與整合測試環境使用各自獨立的 btrfs 卷、容器鏡像與構建快取，因此它們可以併發執行而互不衝突。

### 清理

`make test-full` 在測試完成後會自動清理所有整合容器、registry、Gitea 與 btrfs 環回捲。一個 shell EXIT 陷阱確保即使被訊號中斷也會執行清理。每個整合測試目標（`test-integration`、`test-ui-integration`）也各自使用 EXIT 陷阱，以保證無論配方如何終止（成功、失敗或被訊號中斷）btrfs 環回都會被清理。`clean-btrfs` 目標包含一道安全網，會掃描當前目錄下由 btrfs 鏡像支撐的孤立 loop 裝置，以應對跟蹤檔案缺失的情況。

| 目標                   | 說明                                                                             |
| ------------------------ | --------------------------------------------------------------------------------------- |
| `make clean`             | 移除 `.cache/` 構建快取目錄。                                             |
| `make clean-dev`         | 停止所有開發容器，拆除開發 btrfs，移除 dev-data/dev-repos。                |
| `make clean-cache`       | 從臨時狀態目錄中移除開發資料、開發倉庫與開發 Rolodex 資料。    |
| `make clean-integration` | 移除整合測試容器（test、UI 後端、UI 執行器）並清理 btrfs。       |
| `make clean-btrfs`       | 解除安裝並移除整合測試的 btrfs 卷與孤立的 loop 裝置。         |
| `make clean-image-cache` | 刪除本檢出目錄的鏡像快取（`.cache/images/`）。                           |
| `make clean-containers`  | 移除任意工作目錄/實例下的所有 town-os 與 preflight 容器。      |
| `make clean-all`         | 清理一切：所有容器、構建快取、開發環境、整合測試與 btrfs。             |

### 預檢檢查

| 目標            | 說明                                                                                                    |
| ----------------- | -------------------------------------------------------------------------------------------------------------- |
| `make preflight-dev` | 校驗開發環境：檢查 podman、btrfs-progs、倉庫憑據與橋接網路。 |

### SSH

| 目標     | 說明                                                                                   |
| ---------- | --------------------------------------------------------------------------------------------- |
| `make ssh` | SSH 登入到 `town-os.local` 上正在執行的 Town OS 裝置（自動清除陳舊的主機金鑰）。 |

### 依賴檢查

這些通常不會被直接呼叫；它們作為其他目標的前置條件執行。

| 目標                     | 說明                                  |
| -------------------------- | -------------------------------------------- |
| `make check-go`            | 驗證 `go` 可用。                    |
| `make check-bun`           | 驗證 `bun` 可用。                   |
| `make check-podman`        | 驗證 `podman` 可用。                |
| `make check-runc`          | 驗證 `runc` 可用。                  |
| `make check-btrfs`         | 驗證 `mkfs.btrfs` 可用。            |
| `make check-golangci-lint` | 驗證 `golangci-lint` 可用。         |
| `make check-python3`       | 驗證 `python3` 可用。               |
| `make check-libsystemd`    | 驗證 libsystemd 開發標頭檔案存在。 |

## 許可證

GNU Affero GPL 3.0
