CLAUDE、私が明示的に指示しない限り、このファイルを編集してはならない。

> **本ファイルは [CLAUDE.md](CLAUDE.md) の日本語訳である。英語の原文が正典である。**
> 中国語訳は簡体字 [CLAUDE.zh-CN.md](CLAUDE.zh-CN.md) と繁体字
> [CLAUDE.zh-TW.md](CLAUDE.zh-TW.md)、スペイン語訳は
> [CLAUDE.es-ES.md](CLAUDE.es-ES.md)（スペイン）と
> [CLAUDE.es-MX.md](CLAUDE.es-MX.md)（メキシコ）にある。
> 訳文が原文と食い違う場合は英語の原文が正しく、直すべきは訳文である。コード識別子、
> ファイルパス、コマンド、環境変数、API パス、YAML のキー名は原文のまま訳さない。

**本ファイルはビルド手順とコードスタイルのみを扱う。** システムが実際にどう動くか
——アーキテクチャ、各サブシステムの挙動、API の全体像、起動順序、設定、そしてそれらを
支える不変条件——は [DESIGN.md](DESIGN.md)（日本語訳は
[DESIGN.ja-JP.md](DESIGN.ja-JP.md)）にある。Town OS が**何をするか**を知りたいときは
DESIGN.md を読み、**どうビルドし、どうテストし、どう書くか**を知りたいときは本ファイルを
読むこと。挙動を変える変更をしたときは、DESIGN.md も併せて更新しなければならない。

- **最重要**:
    - **素のコンパイラ／テストツールではなく `make` を使うこと。** `go build`、`go test`、`go vet`、`golangci-lint`、`bun test`、`vitest`、およびそれらに相当するものを直接実行してはならない。必ず make ターゲット経由で実行し、リポジトリのラッパー（クリーンアップトラップ、btrfs のライフサイクル、実行ごとのインスタンス ID）が効くようにすること。
    - **必要なときにいつ実行してもよい make ターゲット**（高速・冪等・リモートへの副作用なし）:
      `make help`、`make lint`、`make check-*`（bun / go / podman / runc / btrfs / libsystemd / golangci-lint）。変更の検証にこれらは自由に使ってよく、事前に尋ねる必要はない。
    - **上記いずれのリストにも無い make ターゲットは、まず尋ねること。**
    - いかなる理由があろうと絶対に force push してはならない。
    - push が必要なときは "origin" にのみ push すること。
    - PUSH する前に、必ず "git pull --rebase" を実行し、マージの問題を解消すること。
    - GPG には一切触れてはならない。単に `git commit` を普通に実行すること。署名に失敗したら、そこで止めてユーザーに尋ねること。gpg-agent を kill してはならず、`--no-gpg-sign` を使ってはならず、GPG を自分で直そうとしてはならない。
    - 署名なしでコミットしてはならない。
    - GPG AGENT を何のためであれいじってはならない。

- パラメータが渡されているときは、それが呼び出し先の関数で実際に使われていることを確認すること

- **同時実行の安全性** —— `make test-full` は、同一リポジトリ内で同時に実行しても衝突しないことが常に保証されなければならない。これより優先されるものは無い。

- golang のプログラムで context.TODO と context.Background を使うべきではない。可能な限り timeout 付き・cancel 付きの context を使い、何かが context を永遠に待ち続けることのないようにすること。

- 行うことすべてにテストを追加すること。**挙動を変える変更にはユニットテストと統合テストの両方が必須である。** ユニットテストはロジックを単体で検証し、統合テストは実物の systemd・btrfs・podman を備えたテストコンテナ内で機能がエンドツーエンドに動くことを検証する。統合テストが書けない場合（純粋な UI 変更など）は、その理由をコミットメッセージに記すこと。

- 結果を使う前に、すべての型アサーションを検査すること

- **コンテナイメージでは ENTRYPOINT ではなく CMD を使うこと** —— すべての Containerfile とインラインの Containerfile 文字列は `ENTRYPOINT` ではなく `CMD` を使わなければならない。これにより `podman run <image> <command>` が `--entrypoint` 無しで既定のコマンドを上書きできる。systemcontroller イメージ、NC イメージ、および動的に生成される Containerfile のすべてに適用される。

- **実行時のコンテナイメージはすべてシステム CA バンドルを同梱しなければならない** —— 最終ステージで Town OS のコードを動かし外向きの HTTPS 呼び出しを行う Containerfile（およびインラインの Containerfile 文字列）はすべて、ベースイメージが既に提供している場合（`caddy`、`oven/bun` など）を除き `ca-certificates` をインストールしなければならない（debian/ubuntu: `apt-get install ca-certificates`、alpine: `apk add ca-certificates`）。CA バンドルが無いと Go の TLS スタックはすべての HTTPS 呼び出しを `x509: certificate signed by unknown authority` で失敗させ、しかも背景ポーラーでの失敗は既定のログレベルでは見えない（`fetchExternalIP` が `ipinfo.io` の応答を黙って捨てていた例を参照）。新しい Containerfile を追加するときは、出荷可能とみなす前に最終イメージに `/etc/ssl/certs/ca-certificates.crt` があることを確認すること。

- **すべての `podman run --name` に `--replace` を付けること** —— リポジトリ内のどこであろうと例外は無い。

- **make パイプライン内の podman はすべて `${SUDO}` 経由で ROOTFUL に実行される** —— `make/lib.sh` の `SUDO="sudo HOME=$HOME"` がそれで、make スクリプト（`build.sh`、`images.sh`、`test.sh`、`dev.sh`、`registry.sh`、`gitea.sh`、`lib.sh`）内の**すべての** `podman` 呼び出しは `${SUDO} podman` でなければならない。rootful と rootless の podman は**別々のイメージストア**を持つ。ベースイメージは root のストア（`/var/lib/containers`）に pull／load され、rootless のユーザーストアは空である。したがって `${SUDO}` の付かない素の `podman` 呼び出しは空の rootless ストアを見に行き、`--pull=never` の下で `image not known` で失敗する —— `${SUDO} podman image exists` はそのイメージが存在すると報告するにもかかわらず（ストアが違う）。make スクリプトに podman コマンドを追加するときは、必ず `${SUDO}` を前置すること。イメージをビルド／ロードする make ターゲットを rootless の podman で実行してはならず、ホスト側のビルドで `CONTAINER_HOST` を rootless のソケットに向けてはならない（`${SUDO} podman` が誤ったストアにルーティングされてしまう）。唯一の例外は可用性の確認（`check.sh`／`preflight.sh` の `command -v podman`）と、`deps.sh` のインストール一覧に現れるパッケージ名としての `podman` である。

- **ビルドにパブリック DNS をハードコードしないこと。podman のビルドは `--network=host` を使う** —— make パイプライン内のすべての `podman build` は `--network=host` で実行し、名前解決がホストのリゾルバ（systemd-resolved）を通るようにする。コンテナネットワークでのビルドではホストのループバックスタブの代わりにパブリックリゾルバが差し込まれ、キャプティブなネットワーク（カフェ、ホテル）は 1.1.1.1／8.8.8.8 への直接のクエリを遮断するため、`bun install`、`apt-get`、`apk add` が無期限に停止する。同じ理由から、テストと dev が使う NC イメージは**ホスト上で**ビルドされ（`nc-image`／`nc-image-dev` ターゲット → `localhost/town-os-networkcontroller:<INSTANCE_ID>`、バイナリは常に systemcontroller と一致するよう production／dev-base イメージから取り出す）、イメージキャッシュ経由でコンテナへロードされる —— 決してコンテナ内で `--dns` を付けてビルドしてはならない。

- **テストスイートの `podman run` コンテナはすべて `--net host` を使う** —— test、UI バックエンド、UI テストランナー、dev、registry、gitea の各コンテナはすべてホストネットワークで動く。registry と gitea は `-p` マッピングではなく `REGISTRY_HTTP_ADDR`／`GITEA__server__HTTP_PORT` を通じてインスタンスごとのランダムポートを直接バインドし、gitea の SSH は無効化されている（`DISABLE_SSH=true`）ため、ホストのポート 22 をバインドしようとするものは無い。理由: ブリッジネットワークのコンテナはキャプティブなネットワークで DNS が壊れ、registry（Docker Hub へのプルスルーのフォールバック）も gitea（リポジトリのマイグレーション）も自前で外向きの呼び出しを行うためである。意図的な唯一の例外は `preflight-dev` の nginx コンテナで、その `-p` マッピングはまさにブリッジネットワークが機能することを検証するために存在する。

- **イメージタグはアーキテクチャごとに分割される** —— push されるタグはすべて、生の `uname -m` の形式のアーキテクチャ接尾辞を持つ（`<arch>` は `x86_64` または `aarch64`）。このタグ接尾辞は OCI のプラットフォーム名 `amd64`／`arm64` とは意図的に区別されている。Go は `archTag()` で `runtime.GOARCH` を接尾辞に対応付け、make は `HOST_ARCH`（`x86_64`／`aarch64` に正規化）を使い、シェルは `make/lib.sh` の `host_arch_tag` を使う。素の `host_arch`／`runtime.GOARCH` の値は `amd64`／`arm64` のままである。podman が `podman pull --platform linux/<arch>` と `.Architecture` の比較にそれを必要とするからで、`x86_64`／`aarch64` を `--platform` に渡してはならない。`push-rc` は `rc.<date>-<arch>`／`rc.latest-<arch>` を push し、`push-release` は `release.<date>-<arch>`／`latest-<arch>` を push する —— 常に push を実行しているホストのネイティブなアーキテクチャである。素の名前（`rc.latest`、`latest`、および日付タグ）は、`ARCHES`（`x86_64 aarch64`）のすべてのアーキテクチャが push を終えたのちに `manifest-rc`／`manifest-release` が組み立てるマルチアーキテクチャのマニフェストリストとして**のみ**存在する。素の名前を単一アーキテクチャのタグとして push してはならない。タグが焼き込まれていない場合の実行時のフォールバックは `main.go` の `defaultVersionTag()`（`rc.latest-<arch>`、`archTag()` で対応付けた GOARCH）である。理由: 一方のホストから push された素の単一アーキテクチャのタグは、もう一方のアーキテクチャでは `exec format error` で失敗する（さらに悪いことに、`Restart=always` の下でクラッシュループしながらステータスのポーリングのテストだけは通ってしまうこともある）。

- **素の便宜的なタグをテストに使ってはならない** —— どのテスト、テストハーネス、dev コンテナ、フィクスチャも、*素の*（アーキテクチャ接尾辞の無い）`quay.io/town/*:rc.latest` や `:latest` のイメージを参照してはならない（存在しないかもしれず、古いマルチアーキテクチャのマニフェストかもしれない）。アーキテクチャ接尾辞付きの形式は許可されており、そちらが既定である。テストが使うのは、rolodex 用のホストのアーキテクチャ別 rc タグ（`rc.latest-<arch>`、すなわち `rc.latest-x86_64`／`rc.latest-aarch64`）、ローカルにビルドした UI イメージ（`make ui-image` → `localhost/town-os-ui:<INSTANCE_ID>`）、ローカルにビルドした NC イメージ（`make nc-image`）、およびイメージが pull も実行もされないモック化されたユニットテストでの中立な偽のタグ（`:testtag` など）である。

- **テストと dev は `localhost/` イメージをビルドし、push ターゲットは常に新しいリリースイメージをビルドする** —— `make/build.sh` の `*-local` の分岐はテストと dev のハーネス向けに `localhost/town-os-*:$(INSTANCE_ID)` を生成し、`release-*` の分岐は `quay.io/town/*` を生成する。**どの push ターゲットも `localhost/*` のイメージをビルドしてはならず、そこからタグを付けても、それに依存してもならない**。そしてすべての push ターゲットは、ローカルのストアにたまたま存在するものにタグを付け直すのではなく、*新しい*リリースイメージをビルドしなければならない。これはすべてのイメージに例外なく適用される。理由: ローカルのテストイメージにタグを付け直すことは、ハーネス向けにビルドされた成果物 —— インスタンスごとのタグ、`--pull=never` のベース、ホストのアーキテクチャのみ、クロスビルドは一切なし —— をリリースの名前で出荷することに等しい。新しくチェックアウトした環境ではこれは失敗するが、開発者のマシンでは成功して誤った成果物を出荷してしまう。そちらのほうが悪い。

- **内容がリポジトリの外から来るローカルイメージには、明示的なキャッシュ破棄が必要である** —— ほとんどの `*-local` イメージはリポジトリのソースからビルドされるため、ソースの変更がレイヤキャッシュを無効化し、対応するリリースイメージから乖離することはあり得ない。しかし内容をビルド時に取得するイメージ（`Containerfile.gfeh` はバージョン指定のない `cargo install gfehd` を実行する）は、バイト単位で同一の `RUN` 行の後ろにあるため、そのレイヤは恒久的なキャッシュヒットとなり、そのマシンで最初にビルドされた時点のものに凍結される。リリースのビルドは `--no-cache` を渡し、ローカルのフィクスチャは日単位の粒度の build-arg（`GFEH_CACHE_DATE`）を渡すことで、毎回の実行で再コンパイルすることなく日ごとに更新される。これが無ければ、統合テストスイートは Town OS がもはや実行できないデーモンを、何も告げないままテストし続けることになる。

- **フェイルファスト** —— make のサブタスク、または make のサブタスクが起動したスクリプトが失敗したら、直ちに停止すること。次のフェーズに進んではならない。

- **終了コードを握り潰さないこと** —— make／テストのコマンドを実行するスクリプトは、決して終了コードを握り潰してはならない。`|| rc=$?` も、テスト呼び出しへの `|| true` も禁止である。`set -e` に仕事をさせること。クリーンアップのコマンド（podman rm、rm -f）は対象外。

- **テストで共有リソースをハードコードしないこと** —— テストの一時ファイル、ソケット、ディレクトリ、ポートはすべて実行ごとに一意なパス（`t.TempDir()`、`filepath.Join`、`findFreePort` など）を使わなければならない。`/tmp/foo.sock` のような固定パスを使ってはならない。

- **許可された make ターゲットは尋ねずに実行してよいが、上記の「許可が要る」一覧にあるそれ以外のものには明示的な了承が要る。** `go`、`go test`、`go vet`、`golangci-lint`、`bun test`、`vitest` などを直接呼び出してはならない —— 必ず make を経由すること。

- **テストコードとビルドコードのいずれも tmpfs を使ってはならない** —— make ターゲット、make スクリプト、テストハーネスが書き出すファイルは、どれ一つとして tmpfs（RAM 上）のファイルシステムに置いてはならない。これは交渉の余地なく絶対である。btrfs のループバック用バッキングイメージ、コンテナ／ボリュームのデータ、アーカイブ、ダウンロード、ポートファイル、追跡用ファイル、その他あらゆる実行ごとの成果物に適用される。理由は美観ではなく致命的である。テスト用の btrfs ファイルシステムは 50G のループバックファイルであり、tmpfs を裏に持つ loop デバイスはメモリ圧の下で**ホストのカーネルをデッドロックさせる** —— tmpfs のページは swap にしか回収できないのに、loop のライトバック経路はそれを吐き出すためにメモリを確保する必要があるので、いったん tmpfs が RAM を埋めるとマシンは完全に固まり、ファームウェア／ウォッチドッグが再起動をかける（Manjaro で観測。systemd が `/tmp` を RAM の 50% サイズの tmpfs としてマウントし、swap はほぼゼロという構成）。よくある開発用ディストリ（Arch／Manjaro／Fedora）では `/tmp` は tmpfs なので、**`/tmp` がディスク上にあると仮定してはならない**。バッキングファイル、loop デバイス、その他まとまった書き込み先を作るテスト／ビルドのコードは、まずそのディレクトリが実際にディスク上のファイルシステムであることを解決しなければならず（例: `findmnt -no FSTYPE <dir>` が `tmpfs`／`ramfs` でないことを確認する、あるいは `/var/tmp` のようにディスク上と分かっているパスにデータを置く）、それができない場合は大きな音を立てて失敗しなければならない。make スクリプトに新しいパスを追加するときは、書き込む前にそれが tmpfs でないことを確認すること。

- **一時的な状態の置き場所** —— 実行ごとの帳簿（ポートファイル、`.disk`／`.loop`／`.mount` の追跡用ファイル、dev のメタデータ）はインスタンスごとに `/tmp/town-os-$(INSTANCE_ID)/` の下にスコープされる。しかし*データを担う*成果物 —— とりわけ btrfs のループバック用バッキングイメージ —— は、決して tmpfs ではなくディスク上のパスに置かなければならない（上記の tmpfs 禁止の規則を参照）。`/tmp` が tmpfs でないことをまず確認せずに、ループバック／ディスクイメージ、コンテナのボリュームデータ、大きなダウンロードを `/tmp` に置いてはならない。

- **コミットと push は指示されたときにのみ行うこと** —— ユーザーが明示的に求めない限り、`git commit` や `git push` を実行してはならない。force push（`--force` や `--force-with-lease`）は決してしてはならない。

- systemcontroller は、サービスが実際に終了させられる場合を除いて os.Exit を呼んではならない。致命的なエラーは fatal のログ出力で対処すること

- すべてのエラーを検査すること。コードのいかなる箇所でも、いかなる理由であれ、エラー検査をアンダースコアで捨てたり省いたりしてはならない

- **カンマ ok 式の `ok` は必ず検査すること。** `value, ok` の組を返す式 —— 型アサーション（`v, ok := x.(T)`）、マップの添字（`v, ok := m[k]`）、チャネルの受信（`v, ok := <-ch`）—— はすべて、`value` を使う前に `ok` を検査しなければならない。`_` で捨ててはならず、アサーション／参照が成功したと仮定してもならない。単一値の型アサーション `v := x.(T)`（不一致で panic する）より、カンマ ok の形式を優先すること。`v, ok := x.(T)` を使い、`!ok` を明示的に扱うこと。これはテストコードにも適用される。（型で綺麗に分岐する switch —— `switch v := x.(type)` —— と、意図的なメンバーシップ確認の `_ = m[k]` だけが例外である。）

- 可能な限り、if 文の中でインラインのエラー構文を使うこと（例: `if err := foo(); err != nil {`）

- **テスト用のサービスはランダムな高位ポートを使う** —— ネットワークサービス（DNS、HTTP、gRPC など）を起動する統合テストは、53 や 80 のようなよく知られたポートではなく、`findFreePort` によるランダムな高位ポートにバインドしなければならない。これにより複数のテスト実行が同時に走っても衝突しない。

- **テストの DNS は決してホストに触れてはならない。** どのテストも、テストハーネスも、make のテストターゲットが起動するものも、ホストの名前解決を変えたり、ホストの DNS ポートを占有したりしてはならない。具体的には、テスト実行は決して以下をしてはならない:
    - `/etc/resolv.conf` を書き換えること（それは `make/dev.sh` の `redirect_host_dns` であり、`make dev` だけのものである）、
    - `/etc/systemd/resolved.conf.d/town-os.conf` を書くこと、あるいは他の方法で `rolodex.ConfigureResolvedRouting` を呼ぶこと、
    - `systemd-resolved` にシグナルを送る、または再起動すること（`pkill -HUP systemd-resolved`）、
    - ホストのネットワーク名前空間で **`127.0.0.2:53`**、あるいは任意の `:53` をバインドすること。

  テストコンテナは意図的に `--net host` で動く（ブリッジネットワークの DNS はキャプティブなネットワークで壊れるため）ので、システムサービスがバインドするポートはすべて**ホスト**の名前空間に着地する。だからこそ `TOWN_OS_DNS_PORT` は実行ごとに `$(STATE_DIR)/.dns-port` へ割り当てられ、`system_port_env`（`make/lib.sh`）によって渡される。そして `main.go` は `dnsPortIsDefault()` が偽であるときは常に resolved のルーティング設定を飛ばす —— ドメインごとの resolved のサーバーアドレスはポートを持たないので、移動させられた rolodex に対して resolved を `DNSLoopback` へ向けると、その TLD へのすべてのクエリがブラックホールに落ちるからである。

  `127.0.0.2:53` がバインドされたまま残るテスト実行や、ホスト上の `town-os.conf` のドロップインは、**不安定なテストではなくハーネスのバグ**として扱うこと。それはポートの上書きがコンテナに届かず、rolodex が既定値にフォールバックしたことを意味する。`ss -lnup | grep 127.0.0.2` と `ls /etc/systemd/resolved.conf.d/` で確認すること —— ホストの `:53` を listen してよいのはそのマシン自身のリゾルバだけであり、我々のものであってはならない。`make dev` だけが唯一の例外で、これは実機を模すことが目的であり、オペレーターが明示的に選ぶものである。

- **リモートの Gitea や GitHub に push するテストを決して書いてはならない。**

- **私が何かをせよと言ったときは、議論しないこと。**

- **テストの git 操作は、どちらでもよい場合はリモートよりローカルのリポジトリを優先すべきである** —— 例えば populate-repos は、ローカルの兄弟ディレクトリが存在するならば GitHub から取得するのではなくそこから clone すべきである。

- テストの警告のうち直せるものは、出てきた端から直すこと

- パッケージ変数は常にコンパイルの工程の一部として翻訳されるべきである。固定のパッケージ変数は常にテストされるべきである。

- すべてのファイルが api ごとに整理されていることを確認すること。サブセクション名によって階層的にスコープされるべきである。行数の目安は 500 行程度とする。


## 性能に関する規約

- **文字列の構築には `strings.Builder` を使うこと** —— `string(append([]byte(s), c))` で一文字ずつ文字列を組み立ててはならない。`strings.Builder` と `WriteByte`／`WriteString` を使い、O(n²) ではなく O(n) の確保に抑えること。`src/packages/packages_compile.go`（`applyTemplate`、`applyTemplates`）を参照。

- **サイズが分かっているときはスライスを事前確保すること** —— 結果のサイズや上限が分かっているとき（ページネーションの `limit` など）は `make([]T, 0, capacity)` を使うこと。ホットパスで `var items []T` のあとに際限なく `append` するのは避けること。

- **`COUNT(*) OVER()` による単一クエリのページネーション** —— ページ分割された一覧のエンドポイントは、別途 `COUNT(*)` のクエリを走らせるのではなく、SELECT の列リストで SQLite のウィンドウ関数 `COUNT(*) OVER()` を使わなければならない。合計値は各行と一緒にスキャンすること。

- **WHERE 句で使う列にはインデックスを張ること** —— `WHERE` のフィルタで使う SQLite の列（とりわけ `created_at`、`success`、`account`）にはすべて適切なインデックスが必要である。複合インデックスはよくあるフィルタの組み合わせに合わせること（例: `CountRecentErrors` のための `(success, created_at)`）。

- **高価な繰り返しの参照はキャッシュすること** —— `RepositoryRoot.LoadPackages()` の結果はリポジトリ名ごとに `sync.Map` にキャッシュされ、`ForceRefresh()` で無効化される。呼び出し側は `LoadPackages()` を直接ではなく `cachedLoadPackages()` を使わなければならない。同様に `GetInternalIP()` は、リクエストごとに `net.InterfaceAddrs()` を呼ぶのではなく結果を `atomic.Value` にキャッシュする。

- **全走査より直接の参照を** —— 単一のパッケージを調べるときは `ListInstalled()` と線形探索ではなく `GetInstalledVersion(repo, name)`（`installed/<repo>/<name>/` を直接読む）を使うこと。

- **独立した操作は I/O を並列に** —— `refreshSystemServices` におけるコンテナイメージの pull は、逐次ループではなくセマフォ（同時実行は最大 3）付きの goroutine を使う。`sync.WaitGroup` とチャネルによるセマフォを使うこと。`errgroup` への依存を追加してはならない。

- **背景の goroutine にはサーバースコープの context を** —— 背景の goroutine（pages の git clone、イメージの展開）は `context.Background()` ではなくサーバースコープの context（`s.ctx`）を使い、グレースフルシャットダウンを尊重しなければならない。HTTP リクエストの context を使ってはならない（その操作はリクエストより長く生きなければならない）。

- **reconcile では依存関係をまとめて読み込むこと** —— すべてのパッケージの依存関係のレコードは、reconcile のループ内でパッケージごとに読み込むのではなく、ループの前にマップへ事前に読み込む。


## 開発の前提条件

Town OS をソースからビルドするには以下が必要である:

- **Go 1.25+** -- システムコントローラ用に CGO を有効にすること（libsystemd とリンクする）。
- **libsystemd-dev** -- systemd の journal と dbus のバインディング用の C 開発ヘッダ。`go-systemd/v22` の依存関係が要求する。
- **Bun** -- UI のビルドとテストのための JavaScript ランタイム。
- **Podman** -- rootful（`sudo`）。コンテナ操作に使う。
- **btrfs-progs** -- テストと dev 用の btrfs ボリュームを作る `mkfs.btrfs` を提供する。
- **golangci-lint** -- Go の lint 用。
- **QEMU** -- VM パッケージの実行に `qemu-system-x86_64`、VM のディスクイメージを raw 形式へ変換するのに `qemu-img`。

### ブートストラップ

`make deps` は、まっさらな Arch もしくは Ubuntu／Debian のマシンに、ホスト側の依存関係
（Go、podman、runc、btrfs-progs、libsystemd のヘッダ、golangci-lint、bun、qemu、
ビルドツール）をすべてインストールする。`make/deps.sh` に実装されており、
`/etc/os-release` からディストリを判別し、再実行しても安全である。

`make help`（既定のターゲット）は、ユーザー向けの make ターゲットをグループ分けして
一覧表示する。`make/help.sh` に実装されている。`make/include.mk` でターゲットを追加
または改名したときは、両方のスクリプトを同期させておくこと。

### プリフライトチェック

Makefile には、テストの実行や dev サーバーの起動の前に開発環境を検証する `preflight-dev` ターゲットがある。検査する内容は:

- **podman** -- `podman` コマンドが PATH にあることを確認する。
- **btrfs-progs** -- `mkfs.btrfs` コマンドが PATH にあることを確認する。
- **リポジトリの資格情報** -- 環境変数 `TOWN_OS_REPO_USERNAME` と `TOWN_OS_REPO_PASSWORD` が設定されていることを確認する。
- **ブリッジネットワーク** -- ポートをバインドしたテスト用の nginx コンテナを起動し、podman の `-p` フラグが正しく動くことを確認する。

各検査は失敗時に説明的なエラーメッセージを表示し、非ゼロのステータスで終了する。「All preflight checks passed.」というメッセージが表示されるには、すべての検査が通らなければならない。

### Ubuntu／Debian でのインストール

Ubuntu や Debian のシステムでは、以下でシステムの依存関係をインストールする:

```
sudo apt-get install -y libsystemd-dev btrfs-progs podman runc qemu-system-x86 qemu-utils
```

Go、Bun、golangci-lint は個別にインストールしなければならない（それぞれの上流のドキュメントを参照）。

## コードの品質

### エラー処理

Go のエラーの戻り値はすべて明示的に検査しなければならない。`errcheck` リンターはプロジェクト全体で有効であり、エラーを捨てるためにブランク識別子（`_ =`）を使ってはならない。

本番コードでは、defer された関数内のクリーンアップのエラーは、名前付きの戻り値を通じて `errors.Join()` で主たるエラーと結合する（例: `defer func() { err = errors.Join(err, f.Close()) }()`）。重要でないベストエフォートの操作は、エラーを捨てるのではなくログに出す。

テストコードでは、クリーンアップのエラーは深刻度に応じて `t.Errorf` または `t.Logf` で報告するか、`//nolint:errcheck` の注記と理由のコメントを付けて明示的に抑制する。

すべての `//nolint` 指示子には理由のコメントが必要である（`nolintlint` が強制する）。

## 統合テスト

### ローカルの Docker Registry

統合テストは、Docker Hub のレート制限を避け再現性を担保するため、ローカルの `registry:2` コンテナに対して実行される。手順は以下のとおり:

1. **イメージの探索** -- `discover-images` ツールが、すべてのテスト用パッケージリポジトリを走査して `docker.io` のイメージ参照（メインのイメージとアーカイブのイメージの両方）を集める。結果は重複を除いて `.cache/.registry-images` に書き出される。
2. **Registry の起動** -- `registry:2` のコンテナがランダムなポートで起動する。
3. **イメージのミラーリング** -- 見つかった各イメージを Docker Hub から pull し、ローカルの registry のアドレスで付け直し、ローカルの registry へ push する（localhost に対しては TLS の検証を無効化する）。
4. **Registry の設定** -- `docker.io` からの pull をローカルのミラーへ向け直す `registries.conf` ファイルが生成される。これはテストコンテナの `/etc/containers/registries.conf.d/` にマウントされる。
5. **透過的な動作** -- コードの変更は不要である。podman が自動的にローカルのミラーを使う。ミラーはキャッシュに無いイメージについては Docker Hub にフォールバックする。

作業ディレクトリごとに（`INSTANCE_ID` によって）専用の registry インスタンスが割り当てられるため、同時に走るテストは衝突しない。

### ローカルの Gitea サーバー

統合テストは、git 操作における GitHub のレート制限を避けるためローカルの Gitea インスタンスを使う。手順はローカルの Docker registry のパターンを踏襲する:

1. **サーバーの起動** -- `gitea/gitea:latest` のコンテナが、インストールを事前にロックした状態でランダムなポートに起動する。管理者ユーザー（`town-os`）が自動的に作成される。
2. **リポジトリのマイグレーション** -- `populate-repos` ツールが、Gitea のマイグレーション API を使ってテスト用のパッケージリポジトリ（`test-packages-core`、`test-packages-extras`）を GitHub からローカルの Gitea インスタンスへ移す。マイグレーションは冪等である。既存の空でないリポジトリは飛ばされ、失敗したマイグレーションによる空のリポジトリは削除して再試行される。
3. **透過的な動作** -- テストは環境変数（`TOWN_OS_TEST_REPO_CORE_URL`、`TOWN_OS_TEST_REPO_EXTRAS_URL`）でローカルの Gitea の URL を受け取る。これらが設定されていない場合、テストは既定の GitHub の URL にフォールバックする。

作業ディレクトリごとに（`INSTANCE_ID` によって）専用の Gitea インスタンスが割り当てられるため、同時に走るテストは衝突しない。イメージの探索は、利用できる場合はローカルの Gitea のリポジトリから読む。

### コンテナのクリーンアップ

`test-full` ターゲットは統合テストの完了後に `clean-integration` と `clean-btrfs` を実行し、テストが失敗した場合でもすべてのテストコンテナ（test、registry、gitea、ui-backend、ui-integration）と btrfs のループバックのマウントが確実に片付くようにする。`clean-dev` ターゲットはキャッシュを掃除する前に `town-os-dev` のコンテナをすべて削除する。`clean-containers` ターゲットは、インスタンスや作業ディレクトリを問わず Town OS のコンテナ（`town-os-*` と `preflight-test-*` のパターンに一致するもの）をすべて削除する。`clean-integration` ターゲットは冪等なクリーンアップのため、エラーを許容するコンテナ削除を使う。`clean-all` ターゲットは、インスタンスをまたいだ網羅的なクリーンアップのため `clean-containers` を使う。監視用のイメージは、イメージキャッシュから統合テストのコンテナへ事前にロードされる。

### Btrfs のループバックのクリーンアップ

テストのターゲット（`test-integration`、`test-ui-integration`、`test-full`）はシェルの EXIT トラップを使い、テストの成功・失敗・シグナルによる中断のいずれであっても btrfs のクリーンアップが確実に走るようにする。レシピは `make/` の下のシェルスクリプトに整理されている。btrfs ボリュームの作成は EXIT トラップを登録したあとテストスクリプト内で行われるため、作成やその後の手順が失敗しても loop デバイスが漏れることはない。

`clean-btrfs` ターゲットはベストエフォートのクリーンアップを行う（`set -e` は使わない）。btrfs ファイルシステムをアンマウントし、ディスクイメージのファイルについて `losetup -j` で見つけた loop デバイスを切り離し、状態の追跡用ファイル（`town-os.disk`、`town-os.loop`、`town-os.mount`）を削除する。安全網として、有効なすべての loop デバイス（`losetup -a`）を走査し、カレントディレクトリの btrfs イメージファイルを裏に持つものを探して、追跡用ファイルが失われている場合でも孤児となったデバイスを切り離す。

### テストファイルの構成

統合テストのファイルはコンポーネントと下位機能ごとに整理されている。各ファイルは特定の領域に集中する。btrfs の操作、git の操作、リポジトリの管理、そしてシステムコントローラの各サブシステムである。システムコントローラのテストはさらに、アーカイブ、ブートストラップ、ファイルシステム、インストール（モックと実物の systemd）、複数リポジトリのシナリオ、ネットワーク、パッケージ、pages、reconcile、リポジトリ、設定、systemd のユニット、ボリュームごとに別々のファイルへ分割されている。共通のテストの初期化とヘルパー関数は専用のヘルパーファイルに集約されている。

### テスト環境

統合テストは、systemd、btrfs、そしてテスト用のバイナリ一式を備えた特権付きの podman コンテナ内で実行される。コンテナにはパッケージのコンテナを動かすための podman と runc が含まれる。テストは実物の systemd のユニットのライフサイクル、btrfs のボリューム管理、コンテナの操作を行使する。
