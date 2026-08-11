CLAUDE, NO TIENES PERMISO PARA EDITAR ESTE ARCHIVO SALVO QUE YO TE LO DIGA.

> **Este archivo es la traducción al español (España) de [CLAUDE.md](CLAUDE.md).
> El original en inglés es el autoritativo.** Cuando ambos discrepen, manda el
> inglés y lo que hay que corregir es la traducción. Los identificadores de
> código, rutas de archivo, comandos, variables de entorno, rutas de la API y
> nombres de claves YAML se conservan sin traducir.
>
> Otras traducciones: español de México ([CLAUDE.es-MX.md](CLAUDE.es-MX.md)) y
> chino, en escritura simplificada ([CLAUDE.zh-CN.md](CLAUDE.zh-CN.md)) y
> tradicional ([CLAUDE.zh-TW.md](CLAUDE.zh-TW.md)).

**Este archivo contiene solo instrucciones de compilación y estilo de código.**
Cómo funciona realmente el sistema — arquitectura, comportamiento de los
subsistemas, superficie de la API, orden de arranque, ajustes y los invariantes
que lo sostienen todo — está en [DESIGN.md](DESIGN.md) (traducción al español
de España en [DESIGN.es-ES.md](DESIGN.es-ES.md)). Lee DESIGN.md cuando necesites
saber **qué hace** Town OS; lee este archivo cuando necesites saber **cómo
compilarlo, cómo probarlo y cómo escribir código en él**. Cuando un cambio
altera el comportamiento, DESIGN.md es el archivo que hay que actualizar junto
con él.

- **LO MÁS IMPORTANTE**:
    - **Usa `make`, no las herramientas de compilación/prueba en crudo.** Nunca ejecutes `go build`, `go test`, `go vet`, `golangci-lint`, `bun test`, `vitest` ni ningún equivalente directamente. Pasa siempre por un objetivo de make para que se apliquen las envolturas del repositorio (trampas de limpieza, ciclo de vida de btrfs, identificadores de instancia por ejecución).
    - **Objetivos de make que puedes ejecutar siempre que los necesites** (rápidos, idempotentes, sin efectos secundarios remotos):
      `make help`, `make lint`, `make check-*` (bun / go / podman / runc / btrfs / libsystemd / golangci-lint). Úsalos libremente para validar cambios — no necesitas preguntar antes.
    - **Si un objetivo de make no está en ninguna de las dos listas anteriores, pregunta primero.**
    - NO HAGAS FORCE PUSH POR NINGÚN MOTIVO, JAMÁS.
    - cuando tengas que hacer push, hazlo solo a "origin".
    - ANTES DE HACER PUSH, EJECUTA SIEMPRE "git pull --rebase" y resuelve cualquier problema de fusión.
    - NO TOQUES GPG DE NINGUNA MANERA. Haz `git commit` con normalidad. Si la firma falla, detente y pregunta al usuario. Nunca mates a gpg-agent, nunca uses --no-gpg-sign, nunca intentes arreglar GPG por tu cuenta.
    - NO HAGAS COMMITS SIN FIRMAR.
    - NO ENREDES CON EL AGENTE GPG PARA NADA

- cuando se suministren parámetros, asegúrate de que se usen en la función que llama

- **Seguridad en concurrencia** — `make test-full` debe poder ejecutarse siempre de forma simultánea en el mismo repositorio sin conflictos. Nada importa más que esto.

- context.TODO y context.Background no deben usarse en programas de Go. Siempre que sea posible, usa contextos con tiempo límite y cancelación para asegurarte de que nada quede esperando indefinidamente sobre un contexto.

- Añade pruebas para todo lo que hagas. **Todo cambio de comportamiento debe tener pruebas unitarias y pruebas de integración.** Las pruebas unitarias verifican la lógica de forma aislada; las de integración verifican que la funcionalidad funciona de extremo a extremo dentro del contenedor de pruebas con systemd, btrfs y podman reales. Si no puede escribirse una prueba de integración (p. ej., un cambio puramente de interfaz), documenta el motivo en el mensaje del commit.

- comprueba todas las aserciones de tipo antes de usar el resultado

- **Usa CMD en lugar de ENTRYPOINT en las imágenes de contenedor** — todos los Containerfile y las cadenas Containerfile en línea deben usar `CMD` en lugar de `ENTRYPOINT`. Esto permite que `podman run <imagen> <comando>` anule el comando predeterminado sin `--entrypoint`. Aplica a la imagen del systemcontroller, a la imagen del NC y a cualquier Containerfile generado dinámicamente.

- **Toda imagen de contenedor de runtime debe incluir un paquete de CA del sistema** — cualquier Containerfile (o cadena Containerfile en línea) cuya etapa final ejecute código de Town OS que haga llamadas HTTPS salientes debe instalar `ca-certificates` (debian/ubuntu: `apt-get install ca-certificates`; alpine: `apk add ca-certificates`) salvo que la imagen base ya lo proporcione (p. ej. `caddy`, `oven/bun`). Sin un paquete de CA, la pila TLS de Go falla en todas las llamadas HTTPS con `x509: certificate signed by unknown authority`, y los fallos en los sondeos en segundo plano son invisibles con el nivel de registro predeterminado (véase `fetchExternalIP` descartando en silencio las respuestas de `ipinfo.io`). Al añadir un Containerfile nuevo, verifica que la imagen final tenga `/etc/ssl/certs/ca-certificates.crt` antes de considerarla apta para distribuir.

- **`--replace` en todos los `podman run --name`** — sin excepciones, en ninguna parte del repositorio.

- **Todo lo relativo a podman en la cadena de make se ejecuta ROOTFUL mediante `${SUDO}`** — `SUDO="sudo HOME=$HOME"` en `make/lib.sh`, y **todas** las invocaciones de `podman` en los scripts de make (`build.sh`, `images.sh`, `test.sh`, `dev.sh`, `registry.sh`, `gitea.sh`, `lib.sh`) DEBEN ser `${SUDO} podman`. Podman rootful y rootless tienen **almacenes de imágenes separados**: las imágenes base se descargan/cargan en el almacén de root (`/var/lib/containers`) y el almacén del usuario rootless está vacío. Por tanto, una llamada a `podman` a secas (sin `${SUDO}`) va al almacén rootless vacío y falla con `image not known` bajo `--pull=never` — aun cuando `${SUDO} podman image exists` informe de que la imagen está presente (almacén distinto). Al añadir cualquier comando de podman a un script de make, prefíjalo siempre con `${SUDO}`; nunca ejecutes objetivos de `make` que construyan/carguen imágenes bajo un podman rootless, y no establezcas `CONTAINER_HOST` a un socket rootless para compilaciones del lado del host (encaminaría `${SUDO} podman` al almacén equivocado). Las únicas excepciones son los sondeos de disponibilidad (`command -v podman` en `check.sh`/`preflight.sh`) y el nombre literal del paquete `podman` en las listas de instalación de `deps.sh`.

- **Sin DNS público fijado en las compilaciones; las compilaciones de podman usan `--network=host`** — todo `podman build` de la cadena de make se ejecuta con `--network=host` para que la resolución de nombres pase por el resolutor del host (systemd-resolved). En las compilaciones con red de contenedor se sustituye el stub de loopback del host por un resolutor público, y las redes cautivas (cafeterías, hoteles) bloquean las consultas directas a 1.1.1.1/8.8.8.8 — dejando colgados indefinidamente `bun install`, `apt-get` y `apk add`. Por la misma razón, la imagen del NC que usan las pruebas y el entorno de desarrollo se construye **en el host** (objetivos `nc-image` / `nc-image-dev` → `localhost/town-os-networkcontroller:<INSTANCE_ID>`, con el binario extraído de la imagen base de producción/desarrollo para que siempre coincida con el systemcontroller) y se carga en los contenedores a través de la caché de imágenes — nunca se construye dentro de ellos con `--dns`.

- **Todos los contenedores `podman run` de la suite de pruebas usan `--net host`** — los contenedores de pruebas, backend de la interfaz, runner de pruebas de la interfaz, desarrollo, registro y gitea se ejecutan todos con red del host. El registro y gitea enlazan directamente su puerto aleatorio por instancia mediante `REGISTRY_HTTP_ADDR` / `GITEA__server__HTTP_PORT` en lugar de mapeos `-p`, y el SSH de gitea está deshabilitado (`DISABLE_SSH=true`) para que nada intente enlazar el puerto 22 del host. Motivo: los contenedores en red bridge se quedan con el DNS roto en redes cautivas, y tanto el registro (respaldo pull-through de Docker Hub) como gitea (migración de repositorios) hacen sus propias llamadas salientes. La única excepción deliberada es el contenedor nginx de `preflight-dev`, cuyo mapeo `-p` existe precisamente para verificar que la red bridge funciona.

- **Las etiquetas de imagen se particionan por arquitectura** — toda etiqueta subida lleva un sufijo de arquitectura en la forma cruda de `uname -m` (`<arch>` es `x86_64` o `aarch64`). Este sufijo de etiqueta es deliberadamente distinto del nombre de plataforma OCI `amd64`/`arm64`: Go mapea `runtime.GOARCH` al sufijo mediante `archTag()`, make usa `HOST_ARCH` (normalizado a `x86_64`/`aarch64`) y el shell usa `host_arch_tag` en `make/lib.sh`. Los valores simples `host_arch` / `runtime.GOARCH` siguen siendo `amd64`/`arm64` porque podman los necesita para `podman pull --platform linux/<arch>` y para las comparaciones de `.Architecture` — nunca pases `x86_64`/`aarch64` a `--platform`. `push-rc` sube `rc.<fecha>-<arch>` / `rc.latest-<arch>`; `push-release` sube `release.<fecha>-<arch>` / `latest-<arch>` — siempre la arquitectura nativa del host que ejecuta el push. Los nombres simples (`rc.latest`, `latest` y las etiquetas con fecha) existen SOLO como listas de manifiestos multiarquitectura ensambladas por `manifest-rc` / `manifest-release` después de que todas las arquitecturas de `ARCHES` (`x86_64 aarch64`) hayan subido; nunca subas un nombre simple como etiqueta de una sola arquitectura. El respaldo en tiempo de ejecución cuando no se incrustó ninguna etiqueta es `defaultVersionTag()` en `main.go` (`rc.latest-<arch>`, con el GOARCH mapeado por `archTag()`). Motivo: una etiqueta simple de una sola arquitectura subida desde un host falla en la otra arquitectura con `exec format error` (o peor, pasa espuriamente las pruebas de sondeo de estado mientras entra en bucle de caídas bajo `Restart=always`).

- **Las etiquetas simples de conveniencia NUNCA deben usarse para pruebas** — ninguna prueba, arnés de pruebas, contenedor de desarrollo o fixture puede referenciar una imagen *simple* (sin sufijo de arquitectura) `quay.io/town/*:rc.latest` o `:latest` (puede que no existan o que sean manifiestos multiarquitectura obsoletos). Las formas con sufijo por arquitectura SÍ están permitidas y son las predeterminadas. Las pruebas usan: la etiqueta rc por arquitectura del host para rolodex (`rc.latest-<arch>`, es decir `rc.latest-x86_64` / `rc.latest-aarch64`), una imagen de interfaz construida localmente (`make ui-image` → `localhost/town-os-ui:<INSTANCE_ID>`), una imagen de NC construida localmente (`make nc-image`) y etiquetas falsas neutras (p. ej. `:testtag`) en pruebas unitarias con mocks donde la imagen nunca se descarga ni se ejecuta.

- **Falla rápido** — si falla cualquier subtarea de make o cualquier script lanzado por una subtarea de make, detente de inmediato. No continúes con la siguiente fase.

- **Nunca te tragues los códigos de salida** — los scripts que ejecutan comandos de make/pruebas nunca deben tragarse los códigos de salida. Nada de `|| rc=$?`, nada de `|| true` en invocaciones de pruebas. Deja que `set -e` haga su trabajo. Los comandos de limpieza (podman rm, rm -f) están exentos.

- **Sin recursos compartidos fijos en las pruebas** — todos los archivos temporales, sockets, directorios y puertos de prueba deben usar rutas únicas por ejecución (`t.TempDir()`, `filepath.Join`, `findFreePort`, etc.). Nunca uses rutas fijas como `/tmp/foo.sock`.

- **Ejecutar los objetivos de make permitidos está bien sin preguntar; cualquier otra cosa de la lista de "requiere permiso" anterior necesita un OK explícito.** Nunca invoques `go`, `go test`, `go vet`, `golangci-lint`, `bun test`, `vitest`, etc. directamente — pasa siempre por make.

- **Nada del código de pruebas ni de compilación puede usar tmpfs** — ningún archivo escrito por un objetivo de make, un script de make o el arnés de pruebas puede vivir en un sistema de archivos tmpfs (respaldado por RAM). Esto es innegociable y absoluto: aplica a las imágenes de respaldo del loopback btrfs, a los datos de contenedores/volúmenes, a los archivos comprimidos, a las descargas, a los archivos de puerto, a los archivos de seguimiento y a cualquier otro artefacto por ejecución. El motivo es fatal, no cosmético: el sistema de archivos btrfs de pruebas es un archivo loopback de 50 G, y un dispositivo de bucle respaldado por tmpfs **bloquea el kernel del host** bajo presión de memoria — las páginas de tmpfs solo pueden recuperarse hacia el swap, pero la ruta de escritura diferida del bucle necesita reservar memoria para drenarlas, así que en cuanto tmpfs llena la RAM la máquina se congela por completo y el firmware/watchdog la reinicia (observado en Manjaro, donde systemd monta `/tmp` como tmpfs dimensionado al 50 % de la RAM con swap casi nulo). `/tmp` es tmpfs en las distribuciones de desarrollo habituales (Arch/Manjaro/Fedora), así que **no supongas que `/tmp` está respaldado por disco**. El código de prueba/compilación que cree un archivo de respaldo, un dispositivo de bucle o cualquier destino de escritura de tamaño considerable DEBE resolver primero su directorio a un sistema de archivos realmente respaldado por disco (p. ej. comprobar que `findmnt -no FSTYPE <dir>` no es `tmpfs`/`ramfs`, o colocar los datos bajo una ruta que se sepa en disco como `/var/tmp`) y fallar ruidosamente si no puede. Al añadir cualquier ruta nueva a un script de make, verifica que no esté en tmpfs antes de escribir en ella.

- **Ubicación del estado efímero** — la contabilidad por ejecución (archivos de puerto, archivos de seguimiento `.disk`/`.loop`/`.mount`, metadatos de desarrollo) se acota por instancia bajo `/tmp/town-os-$(INSTANCE_ID)/`, pero cualquier artefacto *portador de datos* — por encima de todo la imagen de respaldo del loopback btrfs — DEBE colocarse en una ruta respaldada por disco, nunca en tmpfs (véase la regla anterior de no-tmpfs). Nunca pongas una imagen de loopback/disco, datos de volúmenes de contenedor o una descarga grande en `/tmp` sin confirmar antes que `/tmp` no es tmpfs.

- **Haz commit o push solo cuando se te indique** — nunca ejecutes `git commit` ni `git push` salvo que el usuario lo pida explícitamente. Nunca hagas force push (`--force` ni `--force-with-lease`).

- systemcontroller nunca debería llamar a os.Exit salvo que el servicio se esté terminando de verdad; los errores críticos deben tratarse con registro fatal

- comprueba todos los errores, por favor. no uses el guion bajo ni omitas la comprobación de errores por ningún motivo en ninguna parte del código, nunca

- **Comprueba siempre el `ok` de una expresión coma-ok.** Toda expresión que devuelva un par `value, ok` — aserciones de tipo (`v, ok := x.(T)`), indexación de mapas (`v, ok := m[k]`) y recepción de canales (`v, ok := <-ch`) — debe comprobar `ok` antes de usar `value`; nunca lo descartes con `_` ni des por hecho que la aserción/búsqueda tuvo éxito. Prefiere la forma coma-ok a la aserción de tipo de un solo valor `v := x.(T)` (que entra en pánico si no coincide): usa `v, ok := x.(T)` y trata `!ok` de forma explícita. Esto aplica también al código de pruebas. (Los casos de switch con tipado limpio — `switch v := x.(type)` — y una escritura deliberada de pertenencia `_ = m[k]` son las únicas excepciones.)

- Usa siempre la sintaxis de error en línea en las sentencias if cuando sea posible (p. ej., `if err := foo(); err != nil {`)

- **Los servicios de prueba usan puertos altos aleatorios** — las pruebas de integración que arrancan servicios de red (DNS, HTTP, gRPC, etc.) deben enlazar a puertos altos aleatorios mediante `findFreePort`, nunca a puertos bien conocidos como el 53 o el 80. Esto evita conflictos cuando se ejecutan varias tandas de pruebas simultáneamente.

- **El DNS en las pruebas NUNCA debe tocar el host.** Ninguna prueba, arnés de pruebas ni nada que lance un objetivo de pruebas de make puede alterar la resolución de nombres del host ni ocupar el puerto DNS del host. En concreto, una ejecución de pruebas nunca debe:
    - reescribir `/etc/resolv.conf` (eso es `redirect_host_dns` en `make/dev.sh`, y pertenece únicamente a `make dev`),
    - escribir `/etc/systemd/resolved.conf.d/town-os.conf` ni llamar de otro modo a `rolodex.ConfigureResolvedRouting`,
    - señalizar o reiniciar `systemd-resolved` (`pkill -HUP systemd-resolved`),
    - enlazar **`127.0.0.2:53`**, ni ningún `:53`, en el espacio de nombres de red del host.

  El contenedor de pruebas se ejecuta con `--net host` deliberadamente (el DNS en red bridge se rompe en redes cautivas), así que todos los puertos que enlaza un servicio del sistema aterrizan en el espacio de nombres del **host**. Por eso exactamente `TOWN_OS_DNS_PORT` se asigna por ejecución en `$(STATE_DIR)/.dns-port` y lo pasa `system_port_env` (`make/lib.sh`), y por eso `main.go` se salta el enrutamiento de resolved siempre que `dnsPortIsDefault()` es falso — una dirección de servidor por dominio en resolved no lleva puerto, así que apuntar resolved a `DNSLoopback` con un rolodex reubicado enviaría al vacío todas las consultas de ese TLD.

  Trata una ejecución de pruebas que deje `127.0.0.2:53` enlazado, o un drop-in `town-os.conf` en el host, como un **fallo del arnés, no como una prueba inestable**: significa que la anulación de puerto no llegó al contenedor y rolodex recurrió al valor predeterminado. Verifícalo con `ss -lnup | grep 127.0.0.2` y `ls /etc/systemd/resolved.conf.d/` — el único proceso a la escucha en el `:53` del host debería ser el resolutor de la propia máquina, nunca el nuestro. `make dev` es la única excepción y es opcional por parte del operador, porque su propósito es reflejar un equipo real.

- **Nunca escribas pruebas que hagan push a Gitea o GitHub remotos.**

- **Cuando te diga que hagas algo, no discutas.**

- **Las operaciones de git en las pruebas deben preferir repositorios locales a repositorios remotos cuando dé igual** — p. ej., populate-repos debería clonar desde un directorio hermano local si existe, en lugar de descargar desde GitHub.

- Corrige todas las advertencias de las pruebas que se puedan corregir, a medida que aparezcan

- Las variables de paquete siempre deben traducirse como parte del paso de compilación. Las variables de paquete fijas siempre deben tener pruebas.

- Asegúrate de que todos los archivos estén organizados por API. Deben acotarse por nombre de subsección, de forma jerárquica. La métrica de recuento de líneas es de unas 500 aproximadamente.


## Convenciones de Rendimiento

- **Usa `strings.Builder` para construir cadenas** — nunca construyas cadenas carácter a carácter con `string(append([]byte(s), c))`. Usa `strings.Builder` con `WriteByte`/`WriteString` para lograr O(n) en lugar de O(n²) reservas. Véase `src/packages/packages_compile.go` (`applyTemplate`, `applyTemplates`).

- **Reserva las porciones (slices) por adelantado cuando se conozca el tamaño** — usa `make([]T, 0, capacity)` cuando se conozca el tamaño del resultado o una cota superior (p. ej., el `limit` de la paginación). Evita `var items []T` seguido de `append` sin límite en rutas críticas.

- **Paginación de una sola consulta con `COUNT(*) OVER()`** — los endpoints de listado paginado deben usar la función de ventana de SQLite `COUNT(*) OVER()` en la lista de columnas del SELECT en lugar de ejecutar una consulta `COUNT(*)` aparte. Lee el total junto con cada fila.

- **Indexa las columnas usadas en cláusulas WHERE** — toda columna de SQLite usada en un filtro `WHERE` (especialmente `created_at`, `success`, `account`) debe tener un índice adecuado. Los índices compuestos deben corresponderse con las combinaciones de filtro habituales (p. ej., `(success, created_at)` para `CountRecentErrors`).

- **Cachea las búsquedas repetidas y costosas** — los resultados de `RepositoryRoot.LoadPackages()` se cachean en un `sync.Map` por nombre de repositorio y se invalidan en `ForceRefresh()`. Quien llame debe usar `cachedLoadPackages()` en lugar de `LoadPackages()` directamente. De forma similar, `GetInternalIP()` cachea el resultado en un `atomic.Value` en vez de llamar a `net.InterfaceAddrs()` en cada petición.

- **Búsquedas directas mejor que recorridos completos** — usa `GetInstalledVersion(repo, name)` (que lee `installed/<repo>/<name>/` directamente) en lugar de `ListInstalled()` + búsqueda lineal cuando compruebes un solo paquete.

- **E/S en paralelo para operaciones independientes** — las descargas de imágenes de contenedor en `refreshSystemServices` usan goroutines con un semáforo (máximo 3 concurrentes) en lugar de un bucle secuencial. Usa `sync.WaitGroup` + un semáforo de canal; no añadas la dependencia `errgroup`.

- **Contexto acotado al servidor para las goroutines de fondo** — las goroutines de fondo (clonado git de pages, extracción de imágenes) deben usar el contexto acotado al servidor (`s.ctx`) en lugar de `context.Background()` para que respeten el apagado ordenado. NO deben usar el contexto de la petición HTTP (la operación debe sobrevivir a la petición).

- **Carga por lotes de dependencias en la reconciliación** — los registros de dependencias de todos los paquetes se precargan en un mapa antes del bucle de reconciliación, no se cargan por paquete dentro del bucle.


## Requisitos Previos de Desarrollo

Compilar Town OS desde el código fuente requiere:

- **Go 1.25+** -- con CGO habilitado para el controlador del sistema (enlaza contra libsystemd).
- **libsystemd-dev** -- cabeceras de desarrollo de C para el diario (journal) de systemd y los enlaces de dbus, requeridas por la dependencia `go-systemd/v22`.
- **Bun** -- runtime de JavaScript para la compilación y las pruebas de la interfaz.
- **Podman** -- rootful (`sudo`), usado para las operaciones con contenedores.
- **btrfs-progs** -- proporciona `mkfs.btrfs` para crear los volúmenes btrfs de pruebas y de desarrollo.
- **golangci-lint** -- para el análisis estático de Go.
- **QEMU** -- `qemu-system-x86_64` para ejecutar paquetes de VM; `qemu-img` para convertir imágenes de disco de VM a formato raw.

### Arranque Inicial

`make deps` instala todas las dependencias del host (Go, podman, runc, btrfs-progs,
cabeceras de libsystemd, golangci-lint, bun, qemu, herramientas de compilación) en
una máquina Arch o Ubuntu/Debian recién instalada. Está implementado en
`make/deps.sh`, detecta la distribución a partir de `/etc/os-release` y es seguro
volver a ejecutarlo.

`make help` (el objetivo predeterminado) imprime una lista agrupada de todos los
objetivos de make de cara al usuario. Está implementado en `make/help.sh`. Mantén
ambos scripts sincronizados al añadir o renombrar objetivos en `make/include.mk`.

### Comprobaciones Previas

El Makefile proporciona un objetivo `preflight-dev` que valida el entorno de desarrollo antes de ejecutar pruebas o arrancar el servidor de desarrollo. Comprueba:

- **podman** -- verifica que el comando `podman` está disponible en el PATH.
- **btrfs-progs** -- verifica que el comando `mkfs.btrfs` está disponible en el PATH.
- **Credenciales de repositorio** -- verifica que las variables de entorno `TOWN_OS_REPO_USERNAME` y `TOWN_OS_REPO_PASSWORD` están definidas.
- **Red bridge** -- arranca un contenedor nginx de prueba con un enlace de puerto para verificar que la opción `-p` de podman funciona correctamente.

Cada comprobación imprime un mensaje de error descriptivo y sale con un estado distinto de cero en caso de fallo. Todas las comprobaciones deben pasar antes de que se muestre el mensaje "All preflight checks passed.".

### Instalación en Ubuntu / Debian

En sistemas Ubuntu o Debian, instala las dependencias del sistema con:

```
sudo apt-get install -y libsystemd-dev btrfs-progs podman runc qemu-system-x86 qemu-utils
```

Go, Bun y golangci-lint deben instalarse por separado (consulta la documentación oficial de cada uno).

## Calidad del Código

### Manejo de Errores

Todos los valores de error devueltos en Go deben comprobarse explícitamente. El linter `errcheck` está habilitado en todo el proyecto y el identificador en blanco (`_ =`) no debe usarse para descartar errores.

En el código de producción, los errores de limpieza en funciones diferidas se combinan con el error principal mediante `errors.Join()` a través de valores de retorno con nombre (p. ej., `defer func() { err = errors.Join(err, f.Close()) }()`). Las operaciones no críticas de mejor esfuerzo registran los errores en lugar de descartarlos.

En el código de pruebas, los errores de limpieza se comunican mediante `t.Errorf` o `t.Logf` según su gravedad, o se suprimen explícitamente con una anotación `//nolint:errcheck` y un comentario que lo justifique.

Todas las directivas `//nolint` requieren un comentario justificativo (lo impone `nolintlint`).

## Pruebas de Integración

### Registro Docker Local

Las pruebas de integración se ejecutan contra un contenedor `registry:2` local para evitar los límites de tasa de Docker Hub y garantizar la reproducibilidad. El proceso es:

1. **Descubrimiento de imágenes** -- la herramienta `discover-images` recorre todos los repositorios de paquetes de prueba buscando referencias a imágenes de `docker.io`, incluidas las imágenes principales y las de archivo. Los resultados se deduplican y se escriben en `.cache/.registry-images`.
2. **Arranque del registro** -- se arranca un contenedor `registry:2` en un puerto aleatorio.
3. **Replicación de imágenes** -- cada imagen descubierta se descarga de Docker Hub, se reetiqueta con la dirección del registro local y se sube a él (con la verificación TLS deshabilitada para localhost).
4. **Configuración del registro** -- se genera un archivo `registries.conf` que redirige las descargas de `docker.io` al espejo local. Se monta en el contenedor de pruebas en `/etc/containers/registries.conf.d/`.
5. **Operación transparente** -- no hacen falta cambios de código; podman usa el espejo local automáticamente. El espejo recurre a Docker Hub para las imágenes que no estén en caché.

Cada directorio de trabajo obtiene su propia instancia de registro (mediante `INSTANCE_ID`), de modo que las ejecuciones de pruebas concurrentes no entran en conflicto.

### Servidor Gitea Local

Las pruebas de integración usan una instancia local de Gitea para evitar los límites de tasa de GitHub en las operaciones de git. El proceso refleja el patrón del registro Docker local:

1. **Arranque del servidor** -- se arranca un contenedor `gitea/gitea:latest` en un puerto aleatorio con la instalación pre-bloqueada. Se crea automáticamente un usuario administrador (`town-os`).
2. **Migración de repositorios** -- la herramienta `populate-repos` migra los repositorios de paquetes de prueba (`test-packages-core`, `test-packages-extras`) desde GitHub a la instancia local de Gitea usando la API de migración de Gitea. La migración es idempotente: los repositorios existentes que no están vacíos se omiten; los repositorios vacíos procedentes de migraciones fallidas se eliminan y se reintentan.
3. **Operación transparente** -- las pruebas reciben las URL de la Gitea local mediante variables de entorno (`TOWN_OS_TEST_REPO_CORE_URL`, `TOWN_OS_TEST_REPO_EXTRAS_URL`). Cuando no están definidas, las pruebas recurren a las URL predeterminadas de GitHub.

Cada directorio de trabajo obtiene su propia instancia de Gitea (mediante `INSTANCE_ID`), de modo que las ejecuciones de pruebas concurrentes no entran en conflicto. El descubrimiento de imágenes lee de los repositorios de la Gitea local cuando está disponible.

### Limpieza de Contenedores

El objetivo `test-full` ejecuta `clean-integration` y `clean-btrfs` una vez terminadas las pruebas de integración, garantizando que todos los contenedores de prueba (test, registry, gitea, ui-backend, ui-integration) y los montajes loopback de btrfs se desmantelen incluso cuando las pruebas fallan. El objetivo `clean-dev` elimina todos los contenedores `town-os-dev` antes de limpiar las cachés. Un objetivo `clean-containers` elimina todos los contenedores de Town OS (los que coinciden con los patrones `town-os-*` y `preflight-test-*`) de cualquier instancia o directorio de trabajo. El objetivo `clean-integration` usa una eliminación de contenedores tolerante a errores para que la limpieza sea idempotente. El objetivo `clean-all` usa `clean-containers` para una limpieza exhaustiva entre instancias. Las imágenes de monitorización se precargan en los contenedores de pruebas de integración desde la caché de imágenes.

### Limpieza del Loopback Btrfs

Los objetivos de prueba (`test-integration`, `test-ui-integration`, `test-full`) usan trampas EXIT del shell para garantizar que la limpieza de btrfs se ejecute con independencia de que las pruebas tengan éxito, fallen o se interrumpan por una señal. Las recetas están organizadas en scripts de shell bajo `make/`. La creación del volumen btrfs se realiza dentro de los scripts de prueba después de registrar la trampa EXIT, asegurando que los dispositivos de bucle no puedan filtrarse aunque la creación o los pasos posteriores fallen.

El objetivo `clean-btrfs` realiza una limpieza de mejor esfuerzo (sin `set -e`): desmonta el sistema de archivos btrfs, desconecta los dispositivos de bucle encontrados mediante `losetup -j` para el archivo de imagen de disco y elimina los archivos de seguimiento de estado (`town-os.disk`, `town-os.loop`, `town-os.mount`). Una red de seguridad recorre todos los dispositivos de bucle activos (`losetup -a`) en busca de cualquiera respaldado por archivos de imagen btrfs del directorio actual y desconecta los dispositivos huérfanos incluso cuando faltan los archivos de seguimiento.

### Organización de los Archivos de Prueba

Los archivos de pruebas de integración se organizan por componente y subfuncionalidad. Cada archivo se centra en un área concreta: operaciones de btrfs, operaciones de git, gestión de repositorios y subsistemas del controlador del sistema. Las pruebas del controlador del sistema se dividen además en archivos separados para archivos comprimidos, arranque inicial, sistemas de archivos, instalación (systemd simulado y real), escenarios multirrepositorio, redes, paquetes, páginas, reconciliación, repositorios, ajustes, unidades de systemd y volúmenes. La inicialización común de las pruebas y las funciones auxiliares están centralizadas en un archivo de ayudantes dedicado.

### Entorno de Pruebas

Las pruebas de integración se ejecutan dentro de contenedores podman privilegiados con systemd, btrfs y el binario de pruebas completo. El contenedor incluye podman y runc para ejecutar los contenedores de los paquetes. Las pruebas ejercitan el ciclo de vida real de las unidades de systemd, la gestión de volúmenes btrfs y las operaciones con contenedores.
