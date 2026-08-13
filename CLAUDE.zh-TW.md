CLAUDE，未經我明確許可，不得編輯此檔案。

> **本文件是 [CLAUDE.md](CLAUDE.md) 的繁體中文譯本。英文原件為準。**
> 簡體中文譯本見 [CLAUDE.zh-CN.md](CLAUDE.zh-CN.md)；西班牙語譯本見
> [CLAUDE.es-ES.md](CLAUDE.es-ES.md)（西班牙）與 [CLAUDE.es-MX.md](CLAUDE.es-MX.md)（墨西哥）；
> 日語譯本見 [CLAUDE.ja-JP.md](CLAUDE.ja-JP.md)。
> 兩者出現分歧時，以英文原件為準，並應修正譯文。程式碼識別字、檔案路徑、
> 命令、環境變數、API 路徑與 YAML 鍵名一律保留原文，不作翻譯。

**本檔案只包含構建說明與程式碼風格。** 系統實際如何運作——架構、各子系統行為、
API 介面、啟動順序、設定項，以及維繫這些內容的不變數——都在
[DESIGN.md](DESIGN.md)（中文譯本見 [DESIGN.zh-TW.md](DESIGN.zh-TW.md)）中。
需要了解 Town OS **做什麼**時讀 DESIGN.md；需要了解**如何構建、如何測試、
如何在其中寫程式碼**時讀本檔案。凡是改變行為的改動，DESIGN.md 都需要隨之更新。

- **最重要**：
    - **使用 `make`，而非底層的編譯/測試工具。** 絕不直接執行 `go build`、`go test`、`go vet`、`golangci-lint`、`bun test`、`vitest` 或任何等價命令。一律通過 make 目標執行，這樣倉庫的封裝（清理陷阱、btrfs 生命週期、每次執行的例項 ID）才會生效。
    - **可隨時執行的 make 目標**（快速、冪等、無遠端副作用）：
      `make help`、`make lint`、`make check-*`（bun / go / podman / runc / btrfs / libsystemd / golangci-lint）。可自由使用它們來驗證改動——無需事先詢問。
    - **若某個 make 目標不在上述任一列表中，先詢問。**
    - 任何情況下都絕不強制推送（force push）。
    - 需要推送時，只推送到 "origin"。
    - 推送前務必先執行 `git pull --rebase`，並解決所有合併衝突。
    - 絕不以任何方式碰 GPG。正常執行 `git commit` 即可。若簽名失敗，停下來詢問使用者。絕不殺掉 gpg-agent，絕不使用 `--no-gpg-sign`，絕不自行嘗試修復 GPG。
    - 提交必須簽名。
    - 絕不因任何理由擺弄 GPG agent。

- 當提供了引數時，確保呼叫函式中真正使用了這些引數

- **併發安全** —— `make test-full` 必須始終能夠在同一倉庫中同時執行而互不衝突。沒有任何事情比這更重要。

- Go 程式中不應使用 context.TODO 與 context.Background。凡有可能，請使用帶超時和取消的 context，確保不會有任何東西永久等待某個 context。

- 你所做的每件事都要加測試。**每一處行為改動都必須同時具備單元測試和整合測試。** 單元測試隔離驗證邏輯；整合測試在測試容器內、使用真實的 systemd、btrfs 與 podman 端到端驗證功能。若確實無法編寫整合測試（例如純 UI 改動），在提交資訊中說明原因。

- 使用型別斷言的結果前，一律檢查斷言是否成功

- **容器映象中使用 CMD 而非 ENTRYPOINT** —— 所有 Containerfile 及內聯的 Containerfile 字串都必須使用 `CMD` 而非 `ENTRYPOINT`。這樣 `podman run <image> <command>` 無需 `--entrypoint` 即可覆蓋預設命令。適用於 systemcontroller 映象、NC 映象，以及任何動態生成的 Containerfile。

- **每個執行時容器映象都必須自帶系統 CA 證書包** —— 任何 Containerfile（或內聯的 Containerfile 字串），只要其最終階段執行的 Town OS 程式碼會發起對外 HTTPS 呼叫，就必須安裝 `ca-certificates`（debian/ubuntu：`apt-get install ca-certificates`；alpine：`apk add ca-certificates`），除非基礎映象已經提供（例如 `caddy`、`oven/bun`）。缺少 CA 證書包時，Go 的 TLS 棧會讓每一次 HTTPS 呼叫都失敗並報 `x509: certificate signed by unknown authority`，而後臺輪詢器中的失敗在預設日誌級別下是不可見的（參見 `fetchExternalIP` 靜默丟棄 `ipinfo.io` 響應的情況）。新增任何 Containerfile 時，先確認最終映象中存在 `/etc/ssl/certs/ca-certificates.crt`，再認為該映象可以釋出。

- **所有 `podman run --name` 都必須帶 `--replace`** —— 全倉庫無一例外。

- **make 流水線中所有 podman 都通過 `${SUDO}` 以 root 身份執行** —— `make/lib.sh` 中定義 `SUDO="sudo HOME=$HOME"`，並且 make 指令碼（`build.sh`、`images.sh`、`test.sh`、`dev.sh`、`registry.sh`、`gitea.sh`、`lib.sh`）中的**每一處** `podman` 呼叫都必須寫成 `${SUDO} podman`。root 與 rootless 的 podman 擁有**各自獨立的映象儲存**：基礎映象被拉取/載入進 root 儲存（`/var/lib/containers`），而 rootless 使用者儲存是空的。因此裸的（不帶 `${SUDO}` 的）`podman` 呼叫會命中空的 rootless 儲存，並在 `--pull=never` 下以 `image not known` 失敗——即便 `${SUDO} podman image exists` 報告映象存在（那是另一個儲存）。向 make 指令碼中新增任何 podman 命令時，務必加上 `${SUDO}` 字首；絕不要在 rootless podman 下執行會構建/載入映象的 make 目標，也不要為宿主機側構建設定指向 rootless socket 的 `CONTAINER_HOST`（那會把 `${SUDO} podman` 路由到錯誤的儲存）。唯一的例外是可用性探測（`check.sh`/`preflight.sh` 中的 `command -v podman`）以及 `deps.sh` 安裝列表中作為軟體包名出現的字面量 `podman`。

- **構建中不得硬編碼公共 DNS；podman 構建使用 `--network=host`** —— make 流水線中的每一次 `podman build` 都以 `--network=host` 執行，使名稱解析走宿主機的解析器（systemd-resolved）。容器網路下的構建會把宿主機的環回 stub 替換成公共解析器，而強制門戶網路（咖啡館、酒店）會阻斷對 1.1.1.1/8.8.8.8 的直接查詢——從而讓 `bun install`、`apt-get`、`apk add` 無限期卡住。出於同樣的原因，測試與開發所用的 NC 映象是**在宿主機上**構建的（`nc-image` / `nc-image-dev` 目標 → `localhost/town-os-networkcontroller:<INSTANCE_ID>`，二進位制從生產/開發基礎映象中提取，因此始終與 systemcontroller 匹配），再經由映象快取載入進容器——絕不在容器內用 `--dns` 構建。

- **所有測試套件的 `podman run` 容器都使用 `--net host`** —— 測試容器、UI 後端、UI 測試執行器、開發容器、registry 與 gitea 容器全部使用宿主機網路。registry 與 gitea 通過 `REGISTRY_HTTP_ADDR` / `GITEA__server__HTTP_PORT` 直接繫結各自例項的隨機埠，而不是使用 `-p` 對映；gitea 的 SSH 被停用（`DISABLE_SSH=true`），因此不會有任何東西嘗試繫結宿主機的 22 埠。理由：橋接網路容器在強制門戶網路下 DNS 會失效，而 registry（Docker Hub 回源拉取）與 gitea（倉庫遷移）都會自行發起對外呼叫。唯一刻意保留的例外是 `preflight-dev` 的 nginx 容器，它的 `-p` 對映正是為了驗證橋接網路可用。

- **映象標籤按架構分割槽** —— 每個推送的標籤都帶有 `uname -m` 原始形式的架構字尾（`<arch>` 為 `x86_64` 或 `aarch64`）。該標籤字尾刻意區別於 OCI 平臺名 `amd64`/`arm64`：Go 通過 `archTag()` 把 `runtime.GOARCH` 對映到該字尾，make 使用 `HOST_ARCH`（規範化為 `x86_64`/`aarch64`），shell 使用 `make/lib.sh` 中的 `host_arch_tag`。而普通的 `host_arch` / `runtime.GOARCH` 值仍保持 `amd64`/`arm64`，因為 podman 在 `podman pull --platform linux/<arch>` 和 `.Architecture` 比較時需要它們——絕不要把 `x86_64`/`aarch64` 餵給 `--platform`。`push-rc` 推送 `rc.<date>-<arch>` / `rc.latest-<arch>`；`push-release` 推送 `release.<date>-<arch>` / `latest-<arch>`——始終是執行推送的宿主機的本機架構。不帶字尾的普通名稱（`rc.latest`、`latest` 以及日期標籤）**僅**作為多架構 manifest 列表存在，由 `manifest-rc` / `manifest-release` 在 `ARCHES`（`x86_64 aarch64`）中每個架構都推送完成之後組裝；絕不要把普通名稱作為單架構標籤推送。當沒有烘焙進標籤時，執行時的回退值是 `main.go` 中的 `defaultVersionTag()`（`rc.latest-<arch>`，架構來自 `archTag()` 對映後的 GOARCH）。理由：從一臺主機推送的單架構普通標籤，在另一種架構上會以 `exec format error` 失敗（或者更糟：在 `Restart=always` 下不斷崩潰重啟的同時，卻讓狀態輪詢測試虛假通過）。

- **普通便捷標籤絕不可用於測試** —— 任何測試、測試框架、開發容器或夾具都不得引用*普通的*（無架構字尾的）`quay.io/town/*:rc.latest` 或 `:latest` 映象（它們可能不存在，或是過期的多架構 manifest）。帶架構字尾的形式**是**允許的，並且是預設選擇。測試使用：宿主機對應架構的 rolodex rc 標籤（`rc.latest-<arch>`，即 `rc.latest-x86_64` / `rc.latest-aarch64`）、本地構建的 UI 映象（`make ui-image` → `localhost/town-os-ui:<INSTANCE_ID>`）、本地構建的 NC 映象（`make nc-image`），以及在映象從不會被拉取或執行的 mock 單元測試中使用的中性假標籤（例如 `:testtag`）。

- **測試與開發構建 `localhost/` 映象；推送目標始終構建全新的發布映象** —— `make/build.sh` 中的 `*-local` 分支為測試與開發框架生成 `localhost/town-os-*:$(INSTANCE_ID)`；`release-*` 分支生成 `quay.io/town/*`。**任何推送目標都不得構建、從中打標籤、或依賴 `localhost/*` 映象**，且每個推送目標都必須構建*新的*發布映象，而不是給本地儲存中恰好存在的映象重新打標籤。此規則適用於每一個映象，無一例外。理由：給本地測試映象重新打標籤，等於把為測試框架構建的產物——按例項區分的標籤、`--pull=never` 的基礎映象、僅限宿主機架構、從不交叉構建——以發布名稱發布出去。在全新檢出的倉庫中這會失敗；而在開發者的機器上它會成功，並發布錯誤的產物，那更糟。

- **內容來自倉庫之外的本地映象需要顯式的快取失效機制** —— 大多數 `*-local` 映象都從倉庫原始碼構建，因此原始碼變更會使其層快取失效，它們不可能與對應的發布映象發生漂移。而內容在構建時抓取的映象（`Containerfile.gfeh` 執行不帶版本的 `cargo install gfehd`）位於一行逐位元組相同的 `RUN` 之後，因此其層是永久的快取命中，會凍結在該機器上首次構建時的版本。發布構建傳入 `--no-cache`；本地夾具傳入按天粒度的構建引數（`GFEH_CACHE_DATE`），從而每天重新整理一次，而不必在每次執行時重新編譯。若沒有它，整合測試套件就會在悄無聲息地測試一個 Town OS 已經無法執行的守護程序。

- **快速失敗** —— 若任何 make 子任務，或由 make 子任務啟動的指令碼失敗，立即停止。不要繼續進入下一階段。

- **絕不吞掉退出碼** —— 執行 make/測試命令的指令碼絕不能吞掉退出碼。不要寫 `|| rc=$?`，不要在測試呼叫上寫 `|| true`。讓 `set -e` 發揮作用。清理命令（podman rm、rm -f）不在此限。

- **測試中不得硬編碼共享資源** —— 所有測試臨時檔案、socket、目錄與埠都必須使用每次執行唯一的路徑（`t.TempDir()`、`filepath.Join`、`findFreePort` 等）。絕不使用 `/tmp/foo.sock` 這樣的固定路徑。

- **執行上述允許的 make 目標無需事先詢問；"需要許可"列表中的其他任何操作都需要明確同意。** 絕不直接呼叫 `go`、`go test`、`go vet`、`golangci-lint`、`bun test`、`vitest` 等——一律通過 make。

- **測試或構建程式碼中的任何東西都不得使用 tmpfs** —— 任何 make 目標、make 指令碼或測試框架寫出的檔案，都不得位於 tmpfs（記憶體支撐的）檔案系統上。這一條不可協商且絕對：它適用於 btrfs 環回後備映象、容器/卷資料、歸檔、下載、埠檔案、跟蹤檔案，以及每次執行產生的其他一切產物。原因是致命的，而非表面的：測試用 btrfs 檔案系統是一個 50G 的環迴文件，而由 tmpfs 支撐的 loop 裝置在記憶體壓力下會**使宿主機核心死鎖**——tmpfs 頁只能回收到 swap，但 loop 回寫路徑本身又需要分配記憶體才能把它們排空，於是一旦 tmpfs 佔滿記憶體，機器就會硬鎖死，並由韌體/看門狗重啟（已在 Manjaro 上觀察到：systemd 把 `/tmp` 掛載為大小為記憶體 50% 的 tmpfs，而 swap 幾乎為零）。在常見開發發行版（Arch/Manjaro/Fedora）上 `/tmp` 就是 tmpfs，所以**不要假設 `/tmp` 由磁碟支撐**。任何會建立後備檔案、loop 裝置或較大寫入目標的測試/構建程式碼，都必須先把其目錄解析到真正由磁碟支撐的檔案系統上（例如檢查 `findmnt -no FSTYPE <dir>` 不是 `tmpfs`/`ramfs`，或放在 `/var/tmp` 這類已知位於磁碟的路徑下），若無法做到則大聲失敗。向 make 指令碼中新增任何新路徑時，寫入前先確認它不在 tmpfs 上。

- **臨時狀態位置** —— 每次執行的簿記資料（埠檔案，`.disk`/`.loop`/`.mount` 跟蹤檔案，開發後設資料）按例項限定在 `/tmp/town-os-$(INSTANCE_ID)/` 之下；但任何*承載資料*的產物——首先是 btrfs 環回後備映象——都必須放在由磁碟支撐的路徑上，絕不能放在 tmpfs 上（見上面的 no-tmpfs 規則）。在未先確認 `/tmp` 不是 tmpfs 之前，絕不要把環回/磁碟映象、容器卷資料或大型下載放到 `/tmp`。

- **只在被告知時提交或推送** —— 除非使用者明確要求，否則絕不執行 `git commit` 或 `git push`。絕不強制推送（`--force` 或 `--force-with-lease`）。

- systemcontroller 絕不應呼叫 os.Exit，除非該服務確實正在被終止——嚴重錯誤應以 fatal 級日誌處理

- 請檢查所有錯誤。任何程式碼的任何部分，都不得以任何理由用下劃線忽略或跳過錯誤檢查

- **務必檢查 comma-ok 表示式的 `ok`。** 任何返回 `value, ok` 對的表示式——型別斷言（`v, ok := x.(T)`）、map 索引（`v, ok := m[k]`）、channel 接收（`v, ok := <-ch`）——都必須先檢查 `ok` 再使用 `value`；絕不用 `_` 丟棄它，也絕不假定斷言/查詢一定成功。優先使用 comma-ok 形式，而非單值型別斷言 `v := x.(T)`（後者在型別不匹配時會 panic）：使用 `v, ok := x.(T)` 並顯式處理 `!ok`。此規則同樣適用於測試程式碼。（型別明確的 switch 分支——`switch v := x.(type)`——以及刻意的 `_ = m[k]` 成員寫入，是僅有的例外。）

- 儘可能在 if 語句中使用內聯錯誤語法（例如 `if err := foo(); err != nil {`）

- **測試服務使用隨機高位埠** —— 啟動網路服務（DNS、HTTP、gRPC 等）的整合測試必須通過 `findFreePort` 繫結到隨機高位埠，絕不使用 53 或 80 這類知名埠。這可防止多個測試同時執行時發生衝突。

- **測試中的 DNS 絕不允許觸碰宿主機。** 任何測試、測試框架，或由 make 測試目標啟動的任何東西，都不得改動宿主機的名稱解析，也不得佔用宿主機的 DNS 埠。具體而言，一次測試執行絕不能：
    - 重寫 `/etc/resolv.conf`（那是 `make/dev.sh` 中的 `redirect_host_dns`，只屬於 `make dev`），
    - 寫入 `/etc/systemd/resolved.conf.d/town-os.conf`，或以其他方式呼叫 `rolodex.ConfigureResolvedRouting`，
    - 向 `systemd-resolved` 發訊號或重啟它（`pkill -HUP systemd-resolved`），
    - 在宿主機網路名稱空間中繫結 **`127.0.0.2:53`**，或任何 `:53`。

  測試容器刻意以 `--net host` 執行（橋接網路的 DNS 在強制門戶網路下會失效），因此係統服務繫結的每一個埠都落在**宿主機**名稱空間中。這正是為什麼 `TOWN_OS_DNS_PORT` 會按次執行分配到 `$(STATE_DIR)/.dns-port` 並由 `system_port_env`（`make/lib.sh`）傳入，也是為什麼只要 `dnsPortIsDefault()` 為假，`main.go` 就跳過 resolved 路由配置——因為 resolved 的按域名伺服器地址不攜帶埠，把 resolved 指向一個已被遷移埠的 rolodex 的 `DNSLoopback`，會讓該 TLD 下的每一次查詢都被黑洞吞掉。

  如果一次測試執行結束後 `127.0.0.2:53` 仍被佔用，或宿主機上出現了 `town-os.conf` 配置片段，應將其視為**測試框架的缺陷，而非偶發的測試不穩定**：這意味著埠覆蓋沒有傳到容器裡，rolodex 回退到了預設值。用 `ss -lnup | grep 127.0.0.2` 與 `ls /etc/systemd/resolved.conf.d/` 驗證——宿主機 `:53` 上唯一的監聽者應當是機器自己的解析器，絕不是我們的。`make dev` 是唯一的例外，且由操作者主動選擇，因為它本就意在映象一臺真實的機器。

- **絕不編寫會向遠端 Gitea 或 GitHub 推送的測試。**

- **當我讓你做某件事時，不要爭辯。**

- **在無關緊要時，測試的 git 操作應優先使用本地倉庫而非遠端倉庫** —— 例如 populate-repos 應在存在本地同級目錄時從該目錄克隆，而不是從 GitHub 拉取。

- 請隨時修復測試中所有可修復的警告

- 包變數應始終作為編譯步驟的一部分被翻譯。固定的包變數應始終有測試覆蓋。

- 確保所有檔案按 API 組織。它們應按子模組名稱分層限定作用域。行數的參考指標大約為 500 行。


## 效能約定

- **使用 `strings.Builder` 構造字串** —— 絕不用 `string(append([]byte(s), c))` 逐字元構造字串。使用 `strings.Builder` 配合 `WriteByte`/`WriteString`，把 O(n²) 的分配降為 O(n)。參見 `src/packages/packages_compile.go`（`applyTemplate`、`applyTemplates`）。

- **已知大小時預分配切片** —— 當結果大小或其上界已知時（例如分頁中的 `limit`），使用 `make([]T, 0, capacity)`。避免在熱路徑中先 `var items []T` 再無界 `append`。

- **用 `COUNT(*) OVER()` 實現單查詢分頁** —— 分頁列表端點必須在 SELECT 列中使用 SQLite 視窗函式 `COUNT(*) OVER()`，而不是另跑一次 `COUNT(*)` 查詢。在掃描每一行的同時讀出總數。

- **為 WHERE 子句中使用的列建索引** —— 每一個用於 `WHERE` 過濾的 SQLite 列（尤其是 `created_at`、`success`、`account`）都必須有合適的索引。複合索引應與常見的過濾組合相匹配（例如為 `CountRecentErrors` 建立 `(success, created_at)`）。

- **快取昂貴的重複查詢** —— `RepositoryRoot.LoadPackages()` 的結果按倉庫名快取在一個 `sync.Map` 中，並在 `ForceRefresh()` 時失效。呼叫方必須使用 `cachedLoadPackages()`，而不是直接呼叫 `LoadPackages()`。同理，`GetInternalIP()` 把結果快取在 `atomic.Value` 中，而不是每個請求都呼叫 `net.InterfaceAddrs()`。

- **直接查詢優於全量掃描** —— 檢查單個包時使用 `GetInstalledVersion(repo, name)`（直接讀取 `installed/<repo>/<name>/`），而不是 `ListInstalled()` 加線性搜尋。

- **相互獨立的操作並行 I/O** —— `refreshSystemServices` 中的容器映象拉取使用 goroutine 加訊號量（最多 3 個併發），而不是順序迴圈。使用 `sync.WaitGroup` + channel 訊號量；不要引入 `errgroup` 依賴。

- **後臺 goroutine 使用伺服器作用域的 context** —— 後臺 goroutine（pages 的 git clone、映象提取）必須使用伺服器作用域的 context（`s.ctx`），而不是 `context.Background()`，以便它們響應優雅關閉。它們**不得**使用 HTTP 請求的 context（該操作的生命週期必須長於請求）。

- **reconcile 中批次載入依賴** —— 所有包的依賴記錄在進入 reconcile 迴圈之前一次性預載入到一個 map 中，而不是在迴圈內逐包載入。


## 開發前置條件

從原始碼構建 Town OS 需要：

- **Go 1.25+** —— 為 system controller 啟用 CGO（連結 libsystemd）。
- **libsystemd-dev** —— systemd journal 與 dbus 繫結所需的 C 開發標頭檔案，`go-systemd/v22` 依賴它。
- **Bun** —— 用於 UI 構建與測試的 JavaScript 執行時。
- **Podman** —— 以 root 執行（`sudo`），用於容器操作。
- **btrfs-progs** —— 提供 `mkfs.btrfs`，用於建立測試與開發用的 btrfs 卷。
- **golangci-lint** —— 用於 Go 程式碼檢查。
- **QEMU** —— `qemu-system-x86_64` 用於執行 VM 包；`qemu-img` 用於把 VM 磁碟映象轉換為 raw 格式。

### 引導安裝

`make deps` 會在一臺全新的 Arch 或 Ubuntu/Debian 機器上安裝全部宿主機依賴
（Go、podman、runc、btrfs-progs、libsystemd 標頭檔案、golangci-lint、bun、qemu、
構建工具）。它由 `make/deps.sh` 實現，從 `/etc/os-release` 檢測發行版，可安全重複執行。

`make help`（預設目標）會列印一份分組的、面向使用者的 make 目標清單。
由 `make/help.sh` 實現。在 `make/include.mk` 中新增或重新命名目標時，
請保持這兩個指令碼同步。

### 預檢檢查

Makefile 提供了 `preflight-dev` 目標，用於在執行測試或啟動開發伺服器之前驗證開發環境。它檢查：

- **podman** —— 驗證 `podman` 命令在 PATH 中可用。
- **btrfs-progs** —— 驗證 `mkfs.btrfs` 命令在 PATH 中可用。
- **倉庫憑據** —— 驗證已設定 `TOWN_OS_REPO_USERNAME` 與 `TOWN_OS_REPO_PASSWORD` 環境變數。
- **橋接網路** —— 啟動一個帶埠繫結的測試 nginx 容器，驗證 podman 的 `-p` 標誌工作正常。

每項檢查在失敗時列印描述性錯誤資訊並以非零狀態退出。所有檢查通過後才會顯示 "All preflight checks passed."。

### Ubuntu / Debian 安裝

在 Ubuntu 或 Debian 系統上，使用以下命令安裝系統依賴：

```
sudo apt-get install -y libsystemd-dev btrfs-progs podman runc qemu-system-x86 qemu-utils
```

Go、Bun 與 golangci-lint 需分別安裝（參見各自上游文件）。

## 程式碼質量

### 錯誤處理

所有 Go 的錯誤返回值都必須被顯式檢查。`errcheck` linter 在全專案啟用，並且不得使用空白識別符號（`_ =`）丟棄錯誤。

在生產程式碼中，defer 函數里的清理錯誤通過命名返回值用 `errors.Join()` 與主錯誤合併（例如 `defer func() { err = errors.Join(err, f.Close()) }()`）。非關鍵的盡力而為操作應記錄錯誤，而不是丟棄。

在測試程式碼中，清理錯誤依嚴重程度通過 `t.Errorf` 或 `t.Logf` 報告，或以 `//nolint:errcheck` 註解加理由註釋顯式抑制。

所有 `//nolint` 指令都必須帶理由註釋（由 `nolintlint` 強制）。

## 整合測試

### 本地 Docker Registry

整合測試針對一個本地 `registry:2` 容器執行，以避免 Docker Hub 的速率限制並確保可復現性。流程如下：

1. **映象發現** —— `discover-images` 工具掃描所有測試包倉庫中的 `docker.io` 映象引用，包括主映象與歸檔映象。結果去重後寫入 `.cache/.registry-images`。
2. **啟動 registry** —— 在一個隨機埠上啟動 `registry:2` 容器。
3. **映象映象化** —— 每個發現的映象從 Docker Hub 拉取，重新打上本地 registry 地址的標籤，並推送到本地 registry（對 localhost 停用 TLS 校驗）。
4. **registry 配置** —— 生成一個 `registries.conf` 檔案，把 `docker.io` 的拉取重定向到本地映象源。該檔案被掛載進測試容器的 `/etc/containers/registries.conf.d/`。
5. **透明運作** —— 無需修改任何程式碼；podman 會自動使用本地映象源。對於未快取的映象，映象源會回退到 Docker Hub。

每個工作目錄都有自己的 registry 例項（通過 `INSTANCE_ID` 區分），因此併發的測試執行不會衝突。

### 本地 Gitea 伺服器

整合測試使用本地 Gitea 例項，以避免 git 操作觸及 GitHub 的速率限制。流程與本地 Docker registry 模式一致：

1. **啟動伺服器** —— 在隨機埠上啟動 `gitea/gitea:latest` 容器，安裝嚮導預先鎖定。自動建立一個管理員使用者（`town-os`）。
2. **倉庫遷移** —— `populate-repos` 工具使用 Gitea 遷移 API，把測試包倉庫（`test-packages-core`、`test-packages-extras`）從 GitHub 遷移到本地 Gitea 例項。遷移是冪等的：已存在且非空的倉庫會被跳過；因遷移失敗而殘留的空倉庫會被刪除並重試。
3. **透明運作** —— 測試通過環境變數（`TOWN_OS_TEST_REPO_CORE_URL`、`TOWN_OS_TEST_REPO_EXTRAS_URL`）獲得本地 Gitea 的 URL。若未設定這些變數，測試會回退到預設的 GitHub URL。

每個工作目錄都有自己的 Gitea 例項（通過 `INSTANCE_ID` 區分），因此併發的測試執行不會衝突。映象發現在本地 Gitea 倉庫可用時會從中讀取。

### 容器清理

`test-full` 目標在整合測試完成後執行 `clean-integration` 與 `clean-btrfs`，確保即使測試失敗，所有測試容器（test、registry、gitea、ui-backend、ui-integration）與 btrfs 環回掛載也會被拆除。`clean-dev` 目標在清理快取前先刪除所有 `town-os-dev` 容器。`clean-containers` 目標刪除任意例項或工作目錄下的所有 Town OS 容器（匹配 `town-os-*` 與 `preflight-test-*` 模式）。`clean-integration` 目標使用容錯的容器刪除方式以實現冪等清理。`clean-all` 目標使用 `clean-containers` 進行跨例項的全面清理。監控映象會從映象快取預載入進整合測試容器。

### Btrfs 環回清理

測試目標（`test-integration`、`test-ui-integration`、`test-full`）使用 shell 的 EXIT 陷阱，保證無論測試成功、失敗還是被訊號中斷，btrfs 清理都會執行。相關配方組織在 `make/` 下的 shell 指令碼中。btrfs 卷的建立在 EXIT 陷阱註冊之後、於測試指令碼內部進行，確保即使建立或後續步驟失敗，loop 裝置也不會洩漏。

`clean-btrfs` 目標執行盡力而為的清理（不使用 `set -e`）：解除安裝 btrfs 檔案系統，通過 `losetup -j` 查詢該磁碟映象檔案對應的 loop 裝置並分離，並刪除狀態跟蹤檔案（`town-os.disk`、`town-os.loop`、`town-os.mount`）。一道安全網會掃描所有活躍的 loop 裝置（`losetup -a`），查詢任何由當前目錄下 btrfs 映象檔案支撐的裝置，即使跟蹤檔案缺失也會分離這些孤立裝置。

### 測試檔案組織

整合測試檔案按元件與子功能組織。每個檔案聚焦一個特定領域：btrfs 操作、git 操作、倉庫管理，以及 system controller 的各子系統。system controller 的測試進一步拆分為獨立檔案：歸檔、引導、檔案系統、安裝（mock 與真實 systemd）、多倉庫場景、網路、包、pages、reconcile、倉庫、設定、systemd 單元與卷。通用的測試初始化與輔助函式集中在一個專門的 helpers 檔案中。

### 測試環境

整合測試在特權 podman 容器內執行，容器中帶有 systemd、btrfs 與完整的測試二進位制。該容器包含 podman 與 runc，用於執行包容器。測試會實際演練真實的 systemd 單元生命週期、btrfs 卷管理與容器操作。
