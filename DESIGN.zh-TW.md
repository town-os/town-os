# Town OS 設計

Town OS 如何運作：架構、各子系統的行為、API 介面，以及維繫它們的不變量。
構建說明、測試規則與程式碼風格在 [CLAUDE.md](CLAUDE.md)（繁體中文譯本見
[CLAUDE.zh-TW.md](CLAUDE.zh-TW.md)）中。

> **本文件是 [DESIGN.md](DESIGN.md) 的繁體中文譯本。英文原件為準。**
> 簡體中文譯本見 [DESIGN.zh-CN.md](DESIGN.zh-CN.md)；西班牙語譯本見
> [DESIGN.es-ES.md](DESIGN.es-ES.md)（西班牙）與 [DESIGN.es-MX.md](DESIGN.es-MX.md)（墨西哥）；
> 日語譯本見 [DESIGN.ja-JP.md](DESIGN.ja-JP.md)。
> 兩者出現分歧時，以英文原件為準，並應修正譯文。程式碼識別符號、檔案路徑、
> 命令、環境變數、API 路徑與 YAML 鍵名一律保留原文，不作翻譯。

行為上的改動應當在做出該改動的同一個提交中寫入本檔案。倉庫構建方式或測試方式
的改動則屬於 CLAUDE.md。

## 架構不變量

這些規則約束的是設計，而非程式碼。違反其中任何一條都不會讓構建或 linter 失敗——
它產生的是一台能啟動、但隨後行為失常的機器，而且失常之處通常離改動很遠。

- **儲存層管理卷；gfeh 提供物件儲存。** `src/storage` 只處理 btrfs 子卷與配額，別無其他——它完全不負責物件儲存。物件、每個檔案的後設資料與權限、分層的使用者/ACL 資料庫、共享、按檔案的 HTTP 暴露、聯邦，以及每一種協議檢視（S3、IPFS、Google Drive、純 HTTP——以及 SMB/CIFS，gfehd 實現了它但 Town OS 不提供服務）都屬於 gfeh，由它負責。絕不要向 `src/storage` 或 `/storage/*` 新增物件/blob/按檔案的端點，也絕不要讓 `storage.Storage` 或 `storage.Controller` 知道使用者、權限或協議。參見 [Storage](#儲存)。

- **Pages 功能始終啟用** —— pages 子系統（通過 Caddy 託管靜態站點）在啟動時無條件初始化；不存在 `TOWN_OS_PAGES` 環境變數開關。正常啟動時 pages manager 非 nil，因此 pages API 始終可用。處理器仍保留一個防禦性的 nil-manager 保護，返回 "pages not configured"（由那些構建伺服器時不帶 `ServerConfig.PagesMgr` 的測試觸發），但真實啟動永遠不會走到那裡。

- **版本變更檢測與單元重啟** —— systemcontroller 通過比較執行中容器的鏡像 SHA（取自 `/proc/1/cgroup` → `podman inspect`）與持久化在 `<btrfsPath>/town-os-version` 的版本檔案來檢測鏡像升級。版本變更時：(1) 拉取所有容器鏡像，(2) 重建 NC 鏡像，(3) reconcile 重新生成所有 systemd 單元，(4) 內容發生變化的單元按順序重啟：先 NC 單元（它們擁有網路），再依賴服務，最後父服務/獨立服務，(5) 對於單元發生變化的容器包，通過 `podman exec` 執行更新後命令（`post_update` 欄位）。版本檔案在 reconcile 成功之後寫入。單元內容通過 `ReadUnit()` 在前後比對，以避免內容未變時的無謂重啟。

- **網路控制器鏡像是拉取的，而非啟動時構建** —— NC 鏡像是一個已釋出的同族鏡像（`quay.io/town/networkcontroller:<tag>`，標籤來自 `resolveImageTag()`），與其他核心鏡像一同拉取，就像 UI、rolodex 與 ingress 鏡像一樣。它**不會**在啟動過程中用 `podman build` 構建；早先的啟動時構建（`localhost/town-os-networkcontroller:local`，alpine 基礎鏡像，`--dns=8.8.8.8`）已經移除。`NC_IMAGE` 覆蓋推匯出的預設值，整合測試框架正是用它注入本地構建的鏡像。拉取失敗不是致命的：每個包的 NC 單元都帶有一個 `ExecStartPre` 的 `--pull=never` 網路建立兜底，因此失敗的拉取可以在下次啟動時恢復。

- **所有監控服務都是系統服務** —— Prometheus、Node Exporter 與監控 UI 全部執行在系統服務名稱空間下（`town-os-system--` 字首），在 reconcile 之前直接由 `main.go` 啟動。它們從不通過包倉庫系統安裝；不存在可安裝的 "monitoring" 包。這三個服務是：`town-os-system--node-exporter.service`（宿主機網路，埠 9100）、`town-os-system--prometheus.service`（埠 9090，從 `{btrfsBase}/monitoring/` 繫結掛載配置與資料）、`town-os-system--monitoring-ui.service`（埠 5308）。監控 UI 服務執行的要麼是 socat 轉發器（uPlot 模式，預設），要麼是 Grafana（grafana 模式），由 `monitoring_backend` 設定控制。Prometheus 配置直接寫入磁碟。Prometheus、Grafana 與 uPlot 的 socat 轉發器都通過設定了 `PackageUnitConfig.SystemServiceKey` 的 `systemd.GeneratePackageUnits` 生成，因此它們同樣獲得完整的網路控制器、socket 啟用與私有 podman 網路——與普通包相同的管路，只是採用系統服務的命名。

- **宿主機卷的屬主在 `HostVolumeMount` 上以宣告方式指定，且不遞迴** —— 內部 uid 寫死的容器鏡像（Grafana 的 `472`、Prometheus 的 `65534` 等）需要寫入其繫結掛載的宿主機路徑，而繫結掛載會直接透傳宿主機的屬主資訊，因此宿主機路徑必須在容器啟動前就屬於該 uid:gid。我們使用繫結掛載（而不是命名的 podman 卷，那樣 podman 會在首次建立時自動 chown），因為我們希望資料位於帶配額的 btrfs 子捲上。`src/systemd/unit.go` 中的 `systemd.HostVolumeMount` 結構體帶有可選的 `UID *uint32` 與 `GID *uint32` 欄位；當兩者都設定時，單元生成器會為該掛載點在 `ExecStartPre=/bin/mkdir -p` 各行之後、`podman run` 之前發出 **`ExecStartPre=/bin/chown <uid>:<gid> <hostpath>`**（不帶 `-R`）。這是系統服務上宿主機繫結掛載卷屬主的唯一宣告式來源，取代了此前在 `GrafanaPackageConfig` 與 `PrometheusPackageConfig` 中手寫的 `ExecStartPreExtra` chown 條目。

  chown 刻意不遞迴，這已經足夠，原因是：
  1. **可寫掛載**（`grafana-data` → `/var/lib/grafana`，`prometheus-data` → `/prometheus`）只需要頂層屬主正確，容器就能在其中建立自己的子目錄。容器程序以自己的 uid（472 或 65534）建立這些子項，因此它們本來就屬主正確，永遠不會漂移。無需遞迴。
  2. **只讀掛載**（`grafana-provisioning` → `/etc/grafana/provisioning`）根本不宣告 UID/GID，也不會產生 chown 行。只要宿主機權限是 0755/0644（`WriteGrafanaProvisioningFiles` 就是這樣設定的），任何 uid 都能讀取其內容，與屬主是誰無關。

  `EnsureGrafanaStorage`（`src/monitoring/monitoring_ui.go`）現在只建立目錄然後返回；它完全不做 chown。`WriteGrafanaProvisioningFiles` 以全域可讀的權限寫出資料來源與儀表板的 YAML/JSON 檔案，之後無需再修正屬主。過去每次啟動都遍歷 `grafana-data` 的、基於 `filepath.WalkDir` 的程序內 chown 已經移除；由 systemd 發出的那一次 `chown` 系統呼叫就是權威的修正。uid/gid 常量仍留在各自的檔案中（`monitoring_ui.go` 中的 `grafanaUID = 472` / `grafanaGID = 472`，`prometheus.go` 中的 `prometheusUID = 65534` / `prometheusGID = 65534`）；除非上游容器鏡像也隨之改變，否則不要改動它們。

- **網路狀態目錄必須與宿主機共享** —— `-network-state` 的預設值是 `/run/town-os`（`src/svc/systemcontroller/cmd/systemcontroller/main.go` 中的 `DefaultNetworkStatePath`）。systemcontroller 執行在容器內，卻通過 `CONTAINER_HOST` 在宿主機上建立 NC 容器，因此繫結掛載的源路徑（每個 NC 單元中的 `-v /run/town-os:/run/town-os:ro`）必須存在於宿主機檔案系統上。install 倉庫中的 systemcontroller systemd 單元必須繫結掛載 `/run/town-os:/run/town-os`，並確保在 systemcontroller 啟動之前該宿主機目錄已經存在（`ExecStartPre=/usr/bin/mkdir -p /run/town-os` 或 `RuntimeDirectory=town-os`）。沒有這個掛載，systemcontroller 的 `os.MkdirAll` 與狀態檔案寫入都會落在容器的 tmpfs 裡，宿主機目錄並不存在，NC 容器隨即以 `Error: statfs /run/town-os: no such file or directory` 啟動失敗——進而拖垮 Prometheus、監控 UI 以及每一個帶網路的包。絕不要把預設值設為 `/var/run/town-os`，或 `/var/run`、`/tmp` 之下的任何路徑；該路徑必須位於 `/run` 之下（或另一個與宿主機共享的繫結掛載中），並且在掛載兩側必須是同一個路徑。

## System Controller 啟動順序

`src/svc/systemcontroller/cmd/systemcontroller/main.go` 中的 system controller 啟動嚴格遵循以下順序。標註 **(非致命)** 的步驟在失敗時向 stderr 記錄日誌並繼續；其餘步驟失敗即為致命，會中止啟動。

啟動過程是**可觀測的**：在任何工作開始之前就先繫結 `:5309`，由一個最小化的啟動狀態樁（stub）承接並流式推送進度；完整的 Echo 路由器在最後被換入，全程不關閉監聽套接字。進度以五個粗粒度階段上報（`boot_controller`、`boot_dns`、`boot_services`、`restart_packages`、`ready`）——參見 [Boot Status and Refresh](#啟動狀態與重新整理)。

1. **設定 `CONTAINER_HOST`** —— `setupPodmanEnv()` 設定 `CONTAINER_HOST=unix:///run/podman/podman.sock`，使後續每一次 `podman` 呼叫（以及子程序）都走宿主機的 podman socket，而不是 systemcontroller 容器隔離的儲存。
2. **解析命令列標誌與環境變數** —— `-db`、`-btrfs`、`-repo-dir`、`-network-state`、`-listen`。環境變數覆蓋：`TOWN_OS_LISTEN`。
3. **用啟動處理器繫結 `:5309`** —— `NewBootStatus()` + `NewRootHandler(NewBootHandler(bs))` 在任何啟動工作之前立即繫結監聽。在第 24 步的切換髮生之前，該套接字只應答 `GET /status/ping`（503，附 `{booting, step, done, boot_id}`）與 `GET /boot-status`（SSE）；其餘一律 403。
4. **階段 `boot_controller`** —— 臨時工作目錄；建立 btrfs 基礎目錄與網路狀態目錄；清除舊部署遺留在 btrfs 根上的陳舊 `town-os.db`（`cleanupStaleRootDB`），並拒絕會重新建立它的 `-db` 路徑（`validateDBPath`）——執行時資料庫位於 `<btrfsBase>/data/db/system.db`，絕不在根目錄。
5. **開啟 SQLite 資料庫** —— 設定了 `-db` 則持久化，否則使用臨時檔案。
6. **初始化帳戶 manager** —— 建立 accounts 表並遷移舊錶（能力列轉為 grants；`smb_nt_hash` 被丟棄）。隨後 `PurgeLegacyServiceAccounts` **(非致命)** 在升級後的首次啟動時，一次性移除物件儲存守護程序舊的管理員帳戶及其儲存的密碼——參見 [No service accounts](#沒有服務帳戶)。
7. **生成臨時 JWT 簽名金鑰** —— 通過 `crypto/rand` 取 32 位元組隨機數，可用 `TOWN_OS_SIGNING_KEY` 覆蓋。初始化會話 manager，它會清除此前所有會話（舊令牌在新金鑰下無效）。
8. **初始化審計、設定、pages 與網路 manager** —— 設定項以預設值播種（`default_quota`、`max_archive_size`、`locale`、`dns_tld`、`dns_resolution_mode`、`peer_ttl` 等）；pages 始終初始化；網路 manager 擁有 WireGuard 網路表與 peer 表，**並播種 home 網路**，因此從此刻起它必然存在（參見 [The home network always exists](#home-網路始終存在)）。
9. **播種倉庫** —— 若 `repositories.json` 不存在，寫入預設倉庫（若設定了 `TOWN_OS_TEST`/`DEBUG` 則寫入測試倉庫）。應用 `TOWN_OS_REPO_USERNAME`/`TOWN_OS_REPO_PASSWORD` 憑據。
10. **初始化倉庫根並強制重新整理** —— 通過 go-git 克隆/拉取所有已配置的倉庫。
11. **初始化安裝 manager、btrfs 儲存、systemd manager**。
12. **解析鏡像標籤** —— `resolveImageTag()`：優先取 `TOWN_OS_TAG` 環境變數（由 install 構建系統設定），否則取 `rc.latest-<arch>`（`defaultVersionTag()`，架構由 `runtime.GOARCH` 經 `archTag()` 對映為 `x86_64`/`aarch64`）。不存在 `/town-os.tag` 檔案，也沒有編譯期的 `Version` 固定值。每一個同族鏡像標籤（UI、rolodex、network controller、ingress）都由這一個值推導；推送標籤是按架構分的，因此推匯出的同族標籤也是。
13. **推導 NC 鏡像** —— `quay.io/town/networkcontroller:<tag>`，可通過 `NC_IMAGE` 覆蓋。它是拉取的（第 17 步），從不構建。
14. **啟動後台倉庫重新整理** —— goroutine 每 5 分鐘輪詢一次。
15. **階段 `boot_dns`：寫入 Rolodex 配置，內容變化則重啟** **(非致命)** —— Rolodex 是由 systemd 管理的啟動服務。systemcontroller 寫出 `rolodex.yml`（冪等：若該檔案比二進位制更新且內容未變則跳過），並且僅在檔案確實被寫入時才重啟服務。`resolution.mode` 來自 `dns_resolution_mode` 設定；儲存值無法解析時回退到預設值，而不是渲染出一份 rolodex 會拒絕的配置。`forwarders:` 來自 `dns_local_forwarders` 設定：開啟時，該列表在每次啟動時從宿主機的解析器中發現，因此換了網路的機器無需操作者做任何事就能用上新的解析器（參見 [Local forwarders](#本地轉發器)）。rolodex 容器以 `--net host` 執行，並直接把 DNS 繫結到 `127.0.0.2:{port}`。隨後等待 DNS 就緒（TCP 連線輪詢），並配置 systemd-resolved 把該 TLD 路由到 rolodex——**當 `TOWN_OS_DNS_PORT` 已把 rolodex 從 `:53` 遷走時，這一步被跳過**，因為 resolved 的按域名伺服器地址不攜帶埠，那樣會讓該 TLD 下的每一次查詢都被黑洞吞掉。
16. **讀取監控後端並發現 btrfs 磁碟裝置** —— `monitoring_backend`（預設 `uplot`）；`monitoring.BtrfsDevices(btrfsPath)` **(非致命)** 通過 `/monitoring/status` 暴露底層塊裝置。
17. **階段 `boot_services`：拉取核心容器鏡像** **(非致命)** —— NC 鏡像、Prometheus、Node Exporter、UI 鏡像、物件儲存（gfeh）鏡像、ingress 鏡像，以及在選中該後端時的 Grafana，通過 `parallelEnsureImages` 並行拉取（鏡像已載入時跳過拉取）。凡是被啟動期單元參照的鏡像都屬於這裡：鏡像不在本地的單元會在 `podman run` 內部自行拉取，於是它的就緒等待要與一次 registry 下載賽跑。gfeh 與隨後的 ingress 曾先後從這份清單中缺席，而每一次看上去都只是某個服務沒起來。監控 UI 無需單獨條目——在 uPlot 後端下它執行的就是 NC 鏡像，而後者已在集合的首位。
18. **啟動監控系統服務** **(全部非致命)** —— 先拆除上一版設計遺留的 NC/socket 監控單元（它們仍佔用 `-p 9090`/`-p 5308`，會讓新服務不斷崩潰重啟）。Node Exporter、Prometheus 與監控 UI 都以 `--net host` 執行；node-exporter 與 Prometheus 繫結環回地址，只有監控 UI 的 `:5308` 面向區域網。這三個埠都來自 `monitoringPortsFromEnv()`，其零值即為生產預設值（[System-service host ports](#系統服務的宿主機埠)）。隨後安裝每夜執行的 podman prune 定時器 **(非致命)**。每日的更新定時器不在這裡安裝——它隨安裝器一同交付，參見[自動更新](#自動更新)。
19. **確保本地 TLS CA 存在** **(非致命)** —— 在 reconcile 之前執行 `tls.EnsureCA(<btrfsPath>/tls)`，這樣 reconcile 遍歷已安裝包時才能簽發葉子證書。
20. **啟動 ingress 與 pages 服務** **(非致命)** —— `ingressctl.Manager` 安裝並啟動 `town-os-system--ingress`（共享的 `:443` SNI + `:80` Host 路由器），僅當宿主機擁有全域 IPv6 時才啟用雙棧。pages 的 Caddy 服務隨之啟動。當 `INGRESS_IMAGE` 被顯式設為空時（開發模式），兩者都會跳過。
21. **Reconcile 物件儲存** **(非致命)** —— `ReconcileGfeh` 確保每個網路有一個 gfeh 分割槽：`gfeh/<network>` 子卷（chown 給 uid 2000）、渲染出的 `gfehd.yaml`，以及 `town-os-system--gfeh-<network>` 單元，且僅在渲染內容發生變化時才重啟。當 `GFEH_IMAGE` 被顯式置空時整體跳過；當 ingress 被停用時也跳過（四個 HTTP 檢視只能經由它訪問）。分割槽的*名稱*會在稍後非同步釋出——見第 30 步。參見 [Object Storage (gfeh)](#物件儲存gfeh)。
22. **檢測版本變更** —— 將執行中容器的鏡像 SHA（`/proc/1/cgroup` → `podman inspect`）與 `<btrfsPath>/town-os-version` 比較。為 reconcile 設定 `versionChanged`。
23. **Reconcile** —— 遍歷所有已安裝的包並恢復執行時狀態：
    - 建立根 btrfs 子卷（`installed`、`uninstalled`、`archives`、`pages`、`vm-images`、`user`、`tls`、`gfeh`）。
    - 對每個已安裝的包（每個 repo/name 取最新版本）：載入 YAML，用儲存的應答編譯，建立帶配額的 btrfs 卷，從歸檔/git/proton 播種空卷，應用檔案模板，簽發該包的 TLS 葉子證書，寫出網路狀態檔案（含解析後的 `fqdn`），生成並安裝 systemd 單元（service + NC + socket），啟動服務。
    - 若 `versionChanged`：重啟內容發生變化的單元（先 NC，再依賴，最後服務），然後執行 `post_update` 命令。
    - Reconcile pages：確保子卷、符號連結與頁面內容就位。
    隨後把當前鏡像 SHA 持久化到 `<btrfsPath>/town-os-version`。
24. **Reconcile DNS 與網路** —— 撥號 rolodex 的 gRPC socket（最多重試 30 秒）。`RebuildDNS` 清空並從零重建 rolodex，從而丟棄上一次崩潰執行留下的漂移；`RebuildNetworkDNS` 為非預設網路的包重新註冊面向區域網的全域記錄（以及 DANE pin）。隨後 `ReconcileNetworks` 將 home 網路的 TLD 與 `dns_tld` 對齊，並拉起每一個已啟用網路的 WireGuard 介面，同時傳入 rolodex 客戶端，使每個網路的 TLD 作用域都被認領——包括僅 DNS 的 home 作用域。全部非致命。之後物件儲存會被**第二次** reconcile（冪等），這樣本步驟拉起的網路無需等待重啟即可獲得自己的分割槽。
25. **編排 ingress** **(非致命)** —— 等待就緒，撥號其 gRPC socket，`RebuildIngress` 以宣告式方式推送完整路由集（HTTP 包 + pages + 物件儲存的檢視與索引），與 `RebuildDNS` 是同一模型。它還會在同一遍中，從構建這些路由所用的同一站點集合渲染每個分割槽的索引頁——路由不能在它所服務的位元組存在之前就被編排（[The partition index](#分割槽索引頁)）。
26. **啟動 UI 容器** **(非致命)** —— `town-os-system--ui.service`；當 `UI_IMAGE` 被顯式置空時跳過（開發模式，此時由 bun 提供 UI）。
27. **階段 `restart_packages`：新鮮度階段** —— 若上一個程序留下了重新整理標記，則序列重啟每一個已安裝的包單元，併為每個包發出一條進度事件，讓 UI 各渲染一行。崩潰遺留的陳舊標記是無害的。
28. **建立 HTTP 處理器** —— 把所有 manager 接入 `ServerConfig`，啟動後台輪詢器（每小時的外部 IP、DNS 漂移修復、過期 peer 回收），並配置 Echo 路由器的 CORS、失敗即拒的 grant 白名單、鑑權與審計中介軟體。
29. **階段 `ready`：切換根處理器** —— 在已經繫結的監聽套接字上，把啟動樁原子地替換為完整的 Echo 路由器，因此不會出現埠抖動，進行中的 `/boot-status` SSE 訂閱者也能安然跨過這次交接。隨後 `BootStatus.Done()` 關閉該事件流。**系統至此就緒。**
30. **釋出物件儲存的名稱** **(非致命，後台)** —— `publishGfehNames` 等待至少一個分割槽的管理 socket 有應答，然後重新執行 DNS 與 ingress 重建，使每個分割槽 `/v1/names` 的輸出變成 A 記錄、TLSA pin、葉子證書 SAN 與 ingress vhost。它在切換**之後**、且以非同步方式執行，因為 gfehd 在認證之前會輪詢 `/status/ping`——而後者在第 29 步之前一直返回 503——所以在此處同步等待會讓它所等待的這次啟動自我死鎖。若屆時沒有任何分割槽就緒，這些名稱會由下一次 reconcile 釋出。
31. **優雅關閉** —— 收到 SIGINT 時：取消 context，以 30 秒超時關閉 HTTP 伺服器。所有後台 goroutine 通過 context 取消退出。

# Town OS 功能規格說明

Town OS 是面向家庭使用者的自託管雲平台。它完全從 U 盤在記憶體中執行，把系統的全部儲存用於使用者資料。打包、儲存與網路是完全一體化的。一個 Web UI 為非技術使用者提供管理介面。

## Git 庫

所有內部 git 操作都使用純 Go 庫（`go-git/go-git/v5`），而不是呼叫 `git` 命令列。

### 客戶端介面

`git.Client` 介面抽象了所有 git 操作：

- **Clone** —— 把倉庫克隆到父目錄下的一個具名子目錄中。
- **Pull** —— 以 rebase 方式拉取。
- **Diff** —— 報告工作樹是否存在未提交的改動。
- **Stash / StashApply** —— 暫存與重新應用未提交的改動。
- **Fetch** —— 從 origin 遠端拉取。
- **Checkout** —— 檢出分支、標籤或提交雜湊。
- **Init** —— 初始化新倉庫。若父目錄不存在則返回錯誤。
- **Add** —— 按 pathspec 暫存檔案（支援用 `"."` 表示全部檔案）。
- **Commit** —— 使用本地 git 使用者配置建立提交（回退為 `Town OS <town-os@localhost>`）。
- **RevParse** —— 把一個引用解析為 SHA 雜湊。
- **Run** —— 分發任意 git 子命令（`config`、`branch`、`rev-parse --abbrev-ref`、`log`、`init`、`status`）。

### 實現

`GoGitClient` 使用 `go-git` 實現該介面。它支援：

- URL 中內嵌的憑據（`scheme://user:pass@host/...`），會被提取並作為 `http.BasicAuth` 傳遞。
- 所有操作都支援基於 context 的超時與取消。
- 一個 `Home` 欄位，可覆蓋 HOME 目錄以進行隔離操作。

### Mock 客戶端

`MockClient` 提供執行緒安全的 mock 實現，用於單元測試。它記錄所有方法呼叫及其引數，並支援按方法注入錯誤與返回值。

### 使用場景

- **包倉庫**：克隆、拉取（對髒工作樹前後配合 stash/apply）與 fetch，用於倉庫重新整理（通過 `GoGitClient`）。
- **卷播種**：在安裝與 reconcile 期間把 git 倉庫克隆進空卷（通過 `GoGitClient`）。
- **Pages**：克隆並更新靜態站點倉庫（通過 `GoGitClient`）。
- **Git 源重建**：更新已安裝包的 git 卷並重啟依賴它的服務（通過 `GoGitClient`）。

## 倉庫管理

### 倉庫模型

倉庫由名稱、URL 與可選憑據（使用者名稱與密碼）定義。它們儲存在基礎目錄下的 `repositories.json` 檔案中。若未配置任何倉庫，則播種一個預設倉庫。

### 倉庫 API

- `POST /repository/add`（需要管理員）—— 新增新倉庫。接受名稱、URL 與可選的使用者名稱/密碼憑據。若未提供憑據，則使用系統預設憑據。倉庫會通過 go-git 克隆，並觸發一次重新整理。
- `POST /repository/remove`（需要管理員）—— 按名稱移除倉庫並觸發重新整理。
- `POST /repository/move`（需要管理員）—— 改變倉庫的優先順序位置。接受名稱與目標位置索引。
- `POST /repository/refresh`（需要管理員）—— 強制重新整理所有倉庫。返回任何重新整理錯誤。
- `GET /repository`（需要鑑權）—— 列出所有倉庫，支援搜尋、排序與分頁。每一項包含名稱、URL、使用者名稱，以及任何重新整理錯誤。

### 倉庫重新整理

倉庫會週期性重新整理（預設間隔 5 分鐘），通過 go-git 從 origin 拉取。重新整理過程中對髒工作樹前後使用 stash/apply。重新整理錯誤按倉庫跟蹤，並通過列表介面與狀態 ping 介面暴露。

## 包系統

### 包定義

包以 YAML 定義，結構如下：

- `image` —— 容器鏡像引用（與 `vm` 互斥）。
- `vm` —— 虛擬機器配置（與 `image` 互斥）。見下文 **VM 配置**。
- `proton` —— 用於 Windows 執行檔的 Proton/Wine 執行器配置（與 `vm` 和 `command` 互斥）。見下文 **Proton 配置**。
- `entrypoint` —— 字串列表，在 podman run 時替換鏡像內建的 `ENTRYPOINT`。以 `podman run --entrypoint='["..."]'` 形式發出（JSON 陣列，用單引號包裹，使 systemd 原樣轉發）。對於上游 ENTRYPOINT 是一個拒絕任意命令引數的包裝指令碼的鏡像，這是必需的（例如 `matrixdotorg/synapse` 的 `/start.py` 把第一個引數解釋為 "mode"，遇到任何未知值就報錯——因此想用 `command: [sh, -c, "…"]` 的包必須同時設定 `entrypoint: [sh, -c]`，讓 podman 徹底替換掉 `/start.py`）。僅限容器執行時；對 VM 包會被拒絕（`ErrEntrypointVMNotSupported`），對 Proton 包也會被拒絕（Proton 會自動生成自己的命令）。
- `command` —— 字串列表，成為容器的 CMD（在 entrypoint **之後**傳入的 argv）。僅限容器執行時；與 `proton` 互斥。包含空白或 shell 元字元的多詞引數會在生成的單元檔案中用單引號包裹，使 systemd 的 ExecStart 分詞器把它們作為單個 argv 元素轉發——一個串聯的 `"a && exec b"` 字串仍是一個引數，其中的 `&&` 會被轉發給 `sh -c`（當 entrypoint 為 `[sh, -c]` 時），而不是被 systemd 拆開。
- `environment` —— 鍵值形式的環境變數（支援模板替換；僅限容器執行時）。
- `network` —— 外部與內部埠對映（支援模板替換）。
- `volumes` —— 具名卷，含掛載點、可選配額、可選歸檔來源、可選 git 播種 URL，以及可選的 UID/GID。
- `questions` —— 安裝期間向用戶呈現的具名問題。
- `notes` —— 帶型別的後設資料（URL、電話、郵箱），安裝後展示。型別在編譯期校驗：URL 必須能解析為合法 URL，郵箱必須匹配 `user@domain.tld` 格式，電話號碼必須是數字加可選的格式化字元。
- `description` —— 人類可讀的包描述。
- `supplies` —— 該包提供的能力列表。
- `archives` —— 安裝時用於填充卷的容器鏡像歸檔列表（僅限容器執行時）。
- `templates` —— 具名檔案模板，通過 Go text/template 渲染進卷中。每個模板指定目標卷、檔案路徑與模板內容。
- `post_update` —— 在 reconcile 期間檢測到鏡像 SHA 變化後，於執行中的容器內執行的 shell 命令列表（僅限容器執行時；VM 包不支援）。見下文 **更新後命令**。

### 執行時型別

每個包都有執行時型別：`container`（預設）或 `vm`。執行時由出現的頂層欄位決定：`image`（或 `proton`）選擇容器執行時（podman），`vm` 選擇 VM 執行時（QEMU）。一個包必須恰好指定 `image`/`proton` 與 `vm` 之一；兩者都指定或都不指定都是校驗錯誤。Proton 包是容器包的一種特化形式——它們使用容器執行時，但會自動生成命令，並從另一個容器鏡像中提取 Windows 應用檔案。

### VM 配置

`vm` 段配置一台 QEMU 虛擬機器：

- `image` —— VM 磁碟鏡像 URL 或本地檔名（必填）。可以是指向遠端鏡像的 HTTP/HTTPS URL，也可以是引用 `vm-images` 子卷中已快取鏡像的檔名。支援 `@variable@` 模板替換。
- `memory` —— VM 記憶體，人類可讀的位元組字串（例如 `2gb`、`512mb`）。預設 `1gb`。支援 `@variable@` 模板替換。
- `cpus` —— 虛擬 CPU 數量。預設 `1`。必須為非負數。

### Proton 配置

`proton` 段配置一個通過 Proton/Wine 相容層執行的 Windows 應用：

- `app_image` —— 包含 Windows 應用檔案的容器鏡像引用（必填）。編譯期會被規範化。支援 `@variable@` 模板替換。
- `app_directory` —— 容器內應用安裝位置的絕對路徑（必填，例如 `/app`）。支援 `@variable@` 模板替換。
- `volume` —— 應用檔案將被提取到的、已定義的包卷名稱（必填）。支援 `@variable@` 模板替換。
- `exe` —— 要執行的 Windows 執行檔路徑（必填，例如 `/app/myapp.exe`）。支援 `@variable@` 模板替換。
- `args` —— 傳給執行檔的可選命令列引數。每個元素都支援 `@variable@` 模板替換。

安裝時，系統拉取 `app_image`，把 `app_directory` 提取到指定卷中，並自動生成容器命令 `proton run <exe> [args]`。用於執行該應用的容器鏡像取自系統級的 `proton_image` 設定（預設 `quay.io/town/proton:latest`），可通過設定 `image` 按包覆蓋。在 reconcile 期間，僅當目標卷為空時才會重複執行應用提取。

### 模板變數

模板替換使用 `@variable_name@` 語法。變數在包編譯期被替換為問題的應答。替換適用於：環境變數值、網路埠名稱與目標、卷掛載點、卷配額、卷歸檔引用、卷 git URL、VM 鏡像 URL，以及 VM 記憶體值。另有兩個內建變數可用：`@LOCAL_EXTERNAL_HOST@` 與 `@LOCAL_INTERNAL_HOST@`。

`@@` 序列是字面量 `@` 的轉義。若要產生一個字面 `@` 緊跟一個模板變數，使用三個 `@`：`@@@variable@`。例如 `ssh://git@@@PACKAGE_DNS@:@sshport@` 解析為 `ssh://git@gitea.default.home:2222`。單獨的 `@@` 解析為 `@`（例如 `admin@@example.com` → `admin@example.com`）。

注意：note 的編譯使用單遍解析器（`ApplyTemplates`），它把上下文變數（`PACKAGE_DNS`、`LOCAL_EXTERNAL_HOST`、`LOCAL_INTERNAL_HOST`）與使用者應答合併到一遍中處理，從而正確處理 `@@` 轉義。其他欄位（環境變數、埠、卷）使用按鍵解析器（`applyTemplate`），它在多遍處理中保留 `@@`，並在 `Compile` 結束時做最後一次 `@@` → `@` 解析。

### 問題（Questions）

問題在包安裝期間提示使用者。每個問題有 `query`（展示文本）、可選的 `type`（用於校驗的輸出型別）與可選的 `default` 預設值。問題名稱必須以字母或數字開頭，且只能包含字母、數字與下劃線（例如 `port`、`dbpass`、`registration_secret`）。短橫線、點號與其他標點會被拒絕；允許下劃線，是因為問題名稱會被用作 `@template@` 標記，而 `registration_secret` 這類多詞識別符號在真實包中很常見。

#### 輸出型別

- **port** —— 校驗過的埠號（1–65535）。當應答為空或為 `"auto"` 時，自動在 10000–60000 範圍內生成一個可用的隨機埠。
- **hostname** —— 小寫字母數字加短橫線。為空時自動生成 `<package-name>-<4位十六進位制>`。
- **volume** —— 字母數字加短橫線與下劃線。
- **bytes** —— 人類可讀的位元組大小（`mb`、`gb`、`tb` 字尾）。
- **archive** —— 歸檔檔名。
- **duration** —— 時間長度（`s`、`m`、`h`、`d` 字尾）。
- **secret** —— 當應答為空或為 `"auto"` 時自動生成一個密碼學安全的值。通過 `crypto/rand` 生成 32 位元組，返回 64 字元的十六進位制字串（256 位熵）。適用於密碼、加密金鑰鹽值等秘密值。使用者可提供明確應答來覆蓋。
- **boolean** —— 是/否選項，在安裝問題對話方塊中渲染為**核取方塊**而非文本輸入。校驗使用 `strconv.ParseBool`，它恰好接受 yaml.v3（YAML 1.2）視為布林的那些寫法，外加 `1`/`0`/`t`/`f`，且不區分大小寫；`yes`/`no` **不被**接受。應答會被規範化為字串 `"true"` 或 `"false"`，因此 `@variable@` 替換與檔案模板（`{{.Responses.key}}`）看到的始終是同一種規範形式，可以用 `{{if eq .Responses.key "true"}}` 判斷。

  未勾選的核取方塊不會提交任何內容，而依賴包的布林問題也常常不被其父包回答——若不處理，這兩種情況都會觸發 `Compile` 的空應答校驗。因此 `autoGenerateResponses`（`controller_install_preview.go`）會把缺失或為空的布林值解析為該問題的 `default`（規範化後），若未宣告預設值則解析為 `"false"`。來自表單的顯式 `"false"` 始終優先於 `default: "true"`，這樣預設開啟的選項才真的能被關掉；無法被 `strconv.ParseBool` 解析的 `default` 是包的缺陷，會讓安裝失敗，而不是悄悄以關閉狀態安裝。

  包資訊對話方塊把儲存的布林應答渲染為 Yes/No，而不是原始的 `"true"`/`"false"` 字串；布林問題在安裝對話方塊中繞過"快取值 + 清除按鈕"的路徑——已儲存的應答只是預先勾選核取方塊，並且保持可直接編輯。

- **oauth** —— 通過在安裝對話方塊中執行裝置流（device flow）獲得的令牌，而非手工輸入。校驗方式與 secret 相同（任意非空字串），從不自動生成，並在包資訊對話方塊中被掩碼。安裝對話方塊在文本框的位置渲染一個 **Connect** 按鈕；來自上一次安裝的快取應答會渲染為"已連線"，因此重灌不會把操作者再趕回供應商那裡一趟。

#### OAuth 問題

有些應用需要一個只有其供應商才能簽發的憑據——Plex 帳戶令牌、GitHub 個人令牌——而獲取它的唯一辦法一直是手工執行一個 shell 指令碼，再把它列印的內容貼上過來。`oauth` 問題改為直接在對話方塊中執行該流程。

**不存在供應商登錄檔**。問題自帶一個 `oauth:` 塊，其中寫明供應商自己的 URL，因此任何提供裝置式流程的供應商都可以被包使用，而無需改動 Town OS：

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

`start` 開啟流程；`extract` 指定要從其響應中提取的 JSON 欄位；`approve` 是瀏覽器開啟的 URL；`poll` 會被反覆執行，直到由 `token` 指定的 JSON 欄位不再缺失或為 null——在協議層面，這恰恰就是"使用者尚未授權"的樣子。`{{...}}` 佔位符針對提取出的值以及 `{{client_id}}` 解析，後者是控制器在每一步都發送的、每次流程隨機生成的識別符號（Plex 會把 pin 與它繫結）。提取出的 JSON 數字會被渲染為數字串，而不是 `1.234567e+06`——浮點格式化的 pin id 會讓輪詢 URL 返回 404，並永遠卡在 "pending"。

該流程的實現位於 `src/packages/oauth.go`（模式定義與校驗）與 `src/svc/systemcontroller/controller_oauth.go`（執行）。`POST /packages/oauth/start` 執行 start 步驟並返回 `{flow_id, approve_url, user_code, interval_ms}`；`POST /packages/oauth/poll` 執行一次輪詢並返回 `pending`、帶令牌的 `approved`，或 `expired`。兩者都需要管理員權限。伺服器只在令牌被兌取之前保留該流程——令牌被交給瀏覽器，瀏覽器再像其他任何應答一樣把它作為該問題的答案提交，因此在服務端保留副本只會多出一處洩露點。

校驗分為兩半，把它們混為一談就是缺陷。`ValidateOAuthSpec` 檢查流程的*形狀*（必填欄位、可解析的時長、URL 的 host 中不含模板），這是安裝包時 `Compile` 所執行的。`ValidateOAuthFlow` 是它再加上下面的地址策略，只在流程即將被*執行*時執行。安裝發生在流程執行很久之後，而且發生在一台 `Compile` 無法看到其 `OAuthAllowPrivate` 設定的主機上——所以在編譯期套用地址策略，會拒絕掉一個其自身流程剛剛成功過的安裝。

**地址守衛是關鍵的承重結構。** 包指定任意 URL，而是*控制器*去撥號它們，因此沒有守衛的話，一個包就能把它指向宿主機自己的網路。`packages.CheckOAuthAddr` 執行在 HTTP 客戶端的 `DialContext` 中（每次重定向也會執行），拒絕環回、私有、鏈路本地、組播、未指定與 CGNAT 地址；URL 必須是 `https`。在撥號時而非解析時檢查，正是它能防住 DNS 重繫結的原因。`ServerConfig.OAuthAllowPrivate` 放寬這一限制，它的存在僅僅是為了讓測試能把流程指向 127.0.0.1 上的 `httptest` 伺服器。

#### 可選問題

任何問題都可以設定 `optional: true`。其他所有問題都必須以非空值作答，這讓包作者無法表達"應用確實可以沒有"的設定項——SMTP 中繼、API 金鑰——除非編造一個佔位預設值，然後指望操作者會覆蓋它。

可選問題可以完全不出現在應答 map 中，或以空字串作答；`Compile` 免除它的 `ErrMissingResponse` 與 `ErrEmptyResponse` 校驗，並在其 `@variable@` 出現處替換為**空字串**。空白應答同樣跳過 `OutputType.Output`——後者的職責恰恰是為帶型別的問題拒絕空值（空字串不是合法埠）——因此 `optional` 與 `type` 可以組合：作答過的可選埠仍會按埠校驗，而空白的則被編譯為無。

有兩個細節對正確性至關重要。`Compile` 是通過遍歷收到的應答來做替換的，因此完全不出現在 map 中的問題會經過第二遍處理，用空字串填充它的標記；沒有這一遍，字面量 `@smtp_host@` 就會一路殘留進容器的環境變數。另外，`autoGenerateResponses` 在進入型別分支之前就跳過可選問題：為它生成值會讓"可選"失去意義，因為一個空白的可選 secret 否則就會變成一個隨機的 256 位字串，而應用會老老實實拿它去嘗試認證。空白的可選問題若聲明瞭 `default` 則回退到預設值，否則回退到空字串。

`optional` 對布林問題沒有意義，因為布林是核取方塊，永遠會解析為它兩個取值之一。

#### 條件問題（`show_if`）

問題可以帶上 `show_if: <boolean_question>`，指向同一個包中的某個布林問題。安裝對話方塊在該核取方塊被勾選之前一直隱藏這個問題，因此包可以把一組進階選項——SMTP 中繼、API 金鑰——收納到一個開關之後，而不是把所有欄位一次性攤到操作者面前。

它不只是 UI 提示：編譯器也遵守它。只要控制它的布林值解析為 false，該條件問題就被編譯為**空字串**，並免除"必須作答且非空"的要求——完全等同於它被標為 `optional` 且留空——*無論那個仍然掛載著的欄位提交了什麼*。`questionHidden`（`src/packages/questions.go`）從提交的應答中讀取控制值，若操作者從未碰過它則回退到該布林問題宣告的 `default`，並且解析時較為寬鬆，因為未勾選的核取方塊可能以 `"false"`、`"0"` 的形式到達，也可能根本不到達。`Compile` 對隱藏的問題強制使用空字串並跳過 `Output()`，因此陳舊的值不會讓一個操作者根本看不見的欄位栽在型別校驗上；完全不出現在應答 map 中的問題，其 `@marker@` 出現處同樣會被填為空字串。當布林值為真時，非可選的條件問題照常為必填。

`ValidateShowIf` 會拒絕以下幾種 `show_if`：指向不存在的問題（`ErrShowIfUnknown`）、指向型別不是 `boolean` 的問題（`ErrShowIfNotBool`）、指向問題自身（`ErrShowIfSelf`），或指向另一個本身就是條件問題的問題（`ErrShowIfChain`——不允許鏈式）。只有當控制其可見性的東西是一個純粹的核取方塊時，條件問題才是自洽的。

### 編譯

編譯會校驗所有應答、施加型別相關的校驗、替換所有模板變數、規範化容器鏡像 URL，併產出一個已解析的 `Package` 結構體。對於 VM 包，記憶體字串會被解析為位元組數，並應用 CPU 預設值。更新後命令會被去除首尾空白。校驗錯誤會被收集起來一併返回。

### 更新後命令（Post-Update Commands）

`post_update` 欄位是一組 shell 命令字串，在 system controller 於 reconcile 期間檢測到鏡像 SHA 變化之後，於執行中的容器內執行。這使自動化遷移任務成為可能（例如 PostgreSQL 容器更新後執行 `pg_upgrade`）。

- **僅限容器** —— 對 VM 包，`post_update` 在校驗期被拒絕（`ErrPostUpdateVMNotSupported`）。
- **模板替換** —— 每條命令都支援來自問題應答的 `@variable@` 替換，與環境變數和網路欄位一致。
- **空白裁剪** —— 每條命令在編譯期被去除首尾空白。空命令或僅含空白的命令在校驗期被拒絕。
- **執行觸發條件** —— 只有當 `ReconcileConfig.VersionChanged` 為真**且**該包的 systemd 單元內容與先前安裝的單元不同時，命令才會執行。任一條件不滿足，就不會執行任何命令。
- **執行順序** —— 命令在所有版本變更引發的重啟完成之後按順序執行（先 NC 單元，再依賴，再服務，最後才是更新後命令）。在同一個包內，命令按列表順序執行。
- **執行方式** —— 每條命令通過 `podman exec <container-name> sh -c '<command>'` 執行，超時為 5 分鐘。`ReconcileConfig` 上的 `PostUpdateExec` 函式提供執行機制；為 nil 時停用更新後命令的執行。
- **非致命** —— 命令失敗會被記錄，但不會中止 reconcile，也不會阻止後續命令執行。

包 YAML 示例：

```yaml
image: postgres:16
post_update:
  - "pg_upgrade --check"
  - "pg_upgrade"
  - "vacuumdb --all --analyze-in-stages"
```

### 檔案模板

模板是包 YAML 中的具名物件，含三個欄位：`volume`（目標卷名）、`path`（卷內檔案路徑）與 `content`（Go text/template 字串）。

模板資料上下文提供四個名稱空間：

- `.Responses.key` —— 問題應答值（以問題名為鍵）。
- `.Package.Name`、`.Package.Version`、`.Package.Repo`、`.Package.Image`、`.Package.Description` —— 包後設資料。
- `.System.Hostname`、`.System.ExternalIP`、`.System.InternalIP` —— 系統級資訊。
- `.Dep.KEY.Host` 與 `.Dep.KEY.Ports` —— 已安裝依賴的執行時座標，鍵名與父包 YAML 在 `dependencies:` 下宣告的 dep key 相同。`Host` 是 podman 容器名（可通過共享網路上的 podman DNS 解析）；`Ports` 是 `map[string]string`，同時以數字容器埠（例如 `"5432"`）和該依賴網路條目上宣告的任意語義名（小寫，例如 `"sql"`）為鍵。用 `{{index .Dep.db.Ports "sql"}}` 訪問具名埠。對於沒有依賴的包，該 map 為 nil；對不存在的依賴使用 `{{.Dep.db.Host}}` 會渲染出 `<no value>`（與其他任何缺失的 map 鍵一樣），而對 nil 的 `Ports` 使用 `index` 會刻意報錯，從而讓配置有誤的模板大聲失敗。

`volume` 與 `path` 欄位支援 `@variable@` 替換（與環境變數、網路和卷欄位使用的是同一機制）。`content` 欄位使用 Go `text/template` 語法，如 `{{.Responses.key}}`、`{{.Package.Name}}`、`{{.Dep.KEY.Host}}` 等。`content` 內部**不**支援 `@dep_*@` 標記形式——請改用 Go 模板的 `.Dep` 名稱空間；`@dep_*@` 在 `environment:` 值與依賴的 `responses:` 塊中仍是正確形式。

模板在卷播種（歸檔、git 克隆）**以及所有依賴安裝完成之後**才應用，因此渲染父包內容時 `.Dep` 已經填充完畢。reconcile 期間模板會被重新渲染，但已存在的檔案絕不會被覆蓋，從而保留來自歸檔上傳或先前執行的資料；依賴 map 會從持久化的依賴記錄中重建，因此當 reconcile 確實需要寫出一個缺失的模板時，`.Dep` 仍然可以解析。

校驗強制要求：模板名稱遵循卷命名約定（字母數字加點、短橫線與下劃線），路徑必須是相對路徑且不含目錄穿越，`volume` 必須引用一個已定義的包卷（除非該欄位中含有模板變數），並且 `content` 必須能解析為合法的 Go `text/template`。

### 鏡像規範化

容器鏡像引用在編譯期被規範化：
- 單一名稱（`nginx`）變為 `docker.io/library/nginx:latest`。
- 兩段式（`user/app`）變為 `docker.io/user/app:latest`。
- 完整引用被保留；若不含標籤則追加 `:latest`。

### 應答持久化

應答按版本儲存在 `responses/<repo>/<pkg>/<version>.json`。另有一份 `last` 副本儲存在 `responses/last/<repo>/<pkg>.json`，供升級以及從已解除安裝卷重新安裝時複用。安裝成功後 last 應答會被清除。

有兩個 API 端點管理 last 應答：

- `POST /packages/last-responses`（需要管理員）—— 取回某個包快取的 last 應答（按 repo 與 name）。
- `POST /packages/clear-last-responses`（需要管理員）—— 刪除快取的 last 應答檔案。

### 安裝問題 UI

使用者安裝包時，問題對話方塊會載入已有應答（來自當前安裝），若不存在則載入快取的 last 應答（來自上一次解除安裝）。當前應答優先於 last 應答。

**快取應答**以只讀的樣式化容器展示，背景色較淡，顯示已儲存的值（密碼顯示為 `********`）。一個隱藏的表單輸入保留該值以便提交。每個快取欄位都有一個清除按鈕（X 圖示）並帶工具提示（"Clear to enter a new value"），點選後把只讀展示替換為可編輯輸入框。清除按鈕採用 ghost 樣式，懸停時變紅。

**預設值**在沒有快取值時以兩種方式呈現：作為輸入框的佔位文本（例如 "Default: 8080"），以及作為輸入框下方的淺色輔助文本，其中的值用等寬字型。當未定義預設值時，會展示與型別相關的佔位文本：埠為 "Auto-assigned if empty"，主機名為 "Auto-generated if empty"，時長為 "e.g. 30s, 5m, 2h, 1d"。

來自伺服器的**校驗錯誤**按欄位以紅色文本顯示在輸入框下方，並且輸入框會帶上紅色邊框。

**尺寸與分頁。** 對話方塊高度上限為視口高度（減去邊距），並以 flex 列布局，因此頁首與頁尾保持固定，而問題區域滾動——否則基礎 `DialogContent` 的 `overflow-hidden` 會讓問題很多的包溢位部分無法觸及。問題**每頁 5 個**分頁展示，配有 Previous/Next 控制元件，在最後一頁讓位給 Install 按鈕。每一頁都保持掛載狀態（非活動頁為 `display:none`），這樣非受控的表單輸入會保留已輸入的值並且仍會被提交；解除安裝某一頁會悄無聲息地丟掉該頁上的答案。欄位錯誤會跳轉到承載它的那一頁，因此校驗錯誤絕不會被藏在分頁器背後。分頁器複用既有的 `datatable.next`/`previous` 字串與一個數字頁碼計數，因此不會新增翻譯鍵。

用 `show_if` 宣告的**條件問題**在其控制核取方塊被勾選之前保持隱藏（參見 [Conditional questions](#條件問題show_if)）。

**OAuth 問題**依據每個問題單一的狀態渲染——`idle`、`starting`、`waiting`、`connected`、`error`——該狀態由快取應答播種，而不是由"某處是否存在令牌"決定。過去，來自上一次安裝的快取令牌會讓該欄位在任何事情發生之前就顯示為已連線，並且在一次失敗的重連過程中繼續如此顯示，於是綠色的 Connected 徽標壓在紅色錯誤之上。現在令牌只被用於一個判斷（Connect 還是 Reconnect），此外它僅僅是隱藏輸入所提交的內容：一次失敗的重連讓操作者仍保有原先的令牌，但不會有任何東西聲稱這次失敗的嘗試成功了；正在進行中的重連不會顯示為已連線；而一次不攜帶令牌的授權是錯誤，而不是會安裝一個空憑據的靜默成功。

### 包資訊對話方塊

包資訊對話方塊以帶標籤的列表展示 notes。notes 按其型別渲染：URL 型別是可點選的超連結，在新標籤頁開啟（`target="_blank"`）；email 型別是 `mailto:` 連結，開啟使用者的郵件客戶端；phone 型別是 `tel:` 連結。無型別的 note 渲染為不帶連結的純程式碼塊。

### 包清單 API

`POST /packages/manifest`（需要鑑權）返回某個包的原始 YAML 定義。接受 repo、name 與 version。以 `Content-Type: text/x-yaml; charset=utf-8` 返回檔案內容。若包檔案不存在則返回 404。

### 包操作下拉選單

在包列表 UI 中，每一行包都有一個 `...` 下拉選單（平鋪檢視與按倉庫分組檢視都有）。下拉選單包含：

- **Info**（僅已安裝的包）—— 開啟包資訊對話方塊，展示問題、應答與編譯後的 notes。
- **Manifest** —— 開啟一個對話方塊展示原始 YAML 包定義，並附帶複製按鈕。
- **Version/Repository** —— 以停用項的形式展示版本與倉庫名。
- **Uninstall**（僅已安裝的包）—— 觸發解除安裝確認對話方塊。

### 精選包（Featured Packages）

每個倉庫都可以包含一個 `featured.json` 檔案，內含一個包名的 JSON 陣列。它們由 `LoadFeatured` 載入，並隨包列表一起在 `RepoPackageGroup` 中返回。平鋪的包列表 API 會為每一項設定 `featured` 布林值。分組的包列表 API 即使在搜尋過濾縮減了包列表時，也會在每個分組上保留 `Featured` 陣列。

- `GET /packages`（需要鑑權）—— 列出包，支援搜尋、排序、分頁，以及可選的 `featured_only` 與 `installed_only` 過濾。
- `GET /packages/featured`（需要鑑權）—— 列出所有倉庫中的精選包。
- `GET /packages/by-repo`（需要鑑權）—— 按倉庫分組列出包。接受 `search` 與 `featured_only` 查詢引數。

#### 精選包過濾器

平鋪的包列表 API（`GET /packages`）與分組的包列表 API（`GET /packages/by-repo`）都接受 `featured_only` 查詢引數。設為 `"true"` 時只返回被標記為精選的包。該過濾與 `installed_only` 取交集——兩者可同時生效。在 UI 中，一個 "Featured only" 核取方塊切換該過濾。精選過濾的預設狀態是 `true`（首次訪問時只顯示精選包）。過濾偏好（`pkg_group_by_repo`、`pkg_installed_only`、`pkg_featured_only`）持久化在 `localStorage` 中。

### 已安裝包過濾器

平鋪的包列表 API（`GET /packages`）接受 `installed_only` 查詢引數。設為 `"true"` 時只返回已安裝的包。過濾在服務端於搜尋、排序與分頁之前施加，確保頁數與偏移量正確。在 UI 中，一個 "Installed only" 核取方塊切換該過濾並把分頁重置到第一頁。

### 包的安裝與解除安裝

#### 安裝 API

`POST /packages/install`（需要管理員）安裝一個包。接受 repo、name、version、responses 與可選標誌：

- `reuse_volumes` —— 複用先前已解除安裝版本的卷。
- `import_from_version` —— 從指定的先前版本匯入卷。
- `skip_response_reuse` —— 不從先前安裝自動填充答案。

安裝過程會：從倉庫中的包檔案建立硬連結到 installed 目錄，持久化應答，建立帶配額與可選 UID/GID 的卷，從歸檔與 git 播種卷（僅限容器執行時），應用檔案模板，生成 systemd 單元檔案，寫出網路狀態檔案，安裝並啟動 systemd 單元，並在成功後清除 last 應答。last 應答在安裝前儲存，以便解除安裝時恢復。對於 VM 包，VM 磁碟鏡像會在生成單元之前被下載並轉換為 raw 格式（若為遠端 URL）；卷播種（歸檔、git 克隆）則被跳過。

#### 解除安裝 API

`POST /packages/uninstall`（需要管理員）解除安裝一個包。接受 repo、name、version 與可選標誌：

- `purge_volumes` —— 立即刪除所有相關卷。

不做清除時，卷會從 `installed/` 字首移動到 `uninstalled/` 字首。網路狀態檔案被刪除，systemd 單元被停止、停用並解除安裝。

**依賴級聯。** 解除安裝父包會遞迴解除安裝它擁有的每一個依賴。級聯讀取父包持久化的依賴記錄（`LoadDependencies`），並深度優先遍歷每個子項，在每一層都重複查詢，因此巢狀的子依賴（`parent--dep--child--dep--grandchild`）也會被移除。對每個依賴，級聯會登出其 DNS 記錄、解除安裝其 systemd 單元（service + NC + socket）、刪除其網路狀態檔案、呼叫 `inst.Uninstall` 丟棄安裝記錄，並根據 `purge_volumes` 是否設定，要麼清除其卷，要麼把它們移動到 `uninstalled/` 字首。級聯在 `uninstallDependencies`（`src/svc/systemcontroller/controller_install_dependencies.go`）中實現，並在父包自身解除安裝完成之後執行。不存在引用計數：每個依賴恰好屬於一個父包（其安裝記錄位於 `installed/<repo>/<parent--dep--key>/`），因此在兩個父包下分別安裝的"共享"依賴其實有兩份獨立記錄，解除安裝其中一個父包只會移除它自己的那一份。

#### 已安裝包資訊

`POST /packages/installed/info`（需要鑑權）返回某個已安裝包的問題、應答、編譯後的 notes 與 note 型別。

**非管理員只拿得到 notes，別無其他。** 該路由保持 `requireAuth`，因為儀表盤會為每個帳戶渲染每個已安裝服務的 notes——那正是 notes 的用途——但 `type: secret` 的問題的答案是生成的憑據，`type: oauth` 的答案是供應商令牌，因此把完整的應答 map 返回給任何有登入權的人，就等於把每個包的憑據都交出去。問題本身也被扣下：一個問題的 `query` 無害，但把它與一份被塗黑的應答 map 配對，只會告訴對方有什麼東西被藏起來了；而唯一渲染問題的介面是僅限管理員的安裝對話方塊。僅僅丟掉這個 map 還不夠——note 正是由這些答案編譯出來的，因此 `redactSecretsInNotes` 會掩碼任何被 note 引用到的 secret 或 oauth 答案，且按值匹配，這樣從不引用它們的 note 會被完整保留。短於六個字元的答案不作處理：兩個字元的 secret 並不是任何人選擇的憑據，而掩碼它的每一次出現只會把無關的 note 文本撕得粉碎。

#### 包版本

`POST /packages/versions`（需要鑑權）按名稱列出某個包的可用版本。

#### 包問題

有兩個端點用於獲取包的問題：

- `POST /packages/questions`（需要管理員）—— 按包名獲取問題（最新版本）。
- `POST /packages/questions/identity`（需要管理員）—— 按 repo、name 與 version 獲取問題。

### 時區處理

UI 維護一份常見 IANA 時區名稱的靜態副本，並提供 `getTimezoneOffsetMinutes()` 工具函式，使用瀏覽器的 `Intl` API 在客戶端計算 UTC 偏移。伺服器通過狀態 ping 響應暴露本機系統的 UTC 偏移分鐘數。

### 安裝預覽

- `POST /packages/install-preview`（需要鑑權）—— 預覽安裝某個包會建立些什麼。接受 repo、name 與 version。返回 repo、name、version、description、image、卷、埠、升級資訊、執行時型別，以及該包是否含有問題。對於 VM 包，預覽還包含 VM 配置（鏡像 URL、人類可讀的記憶體量與 CPU 數）。

### 子包

- `POST /packages/children`（需要鑑權）—— 列出給定 repo 與包名下的子包名稱。

### 已解除安裝卷列表

- `POST /packages/uninstalled-volumes`（需要鑑權）—— 檢查某個包是否有上次解除安裝遺留的卷。返回是否存在已解除安裝的卷、已解除安裝版本列表與已安裝版本列表。

### 已安裝包管理

- `GET /packages/installed`（需要鑑權）—— 列出所有已安裝的包，支援搜尋、排序與分頁。
- `POST /packages/responses`（需要管理員）—— 按 repo、name 與 version 獲取某個已安裝包儲存的應答。
- `POST /packages/purge-volumes`（需要管理員）—— 永久刪除某個已安裝包的卷。

### 包的啟用/停用

- `POST /packages/disable`（需要管理員）—— 停用一個包。設定 disabled 標誌並停止所有相關的 systemd 服務。
- `POST /packages/enable`（需要管理員）—— 重新啟用一個被停用的包。清除 disabled 標誌並啟動所有相關的 systemd 服務。

除核心的 `Install`、`Uninstall`、`ListInstalled` 與 `GetResponses` 方法外，`Installer` 介面還支援 `SetDisabled`、`IsDisabled` 與 `IsPackageChanged`。

### 已解除安裝卷管理

- `POST /packages/purge-uninstalled-volumes`（需要管理員）—— 永久刪除某個包所有已解除安裝的卷。

## 儲存

儲存使用帶配額約束的 btrfs 子卷。

### 關注點分離：卷 vs. 物件儲存

**儲存層管理卷。gfeh 提供物件儲存。儲存層完全不處理物件儲存——gfeh 才是負責方。**

`src/storage` 建立、調整大小、重新命名、快照和刪除 btrfs 子卷，並報告磁碟使用情況。這就是它的全部職責。它絕不能知道什麼是物件、桶（bucket）、鍵、檔案控制代碼、內容識別符號（CID）、ACL、共享或協議檢視。對儲存層而言，子卷就是一片帶配額的、不透明的位元組場地。

gfeh（`gitea.com/town-os/gfeh`，一個以 `town-os-system--gfeh` 形式釋出的 Rust 系統服務）擁有這條線以上的一切：物件名稱空間、每個檔案的後設資料與權限、分層的使用者/ACL 資料庫、共享、按檔案的 HTTP 暴露、向外部服務的聯邦，以及每一種協議檢視（S3、IPFS、Google Drive、純 HTTP；SMB/CIFS 在 gfehd 中存在，但 [Town OS 不提供該服務](#不提供-smb-檢視)）。它使用儲存層，純粹是為了給自己分割槽所在的子卷做置備與擴容，之後便在繫結掛載的子樹上自行進行直接 I/O。

改動任何一側時都必須遵守的後果：

- **不要**向 `src/storage` 或 `/storage/*` API 新增物件、blob、鍵值或按檔案的端點。若某個功能需要定址單個檔案，它屬於 gfeh。既有的 `upload-archive`/`download-archive` 端點是用於卷播種的 tar 傳輸通道，不是物件 API，也不得朝那個方向生長。
- **不要**讓 `storage.Storage` 或 `storage.Controller` 知道使用者、權限或協議。配額是儲存層強制的唯一策略。
- gfeh 分割槽位於保留的 `gfeh/` 子卷字首之下。它們是**在程序內**通過 `storage.Storage` 的 `CreateFilesystem`/`ModifyFilesystem` 置備的，而不是通過 `/storage/*` HTTP API：`createFilesystem` 會無條件地把每一個提交的名稱改寫為 `user/<name>`（`controller_storage.go`），因此那條路由不可能產出任何其他字首下的卷。分割槽置備因此需要自己的 `/gfeh/partitions/*` 處理器，這也把保留字首的強制、配額策略與審計日誌集中在一處，而不是在 gfeh 中重複一遍。

- **gfeh 依賴一份成文的契約，而這裡的改動可能破壞它。** gfeh 倉庫中的 `TOWNOS_CONTRACT.md` 列出了 gfeh 依賴 Town OS 的每一條路由、行為與不變量——`user/` 改寫、保留字首規則、`/gfeh/partitions/*` 的狀態碼、無法區分的鑑權失敗，以及空 `Account.Networks` 的失敗即拒含義——並鎖定了它據以驗證的 Town OS 版本。gfeh 模擬該契約，使其測試無需 root、systemd、podman 或 btrfs 即可執行。

  **改動 `src/storage`、`src/account` 或 system controller 的路由時，請在 gfeh 檢出目錄中重新執行 `make check-townos-sync`。** 漂移的模擬器會讓 gfeh 拿到一份全綠的測試套件和一個壞掉的部署。模擬器與契約文件要一起對齊；絕不能只改其一。

這套整合在 Town OS 一側的內容——分割槽路由、按網路劃分的守護程序、管理 socket，以及名稱如何進入 DNS 與 ingress——見 [Object Storage (gfeh)](#物件儲存gfeh)。

### 檔案系統操作

`Storage` 介面提供：

- **CreateFilesystem** —— 建立帶可選配額的新 btrfs 子卷。
- **ModifyFilesystem** —— 修改卷的名稱和/或配額。
- **RemoveFilesystem** —— 刪除卷。
- **ListFilesystems** —— 列出卷，支援按字首與狀態（`user`、`installed`、`uninstalled`）過濾、排序、分頁與搜尋。當找不到 btrfs 掛載點時返回空列表（而非錯誤）。
- **RenameFilesystem** —— 重新命名卷。
- **SnapshotFilesystem** —— 建立 btrfs 快照。
- **DiskUsage** —— 報告磁碟使用統計。

配額在 btrfs qgroup 層面強制執行。配額為 0 表示不限。

### 儲存 API

- `POST /storage/create`（需要鑑權）—— 以名稱與可選配額建立新的使用者檔案系統。
- `POST /storage`（需要鑑權）—— 列出檔案系統，支援按字首與狀態過濾、排序、分頁與搜尋。
- `POST /storage/modify`（需要鑑權）—— 修改卷的名稱和/或配額。只有使用者檔案系統允許重新命名；包卷不能重新命名。
- `POST /storage/remove`（需要鑑權）—— 刪除使用者檔案系統。
- `POST /storage/package-volumes`（需要鑑權）—— 按包分組列出包卷，可選擇是否包含已解除安裝的卷。
- `POST /storage/remove-package-volume`（需要管理員）—— 按內部名稱刪除某個具體的包卷。
- `POST /storage/remove-package-volume-group`（需要管理員）—— 儲存樹中非葉節點刪除按鈕背後的級聯刪除。`repo` 與 `name` 必填；`version` 為空則針對該包的所有已安裝版本。**在移除任何子卷之前，目標包依賴樹中的每一個 systemd 單元都會被停止**，因此仍然開啟著某個卷的 podman 容器不可能與 btrfs 刪除發生競爭。`include_uninstalled` 會額外清掃匹配的 `uninstalled/` 子樹（與驅動卷列表的那個 "Show uninstalled" 開關相連）。
- `POST /storage/upload-archive`（需要管理員）—— 上傳歸檔並解包進某個卷。
- `POST /storage/download-archive`（需要管理員）—— 把卷作為壓縮歸檔下載。

### 卷名稱空間

- **使用者卷** —— 磁碟上為 `user/<name>`。`user/` 字首由 create、remove、modify 與 list 處理器透明地新增，並在 API 響應中剝離，因此 API 使用方只看到裸名稱。`user` 根子卷在啟動時由 reconcile 建立。
- **已安裝包卷** —— `installed/<repo>/<name>/<version>/<volname>`。
- **已解除安裝包卷** —— `uninstalled/<repo>/<name>/<version>/<volname>`。
- **歸檔儲存** —— `archives/` 字首（系統管理）。
- **VM 鏡像** —— `vm-images/` 子卷（系統管理）。存放快取的 raw 格式 VM 磁碟鏡像。
- **物件儲存分割槽** —— `gfeh/<network>`，每個 Town OS 網路一個，屬主為 uid/gid 2000。屬保留區：`/storage/create` 無法產出它（該路由把每個名稱都改寫為 `user/<name>`），因此它們通過 [`/gfeh/partitions/*`](#協議一分割槽置備gfehpartitions) 置備。

所有字首根名稱（`installed`、`uninstalled`、`archives`、`pages`、`vm-images`、`user`、`gfeh`）都是保留的，使用者不能直接建立、修改或刪除它們。歸檔的上傳與下載在遇到不帶內部字首的子卷名稱時，會通過新增 `user/` 字首來解析。

**除非字首之後的名稱無法爬回上層，否則字首不構成邊界。** `filepath.Join` 會摺疊 `..`，因此提交給一個會新增 `user/` 字首的處理器的 `../gfeh/home`，會變成 `user/../gfeh/home`，從而定址到另一個網路的物件儲存分割槽——而且它也能溜過保留名稱檢查，因為該檢查匹配的是一個此時穿越尚未產生的前導字首。因此 `storage.ValidateFilesystemName`（不允許前導斜槓、不允許空位元組、不允許空的或 `.`/`..` 的路徑分量，並限制字元集）在 `ModifyFilesystem` 中被施加於**兩個**名稱——只校驗重新命名目標，會讓呼叫方把別人的子卷挪進自己的名稱空間——同時也施加於 `RemoveFilesystem`，後者過去完全不做校驗，而它偏偏是破壞性的那一個。`/storage/*` 的處理器在新增 `user/` 字首**之前**校驗提交的名稱，這正是保留名稱檢查名副其實的原因。這些路由是 `requireAuth` 而非 `requireAdmin`，因此這個問題此前對機器上任何普通帳戶都是可達的。

**list** 的字首被刻意豁免：`nest/` 是呼叫方索取 `nest` 之下全部內容的方式，沒有任何東西會把它拼接進檔案系統路徑（儲存層從自身基準目錄列舉，並把它當作字串過濾器使用），而 `user/` 是無條件新增的，因此帶穿越的字首只會匹配不到任何東西，而不是夠到任何東西。

### 歸檔格式檢測

歸檔的壓縮格式通過檢查上傳流開頭的魔數字節來檢測。前 6 個位元組經由帶緩衝的 reader 預讀，並與已知簽名比對：

- **gzip** —— `0x1f 0x8b`
- **bzip2** —— `0x42 0x5a 0x68`（`BZh`）
- **xz** —— `0xfd 0x37 0x7a 0x58 0x5a 0x00`（`\xfd7zXZ\x00`）

無法識別的簽名會被立即拒絕。副檔名也會被獨立校驗以確認格式。

### 歸檔流校驗

格式檢測之後，解壓後的流通過 `io.TeeReader` 被校驗為 tar 歸檔。tee 的一路餵給 Go 的 `archive/tar` reader 以校驗 tar 頭；另一路餵給真正的 `tar -xf` 解包程序。若校驗發現 tar 流非法，解包會被中斷。解壓在可用時使用並行實現：gzip 用 `pigz`，bzip2 用 `lbzip2`，xz 用 `xz`。

### 歸檔上傳

`POST /storage/upload-archive`（需要管理員）接受一個 multipart 表單：

- `subvolume`（必填）—— 目標子卷路徑。
- `archive`（必填）—— 歸檔檔案。支援格式：`.tar`、`.tar.gz`/`.tgz`、`.tar.bz2`/`.tbz2`、`.tar.xz`/`.txz`。
- `subpath`（可選）—— 卷內用於解包的相對路徑；按需建立。
- `stop_service`（可選）—— 解包前停止、完成後重啟的 systemd 單元名。

歸檔直接以流式處理，不落臨時檔案。解包後會校驗路徑穿越（解析符號連結）。最大上傳大小預設 1 GB（`max_archive_size` 設定）。解包超時預設 600 秒（`archive_unpack_timeout` 設定）。

### 歸檔下載

`POST /storage/download-archive`（需要管理員）接受一個 JSON 請求體：

- `subvolume`（必填）—— 源子卷路徑。
- `paths`（可選）—— 子卷內要包含的具體路徑陣列。
- `stop_service`（可選）—— 歸檔期間停止、完成後重啟的 systemd 單元名。
- `format`（可選）—— 壓縮格式：`tar.gz`（預設）、`tar.bz2` 或 `tar.xz`。
- `filename`（可選）—— 下載檔案的自定義基礎名。伺服器會清洗該值（去除路徑分隔符與控制字元），移除任何已有的歸檔副檔名以防重複，併為所選格式追加相應副檔名。未提供或清洗後為空字串時預設為 `download`。

返回所請求格式的流式歸檔。壓縮分別使用 `pigz`、`lbzip2` 或 `xz`。Content-Type 與 Content-Disposition 的 filename 頭會與所選格式和自定義檔名保持一致。提供 `paths` 時只包含匹配的路徑。

### 從容器鏡像自動歸檔

包定義中可以包含引用容器鏡像的 `archives` 段。在安裝與 reconcile 期間，空卷會通過拉取鏡像、建立臨時容器並把指定目錄複製進卷的方式來填充。

### Git 卷播種

卷可以指定帶倉庫 URL 的 `git` 欄位。在安裝與 reconcile 期間，空卷會通過克隆該倉庫來播種（超時 5 分鐘）。該 URL 可以引用模板變數，使使用者能通過問題應答覆蓋倉庫地址。已有資料絕不會被覆蓋。克隆失敗會被記錄並跳過（非致命）。

### Git 源重建

`POST /packages/rebuild-git`（需要管理員）更新某個已安裝包的 git 播種卷。它通過 go-git 為每個 git 卷拉取最新改動，然後重啟依賴它的服務。需要包的 repo、name 與 version。重建前會針對已儲存的應答重新求值模板變數。

### VM 鏡像管理

VM 包需要 raw 格式的磁碟鏡像。遠端鏡像會被下載並用 `qemu-img convert -O raw` 轉換；轉換後的 `.raw` 檔案快取在 `vm-images` 子卷中。後續安裝複用該快取鏡像。本地鏡像引用直接從 `vm-images` 子卷解析。

- `GET /vm-images`（需要鑑權）—— 列出已快取的 VM 磁碟鏡像。為每個鏡像返回名稱與檔案大小。
- `POST /vm-images/upload`（需要管理員）—— 從 URL 下載 VM 鏡像並轉換為 raw 格式。接受一個 URL 與可選名稱。名稱預設取 URL 的檔名並加 `.raw` 副檔名。下載超時為 30 分鐘。轉換後的鏡像存入 `vm-images` 子卷。
- `POST /vm-images/delete`（需要管理員）—— 按名稱移除已快取的 VM 鏡像。

### 展示名稱剝離

已安裝與已解除安裝包卷的 API 響應會剝離路徑中的前導倉庫段（例如 `default/nginx/2.0/data` 變為 `nginx/2.0/data`）。完整的磁碟路徑保留在 `internal_name` 欄位中，供需要它的操作使用（例如在歸檔操作中推導用於停止/啟動的 systemd 服務名）。

### 儲存 UI

儲存管理介面分為兩部分：

**使用者檔案系統** —— 一個可分頁、可排序、可搜尋的資料表。每行都有 Modify（名稱與配額）與 Delete 按鈕。建立對話方塊會用系統 `default_quota` 設定預填配額欄位。

**包卷** —— 一棵按包組織的層級樹。每個包是一個可摺疊的樹標題，顯示：卷總數、版本數、聚合配額與安裝狀態徽標。當一個包有多個版本時，會顯示版本子標題，並帶上按版本的配額與狀態。當 "Show uninstalled volumes" 開關啟用時，已解除安裝的卷也會包含進來。

每一行葉子卷都顯示配額與狀態，並提供三個操作：

- **Download**（圖示按鈕）—— 開啟一個對話方塊，含可選的檔名欄位（下載檔案的基礎名；歸檔副檔名會自動追加）、壓縮格式選擇器（gzip、bzip2、xz）、可選的逗號分隔路徑過濾，以及一個在下載期間停止依賴服務的核取方塊。使用 File System Access API 做流式儲存，並回退到 blob 下載。
- **Upload**（圖示按鈕）—— 開啟一個對話方塊以選擇歸檔檔案（`.tar`、`.tar.gz`、`.tgz`、`.tar.bz2`、`.tbz2`、`.tar.xz`、`.txz`），含可選的解壓子路徑，以及一個在上傳期間停止依賴服務的核取方塊。
- **Modify**（按鈕）—— 開啟一個對話方塊，顯示卷名、狀態與關聯的服務名，並提供一個修改配額的欄位。對包卷而言名稱欄位不可編輯。

## Pages

Pages 是靜態站點託管功能，支援三種內容來源型別：歸檔上傳、容器鏡像與 git 倉庫。使用者指定一個域名或子域名，系統通過一個 Caddy 容器提供內容服務。更新通過重建或重新上傳手動觸發。

### 資料模型

每個 page 站點包含：唯一名稱（主鍵）、來源型別（`archive`、`container_image` 或 `git`；預設 `archive`）、倉庫 URL（git 必填）、分支（預設 `main`）、容器鏡像引用（container_image 必填）、鏡像目錄（container_image 必填）、域名（預設取 page 名稱）、狀態（`pending`、`active` 或 `error`）、一個**網路**，以及建立/更新時間戳。Pages 儲存在一張 SQLite 表中。

`Network` 是該 page 的釋出網路，與包的安裝網路完全一致：它決定 page 的主機名、葉子證書 SAN、DANE TLSA 屬主與 ingress vhost 都在哪個 TLD 之下命名，也決定誰能解析這個 page。為空——即零值與資料庫預設值——表示預設/home 網路，與包的 `Installer.LoadNetwork` 是同一約定。參見 [Pages are network-scoped too](#pages-同樣是按網路限定作用域的)。它在建立時被接受，也是部分更新欄位之一。

Pages 內容儲存在 `pages/` 字首下的 btrfs 子卷中。每個 page 獲得一個位於 `pages/{name}` 的子卷，以及一個位於 `pages-webroot/{name}`、指向 `/data/pages/{name}` 的符號連結。`pages` 字首是保留的，不能通過通用儲存 API 重新命名或刪除。

### Pages API

所有變更端點都需要管理員鑑權；列表端點需要普通鑑權。

- `POST /pages/create`（需要管理員）—— 建立新 page。接受名稱、來源型別、倉庫 URL、分支、域名、容器鏡像與鏡像目錄。來源型別預設為 `archive`。校驗隨來源型別而變：git 需要倉庫 URL；container image 需要鏡像與鏡像目錄兩者。會建立 btrfs 子卷與 webroot 符號連結。git 與 container image 型別的 page 以非同步方式置備（克隆或鏡像提取）；狀態在成功時從 `pending` 轉為 `active`，失敗時轉為 `error`。archive 型別的 page 會保持 `pending` 狀態，直到通過 `/pages/upload` 上傳內容。未提供域名時預設取 page 名稱。
- `POST /pages/upload`（需要管理員）—— 為 archive 型別的 page 上傳內容。接受含 `name` 與 `archive` 檔案的 multipart 表單。僅對來源型別為 `archive` 的 page 有效；其他來源型別返回 400。使用與儲存歸檔上傳相同的魔數格式檢測、副檔名校驗與流校驗。直接解包進該 page 的 btrfs 子卷。成功時狀態置為 `active`，失敗時置為 `error`。
- `POST /pages/update`（需要管理員）—— 對 page 的倉庫 URL、分支、域名、來源型別、容器鏡像或鏡像目錄做部分更新。只有提供了的欄位才會被修改。
- `POST /pages/remove`（需要管理員）—— 從資料庫中刪除 page，移除 webroot 符號連結，並刪除 btrfs 子卷。
- `POST /pages/rebuild`（需要管理員）—— 行為隨來源型別而變：git 型別拉取最新改動（若缺少 `.git` 則重新克隆）；container image 型別通過 podman 從鏡像重新提取；archive 型別返回 400（請改用 `/pages/upload` 重新上傳）。
- `GET /pages`（需要鑑權）—— 列出所有 page，支援排序、搜尋與分頁。可按名稱、倉庫 URL、分支、域名、來源型別、狀態與時間戳排序。

### Pages UI

Pages 管理介面展示一個可分頁、可排序、可搜尋的資料表，列包括名稱、域名、來源型別、倉庫 URL、分支與狀態。來源型別以徽標展示。狀態以帶顏色的徽標展示（active 為預設色，error 為紅色，pending 為次要色並帶旋轉的載入圖示與 "Provisioning..." 文字）。

建立對話方塊頂部有一個來源型別下拉框（Archive Upload / Container Image / Git Repository，預設 Archive Upload）。欄位隨所選來源型別動態變化：git 顯示倉庫 URL 與分支；container image 顯示鏡像引用與鏡像目錄；archive 顯示一個可選的檔案上傳輸入。對於 git 與 container image 型別的 page，提交表單會觸發置備：所有輸入被停用，提交按鈕顯示帶 "Provisioning..." 文字的載入動畫，且對話方塊不能被關閉。UI 每 2 秒輪詢一次 page 狀態，最多輪詢 60 秒。對於選擇了檔案的 archive 型別 page，上傳在建立之後同步進行。

每行的操作隨來源型別而變：archive 型別顯示 Upload 按鈕；git 與 container image 型別顯示 Rebuild 按鈕（帶確認）。所有 page 都有 Edit 與 Delete 操作。編輯對話方塊顯示與該 page 來源型別相符的欄位。

## 物件儲存（gfeh）

gfeh 是 [關注點分離](#關注點分離卷-vs-物件儲存) 中所述分工的物件儲存那一半：`src/storage` 擁有 btrfs 子卷與配額，gfeh 擁有物件、按檔案的權限、使用者/ACL 森林、共享，以及每一種協議檢視。本節講的是這條邊界在 Town OS 一側的內容——守護程序如何部署，以及跨越這條邊界的每一種協議。

`gfehd` 是一個釋出到 crates.io 的 Rust 二進位制，在此打包為 `quay.io/town/gfeh`（`Containerfile.gfeh`），因為 gfeh 自己的倉庫並不提供鏡像。它是**每個分割槽一個程序**，而不是單一的多租戶守護程序。

### 部署形態：每個網路一個分割槽

一個**分割槽**由一個 btrfs 子卷、一個 `gfehd` 程序、一個管理 socket，以及**它自己的一套使用者**構成。每個 Town OS 網路恰好有一個分割槽，因此物件儲存的名稱空間是被劃分 DNS 與 WireGuard 的同一條邊界所劃分的：`office` 分割槽中的一個主體（principal）、一項授權和一個暴露，在 `home` 中毫無意義。

| 內容 | 位置 |
|---|---|
| 分割槽資料 | `<btrfsBase>/gfeh/<network>` → 容器內 `/data/<network>` |
| 配置 | `<btrfsBase>/gfeh-control/<network>/gfehd.yaml` → `/etc/gfeh/gfehd.yaml` |
| 管理 socket | `<btrfsBase>/gfeh-control/<network>/run/admin.sock` → `/run/gfeh/admin.sock` |
| 單元 | `town-os-system--gfeh-<network>.service` |

路徑輔助函式位於 `src/gfeh/layout.go`——`PartitionVolume`、`ConfigPath`、`SocketPath`、`ServiceKey`、`NetworkFromKey`——它們是唯一拼裝這些字串的地方。

socket 位於 btrfs 上，因為那是 gfehd 容器與 systemcontroller 容器都能看到的唯一檔案系統；`ingressctl` 為其 gRPC socket 用的是同一招。gfehd 以 **uid/gid 2000** 執行（`gfeh.UID`/`gfeh.GID`），而繫結掛載會直接透傳宿主機屬主資訊，因此分割槽子卷在建立時就被 chown 給該 uid——這也是 `storage.Filesystem` 帶有可選 `UID`/`GID`、`storage.Controller` 帶有 `Chown` 的原因。不遞迴，理由與 `HostVolumeMount` 的 chown 相同：守護程序以自己的 uid 建立自己的子項，因此它們本來就屬主正確，永遠不會漂移。

**埠。** 四個 HTTP 檢視在**每個分割槽上都繫結固定且相同的容器埠**——s3 9000、http 9001、drive 9002、ipfs 9003——並且**不釋出任何宿主機埠**。這樣做之所以安全，恰恰是因為它們不釋出宿主機埠：每個容器有自己的網路名稱空間，ingress 通過容器名訪問它，就像訪問一個包一樣。兩個分割槽都在 9000 上提供 S3 服務也不會衝突，在併發的 `make test-full` 下同樣如此。

**沒有任何分割槽釋出宿主機埠**，因為 SMB——唯一需要宿主機埠的檢視，既不是 HTTP，也無法置於 ingress 之後——是[不提供服務的](#不提供-smb-檢視)。`DefaultSMBPortBase`（`4450`）與 `GFEH_SMB_PORT_BASE` 保留但未被使用，這樣即使該檢視將來回歸，測試框架的設定也不會造成危害。

### 協議一：分割槽置備（`/gfeh/partitions/*`）

這四條路由之所以存在，是因為 `createFilesystem` 會無條件把每個提交的名稱改寫為 `user/<name>`，因此 `/storage/create` **不可能**產出 `gfeh/` 字首下的卷。它們在 `TOWNOS_CONTRACT.md` 中被宣告，而 gfeh 的 Rust 客戶端解析的正是這些確切形狀，因此**這裡的改動是契約變更，不是重構**。在 gfeh 檢出目錄中執行 `make check-townos-sync` 是捕捉漂移的手段；`controller_gfeh_partitions_test.go` 則在本側固定這些線上形狀。

| 路由 | 鑑權 | 請求 | 響應 |
|---|---|---|---|
| `POST /gfeh/partitions/create` | 管理員 | `{name, quota}`，name **不帶**字首 | `Filesystem` `{name:"gfeh/<n>", quota}` |
| `POST /gfeh/partitions/modify` | 管理員 | `{name, quota}` | `Filesystem` |
| `POST /gfeh/partitions/remove` | 管理員 | `{name}` | 200，空體 |
| `POST /gfeh/partitions` | 鑑權 | 無請求體 | `Filesystem` 的**純 JSON 陣列** |

有兩個細節是承重的：

- **列表返回的是裸陣列，而非 `PageResult`。** Town OS 其他所有列表端點都分頁；這一個不能，因為 gfeh 的 `list_partitions()` 直接反序列化為 `Vec<Filesystem>`，而帶分頁信封的響應在 Rust 側無法解碼。
- **字首是不對稱的。** 請求攜帶裸名稱，響應攜帶 `gfeh/<name>`。該字首是 Town OS 的名稱空間產物，並非分割槽身份的一部分；gfeh 的 `Partition::from_volume` 在接收時會把它剝掉。

gfeh 客戶端會據以分支的狀態碼：**409** 已存在（它的置備邏輯是"建立或擴容"，並靠這個狀態碼區分二者——否則，一個除首次啟動外每次啟動時分割槽都已存在的守護程序，就只能成功啟動那一次），**404** 不存在，**400** 名稱非法，**403** 非管理員。含路徑分隔符的名稱在這條邊界上被拒絕，因為 gfehd 在它自己那側也會拒絕；對"什麼是合法分割槽名"意見不一，會讓 `../user/something` 定址到物件儲存根目錄之外的卷。

處理器在程序內呼叫 `storage.Storage`，絕不走 `/storage/*`，因此保留字首的強制、配額策略與審計日誌都留在同一處。這些路由**不在** `grantRoutes` 中——置備一棵權限樹的根不是某項授權所能買到的東西，因此持有授權的帳戶會在任何處理器執行之前就被全域白名單拒絕。

### 協議二：管理 socket（`/v1/*`）

每個守護程序的管理介面都是**僅在 Unix socket 上**的 JSON-over-HTTP——絕不使用埠。沒有令牌，也沒有認證：socket 上的檔案系統權限就是訪問控制，因此能夠訪問它就已經意味著在這台機器上擁有 root。`src/gfeh/client.go`（`UnixClient`）是 Go 側實現；它把 `DialContext` 固定到該 socket，並使用一個假的 `http://gfeh` 權威名。

| 呼叫 | 方法 + 路徑 | 用途 |
|---|---|---|
| `Health` | `GET /v1/health` | 存活性；同時也是就緒探針 |
| `Names` | `GET /v1/names` | 該分割槽希望釋出的名稱 |
| `ListPrincipals` / `CreatePrincipal` / `DeletePrincipal` | `GET`/`POST` `/v1/principals`，`DELETE /v1/principals/<name>` | 該分割槽的使用者森林 |
| `ListGrants` / `CreateGrant` / `RevokeGrant` | `GET /v1/grants?principal=`，`POST /v1/grants`，`DELETE /v1/grants/<id>` | ACL |
| `ListExposures` / `WithdrawExposure` | `GET /v1/exposures`，`DELETE /v1/exposures/<token>` | 已釋出的 `/f/<token>` 連結 |

gfehd 把內部錯誤對映到 HTTP 狀態碼（404/409/400），而 `StatusError.Unwrap` 再把它們映射回 Go 的哨兵錯誤，因此 `errors.Is` 可用。

新增使用者是 `POST /v1/principals {name, parent, ceiling}`——**沒有密碼**，這正是 UI 從不索要密碼的原因。ceiling 遵循 gfeh 的投影規則：Town OS 管理員為 `all`，其他為讀/寫。授權會被 gfehd 收斂到該主體的 ceiling 之內，因此 UI 展示的是*返回回來*的權限，而不是傳送出去的權限：管理員必須能看到一項授權被收窄了。

### 協議三：名稱——gfeh 回答，Town OS 組裝

**gfeh 從不註冊 DNS 記錄或 ingress 路由。** `RebuildDNS` 呼叫 `TeardownTLD`，`RebuildIngress` 用完整的推導集合呼叫 `SetRoutes`——兩者都會摧毀外來狀態——因此 gfeh 直接註冊的任何東西，都只能存活到下一次 reconcile。取而代之的是，`GET /v1/names` 返回帶檢視與埠的**標籤**（`s3.<partition>`），由 Town OS 組裝出區域。因此這些名稱是在每次重建時被*詢問*的，而不是被推送一次。

`gfehFQDN(label, tld)`（`gfeh_tls.go`）把標籤在網路的 TLD 之下限定，並且是 A 記錄、葉子證書 SAN、DANE TLSA 屬主與 ingress vhost 都必須一致同意的那一個字串——與 `packageFQDN` 和 `pageFQDN` 所維護的是同一條不變量。它**總是**限定：它不查詢 `isPublicFQDN`，因為每個 gfeh 標籤本身就含有一個點（`s3.gfeh`），而那個判定會把任何這樣的名稱讀作公共 FQDN，結果會讓每個名稱都不被限定，併為一個無人擁有的域名申請 ACME 證書。

**它同時也是標籤從一根線上的字串變成 vhost、DNS 記錄和檔案系統路徑的那個咽喉點**，因此 `gfeh.ValidateLabel` 只在這裡施加，別無他處。ingress 的 vhost 被寫作 `https://<hostname> {` 且不加引號，因此一個攜帶換行與花括號的標籤會閉合這個塊並另開一個——而 Caddy 不會只拒絕那一個壞 vhost，它會拒絕整份配置，並把機器上的每一個名稱一起拖下水。校驗不通過的標籤產出空字串，而每個呼叫方本來就會丟棄空的 FQDN，因此畸形的名稱貢獻的是"沒有記錄、沒有路由、沒有證書、沒有目錄"，而不是一個壞掉的。長度（`gfeh.NameMaxLen`）是在**組裝後**的名稱上檢查的，而非只檢查標籤：在限長之內的標籤，在很長的 TLD 之下限定後仍可能超限，而 DNS 承載不了的名稱，證書與 vhost 同樣不該聲稱擁有。

釋出方式與包和 pages 完全一致：

- **雙棲 DNS** —— 非預設網路的分割槽會獲得一條位於本機 overlay IP 的作用域 A 記錄（服務於該網路的 WireGuard peer），*以及*一條位於區域網 IP 的全域 A 記錄，二者分別由 `RebuildDNS` 與 `RebuildNetworkDNS` 中的合併邏輯寫入。DANE TLSA 在兩側都會被固定。
- **TLS** —— 每個名稱一張本地 CA 簽發的葉子證書，並把本機在該網路上的 overlay IP 作為 SAN 帶上，使 peer 能夠直接用 WireGuard 裸地址撥號。
- **Ingress** —— 每個 HTTP 檢視一個 vhost，後端為共享的 `town-os-ingress` podman 網路上的 `<container>:<port>`。`dedupeIngressRoutes` 以"先到先得"的方式守護路由集合，因為 Caddy 會因為一個重複的 vhost 而拒絕整份配置。

`IsHTTPView` 把守最後這一步，並且**未知**檢視被當作非 HTTP 處理：為不講 HTTP 的東西建 vhost，會先接受 TLS 握手然後失敗，這比完全沒有路由更糟。（非 HTTP 的檢視會貢獻一條 DNS 記錄而沒有 ingress 路由；當前提供服務的四個檢視全是 HTTP。）

### 分割槽索引頁

gfeh 提供的每一個檢視都應答某種**協議**，沒有一個應答瀏覽器：HTTP 檢視恰好只有一條路由 `/f/{token}`，因此它的根是 404；S3 對任何它無法解析為操作的請求返回 XML 錯誤；Drive 與 IPFS 應答各自的 API。於是，任何人拿到一個新名稱後會做的那一件事——開啟它——報告的是物件儲存壞了，而事實上那裡從來就沒有可看的東西。

每個分割槽在 **`gfeh.<tld>`** 釋出一個靜態索引頁——即 `gfeh.IndexLabel`，它取自 `VolumePrefix` 而不是把字串 `"gfeh"` 再寫一遍，因為索引必須落在它所索引的那些檢視標籤的父節點上。不需要學習任何新名稱：檢視本來就是 `s3.gfeh`、`http.gfeh`、`drive.gfeh`、`ipfs.gfeh`。

- **它由 `collectGfehSites` 作為一個普通的 `GfehSite` 貢獻出來**，這正是要點所在：它從為檢視推導全部六項內容的同一段程式碼那裡繼承 A 與 AAAA 記錄、作用域 overlay 記錄、DANE pin、葉子證書 SAN 與 ingress 路由，因此 vhost 與證書不可能由不同的字串拼出。只有當該分割槽至少有一個由 ingress 承載的檢視時才會新增它——一個什麼都不可瀏覽的分割槽的索引頁，只會是一個名稱、一張證書和一條路由，只為渲染一句"這裡沒什麼可看"。
- **它由 pages 容器提供服務，而不是 gfehd。** 靜態 HTML 不需要自己的伺服器，而把它作為 Caddy 的 `respond` 響應體內聯發出，會把生成的標記放進配置檔案裡，那裡一個轉義錯誤就會讓 Caddy 拒絕一切。
- **內容位於它自己的 `gfeh-index/` 根之下**，與 `gfeh/` 平級，理由和 `gfeh-control/` 相同：`pages/` 之下的一切都是一個 page，由一行記錄擁有，並被 pages 的 reconcile 清掃。webroot 是兩者唯一共享的東西，因為那是容器實際提供服務的目錄。`ViewIndex` 刻意**不在** `HTTPViews` 中，因此 `IsHTTPView` 不接受它——那個判定回答的是"這是否是 gfehd 上報的、ingress 可以承載的檢視"，而索引頁既非 gfehd 上報，也非它提供服務。
- **`pruneStalePageSymlinks` 合併了 `gfehIndexHostnames`。** 索引頁不是 page，因此若無此舉，第一次 `reconcilePages` 就會刪除每一個索引連結——而一台有物件儲存卻沒有 pages 的機器，每一輪都會撞上這種最激進的情況。有效集合僅從**網路集合**推導，絕不去詢問守護程序，這樣僅僅是啟動較慢的分割槽不會被剪掉自己的索引：可以刪除什麼，必須能由 Town OS 自己擁有的狀態判定。
- **索引頁由 `reconcileGfehIndexes` 渲染，呼叫點在 `RebuildIngress`**，而不是 `ReconcileGfeh`。這個位置是承重的：ingress 重建會在啟動時、每小時的 reconcile 中、包與 page 的增刪改時執行，尤其是在 `publishGfehNames` 中——那是冷啟動時第一次真正有守護程序在應答的時機，因為 gfehd 會輪詢 `/status/ping`，而後者在處理器切換之前一直是 503。從 gfeh 的 reconcile 中寫出的索引頁，會在守護程序還說不出自己提供什麼之前就被寫出，並一直陳舊到下一個小時。

索引頁**只**承載檢視，而它們本來就在 DNS 中。不含暴露、主體、授權或配額：它在沒有任何認證的情況下提供服務，而每一個已釋出的 `/f/<token>` 連結都是一個持有即用的憑據——恰恰是無認證頁面絕不能列舉的東西。

### 協議四：UI 代理（`/gfeh/*`）

管理 socket 未經認證且不可通過網路訪問，因此由 Town OS 代理它。這些路由刻意**與那四條契約路由分開**，以便 `check-townos-sync` 始終精確匹配契約所宣告的內容。

| 路由 | 鑑權 |
|---|---|
| `GET /gfeh` | 鑑權 —— 分割槽及其網路、TLD、配額、單元狀態與 `/v1/names` 輸出 |
| `GET /gfeh/principals?network=` | 鑑權 |
| `POST /gfeh/principals/add` / `remove` | `requireObjectStorage`（管理員或 `gfeh` 授權） |
| `GET /gfeh/grants?network=&principal=` | 鑑權 |
| `POST /gfeh/grants/add` / `revoke` | `requireObjectStorage` |
| `GET /gfeh/exposures?network=` | 鑑權 |
| `POST /gfeh/exposures/withdraw` | `requireObjectStorage` |

四個 `GET` 不計入審計；五個變更操作帶有審計鍵。在未配置任何分割槽時，`GET /gfeh` 會報告"物件儲存未配置"，而不是報錯。

**其中每一條——包括讀操作——都由 `requireNetworkScope` 限制在呼叫者自己的網路內**，因為"哪個網路"存在於只有處理器才解析過的請求體或查詢引數裡。一個受限帳戶列出另一個網路的主體或已釋出連結，恰恰就是作用域機制要防止的洩露；而讀操作是 `requireAuth`，因此上游沒有任何東西會阻止它。`GET /gfeh` 不指定網路（它就是要列舉網路），因此它改為過濾行——依據同一個 `Restricted()` 判定，因為拿一個普通帳戶去和它空的作用域做過濾，會讓每個分割槽對每個普通帳戶都不可見，而不是限制住任何人。

**`gfehClientFor` 內部的順序是承重的：先形狀，再權限，最後存在性。** 空網路對所有人都是 400（打字錯誤不是權限問題）；越界的網路在任何分割槽查詢**之前**就返回 403；只有在這之後，缺失的登錄檔才配得上 503，未知網路才配得上 404。若把查詢放在前面，一個本就無權詢問的呼叫者就能得知那個分割槽是否存在、其守護程序是否在執行，而且得到的是另一種形式的*成功*拒絕——於是沒有任何記錄表明一個受限帳戶曾伸手到自己作用域之外。

### 沒有服務帳戶

早先的版本建立了一個專用的管理員帳戶 `gfeh`，其密碼存放在 `gfeh_service_password` 設定中，以便守護程序能向控制平面認證。**那已經沒有了。** Town OS 在守護程序啟動之前就自行置備每個分割槽的子卷與配額，並通過管理 socket 建立主體，因此那份憑據什麼也沒買到——代價卻是一個*無人建立的、處於啟用狀態的管理員帳戶*，堂而皇之地出現在每台機器的使用者列表裡，權限足以解除安裝一切，並且讓每一個"這台機器有管理員嗎"的問題都被迫變成"有*人類*管理員嗎"。

`hasEnabledAdmin`（`src/svc/systemcontroller/admin_presence.go`）現在就是那個樸素的問題，由 `/status/ping` 中的初始化標誌與 `POST /account/create` 的引導分支共享，因此兩者永遠不會各執一詞——一台機器如果一處說"已初始化"而另一處不這麼說，那就是一台誰也進不去的機器。

`account.PurgeLegacyServiceAccounts` 在升級後的首次啟動時刪除該行與儲存的密碼，並報告它是否真的刪除了什麼，這樣機器只會說一次，而不是每次啟動都記一條日誌。它刻意使用原始 SQL：`Manager` 沒有 `Delete`，而"刪除帳戶"這項能力不該作為一次清理的副作用被引入。

`gfehd.yaml` 中留下的是 `credentials:` 與 `drive.tokens:`——那是**終端使用者向 gfeh 的各檢視認證**用的，絕不是 Town OS 的登入憑據。`town_os:` 塊仍然存在於配置模式中（gfehd 的 YAML 被精確鏡像），但 Town OS 不會向其中渲染任何帳戶。

### 不提供 SMB 檢視

SMB **不提供服務**。它是唯一無法置於 ingress 之後的檢視，也是唯一需要自己那份憑據的：一個 NT 雜湊（`MD4(UTF16LE(password))`），它無法從儲存的密碼雜湊推匯出來，因此每個想要共享的使用者都得額外背一個密碼。Town OS 的帳戶沒有這樣的密碼，因此 gfehd 無人可認證——而在區域網上開一個無認證的共享，不是可以退而求其次的選項。

後果：沒有任何分割槽宣告 `smb:` 塊，也不為它分配宿主機埠（保留 `SMBPortBase` 僅僅是為了讓測試框架的 `GFEH_SMB_PORT_BASE` 保持接線狀態），`Account.SMBNTHash` 與 `src/account/smb_credential.go` 已被移除，`smb_nt_hash` 列由 `migrateLegacyAccountColumns` 丟棄——NT 雜湊不加鹽、沒有工作因子，對任何仍在講 NTLM 的東西而言等價於密碼明文，因此為一個無人提供服務的檢視把它靜置在磁碟上，是兩頭最壞的組合。其餘四個檢視不受影響。

### 配置檔案

`src/gfeh/config.go` **精確**鏡像 gfehd 的 YAML。gfehd 的每一個配置結構體都是 `#[serde(deny_unknown_fields)]`，因此多出來的鍵不會被忽略——它是一個硬性的啟動失敗。頂層欄位：`data_dir`、`partition`、`network`（一個**指標**：缺失表示預設分割槽，而空字串是另一種、非法的請求）、`admin_socket`、五個可選的檢視塊、`credentials` 與 `town_os`。Town OS 渲染五個檢視中的四個，既不渲染 `smb:` 塊，也不渲染 `town_os:` 帳戶。檔案以 `0640` 寫入 `<btrfsBase>/gfeh-control/<network>/` 之下，並對 gfeh 的 gid 組可讀，因為守護程序以 uid 2000 執行且必須讀取它。

### 啟動與 reconcile

`ReconcileGfeh` 在啟動時於 **ingress 與 pages 之後**、**`Reconcile` 之前**執行——那時 TLS CA 與儲存已經就位，而這些名稱必須能供後面的 `RebuildDNS`/`RebuildIngress` 呼叫使用。它在 **`ReconcileNetworks` 之後再執行一次**，該操作是冪等的（未發生變化的分割槽會被放過，而不是被重啟），並覆蓋本次 reconcile 新建出的任何網路。它也會被 `/networks/create`、`/networks/remove`、`/networks/enable` 與 `/networks/disable` 呼叫，因此執行時新增的網路也能獲得分割槽。全過程非致命。

對每個網路，它確保子卷存在（帶 UID/GID）、渲染配置，並**僅在渲染內容發生變化時**才安裝並重啟單元（即 reconcile 已在使用的 `ReadUnit` 差異慣用法）。`pruneGfehPartitions` 移除已不存在網路所對應的單元。

**按分割槽的等待已經取消，而它的缺席是承重的。** `reconcileGfehPartition` 啟動單元後就到此為止；某個守護程序是否在應答，由 `GfehReadyNetworks` 和名稱收集器分別去問，而這兩者本來就把沉默的分割槽當作"什麼也沒貢獻"，而不是當作失敗。那個等待過去位於迴圈內部，每個分割槽一次——包括它其實什麼也沒做的那些分割槽，因為除 home 之外的任何網路，`ensureFirstUserPrincipal` 在第一行就返回了。在一個帶截止時間的 context 上，這不只是慢：第一個永遠不應答的守護程序會在 `WaitForReady` 中耗盡全部剩餘預算，於是它之後的每個分割槽都在一個已過期的 context 上嘗試 `Start`，而 `pruneGfehPartitions` 根本沒機會執行。一個死掉的守護程序按網路名的排序順序，把物件儲存的其餘部分一起拖垮了。

唯一保留下來的等待是 reconcile 最末尾的 `seatGfehFounder`：它只等待 **home** 分割槽，上限為 `gfehFounderWaitBudget`（10 秒，測試中可按配置覆蓋），隨後為機器安置第一個帳戶。因為它在最後，超時只會拖延已經完成的工作；仍在冷啟動的守護程序會在下一輪被安置，而啟動流程緊接著 `ReconcileNetworks` 就會跑下一輪。出於同樣的理由，`GfehReadyNetworks` 通過 `context.WithoutCancel` 為每次健康探測給出各自的預算，而不是去消耗呼叫方剩下的那點時間——否則一個已耗盡的截止時間會讓所有分割槽同時顯得已經死亡。取消仍然被遵守；那屬於關機。

**物件儲存沒有開關設定。** 存檔案正是這台機器存在的目的，所以它像 DNS 和 ingress 一樣執行——作為 Town OS 之所以是 Town OS 的一部分，而不是一個需要被啟用的功能。一個開關只會帶來"某人正在排查檔案去哪了，卻發現它處在關閉位置"的機會；想讓守護程序停下的管理員，可以像對待其他任何系統服務一樣，在服務面板裡停止它們。升級後的機器設定表中若殘留 `object_storage_enabled` 行，沒有任何東西會讀取它。

餘下的逃生艙口關乎*構建*，而非策略：它以 ingress 為前提（當 `INGRESS_IMAGE` 為空時，四個 HTTP 檢視對任何人都不可達，因此啟動分割槽只會釋出無人提供服務的名稱），而顯式置空的 `GFEH_IMAGE` 會完全跳過物件儲存（開發模式）——與 `UI_IMAGE` 和 `INGRESS_IMAGE` 使用同樣的 `LookupEnv` 約定，因為 `Getenv` 會讓空值意味著"使用預設值"，從而根本沒有關閉開關。

**第一個帳戶被安置在 home 分割槽中。** `ensureFirstUserPrincipal` 以本機最早建立的帳戶命名建立一個主體（按 `CreatedAt`，以使用者名稱作為平局裁決，這樣創始帳戶不會因 map 迭代順序而在兩次 reconcile 之間發生變化），並使用 `gfeh.CeilingForAccount(admin)`。森林為空的分割槽誰也服務不了：操作者開啟 Users 標籤頁，什麼也看不到，還得自己琢磨出"我自己的帳戶不在裡面"。**僅限 home**——每台機器都有這個分割槽，而後來新增的網路屬於被授予其上權限的人，把創始帳戶安置進去等於把別人建立的名稱空間交給他。冪等性由 gfehd 保證，它對已存在的主體返回 409。

**名稱在處理器切換之後才釋出。** `publishGfehNames` 在後台執行：gfehd 輪詢 `/status/ping`，而後者**在完整路由器就位之前一直返回 503**（[Boot Status](#啟動狀態與重新整理)），因此分割槽在啟動基本完成之前無法完成自己的啟動。在此處同步等待會讓它所等待的這次啟動自我死鎖。若屆時沒有任何分割槽就緒，這些名稱就交由下一次 reconcile 釋出。

分割槽會在 `collectSystemServices()` 中註冊，因此 `POST /system-services/refresh` 會重新拉取並重啟它們——正是這一處遺漏曾讓 ingress 悄悄停留在舊版本上。

### 版本耦合

**Town OS 構建的是當前的 gfehd，並且刻意不提供版本旋鈕。** `Containerfile.gfeh` 執行的是一條光禿禿的 `cargo install gfehd`——沒有 `--version`，也沒有 `--locked`——因此鏡像攜帶的是構建時 crates.io 上的版本，並針對當前的依賴解析。

曾經有過一個旋鈕（`GFEH_VERSION`，外加一個 `GFEH_LATEST` 的逃生艙），而它造成的是實際的傷害。Makefile 在每次構建時都以 `--build-arg` 傳入版本，而 `--build-arg` **會壓過 `ARG` 的預設值**，因此真正發布出去的是 Makefile 裡的數字，而 Containerfile 裡的那個只是擺設。兩者隨後漂移開了：Containerfile 寫著 `0.1.2` 並長篇解釋了為什麼更舊的版本無法在 Town OS 下執行，而 Makefile 卻在悄悄地構建 `0.1.1`。採用當前發布版，等於在構造上就滿足了下限，也消除了答案的第二個棲身之處。

這兩種失敗都逃過了測試套件，但原因各不相同。**單元**測試套件用一個假的 gfehd 頂替，根本不會執行真正的守護程序。**整合與 UI** 測試套件確實會執行真的——`make/test.sh` 會把構建好的映象載入這兩個容器，因為沒有任何替身能夠證明一個分割區確實啟動、確實應答其管理通訊端、確實執行自身的配額上限——但一個僅僅是*陳舊*的守護程序照樣能構建、推送、安裝並啟動。它只會表現為真實機器上物件儲存永遠起不來。為存檔起見，這也正是當初存在下限的原因：0.1.1 無法解析任何帶有 SMB 使用者的分割區設定（gfehd 的每個設定結構體都是 `#[serde(deny_unknown_fields)]`，而它的 `SmbConfig` 沒有 `users` 欄位），並且它一啟動就去認證，而那時在啟動過程中回應的是 `:5309` 樁，除 ping 之外一律 403。

**兩種構建都以各自的方式擊破層快取。** `cargo install gfehd` 在每次構建時都是逐位元組相同的一行 `RUN`，因此它那一層是永久的快取命中——否則，一個全部契約就是「採用今天 crates.io 上的版本」的構建，會永遠提供第一次構建時的那個 crate，悄無聲息，而且每次的日誌都乾乾淨淨。也不存在更廉價的快取鍵：想知道 crate 何時變化就得去問 crates.io，而那正是這次構建要做的事。

- `release-gfeh` 傳入 **`--no-cache`**。對於要發布出去的東西，任何更弱的做法都不可接受。
- `gfeh-local` 傳入按天粒度的 **`GFEH_CACHE_DATE`** 構建引數。該夾具是每一次整合與 dev 執行的前置條件，因此在這裡用 `--no-cache` 會讓每一次執行都重新編譯整個 Rust 相依樹；但純粹的快取命中又會把它凍結在該機器上首次構建時的那個 gfehd——而整合與 UI 測試套件會針對它啟動真實的分割區。按天是折中：當天之內是快取命中，跨日則重新構建。

無論哪種方式，cargo 的 registry 卷都會留存，因此相依樹會被重新編譯，但不會被重新下載。這是一條通用構建規則的具體體現（見 [CLAUDE.md](CLAUDE.md)）：從倉庫原始碼構建的本地映象不會漂移，因為原始碼變更會使其快取失效；而內容在構建時抓取的映象則需要顯式的快取失效機制。

### UI

`/dashboard/objects`（導航項 `nav.objects`，"Object Storage"）。頂部是網路選擇器，其下是 `?tab=` 子標籤頁，每個對應 `ui/src/routes/objects/` 下的一個檔案：**Overview**（按分割槽的狀態、配額，以及已釋出的名稱，並標明每個名稱是經由 ingress 訪問還是直接撥號）、**Users**（主體與 ceiling；新增時會投影一個 Town OS 帳戶）、**Grants**，以及 **Links**（暴露，可撤回）。讀操作是 `requireAuth`，因此該標籤頁不限管理員；變更控制元件需要管理員或 `gfeh` 授權，並且無論哪種都只限於呼叫者自己的網路。

該介面上有兩個細節，其存在是為了防止讀者據一個用不了的數字或令牌採取行動：

- **對 HTTP 檢視，Overview 的 Port 列是空的。** gfehd 為這類檢視上報的埠是 ingress 代理到的*容器側後端埠*，從讀者所在的任何位置都不可達——在 "Ingress (HTTPS)" 旁邊印出 `9000`，只會招致有人去撥 `s3.gfeh.home:9000`，然後斷定這個功能壞了。SMB 保留它的數字，因為那本會是一個真實的宿主機埠。
- **Links 標籤頁渲染的是完整 URL，且由服務端組裝。** `GfehExposureView.URL` 由 `gfehPublishedLinkBase` 構建——`https://<http-view-fqdn>/f/`——它來自為 ingress vhost 與葉子證書 SAN 命名的同一個收集器，因此已釋出的連結在構造上就是 ingress 會路由、證書也覆蓋的名稱。它不在瀏覽器裡組裝，是因為 UI 將不得不知道四件伺服器早已掌握的事實：提供服務的名稱是 *http 檢視的*，而不是分割槽的或本機的；它是在該分割槽自己網路的 TLD 之下限定的，而非全域 TLD；路由是 `/f/<token>`；以及上報的埠絕不能出現。當分割槽不提供任何 HTTP 檢視時該欄位為空——這是誠實的答案，因為那時確實沒有任何東西在服務那個令牌——而被停用的暴露渲染為純文本，而不是一個可點選的 404。

**這個介面是管理物件儲存的唯一場所。** 服務介面上沒有物件儲存專區：一個分割槽**就是**一個系統服務——各自一個 `town-os-system--gfeh-<network>` 單元——因此它本來就是該介面 System Services 表中的一行，`Object Storage (<network>)`，帶有與其他系統服務相同的狀態徽標和相同的啟動/停止/重啟/日誌操作。此前旁邊那塊面板重複了這一行，並且獨立於它輪詢，於是同一個單元在兩個層級上有兩套可能互相矛盾的控制元件；它還會無條件渲染，而表格卻要等自己的輪詢返回後才顯示，這就使得首次繪製時物件儲存孤零零地排在介面頂部，片刻之後系統服務才插進它上方。服務介面上的 `?expand=objects` 會展開 System Services，那一行就在那裡。

## 服務

### 服務單元過濾

systemd 單元查詢在 dbus 層面就被限定為 `town-os-package--*` 模式，只獲取 Town OS 的包單元，而不是系統上的全部單元。系統服務單元（`town-os-system--*`）通過 `IsSystemServiceUnit()` 單獨識別。結果集進一步排除網路控制器（`-network.service`）、uPnP 助手（`-upnp.service`）與埠轉發（`-fwd-`）。網路控制器單元在內部保留以供故障檢測，但不出現在面向使用者的列表中。

### 服務描述富化

包描述以批次方式載入，每個倉庫呼叫一次 `LoadPackages`，而不是逐包讀取 YAML。描述通過由每個包身份構造出的預期單元名與服務單元匹配。

### 服務單元生成

systemd 服務單元依據包的執行時型別以不同方式生成。

**容器包**生成基於 podman 的單元，啟動用 `podman run`，停止用 `podman stop`，其中包含埠對映（`-p`）、環境變數（`-e`）與卷掛載（`-v`）。

**VM 包**生成基於 QEMU 的單元，使用 `qemu-system-x86_64` 並帶上：

- `-m {MB}` —— 以兆位元組為單位的記憶體（由編譯出的位元組值換算）。
- `-smp {cpus}` —— 虛擬 CPU 數量。
- `-nographic` —— 無頭執行（無顯示輸出）。
- `-enable-kvm` —— KVM 硬體加速。
- `-drive file={image},format=raw,if=virtio` —— 以 virtio 塊裝置形式掛載 raw 磁碟鏡像。
- `-netdev user,id=net0`，併為每個埠對映帶上 `hostfwd=tcp::{external}-:{internal}` —— QEMU 使用者態網路加宿主機到客戶機的埠轉發。
- `-device virtio-net-pci,netdev=net0` —— 半虛擬化網路裝置。

VM 單元還會在啟動前與停止後的鉤子中通過 `firewall-cmd` 管理防火牆埠，並與 socket 單元協調以避免埠衝突。

### 服務單元 API

- `GET /systemd/units`（localhost 或需要鑑權）—— 平鋪列出所有包服務單元。返回的單元狀態附帶包識別符號、包描述與網路控制器故障標誌。
- `GET /systemd/units-tree`（localhost 或需要鑑權）—— 同樣的資料，但組織成依賴樹：根包在頂層，依賴遞迴巢狀在其父包之下（形狀與 `/storage/package-volumes` 一致）。每個節點除了面向人的 `package_identifier` 之外，還帶有 `repo`/`name`/`version`（原始有效名，可能含 `--dep--`），以及與平鋪端點相同的狀態欄位，因此客戶端無需二次請求即可富化行資料。**搜尋與分頁只作用於根節點**——依賴後代不計入分頁，因此即便在分頁邊界上，一棵樹也總是帶著完整子樹返回。
- `POST /systemd/status`（需要管理員）—— 改變某個服務單元的狀態。接受單元名與動作（start、stop、restart、enable、disable）。
- `POST /systemd/status/tree`（需要管理員）—— 對某個根包的整棵依賴樹施加一個動作。接受 `repo`、`name`（原始有效名，因此來自安裝 API 的值可以原樣回傳）、`version` 與 `action`。只允許 `start`、`stop` 與 `restart`——`enable`/`disable` 會被拒絕——並且拒絕停止 system controller 自身的單元。**遍歷順序取決於動作**：單元以葉子優先的順序收集（這是啟動與重啟的自然順序），而對 stop 則把順序反轉，使根節點先於其後代停下。

### 服務管理 UI

服務介面展示一個已安裝包 systemd 單元的分頁資料表。每行顯示包識別符號、描述、活動狀態、子狀態與一個操作下拉選單。

#### 服務操作

每個服務的操作下拉選單提供：

- **Start** —— 啟動服務（帶確認）。
- **Stop** —— 停止服務（帶確認；對 system controller 自身停用）。
- **Restart** —— 重啟服務（帶確認）。
- **Service Logs** —— 開啟該服務單元的日誌檢視器。
- **Network Logs** —— 開啟該服務的網路控制器單元的日誌檢視器（單元名加 `-network.service` 字尾）。

### 高階日誌

服務表下方的 "Advanced Logs" 按鈕會開啟一個模態框，其中包含：

- **Controller Logs** —— 檢視 `town-os-systemcontroller.service` 的日誌。
- **System Logs** —— 檢視系統級日誌（所有單元）。
- **Journal Errors** —— 檢視按優先順序 3 過濾的系統日誌（錯誤及以上，等價於 `journalctl -p 3`）。
- **自定義服務名** —— 一個文本輸入框，可檢視任意 systemd 單元的日誌。

### 日誌檢視器

日誌檢視器對話方塊提供：

- 動態標題，依上下文顯示單元名、"System Logs" 或 "Journal Errors"。
- 狀態徽標，顯示該單元的活動狀態與子狀態（在檢視具體單元時）。
- 即時搜尋，帶防抖過濾（300 毫秒）。
- 按日期與小時的時間範圍過濾。
- 跟隨模式開關，可持續追蹤日誌並自動滾動（當搜尋或時間過濾生效時自動停用）。
- 初始滾動到底部：檢視器開啟後，一旦條目載入完畢，日誌容器就滾動到末尾。滾動到底部的 effect 以 `journalEntries.length > 0` 為條件，因此它不會在條目到達之前的空首次渲染中被消耗掉；隨後一個 `requestAnimationFrame` 會在佈局穩定後重新釘住 scrollTop，以防展開的樹在提交與繪製之間變高。
- 樹狀檢視開關，按分鐘分組條目。樹狀檢視是預設檢視，且每個分鐘分組**預設展開**。展開狀態的 map 只儲存顯式的摺疊：未定義的條目被視為展開，因此首次切換是摺疊而非展開。
- 一鍵複製全部已顯示的日誌條目。
- 日誌輸出中的 ANSI 顏色碼渲染。
- 結構化欄位高亮（`name=value` 鍵值對）。

### 日誌 API

有兩類端點提供日誌資料：

- `GET /systemd/logs`（localhost 或需要管理員）—— 通過 Server-Sent Events 流式推送歷史日誌條目。`unit` 查詢引數選擇服務；為空或為 `__system__` 時返回系統級日誌。
- `GET /systemd/logs/tail`（localhost 或需要管理員）—— 返回一頁 JSON 格式的日誌條目。支援引數：`unit`、`lines`（預設 100）、`before`/`after`（游標分頁）、`grep`（不區分大小寫的搜尋）、`since`/`until`（Unix 時間戳）與 `priority`（syslog 嚴重級別過濾，0 表示不過濾）。
- `GET /systemd/logs/tree` 與 `GET /systemd/logs/tree/tail`（localhost 或需要管理員）—— 按樹限定的對應端點。它們不接受 `unit`，而是接受 `repo`、`name` 與 `version`（全部必填），並覆蓋該包依賴樹中的**每一個** systemd 單元，因此父包的日誌與其依賴的日誌會在同一個檢視中交織。除此之外，重放與分頁語義與 `/systemd/logs` 和 `/systemd/logs/tail` 一致。

## 帳戶管理

### 帳戶模型

每個帳戶包含：使用者名稱（主鍵）、密碼雜湊（絕不在 JSON 中暴露）、郵箱、電話、真實姓名、管理員標誌、停用標誌、一個**授權集合**、一個網路作用域，以及建立/更新時間戳。帳戶儲存在一張 SQLite 表中。

**不存在帳戶"種類"這一概念**。一個帳戶要麼是管理員（在每個網路上持有全部授權），要麼不是；而非管理員持有的就是那些被開啟的授權。`Account.Restricted()`——即持有至少一項授權的非管理員——是推匯出來的，從不儲存。

**不存在服務帳戶。** 早先的版本給物件儲存守護程序配了自己的管理員帳戶；它已經沒有了，`account.PurgeLegacyServiceAccounts` 會在升級後的首次啟動時刪除它（及其儲存的密碼）。參見 [No service accounts](#沒有服務帳戶)。

### 校驗規則

- **密碼** —— 最少 8 個字元，且只能是可列印 ASCII（`0x21`–`0x7E`，不含空格）。高位位元組與控制位元組在建立時即被拒絕（`ErrPasswordInvalidChars`），而不是去指望通往 bcrypt 路徑上的每一層——HTTP Basic 認證、JSON、URL 編碼、資料庫的 latin1 列——都能一模一樣地往返編碼。
- **郵箱** —— 標準郵箱格式（`user@domain.tld`）。
- **電話** —— 數字加可選的格式化字元（`+`、空格、短橫線、圓括號）。
- **聯絡資訊** —— 郵箱、電話與真實姓名全部必填（非空）。
- **授權** —— 每個名稱都必須在 `account.AllGrants` 中（`ErrInvalidGrant`）；管理員不得顯式持有任何授權（`ErrGrantsAdmin`——它本來就全部持有，因此儲存的子集只可能與之矛盾）；持有任何授權的帳戶必須至少限定到一個網路（`ErrGrantsNoNetworks`）。
- **網路作用域** —— 每一項都必須是合法的網路名（`ErrInvalidNetworkName`）。空列表絕不會被讀作"任意網路"。

### 授權（Grants）

**授權**是非管理員帳戶可以持有的具名能力。目前有兩個：

| 授權 | 常量 | 可換來 |
|---|---|---|
| `wireguard` | `account.GrantWireGuard` | 在該帳戶的網路上登記與重新整理 WireGuard peer |
| `gfeh` | `account.GrantGfeh` | 管理這些網路所擁有的物件儲存——主體、它們的授權、已釋出的連結 |

`account.AllGrants` 就是登錄檔：不在其中的授權無法被儲存，這正是阻止 API 請求中的一個拼寫錯誤變成一項永遠悄悄匹配不到任何東西的權限的機制。新增一項能力就是在那裡加一條，再加上它在 `grantRoutes` 中的路由——不需要新列、不需要新遷移、不需要新的 `UpdateFields` 指標。UI 從鏡像檔案 `ui/src/lib/grants.js` 渲染核取方塊，因此新增授權也不需要新的標記程式碼。

兩者是**獨立的**。持有 `wireguard` 在物件儲存中什麼也換不到，持有 `gfeh` 也換不到 peer 登記能力；一個帳戶可以兩者兼有。`Account.HasGrant` 回答"這個呼叫者到底能不能做這件事"，而 `Account.MayAdministerNetwork` 回答"在哪個網路上"——二者絕不互相替代。

#### 強制分三層，而分層組合正是要點

1. **`grantAllowlist`** 是一個*全域的*、失敗即拒的中介軟體。明天新增的路由，在有人把它列入 `grantRoutes`（`src/svc/systemcontroller/controller_auth.go`，以 `"METHOD PATH"` 為鍵）之前，對受限帳戶預設是拒絕的。沒有有效令牌的請求、來自管理員的請求，以及來自不持有任何授權的普通帳戶的請求，都會直接穿過它交給路由自身的鑑權——授權是給那些為行使它而存在的帳戶*疊加的*權限，因此這一層只約束這類帳戶。
2. **路由自身的中介軟體** —— `requirePeerEnroll`（`wireguard` 授權）與 `requireObjectStorage`（`gfeh` 授權），兩者都由 `requireGrant` 構建，後者放行管理員，因為管理員持有全部授權。讀操作仍是 `requireAuth`。
3. **`requireNetworkScope`**，位於處理器內部，因為網路存在於請求體或查詢引數中，只有處理器才解析過它。它做的是**限制**，而不是授予；並且它只限制 `Restricted()` 帳戶——普通帳戶不持有任何授權，因此也沒有作用域，而空作用域會拒絕一切網路，所以把它施加於普通帳戶會讓那些刻意保持 `requireAuth` 的路由上的每一次讀操作都變成 403。

`grantRoutes` 就是授權所能換來的全部：

```
wireguard: GET  /networks/peers   POST /networks/peers/add   POST /networks/peers/refresh
gfeh:      GET  /gfeh             GET  /gfeh/principals      POST /gfeh/principals/add
           POST /gfeh/principals/remove                      GET  /gfeh/grants
           POST /gfeh/grants/add  POST /gfeh/grants/revoke   GET  /gfeh/exposures
           POST /gfeh/exposures/withdraw
```

外加 `grantCommonRoutes`，任何持有授權的帳戶無論持有哪一項都可訪問：`POST /account/authenticate`、`GET /account/me`、`GET /networks`、`GET /dns/services`、`GET /tls/ca.crt` 與 `GET /status/ping`。沒有它們，任何授權都無法使用——你不先登入就無法行使任何授權——因此它們是共用的，而不是被複制進每一項授權。

`GET /status/ping` 出現在那個列表上還有第二個理由：它是**公開的**，註冊時完全沒有鑑權中介軟體，因此匿名的陌生人也能拿到 200。由於白名單是全域且失敗即拒的，遺漏它就意味著一個有效令牌會把那個 200 變成 403——認證反而讓呼叫者嚴格地比什麼都不出示更糟。它同時還是儀表盤 60 秒一次的會話心跳，以及整個狀態面板的資料來源，因此持有 `gfeh` 的帳戶本可以訪問每一條 `/gfeh` 路由，卻仍然得不到一個可用的頁面。同時再授予 `wireguard` 也無濟於事：ping 不與任何一項授權掛鉤。

請注意刻意**缺席**的內容：`/gfeh/partitions/*` 保持 `requireAdmin`（置備一個分割槽就是建立一棵權限樹的根並分配一個 btrfs 子卷；`TOWNOS_CONTRACT.md` 把它保留給管理員，而 gfeh 的客戶端會依據 403 分支），以及 `GET /networks/peers/connected`，它聚合了所有網路上每個帳戶的 peer 與觀測到的源地址。

與建立後不可變的 `Admin` 不同，授權是可變的；而 `account.Manager.CreateGranted` 是獨立於 `Create` 的方法，這樣那些不變量（持有授權者絕不是管理員，且始終有非空作用域）就在建立時於一處被強制，而不是從一個被加寬的位置引數簽名裡拼湊出來。

#### 從舊列遷移

早先的版本為每項能力保留一個布林列。`legacyGrantColumns`（`src/account/sqlite.go`）把每一列對映到它將成為的授權，`migrateLegacyAccountColumns` 負責搬運並刪除該列：

| 舊列 | 變為 |
|---|---|
| `wireguard` | `wireguard` |
| `object_storage` | `gfeh` |
| `network_only`（一個把兩者合成一個標誌的中間態模式） | 兩者 |

**一列，一項授權。** 原本能登記 peer 的帳戶仍然能，原本不能的也不會悄悄獲得這項能力——在升級過程中擴大權限是不可撤回的方向，因為帳戶保留著它的密碼，而介面上沒有任何東西會說它的權限變大了。`smb_nt_hash` 被直接丟棄（參見 [No SMB view](#不提供-smb-檢視)）。

### 每個帳戶都屬於 home 網路

`Manager.Create`——**第一個**帳戶與每個普通帳戶所走的路徑——寫入 `networks: ["home"]`。`CreateGranted` 不會把它並進去：在那條路徑上，管理員選定的作用域恰恰就是該帳戶可以觸及的網路，把 `home` 摺進去會擴大一個本應限定在 `office` 的門戶帳戶的範圍。

這樣做是安全的，因為對於不持有授權的帳戶，作用域是**成員身份，而非限制**：`Restricted()` 為假，因此上面各層都不會去查詢它。而且它絕不可能指向一個不存在的網路——參見 [The home network always exists](#home-網路始終存在)。

### 帳戶 API

- `POST /account/create` —— 建立新帳戶。在引導模式下（不存在處於啟用狀態的管理員帳戶）允許未認證訪問；否則需要管理員認證。非空的 `grants` 陣列會轉由 `CreateGranted` 處理並使用所提供的 `networks`；否則帳戶通過 `Create` 建立並加入 home 網路。使用者名稱重複的錯誤會返回通用失敗資訊，以防使用者列舉。
- `POST /account` —— 按使用者名稱獲取帳戶（需要鑑權）。
- `GET /account` —— 列出所有帳戶，支援分頁與搜尋（需要鑑權）。
- `POST /account/update` —— 更新帳戶欄位（需要鑑權）。被更新的使用者名稱來自**請求體**，因此編輯他人帳戶僅限管理員：沒有這項檢查，任何已認證帳戶都能 POST `{"username":"admin","fields":{"password":"..."}}` 從而接管這台機器——控制器驅動著宿主機的 podman socket，所以那就是 root。普通帳戶仍可編輯自己的聯絡方式與密碼，這正是該路由沒有直接設為 `requireAdmin` 的原因。管理員身份在帳戶建立後不可更改；授權與網路作用域可以更改，但**只能由管理員更改，即便是改你自己的帳戶也一樣**——否則普通使用者就能給自己授予 `gfeh` 從而闖進某個分割槽，或授予 `wireguard` 從而在 overlay 上登記一個 peer。`networks` 為 nil 時保持已儲存的作用域不變；非 nil 時整體替換。`validateGrantResult` 檢查更新*之後*該行的狀態，因此給管理員授予授權、把持有授權者提升為管理員，以及把作用域從授權之下清空，這三種情況都會被捕獲。
- `POST /account/disable` —— 停用帳戶，阻止其認證（需要管理員）。同時撤銷該帳戶的活動會話。讓停用生效的並不是這一步——`SessionManager.Validate` 本身就會拒絕被停用帳戶的令牌，因此這項保證並不依賴撤銷是否成功——它的作用是：若該帳戶日後被重新啟用，停用之前簽發的令牌不會重新生效，而那並不是管理員在撤銷某人訪問權之後所說的"啟用"的含義。
- `POST /account/enable` —— 重新啟用被停用的帳戶（需要管理員）。

### 帳戶管理 UI

使用者管理介面（`/dashboard/users`）展示一個可分頁、可排序、可搜尋的帳戶資料表。每行顯示使用者名稱、郵箱、電話、真實姓名、管理員/使用者角色徽標與啟用/停用狀態。每行的操作包括一個 Edit 按鈕（開啟對話方塊以更新密碼、郵箱、電話、真實姓名、**授權核取方塊**與網路作用域選擇器）以及一個帶確認的啟用/停用開關。有一個連結可跳轉到專門的建立使用者頁面（`/dashboard/users/create`），其登錄檔單帶有同樣的控制元件。兩個表單都從 `ui/src/lib/grants.js` 渲染核取方塊，並拒絕在未選擇任何網路的情況下授予任何權限。

### 會話管理

會話使用 JWT 令牌（HS256），claim 包含會話 ID（UUID）、使用者名稱與簽發時間戳。簽名金鑰是臨時的：每次服務啟動時通過 `crypto/rand` 生成 32 位元組隨機數，絕不落盤。`InitSessionManager` 在啟動時執行，會清除所有已存在的會話（`DELETE FROM sessions`），因為舊令牌在新金鑰下無效。`TOWN_OS_SIGNING_KEY` 環境變數可覆蓋生成的金鑰。會話在最後一次使用後 7 天過期。一個後台清理任務定期移除過期會話。

**被停用帳戶的令牌一到就是死的。** `Validate` 檢查 `Disabled` 並拒絕，因為登入之後的每一個請求都僅由該函式授權：沒有這項檢查，停用一個帳戶只是阻止它*再次*登入，而它已經持有的令牌在整個會話生命週期內仍然有效，並且會因被使用而自我續期。

`SessionManager` 介面提供：`Create`、`Validate`、`Revoke`、`RevokeAllForUser`、`Cleanup`、`List`、`GetUsername`、`HasActiveAdminSessions` 與 `StartCleanup`。

會話 API 端點：

- `POST /account/authenticate` —— 使用者名稱/密碼登入（公開）。返回 JWT 令牌與帳戶物件。所有認證失敗（密碼錯誤、使用者不存在、帳戶被停用）都返回同一個通用的 "invalid credentials" 錯誤，以防使用者列舉。
- `GET /account/sessions` —— 列出當前已認證使用者的會話（需要鑑權）。
- `GET /account/me` —— 獲取當前已認證使用者的使用者名稱（需要鑑權）。
- `POST /account/session/revoke` —— 按 ID 撤銷特定會話（需要鑑權）。

### 審計日誌

所有管理操作都被記入審計日誌。每條記錄包含：自增 ID、帳戶（使用者名稱）、動作描述、請求路徑、經過清洗的詳情（憑據被掩碼）、成功標誌、錯誤資訊與建立時間戳。

**清洗器做的是掩碼而非刪除**，它把憑據的值替換為 `[REDACTED]` 並保留鍵名。審計的閱讀者應當能夠看出某個欄位存在過但被扣下了，而不是根本無法把它與一個從未攜帶該欄位的請求區分開。它以不區分大小寫的方式，把 `auditRedactedKeys` 與整個鍵名、以及鍵名最後一個下劃線之後的字尾做匹配，因此 `smtp_password` 會被捕獲，而不需要一條同樣會吞掉無害名稱的子串規則；它還會同時遞迴進陣列與 map。包安裝的 `responses` map 被視為**不透明**並整體掩碼：它的鍵屬於包作者，因此沒有可供匹配的詞彙表，而它的值恰恰就是生成的 `type: secret` 與 `type: oauth` 答案——日誌絕不能變成它們的副本。裸的 `key` 刻意**不在**列表上——否則字尾規則會捕獲 `public_key`，而 `POST /networks/peers/add` 正攜帶該欄位，且 WireGuard 公鑰在構造上就是公開的，同時它又是唯一能說明"登記的是哪台裝置"的欄位。

被跟蹤的動作包括：建立/修改/移除檔案系統，新增/移除/移動/重新整理倉庫，安裝/解除安裝包，清除卷，停用/啟用包，設定單元狀態，建立/更新/停用帳戶，認證，撤銷會話，更新設定，忽略升級提示，上傳/下載歸檔，建立/更新/移除/重建 page，上傳/刪除 VM 鏡像。

只讀端點被明確排除在審計日誌之外。被排除的路徑包括根路徑（`/`）、所有 GET 列表/查詢端點、資訊端點（`/packages/installed/info`）、應答獲取（`/packages/last-responses`、`/packages/responses`）、安裝預覽（`/packages/install-preview`）、版本/問題查詢、時區列表、pages 列表端點、狀態 ping、系統服務列表（`/system-services`）、審計日誌查詢、設定讀取，以及日誌流式端點。

- `POST /audit/log`（localhost 或需要管理員）—— 查詢審計日誌，支援基於游標或偏移的分頁、按帳戶過濾、排序與搜尋。

### 設定管理

鍵值形式的設定儲存在 SQLite 中。預設設定包括 `default_quota`（50 GB）、`max_archive_size`（1 GB）、`archive_unpack_timeout`（600 秒）、`locale`（en-US）、`dns_tld`（home）、`dns_resolution_mode`（auto）、`dns_local_forwarders`（false）、`peer_ttl`（7200 秒）與 `gfeh_partition_quota`（0）。`proton_image` 只在帶 `proton` 構建標籤的構建中註冊。完整表格見 [Settings](#設定項)。

- `GET /settings` —— 獲取所有設定（需要管理員）。
- `POST /settings/get` —— 按鍵獲取特定設定（需要管理員）。
- `POST /settings/set` —— 設定某項設定的值（需要管理員，計入審計）。位元組值類設定（`default_quota`、`max_archive_size`）接受人類可讀的字串（例如 "500GB"、"10MB"），它們會被解析並以數值位元組數儲存。

### 設定 UI

系統設定介面為所有系統級設定提供管理員可配置的控制元件。每項設定都展示在一個帶邊框的區塊中，含標題、以人類可讀格式顯示當前值的說明，以及一個帶數字輸入框、單位選擇器與儲存按鈕的表單。

- **Default Volume Quota** —— 可按 GB、MB 或位元組配置。設為零時顯示 "0 (no quota)"。
- **Max Archive Size** —— 可按 GB、MB 或位元組配置。控制歸檔上傳所允許的最大檔案大小。
- **Archive Unpack Timeout** —— 可按秒、分鐘或小時配置。控制解包上傳歸檔所允許的最長時間。
- **Language** —— 一個下拉框，以母語文字顯示常用語言。可展開區域會顯示擴充語言環境。未填充的語言環境帶星號顯示並被停用。
- **Proton Image** —— 一個可編輯文本輸入框，用於填寫 Proton 執行器容器鏡像引用（例如 `quay.io/town/proton:latest`）。
- **Local DNS Forwarders** —— 一個由 `dns_local_forwarders` 支撐的開關。其下方顯示 rolodex *實際*正在轉發到的地址，這些地址讀自 `GET /dns/status` 而非由設定推斷；當發現過程沒有找到任何可用地址時，面板會說明仍在使用公共轉發器，而那正是"開關顯示為開、卻什麼也沒變"的那唯一一種情形。參見 [Local forwarders](#本地轉發器)。

當前值會被分解為最合適的單位來顯示（例如 1073741824 位元組顯示為 "1 GB"，120 秒顯示為 "2 minutes"）。輸入校驗會拒絕負數與非數字值。

## 包升級

### 升級檢測

升級系統把已安裝的包版本與已配置倉庫中的最新可用版本作比較。當存在更新的版本，或檢測到本地修改時，該包會被標記為可升級。

- `GET /packages/upgrades`（需要鑑權）—— 列出可用升級。每一項包含 repo、name、已安裝版本、最新版本與一個 changed 標誌。
- `POST /packages/upgrades/dismiss`（需要管理員）—— 把當前的升級標記為已忽略。計算當前升級集合的 SHA256 雜湊並存入 `dismissed_upgrades_hash` 設定。

狀態 ping 響應中包含 `upgrades_available`（數量）與 `upgrades_dismissed`（布林值，雜湊匹配時為真）。

## 網路

### UPnP 埠對映

`upnp.Manager` 介面提供 `AddPortMapping` 與 `RemovePortMapping`，用於經由 UPnP/IGD 在本地網路閘道器上管理 TCP 埠轉發。其實現通過 SSDP 發現 Internet 閘道器裝置，並使用 WANIPConnection2 的 SOAP 方法。本機 IP 通過連線一個外部地址（8.8.8.8:80 UDP）來探測。

### 網路控制器

網路控制器管理按包劃分的埠轉發與 UPnP 對映。每個有網路需求的包都有一個 JSON 狀態檔案，指明埠的外部/內部對映、UPnP 標誌與轉發標誌。

- **Socat 轉發**（當 `forward=true` 時）—— 執行 `socat TCP-LISTEN:{externalPort},fork,reuseaddr TCP:127.0.0.1:{internalPort}` 來轉發流量。
- **UPnP 對映**（當 `upnp=true` 時）—— 在閘道器上對映埠。當 `forward=true` 時對映外部到外部（由 socat 監聽）；當 `forward=false` 時對映外部到內部（由 podman 網橋處理）。
- **Reconcile** —— 通過 fsnotify 監視狀態檔案，按需停止/啟動轉發器與對映。
- **續期** —— UPnP 對映每 10 分鐘續期一次，TTL 為 1800 秒。
- **關閉** —— context 被取消時移除所有 UPnP 對映並殺掉所有 socat 程序。

### 依賴共享網路

包的依賴共享父包的 podman 網路。這讓同一依賴樹中的容器可以直接通過容器名互相通訊（藉助共享網路上 podman 內建的 DNS），而不必經由宿主機埠轉發。

- **冪等的網路建立** —— 無論是否存在網路控制器（NC），每個服務單元都包含 `ExecStartPre=-/usr/bin/podman network create {network}`。這是一道啟動順序上的安全網：若 NC 尚未建立該網路（例如鏡像未構建、systemd 競態），服務仍然能啟動。NC 也會建立該網路——誰先到誰生效，另一個成為空操作。
- **網路歸屬** —— 父包擁有該 podman 網路（`town-os-net--{repo}-{name}-{version}`）。NC 在 `ExecStartPre` 中建立它，並在 `ExecStopPost` 中移除它（`podman network rm -f`）。
- **依賴加入父網路** —— 依賴的服務單元使用 `--net {parent-network}` 而不是建立自己的網路。它們在 `ExecStartPre` 中冪等地建立該網路（以防它們先於父包啟動），但從不移除它。
- **沒有埠的獨立包**沿用原有模式：在 `ExecStartPre` 中先 `podman network rm -f` 再 `podman network create`，並在 `ExecStopPost` 中 `podman network rm -f`。只有既沒有 NC 也沒有父 NC 的獨立包才會在 `create` 之前執行 `rm -f`。
- **帶依賴的父包**在 `ExecStartPre` 中**不**先 `rm -f` 再 `create`，因為依賴可能已經在該網路上執行（藉助 `Before=` 排序，它們先啟動）。

### 依賴的 systemd 排序

依賴的 systemd 單元帶有排序指令，確保相對於父包的啟停順序正確：

- **依賴單元**：`PartOf={parent-service}`（停止父包會級聯到依賴）與 `Before={parent-service}`（依賴先於父包啟動、後於父包停止）。
- **父單元**：`Wants={dep1} {dep2} ...` 與 `After={dep1} {dep2} ...`（父包需要依賴，並在啟動前等待它們）。
- **網路控制器**：既有的針對 NC 的 `Wants=` 會與依賴的 `Wants=` 目標合併。

這些通過 `PackageUnitConfig` 的欄位配置：`ParentNetwork`、`ParentUnitName`（用於依賴）與 `DependencyUnitNames`（用於父包）。reconcile 會從依賴記錄與 `ParentName()` 計算出它們。

### 依賴環境變數

父包會獲得用於在共享網路上訪問其依賴的環境變數：

- `TOWNOS_DEP_{KEY}_HOST` —— 該依賴的 podman 容器名（可通過共享網路上的 podman DNS 解析）。
- `TOWNOS_DEP_{KEY}_PORT_{containerPort}` —— 容器側埠號（由於父包與依賴在同一網路上，無需宿主機埠對映）。
- `TOWNOS_DEP_{KEY}_PORT_{NAME}` —— 當依賴在 `network.external` / `network.internal` 中聲明瞭語義埠名時（見下文 **具名埠**），會在數字形式之外額外發出這一形式。名稱會被轉為大寫，因此依賴中的 `sql` 在父包上變成 `TOWNOS_DEP_DB_PORT_SQL`。數字形式與具名形式並存，且始終攜帶相同的值。

### 依賴模板變數

除上述執行時環境變數之外，依賴的主機與埠值在包編譯期也可作為 `@variable@` 模板標記使用。這讓父包能在編譯期於其 `environment` 欄位值中引用依賴，也讓**同級依賴**能在 `dependencies.<key>.responses` 塊中互相引用。

- `@dep_KEY_host@` —— 解析為該依賴的 podman 容器名（可通過共享網路上的 podman DNS 解析）。
- `@dep_KEY_port_N@` —— 解析為該依賴的數字容器埠 N。
- `@dep_KEY_port_NAME@` —— 解析為該依賴以語義名 `NAME` 標記的容器埠（見下文 **具名埠**）。模板中為小寫；與環境變數字尾不區分大小寫地匹配。對同一個埠，它與 `@dep_KEY_port_N@` 並存。

模板鍵由 `TOWNOS_DEP_*` 執行時環境變數名推導而來：去掉 `TOWNOS_` 字首並把其餘部分轉為小寫。例如 `TOWNOS_DEP_DB_HOST` 變成模板鍵 `dep_db_host`，`TOWNOS_DEP_DB_PORT_5432` 變成 `dep_db_port_5432`。

`@dep_*@` 形式只在本來就會執行 `@variable@` 替換的地方生效——`environment` 值與依賴的 `responses`。在檔案模板的 `content` 內部，請改用 Go 模板的 `.Dep` 名稱空間（見上文 **檔案模板**）：`{{.Dep.KEY.Host}}` 與 `{{index .Dep.KEY.Ports "sql"}}` 攜帶相同的值。`.Dep` 由同一套 `TOWNOS_DEP_*` 計算填充，並把每個埠同時以其數字鍵（`"5432"`）和（若宣告過）其小寫語義名（`"sql"`）暴露出來。

在**父包**一側，這些變數在依賴安裝完成、其容器名與埠已知之後才被解析。它們在單元生成期間被應用到父包的環境變數值上。reconcile 也會重建依賴環境變數，使 systemd 單元在重啟與版本變更之後仍然正確。

在**依賴**一側（即 `dependencies.<key>.responses` 下宣告的、引用另一個同級鍵的應答），解析發生在 `installDependencies` 期間，通過一次拓撲排序完成：

- `src/svc/systemcontroller/controller_install_dependencies.go` 中的 `orderDependencies` 解析每個同級依賴的 `Responses` 中的 `@dep_KEY_host@` / `@dep_KEY_port_N@` 標記並構建 DAG。沒有引用的同級依賴先執行；有引用的同級依賴在它所指名的同級之後執行。在同樣就緒的依賴之間，以字母序打破平局以保證確定性（Go 的 map 迭代是隨機的，因此排序對可復現性是必需的）。
- 同級依賴之間的環是硬錯誤，會在任何依賴被置備之前中止安裝。
- 對該順序中的每個依賴，都會在 `depIP.CompileWithContext` 執行**之前**對其 `Responses` 呼叫 `applyDepTemplates`，把 `@dep_OTHER_*@` 標記替換為已安裝同級所累積的容器名/埠值。若沒有這次預編譯替換，依賴 YAML 中帶型別的問題（例如 `type: port`，或任何其 `Output` 會執行 `strconv.ParseUint` 的型別）就會以 `ErrInvalidResponseType` 拒絕那個字面佔位符，導致安裝中途中止，並在磁碟上留下一個裝了一半的父包。
- 自引用（依賴 X 引用 `@dep_X_host@`）會被忽略，而不是當作環。對未被宣告為同級鍵的名稱的引用，會被視為外部模板變數並在排序時忽略。
- 安裝處理器通過 SSE 流式推送錯誤，並從 HTTP 處理器返回 `nil`，因此無論安裝是否真的完成，審計日誌都始終記錄 `success=true`。這意味著部分安裝失敗（裝了一半的依賴樹、`installed/<repo>/<parent>/<version>/` 之下的孤立 btrfs 卷）只在 SSE 流與 systemd 單元列表中可見——在 `/audit/log` 中看不到。

示例：一個帶依賴鍵 `db`（一個暴露 5432 埠的 Postgres 容器）的包，可以在其 environment 段中使用 `@dep_db_host@` 與 `@dep_db_port_5432@`，而不必硬編碼 `127.0.0.1`：

```yaml
environment:
  DB_HOST: "@dep_db_host@"
  DB_PORT: "@dep_db_port_5432@"
```

同級互相引用的示例（jitsi 的形態）：`jitsi` 依賴 `prosody`、`jicofo` 與 `jvb`。`jicofo` 與 `jvb` 各自都需要 prosody 的容器名與內部 XMPP 埠，因此父包 YAML 通過每個引用方依賴的 `responses` 塊把它們串起來。`orderDependencies` 先安裝 `prosody`，然後是 `jicofo` 與 `jvb`（這兩者之間按字母序），每個都已把佔位符替換為 prosody 的具體容器名與埠 5222：

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

### 依賴共享卷

同一依賴樹中的包可以通過雙方共同選擇加入的方式共享 btrfs 子卷。依賴的作者把某個卷標記為 `shareable: true`；父包的作者隨後宣告一個 `expose:` 塊（把該依賴的卷掛載進父包的容器），或者在另一個依賴上宣告 `consume:` 塊（把一個同級的卷掛載進另一個同級的容器）。沒有 `shareable: true` 的卷不能被跨包掛載——安裝/reconcile 環節會拒絕對非共享卷的任何引用。

這套接線是既有 `HostVolumeMount` 基礎設施之上的一層薄封裝：安裝路徑把每個 `expose`/`consume` 條目解析為一個指向生產方依賴在磁碟上的 btrfs 子卷的 podman `-v <hostpath>:<containerpath>:<options>` 標誌。reconcile 在每次啟動時從父包持久化的 YAML 重建同樣的標誌，而 `installUnitIfChanged` 的內容差異比對會自動捕捉變化——不需要特殊的重啟鉤子。

**依賴側選擇加入。** 依賴按卷宣告 `shareable: true`：

```yaml
# radarr/1.0.yaml
volumes:
  movies:
    mountpoint: /movies
    quota: "@moviesize@"
    shareable: true     # 選擇加入：父包或同級可以掛載它
  config:
    mountpoint: /config  # 非共享；若有父包嘗試 expose 它則被拒絕
```

**父包 → 依賴（`expose:`）。** 父包的 `dependencies.<key>.expose:` map 指明要繫結掛載進父包容器的依賴卷。每一項接受一個容器路徑與可選的 `readonly` 標誌（預設 `true`，因為父包通常只是消費依賴的產出）：

```yaml
# plex/1.0.yaml
dependencies:
  radarr:
    package: radarr
    expose:
      movies:                  # radarr YAML 中的卷名
        path: /data/movies     # Plex 容器內的路徑
        readonly: true
  sonarr:
    package: sonarr
    expose:
      tv:
        path: /data/tv
        readonly: true
```

**同級 → 同級（`consume:`）。** `dependencies.<key>.consume:` 列表把一個同級依賴的卷掛載進**本**依賴的容器。每一項接受 `from:`（同一父包 `dependencies:` map 中的同級依賴鍵）、`volume:`（同級 YAML 中的卷名）、`path:`（消費方依賴上的容器路徑），以及可選的 `readonly`（預設 `false`，因為同級之間的共享通常需要可寫——例如某個 *arr 要往下載客戶端的 `/downloads` 裡匯入）：

```yaml
# media/1.0.yaml —— 把下載客戶端與各 arr 接線起來的父包
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

**拓撲安裝順序。** `consume.from` 引用會與既有的 `@dep_KEY_*@` 應答引用一起，為 `orderDependencies` 構建的安裝期 DAG 新增邊。消費同級 A 的依賴 B 會嚴格在 A 之後安裝，這樣 B 的容器啟動時 A 的 btrfs 子卷已經存在。consume 邊之間的環（A 消費 B，B 消費 A）是硬錯誤，會在任何依賴被置備之前中止安裝。自消費（`from:` 等於該依賴自身的鍵）在校驗期即被拒絕。

**校驗。** 編譯期校驗會拒絕：相對路徑或含穿越的掛載路徑、指向同一 `dependencies:` map 中未宣告鍵的 `consume.from`、自消費，以及同一依賴內重複的 consume 路徑。跨包校驗（生產方對應捲上是否有 `shareable: true`）發生在安裝/reconcile 時載入生產方 YAML 的那一刻——expose 或 consume 了非共享卷的父包會以 `volume %q is not marked shareable on %s` 安裝失敗。

**模板路徑替換。** `expose.<volname>.path` 與 `consume[].path` 與普通卷掛載點一樣參與 `@question@` 替換。`consume.from` 與 `consume.volume`（以及 `expose` 的 map 鍵）是識別符號而非資料，不會被替換。

**權限注意事項——繫結掛載會透傳 UID/GID。** 依賴在宿主機上的 btrfs 子卷，屬主是依賴容器建立它時所用的那個 uid:gid。若依賴以 1000:1000 執行（linuxserver/* 的預設值），而消費方的父包或同級以不同的 uid 執行，消費方在讀或寫時會得到 EACCES。修復之道在包的 YAML 中，而不在平台裡：讓共享卷的各包之間的 `PUID`/`PGID` 問題預設值保持一致。`HostVolumeMount.UID`/`GID` 的 chown 行刻意不遞迴，並且只在依賴作者在可寫掛載上顯式設定了它們時才生效；共享卷解析器從不自動 chown。

**模板名稱空間。** 依賴的可共享卷也會在檔案模板的 `.Dep` 名稱空間中以 `.Dep.<key>.Volumes.<volname>` 暴露（其值是該卷在依賴容器內的掛載點）。這與 `.Dep.<key>.Ports` 是平行的。非共享卷被刻意排除在該 map 之外，因此檔案模板無法觸及依賴作者未選擇暴露的資料。

**解除安裝順序。** 既有的 `Before=`/`PartOf=` 指令已經保證父包先於依賴停止、依賴先於其生產方停止，因此當父包被解除安裝（級聯解除安裝其依賴）時，消費方的容器在生產方的卷被觸碰之前就已經消失。不需要新的解除安裝邏輯。

**範圍之外。** 一個依賴恰好屬於一個父包（既有不變量）；共享卷並不會讓依賴變成多租戶的。反方向共享（父包的卷 → 依賴）在 v1 中不支援；若將來確有需要，模式仍是可擴充的。系統服務（`town-os-system--*`）不具備此功能——`GenerateSystemServiceUnit` 不查詢 `expose`/`consume`。

### 具名埠

依賴埠的引用可以使用語義名，而不是容器埠號。依賴在 `network.external` / `network.internal` 中把名稱宣告為 YAML 鍵；父包則通過 `@dep_KEY_port_NAME@` 引用同一個埠。這讓原始埠號只存在於唯一一處（擁有它的那個依賴），並讓父包談論角色（`sql`、`http`、`admin`）而不是協議瑣事。

**規範形態。** 埠號由依賴擁有——理想情況下作為 `type: port` 問題的預設值，這樣自動生成與覆蓋都能正常工作：

```yaml
# 依賴：named-db/1.0.yaml
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
  DB_PORT: "@dep_db_port_sql@"   # 父包中任何地方都沒有 "5432"
dependencies:
  db:
    package: named-db
```

**Map 模式。** `network.external` 或 `network.internal` 中的埠條目，其 YAML 鍵要麼是：

- 一個數字埠字串（舊形式）：`"5432": "5432"` → 宿主機埠 5432 → 容器埠 5432。不記錄任何名稱。
- 一個匹配 `PortNameRegexp`（`^[a-zA-Z][a-zA-Z0-9_]*$`）的語義名：`sql: "5432"` → 容器埠（值）同時兼作宿主機埠，且名稱 `sql` 被存入 `PackageNetwork.{External,Internal}Names[containerPort]`。名稱必須以字母開頭（以避免與數字解析產生歧義），並可包含字母數字與下劃線。

兩種形式可以並存於同一個 map 中；解析器依據鍵來分支。一個名稱對映到兩個不同的容器埠，或兩個名稱對映到同一個容器埠，都是編譯期錯誤。編譯後的 `Package` 型別在既有的 `PortMap` 之外新增兩個可選的 `PortNameMap` 欄位；只關心數字埠的消費方（單元生成、網路狀態序列化）不受任何影響。

**環境變數與模板的發出。** 對編譯後依賴中的每一個埠，安裝器都會發出 `TOWNOS_DEP_<KEY>_PORT_<N>=<N>`（始終發出）。若該埠有名稱，它還會額外發出值相同的 `TOWNOS_DEP_<KEY>_PORT_<UPPER_NAME>=<N>`。模板解析器去掉 `TOWNOS_` 字首並把其餘部分轉為小寫，因此 `@dep_db_port_5432@` 與 `@dep_db_port_sql@` 解析為同一個值。`controller_install_dependencies.go` 中的 `depKeyRefRegex` 接受兩種形式；同級依賴的拓撲排序在構建 DAG 時也能識別具名引用。

**向後相容。** 使用數字形式的既有包繼續原樣工作——不強制遷移。父包可以在同一個檔案中對同一個依賴混用數字與具名引用。reconcile 在啟動期間重建兩種形式，因此仍然存在的既有安裝絕不會退化。

**何時使用名稱。** 只要父包引用了依賴的埠就該用。名稱是父包唯一可以援引的事實；埠號歸依賴所有。優先對內部埠使用名稱（父包與依賴之間的流量正是走共享 podman 網路的），外部具名埠雖然允許但不常見，因為父包通常不會通過宿主機繫結去撥號依賴。

## 網路（WireGuard Overlay）

一個**網路**是一個具名的 WireGuard overlay，與一個 DNS TLD 配對。包安裝進網路；peer 加入網路；而 TLD 決定了誰能解析什麼（參見 [Network TLDs, Dual-Home, and Split-Horizon Resolution](#網路-tld雙棲與分離視界解析)）。

### 網路模型

`account.Network`（`src/account/network.go`）攜帶：`Name`、`TLD`、`Subnet`、`Address`（本機自己的 overlay 地址，永遠是 `.1` 主機位）、`PublicKey`、`PrivateKey`（絕不序列化）、`ListenPort`、`Enabled` 與時間戳。名稱必須是 DNS 標籤安全的（`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`，最長 32 字元），因為它們會被複用為 WireGuard 介面字尾與 systemd 單元名。

`Enabled` 只控制*傳輸層*：為假時不拉起 WireGuard 介面，切斷遠端訪問，而本地 DNS 解析與容器本身繼續執行。

### home 網路始終存在

`DefaultNetworkName` 是 `home`，它由 **`account.InitNetworkManager` 播種**，與建表同時進行——而不是由啟動時的 reconcile 播種。因此從資料庫存在的那一刻起它就在：在控制器啟動之前、在每一個測試伺服器中，以及在這台機器服務的第一個請求時。`account.DefaultNetwork()` 是那一行的規範定義。

這很重要，因為下游的一切都是假定它存在而寫的：第一個帳戶被限定到它（[每個帳戶都屬於 home 網路](#每個帳戶都屬於-home-網路)），預設 TLD 就是它的 TLD，而 gfeh 會給它一個分割槽並把創始帳戶安置在那裡。如果它需要先被建立，就會存在一個視窗期，其間上述一切都不成立——這正是過去物件儲存在首次啟動時一直死著、直到後來某次重啟碰巧發現網路已經存在的原因。

它**不能被移除**（`ErrNetworkProtected`，且 `POST /networks/remove` 會拒絕），也不能被建立第二次——對 `home` 呼叫 `POST /networks/create` 會因 TLD 衝突檢查而得到 409。

它是**僅 DNS 的**：`applyNetworkTransport` 不給它 WireGuard 介面、不給 overlay 子網、也不給 peer，因此它永遠不可能有隧道裝置。所播種的那一行因此**完全不攜帶傳輸欄位**——空子網、無金鑰對、埠 0。這是事實而非佔位符；推匯出的子網與金鑰會是沒有任何東西讀取的欄位。

**在它上面登記 peer 會被拒絕——這是刻意的，而且在兩層上都拒絕。** `POST /networks/peers/add` 對 `home` 返回 400，而 `NetworkManager.AddPeer` 無論被誰呼叫都返回 `ErrNetworkDNSOnly`。這很重要，因為[每個帳戶都屬於 home 網路](#每個帳戶都屬於-home-網路)：如果在那裡的登記被接受，僅憑成員身份就等於拿到了一條上隧道的路，而存下來的 peer 描述的是一條並不存在、也永遠不會存在的隧道。peer 是在真實 overlay 上動態建立的，所以想要隧道的呼叫者要指名一個。

這條拒絕過去是**偶然的**：沒有任何地方檢查網路，處理器一路落到 `netip.ParsePrefix`，對播種行的空 `Subnet` 解析失敗，最後以 **500** 冒出來。那讀起來像是機器壞了而不是一次拒絕，它沒有告訴呼叫者任何原因，而且只要有什麼東西往那一行寫入了子網，它就會不再拒絕。這道守衛按名字進行，位於伺服器端生成金鑰對之前，後面還跟著一道針對「無傳輸行」的檢查，以涵蓋子網因其他原因為空的網路。

**它的 TLD 來自 `dns_tld`，並由控制器保持二者同步。** 播種時無法知道該值（account 包沒有設定 manager），因此該行以裸預設值出現，再由 `ensureDefaultNetwork` 在啟動時對齊，且僅在兩者不一致時才寫入。`POST /dns/tld` 在寫入設定的同時也會重新指向它。兩者都經由 `NetworkManager.SetTLD`，它的存在正是為此。搞錯它不是外觀問題：`applyNetworkTransport` 會把 `n.TLD` 交給 `rolodex.EnsureNetworkScope`，由後者決定 home 作用域擁有哪個區域。

### 編址與介面

- **子網** —— `wireguard.SubnetForNetwork(seed, name)` 從一個機器身份種子與網路名推匯出確定性的 `/24`。以機器身份為鍵意味著兩台都對外提供 peer 服務的 Town OS 機器會選出不同的子網，因此同時加入兩者的裝置永遠不會遇到衝突。子網取自 `10.64.0.0/10`，以避開消費級路由器常發的 `10.0`/`10.1` 段。種子是 `networkIPAMSeed()`：systemd machine-id，其次是主機名，再次是一個常量，因此推導永不失敗——並把實例鹽折入其中。
- **介面名** —— `wireguard.InterfaceName(salt, name)` 是 `"town"` 加上加鹽網路名 SHA-256 的 4 位十六進位制：與建立順序無關、與網路數量無關，並且在核心 15 字元的限制之內。wg-quick 從配置檔名推導介面名，因此配置被寫作 `<InterfaceName>.conf`。`systemcontroller.NetworkInterfaceName(name)` 是整合測試所使用的、已應用鹽值的形式，這樣測試就絕不會去斷言一個根本沒被建立的裝置。
- **監聽埠** —— `wireguard.ListenPortForName(salt, name)` 以加鹽名稱的雜湊為偏移，從 `DefaultListenPortBase`（51820）起算，並在遇到另一個網路已佔用的埠時向前探測。

#### 實例鹽

WireGuard 的介面名、它的 UDP 監聽埠與它的 overlay 子網都是**名稱空間全域的**，而測試容器與開發容器都以 `--net host` 執行（這是刻意的——橋接網路的 DNS 在強制門戶網路下會失效）。沒有鹽值時，一台 `make test-full` 的機器與一台 `make dev` 的機器會為同一個網路名推匯出*相同*的介面名與監聽埠：後啟動的那個無法建立自己的裝置，它的 overlay 直接是死的。兩個併發的測試工作樹也會以同樣方式衝突——IRON RULE。

`TOWN_OS_WG_SALT`（`EnvWireGuardSalt`）被讀取一次到 `wireGuardSalt` 中。測試框架通過 `make/lib.sh` 中的 `wireguard_salt` 把它設為 `<role>-<INSTANCE_ID>`——role 在同一個檢出中區分測試機與開發機，`INSTANCE_ID` 區分不同檢出，兩部分缺一不可。對於給定的 role 與檢出它是穩定的，這一點對開發模式很重要，因為開發模式的資料庫跨執行留存，否則其中儲存的子網會指向以上一次鹽值命名的裝置。**真實機器不設定任何值，並保持歷史上不加鹽的名稱**；空鹽值會讓每一次推導原樣返回。

**podman 的預設子網池必須避開 `10.64.0.0/10`。** 執行時鏡像寫出的 `/etc/containers/containers.conf` 中含 `default_subnet_pools = [{"base" = "172.16.0.0/12", "size" = 24}]`，正是因為 podman 的預設值（10.89/16、10.90/15、10.96/11 等）全都落在 overlay 範圍之內：範圍內的 `/24` 會因與 overlay 路由衝突而被跳過，池子在負載下耗盡並報 "could not find free subnet from subnet pools"，包的容器網路隨之停止工作。不要刪除該檔案，也不要把池子重新擴回 `10.64.0.0/10`。

`wireguard` 包**自身不做任何介面控制**。它生成金鑰對並渲染 wg-quick 風格的配置；由 systemcontroller 把渲染好的配置寫入與宿主機共享的網路狀態目錄，再由一個生成的 systemd 單元拉起或關閉核心介面。這正是讓 systemcontroller 容器免於依賴宿主機網路名稱空間的原因。

**`applyNetworkTransport` 中的順序很重要。** 必須在介面已啟動、overlay 地址已分配、鏈路處於 UP 狀態並被路由覆蓋*之後*，才去編排 rolodex——已分配不等於可用。先編排它，就是在要求 rolodex 繫結一個宿主機尚不具備的地址；繫結會以 `EADDRNOTAVAIL` 失敗，而該監聽器會永久死亡，因為 rolodex 在 spawn 時就登記了監聽器，那具"屍體"隨後會擋住每一次重新宣告。

### Peer

`account.NetworkPeer` 攜帶 `Network`、`PublicKey`、`Name`、`AllowedIP`、`Endpoint`、`Rolodex`、`CreatedBy`、`ExpiresAt` 與 `CreatedAt`。

- **`Rolodex`** 標記那些在其 overlay 地址上執行 rolodex DNS 伺服器的 peer。本機隨後把該地址註冊為按 TLD 的轉發器，於是共享 TLD 之下、在該 peer 上具有權威性的名稱便可跨 overlay 解析。手機與筆記型電腦保持它為 false。
- **`CreatedBy`** 是歸屬鍵：持有 `wireguard` 授權的帳戶只能重新整理自己建立的 peer，因此受限帳戶無法讓別人的 peer 一直存活。
- **`Endpoint`** 取自**登記客戶端所撥打的地址**（其 `peers/add` 請求的 `Host` 頭），而不是取自本機對自身的認知。本機的公網 IP（來自 ipinfo.io）或區域網地址在 NAT、埠轉發或中繼之後是不可達的——同一 Wi-Fi 上的手機無法迴環到公網 IP，更完全無法路由到私有區域網地址，於是該 peer 會向著虛空握手，而在使用者看來這就像 DNS 壞了。被撥打的地址在構造上就是可達的：請求正是經由它到達的。若沒有可撥打的地址（例如環回登記），則**省略** endpoint，而不是設成一個不可能工作的值。

### Peer 登記的 TTL 與回收器

登記不會永久有效。`peer_ttl` 設定（單位秒，預設 `7200`）決定一次登記的有效時長。長期存活的客戶端會在其到期前通過 `POST /networks/peers/refresh` 重新整理自己的 peer；被棄用裝置的 peer 則自行過期，因此只增不減的 `peers/add` 端點不會悄悄堆積死 peer 並燒掉 overlay 地址。`ExpiresAt` 為 nil 表示該 peer 永不過期——例如 rolodex 伺服器與運維手動新增的裝置這類永久 peer。

過期時間始終由**服務端計算**為 `now + peer_ttl`；呼叫方從不選擇它。一個後台回收 goroutine 呼叫 `ReapExpiredPeers`，隨後為每個受影響的網路重新渲染一次傳輸配置，使執行中的 WireGuard 裝置與 rolodex 轉發器丟棄被回收的 peer。它是盡力而為且冪等的：持久化的 peer 集合才是事實來源，一次失敗的重新渲染會由下一次滴答或啟動時的 reconcile 修復。`peerReapInterval` 是 TTL 的四分之一，並被約束在 `[1m, 15m]`，因此失效的 peer 最多在過期後殘留約 TTL/4，而過小或過大的 TTL 都不會導致病態的掃描頻率。

### 已連線 Peer

`GET /networks/peers/connected` 把持久化的行與每條隧道的即時核心狀態聯接起來。持久化的那一半（名稱、帳戶、overlay 地址、過期時間）回答"誰被允許接入"；`wg show <iface> dump` 的那一半（握手、觀測到的 endpoint、傳輸量）回答"此刻誰真的線上"——任何一半單獨都不是完整的問題，這正是存在 `ConnectedPeerView` 而不是複用 `account.NetworkPeer` 的原因。

解析邏輯位於純函式 `wireguard.ParseDump` 中。dump 的**第一行**描述的是介面本身，會被刻意跳過；把它當作 peer 會憑空造出一個持有介面自身金鑰的幽靈。`wg` 的 `(none)` 與 `off` 佔位符會被解碼，而不是作為字面字串原樣透傳。

**連線與否，取決於握手是否落在 WireGuard 180 秒的 `REJECT_AFTER_TIME` 視窗內**（`HandshakeStaleAfter`）——那是該協議所能提供的唯一存活性訊號。協議沒有會話拆除，因此走開的 peer 與僅僅空閒的 peer 在其握手過期之前無法區分。*從未*握手過的 peer 保留 nil 時間戳而不是紀元時間，因為"從未建立"與"離線一天"是關於一台裝置的兩個不同事實。

systemcontroller 以 `--net host` 執行，因此它本來就與 wg-quick 建立裝置的名稱空間相同；執行時鏡像僅為 `wg` 這一個二進位制而附帶 `wireguard-tools`（wg-quick 仍在宿主機上、由生成的單元執行）。介面不存在不是錯誤——被停用的網路，或傳輸尚未拉起的網路，本來就沒有活躍 peer，而它持久化的行仍然必須渲染出來——而 dump 失敗會退化為只顯示持久化的行，而不是把面板整個清空。`home` 網路被完全排除：它沒有傳輸層，把它包含進來只會在一個講"誰通過隧道接入"的面板裡放一行永遠斷連的記錄。

**斷開連線複用 `POST /networks/peers/remove`**，而不是新增端點。WireGuard 沒有可以殺掉的會話，因此移除該 peer 就是唯一存在的強制終止手段。

### 網路 API

- `GET /networks`（需要鑑權）—— 列出所有網路，附帶 peer 數量、推匯出的介面名與執行狀態。私鑰絕不暴露。
- `POST /networks/create`（需要管理員）—— 建立網路。接受名稱與可選 TLD（預設取名稱）。推導子網、生成金鑰對、分配監聽埠，並返回創建出的網路。名稱或 TLD 已被佔用時返回 409——包括始終存在的 `home`。
- `POST /networks/remove`（需要管理員）—— 按名稱刪除網路。home 網路不能被移除。
- `POST /networks/enable` / `POST /networks/disable`（需要管理員）—— 拉起或關閉 overlay 介面。
- `GET /networks/peers?network=<name>`（需要鑑權，並受 `requireNetworkScope` 限制）—— 列出某個網路上已登記的 peer。該路由在 `wireguard` 授權的白名單上，因此受限帳戶可以訪問它；而 peer 列表會指明裝置、登記它們的帳戶以及它們的 overlay 地址——授權是對呼叫者自己網路的權限，而讀操作恰恰是最容易忘記這一點的地方。
- `GET /networks/peers/connected`（**需要管理員**）—— 所有 WireGuard 網路上的每一個 peer，聯接即時隧道狀態。它刻意比 `requireAuth` 的同類更嚴，並且不在 `grantRoutes` 中。
- `POST /networks/peers/add`（`requirePeerEnroll`：管理員或 `wireguard` 授權，且限定在呼叫者的網路內）—— 登記一個 peer。當 `public_key` 為空時，伺服器生成金鑰對並返回私鑰以及一份可直接匯入的裝置配置。接受可選的 `endpoint` 與一個 `rolodex` 標誌。**home 網路是 400** —— 它是僅 DNS 的，不承載 peer，無論誰來請求（參見[home 網路永遠存在](#home-網路始終存在)）。
- `POST /networks/peers/refresh`（`requirePeerEnroll`，且只能針對呼叫者自己登記的 peer）—— 把某個 peer 的 TTL 延長 `peer_ttl` 並返回新的過期時間，使客戶端能在 TTL 到期之前從容安排下一次心跳。
- `POST /networks/peers/remove`（需要管理員）—— 按公鑰移除 peer。

### 網路 UI

`/dashboard/networks` 列出各網路，並提供建立/移除/啟用/停用操作以及按網路的 peer 登記功能。第二塊 **Connected Peers** 面板逐項列出所有 WireGuard 網路上的每一個 peer——裝置、登記它的帳戶、它的 overlay 地址、它正在撥打的 endpoint、即時握手與傳輸狀態，以及它的登記過期時間——併為每一行提供 Disconnect 操作。

## TLS 與本地 CA

Town OS 執行自己的 X.509 證書頒發機構，因此包與 page 的流量可以按名稱經 HTTPS 提供服務，在區域網上既不需要公共 CA，也不依賴 ACME。

- **CA**（`src/tls/ca.go`）是位於 btrfs `tls` 子卷下的一對 ECDSA P-256 金鑰（`ca.crt`、`ca.key`），有效期 10 年，因此可以跨重啟存活。`EnsureCA` 載入已有 CA，或按需生成一個；證書全域可讀，而私鑰僅屬主可讀且絕不能被提供出去。CA 失敗是非致命的——系統會在沒有 HTTPS 的情況下啟動，而不是乾脆不啟動。
- **葉子證書**（`src/tls/leaf.go`）按包、按 page 簽發，寫作同一目錄中的 `cert.pem`/`key.pem`，因此消費方只需要一條掛載路徑。`IssueLeaf` 是**冪等的**：當已有證書恰好覆蓋所請求的 SAN 集合且仍然有效時，它不碰磁碟直接返回，這正是讓 reconcile 每次啟動都呼叫它而不會攪動證書檔案的原因。主機名可以是 DNS 名或 IP 字面量；任何能解析為 IP 的進入 `IPAddresses`，其餘進入 `DNSNames`。
- **`GET /tls/ca.crt`** 是**公開的**（並位於 `grantCommonRoutes` 中），因此任何客戶端——瀏覽器，或通過 overlay 加入的手機——都能取得根證書並信任這台機器。

包的葉子證書 SAN 集合與它的 A 記錄、DANE TLSA 屬主和 ingress vhost 由同一個 FQDN 推導；參見 [The package FQDN is one string](#包的-fqdn-是同一個字串--a-記錄葉子證書-santlsa-屬主ingress-vhost)。葉子證書還會帶上本機在安裝網路上的 overlay IP，因此 peer 可以用 WireGuard 裸地址訪問該包，而不只是靠名稱。

## Ingress

ingress 是共享的 Host 路由器：一個 sidecar，監管一個 Caddy 子程序，並暴露一套由 systemcontroller 編排的 gRPC 管理 API，方式與編排 rolodex 相同。它在記憶體中持有期望的路由集合，每次變更時渲染一份 Caddyfile，並零停機地過載 Caddy。

- **`src/ingress`** 是容器內的服務（`Server`、`renderCaddyfile`、gRPC 客戶端與 `town-os-ingress` 二進位制）。它以 `CGO_ENABLED=0` 構建。
- **`src/ingress/ingressctl`** 是 systemcontroller 一側的生命週期控制器：它生成、安裝並重啟 `town-os-system--ingress` 單元，並暴露 systemcontroller 所撥號的 gRPC socket 路徑。它之所以是一個獨立的包，正是為了讓無 CGO 的 ingress 二進位制永遠不會匯入 `src/systemd`（後者經由 sdjournal 引入 cgo）。

### 路由

- **`:443`** —— 每條路由一個 `https://<hostname>` vhost，用該路由以檔案方式固定的本地 CA 葉子證書終止 TLS（若是公共 FQDN 則用顯式的 ACME 簽發者），並反向代理到共享的 `town-os-ingress` podman 網路上的後端容器。
- **`:80`** —— 按 Host 路由：pages（`ServeHttp`）直接以明文 HTTP 提供服務（靜態內容，不含敏感資訊），包則獲得 HTTP→HTTPS 重定向以保持僅 HTTPS，而任何未被路由匹配的 host 落到預設後端——Town OS UI，因此在 UI 不再霸佔宿主機 `:80` 之後，裸 IP 登入（`http://<box-ip>/`）仍然可用。
- **尚未簽發葉子證書**的路由（非 ACME、證書目錄為空）在 HTTPS 上被跳過，因此置備到一半的條目絕不會讓 Caddy 拒絕整份配置；page 仍會獲得它的 `:80` vhost，那不需要證書。包只有在 HTTPS 目標真正存在之後才會被重定向，因此不會有任何東西重定向到尚未置備的證書上。

### 渲染

輸出**按主機名排序**，因此跨多次 reconcile 渲染出的位元組是確定性的——這正是讓監管程序對內容未變的過載做空操作的前提。全域配置為 `auto_https off`（證書由 Town OS 管理）與 `protocols h1 h2`（ingress 只發布 TCP，因此基於 UDP 的 H3/QUIC 不可達）。Caddy 的管理 API 被刻意**保持啟用**在其預設的容器本地 `localhost:2019` 上：監管程序用 `caddy reload` 編排新路由，而該命令正是與那個端點通訊，因此 `admin off` 會讓首次啟動之後的每一次路由更新都失效。

ingress 是**與網路介面無關的**：它以 `-p 443:443` / `-p 80:80` 釋出且不指定宿主機 IP，其 Caddyfile 中**沒有 `bind` 指令**，因此 Caddy 在所有介面上監聽，並純粹依據 SNI/Host 選擇 vhost。區域網客戶端與 overlay peer 命中同一個監聽器、SNI 選中同一個 vhost、拿到同一張本地 CA 葉子證書，並被代理到同一個容器。不要新增 `bind` 指令，也不要新增按網路的監聽器。

生產環境繫結 443/80；整合測試傳入臨時埠（渲染為 `host:PORT`），因此 `make test-full` 絕不會在特權埠上衝突。啟動流程通過 `RebuildIngress` 以宣告式方式編排完整路由集，與 `RebuildDNS` 是同一種推送模型；包與 page 的增刪改則通過同一套 gRPC API 編排增量變更。

## 啟動狀態與重新整理

`:5309` 在任何啟動工作開始之前就被繫結，因此 UI 可以觀看一次啟動——包括一次自我更新——的推進過程，而不是去輪詢一個死掉的埠。

### 啟動樁

`NewBootHandler` 是一個純粹的 `http.ServeMux`（這是刻意的，這樣它永遠不可能意外掛載一條真正的 API 路由），只提供三樣東西：

- `GET /status/ping` → `{booting, step, done, error, boot_id}`。它在**啟動過程中返回 503**，完成後返回 200，因此外部就緒探針——測試容器的 `wait_for_url`、編排系統的健康檢查——不會把這個樁當成"服務就緒"而開始猛擊一個只啟動了一半的控制器。JSON 響應體中仍然攜帶進度欄位，因此 UI 能區分"正在啟動"與"完全宕機"。
- `GET /boot-status` → 進度事件的 SSE 流。
- 其餘一切 → **403**，而不是 404：該路由在完整處理器中是存在的，只是在切換之前不可用。

`RootHandler.Swap` 在啟動末尾把這個樁原子地替換為完整的 Echo 路由器。監聽套接字從不關閉，因此不會出現埠抖動，而已經被分發的 SSE 處理器持有各自的 writer，可以跨越這次切換繼續流式輸出。

### 進度階段

五個粗粒度階段，刻意做得少而面向使用者——觀看自我更新的人想知道的是"控制器"、"DNS"、"系統服務"還是"我的包"卡住了，而不是哪個內部建構函式正在執行：

`boot_controller` → `boot_dns` → `boot_services` → `restart_packages` → `ready`

新鮮度階段會為每個已安裝的包額外發出一個事件，字首為 `restarting_`（`PackageStepPrefix`）；UI 去掉字首後把每個渲染成獨立一行，權重與粗粒度階段相同，這樣裝了很多包的機器展示的是真實進度，而不是一根卡住的進度條。這些按包生成的名稱刻意不符合固定階段所強制的 `[a-z0-9_]+` 形狀——它們是動態值。

階段字面量在 `main.go` 中以 `bs.Step("...")` 呼叫的形式重複書寫，而不是引用常量，因為 `TestBootStepsFrontendInSyncWithBackend` 會從 `main.go` 中解析它們，以證明前端的列表與之一致。**請保持兩者同步**；一旦漂移，該測試會大聲失敗。

### 廣播語義

`BootStatus` 可安全併發使用，並且**絕不阻塞啟動**。`Subscribe` 會先把歷史事件重放給新訂閱者（因此遲到的訂閱者不會錯過任何東西），並把緩衝區大小設為足以容納完整重放加上餘量；若啟動已經結束，它會在重放之後立即關閉 channel，使 `for range` 的消費者退出。`publish` 以非阻塞方式傳送——緩衝區被填滿的訂閱者會被丟棄並關閉，其客戶端會重連並再次獲得歷史重放。任何事件都不可能出現在 `Done` 之後。

### 程序身份與重新整理

`boot_id` 是每次 systemcontroller 啟動時重新生成的隨機 UUID，由樁與完整路由器的 `/status/ping` **雙方**上報（甚至在未認證的最小 ping 響應中也會攜帶，因為瀏覽器在重啟期間會短暫地沒有令牌）。在請求重新整理之前捕獲了該 id 的客戶端，可以據此區分"舊程序仍在應答"（id 相同）與"新程序已經起來"（id 不同）——否則二者無從區分，因為兩者都對 ping 返回 200，且啟動完成後都會對 `/boot-status` 返回 404。這正是 UI 的 Refresh Core Services 流程能夠觀看自己的後繼程序的原因。

`/boot-status` 因同樣的理由被排除在審計日誌之外：跨越處理器切換而保持流開啟的 UI，其下一個請求會落到完整路由器上並得到 404。那是這條流預期中的結束，而不是一次運維操作——審計它會在每一次成功的重新整理中記下一行失敗動作，並把儀表盤上那顆紅色的失敗計數徽標撐大。

`POST /system-services/refresh`（管理員）按依賴順序拉取每個系統服務鏡像——先是 systemcontroller 鏡像（版本錨點，這樣它在最後自我重啟時，剛拉取的鏡像已經在本地），然後是 rolodex（本機的 DNS，其餘拉取可能需要它來解析各自的 registry），最後是其餘鏡像並行拉取（最多 3 個併發）——並留下一個標記，供下一個程序的新鮮度階段消費以重啟已安裝的包。

## DNS 管理（Rolodex）

Town OS 內建一個由 `rolodex-dns` 容器驅動的本地 DNS 解析器。rolodex 伺服器為已安裝的包管理區域檔案與記錄，並通過 gRPC Unix socket 介面提供本地名稱解析。

### Rolodex Manager

rolodex 本身是由 systemd 安裝與監管的啟動服務——systemcontroller 不在容器層面安裝、啟動、停止或重啟它。取而代之，`rolodex.Manager` 負責：

- **`WriteConfig`** —— 把 `rolodex.yml` 寫入 `DataDir`。冪等：當該檔案存在、比 systemcontroller 二進位制更新、且內容已與預期一致時跳過寫入。返回一個布林值表示檔案是否被寫入（以便呼叫方決定是否重啟 systemd 單元）。
- **`WaitForDNSReady`** —— 通過 TCP 輪詢 `DNSLoopback:{port}`，直到它接受連線或超過 30 秒截止時間。在啟動時、任何依賴 DNS 的操作（例如鏡像拉取）之前呼叫。
- **`SystemServices`** —— 返回 rolodex 系統服務的後設資料（key、顯示名、鏡像、埠、單元名），使它與其他系統服務一同出現在狀態響應與 UI 中。
- **`Status`** —— 查詢 systemd 單元狀態以報告 rolodex 是否在執行。

rolodex 容器以 `--net host` 執行，並把 DNS 繫結到 `DNSLoopback`（`127.0.0.2`）的配置埠上（預設 `53`，測試中可通過 `DNSPort` 覆蓋）。鏡像標籤由 system controller 的釋出標籤推導（`quay.io/town/rolodex:<tag>`），可通過 `ROLODEX_IMAGE` 環境變數覆蓋。

**解析模式。** `rolodex.yml` 通過 `Config.ResolutionMode` 顯式固定 `resolution.mode`，預設為 **`auto`**（`DefaultResolutionMode`）——即 rolodex 自己的分層回退鏈：先從根伺服器迭代，然後 DoH/DoT，然後 `forwarders:` 列表，最後是 :53 上的公共解析器，並粘住最後一次成功的那一層。該模式被顯式寫出而不是留給 rolodex 的預設值，這樣當上遊改變其預設值時 Town OS 的行為不會隨之移動。轉發相關的整合測試會主動選用 `ResolutionModeForward`，並把 forwarders 指向一個本地樁。

**不要把裸 `recursive` 作為預設值。** 它*沒有*回退，而且 rolodex 的迭代解析器（`src/resolver.rs`）對每個域名伺服器只發送**一個不重傳的 UDP 資料包，截止時間 1500 毫秒**；噹噹前委派集合中的每個伺服器都失敗時，`resolve()` 報錯，而 `iterative_query` 會把*任何*錯誤都轉換成 SERVFAIL。因此一個丟包就會讓一次查詢 SERVFAIL；而在過濾或劫持出站 :53 的網路上（酒店、強制門戶、某些 ISP），*每一個*外部名稱都會 SERVFAIL。`auto` 在網路允許的地方保留遞迴帶來的隱私性，在不允許的地方則降級而不是失敗。相關：rolodex 的委派快取與否定快取落在 `ce44bb5`，而該提交**不在任何已釋出的標籤中**——在釋出版本包含它之前，recursive 模式會為每一個未快取的名稱與每一次 NXDOMAIN 重新從根開始走一遍（實測：冷公共名稱 0.6–1.9 秒，RFC1918 PTR 為 2.7 秒）。

該模式可由運維在執行時通過 `dns_resolution_mode` 設定配置（`auto` | `recursive` | `forward`；由 `ValidateDNSResolutionMode` 校驗，因此無法解析的值絕不可能到達 `rolodex.yml` 並把 DNS 弄砸）。`main.go` 在啟動時把它讀入 `rolodex.Config`；通過 `POST /settings/set` 的更改會執行 `Controller.RefreshDNSResolutionMode`，後者呼叫 **`Manager.RewriteConfig()`** 並重啟 rolodex 單元。`RewriteConfig` 之所以存在，正是因為 `WriteConfig` 拒絕覆蓋比 systemcontroller 二進位制更新的 `rolodex.yml`（它把那視為被手工編輯過）——而上一次啟動寫出的檔案*總是*滿足該條件，因此對於運維發起的更改 `WriteConfig` 會靜默地什麼也不做。啟動時用 `WriteConfig`，執行時更改用 `RewriteConfig`。

### 本地轉發器

Town OS 預設寫出的 `forwarders:` 列表是 `DefaultForwarders`——公共解析器。在阻斷外部 DNS 的網路上（酒店、強制門戶、只允許向自家伺服器發出 `:53` 的 ISP），這些恰恰就是被丟棄的地址，因此 `auto` 的轉發器層——那一層是在根伺服器與加密上游都已失敗*之後*才到達的，而那正是此種情形——無處可退。而該網路通過 DHCP 下發的解析器確實仍然會應答。

`dns_local_forwarders` 設定（預設 `false`，由 `ValidateBool` 校驗）把轉發器列表替換為本機自身網路配置所指向的解析器。它**不是一種解析模式**：它改變的是本地那一層*持有哪些*地址，而是否會去查詢那一層仍由模式決定——在 `auto` 中它是最後手段，在 `forward` 中它是唯一上游，在 `recursive` 中它根本不被使用。因此開啟它絕不能改變模式。

**預設關閉，而這個方向才是要緊的。** 本地解析器會看到這個家庭查詢的每一個名稱，而那正是從根解析所要避免的事情。這是運維在知情下做出的權衡，而不是機器在網路第一次出問題時替他做的決定。

發現邏輯位於 `src/rolodex/hostdns.go`。`HostResolversFrom` 按順序讀取 `hostResolvConfPaths`——**先**讀 `/run/systemd/resolve/resolv.conf`，再讀 `/etc/resolv.conf`——勝出的是第一個產出可用地址的檔案，而不僅僅是第一個存在的檔案。這個順序是承重的：在使用 resolved 的機器上，`/etc/resolv.conf` 裡是那個 stub（`127.0.0.53`），會因是環回地址而被丟棄，因此若發現邏輯止步於第一個*可讀*檔案，恰恰會在這項功能所服務的那些機器上一無所獲。上游檔案在容器內部可達，是因為 systemcontroller 單元繫結掛載了 `-v /run/systemd:/run/systemd`；丟掉這個掛載會讓發現悄悄退化。環回、未指定、組播與鏈路本地地址全部被丟棄——轉發到 resolved 的 stub 或轉發到 rolodex 自己的 `DNSLoopback` 監聽器都是查詢環路而非上游，而鏈路本地地址在缺少 `resolv.conf` 行不攜帶的 zone 時毫無意義。

**一無所獲的發現會保留已經配置好的轉發器。** `Manager.forwarders()` 依次回退到 `Config.Forwarders`，再到 `DefaultForwarders`，因此開啟這個開關絕不會讓本地那一層指向空無一物——那會比它本要替換掉的公共預設值嚴格更糟。

`main.go` 在啟動時把該設定讀入 `rolodex.Config`（無法解析的儲存值被讀作關閉——這是安全方向），因此換了網路的機器會在下一次啟動時用上新的解析器，無需運維操作。通過 `POST /settings/set` 的更改會執行 `Controller.RefreshDNSLocalForwarders`，與解析模式不同的是，它在標誌未變化時**不會**短路返回：在它已經開啟的情況下，被發現的地址本身可能已經變了，而重新渲染正是讓這一變化到達 rolodex 的途徑。`RewriteConfig` 仍會報告位元組是否真的變了，因此渲染結果相同就不會產生重啟。

`GET /dns/status` **同時**報告 `local_forwarders`（運維要求的）與 `forwarders`（`rolodex.yml` 實際持有的）。它們只在一種情況下不一致——發現沒有找到任何可用地址、於是保留了公共預設值——而那正是"開關顯示為開、卻什麼也沒變"的那唯一一種情形，因此只顯示標誌的 UI 會展示一項並未生效的設定。設定介面正因如此才渲染生效中的列表，並在它為空時明確說明。

**測試與開發中 rolodex 鏡像按架構拉取** —— make 測試框架拉取宿主機對應架構的 rc 標籤 `quay.io/town/rolodex:rc.latest-<arch>`（其中 `<arch>` 是 `uname -m` 的原始形式 `x86_64`/`aarch64`），而**不是**不帶架構字尾的普通 `rc.latest`。Town OS 內部的鏡像拉取預設走 rc 通道，因此測試框架、開發環境與執行時都跟蹤 `rc.latest-<arch>`。rolodex 從每台主機本機推送按架構的標籤（rolodex-dns 倉庫中的 `make push-rc` / `make push-release`），因此任何架構的測試主機都不需要多架構 manifest 組裝；*普通的* `rc.latest`（無架構字尾）是單架構 manifest，在另一種架構上會以 `exec format error` 崩潰重啟——只有帶字尾的 `rc.latest-<arch>` 可以安全地直接拉取。Makefile 計算 `HOST_ARCH`（規範化為 `x86_64`/`aarch64`）並把 `ROLODEX_IMAGE_TAG` 預設設為 `rc.latest-$(HOST_ARCH)`；`ROLODEX_IMAGE` 由它推導，並經由環境變數注入測試/開發容器。可用 `make ROLODEX_IMAGE_TAG=<tag> ...`（例如用 `latest-$(HOST_ARCH)` 取已釋出的 rolodex）或 `ROLODEX_IMAGE` 環境變數覆蓋。生產/執行時行為與之一致——除非設定了 `ROLODEX_IMAGE`，否則 systemcontroller 從自己的釋出標籤推導（並通過 `defaultVersionTag()` 回退到 `rc.latest-<arch>`）；測試與開發框架總是會設定它。開發容器中烘焙的 rolodex 單元（`integration/testdata/town-os-system--rolodex.service`）使用 `@ROLODEX_IMAGE@` 佔位符，在鏡像構建時經由 `integration/testdata/Containerfile.dev` 中的 `ROLODEX_IMAGE` 構建引數替換（該引數為空時構建失敗），因此烘焙的單元始終與測試框架載入的鏡像一致。

### 網路 TLD、雙棲與分離視界解析

每個網路都擁有一個 TLD，在 rolodex 中註冊為一個 `home_domain` 即該 TLD 的網路作用域
（`rolodex.EnsureNetworkScope`，由 `controller_networks_reconcile.go` 中的
`applyNetworkTransport` 呼叫）。擁有該 TLD 正是**劃分**它的機制：rolodex 會對加入
*其他*作用域的任何 WireGuard peer 隱藏本作用域的 TLD。預設/home 網路
（`account.DefaultNetworkName`，TLD 取自 `dns_tld` 設定，預設 `home`）以**僅 DNS**
作用域的形式擁有 `home.`——它不獲得 WireGuard 介面、overlay 子網或 peer 關聯，因此
永遠不會有源 IP 被繫結到 home 作用域。`.home` 因此只在區域網內有效、對每一個
WireGuard peer 都隱藏，同時在區域網上完全可解析。

**雙棲。** 安裝進非預設網路的包會被髮布兩次
（`registerScopedPackageDNS`）：

- 一條位於本機 **overlay IP** 的、該網路 TLD 之下的**作用域** A 記錄——按源 IP 提供給
  WireGuard overlay peer（`AddScopedRecord`）；以及
- 同一個 FQDN 位於本機 **區域網 IP** 的一條**全域** A 記錄
  （`RegisterPackageDNS`）——提供給環回/區域網客戶端。

每一側都得到一個它真正能路由到的地址。該網路 TLD 不會發布全域權威區域：裸的全域 A
記錄在區域網上無需區域即可解析，而 rolodex 的 **區域網→歸屬作用域回退**（rolodex-dns
解析步驟 5）會把作用域所擁有的 TLD 視為對區域網來源具有權威性——因此該網路 TLD 之下
未匹配到的名稱會從區域網側得到一個權威 NXDOMAIN，而不是把這個私有 TLD 洩漏到上游。
預設網路的包只留在全域 home 區域中（`registerPackageDNS`）；非預設網路的包絕不能出現
在那裡（這正是最初那個"解析成 `.home`"的缺陷）。

**分離視界小結。** 區域網客戶端（無 WireGuard）能解析**每一個**網路的 TLD（`.home`，
以及每個 WireGuard 網路的 TLD）加上公共網際網路。加入某一個網路的 WireGuard peer **只能**
解析那個網路的 TLD 加上公共網際網路——同級網路的 TLD 與 `.home` 都返回 NXDOMAIN。區域網
檢視從不被劃分；被劃分的只有 overlay peer。`RebuildNetworkDNS`（`reconcile.go`，啟動時
呼叫）為每個非預設網路的包重新註冊面向區域網的全域記錄，因此已安裝的包在重啟之後仍能
在區域網上解析；作用域記錄則在 rolodex 中獨立留存。啟動時的網路 reconcile 會被傳入
rolodex 客戶端，因此即便是冷啟動，home 作用域（以及每個網路作用域）也會被建立起來。

### 包的 FQDN 是同一個字串 —— A 記錄、葉子證書 SAN、TLSA 屬主、ingress vhost

**包的 DNS 名稱始終由其*安裝網路的* TLD 推導，絕不來自全域 `dns_tld` 設定。**
`packageFQDN(repo, name, tld)`（`src/svc/systemcontroller/controller_tls.go`）是唯一的
事實來源，而 TLD 來自 `networkTLDValue(nm, settingsMgr, network)`（它只在預設網路時才
回退到 `dns_tld`）。有四樣東西必須以完全相同的方式為一個包命名，其中任何一處不一致都
會悄無聲息地破壞服務：

1. 它的 **A 記錄**，2. 它的**葉子證書 SAN**，3. 它的 **DANE TLSA 屬主**，
以及 4. 它在共享 `:443` 上的 **ingress vhost**。

為防止它們漂移，FQDN 只被計算**一次**——在 `applyPackageTLS` 中，與簽發葉子證書同一行——
並作為 `PackageNetworkState.FQDN` 持久化（按包的網路狀態 JSON 中的 `fqdn`）。ingress 路由
構建器（`collectPackageIngressSites`）讀取該欄位而不是重新拼裝名稱，因此 vhost 在構造上
就是證書有效的那個名稱。`reconcileWriteNetworkState` **從其呼叫方**取得 TLD
（`reconcilePackage`，它已從安裝網路解析出該值）；它絕不能自行呼叫 `reconcileDNSTLD`。
那樣做過去是一個真實的缺陷：每次啟動都會以 SAN `<pkg>.<repo>.home` 重新簽發一個
`fart` 網路包的葉子證書，覆蓋掉正確的 `.fart` SAN，同時 ingress 渲染出一個無人撥打的
`<pkg>.<repo>.home` vhost——於是該包在區域網上可以解析，卻從未被真正提供服務。空的
`fqdn`（升級前的狀態檔案，或非 HTTP 的包）會回退到全域 TLD，並在下一次 reconcile 時自愈。

**ingress 與網路介面無關，也不需要按網路繫結。** 它以 `-p 443:443` / `-p 80:80` 釋出且
不指定宿主機 IP（即 `0.0.0.0`，因此區域網 + WireGuard + 環回都能到達），其 Caddyfile 中
**沒有 `bind` 指令**，因此 Caddy 在所有介面上監聽，並純粹依據 **SNI/Host** 選擇 vhost。
後端通過共享的 `town-os-ingress` podman 網路上的容器名訪問，而每一個由 HTTP 前置的包，
無論其 WireGuard 網路是什麼，都會加入該網路。因此區域網客戶端與 overlay peer 命中同一個
監聽器、SNI 選中同一個 vhost、拿到同一張本地 CA 葉子證書，並被代理到同一個容器。沒有
任何東西把監聽套接字繫結到 overlay IP——`BindOverlayAddress` 是 rolodex 的 *DNS 作用域
關聯*，不是套接字繫結。不要給 ingress 新增 `bind` 指令或按網路的監聽器。

包的葉子證書還會把本機在該網路上的 **overlay IP** 作為 SAN 帶上
（`networkOverlayIPValue`），因此 peer 可以用 WireGuard 裸地址訪問該包
（`https://10.65.0.1`），而不只是靠名稱。對預設網路（它沒有 WireGuard 傳輸層）該值為空，
這使得預設網路的葉子證書不會在每次 reconcile 時被攪動。

網路包的 DANE TLSA 與其 A 記錄一樣是**雙棲的**：`RebuildNetworkDNS` 註冊一個全域 pin
（經由區域網→歸屬作用域回退提供給區域網來源）*以及*一個作用域 pin（提供給 overlay
peer，它們的查詢永遠看不到全域記錄）。僅靠安裝流程只會寫出作用域那一半，而且跨重啟時
兩半都不會被重新發布。

### Pages 同樣是按網路限定作用域的

page 攜帶一個 `network`（`PageSite.Network` 列；`""` 表示預設/home 網路，與包的
`Installer.LoadNetwork` 是同一約定），並獲得**與包完全相同的待遇**：它的名稱來自該網路
的 TLD，它是雙棲的（作用域 overlay 記錄 + 全域區域網記錄），它的葉子證書攜帶該網路的
FQDN 加上本機的 overlay IP，它的 DANE TLSA 在該網路 TLD 之下被固定（全域 + 作用域），
並且它對*其他每一個*網路的 peer 都是隱藏的。`pageFQDN`（`pages_tls.go`）是
`packageFQDN` 在 page 一側的孿生體，`pageNetworkTLD` 則對應 `networkTLDValue`。

page 特有的一處曲折：page 的 FQDN **同時還命名著它在磁碟上的 btrfs
子卷與它的 webroot 符號連結**（pages 的 Caddy 以 `/srv/<host>` 為根）。因此 FQDN 不只是
一個標籤——弄錯它，內容就會從 ingress 所服務的那個名稱底下移走。三條推論：

- `reconcilePages` 用 `pageFQDN` 構建它的 `valid` 集合，因為該集合驅動
  `pruneStalePageSymlinks`——在那裡把一個 `fart` 網路的 page 命名為 `blog.home`，既會
  錯過它真正的 `blog.fart` 目錄，*又會*把仍在使用的符號連結剪掉。
- 改變 page 的**網路**會重新命名它的子卷/符號連結（`migratePageDir`），這與 `dns_tld`
  變更對預設網路 page 所做的完全一樣。
- `migratePageDirsForTLD`（`dns_tld` 變更的處理器）**跳過非預設網路的 page**——它們並非
  在全域 TLD 之下命名，因此重新命名它們會弄壞一個本來正常工作的 page。

pages 仍由 ingress 之後那個唯一共享的 `town-os-system--pages` 容器提供服務；網路只是
命名/DNS/證書層面的關切，不涉及按網路的容器或 podman 管路。

### DNS API

- `GET /dns/status`（需要鑑權）—— 返回 DNS 狀態，包括啟用標誌、執行狀態、TLD、記錄數量、`local_forwarders`（轉發器列表是否取自宿主機自身的解析器），以及 `forwarders`（`rolodex.yml` 實際持有的地址——參見 [Local forwarders](#本地轉發器)）。
- `GET /dns/records`（需要鑑權）—— 列出所有 DNS 記錄。
- `POST /dns/records/add`（需要管理員）—— 新增 DNS 記錄。接受名稱、記錄型別、值與 TTL。
- `POST /dns/records/remove`（需要管理員）—— 按名稱與型別移除 DNS 記錄。
- `GET /dns/tld`（需要鑑權）—— 獲取當前頂級域。
- `POST /dns/tld`（需要管理員）—— 設定 TLD。更改現有 TLD 並重新註冊所有已安裝的包。
- `POST /dns/setup`（需要管理員）—— 初始化 DNS 並註冊所有已安裝的包。
- `GET /dns/rbl`（需要鑑權）—— 獲取 RBL（Realtime Blackhole List，反向 IP）配置：全域啟用標誌、各提供方區域及其**已解析為實際生效值**的拒絕碼、列表級的 `refusal_cooldown_secs`，以及 `rotated_out`（當前因拒絕查詢而被輪換出去的提供方，附帶拒絕碼與剩餘秒數）。參見 [Refusal codes](#拒絕碼提供方說別再問了不等於說這個被列入了)。
- `POST /dns/rbl`（需要管理員）—— 替換 RBL 配置。接受一個啟用標誌、一個列表級的 `refusal_cooldown_secs`，以及一組 `{zone, enabled, refusal_codes, refusal_cooldown_secs}` 提供方。區域會被校驗為完全限定主機名，並轉小寫、去空白、去重；拒絕碼由 `ValidateRefusalCodes` 校驗（IPv4 地址或 `address/prefix`，按字首掩碼，`"none"` 只能單獨出現，不允許重複）。
- `GET /dns/dnsbl`（需要鑑權）—— 獲取 DNSBL（域名黑名單，正向名稱）配置，形狀與 `/dns/rbl` 相同。
- `POST /dns/dnsbl`（需要管理員）—— 替換 DNSBL 配置（形狀與校驗同 `/dns/rbl`；其拒絕冷卻時間與 RBL 的相互獨立）。
- `GET /dns/rbl/local`（需要鑑權）—— 列出本地 RBL 黑名單條目（`{name, reason}`）。
- `POST /dns/rbl/local/add`（需要管理員）—— 新增本地 RBL 條目。接受一個名稱（域名或 IP）與可選原因。名稱會被校驗（域名或 IP）、轉小寫並去空白。
- `POST /dns/rbl/local/remove`（需要管理員）—— 按名稱移除本地 RBL 條目。
- `GET /dns/dnsbl/allowlist`（需要鑑權）—— 列出 DNSBL 白名單條目（`{name, reason}`）。
- `POST /dns/dnsbl/allowlist/add`（需要管理員）—— 把某個名稱從基於名稱的黑名單檢查中豁免。接受一個名稱與可選原因。名稱會被轉小寫、去空白，並且**只校驗為域名**——IP 字面量會被拒絕（`ValidateDnsblAllowlistName`），因為白名單匹配的是名稱及其子域，永遠不可能匹配到一個地址。
- `POST /dns/dnsbl/allowlist/remove`（需要管理員）—— 按名稱移除白名單條目。名稱會被規範化但不會重新校驗，因此早於某次校驗規則變更的條目仍然可以被移除。
- `GET /dns/services`（需要鑑權）—— 列出已安裝的包服務及其釋出狀態（是否在 DNS 區域中）（`{repo, name, version, fqdn, domains, published}`），按 repo/name 去重。
- `POST /dns/services/set`（需要管理員）—— 在 DNS 區域中釋出或取消釋出某個包服務。接受 `{repo, name, published}`。持久化該選擇並立即註冊/登出記錄。

DNS 的只讀端點（`/dns/status`、`/dns/records`、`/dns/rbl/local`、`/dns/dnsbl/allowlist`、`/dns/services`、`GET /dns/tld`、`GET /dns/rbl`、`GET /dns/dnsbl`）被排除在審計日誌之外。白名單的*寫*操作會被審計（把一個名稱從所有黑名單中豁免是一項需要問責的變更）；與它們所對應的黑名單寫操作一樣，它們在 `account.RouteActions` 中沒有具名動作——由路徑本身標識它們。

### RBL / DNSBL 黑名單

Rolodex（0.2.4+）提供三種互補的垃圾/惡意/廣告攔截機制，外加（0.4.3+）一種撤銷機制與一種"不相信拒絕了查詢的提供方"的機制，全部通過 DNS API 與 `rolodex.Client` 封裝暴露（`SetRblConfig`/`GetRblConfig`、`SetDnsblConfig`/`GetDnsblConfig`、`AddLocalRblEntry`/`RemoveLocalRblEntry`/`ListLocalRblEntries`、`AddDnsblAllowlistEntry`/`RemoveDnsblAllowlistEntry`/`ListDnsblAllowlistEntries`）。全部由 **rolodex 按需查詢**——Town OS 從不下載、解析或預快取黑名單訂閱源。

注意該封裝的兩個 `Set*` 方法把列表級的拒絕冷卻時間作為末位引數（`SetRblConfig(ctx, enabled, providers, refusalCooldownSecs)`）；它們對映到上游的 `Set*ConfigWithRefusalCooldown`，因為上游那些保持引數個數不變的寫法是為了外部 API 相容性而存在的，而內部封裝並不需要這一點。

- **RBL**（Realtime Blackhole List）—— 反向 IP 黑名單區域，按需以反轉後的 IP 對某個區域發起查詢（例如 `zen.spamhaus.org`）。用於檢查反向 DNS 查詢中出現的 IP。通過 `/dns/rbl` 配置為一組 `{zone, enabled, refusal_codes, refusal_cooldown_secs}` 提供方，外加一個全域啟用標誌與一個列表級的 `refusal_cooldown_secs`。
- **DNSBL**（域名黑名單）—— 域名黑名單區域，按需通過把被查詢的域名前置到該區域來發起查詢（例如 `googleadservices.com` + `dbl.spamhaus.org`）。DNSBL 的命中優先於轉發/迭代得到的答案。通過 `/dns/dnsbl` 配置，形狀與 RBL 相同，並有自己獨立的冷卻時間。
- **本地 RBL 條目** —— 一份由資料庫支撐的名稱/IP 列表，通過 `/dns/rbl/local*` 手動管理，在外部提供方之前被檢查。**域名**型別的本地條目會以 `NXDOMAIN` 阻斷該域名的正向 A/AAAA 查詢，並立即生效（rolodex 在新增時更新記憶體快取）。
- **DNSBL 白名單**（rolodex 0.4.3+）—— 運維應對第三方訂閱源誤報的逃生艙口，通過 `/dns/dnsbl/allowlist*` 管理。一個條目覆蓋該名稱**以及它之下的每一個名稱**，因此把 `vendor.example` 加入白名單也會豁免 `cdn.vendor.example`。它會**短路整個基於名稱的檢查**，優先於已配置的 DNSBL 提供方以及任何匹配的本地 RBL 條目，並且它在提供方查詢*之前*執行，因此被豁免的名稱永遠不會發出那次查詢。同樣由資料庫支撐並帶記憶體快取，因此立即生效。

  沒有它，面對一個把家庭所需名稱列入黑名單的訂閱源，唯一的補救辦法就是停用整個提供方。請注意它與本地黑名單的不對稱：白名單條目**只能是名稱**，絕不能是 IP，因為它所短路的正是基於名稱的那次檢查。基於 IP 的 RBL 路徑不受它影響。

  **版本下限：** 較老的 rolodex 會以 gRPC `Unimplemented` 應答這三個白名單 RPC，表現為 500。`make test` 與 mock 的整合測試都發現不了這一點——`TestRolodexDnsblAllowlistRoundtripReal` 才是證明所固定鏡像足夠新的那個測試。

#### 拒絕碼：提供方說"別再問了"不等於說"這個被列入了"

DNSxL 對"命中黑名單"與"對查詢者的抱怨"返回的是**同一種記錄**——`127.0.0.0/8` 之下的一條 `A`——因此區分二者的只有地址本身。`127.0.0.2` 表示該名稱被列入；`127.255.255.254` 表示該查詢是經由公共解析器到達的，而 `127.255.255.255` 表示查詢者已超出限額。若把第二類讀作命中，那麼對該提供方檢查過的**每一個**名稱都會變成 `NXDOMAIN`：黑名單不再是黑名單，而成了一次故障。Spamhaus 公佈的免費使用限額，家用機器可能在毫無察覺的情況下越過，而越過時的症狀就是整個網路一片漆黑——那看上去像 DNS 壞了，而不像限流。

Rolodex 能識別這些碼，並在遇到拒絕時**把該提供方從查詢輪換中移出一段冷卻時間**，而不是相信它。Town OS 把兩半都暴露出來：

- **`refusal_codes`**，按提供方配置，兩個列表都支援。每一項是一個 IPv4 地址或 `address/prefix`——之所以支援字首，是因為提供方公佈的是整段範圍，而 Spamhaus 把整個 `127.255.255.0/24` 保留給錯誤碼並會隨時間往裡新增新碼，因此把今天的三個枚舉出來，會導致明天的第四個被悄悄讀作命中。
- **`refusal_cooldown_secs`**，按提供方與按列表配置。提供方的 `0` 表示沿用列表值；列表的 `0` 表示使用 rolodex 內建的預設值（3600）。
- **`rotated_out`**，出現在 `GET` 中，報告當前哪些提供方沒有被詢問、各自以什麼碼拒絕、以及剩餘多少秒。這是運維可見的那一半：沒有它，某個黑名單不再被查詢的唯一訊號，就是它不再攔截東西了。

**`ValidateRefusalCodes`（`controller_dns_validate.go`）精確鏡像 rolodex 的 `resolve_refusal_codes`**，因為該列表是被原樣透傳的，而對某一項的含義各執一詞會比根本不校驗更糟。三種情形：

- **為空** ⇒ rolodex 代入它內建的集合，因此在這一切存在之前寫下的配置無需編輯就能獲得安全的讀法；
- **恰好是 `"none"`** ⇒ 關閉檢測，供那些真實命中碼與內建碼衝突的私有黑名單使用；
- **其他任何值** ⇒ 恰好就是這些碼，且刻意**不**併入內建碼。

`"none"` 與真實碼混用會被拒絕——一個既要關閉檢測又指名了要檢測哪些碼的列表，沒有可選的讀法。碼會按其字首掩碼，且 **`/32` 渲染為裸地址**，與 rolodex 的 `Display` 一致：讀回來與剛提交的不一樣的碼，看上去就像機器改寫了運維的輸入。

**`GET` 報告的是已解析的碼**，因此一個沒有指名任何碼的提供方讀回來會帶著內建集合——這正是要點，因為運維必須能看到機器實際在拿什麼做匹配。這也意味著**客戶端絕不能在下一次儲存時把它原樣回傳**：那樣做會把今天的列表凍結進儲存的配置，此後 rolodex 新增的碼就會開始被讀作命中——正是這一機制要防止的失敗，只不過在上一層被重新引入。`BlocklistsTab.jsx` 中的 `toWire` 會把已解析的內建集合收攏回一個預設欄位，而 UI 保留一份內建列表的副本（`BUILTIN_REFUSAL_CODES`）只為一個用途：決定設定對話方塊開啟時選中哪個單選項。若那份副本漂移，對話方塊會開啟在 "Custom" 並預填當前生效的碼——那是外觀上的錯誤預設值，而不是錯誤的配置，因為除非運維儲存，否則什麼也不會改變。

**版本下限：** 早於拒絕碼處理的 rolodex 會接受這些欄位——proto3 忽略未知欄位——卻什麼也不儲存。mock 測試無法把這與成功區分開，因為 mock 會把遞給它的東西原樣回傳。`TestRolodexRblRefusalCodesRoundtripReal` 及其 DNSBL 孿生測試斷言：**空的**已配置列表讀回來必須是*已解析*的，而這正是老鏡像通不過的斷言。

**不存在訂閱源攝取/預快取**：提供方區域就是配置的單位；UI 提供一份精選的知名 DNSBL/RBL 區域列表作為一鍵快捷新增，但使用者可以新增任何區域。提供方區域的寫入會替換整份配置（經校驗、轉小寫、去重）。

**快捷新增列表是一種背書，並據此標準精選**（`ui/src/routes/dns/BlocklistsTab.jsx` 中的 `DNSBL_SUGGESTIONS` / `RBL_SUGGESTIONS`）。一個區域只有在家用機器開箱即可使用時才應出現在那裡：仍在運營、免費，並且無需註冊步驟即可應答一個自遞迴的解析器。當前的 DNSBL 有 Spamhaus DBL、SURBL、URIBL、NordSpam DBL、Spam Eating Monkey；RBL 有 Spamhaus ZEN、SpamCop、PSBL。

有三個被刻意**排除在外**，而 `TestBlocklistsTab` 的"不提供已停運或需註冊的區域"用例保證它們一直如此：`dnsbl.sorbs.net` 已於 2024-06-05 停運且其區域被清空，因此它是一個讀起來像保護的永久空操作；`b.barracudacentral.org` 要求先註冊查詢方 IP，未註冊的機器可能應答一陣子然後被切斷；UCEPROTECT 的 2/3 級會列出整個 ASN，因此一個壞鄰居就能封掉一整家 ISP。這三者都是*靜默*失敗——運維看到一個已配置的區域，就假定它在工作。

另請注意，RBL（反向 IP）區域只在反向 DNS 查詢中出現 IP 時才被查詢，而普通瀏覽幾乎不產生這類查詢。真正影響瀏覽的是 DNSBL（域名）區域，而它們是針對郵件中的垃圾 URL 調優的，而非針對廣告或追蹤器——廣告/追蹤器攔截屬於訂閱源的領域，而那[被刻意排除在範圍之外](#rbl--dnsbl-黑名單)。

### 按服務的 DNS 釋出

釋出是選擇退出制：每個已安裝的包服務都會被髮布到 DNS 區域中，除非它的 `repo/name` 鍵出現在 `dns_excluded_services` 設定裡（一個 JSON 陣列）。`/dns/services/set` 切換其成員身份並立即註冊/登出記錄；`RebuildDNS` 與 `ReconcileDNS` 會過濾被排除的服務（經由 `filterExcludedDNSInfo` + `loadDNSExcludedServices`），因此該選擇在重啟與 reconcile 之後仍然有效。未釋出的服務照常執行，但無法按名稱解析。

### DNS 管理 UI

DNS 管理介面在四個可深鏈的子標籤頁（`?tab=`）之上顯示 DNS 狀態（啟用、執行中、TLD、記錄數量）：

- **Records** —— DNS 記錄表，配有用於新增記錄（型別：A、AAAA、CNAME、MX、TXT、SRV、PTR）、移除記錄、更改 TLD 與初始化 DNS 的對話方塊。
- **Blocklists** —— DNSBL 與 RBL 的提供方區域區塊（全域啟用開關、按區域的啟用/移除、按區域的拒絕碼設定、建議區域快捷新增、自定義區域新增——全部為按需查詢），外加一張手動本地條目表（新增/移除）。每個區塊的開頭會列出當前因拒絕查詢而被退避的提供方（如果有的話）。沒有訂閱源，沒有"應用"按鈕，什麼也不快取。
- **Allow Lists**（`?tab=allowlists`，`ui/src/routes/dns/AllowListsTab.jsx`）—— DNSBL 白名單：一張帶原因的豁免域名錶，以及新增與移除。讀操作是 `requireAuth`，因此該標籤頁不限管理員；新增/移除控制元件僅限管理員。它是一個平級標籤頁而非 Blocklists 上的一張卡片，因為當某個東西無法訪問時，運維是按名稱去找豁免項的，而不是在滾動瀏覽提供方區域時順便發現它。
- **Services** —— 已安裝的包服務，配有釋出開關（在 DNS 區域中釋出/取消釋出）。

## 狀態端點

`GET /status/ping`（公開）返回系統狀態，包括：檔案系統數量（user、installed、uninstalled）、倉庫與包的數量、已安裝包數量、帳戶與管理員數量、服務單元數量（總數、活動、失敗）、系統服務單元數量（總數、活動、失敗）、近期審計錯誤（最近 5 分鐘）、初始化狀態（`needs_setup` 僅在不存在處於啟用狀態的管理員帳戶時為真；只要存在管理員，無論會話狀態如何都會顯示登入頁）、外部 IP（每小時從 ipinfo.io 獲取）、內部 IP（第一個非環回 IPv4 地址）、磁碟使用統計、升級可用性、伺服器 UTC 時區偏移的分鐘數、當前語言環境、`proton_enabled`（本次構建是否帶 `proton` 構建標籤）、`boot_id`，以及在提供了有效令牌時的已認證使用者名稱。

服務單元數量被拆為兩個欄位：`units` 只統計包服務單元（匹配 `town-os-package--*` 的），而 `system_services` 統計系統服務單元（匹配 `town-os-system--*` 的）。已解除安裝包遺留的 systemd 單元會被排除在包計數之外。已安裝包列表通過由每個包身份構造出的預期單元名，與發現到的 systemd 單元交叉比對。

該處理器只列舉一次帳戶（用於 `needs_setup`、總數與管理員計數），並且卷計數使用 `FilesystemNames` 而非 `ListFilesystems`——後者每個子卷都要執行一次 `btrfs qgroup show` 加一次 rootid 查詢，在約 30 個子卷的規模下，為了一個 ping 根本不會讀取的配額，要花掉這次 ping 大約一秒的延遲預算。

來自非 localhost 來源的未認證請求會收到一個最小響應，只含 `status`、`needs_setup` 與 `boot_id`。`boot_id` 即便在那裡也會攜帶，因為重新整理流程會跨控制器重啟輪詢 ping，而在此期間瀏覽器會短暫地沒有令牌；它是每個程序隨機生成的 UUID，不洩露任何系統資訊。已認證請求以及所有來自 localhost 的請求都會收到包含上述全部欄位的完整響應，另加 `repository_errors`（一個倉庫名到錯誤字串的 map，跟蹤按倉庫的重新整理失敗）。

當控制器仍在啟動過程中時，該路徑改由啟動樁提供服務，並返回 **503** 與 `{booting, step, done, error, boot_id}`——參見 [Boot Status and Refresh](#啟動狀態與重新整理)。

### 外部 IP 輪詢

system controller 從 `https://ipinfo.io/json` 獲取伺服器的公網（外部）IP 地址。該輪詢器在 HTTP 處理器建立時（`NewHandler`）以及 Unix socket 伺服器啟動時自動開啟。它在啟動時立即獲取一次 IP，隨後每 1 小時輪詢一次。每次獲取有 10 秒的 HTTP 超時。結果被快取在一個原子值中，並作為 `external_ip` 包含在已認證的 ping 響應裡。獲取失敗以 debug 級別記錄，不影響系統其餘部分；當尚未獲取到任何 IP 時，該欄位會從響應中省略。

## 監控

一套整合的 Prometheus + Node Exporter 監控棧提供系統指標。`monitoring.Manager` 把這套棧作為由 systemd 監管、帶 `Restart=always` 的 podman 容器（系統服務）來管理，使用 `town-os-system--` 命名字首。儀表盤前端可通過 `monitoring_backend` 設定配置。

### 監控埠

埠 **5308** 是專用的監控儀表盤埠（`TOWN_OS_MONITORING_PORT` 可遷移它；兩個環回埠同理，分別由 `TOWN_OS_PROMETHEUS_PORT` 與 `TOWN_OS_NODE_EXPORTER_PORT` 控制——參見 [System-service host ports](#系統服務的宿主機埠)）。這些埠以單個 `monitoring.Ports` 值傳達給三個服務，其空欄位由 `withDefaults()` 填充，因此預設值邏輯只存在於一處。當前生效的後端決定了在儀表盤埠上監聽的是什麼：

- **uPlot 模式**（預設）：一個 socat 轉發器（`socat TCP-LISTEN:5308,fork,reuseaddr TCP:localhost:9090`）把 Prometheus 的 HTTP API 暴露在 5308 埠上。React UI 直接查詢 Prometheus 的 `/api/v1/query_range` 並用 uPlot 渲染圖表。
- **Grafana 模式**：Grafana 直接監聽 5308 埠（經由 podman 埠對映）。React UI 內嵌一個 Grafana iframe。

**不存在**經由 systemcontroller（5309 埠）的反向代理。瀏覽器就所有監控資料直接與 5308 埠通訊。

### 監控後端設定

`monitoring_backend` 系統設定控制使用哪個儀表盤前端：

- `"uplot"`（預設）—— 在 React UI 中用 uPlot（約 35 KB）渲染的輕量內建圖表。經由 socat 轉發器在 5308 埠查詢 Prometheus。不會拉取或啟動 Grafana，首次啟動可省下約 771 MB。
- `"grafana"` —— 完整的 Grafana 儀表盤。Grafana 容器鏡像會被拉取並在 5308 埠啟動。預置了一個 Prometheus 資料來源以及登錄檔中的每一個儀表盤。

更改該設定會立即生效：切換到 `"grafana"` 會拉取 Grafana 鏡像並啟動容器（同時停止 socat 轉發器）；切換到 `"uplot"` 會停止 Grafana 並啟動 socat 轉發器。

### 監控容器

- **Node Exporter**（`quay.io/prometheus/node-exporter:latest`，宿主機埠 9100）—— 採集宿主機系統指標。以宿主機 PID 名稱空間、`SYS_TIME` 能力，以及把宿主機根檔案系統只讀繫結掛載到 `/host` 的方式執行。其 systemd 單元傳入 `--collector.diskstats.device-exclude=^(ram|fd)\d+$`（即 `monitoring.DiskstatsDeviceExclude` 常量）以覆蓋 node_exporter 的上游預設值（`^(ram|loop|fd|(h|s|v|xv)d[a-z]|nvme\d+n\d+p)\d+$`），後者會過濾掉分割槽（`sda3`、`nvme0n1p3`）與 loop 裝置——而那恰恰就是 `monitoring.BtrfsDevices` 為支撐 `/town-os` 的 btrfs 檔案系統所報告的裝置形態。沒有這項覆蓋，Disk I/O 儀表盤的查詢會靜默地返回零個序列，面板渲染為空。除非你同時把 Disk I/O 查詢遷離 `node_disk_*`，否則不要移除或放寬該標誌。迴歸覆蓋：`TestNodeExporterUnitConfigDiskstatsExcludeAllowsRealDevices` 固定該標誌與正則，而 `TestMonitoringNodeExporterEmitsDiskMetricsForFilteredDevices` 啟動一個真實的 node_exporter 容器，確認它至少為一個被上游預設值排除的裝置發出 `node_disk_read_bytes_total`。
- **Prometheus**（`quay.io/prometheus/prometheus:latest`，宿主機埠 9090）—— 以 15 秒間隔抓取 Node Exporter、它自身、rolodex（job `rolodex`）與 system controller（job `systemcontroller`，參見 [System Controller Metrics](#system-controller-指標)）。那兩個可選 job 在其地址未設定時會被省略，而不是指向一個猜測的預設值，因為無人配置過的目標會永久處於 down 狀態，讀起來像一個壞掉的服務而非一個缺席的服務。資料以 30 天保留期存放在持久化資料目錄中。配置與資料卷從監控資料目錄繫結掛載。該 systemd 單元包含 `ExecStartPre` 的 mkdir 指令，以便在啟動時預先建立卷目錄。
- **Grafana**（`docker.io/grafana/grafana:latest`，宿主機埠 5308）—— 可選的儀表盤 UI，僅當 `monitoring_backend` 為 `"grafana"` 時啟動。使用淺色主題（`GF_USERS_DEFAULT_THEME=light`）。匿名瀏覽以 Viewer 角色啟用，允許 iframe 內嵌。該 systemd 單元包含 `ExecStartPre` 的 mkdir 指令以在啟動時預先建立卷目錄。預置了一個 Prometheus 資料來源以及 [Dashboards](#儀表盤) 中描述的那些儀表盤；它們是如何被放到位的，參見 [Dashboard Provisioning](#儀表盤置備)。
- **Socat 轉發器** —— 即 uPlot 形態下的 `monitoring-ui` 單元（`town-os-system--monitoring-ui.service`），僅當 `monitoring_backend` 為 `"uplot"` 時啟動。把 5308 埠轉發到 9090 埠的 Prometheus。它使用的是與 Grafana *相同的單元 key*，而不是第二個單元：兩者是同一個服務的兩種可選實體，正因如此，切換後端才是一次單元重寫加重啟，而不是一對可能讓兩者都在執行或都不執行的啟停呼叫。

### 儀表盤

共有三個儀表盤，而且**兩種後端都從同一批查詢渲染出同樣的這三個**。它們之所以分開而不是併成一長頁，是因為它們回答不同的問題：System 是運維在機器髮卡時看的，DNS 是他們在某個名稱無法解析時開啟的，Controller 則是他們在 Town OS 所執行的某樣東西沒有在執行時開啟的。把八個 DNS 面板與十一個 controller 面板折進 overview，只會把那四個主機面板——人們開啟它的理由——埋掉。

**System**（Grafana uid `town-os-overview`，"Town OS Overview"）—— 四個面板：

1. **Disk I/O (/town-os)** —— 在支撐該 btrfs 檔案系統的各塊裝置上求和的讀/寫吞吐，因此無論檔案系統跨越多少裝置，面板都只顯示一條 Read 線與一條 Write 線。裝置正則由 `monitoring.BtrfsDevices` 代入；空列表會解析為 `NoBtrfsDevicesSentinel`，它匹配不到任何東西，因此面板渲染為空，而不是悄悄把宿主機上每一塊磁碟加總起來。
2. **Network (External)** —— 每個物理裝置的收/發位元每秒（排除 `lo`、veth、podman、cni、tailscale、網橋與 docker），並與 `node_network_up == 1` 聯接，因此曾經存在但現已 down 的介面不會拉出一條條平直的零線把圖例擠出螢幕。
3. **CPU Usage** —— 按模式（user、system、iowait、irq、softirq、steal、nice）堆疊，併疊加一條 Total 線，0–100%。
4. **Memory Usage** —— 總量、已用、可用。

**DNS**（Grafana uid `town-os-dns`，"Town OS DNS"）—— 基於 `rolodex` 抓取 job 的八個面板：

1. **DNS Queries by Response Code** —— `rate(rolodex_dns_queries_total)` 按 `rcode` 求和，堆疊。這個拆分本身就是面板，而不是一個下鑽檢視，因為單純的查詢計數無法區分繁忙的解析器與對一切都 SERVFAIL 的解析器——它們是同一條線。
2. **Query Latency** —— 由 `rolodex_dns_query_duration_seconds_bucket` 得出的 p50/p95/p99。這些桶在 `histogram_quantile` *之前*先按 `le` 求和，因為原始序列帶有 `proto` 標籤，不聚合就分位會畫出每種傳輸方式一條線，而不是全機範圍的延遲。
3. **Answers by Source** —— 由哪個解析階段作答（cache、local、scoped，或某個上游層級），堆疊。這個面板說明的是本機在自問自答，還是在轉發。
4. **Cache Hit Ratio** —— 命中加否定命中佔全部查詢的比例，0–100%。被快取的 NXDOMAIN 算作命中：它與一次正向命中同樣省下了一次上游往返。分母刻意不做鉗制，因此空閒的機器會讓線斷開，而不是為一個從沒被問過任何東西的快取畫出一個自信的 0%。
5. **Cache Entries** —— 正向、否定與黑名單快取的條目數。
6. **Blocklist Activity** —— 按種類的攔截數、被白名單豁免數，以及**被拒絕數**。拒絕與攔截共處一個面板是刻意的：提供方回答"別再問了"而不是"這個被列入了"，正是悄悄把黑名單變成一次故障的原因（[Refusal codes](#拒絕碼提供方說別再問了不等於說這個被列入了)），而它只有與被它取代的攔截率並列時才顯得反常。
7. **Upstream Tier Outcomes** —— 每一層的成功與失敗次數，以及耗盡了所有層級的查詢數。
8. **DNS Traffic** —— 線上收/發位元組數。

**Controller**（Grafana uid `town-os-controller`，"Town OS Controller"）—— 基於 `systemcontroller` 抓取 job 的十一個面板，也是唯一讀取本機自身 [`townos_*` 指標](#system-controller-指標)的儀表盤：

1. **Service Units by State** —— `townos_system_units` 與 `townos_package_units` 按 state，同處一個面板且**不堆疊**：它們是兩個各自獨立的總數，堆疊會畫出一個誰也管不著的合計高度。
2. **Service Health** —— `townos_system_unit_active` 與 `townos_package_unit_active`，每個單元一條序列，固定在 0–1。這個面板說明的是*哪一個*服務掛了，而不是掛了幾個。軸之所以固定，是因為該指標是布林值：自動縮放時，一台完全健康的機器會被畫成 1.0 附近的一團噪聲，恰恰在什麼都沒出問題的時候顯得嚇人。
3. **API Requests by Status** —— `rate(townos_http_requests_total)` 按 `status` 求和，堆疊。特意按 status 求和：該指標族還帶有 `method`，若保留它，按狀態的面板會畫出每個組合一條線。
4. **Audit Events** —— `rate(townos_audit_events_total)` 按 `result`，堆疊。
5. **Recent Failures** —— `townos_audit_recent_errors`（與儀表盤那顆紅色藥丸渲染的是同一個五分鐘計數）與 `townos_repository_errors` 並列。二者同處一個面板，是因為檢視「有沒有什麼壞了」的運維不應該還得先知道該去哪個子系統底下找；而且兩者都是近期視窗上的 gauge，因此歸零意味著恢復，而不是一個不再攀升的計數器。
6. **Package Inventory** —— 已安裝、可用、可升級，以及已配置的倉庫數。
7. **Town OS Disk Usage** —— `townos_disk_used_bytes` 與 `townos_disk_available_bytes`，堆疊。用的是已用與可用，而不是已用與總量：堆疊起來，這兩者*就是*檔案系統的大小，因此第三條序列只會把它重述一遍。
8. **Accounts** —— `townos_accounts` 按 kind，堆疊（這些 kind 恰好把賬戶列表劃分一次，因此堆疊高度就是真實總數）。
9. **Granted Accounts** —— `townos_accounts_granted`，之所以單列，是因為它是 user 這一桶的*子集*而不是第四種 kind，堆疊會重複計數。
10. **btrfs Subvolumes** —— `townos_filesystems` 按名稱空間，堆疊。
11. **Controller Uptime** —— `time() - townos_start_time_seconds`。訊號是那道鋸齒，而不是高度：一個在 `Restart=always` 之下悄悄崩潰重啟的 controller，在這裡的其他每個面板上都顯得健康。

`townos_up` 與 `townos_disk_total_bytes` 刻意**不**作圖。前者是抓取存活性的常量，一條平直的 1 不成其為面板；後者是第 7 個面板已經堆疊起來的那兩條序列之和。

每一條 DNS 查詢都帶有由 `monitoring.RolodexJobName` 構建的 `{job="rolodex"}` 選擇器，每一條 controller 查詢都帶有由 `monitoring.ControllerJobName` 構建的 `{job="systemcontroller"}` 選擇器，因此抓取配置發出的標籤與儀表盤所選擇的標籤不可能漂移開——不一致在任何地方都不是錯誤，它表現為一台明明正常的機器上一整個標籤頁空空如也的面板。

兩個前端是用不同語言寫的、渲染同一個儀表盤的兩套獨立程式碼，而它們**唯一**的差別是速率視窗：Grafana 按面板展開 `$__rate_interval`，而 uPlot 前端沒有巨集展開，因此它固定使用 `RATE_INTERVAL`（`5m`）。宏若洩漏到 uPlot 一側，就是一個 Prometheus 解析錯誤，會把整個標籤頁變成空白。

有四類測試把兩側綁在一起，因為再沒有別的東西連線它們：

- `TestRolodexDashboardMirroredInFrontendQueries` 與 `TestControllerDashboardMirroredInFrontendQueries` 從 Go 測試中讀取 `ui/src/components/monitoring/queries.js`，若任一側提到了另一側沒有的指標族則失敗——與 `TestBootStepsFrontendInSyncWithBackend` 對啟動階段所用的是同一種防漂移手段。
- rolodex 抓取整合測試斷言**所固定的 rolodex 鏡像確實匯出了** `monitoring.RolodexDashboardMetrics()` 中的每一個指標族，而 `TestControllerDashboardMetricsAreServed` 針對 `monitoring.ControllerDashboardMetrics()`、對著 controller 自身端點的一次真實抓取做同樣的斷言。兩者都以 `# TYPE` 行匹配，這樣名稱是另一個字首的指標族就無法為一個缺失的族背書。面板提到本機並不發出的指標族時，會渲染出一張空圖，而那與一台空閒的機器無法區分。
- `TestDashboardQueriesParseInPrometheus` 把每個儀表盤的每一條表示式送到一個真實的 Prometheus 面前。JSON 內部畸形的 PromQL 在任何地方都不是語法錯誤：檔案被成功置備，儀表盤被載入，面板畫出座標軸，然後永遠顯示 "No data"。
- `MonitoringDashboard.test.jsx` 斷言每個標籤頁掛載的都是**各不相同的** uPlot 元件。標籤頁列表裡寫的是元件本身，而不是由某個分支去挑一個，因為一個被遺漏了分支的標籤頁會落到 System 圖表上——在錯誤的標題下渲染出一個真實的儀表盤，而那看上去像是正常工作。

controller 儀表盤是唯一依賴於一個可能缺席的抓取 job 的：`ports.ControllerMetrics` 由 `-listen` 的值推導而來，而 `WritePrometheusConfig` 在推導不出時會**略去**該 job，而不是猜一個地址。被略去的 job 意味著一個沒有資料的儀表盤，而不是一個壞掉的儀表盤。

### 儀表盤置備

`monitoring.GrafanaDashboards(diskDevices)`（`src/monitoring/dashboard.go`）就是那份登錄檔——每個儀表盤的檔名、uid、標題與渲染出的 JSON——而 `WriteGrafanaProvisioningFiles` 遍歷它。新增一個儀表盤就是在那裡加一條，僅此而已：置備器（`GrafanaDashboardProviderYAML`）指向的是 `dashboard-json` **目錄**，因此其中的每個檔案都會被拾取。在登錄檔存在之前，檔案寫入器就是事實上的清單，這意味著要新增第二個儀表盤，就只能去改一段與儀表盤毫不相干的程式碼。

那些 uid 是常量（`OverviewDashboardUID`、`DNSDashboardUID`、`ControllerDashboardUID`），因為 Web UI 會深鏈它們。漂移的 uid 在任何地方都不會產生錯誤——Grafana 只會在 iframe 裡呈現一個 "dashboard not found" 頁面。

DNS 與 controller 儀表盤是**由面板規格構建並序列化**出來的（`src/monitoring/dashboard_dns.go`、`src/monitoring/dashboard_controller.go`），而不是像仍然如此的舊 overview 儀表盤那樣拼接進一份 JSON 模板。儀表盤中畸形的 JSON 代價不是少一個面板；它會讓置備失敗，於是該儀表盤根本不會出現。面板的 target 攜帶物件形式的資料來源引用（`{"type":"prometheus","uid":GrafanaDatasourceUID}`）——Grafana 13+ 無法解析 target 中的舊字串形式，會在不報任何錯誤的情況下渲染 "No data"。

### 生命週期

Prometheus 與 Node Exporter 在啟動時總是被啟動。監控後端設定決定另外啟動的是 Grafana 還是 socat 轉發器。啟動失敗是非致命的；系統會在沒有監控的情況下繼續執行。重啟由 systemd 的 `Restart=always` 策略處理。`Stop()` 方法是空操作，因為系統服務跨控制器重啟而留存。

### 監控 API

- `GET /monitoring/status`（需要鑑權）—— 返回 `backend`（`"uplot"` 或 `"grafana"`）、每個服務的執行標誌（`prometheus`、`node_exporter`、`monitoring_ui`，以及僅在 Grafana 模式下的 `grafana`），以及 `disk_devices`：支撐該 btrfs 檔案系統的核心裝置基名，前端會把它代入 Disk I/O 查詢。`disk_devices` 為空表示發現失敗，面板會回退到一個匹配不到任何東西的正則。當監控未配置時返回 `{"status": "disabled"}`。按服務的鏡像與單元後設資料不在此處——那是 `GET /system-services`。
- `GET /metrics`（localhost 或需要管理員）—— system controller 自身的 Prometheus 端點。參見 [System Controller Metrics](#system-controller-指標)。

### System Controller 指標

控制器把自身狀態以 Prometheus 文本展示格式匯出在**它已有的監聽器**上（`:5309`，`MetricsPath = "/metrics"`），而不是自己的埠上。這是刻意的：該端點因此搭載在測試框架已經會用 `TOWN_OS_LISTEN` 遷移的那個監聽器上，於是不需要向 `SYSTEM_PORT_FILES` 再新增宿主機埠，`make test-full` 與 `make dev` 也不可能在它上面衝突——IRON RULE。

它是 **localhost 或管理員**可訪問的，而非公開。這次抓取聚合了帳戶數量、磁碟使用與哪些服務已經宕掉：那是一張"攻擊什麼、以及機器何時最無力抵抗"的地圖。Prometheus 以 `--net host` 執行，因此它無需 podman 網路跳轉即可到達環回地址，與 node-exporter 目標完全一樣。

`src/metrics` 用幾百行渲染該格式，而不是依賴 `prometheus/client_golang`，理由與當初把 `errgroup` 擋在門外相同。那個庫的價值在於它的登錄檔、collector 介面與直方圖機制——而這裡一樣都沒用到，因為此處的每個值要麼是程序生命週期內的計數，要麼是每次抓取時從某個 manager 讀取的——與此同時它的傳遞依賴樹（`prometheus/common`、`procfs`、protobuf）卻是實打實的，並會落進一個從記憶體啟動的鏡像裡。

**標籤值的轉義是承重的，而非防禦性的。** 標籤值攜帶運維輸入（倉庫名、包名、systemd 單元名）。一個未轉義的引號毀掉的不是一行——它會讓 Prometheus 拒絕*整次*抓取，於是一個名字古怪的包就能悄悄把全部監控搞垮。

匯出的內容：

| 指標 | 型別 | 說明 |
|---|---|---|
| `townos_up` | gauge | 服務期間恆為 1；不服務時不存在 |
| `townos_start_time_seconds` | gauge | 執行時長為 `time() - 此值`，以抓取方的時鐘計 |
| `townos_package_units{state}` | gauge | `active`/`failed`/`inactive`，僅限已安裝的包 |
| `townos_system_units{state}` | gauge | `town-os-system--*`，排除 NC 與 socket 單元 |
| `townos_package_unit_active{unit}` | gauge | 按單元的 1/0，因此運維能看出*哪個*服務宕了 |
| `townos_system_unit_active{unit}` | gauge | 系統服務同理 |
| `townos_packages_installed` / `townos_packages_available` | gauge | 清單數量 |
| `townos_repositories` / `townos_repository_errors` | gauge | 錯誤只計數，不按名稱打標籤 |
| `townos_upgrades_available` | gauge | |
| `townos_accounts{kind}` | gauge | `admin`/`user`/`disabled` |
| `townos_accounts_granted` | gauge | 持有至少一項授權的非管理員 |
| `townos_filesystems{state}` | gauge | `user`/`installed`/`uninstalled` |
| `townos_disk_total_bytes` / `_used_bytes` / `_available_bytes` | gauge | |
| `townos_audit_recent_errors` | gauge | 與儀表盤上那顆紅色徽標所顯示的是同一個數字 |
| `townos_audit_events_total{result}` | counter | `success`/`failure`，由 `auditMiddleware` 遞增 |
| `townos_http_requests_total{method,status}` | counter | status 是一個**類別**（`2xx` 等），絕不是精確狀態碼 |

除 `townos_up` 與 `townos_disk_total_bytes` 之外，以上全部由 [Controller 儀表盤](#儀表盤)作圖，其面板集合是針對 `monitoring.ControllerDashboardMetrics()` 宣告的，因此兩份清單不可能漂移開。

這裡有幾個選擇本身就是要點，而非無關緊要：

- **一次抓取絕不會整體失敗。** 每個 collector 都容忍 nil 的 manager，並在出錯時記錄日誌後跳過。因為某一個子系統生病就返回 500，會讓其餘每一個指標恰恰在最需要它們的時刻消失，於是機器讀起來是徹底死了而不是部分降級——而且啟動過程中的抓取本就該報告哪些已經起來了。
- **值為零的桶仍然會被髮出。** 在零時消失的 gauge 與機器停止上報的 gauge 無法區分，因此"沒有失敗的單元"看起來會與"單元採集壞了"一模一樣。
- **狀態按類別分桶。** 每一個不同的狀態碼都會成為一個永久序列，而一個在數十條路由上返回 400/401/403/404/409/422 的控制平面會迅速把序列數乘開，只為回答一個沒人會對家用機器提出的問題。精確狀態碼本來就在審計日誌與請求日誌裡。
- **計數器在記憶體中且按程序計。** 跨重啟存活的計數器描述的是這台機器的歷史而非本程序的歷史，而 Prometheus 本來就理解計數器重置。這也讓一次抓取——以及為它供數的審計中介軟體——完全不碰資料庫。
- **`/metrics` 被排除在審計日誌之外**，也被排除在它自己的請求計數器之外。否則 15 秒一次的抓取每天會寫下約 5,700 行審計記錄，而它們描述的不是任何運維做過的事，並且會主導它所服務的那個計數器。
- **`metricsMiddleware` 註冊在三者的最外層**（在審計與授權白名單之前），這樣被任一道門拒絕的請求仍會被計數——一個無法解釋的 403 恰恰是這個計數器要暴露的東西。它從返回的 error 中取狀態碼，因為返回 error 的處理器此時還沒有寫出自己的狀態碼。

**抓取目標在任何地方都不會被重新拼裝。** `MetricsScrapeTarget(listenAddr)` 從伺服器繫結所用的同一個字串推導它，而 `main.go` 把結果交給 `monitoring.Ports.ControllerMetrics`——與 `PackageNetworkState.FQDN` 和 `Manager.MetricsAddr()` 存在的理由相同，都是唯一事實來源。通配繫結（`:5309`、`0.0.0.0:5309`、`[::]:5309`）會被改寫為 `localhost`，因為萬用字元不是任何東西能連線的地址；而顯式指定的主機會被原樣保留，因為改寫它會把抓取指向控制器刻意不在的地址。結果為空時會省略該 job，而不是指向一個猜測。當 `TOWN_OS_TLS` 開啟時，`ControllerMetricsScheme` 為 `https`，該 job 還會攜帶 `insecure_skip_verify`——葉子證書由本機自己的 CA 簽發，Prometheus 沒有理由信任它，也沒有乾淨的途徑拿到它，而這次抓取是宿主機名稱空間內的環回通訊，因此不可能有別的東西冒充它應答。

### 監控 UI

側邊欄導航中的監控標籤開啟一個儀表盤頁面，頁面上帶有 **System / DNS / Controller 子標籤頁**，與其他每一個帶子標籤頁的介面一樣可通過 `?tab=system|dns|controller` 深鏈，因此有人在故障期間正在看的儀表盤能挺過一次重新整理，也可以被分享成連結。未知的 `?tab=` 值會回退到 System，而不是什麼也不渲染。這份標籤頁清單是同一個陣列，既持有要掛載的 uPlot 元件，也持有展示相同面板的 Grafana uid，因此不可能出現某個標籤頁在一種後端有、在另一種沒有的情況——而在那裡寫明元件本身、而不是按標籤頁的值去分支，正是防止某個被遺漏的分支把 System 圖表渲染到另一個標籤頁標題之下的手段。

渲染方式取決於狀態響應中的 `backend` 欄位：

- **uPlot 模式**：在 React 中用 uPlot 直接渲染面板，在 5308 埠查詢 Prometheus。System 網格把自己釘在視口內（四個面板，每行兩個）；DNS 與 Controller 網格**不這樣做**——八個或十一個面板擠進一屏後每個只剩約 100px 甚至更少的畫布高度，到那個程度延遲圖就只是裝飾了，因此面板採用固定高度，頁面滾動。
- **Grafana 模式**：一個內嵌的 Grafana iframe，指向 5308 埠，使用 kiosk 模式與淺色主題。切換標籤頁會把該框架重新指向另一個儀表盤的 uid，並且 iframe 以該 uid 作為 key，因此框架是被*替換*而不是在其中導航——Grafana 有自己的歷史記錄，而在活動框架上替換 src 會讓瀏覽器的後退按鈕在多個儀表盤之間來回走，而不是離開該頁面。

兩種後端的面板標題完全一致：切換後端的運維不該還要去琢磨哪個面板變成了哪個。它們是硬編碼的英文——本介面不含任何 `t()` 呼叫，而且 Grafana 的面板標題無論如何都無法翻譯，因為它存在於被置備的 JSON 之中。

當所需服務未在執行時，會改為顯示一條警告橫幅與佔位資訊。

## UI 容器

system controller 通過 `ui.Manager` 把一個獨立的 UI 容器（`quay.io/town/ui`）作為系統服務管理。鏡像標籤由 system controller 的釋出標籤推導（`quay.io/town/ui:<tag>`），可通過 `UI_IMAGE` 環境變數覆蓋。啟動失敗是非致命的；系統會在沒有 UI 容器的情況下繼續執行。

## Web UI 佈局
### 儀表盤服務面板

儀表盤首頁在統計卡片網格之上顯示一個全寬的已安裝服務面板。該面板列出從 `GET /systemd/units` 獲取的所有包服務單元。每一行服務顯示：

- 一個狀態圖示：活動為綠色對勾圓圈，失敗為紅色叉號圓圈，未啟用為灰色圓圈。
- 包名（從 `package_identifier` 欄位解析）。
- 以文本形式呈現的活動狀態。
- 包描述（若有）。
- 來自 `POST /packages/installed/info` 的編譯後 notes，內聯渲染並帶有型別感知的連結（URL、郵箱、電話）。

點選服務行——狀態圖示或套件名稱皆可——會跳轉到 `/dashboard/system?search=<package_identifier>`，也就是該服務在服務頁面上自己那一行。服務頁面以 `?search=` 的值預先填入過濾框，並把該詞傳給 `GET /systemd/units-tree`；該搜尋比對根節點自身的欄位，因此頁面開啟時只顯示這一個套件及其相依子樹，而非整份清單。這個詞只是初始值，不是鎖定：清空或修改過濾框會重新展開清單。連結攜帶的一律是原始的 `package_identifier`，絕不是美化後的 `display_identifier`——後者不是樹搜尋能比對的詞，用它建構的連結會落在空樹上。當沒有安裝任何服務時該面板隱藏。notes 每個服務只獲取一次並被快取。

### 佈局

儀表盤採用雙欄佈局：左側是吸頂的側邊欄，右側是帶吸頂頂欄的內容區。

**側邊欄** —— 一個 256px 寬（`w-56`）的縱向面板，頂部是灰色橫幅中的 Town OS 徽標與品牌文字，其下是縱向堆疊的導航按鈕（每個帶圖示與標籤）。當前路由使用 `variant="secondary"`，非當前路由使用 `variant="ghost"`。

**頂部狀態列** —— 一條右對齊的橫條，顯示：連線狀態膠囊（載入中/離線/線上）、系統服務失敗計數（當 `system_services.failed > 0` 時顯示紅色膠囊徽標並連結到 `/dashboard/system?expand=system`）、帶管理員徽標的登入使用者名稱，以及登出按鈕。

## 系統服務

系統服務是由 systemd 管理的基礎設施容器（區別於使用者安裝的包服務）。它們使用 `town-os-system--` 單元名字首。

這一集合是：rolodex、ingress、pages、UI、node-exporter、Prometheus、監控 UI（socat 轉發器或 Grafana），以及**每個網路一個 gfeh 分割槽**（`town-os-system--gfeh-<network>`）。該清單中的每一項都必須在 `collectSystemServices()` 中註冊，這樣 `POST /system-services/refresh` 才會重新拉取並重啟它——那裡的遺漏是不可見的，直到某次升級悄悄把該服務留在舊鏡像上。

### 自動更新

**安裝器只交付兩個映象，其餘由控制器自行取得。** `install.sh` 只把兩個映象參照寫進它佈置的單元檔案——`quay.io/town/town`（systemcontroller）與 `quay.io/town/rolodex`——並且完全不把任何映象內容烘焙進 squashfs。其餘每一個系統服務映象（UI、ingress、網路控制器、物件儲存）都由控制器**在本機上**拉取：啟動時經由 `coreBootImages` 交給 `parallelEnsureImages`，按需時經由 `POST /system-services/refresh`。這正是那些倉庫必須允許匿名讀取的原因：機器本身不攜帶任何 registry 憑證，控制器中也沒有任何程式碼會寫出 `auth.json` 或執行 `podman login`。倉庫私有的映象就是這套設計取不到的映象，而其表現形式是一個不斷以 `unauthorized` 崩潰重啟的單元，而不是任何會點明真正原因的東西。

**一個每日定時器執行的，正是 UI 上那個按鈕所執行的同一套更新流程，而它隨安裝器一同交付。** `town-os-update.timer` 及其 service 位於 `../install/systemd/`，並在映像檔構建期就被啟用（`make/install.sh` 中的 `ENABLE_UNITS` 清單），因此機器從首次開機起就擁有它們，而不是要向一個必須先跑起來的控制器索取。該定時器在 `04:23` 觸發，並向 `/system-services/refresh` 發起 POST——刻意複用同一個端點，而不是再實作一遍「拉取全部」，這樣排程路徑就不可能與手動路徑發生漂移。單元只決定*何時*，控制器決定*做什麼*。它是 `Persistent=true` 的，因此在 04:23 處於關機狀態的機器會在下次啟動時補做更新，而不是再等一天——對於關機時間多過開機時間的機器，這個補做就是它唯一會得到的更新。該時刻刻意避開 podman prune 的 `03:17`：一邊回收未被參照的映象、一邊拉取新映象，是一場毫無收益的競爭。

**定時器的 POST 不帶憑證，這正是該路由為 `localhostOrAdmin` 的原因。** 該單元沒有任何權杖可以出示，因此 `/system-services/refresh` **僅**接納來自回送位址的未認證呼叫，而來自其他任何位置的呼叫仍需管理員權限——這與 `GET /metrics` 所依賴的豁免相同，理由也相同：來源位址為 `127.0.0.0/8` 的封包無法從網路被路由到本機。因此產生的單元明確指向 `127.0.0.1`，而絕不指向本機的可路由位址；後者既會把一個未認證的 POST 送上區域網路，又會在對端被拒絕。連線埠會跟隨控制器的 `-listen` 位址，因此被重新定位連線埠的控制器（整合測試框架為每次執行分配各自的連線埠）依然能夠自我更新。

**`auto_update_enabled` 約束的是定時器，而不是操作者。** 定時器會用 `?scheduled=1` 標記自己的呼叫；只有帶該標記的呼叫才會去查閱該設定。管理員按下更新按鈕時總是會更新，因為一個名為「自動更新」的開關，對一次明確的請求無話可說。被跳過的排程執行回傳 `200` 與 `{"status":"skipped"}` 而非錯誤——定時器詢問了是否該更新，並得到了「現在不用」這個有效答覆，這不是該讓 `systemctl status` 標紅的失敗。即便設定為關閉，定時器依然保持安裝並執行，因此翻轉該設定會在下一次觸發時生效，無需改動單元，也不存在單元狀態與設定各執一詞的可能。

該設定預設為**開啟**，且無法辨識的值一律視為開啟。關閉是一個封閉清單（`0`、`false`、`off`、`no`，不分大小寫）；其餘一切——包括手滑的錯字與讀取失敗的設定列——都讓更新繼續執行。這種不對稱是刻意的：正因為安裝器只交付兩個映象，一台停止拉取的機器就是一台永遠無法取得其餘大部分服務的機器，所以把「關閉」猜錯的代價，遠高於多拉取一次的代價。

### 系統服務單元生成

`GenerateSystemServiceUnit` 產出基於 podman、帶 `Restart=always` 的 systemd 單元。單元配置支援一個 `VolumeDirs` 欄位，列出需要通過 `ExecStartPre=/bin/mkdir -p <dir>` 行預先建立的宿主機目錄，以防容器在重啟後、system controller 尚未執行之前啟動時掛載失敗。

### 系統服務 API

- `GET /system-services`（localhost 或需要鑑權）—— 列出系統服務及其即時單元狀態。每一項包含 key、顯示名、鏡像、埠與 systemd 單元狀態欄位。當監控未配置時返回空列表。不計入審計日誌。
- `POST /system-services/status`（需要管理員）—— 改變某個系統服務的狀態。接受 key 與動作（`start`、`stop`、`restart`）。`enable` 與 `disable` 動作會被拒絕。
- `POST /system-services/refresh`（需要管理員）—— 刷新系統服務狀態。

## Web UI 生產鏡像

一個獨立的 UI 容器鏡像（`quay.io/town/ui`）由 `Containerfile.ui` 構建。它採用兩階段構建：`oven/bun:latest` 構建 UI 靜態檔案，然後 `docker.io/library/caddy:latest` 在 80 埠以 SPA 路由方式（`try_files {path} /index.html`）提供它們。UI 經由共享的 ingress 訪問，而不是直接霸佔宿主機的 `:80`——它是 ingress 對任何未被路由匹配的 host 的預設 `:80` 後端，因此裸 IP 登入仍然可用。

**快取頭是承重的**（`Caddyfile.ui`）。`/assets/*` 之下的一切都由 Vite 加了指紋，因此一個資源 URL 永遠精確對應某一次構建，並以 `public, max-age=31536000, immutable` 提供。`index.html` 是 Vite **不**加指紋的那一個檔案，而它正是指明當前 bundle 的那個；若提供時完全不帶 `Cache-Control`，瀏覽器可能會施加啟發式新鮮度（RFC 9111 §4.2.2）並在不重新驗證的情況下複用其快取副本，於是升級後的機器會繼續發放上一個版本的 `index.html`，而它指向的是上一個版本的 bundle。症狀是一次看上去根本沒發生過的升級——新功能渲染得就像 UI 從未聽說過它們。所有非資源路徑都是由 `try_files` 解析到 `index.html` 的 SPA 路由，因此 `no-cache` 規則被寫成覆蓋它們全部（`@html not path /assets/*`）。

`make release-ui` 以 `--no-cache` 構建，因此 `push-rc` 總是釋出新鮮構建的 UI 資源，而不是層快取中的 bundle。

**測試從不拉取 quay 上的 UI 鏡像** —— `ui-image` make 目標在本地把 `Containerfile.ui` 構建為 `localhost/town-os-ui:<INSTANCE_ID>`（始終與宿主機架構及倉庫內的 UI 原始碼一致），儲存到鏡像快取，測試框架再把它載入進測試容器並經由 `UI_IMAGE` 環境變數注入。`test-integration-build` 與 `test-ui-integration` 依賴 `ui-image`。quay.io/town/ui 的標籤只用於生產/釋出推送。`integration/systemcontroller_ui_test.go` 中的 `uiTestImage` 在 `UI_IMAGE` 未設定時跳過其測試，而不是回退到某個 quay 標籤。

## Proton 執行器鏡像

Proton 執行器鏡像（`quay.io/town/proton`）由 `Containerfile.proton` 構建。它採用兩階段構建：下載階段獲取 GE-Proton 發行版壓縮包（通過 `GE_PROTON_VERSION` 構建引數固定版本），執行時階段安裝 Wine/Proton 依賴（64 位 + 32 位）、用於無頭執行的 Xvfb，以及位於 `/usr/local/bin/proton` 的包裝指令碼，該指令碼會先啟動虛擬幀緩衝並配置 Proton 環境，再執行應用。

make 流水線提供：`release-proton-image`（構建）、`push-proton-rc`（推送按架構的候選釋出標籤 `rc.<date>-<arch>` + `rc.latest-<arch>`），以及 `push-proton-release`（推送按架構的釋出標籤 `release.<date>-<arch>` + `latest-<arch>`）。當 `PROTON_ENABLED=1` 時，proton 鏡像也包含在完整的 `push-rc` / `push-release` 流程以及 `manifest-rc` / `manifest-release` 的組裝之中。

## Web UI API 客戶端

瀏覽器在執行時從 `window.location` 確定 API 基礎 URL，使用當前協議與主機名加上 5309 埠（例如 `https://myhost:5309`）。不涉及任何服務端代理；瀏覽器直接與 system controller API 通訊。

設定了 `VITE_API_URL` 環境變數時，它會覆蓋瀏覽器推匯出的 URL。這在 API 伺服器運行於不同主機或埠的開發過程中很有用。

監控儀表盤從當前主機名推導其監控埠 URL（5308 埠）。當設定了 `VITE_API_URL` 時，主機名從中提取；否則使用 `window.location.hostname`。

## Web UI 無障礙

所有對話方塊元件都包含一個 `DialogDescription` 元素，簡要描述該對話方塊的用途。這滿足了 Radix UI 對螢幕閱讀器的無障礙要求，並消除 `aria-describedby` 警告。這些描述放在對話方塊頭部標題之後，對所有使用者可見。

## 國際化

所有面向用戶的字串（UI 標籤、錯誤資訊、toast 通知、審計日誌動作描述）都通過訊息目錄模式實現可翻譯。

### 後端

`i18n` 包提供一個 `T(locale, key, args...)` 函式來解析翻譯鍵。回退鏈是：請求的語言環境，然後 `en-US`，最後是原始鍵字串。當提供了 `args` 時會施加 `fmt.Sprintf` 格式化。訊息鍵使用點分名稱空間（例如 `auth.login_failed`、`pages.toast_provisioned`）。

### 已填充的目錄

後端目錄在 `src/i18n` 中按語言環境一個檔案（`de_de.go`、`zh_cn.go` 等）；前端鏡像位於 `ui/src/i18n`（`de-DE.js`、`zh-CN.js` 等）。兩側保持同步——每一個已填充的後端目錄都有一個前端孿生體。

`PopulatedLocales()` 是權威清單（48 項）：`en-US`、`ar-AE`、`ar-EG`、`ar-SA`、`bn-BD`、`bn-IN`、`cs-CZ`、`da-DK`、`de-AT`、`de-CH`、`de-DE`、`en-AU`、`en-CA`、`en-GB`、`en-IN`、`en-NZ`、`en-ZA`、`es-AR`、`es-ES`、`es-MX`、`fi-FI`、`fr-BE`、`fr-CA`、`fr-CH`、`fr-FR`、`hi-IN`、`hr-HR`、`hu-HU`、`it-IT`、`ja-JP`、`ko-KR`、`nl-BE`、`nl-NL`、`pl-PL`、`pt-BR`、`pt-PT`、`ro-RO`、`ru-RU`、`sa-IN`、`sk-SK`、`sl-SI`、`sv-SE`、`th-TH`、`tr-TR`、`uk-UA`、`vi-VN`、`zh-CN`、`zh-TW`。不在其中的一律回退到英語。`IsPopulated(code)` 是 UI 用來在語言選擇器中停用未填充條目的依據。

這份清單是**從目錄對映派生出來的，而不是手寫出來的**：`buildPopulatedLocales()` 在 init 時讀取 `catalogs` 的鍵，將其排序，並把 `en-US` 釘在最前面；`IsPopulated` 則直接對 `catalogs` 做索引。它過去是一份手工維護的切片字面量，只有一種失敗模式，而那種失敗是無聲的——一個已在 `catalogs` 中註冊、卻在字面量裡被遺漏的目錄，被翻譯了、被發布了，卻從未在選擇器中被提供出來。`PopulatedLocales()` 返回一個克隆，因為這份清單如今是套件層級的狀態，而不再是每次呼叫都新建的字面量；呼叫方對結果做排序或截斷，不能因此擾動下一個呼叫方看到的內容。

### 國家變體

一個目錄屬於兩種類型之一，其差別在於檔案是怎麼寫的，而不在於它是怎麼被選中的——兩種類型都算已填充，也都出現在選擇器中。

**語言目錄**是一份翻譯，完整寫出：`de_de.go`、`cs_cz.go`、`ja_jp.go`。

**國家目錄**由 `derive(base, overrides)`（`src/i18n/derive.go`，前端鏡像為 `ui/src/i18n/derive.js`）構建：取它所屬語言的目錄，再加上該國家確實說得不一樣的那些字串。奧地利德語就是德語；`de_at.go` 回答的問題不是「這句話德語怎麼說」，而是「這些句子裡，哪一句奧地利人不會那樣寫」。把 `de-DE` 複製進 `de_at.go` 再改上四行，將意味著下一個加進 `de-DE` 的訊息鍵會悄無聲息地以英文抵達奧地利，而對一條德語字串的修正得在三個檔案裡被找出來並重複一遍。繼承基礎目錄、只列出分歧之處，讓變體預設就是對的：一個新鍵在其基礎語言擁有它的那一刻，就落到了每一處。

有十八個語言環境是這樣派生出來的：

| 基礎 | 由它派生 |
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

`es-latam`（`src/i18n/es_latam.go`、`ui/src/i18n/es-latam.js`）是唯一的中間層：它承載所有美洲變體共有的、相對於半島西班牙語的分歧——`inválido` 而非 `no válido`，`agregar` 而非 `añadir`，直引號而非 `« »`——`es-AR` 與 `es-MX` 都建立在它之上。**它沒有註冊進 `catalogs`，也不可被選中**，因為它是一個共享片段，而不是任何人真正生活的地方；把它公布出去，等於提供一個並不存在的國家代碼。

有些覆寫對映很小，還有幾個（後端的 `en-CA`、`de-CH`，以及 `es-MX`）是空的。對一塊技術性的控制面板來說，這是誠實的答案——加拿大英語保留美式的 `-ize` 拼寫，而 `de_de.go` 裡沒有任何一條訊息含有 `ß`，瑞士的 `ss` 規則無處可施（前端的 `de-CH.js` 確實帶有真實的覆寫，因為 `de-DE.js` 用了 `ß`）。一份空的覆寫對映仍然標記出：這個語言環境是被慎重審閱過的，而不是被遺忘的。

這套方案由兩側的測試（`src/i18n/derive_test.go`、`ui/src/i18n/derive.test.js`）守住：每一個覆寫鍵都必須存在於其基礎目錄中，每一條覆寫都必須與它所替換的基礎字串確有不同，每一個派生目錄都必須帶有其基礎目錄的完整鍵集，並且每一個派生目錄都必須列在測試的 `variants()` 表裡——因此一個國家目錄不可能在這些規則未施加於它的情況下被發布。

**每個語言環境代碼都帶有地區子標籤**，`TestLocaleCodesAreRegionQualified` 維持這一點。蘇美爾語（`sux`）曾是唯一的例外——一個裸的 ISO 639-3 代碼——它已被移除。移除它是因為它的文字而非它的形狀：楔形文字位於 `U+12000`–`U+1254F`，幾乎沒有任何系統自帶能顯示它的字型，因此在任何沒有 Noto Sans Cuneiform 的機器上，該語言環境的每一個字串都會畫成替換方塊。目錄中括號裡攜帶的羅馬化轉寫卻留了下來，這讓情況比全空更糟——一堆窟窿周圍散落著拉丁字母片段與標點。要誠實地渲染它，就意味著自帶一份 webfont（該目錄用到 45 個不同碼位，但整套字型有 462K，做子集化又需要構建主機上有 `fonttools`），並新增 UI 完全沒有的 `@font-face` 機制——為一門沒有使用者的語言配備這麼多裝置，實在過重。

### 語言環境清單

全程使用 BCP 47 語言環境程式碼。提供了兩份精選清單：

- **CommonLanguages**（21 項）—— 阿拉伯語（ar-SA）、孟加拉語（bn-BD）、德語（de-DE）、英語（en-US）、西班牙語（es-ES）、法語（fr-FR）、印地語（hi-IN）、義大利語（it-IT）、日語（ja-JP）、韓語（ko-KR）、荷蘭語（nl-NL）、波蘭語（pl-PL）、葡萄牙語（pt-BR）、俄語（ru-RU）、梵語（sa-IN）、瑞典語（sv-SE）、泰語（th-TH）、土耳其語（tr-TR）、烏克蘭語（uk-UA）、越南語（vi-VN）、中文（zh-CN）。每一項都包含母語文字名稱與英文名稱。
- **ExtendedLocales**（89 項）—— 國家/地區特定語言環境變體的完整清單（例如 de-AT、en-GB、es-MX、fr-CA、pt-PT、zh-TW）。

### 前端

一個 React 上下文提供者（`I18nProvider`）包裹整個應用，並暴露一個 `useI18n()` hook，返回 `{ locale, setLocale, syncServerLocale, t }`。`t` 函式以與後端相同的回退鏈，針對前端目錄解析鍵。引數插值使用 `{name}` 佔位符（例如 `t('greeting', { name: 'Alice' })`）。

與之並列還匯出了 `translateIn(locale, key, params)`：它在指定的語言環境中翻譯，而不是在當前生效的語言環境中，回退鏈相同。它的存在是為了那條確認語言已切換的訊息：`t` 閉包捕獲的是呼叫它的那次渲染所用的語言環境，因此從語言表單裡發出的確認訊息會寫在**正在離開**的那門語言中——而這恰恰是頁面上唯一一條以"該語言已不再使用"為內容的訊息。

### 語言環境的檢測、儲存與同步

UI **首先從瀏覽器**選擇語言，而不是從全域設定。載入時它讀取 `navigator.languages`，並把這些有序偏好與已發布的目錄做匹配。匹配不區分大小寫，並會先在所有偏好上嘗試精確標籤，然後才按以下順序回退：

1. **精確匹配。** `de-CH` 如今自帶目錄，因此 `de-CH` 解析為 `de-CH`，而不是摺疊到 `de-DE`。
2. **中文按文字/地區消歧。** `zh-Hant` 或 `TW`/`HK`/`MO` 地區 → `zh-TW`，否則 `zh-CN`。文字是比任何預設值都更強的訊號，因此這一條排在下面兩條之前。
3. **有名有姓的地區預設值。** 那些不自帶目錄、卻讀某個變體而非其語言預設值的國家：西班牙語的拉丁美洲 → `es-MX`，葡語非洲與東帝汶 → `pt-PT`，愛爾蘭、非洲以及南亞與東南亞的英語 → `en-GB`。沒有這一條，`es-CO` 會拿到半島西班牙語，`en-IE` 會拿到美式英語。
4. **有名有姓的語言預設值。** `ar` → `ar-SA`，`bn` → `bn-BD`，`de` → `de-DE`，`en` → `en-US`，`es` → `es-ES`，`fr` → `fr-FR`，`nl` → `nl-NL`，`pt` → `pt-BR`。
5. **任何共享主子標籤的目錄。**

第 3、4 步之所以存在，是因為回退過去只有第 5 步，而那只在每種語言恰好有一個目錄時才是對的。如今有八種語言不止一個目錄：一個瀏覽器要一個光禿禿的 `en`，或者要 `en-PH`，否則就會落到 `catalogs` 物件中最先宣告的那一份英語上，於是答案成了匯入順序的屬性，而不是任何人做出的決定。

優先順序從高到低：

1. 明確的選擇，**按瀏覽器**持久化在 `localStorage` 中——*已釘住*
2. 瀏覽器檢測到並匹配上已釋出目錄的語言——*已釘住*
3. 伺服器的全域 `locale` 設定，稍後經由 `syncServerLocale` 應用——*未釘住*

一旦語言環境被釘住，`syncServerLocale` 就是空操作。這正是這一拆分的意義所在：過去 60 秒一次的狀態 ping 會呼叫 `setLocale`，從而在每次輪詢時把管理員的全域 `locale` 設定強加到每一個瀏覽器上。`locale` 設定（系統級，預設 `en-US`，仍在 ping 響應中上報）如今只是 Town OS 未提供目錄的語言的回退值。

### 語言環境 API

- `GET /locales`（需要鑑權）—— 返回當前語言環境、已填充語言環境清單、常用語言與擴充語言環境。不計入審計日誌。

### 設定 UI

系統設定頁面包含一個語言選擇器。常用語言以母語文字名稱顯示在下拉框中。一個可展開區域會顯示擴充語言環境清單。未填充的語言環境（即沒有翻譯目錄的）會帶星號字尾顯示，並在選擇器中被停用，從而無法被選中。

選擇器預設選中**頁面當前實際渲染所用的語言環境**——即 `useI18n()` 持有的那一個——而不是 `GET /locales` 返回的 `current`。二者在通常情況下就並不一致：語言環境由瀏覽器選定並被固定，而全域 `locale` 設定仍停留在預設的 `en-US`（參見[語言環境的檢測、儲存與同步](#語言環境的檢測儲存與同步)）。預選 `current` 會讓這個控制項在一個並非英文的頁面上顯示 "English"。當前語言環境若是國家變體，它位於預設摺疊的擴充清單中，因此載入時會展開該清單，以免下拉框停留在一個可見選項裡根本不存在的值上；再次摺疊時也會保留該條目，理由相同。`current` 只作為回退使用——僅當伺服端並不提供當前語言環境時。

儲存時會同時與這兩者比較。只符合其中之一仍然有事可做：與伺服端一致但與頁面不一致，意味著切換頁面語言（呼叫 `setLocale`，並為該瀏覽器固定此選擇），而不寫入設定；與頁面一致但與伺服端不一致，意味著寫入設定。只有當所選項與兩者都一致時，才真的無事可做。成功提示用 `translateIn` 寫在剛剛選定的那門語言中，因為它背後的介面已經切換過去了；而"無事可做"提示仍用螢幕上當前的語言，因為什麼都沒有改變。此前僅與 `current` 比較，使得正在顯示的那門語言無法被選中——對它按下儲存只會提示"無事可做"，因此要切回英文，必須先儲存第三種語言。

## System Controller 配置

### 啟動順序

逐步的權威啟動順序位於 [System Controller Boot Sequence](#system-controller-啟動順序)。概括如下：

1. `setupPodmanEnv()` 把 `CONTAINER_HOST` 指向宿主機的 podman socket。
2. 解析標誌，隨後立即以啟動狀態樁繫結 `:5309`。
3. 建立目錄、清理陳舊的根 DB、開啟資料庫，以及帳戶（外加舊服務帳戶清除）、會話、審計、設定、pages 與網路 manager——最後一個會播種 home 網路。
4. 播種倉庫、強制重新整理倉庫根。
5. 安裝 manager、btrfs 儲存、systemd manager；解析鏡像標籤。
6. 寫入 Rolodex 配置並等待就緒（rolodex 本身由 systemd 監管）。
7. 拉取核心鏡像（NC、監控、UI）並啟動監控系統服務。
8. 本地 TLS CA、ingress 與 pages 服務。
9. Reconcile 物件儲存（每個網路一個 gfeh 分割槽）。
10. 檢測版本變更、reconcile、執行更新後命令。
11. 重建 DNS、reconcile 網路、第二次（冪等的）物件儲存 reconcile、編排 ingress、啟動 UI 容器。
12. 新鮮度階段（重新整理之後按包重啟）。
13. 構建處理器，並把啟動樁原子地切換為完整路由器。
14. 一旦有分割槽應答，就在後台釋出物件儲存的名稱。

監控、Rolodex 配置、核心鏡像拉取、TLS CA、ingress、pages 服務、物件儲存、網路 reconcile 與 UI 容器的啟動失敗都是非致命的；系統會在沒有它們的情況下繼續執行。所有容器鏡像拉取都使用 `ensureImage` 助手，它在拉取前先檢查 `podman image exists`，從而避免在鏡像已預載入的測試/開發環境中重複拉取。非必要服務的拉取失敗會記錄到 stderr 且不阻止啟動，使系統即便在網路暫時不可用時也能啟動。

### 版本標籤檢測

system controller 為每一個同族服務（UI、Rolodex、網路控制器、ingress）推匯出匹配的鏡像標籤，全部來自 `resolveImageTag()` 解析出的同一個標籤：若設定了 `TOWN_OS_TAG` 環境變數則取之，否則取 `rc.latest-<arch>`（`defaultVersionTag()`，架構由 `runtime.GOARCH` 經 `archTag()` 對映為 `x86_64`/`aarch64`）。不存在編譯期的 `Version` 固定值，也不存在 `/town-os.tag` 檔案——兩者都被移除了，因為其中任何一處的陳舊值都會在控制器已經前進之後，仍悄悄把每個同族鏡像按住在舊標籤上。install 構建系統通過在 systemcontroller 的 systemd 單元上設定 `TOWN_OS_TAG` 來固定某個具體標籤（`../install/make/install.sh` 從 `CONTROLLER_IMAGE` 推導它）；沒有覆蓋時，整個機群始終跟蹤 `rc.latest-<arch>`。該標籤用於構造諸如 `quay.io/town/ui:<tag>` 與 `quay.io/town/rolodex:<tag>` 的鏡像引用；推送的標籤是按架構的，因此每一個推匯出的同族標籤都帶有架構字尾。

### 錯誤格式

所有 API 錯誤都以 RFC 9457 Problem Detail 物件返回（含 type、title、status 與 detail 欄位的結構化 JSON）。一個自定義的 `ProblemDetailHTTPErrorHandler` 被設為 Echo 的錯誤處理器。

### 請求日誌

Echo 的 `RequestLogger()` 中介軟體全域啟用，把所有 HTTP 請求記錄到 stderr。詳略程度由 `LOG_LEVEL` 環境變數控制。

### 登入限流

`POST /account/authenticate` 是公開的，而每一次嘗試都要付出一次 64 MiB 的 argon2id 雜湊。對密碼雜湊而言那是恰當的代價，但讓未認證的呼叫者無限制地安排這種代價就是錯的：幾百個併發嘗試就是幾十 GB 的分配，而這台機器的整個設計要點就是從記憶體執行，其失敗方式不是登入變慢——而是 OOM killer 把控制器帶走。

兩道相互獨立的限制，因為它們回答不同的問題。`loginLimiter` 在一個時間窗內限制**每個來源的嘗試次數**（5 分鐘 20 次），這是讓線上密碼猜測變得不可行的機制，並且它按來源地址分鍵，因此一個濫用的客戶端無法把這個家庭鎖在門外。`loginGate` 限制跨所有來源的**併發雜湊數**（4 個，把 argon2 的峰值記憶體約束在四分之一 GB 附近），而這是僅靠按來源限流做不到的。兩者都在記憶體中且按程序計：它們保護的是本程序的記憶體與 CPU，而持久化它們會讓一次失敗的登入變成一次資料庫寫入。

兩者都在雜湊**之前**檢查，而不是之後——要防禦的代價正是雜湊本身，因此一次仍然做了雜湊的拒絕，等於為它所拒絕的攻擊付了錢。gate 的名額通過閉包內部的 `defer` 釋放，而不是在呼叫之後釋放，因為被 panic 洩漏掉的名額會在程序餘生中消失，四個這樣的名額就能讓這台機器上的每一次登入卡死到重啟為止。一次被證實正確的密碼會清空該來源的時間窗，因此處在同一個 NAT 地址之後的一個家庭，不會因正常使用而走進鎖定狀態。

### CORS

在 `DEBUG` 模式下允許所有來源。否則，允許來自同一主機名的跨埠請求（例如 80 埠上的瀏覽器與 5309 埠上的 API 通訊），**但前提是 Host 頭已被核對為這台機器可以合法被稱呼的名稱之一**。允許的方法：GET、HEAD、POST、PUT、PATCH、DELETE、OPTIONS。允許攜帶憑據，最大存活時間 3600 秒。

這項檢查之所以重要，是因為舊規則——"Origin 的主機名等於 Host 頭的主機名"——比較的是兩個都來自同一個攻擊者選定 URL 的值。把 `box.evil.example` 指向這台機器的區域網地址，瀏覽器就會發送 `Origin: http://box.evil.example` 與 `Host: box.evil.example:5309`，二者匹配。那正是 DNS 重繫結的形態，而在 `AllowCredentials` 之下，它把引導視窗（在不存在啟用的管理員時 `POST /account/create` 會以未認證方式應答）交到了一個順路訪問的網頁手裡。

因此 `originAllowed` 要求 Host 頭指名這台機器：它自己的主機名、`<hostname>.local`、`<hostname>.<dns_tld>`、它所應答的環回與區域網地址，或運維在 `AllowedHosts` 中配置的任何名稱。這些形式是**逐一列舉的，而不是按字尾匹配**——像"任何第一個標籤是該主機名的名稱"這樣的規則會接受 `townos.evil.example`，而攻擊者只需去註冊它即可。IP 字面量單獨即可被接受：地址無法被 DNS 別名化，因此 `http://192.168.1.10/` 訪問 `http://192.168.1.10:5309` 在構造上就是同一台機器，而這也是實際中最常見的用法。

**私有網路訪問（PNA）只對 CORS 會接受的來源作答。** `Access-Control-Allow-Private-Network` 頭此前是無條件回顯的，那等於把瀏覽器"可以訪問私有地址"的許可交給網際網路上的每一個來源——而那正是 PNA 在 CORS 之上要額外提供的唯一保護。它的中介軟體註冊在 CORS 中介軟體**之前**，因此在預檢請求上它仍然會執行——預檢由 CORS 自己應答，不會繼續呼叫後面的鏈條。

### 優雅關閉

SIGINT 觸發 context 取消。HTTP 伺服器關閉，所有後台 goroutine 經由 context 通道退出。Rolodex 由 systemd 監管，不由 systemcontroller 停止。

### 命令列標誌

- `-db <path>` —— SQLite 資料庫路徑（預設為臨時檔案）。
- `-btrfs <path>` —— btrfs 子卷操作的基礎路徑。
- `-repo-dir <path>` —— git 倉庫的基礎目錄（預設為臨時目錄）。
- `-network-state <path>` —— 按包的網路狀態檔案所在目錄（預設 `/run/town-os`，即 `DefaultNetworkStatePath`；它必須是 systemcontroller 容器與宿主機共享的路徑——絕不能是 `/var/run/...` 或 `/tmp`）。
- `-listen <addr>` —— HTTP 監聽地址（預設 `:5309`）。

網路控制器鏡像同樣不是標誌；它由解析出的鏡像標籤推導，並可用 `NC_IMAGE` 覆蓋。

### 環境變數

- `CONTAINER_HOST` —— 宿主機 podman 守護程序的 unix socket URL。啟動時自動設為 `unix:///run/podman/podman.sock`（參見 `HostPodmanSocket`）。每一次 `podman` 呼叫——包括 systemcontroller fork 出的子程序——都從程序環境繼承它，並走宿主機 socket，而不是 systemcontroller 容器隔離的 podman 儲存。install 倉庫中的 systemd 單元也應設定 `Environment=CONTAINER_HOST=...` 以便在 `systemctl` 輸出中可見，但 `setupPodmanEnv()` 的呼叫才是執行時的事實來源。
- `TOWN_OS_LISTEN` —— 覆蓋 `-listen` 標誌。
- `TOWN_OS_SIGNING_KEY` —— 覆蓋臨時的 JWT 簽名金鑰（參見會話管理）。
- `TOWN_OS_TLS` —— 讓控制平面自己的監聽器（`:5309`）以 HTTPS 提供服務，由本機的本地 CA 終止，其葉子證書的簽發方式與包的完全一致。**預設關閉，而這是次序問題而非折中**：沒有拿到本機 CA 的瀏覽器無法對一張不受信任的證書完成 XHR，而與頁面導航不同的是，這裡沒有可以點選通過的中間頁——UI 會直接停止工作，而且無從抵達那個解釋原因的介面。今天 UI 也是通過明文 HTTP 提供的（它是 ingress 的預設 `:80` 後端），因此沒有先安裝 CA 就開啟它的機器，會從"未加密"直接變成"宕機"。運維應先安裝 CA（`GET /tls/ca.crt`，公開），再設定本項。接受 `1`/`true`/`yes`/`on`。它在監聽器繫結**之前**解析，因此以 HTTP 開始的啟動狀態流絕不會在其客戶端腳下變成 HTTPS；並且失敗時是**致命的**，而不是回退到明文：一個要求了 TLS 卻悄悄得到明文的運維，處境比一台拒絕啟動並說明原因的機器更糟。
- `TOWN_OS_TLS_CERT` / `TOWN_OS_TLS_KEY` —— 運維自備的證書與私鑰，適用於前置名稱已經擁有公共受信證書的機器。**同時**設定兩者即可自行啟用 TLS，且不會查詢本地 CA；只設置其中一個則什麼也不會發生。
- `TOWN_OS_TLS_SANS` —— 為生成的葉子證書追加的名稱或 IP，逗號分隔，適用於通過控制器無法推導的名稱訪問的機器（CNAME，或路由器分配的 DHCP 名稱）。
- `TOWN_OS_TEST` —— 若設定，則使用測試倉庫而非生產預設倉庫。
- `DEBUG` —— 若設定，則允許所有 CORS 來源，並把測試倉庫前置到預設倉庫之前。
- `LOG_LEVEL` —— 日誌級別：`debug`、`info`、`warn`、`error`（預設 `error`）。
- `TOWN_OS_REPO_USERNAME` / `TOWN_OS_REPO_PASSWORD` —— 首次初始化時應用到所有倉庫的倉庫憑據。
- `TOWN_OS_TAG` —— 固定每個同族鏡像所推導自的鏡像標籤（參見 [Version Tag Detection](#版本標籤檢測)）。由 install 構建系統在 systemcontroller 的 systemd 單元上設定。
- `ROLODEX_IMAGE` —— 覆蓋 Rolodex 容器鏡像（預設 `quay.io/town/rolodex:<tag>`）。
- `UI_IMAGE` —— 覆蓋 UI 容器鏡像（預設 `quay.io/town/ui:<tag>`）。把它設為**空字串**（顯式存在但為空）會完全跳過 UI 容器——開發模式，此時由 bun 提供 UI。
- `NC_IMAGE` —— 覆蓋網路控制器鏡像（預設 `quay.io/town/networkcontroller:<tag>`）。整合測試框架用它注入本地構建的 NC。
- `INGRESS_IMAGE` —— 覆蓋 ingress 鏡像（預設 `quay.io/town/ingress:<tag>`）。把它設為空字串會跳過 ingress 與 pages 服務——開發模式。
- `GFEH_IMAGE` —— 覆蓋物件儲存鏡像（預設 `quay.io/town/gfeh:<tag>`）。把它設為**空字串**會完全跳過物件儲存——開發模式。當 ingress 被停用時物件儲存同樣會被跳過，因為四個 HTTP 檢視只能經由它訪問。
- `GFEH_SMB_PORT_BASE` —— 覆蓋 SMB 監聽器本會起始的宿主機埠（預設 `4450`）。這是遺留項：[沒有任何分割槽提供 SMB 服務](#不提供-smb-檢視)，因此不會分配宿主機埠。保留接線是為了讓測試框架的設定保持無害。
- `TOWN_OS_WG_SALT` —— 實例鹽，用於把本機的 WireGuard 介面名、監聽埠與 overlay 子網與共享同一網路名稱空間的另一個 Town OS 區分開。真實機器不設定它；由測試與開發框架設定。參見 [The instance salt](#實例鹽)。

#### 系統服務的宿主機埠

每個系統服務都以 `--net host` 執行，因此這些埠全都繫結在控制器所處的那個網路名稱空間中——即*宿主機*名稱空間，在整合測試框架內部也是如此（其容器同樣刻意以 `--net host` 執行，以便在橋接 DNS 失效的強制門戶網路下構建仍能工作）。因此一台 `make test-full` 的機器與一台 `make dev` 的機器會爭奪這裡的每一個埠，並在 `Restart=always` 之下永遠互相把對方拖入崩潰重啟。

下列每一項各自遷移其中一個埠，並且**預設為生產埠**，因此未設定任何環境變數時會精確復現今天的啟動行為。`make/lib.sh` 的 `system_port_env` 按次執行把它們分配到 `SYSTEM_PORT_FILES` 並傳給測試容器——IRON RULE。`make dev` 刻意**一個都不設定**：dev 鏡像的是真實機器，那裡 `redirect_host_dns` 需要 rolodex 在 `:53` 上，瀏覽器需要 ingress 在 `:443` 上。無法解析的值會在 stderr 上報告並回退到預設值，因為打字錯誤否則看起來會與根本沒設定一模一樣。

- `TOWN_OS_DNS_PORT` —— rolodex 提供 DNS 服務的埠（預設 `53`，位於 `DNSLoopback`）。**當它為非預設值時，systemd-resolved 的路由配置會被完全跳過**：resolved 的按域名伺服器地址不攜帶埠，因此把 resolved 指向 `DNSLoopback` 只會悄悄黑洞掉該 `.tld` 之下的每一次查詢，而不是把它們留給正常的解析路徑。
- `TOWN_OS_ROLODEX_METRICS_PORT` —— rolodex 提供其 Prometheus `/metrics` 端點的埠，同樣位於 `DNSLoopback`（預設 `9153`）。它與 DNS 埠是彼此獨立的監聽器，需要各自的覆蓋項；`rolodex.Manager.MetricsAddr()` 是 `rolodex.yml` 與 Prometheus 抓取目標共同構建自的那一個字串，因此遷移它會同時移動兩者。
- `TOWN_OS_NODE_EXPORTER_PORT` —— node-exporter 的環回指標埠（預設 `9100`）。
- `TOWN_OS_PROMETHEUS_PORT` —— Prometheus 的環回 HTTP API 埠（預設 `9090`）。
- `TOWN_OS_MONITORING_PORT` —— 唯一面向區域網的監控埠（預設 `5308`）。
- `INGRESS_HTTPS_PORT` / `INGRESS_HTTP_PORT` —— ingress 釋出的埠（預設 `443` / `80`）。

## 設定項

| 鍵                      | 預設值                          | 說明                                     |
| ------------------------ | -------------------------------- | ----------------------------------------------- |
| `default_quota`          | `53687091200`                    | 預設卷配額，單位位元組（50 GB）           |
| `max_archive_size`       | `1073741824`                     | 最大上傳大小，單位位元組（1 GB）             |
| `archive_unpack_timeout` | `600`                            | 解包超時，單位秒（10 分鐘）              |
| `locale`                 | `en-US`                          | BCP 47 語言環境程式碼（系統級回退值）       |
| `dns_tld`                | `home`                           | 包 DNS 記錄的預設頂級域|
| `dns_resolution_mode`    | `auto`                           | Rolodex 上游解析方式：`auto`、`recursive` 或 `forward` |
| `dns_local_forwarders`   | `false`                          | 從本機所在網路下發的解析器取轉發器列表，而不是使用公共預設值 |
| `peer_ttl`               | `7200`                           | WireGuard peer 登記有效期，單位秒（2 小時） |
| `gfeh_partition_quota`   | `0`                              | 每個物件儲存分割槽的配額，單位位元組（0 = 不限） |
| `proton_image`           | `quay.io/town/proton:latest`     | Proton 執行器鏡像——**僅在 `proton` 構建標籤下注冊** |

`DefaultSettings`（`src/account/settings.go`）在首次初始化時被播種，且已有的值絕不會被覆蓋。

有幾個鍵是**只讀取、從不播種**的——在有東西寫入之前它們沒有對應的行，
其預設值位於讀取處，作為空字串的回退。不要以為把它們加入 `DefaultSettings`
不會帶來其他影響：被播種的行與運維的選擇無法區分，而對黑名單配置而言，
這正是"從未配置過，別動它"與"被顯式設為空，推送它"之間的差別
（[RBL / DNSBL Blocklists](#rbl--dnsbl-黑名單)）。

| 鍵 | 缺失時的預設值 | 由誰寫入 |
| --- | --- | --- |
| `monitoring_backend`     | `uplot` | `POST /settings/set` |
| `dns_rbl_config` / `dns_dnsbl_config` | 未配置（與"空"不是一回事） | `POST /dns/rbl`、`POST /dns/dnsbl` |
| `dns_excluded_services`  | 空列表（釋出是選擇退出制） | `POST /dns/services/set` |
| `dismissed_upgrades_hash` | 不存在（未忽略任何升級） | `POST /packages/upgrades/dismiss` |

**不存在 `object_storage_enabled`，也不存在服務帳戶密碼。** 物件儲存不是一個可以開啟的功能（[Boot and reconcile](#啟動與-reconcile)），而守護程序也不持有任何 Town OS 憑據（[No service accounts](#沒有服務帳戶)）。升級後的機器上若殘留這兩者中任何一行，都不會被任何東西讀取。

`proton_image` 不在基礎 map 中：`src/account/settings_proton.go` 帶 `//go:build proton`，並在 `init()` 中註冊該預設值，因此不帶該標籤的構建沒有 Proton 設定、沒有 Proton 安裝路徑，並在狀態 ping 中報告 `proton_enabled: false`。之所以採用構建標籤門控的註冊方式而不是匯出一個 `Register` 函式，是為了不讓任何呼叫方對 `DefaultSettings` 產生呼叫順序上的依賴。
