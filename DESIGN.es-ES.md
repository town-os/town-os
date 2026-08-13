# Diseño de Town OS

Cómo funciona Town OS: la arquitectura, el comportamiento de cada subsistema, la
superficie de la API y los invariantes que lo sostienen todo. Las instrucciones
de compilación, las reglas de pruebas y el estilo de código están en
[CLAUDE.md](CLAUDE.md) (traducción al español de España en
[CLAUDE.es-ES.md](CLAUDE.es-ES.md)).

Un cambio de comportamiento corresponde a este archivo, dentro del mismo commit
que lo introduce. Un cambio en cómo se compila o se prueba el repositorio
corresponde a CLAUDE.md.

> **Este archivo es la traducción al español (España) de [DESIGN.md](DESIGN.md).
> El original en inglés es el autoritativo** — describe el cambio allí, y la
> traducción va después. Los identificadores de código, rutas de archivo,
> comandos, variables de entorno, rutas de la API y nombres de claves YAML se
> conservan sin traducir.
>
> Otras traducciones: español de México ([DESIGN.es-MX.md](DESIGN.es-MX.md)) y
> chino, en escritura simplificada ([DESIGN.zh-CN.md](DESIGN.zh-CN.md)) y
> tradicional ([DESIGN.zh-TW.md](DESIGN.zh-TW.md)); y japonés
> ([DESIGN.ja-JP.md](DESIGN.ja-JP.md)).

## Invariantes Arquitectónicos

Reglas que restringen el diseño, no el código. Romper una de ellas no hace fallar
una compilación ni un linter — produce un equipo que arranca y luego se comporta
mal, normalmente en algún punto muy alejado del cambio.

- **La capa de almacenamiento gestiona volúmenes; gfeh proporciona el almacenamiento de objetos.** `src/storage` se ocupa de subvolúmenes btrfs y cuotas y de nada más -- no maneja en absoluto el almacenamiento de objetos. Los objetos, los metadatos y permisos por archivo, la base de datos jerárquica de usuarios/ACL, la compartición, la exposición HTTP por archivo, la federación y todas las vistas de protocolo (S3, IPFS, Google Drive, HTTP simple — y SMB/CIFS, que gfehd implementa pero Town OS no sirve) pertenecen a gfeh, que es el responsable. Nunca añadas endpoints de objetos/blobs/por archivo a `src/storage` ni a `/storage/*`, y nunca le enseñes a `storage.Storage` ni a `storage.Controller` qué son los usuarios, los permisos o los protocolos. Véase [Almacenamiento](#almacenamiento).

- **La función de pages está siempre habilitada** — el subsistema de pages (alojamiento de sitios estáticos mediante Caddy) se inicializa incondicionalmente en el arranque; no hay ninguna barrera de entorno `TOWN_OS_PAGES`. El gestor de pages no es nil en un arranque normal, así que la API de pages siempre está disponible. Los manejadores mantienen todavía una guarda defensiva de gestor nil que devuelve "pages not configured" (la ejercitan las pruebas que construyen un servidor sin `ServerConfig.PagesMgr`), pero los arranques reales nunca llegan ahí.

- **Detección de cambio de versión y reinicio de unidades** — el systemcontroller detecta las actualizaciones de imagen comparando el SHA de la imagen del contenedor en ejecución (de `/proc/1/cgroup` → `podman inspect`) contra un archivo de versión persistido en `<btrfsPath>/town-os-version`. Al cambiar la versión: (1) se descargan todas las imágenes de contenedor, (2) se reconstruye la imagen del NC, (3) la reconciliación regenera todas las unidades de systemd, (4) las unidades cuyo contenido cambió se reinician en orden: primero las unidades NC (son las dueñas de las redes), luego los servicios de dependencias, luego los servicios padre/independientes, (5) los comandos posteriores a la actualización (campo `post_update`) se ejecutan mediante `podman exec` para los paquetes de contenedor cuyas unidades cambiaron. El archivo de versión se escribe tras una reconciliación correcta. El contenido de las unidades se compara antes/después mediante `ReadUnit()` para evitar reinicios innecesarios cuando el contenido no ha cambiado.

- **La imagen del controlador de red se descarga, no se construye en el arranque** — la imagen del NC es una imagen hermana publicada (`quay.io/town/networkcontroller:<tag>`, con la etiqueta de `resolveImageTag()`) que se descarga junto con las demás imágenes centrales, exactamente igual que las imágenes de la interfaz, rolodex e ingress. **No** se construye con `podman build` durante el arranque; la antigua construcción en tiempo de arranque (`localhost/town-os-networkcontroller:local`, base alpine, `--dns=8.8.8.8`) ha desaparecido. `NC_IMAGE` anula el valor derivado por defecto y es lo que establece el arnés de integración para inyectar una imagen construida localmente. La descarga no es fatal: cada unidad NC de paquete lleva un respaldo `ExecStartPre` de creación de red con `--pull=never`, así que una descarga fallida es recuperable en el siguiente arranque.

- **Todos los servicios de monitorización son servicios del sistema** — Prometheus, Node Exporter y la interfaz de monitorización se ejecutan todos bajo el espacio de nombres de servicios del sistema (prefijo `town-os-system--`), arrancados directamente desde `main.go` antes de la reconciliación. Nunca se instalan a través del sistema de repositorios de paquetes; no existe un paquete "monitoring" instalable. Los tres servicios son: `town-os-system--node-exporter.service` (red del host, puerto 9100), `town-os-system--prometheus.service` (puerto 9090, con configuración/datos montados desde `{btrfsBase}/monitoring/`) y `town-os-system--monitoring-ui.service` (puerto 5308). El servicio de la interfaz de monitorización ejecuta o bien un reenviador socat (modo uPlot, el predeterminado) o bien Grafana (modo grafana), controlado por el ajuste `monitoring_backend`. La configuración de Prometheus se escribe directamente en disco. Prometheus, Grafana y el reenviador socat de uPlot se generan mediante `systemd.GeneratePackageUnits` con `PackageUnitConfig.SystemServiceKey` establecido, así que obtienen un controlador de red completo, activación por socket y una red podman privada — la misma fontanería que los paquetes normales, pero con la nomenclatura de servicios del sistema.

- **La propiedad de los volúmenes del host es declarativa en `HostVolumeMount`, y no recursiva** — las imágenes de contenedor con un uid interno fijo (el `472` de Grafana, el `65534` de Prometheus, etc.) necesitan escribir en su ruta del host montada por bind, y los bind mounts pasan la propiedad del host tal cual, así que la ruta del host debe pertenecer a ese uid:gid antes de que arranque el contenedor. Usamos bind mount (en lugar de un volumen podman con nombre, que podman haría chown en la primera creación) porque queremos los datos en un subvolumen btrfs con cuota. La estructura `systemd.HostVolumeMount` de `src/systemd/unit.go` lleva los campos opcionales `UID *uint32` y `GID *uint32`; cuando ambos están establecidos, el generador de unidades emite **`ExecStartPre=/bin/chown <uid>:<gid> <rutahost>`** (sin `-R`) para ese montaje justo después de las líneas `ExecStartPre=/bin/mkdir -p` y antes de `podman run`. Esta es la única fuente declarativa de propiedad para los volúmenes montados por bind desde el host en los servicios del sistema, y sustituye a las anteriores entradas de chown artesanales en `ExecStartPreExtra` de `GrafanaPackageConfig` y `PrometheusPackageConfig`.

  El chown es deliberadamente no recursivo, lo cual basta porque:
  1. **Los montajes escribibles** (`grafana-data` → `/var/lib/grafana`, `prometheus-data` → `/prometheus`) solo necesitan la propiedad del nivel superior para que el contenedor pueda crear dentro sus propios subdirectorios. El proceso del contenedor crea esos hijos con su propio uid (472 o 65534), así que ya tienen la propiedad correcta y nunca se desvían. No hace falta recursión.
  2. **Los montajes de solo lectura** (`grafana-provisioning` → `/etc/grafana/provisioning`) no declaran UID/GID en absoluto y no emiten ninguna línea de chown. Mientras los permisos del host sean 0755/0644 (que es lo que establece `WriteGrafanaProvisioningFiles`), cualquier uid puede leer el contenido con independencia de quién sea su propietario.

  `EnsureGrafanaStorage` (`src/monitoring/monitoring_ui.go`) ahora solo crea los directorios y retorna; no hace ningún chown. `WriteGrafanaProvisioningFiles` escribe los archivos YAML/JSON de la fuente de datos y de los paneles con permisos legibles por todos y no necesita corregir la propiedad después. El chown en proceso basado en `filepath.WalkDir` que solía recorrer `grafana-data` en cada arranque ha desaparecido; la única llamada al sistema `chown` que emite systemd es el arreglo autoritativo. Las constantes uid/gid siguen viviendo en sus respectivos archivos (`grafanaUID = 472` / `grafanaGID = 472` en `monitoring_ui.go`, `prometheusUID = 65534` / `prometheusGID = 65534` en `prometheus.go`); no las cambies sin que coincidan con la imagen de contenedor original.

- **El directorio de estado de red debe compartirse con el host** — el valor predeterminado de `-network-state` es `/run/town-os` (`DefaultNetworkStatePath` en `src/svc/systemcontroller/cmd/systemcontroller/main.go`). El systemcontroller se ejecuta dentro de un contenedor pero crea los contenedores NC en el host mediante `CONTAINER_HOST`, así que la ruta de origen del bind mount (`-v /run/town-os:/run/town-os:ro` en todas las unidades NC) debe existir en el sistema de archivos del host. La unidad de systemd del systemcontroller del repositorio de instalación debe montar por bind `/run/town-os:/run/town-os` y asegurarse de que el directorio del host existe antes de que arranque el systemcontroller (`ExecStartPre=/usr/bin/mkdir -p /run/town-os` o `RuntimeDirectory=town-os`). Sin ese montaje, el `os.MkdirAll` del systemcontroller y las escrituras de archivos de estado aterrizan dentro del tmpfs del contenedor, el directorio del host no existe y los contenedores NC no arrancan, con `Error: statfs /run/town-os: no such file or directory` — tumbando Prometheus, la interfaz de monitorización y todos los paquetes con red. Nunca uses por defecto `/var/run/town-os` ni ninguna ruta bajo `/var/run` o `/tmp`; la ruta debe vivir bajo `/run` (u otro bind mount compartido con el host) y debe ser la misma ruta a ambos lados del montaje.

## Secuencia de Arranque del Controlador del Sistema

El arranque del controlador del sistema en `src/svc/systemcontroller/cmd/systemcontroller/main.go` sigue este orden exacto. Cada paso marcado como **(no fatal)** registra en stderr y continúa; todo lo demás es fatal y aborta el arranque.

El arranque es **observable**: `:5309` se enlaza antes de que ocurra cualquier trabajo, respaldado por un stub mínimo de estado de arranque que transmite el progreso; el router Echo completo se intercambia al final sin cerrar nunca el listener. El progreso se informa como cinco etapas gruesas (`boot_controller`, `boot_dns`, `boot_services`, `restart_packages`, `ready`) — véase [Estado de Arranque y Refresco](#estado-de-arranque-y-refresco).

1. **Establecer `CONTAINER_HOST`** — `setupPodmanEnv()` establece `CONTAINER_HOST=unix:///run/podman/podman.sock` para que toda invocación posterior de `podman` (y todo proceso hijo) se encamine por el socket de podman del host en lugar del almacenamiento aislado del contenedor del systemcontroller.
2. **Analizar los flags de CLI y las variables de entorno** — `-db`, `-btrfs`, `-repo-dir`, `-network-state`, `-listen`. Anulaciones por entorno: `TOWN_OS_LISTEN`.
3. **Enlazar `:5309` con el manejador de arranque** — `NewBootStatus()` + `NewRootHandler(NewBootHandler(bs))` enlazan el listener de inmediato, antes de cualquier trabajo de arranque. Hasta el intercambio del paso 24, el socket responde únicamente a `GET /status/ping` (503 con `{booting, step, done, boot_id}`) y `GET /boot-status` (SSE); todo lo demás es 403.
4. **Etapa `boot_controller`** — directorio de trabajo temporal; crear la base btrfs y el directorio de estado de red; eliminar cualquier `town-os.db` obsoleto que despliegues antiguos hayan dejado en la raíz de btrfs (`cleanupStaleRootDB`) y rechazar una ruta `-db` que volvería a crearlo (`validateDBPath`) — la base de datos en ejecución vive en `<btrfsBase>/data/db/system.db`, nunca en la raíz.
5. **Abrir la base de datos SQLite** — persistente si `-db` está establecido; si no, un archivo temporal efímero.
6. **Inicializar el gestor de cuentas** — crea la tabla de cuentas y migra una heredada (las columnas de capacidades pasan a ser concesiones; se elimina `smb_nt_hash`). Después, `PurgeLegacyServiceAccounts` **(no fatal)** elimina la antigua cuenta de administrador del demonio de almacenamiento de objetos y su contraseña almacenada, una sola vez, en el primer arranque tras una actualización — véase [Sin cuentas de servicio](#sin-cuentas-de-servicio).
7. **Generar una clave efímera de firma JWT** — 32 bytes aleatorios vía `crypto/rand`, anulable con `TOWN_OS_SIGNING_KEY`. Inicializa el gestor de sesiones, que borra todas las sesiones previas (los tokens antiguos no son válidos con la clave nueva).
8. **Inicializar los gestores de auditoría, ajustes, pages y red** — los ajustes se siembran con valores predeterminados (`default_quota`, `max_archive_size`, `locale`, `dns_tld`, `dns_resolution_mode`, `peer_ttl`, …); pages siempre se inicializa; el gestor de red es el dueño de las tablas de redes y pares de WireGuard **y siembra la red del hogar**, así que a partir de este punto siempre existe (véase [La red del hogar siempre existe](#la-red-del-hogar-siempre-existe)).
9. **Sembrar los repositorios** — si `repositories.json` no existe, escribe los repositorios predeterminados (o los de prueba si `TOWN_OS_TEST`/`DEBUG`). Aplica las credenciales `TOWN_OS_REPO_USERNAME`/`TOWN_OS_REPO_PASSWORD`.
10. **Inicializar la raíz de repositorios y forzar un refresco** — clona/descarga todos los repositorios configurados mediante go-git.
11. **Inicializar el gestor de instalación, el almacenamiento btrfs y el gestor de systemd**.
12. **Resolver la etiqueta de imagen** — `resolveImageTag()`: la variable de entorno `TOWN_OS_TAG` (que establece el sistema de compilación de la instalación) y, en su defecto, `rc.latest-<arch>` (`defaultVersionTag()`, con la arquitectura de `runtime.GOARCH` mapeada a `x86_64`/`aarch64` mediante `archTag()`). No hay archivo `/town-os.tag` ni versión `Version` fijada en tiempo de compilación. Todas las etiquetas de las imágenes hermanas (interfaz, rolodex, controlador de red, ingress) derivan de este único valor; las etiquetas de subida son por arquitectura, así que las etiquetas hermanas derivadas también lo son.
13. **Derivar la imagen del NC** — `quay.io/town/networkcontroller:<tag>`, anulable con `NC_IMAGE`. Se descarga (paso 17), nunca se construye.
14. **Arrancar el refresco de repositorios en segundo plano** — una goroutine consulta cada 5 minutos.
15. **Etapa `boot_dns`: escribir la configuración de Rolodex y reiniciar si cambió** **(no fatal)** — Rolodex es un servicio de arranque gestionado por systemd. El systemcontroller escribe `rolodex.yml` (idempotente: lo omite si el archivo es más reciente que el binario y el contenido no ha cambiado) y reinicia el servicio solo cuando el archivo se escribió. `resolution.mode` viene del ajuste `dns_resolution_mode`, y un valor almacenado que no se pueda analizar recurre al predeterminado en lugar de renderizar una configuración que rolodex rechazaría. `forwarders:` viene del ajuste `dns_local_forwarders`: cuando está activo, la lista se descubre a partir de los resolutores del host en cada arranque, de modo que un equipo que cambió de red toma el nuevo sin que el operador toque nada (véase [Reenviadores locales](#reenviadores-locales)). El contenedor de rolodex se ejecuta con `--net host` y enlaza el DNS directamente a `127.0.0.2:{puerto}`. Después espera a que el DNS esté listo (sondeo de conexión TCP) y configura systemd-resolved para encaminar el TLD a rolodex — **se omite cuando `TOWN_OS_DNS_PORT` ha movido rolodex fuera de `:53`**, ya que una dirección de servidor por dominio en resolved no lleva puerto y enviaría al vacío todas las consultas de ese TLD.
16. **Leer el backend de monitorización y descubrir los dispositivos de disco btrfs** — `monitoring_backend` (predeterminado `uplot`); `monitoring.BtrfsDevices(btrfsPath)` **(no fatal)** expone los dispositivos de bloque subyacentes a través de `/monitoring/status`.
17. **Etapa `boot_services`: descargar las imágenes de contenedor centrales** **(no fatal)** — la imagen del NC, Prometheus, Node Exporter, la imagen de la interfaz, la imagen del almacenamiento de objetos (gfeh), la imagen del ingress y Grafana cuando ese backend está seleccionado, en paralelo mediante `parallelEnsureImages` (omite la descarga cuando la imagen ya está cargada). Toda imagen que referencie una unidad de arranque pertenece aquí: una unidad cuya imagen no es local la descarga desde dentro de `podman run`, así que su espera de disponibilidad compite con una descarga del registro. gfeh y después el ingress faltaron en esta lista por turnos, y cada uno se leyó como un servicio que simplemente estaba caído. La interfaz de monitorización no necesita entrada propia — en el backend uPlot ejecuta la imagen del NC, que ya es la primera del conjunto.
18. **Arrancar los servicios de monitorización del sistema** **(todos no fatales)** — primero se desmantelan las unidades NC/socket heredadas del diseño anterior (todavía retienen `-p 9090`/`-p 5308` y harían entrar en bucle de caídas a los servicios nuevos). Node Exporter, Prometheus y la interfaz de monitorización se ejecutan todos con `--net host`; node-exporter y Prometheus enlazan al loopback, y solo el `:5308` de la interfaz de monitorización mira a la LAN. Los tres puertos vienen de `monitoringPortsFromEnv()`, cuyo valor cero son los predeterminados de producción ([Puertos de host de los servicios del sistema](#puertos-de-host-de-los-servicios-del-sistema)). Después se instala el temporizador nocturno de poda de podman **(no fatal)**. El temporizador diario de actualización no se instala aquí — viene con el instalador, véase [Actualizaciones automáticas](#actualizaciones-automáticas).
19. **Asegurar la CA TLS local** **(no fatal)** — `tls.EnsureCA(<btrfsPath>/tls)` antes de la reconciliación, para que esta pueda emitir certificados hoja mientras recorre los paquetes instalados.
20. **Arrancar el ingress y el servicio de pages** **(no fatal)** — `ingressctl.Manager` instala y arranca `town-os-system--ingress` (router compartido SNI en `:443` + Host en `:80`), en doble pila solo cuando el host tiene una IPv6 global. El servicio Caddy de pages arranca junto a él. Ambos se omiten cuando `INGRESS_IMAGE` se establece explícitamente a vacío (modo de desarrollo).
21. **Reconciliar el almacenamiento de objetos** **(no fatal)** — `ReconcileGfeh` garantiza una partición gfeh por red: el subvolumen `gfeh/<network>` (con chown a uid 2000), el `gfehd.yaml` renderizado y la unidad `town-os-system--gfeh-<network>`, reiniciada solo cuando el contenido renderizado cambió. Se omite por completo cuando `GFEH_IMAGE` está explícitamente vacío, y se omite cuando el ingress está deshabilitado (las cuatro vistas HTTP solo son accesibles a través de él). Los *nombres* de las particiones se publican después y de forma asíncrona — véase el paso 30. Véase [Almacenamiento de Objetos (gfeh)](#almacenamiento-de-objetos-gfeh).
22. **Detectar el cambio de versión** — compara el SHA de la imagen del contenedor en ejecución (`/proc/1/cgroup` → `podman inspect`) contra `<btrfsPath>/town-os-version`. Establece `versionChanged` para la reconciliación.
23. **Reconciliar** — recorre todos los paquetes instalados y restaura el estado de ejecución:
    - Crea los subvolúmenes btrfs raíz (`installed`, `uninstalled`, `archives`, `pages`, `vm-images`, `user`, `tls`, `gfeh`).
    - Para cada paquete instalado (la última versión por repositorio/nombre): carga el YAML, compila con las respuestas guardadas, crea los volúmenes btrfs con cuotas, siembra los volúmenes vacíos desde archivos/git/proton, aplica las plantillas de archivo, emite el certificado hoja TLS del paquete, escribe los archivos de estado de red (incluido el `fqdn` resuelto), genera e instala las unidades de systemd (servicio + NC + sockets) y arranca los servicios.
    - Si `versionChanged`: reinicia las unidades cuyo contenido cambió (primero NC, luego dependencias, luego servicios) y después ejecuta los comandos `post_update`.
    - Reconcilia pages: asegura subvolúmenes, enlaces simbólicos y contenido de las páginas.
    Después persiste el SHA de la imagen actual en `<btrfsPath>/town-os-version`.
24. **Reconciliar el DNS y las redes** — marca el socket gRPC de rolodex (reintentando hasta 30 s). `RebuildDNS` limpia y reconstruye rolodex desde cero para descartar la deriva de una ejecución previa que se cayó; `RebuildNetworkDNS` vuelve a registrar los registros globales de cara a la LAN (y los anclajes DANE) de los paquetes que no están en la red predeterminada. `ReconcileNetworks` reconcilia entonces el TLD de la red del hogar contra `dns_tld` y levanta la interfaz WireGuard de cada red habilitada, pasando el cliente de rolodex para que el ámbito del TLD de cada red tenga dueño — incluido el ámbito solo-DNS del hogar. Todo no fatal. Después se reconcilia el almacenamiento de objetos **por segunda vez** (idempotente), de modo que una red que este paso levantó obtiene su partición sin esperar a un reinicio.
25. **Programar el ingress** **(no fatal)** — espera a que esté listo, marca su socket gRPC y `RebuildIngress` empuja el conjunto completo de rutas (paquetes HTTP + páginas + vistas e índices del almacenamiento de objetos) de forma declarativa, el mismo modelo que `RebuildDNS`. También renderiza la página de índice de cada partición a partir exactamente del mismo conjunto de sitios con el que se construyen esas rutas, en la misma pasada — una ruta no puede programarse antes de que existan los bytes que sirve ([El índice de la partición](#el-índice-de-la-partición)).
26. **Arrancar el contenedor de la interfaz** **(no fatal)** — `town-os-system--ui.service`; se omite cuando `UI_IMAGE` está explícitamente vacío (modo de desarrollo, donde bun sirve la interfaz).
27. **Etapa `restart_packages`: etapa de frescura** — si el proceso anterior dejó una marca de refresco, reinicia en serie todas las unidades de paquetes instalados, emitiendo un evento de progreso por paquete para que la interfaz renderice una fila por cada uno. Una marca obsoleta de una caída es inofensiva.
28. **Crear el manejador HTTP** — conecta todos los gestores a `ServerConfig`, arranca los sondeos en segundo plano (IP externa cada hora, reparación de deriva de DNS, segador de pares caducados) y configura el router Echo con CORS, la lista blanca de concesiones que falla en cerrado, la autenticación y el middleware de auditoría.
29. **Etapa `ready`: intercambiar el manejador raíz** — el stub de arranque se sustituye atómicamente por el router Echo completo en el listener ya enlazado, así que no se produce ningún parpadeo de puerto y los suscriptores de `/boot-status` (SSE) en vuelo sobreviven al traspaso. `BootStatus.Done()` cierra entonces el flujo. **El sistema ya está listo.**
30. **Publicar los nombres del almacenamiento de objetos** **(no fatal, en segundo plano)** — `publishGfehNames` espera a que al menos una partición responda en su socket de administración y luego vuelve a ejecutar las reconstrucciones de DNS e ingress para que la salida de `/v1/names` de cada partición se convierta en registros A, anclajes TLSA, SAN de las hojas y vhosts del ingress. Se ejecuta **después** del intercambio, y de forma asíncrona, porque gfehd sondea `/status/ping` — que responde 503 hasta el paso 29 — antes de autenticarse, así que esperarlo en línea bloquearía el propio arranque que está esperando. Si nada se pone listo a tiempo, los nombres los publica la siguiente reconciliación.
31. **Apagado ordenado** — con SIGINT: cancela el contexto y apaga el servidor HTTP con un tiempo límite de 30 s. Todas las goroutines de fondo salen mediante la cancelación del contexto.


# Especificación Funcional de Town OS

Town OS es una plataforma de nube autoalojada para usuarios domésticos. Se ejecuta íntegramente desde una memoria USB en RAM, usando todo el almacenamiento del sistema para los datos del usuario. El empaquetado, el almacenamiento y la red están totalmente integrados. Una interfaz web proporciona la gestión para usuarios no técnicos.

## Biblioteca de Git

Todas las operaciones internas de git usan una biblioteca puramente en Go (`go-git/go-git/v5`) en lugar de invocar el CLI `git`.

### Interfaz del Cliente

La interfaz `git.Client` abstrae todas las operaciones de git:

- **Clone** -- clona un repositorio en un subdirectorio con nombre de un directorio padre.
- **Pull** -- hace pull con rebase.
- **Diff** -- informa de si el árbol de trabajo tiene cambios sin confirmar.
- **Stash / StashApply** -- guarda y vuelve a aplicar los cambios sin confirmar.
- **Fetch** -- descarga del remoto origin.
- **Checkout** -- cambia a una rama, etiqueta o hash de commit.
- **Init** -- inicializa un repositorio nuevo. Devuelve un error si el directorio padre no existe.
- **Add** -- prepara archivos por pathspec (admite `"."` para todos los archivos).
- **Commit** -- crea un commit usando la configuración local de usuario de git (recurre a `Town OS <town-os@localhost>`).
- **RevParse** -- resuelve una referencia a un hash SHA.
- **Run** -- despacha subcomandos arbitrarios de git (`config`, `branch`, `rev-parse --abbrev-ref`, `log`, `init`, `status`).

### Implementación

`GoGitClient` implementa la interfaz usando `go-git`. Admite:

- Credenciales incrustadas en la URL (`esquema://usuario:contraseña@host/...`), extraídas y pasadas como `http.BasicAuth`.
- Tiempos límite y cancelación basados en contexto en todas las operaciones.
- Un campo `Home` que anula el directorio HOME para operaciones aisladas.

### Cliente Simulado

`MockClient` proporciona una implementación simulada y segura para hilos destinada a las pruebas unitarias. Registra todas las llamadas a métodos con sus argumentos y admite errores y valores de retorno inyectables por método.

### Uso

- **Repositorios de paquetes**: clonado, pull (con stash/apply alrededor de los árboles sucios) y fetch para el refresco de repositorios (mediante `GoGitClient`).
- **Siembra de volúmenes**: clonar repositorios git en volúmenes vacíos durante la instalación y la reconciliación (mediante `GoGitClient`).
- **Pages**: clonar y actualizar repositorios de sitios estáticos (mediante `GoGitClient`).
- **Reconstrucción de origen git**: actualizar los volúmenes git de un paquete instalado y reiniciar el servicio dependiente (mediante `GoGitClient`).

## Gestión de Repositorios

### Modelo de Repositorio

Los repositorios se definen por un nombre, una URL y credenciales opcionales (usuario y contraseña). Se almacenan en un archivo `repositories.json` en el directorio base. Se siembra un repositorio predeterminado si no hay ninguno configurado.

### API de Repositorios

- `POST /repository/add` (requiere admin) -- añade un repositorio nuevo. Acepta nombre, URL y credenciales opcionales de usuario/contraseña. Si no se proporcionan credenciales, se usan las predeterminadas del sistema. El repositorio se clona mediante go-git y se dispara un refresco.
- `POST /repository/remove` (requiere admin) -- elimina un repositorio por nombre y dispara un refresco.
- `POST /repository/move` (requiere admin) -- cambia la posición de prioridad de un repositorio. Acepta el nombre y el índice de posición destino.
- `POST /repository/refresh` (requiere admin) -- fuerza el refresco de todos los repositorios. Devuelve los errores de refresco que haya.
- `GET /repository` (requiere autenticación) -- lista todos los repositorios con búsqueda, ordenación y paginación. Cada entrada incluye nombre, URL, usuario y cualquier error de refresco.

### Refresco de Repositorios

Los repositorios se refrescan periódicamente (intervalo predeterminado de 5 minutos) descargando desde origin mediante go-git. Se usa stash/apply alrededor de los árboles sucios durante el refresco. Los errores de refresco se registran por repositorio y se exponen a través de los endpoints de listado y de ping de estado.

## Sistema de Paquetes

### Definición de Paquete

Los paquetes se definen en YAML con la siguiente estructura:

- `image` -- referencia a la imagen de contenedor (mutuamente excluyente con `vm`).
- `vm` -- configuración de máquina virtual (mutuamente excluyente con `image`). Véase **Configuración de VM** más abajo.
- `proton` -- configuración del runner Proton/Wine para ejecutables de Windows (mutuamente excluyente con `vm` y `command`). Véase **Configuración de Proton** más abajo.
- `entrypoint` -- lista de cadenas que sustituye al `ENTRYPOINT` incorporado de la imagen en el momento de `podman run`. Se emite como `podman run --entrypoint='["..."]'` (array JSON, entrecomillado con comillas simples para que systemd lo reenvíe literalmente). Necesario para imágenes cuyo ENTRYPOINT original es un script envoltorio que rechaza argumentos de comando arbitrarios (p. ej., el `/start.py` de `matrixdotorg/synapse` interpreta el primer argumento como un "modo" y da error ante cualquier valor desconocido — un paquete que quiera `command: [sh, -c, "…"]` debe establecer además `entrypoint: [sh, -c]` para que podman sustituya `/start.py` por completo). Solo para el runtime de contenedor; se rechaza en paquetes de VM (`ErrEntrypointVMNotSupported`) y en paquetes de Proton (Proton genera su propio comando automáticamente).
- `command` -- lista de cadenas que se convierte en el CMD del contenedor (los argv que se pasan DESPUÉS del entrypoint). Solo para el runtime de contenedor; mutuamente excluyente con `proton`. Los argumentos de varias palabras que contienen espacios en blanco o metacaracteres de shell se entrecomillan con comillas simples en el archivo de unidad generado para que el tokenizador de ExecStart de systemd los reenvíe como un único elemento de argv — una cadena encadenada `"a && exec b"` sigue siendo un solo argumento y su `&&` se reenvía a `sh -c` (cuando el entrypoint es `[sh, -c]`) en lugar de que systemd la parta.
- `environment` -- variables de entorno clave-valor (admite sustitución de plantillas; solo runtime de contenedor).
- `network` -- mapeos de puertos externos e internos (admite sustitución de plantillas).
- `volumes` -- volúmenes con nombre, con punto de montaje, cuota opcional, origen de archivo opcional, URL de siembra git opcional y UID/GID opcionales.
- `questions` -- preguntas con nombre que se le presentan al usuario durante la instalación.
- `notes` -- metadatos tipados (URL, teléfono, correo electrónico) que se muestran tras la instalación. Los tipos se validan durante la compilación: las URL deben analizarse como URL válidas, los correos deben coincidir con el formato `usuario@dominio.tld` y los números de teléfono deben coincidir con dígitos y caracteres de formato opcionales.
- `description` -- descripción del paquete legible por humanos.
- `supplies` -- lista de capacidades que proporciona este paquete.
- `archives` -- lista de archivos comprimidos de imágenes de contenedor con los que poblar volúmenes en el momento de la instalación (solo runtime de contenedor).
- `templates` -- plantillas de archivo con nombre renderizadas en los volúmenes mediante text/template de Go. Cada plantilla especifica un volumen destino, una ruta de archivo y el contenido de la plantilla.
- `post_update` -- lista de comandos de shell que se ejecutan dentro del contenedor en marcha después de detectar un cambio de SHA de imagen durante la reconciliación (solo runtime de contenedor; no se admite en paquetes de VM). Véase **Comandos Posteriores a la Actualización** más abajo.

### Tipo de Runtime

Cada paquete tiene un tipo de runtime: `container` (predeterminado) o `vm`. El runtime se determina por qué campo de primer nivel está presente: `image` (o `proton`) selecciona el runtime de contenedor (podman), `vm` selecciona el runtime de VM (QEMU). Un paquete debe especificar exactamente uno de `image`/`proton` o `vm`; especificar ambos o ninguno es un error de validación. Los paquetes de Proton son una forma especializada de paquete de contenedor -- usan el runtime de contenedor pero generan el comando automáticamente y extraen los archivos de la aplicación de Windows de una imagen de contenedor aparte.

### Configuración de VM

La sección `vm` configura una máquina virtual QEMU:

- `image` -- URL de la imagen de disco de la VM o nombre de archivo local (obligatorio). Puede ser una URL HTTP/HTTPS para imágenes remotas o un nombre de archivo que referencie una imagen cacheada en el subvolumen `vm-images`. Admite sustitución de plantillas `@variable@`.
- `memory` -- memoria de la VM como cadena de bytes legible por humanos (p. ej., `2gb`, `512mb`). Por defecto `1gb`. Admite sustitución de plantillas `@variable@`.
- `cpus` -- número de CPU virtuales. Por defecto `1`. Debe ser no negativo.

### Configuración de Proton

La sección `proton` configura una aplicación de Windows para ejecutarla mediante la capa de compatibilidad Proton/Wine:

- `app_image` -- referencia a la imagen de contenedor que contiene los archivos de la aplicación de Windows (obligatorio). Se normaliza durante la compilación. Admite sustitución de plantillas `@variable@`.
- `app_directory` -- ruta absoluta dentro del contenedor donde está instalada la aplicación (obligatorio, p. ej., `/app`). Admite sustitución de plantillas `@variable@`.
- `volume` -- nombre de un volumen de paquete definido donde se extraerán los archivos de la aplicación (obligatorio). Admite sustitución de plantillas `@variable@`.
- `exe` -- ruta al ejecutable de Windows que hay que ejecutar (obligatorio, p. ej., `/app/myapp.exe`). Admite sustitución de plantillas `@variable@`.
- `args` -- argumentos de línea de comandos opcionales que se pasan al ejecutable. Cada elemento admite sustitución de plantillas `@variable@`.

En el momento de la instalación, el sistema descarga `app_image`, extrae `app_directory` en el volumen indicado y genera automáticamente el comando del contenedor como `proton run <exe> [args]`. La imagen de contenedor que se usa para ejecutar la aplicación es la del ajuste global `proton_image` (`quay.io/town/proton:latest` por defecto), que puede anularse por paquete estableciendo `image`. Durante la reconciliación, la extracción de la aplicación solo se repite si el volumen destino está vacío.

### Variables de Plantilla

La sustitución de plantillas usa la sintaxis `@nombre_variable@`. Las variables se sustituyen por las respuestas a las preguntas durante la compilación del paquete. La sustitución se aplica a: valores de entorno, nombres y destinos de puertos de red, puntos de montaje de volúmenes, cuotas de volúmenes, referencias de archivos de volúmenes, URL de git de volúmenes, URL de imágenes de VM y valores de memoria de VM. También hay dos variables integradas disponibles: `@LOCAL_EXTERNAL_HOST@` y `@LOCAL_INTERNAL_HOST@`.

La secuencia `@@` es un escape literal de `@`. Para producir un `@` literal seguido de una variable de plantilla, usa tres signos `@`: `@@@variable@`. Por ejemplo, `ssh://git@@@PACKAGE_DNS@:@sshport@` se resuelve como `ssh://git@gitea.default.home:2222`. Un `@@` aislado se resuelve como `@` (p. ej., `admin@@example.com` → `admin@example.com`).

La compilación de las notas usa un resolutor de una sola pasada (`ApplyTemplates`) que fusiona las variables de contexto (`PACKAGE_DNS`, `LOCAL_EXTERNAL_HOST`, `LOCAL_INTERNAL_HOST`) y las respuestas del usuario en una única pasada, manejando correctamente los escapes `@@`. Los demás campos (entorno, puertos, volúmenes) usan un resolutor por clave (`applyTemplate`) que preserva `@@` a través de varias pasadas, con una resolución final de `@@` → `@` al terminar `Compile`.

### Preguntas

Las preguntas se le plantean al usuario durante la instalación del paquete. Cada pregunta tiene un `query` (texto que se muestra), un `type` opcional (tipo de salida para la validación) y un valor `default` opcional. Los nombres de las preguntas deben empezar por un carácter alfanumérico y solo pueden contener caracteres alfanuméricos y guiones bajos (p. ej. `port`, `dbpass`, `registration_secret`). Los guiones, los puntos y otros signos de puntuación se rechazan; los guiones bajos se permiten porque los nombres de las preguntas se usan como marcadores `@plantilla@` y los identificadores de varias palabras como `registration_secret` son habituales en paquetes reales.

#### Tipos de Salida

- **port** -- número de puerto validado (1--65535). Genera automáticamente un puerto libre aleatorio en el rango 10000--60000 cuando la respuesta está vacía o es `"auto"`.
- **hostname** -- alfanumérico en minúsculas con guiones. Genera automáticamente `<nombre-paquete>-<4-hex>` cuando está vacío.
- **volume** -- alfanumérico con guiones y guiones bajos.
- **bytes** -- tamaños en bytes legibles por humanos (sufijos `mb`, `gb`, `tb`).
- **archive** -- nombre de archivo comprimido.
- **duration** -- duraciones de tiempo (sufijos `s`, `m`, `h`, `d`).
- **secret** -- genera automáticamente un valor criptográficamente seguro cuando la respuesta está vacía o es `"auto"`. Genera 32 bytes mediante `crypto/rand`, devueltos como una cadena hexadecimal de 64 caracteres (256 bits de entropía). Apto para contraseñas, sales de claves de cifrado y otros valores secretos. Los usuarios pueden anularlo proporcionando una respuesta explícita.
- **boolean** -- una opción sí/no, renderizada como **casilla de verificación** en el diálogo de preguntas de instalación en lugar de como entrada de texto. La validación es `strconv.ParseBool`, que acepta exactamente las grafías que yaml.v3 (YAML 1.2) trata como booleanas más `1`/`0`/`t`/`f`, sin distinguir mayúsculas; `yes`/`no` **no** se aceptan. La respuesta se normaliza a la cadena `"true"` o `"false"`, de modo que la sustitución `@variable@` y las plantillas de archivo (`{{.Responses.key}}`) siempre ven una forma canónica y pueden comprobarse con `{{if eq .Responses.key "true"}}`.

  Una casilla sin marcar no envía nada, y a menudo la pregunta booleana de una dependencia se queda sin responder por parte de su padre — ambas cosas harían saltar de otro modo la validación de respuesta vacía de `Compile`. Por eso `autoGenerateResponses` (`controller_install_preview.go`) resuelve un booleano ausente o vacío al `default` de la pregunta (normalizado), o a `"false"` cuando no se declara ningún valor por defecto. Un `"false"` explícito del formulario siempre gana sobre un `default: "true"`, de modo que una opción activada por defecto sí puede desactivarse; un `default` que `strconv.ParseBool` no pueda analizar es un error del paquete y hace fallar la instalación en lugar de instalar en silencio con la opción desactivada.

  El diálogo de información del paquete muestra las respuestas booleanas guardadas como Sí/No en lugar de la cadena cruda `"true"`/`"false"`, y las preguntas booleanas se saltan la ruta de valor cacheado/botón de borrar del diálogo de instalación — una respuesta guardada simplemente premarca la casilla y sigue siendo editable directamente.

- **oauth** -- un token obtenido ejecutando un flujo de dispositivo desde el diálogo de instalación, en lugar de escribirse. Se valida como un secreto (cualquier cadena no vacía), nunca se genera automáticamente y se enmascara en el diálogo de información del paquete. El diálogo de instalación renderiza un botón **Conectar** en lugar de un campo de texto; una respuesta cacheada de una instalación anterior se renderiza como ya conectada, de modo que una reinstalación no manda al operador de vuelta al proveedor.

#### Preguntas OAuth

Algunas aplicaciones se configuran con una credencial que solo su proveedor puede acuñar -- un token de cuenta de Plex, un token personal de GitHub -- y la única forma de obtenerla ha sido ejecutar un script de shell a mano y pegar lo que imprimía. Una pregunta `oauth` ejecuta ese flujo desde el diálogo.

**No hay ningún registro de proveedores.** La pregunta lleva un bloque `oauth:` que nombra las URL del propio proveedor, así que un paquete puede usar cualquier proveedor con un flujo tipo dispositivo sin cambiar nada en Town OS:

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

`start` abre el flujo; `extract` nombra los campos JSON que hay que extraer de su respuesta; `approve` es la URL que abre el navegador; `poll` se repite hasta que el campo JSON nombrado por `token` deja de estar ausente o nulo, que es exactamente el aspecto que tiene "el usuario todavía no ha aprobado" sobre el cable. Los marcadores `{{...}}` se resuelven contra los valores extraídos más `{{client_id}}`, un identificador aleatorio por flujo que el controlador envía en cada paso (Plex ata el pin a él). Un número JSON extraído se renderiza como dígitos, no como `1.234567e+06` -- un id de pin formateado como flotante daría 404 en la URL de sondeo y se quedaría colgado para siempre en "pendiente".

El flujo vive en `src/packages/oauth.go` (esquema más validación) y en `src/svc/systemcontroller/controller_oauth.go` (ejecución). `POST /packages/oauth/start` ejecuta el paso de inicio y devuelve `{flow_id, approve_url, user_code, interval_ms}`; `POST /packages/oauth/poll` ejecuta un paso de sondeo y devuelve `pending`, `approved` con el token, o `expired`. Ambos requieren admin. El servidor conserva el flujo solo hasta que se canjea -- el token se entrega al navegador, que lo envía como respuesta de la pregunta igual que cualquier otra, así que guardar una copia en el servidor solo añadiría un segundo sitio por el que podría filtrarse.

La validación viene en dos mitades, y confundirlas es un error. `ValidateOAuthSpec` comprueba la *forma* del flujo (campos obligatorios, duraciones analizables, ninguna plantilla en el host de una URL) y es lo que ejecuta `Compile` cuando se instala un paquete. `ValidateOAuthFlow` es eso más la política de direcciones descrita abajo, y solo se ejecuta cuando un flujo está a punto de *ejecutarse*. Una instalación ocurre mucho después de que su flujo se ejecutara, en un host cuyo ajuste `OAuthAllowPrivate` `Compile` no puede ver — así que aplicar la política de direcciones en tiempo de compilación rechazaría una instalación cuyo propio flujo acababa de tener éxito.

**La guarda de direcciones es estructural.** Un paquete nombra URL arbitrarias y es el *controlador* quien las marca, así que sin una guarda un paquete podría apuntarlo a la propia red del host. `packages.CheckOAuthAddr` se ejecuta en el `DialContext` del cliente HTTP (y en cada redirección) y rechaza las direcciones de loopback, privadas, link-local, multicast, no especificadas y CGNAT; las URL deben ser `https`. Comprobarlo en el momento de la conexión y no en el del análisis es lo que lo hace a prueba de DNS rebinding. `ServerConfig.OAuthAllowPrivate` lo relaja y existe únicamente para que las pruebas puedan apuntar un flujo a un servidor `httptest` en 127.0.0.1.

#### Preguntas opcionales

Cualquier pregunta puede establecer `optional: true`. Todas las demás preguntas deben responderse con un valor no vacío, lo cual no le deja al autor de un paquete ninguna manera de expresar un ajuste del que la aplicación puede prescindir de verdad — un relay SMTP, una clave de API — salvo inventarse un valor por defecto de relleno y confiar en que el operador lo sobrescriba.

Una pregunta opcional puede estar ausente del mapa de respuestas o responderse con una cadena vacía; `Compile` la exime tanto de `ErrMissingResponse` como de `ErrEmptyResponse`, y sustituye la **cadena vacía** en sus puntos `@variable@`. Una respuesta en blanco también se salta `OutputType.Output`, cuyo trabajo es rechazar exactamente eso en una pregunta tipada — una cadena vacía no es un puerto válido — así que `optional` compone con `type`: un puerto opcional que sí se responde se sigue validando como puerto, mientras que uno en blanco se compila a nada.

Dos detalles importan para la corrección. `Compile` sustituye recorriendo las respuestas que recibió, así que una pregunta omitida por completo del mapa recibe una segunda pasada que rellena sus marcadores con la cadena vacía; sin ella, el literal `@smtp_host@` sobreviviría hasta el entorno del contenedor. Y `autoGenerateResponses` se salta las preguntas opcionales antes del switch de tipo: generar un valor derrotaría a la pregunta, ya que un secreto opcional en blanco llegaría si no como una cadena aleatoria de 256 bits con la que la aplicación intentaría diligentemente autenticarse. Una pregunta opcional en blanco recurre a su `default` si declara uno, y a la cadena vacía en caso contrario.

`optional` carece de sentido en un booleano, que es una casilla de verificación y siempre se resuelve a uno de sus dos valores.

#### Preguntas condicionales (`show_if`)

Una pregunta puede llevar `show_if: <pregunta_booleana>`, nombrando una pregunta booleana del mismo paquete. El diálogo de instalación mantiene la pregunta oculta hasta que esa casilla se marca, de modo que un paquete puede esconder un grupo avanzado — un relay SMTP, una clave de API — detrás de un solo interruptor en lugar de enfrentar al operador con todos los campos a la vez.

Es más que una pista de interfaz: el compilador la respeta. Mientras el booleano de control se resuelva a falso, la pregunta condicional compila a la **cadena vacía** y queda exenta del requisito de estar respondida y no vacía — exactamente como si fuera `optional` y se hubiera dejado en blanco — *sin importar lo que enviara el campo que sigue montado*. `questionHidden` (`src/packages/questions.go`) lee el valor de control de la respuesta enviada, recurriendo al `default` declarado del booleano cuando el operador nunca lo tocó, y lo analiza con manga ancha porque una casilla sin marcar puede llegar como `"false"`, `"0"` o no llegar en absoluto. `Compile` fuerza la cadena vacía y se salta `Output()` en una pregunta oculta, así que un valor obsoleto no puede hacer fallar la validación de tipo de un campo que el operador ni siquiera puede ver; una pregunta omitida por completo del mapa de respuestas sigue recibiendo sus puntos `@marcador@` rellenos con la cadena vacía. Cuando el booleano es verdadero, una pregunta condicional no opcional se exige como de costumbre.

`ValidateShowIf` rechaza un `show_if` que referencia una pregunta inexistente (`ErrShowIfUnknown`), una que no es de tipo `boolean` (`ErrShowIfNotBool`), la propia pregunta (`ErrShowIfSelf`) u otra pregunta que a su vez es condicional (`ErrShowIfChain` — nada de cadenas). Una pregunta condicional solo es coherente si lo que controla su visibilidad es una casilla de verificación simple.

### Compilación

La compilación valida todas las respuestas, aplica la validación específica de cada tipo, sustituye todas las variables de plantilla, normaliza las URL de las imágenes de contenedor y produce una estructura `Package` resuelta. En los paquetes de VM, las cadenas de memoria se analizan a recuentos de bytes y se aplican los valores por defecto de CPU. A los comandos posteriores a la actualización se les recortan los espacios en blanco iniciales y finales. Los errores de validación se recopilan y se devuelven juntos.

**Ningún valor que llegue a una unidad de systemd puede llevar un carácter de control.** Un archivo de unidad está orientado a líneas y su entrecomillado no abarca varias líneas: una directiva termina en el primer salto de línea crudo, sin importar qué comillas la encierren. Así que un valor que lleve uno no corrompe su propia línea — todo lo que va después del salto de línea se analiza como una directiva nueva en la misma sección `[Service]`, y un valor de entorno como `algúnvalor\nExecStartPre=/bin/sh -c '…'` añade un `ExecStartPre` a la unidad generada. Eso cruza una frontera de privilegio en lugar de producir meramente una salida incorrecta: el autor de un paquete ya controla la imagen y el comando, que es autoridad sobre lo que se ejecuta *dentro de un contenedor*, mientras que una directiva de systemd se ejecuta en el **host, como root**, antes siquiera de invocar podman.

`packages.ValidateNoControlChars` rechaza todos los controles C0 y DEL. **El tabulador es la única excepción** — es espacio en blanco legítimo, y el tokenizador de systemd lo trata como un separador que el entrecomillado sí contiene de verdad.

La comprobación se ejecuta **dos veces, y ambas pasadas son estructurales**:

- `InputPackage.Validate()` cubre los literales del autor en `environment`, `command` y `entrypoint`. Se ejecuta al *principio* de `Compile`, así que solo ve texto anterior a la sustitución.
- Un barrido sobre el paquete **compilado** al final de `Compile` cubre todo lo posterior a la sustitución: valores de entorno, comando, entrypoint, puntos de montaje de volúmenes y `post_update`. Esta es la pasada que importa. Un valor que en el YAML es un `@marcador@` a secas no lleva ningún carácter de control propio y pasa `Validate()`; el salto de línea llega con la *respuesta*. Una pregunta que no declara `type:` no la valida nada más en absoluto, lo cual convierte la vía de las respuestas en la que realmente alcanza un archivo de unidad con bytes elegidos por quien llama.

`systemd.quoteCommandArg` elimina los mismos caracteres como red de seguridad, porque la generación de unidades no tiene retorno de error y es el último punto antes de que los bytes se escriban en `/etc/systemd/system`. **Descarta** en lugar de escapar: systemd sí resuelve los escapes al estilo C dentro de las comillas, pero apoyar una frontera de seguridad en un detalle del analizador no compra nada cuando no hay ninguna razón legítima para entregar ese byte.

No se rechaza nada que antes funcionara. Un valor de varias líneas ya producía una unidad rota; el cambio es que ahora falla ruidosamente en tiempo de compilación en lugar de generar en silencio una unidad que nadie inspeccionó.

### Comandos Posteriores a la Actualización

El campo `post_update` es una lista de cadenas de comandos de shell que se ejecutan dentro del contenedor en marcha después de que el controlador del sistema detecte un cambio de SHA de imagen durante la reconciliación. Esto permite tareas de migración automatizadas (p. ej., `pg_upgrade` después de que se actualice un contenedor de PostgreSQL).

- **Solo contenedor** -- `post_update` se rechaza durante la validación en paquetes de VM (`ErrPostUpdateVMNotSupported`).
- **Sustitución de plantillas** -- cada comando admite sustitución `@variable@` a partir de las respuestas a las preguntas, igual que los campos de entorno y de red.
- **Recorte de espacios** -- a cada comando se le recortan los espacios iniciales y finales durante la compilación. Los comandos vacíos o compuestos solo de espacios se rechazan durante la validación.
- **Disparador de ejecución** -- los comandos se ejecutan solo cuando `ReconcileConfig.VersionChanged` es verdadero Y el contenido de la unidad de systemd del paquete difiere del de la unidad instalada anteriormente. Si alguna de las dos condiciones es falsa, no se ejecuta ningún comando.
- **Orden de ejecución** -- los comandos se ejecutan secuencialmente después de que terminen todos los reinicios por cambio de versión (primero las unidades NC, luego las dependencias, luego los servicios, luego los comandos posteriores a la actualización). Dentro de un paquete, los comandos se ejecutan en el orden de la lista.
- **Método de ejecución** -- cada comando se ejecuta mediante `podman exec <nombre-contenedor> sh -c '<comando>'` con un tiempo límite de 5 minutos. La función `PostUpdateExec` de `ReconcileConfig` proporciona el mecanismo de ejecución; si es nil, la ejecución posterior a la actualización queda deshabilitada.
- **No fatal** -- los fallos de los comandos se registran pero no detienen la reconciliación ni impiden que se ejecuten los comandos siguientes.

Ejemplo de YAML de paquete:

```yaml
image: postgres:16
post_update:
  - "pg_upgrade --check"
  - "pg_upgrade"
  - "vacuumdb --all --analyze-in-stages"
```

### Plantillas de Archivo

Las plantillas son objetos con nombre en el YAML del paquete con tres campos: `volume` (nombre del volumen destino), `path` (ruta del archivo dentro del volumen) y `content` (cadena de text/template de Go).

El contexto de datos de la plantilla proporciona cuatro espacios de nombres:

- `.Responses.key` -- valores de las respuestas a las preguntas (indexados por el nombre de la pregunta).
- `.Package.Name`, `.Package.Version`, `.Package.Repo`, `.Package.Image`, `.Package.Description` -- metadatos del paquete.
- `.System.Hostname`, `.System.ExternalIP`, `.System.InternalIP` -- información a nivel de sistema.
- `.Dep.KEY.Host` y `.Dep.KEY.Ports` -- coordenadas de ejecución de las dependencias instaladas, indexadas por la misma clave de dependencia que declara el YAML del padre bajo `dependencies:`. `Host` es el nombre del contenedor podman (resoluble mediante el DNS de podman en la red compartida); `Ports` es un `map[string]string` indexado tanto por el puerto numérico del contenedor (p. ej. `"5432"`) como por cualquier nombre semántico declarado en la entrada de red de la dependencia (en minúsculas, p. ej. `"sql"`). Accede a un puerto con nombre mediante `{{index .Dep.db.Ports "sql"}}`. El mapa es nil en paquetes sin dependencias; `{{.Dep.db.Host}}` sobre una dependencia ausente renderiza `<no value>` (como cualquier otra clave de mapa que falte) e `index` sobre unos `Ports` nil da error deliberadamente para que las plantillas mal configuradas fallen ruidosamente.

Los campos `volume` y `path` admiten sustitución `@variable@` (el mismo mecanismo que usan los campos de entorno, red y volúmenes). El campo `content` usa la sintaxis `text/template` de Go con `{{.Responses.key}}`, `{{.Package.Name}}`, `{{.Dep.KEY.Host}}`, etc. La forma de marcador `@dep_*@` NO se respeta dentro de `content` — usa en su lugar el espacio de nombres `.Dep` de las plantillas de Go; `@dep_*@` sigue siendo la forma correcta en los valores de `environment:` y en los bloques `responses:` de las dependencias.

Las plantillas se aplican después de la siembra de volúmenes (archivos, clones git) **y después de que se instale cualquier dependencia**, así que `.Dep` ya está poblado cuando se renderiza el contenido del padre. Durante la reconciliación, las plantillas se vuelven a renderizar pero los archivos existentes nunca se sobrescriben, preservando los datos de las subidas de archivos o de ejecuciones anteriores; el mapa de dependencias se reconstruye a partir de los registros de dependencias persistidos, de modo que `.Dep` sigue resolviendo cuando la reconciliación efectivamente escribe una plantilla que faltaba.

La validación exige: los nombres de las plantillas siguen la convención de nomenclatura de volúmenes (alfanuméricos con puntos, guiones y guiones bajos), las rutas deben ser relativas y sin recorrido de directorios, el volumen debe referenciar un volumen definido del paquete (salvo que el campo de volumen contenga variables de plantilla) y el contenido debe analizarse como `text/template` de Go válido.

### Normalización de Imágenes

Las referencias a imágenes de contenedor se normalizan durante la compilación:
- Un nombre suelto (`nginx`) pasa a `docker.io/library/nginx:latest`.
- Dos componentes (`user/app`) pasan a `docker.io/user/app:latest`.
- Las referencias completas se conservan; se añade `:latest` si no hay etiqueta.

### Persistencia de Respuestas

Las respuestas se guardan por versión en `responses/<repo>/<pkg>/<version>.json`. Se guarda una copia `last` en `responses/last/<repo>/<pkg>.json` para reutilizarla en actualizaciones y reinstalaciones desde volúmenes desinstalados. Las últimas respuestas se borran tras una instalación correcta.

Dos endpoints de la API gestionan las últimas respuestas:

- `POST /packages/last-responses` (requiere admin) -- recupera las últimas respuestas cacheadas de un paquete (por repositorio y nombre).
- `POST /packages/clear-last-responses` (requiere admin) -- elimina el archivo de últimas respuestas cacheadas.

### Interfaz de Preguntas de Instalación

Cuando un usuario instala un paquete, el diálogo de preguntas carga las respuestas existentes (de una instalación actual) y, si no hay ninguna, las últimas respuestas cacheadas (de una desinstalación previa). Las respuestas actuales tienen prioridad sobre las últimas respuestas.

**Las respuestas cacheadas** se muestran como contenedores estilizados de solo lectura con fondo atenuado, mostrando el valor guardado (las contraseñas se muestran como `********`). Una entrada de formulario oculta preserva el valor para el envío. Cada campo cacheado tiene un botón de borrado (icono X) con un tooltip ("Bórralo para introducir un valor nuevo") que, al pulsarlo, sustituye la vista de solo lectura por una entrada editable. El botón de borrado usa un estilo fantasma que se vuelve rojo al pasar el cursor.

**Los valores por defecto** se muestran de dos formas cuando no hay valor cacheado: como texto de marcador de posición en la entrada (p. ej., "Default: 8080") y como texto de ayuda debajo de la entrada, atenuado y con el valor en monoespaciado. Cuando no hay ningún valor por defecto definido se muestran marcadores de posición específicos del tipo: "Auto-assigned if empty" para los puertos, "Auto-generated if empty" para los nombres de host y "e.g. 30s, 5m, 2h, 1d" para las duraciones.

**Los errores de validación** del servidor se muestran por campo como texto rojo debajo de la entrada, y la entrada recibe un borde rojo.

**Dimensionado y paginación.** El diálogo está limitado a la altura del viewport (menos los márgenes) y dispuesto como columna flex, de modo que la cabecera y el pie se quedan quietos mientras el área de preguntas se desplaza — el `overflow-hidden` del `DialogContent` base hacía inalcanzable de otro modo el desbordamiento de un paquete con muchas preguntas. Las preguntas se paginan **5 por página** con controles Anterior/Siguiente que dejan paso al botón Instalar en la última página. Todas las páginas permanecen montadas (las inactivas están en `display:none`) para que las entradas de formulario no controladas conserven los valores escritos y se sigan enviando; desmontar una página tiraría en silencio las respuestas que contiene. Un error de campo salta a la página que lo lleva, así que un error de validación nunca queda escondido detrás del paginador. El paginador reutiliza las cadenas existentes `datatable.next`/`previous` y un contador numérico de páginas, así que no añade claves de traducción.

**Las preguntas condicionales** declaradas con `show_if` están ocultas hasta que se marca su casilla de control (véase [Preguntas condicionales](#preguntas-condicionales-show_if)).

**Las preguntas OAuth** se renderizan a partir de un único estado por pregunta — `idle`, `starting`, `waiting`, `connected`, `error` — sembrado desde la respuesta cacheada, no a partir de "¿existe un token en alguna parte?". Un token cacheado de una instalación anterior hacía que el campo se leyera como conectado antes de que hubiera pasado nada, y lo mantenía así a lo largo de una reconexión fallida, poniendo una insignia verde de Conectado encima de un error rojo. Ahora el token se lee para exactamente una decisión (Conectar frente a Reconectar) y por lo demás es solo lo que envía la entrada oculta: una reconexión fallida le deja al operador el token que ya tenía, pero nada afirma que el intento fallido funcionara, una reconexión todavía en vuelo no se lee como conectada, y una aprobación que no trae token es un error en lugar de un éxito silencioso que instalaría una credencial vacía.

### Diálogo de Información del Paquete

El diálogo de información del paquete muestra las notas como una lista etiquetada. Las notas se renderizan según su tipo: las notas de URL son hipervínculos que se abren en una pestaña nueva (`target="_blank"`), las notas de correo electrónico son enlaces `mailto:` que abren el cliente de correo del usuario y las notas de teléfono son enlaces `tel:`. Las notas sin tipo se renderizan como bloques de código simples, sin enlaces.

### API del Manifiesto de Paquete

`POST /packages/manifest` (requiere autenticación) devuelve la definición YAML cruda de un paquete. Acepta repositorio, nombre y versión. Devuelve el contenido del archivo con `Content-Type: text/x-yaml; charset=utf-8`. Devuelve 404 si el archivo del paquete no existe.

### Menú Desplegable de Acciones del Paquete

En la interfaz de la lista de paquetes, cada fila de paquete tiene un menú desplegable `...` (tanto en la vista plana como en la agrupada por repositorio). El desplegable contiene:

- **Info** (solo paquetes instalados) -- abre el diálogo de información del paquete con las preguntas, las respuestas y las notas compiladas.
- **Manifiesto** -- abre un diálogo que muestra la definición YAML cruda del paquete con un botón de copiar.
- **Versión/Repositorio** -- se muestra como un elemento deshabilitado con la versión y el nombre del repositorio.
- **Desinstalar** (solo paquetes instalados) -- dispara el diálogo de confirmación de desinstalación.

### Paquetes Destacados

Cada repositorio puede incluir un archivo `featured.json` con un array JSON de nombres de paquetes. Los carga `LoadFeatured` y se devuelven junto con la lista de paquetes en `RepoPackageGroup`. La API de lista plana de paquetes establece un booleano `featured` en cada entrada. La API de lista agrupada preserva el array `Featured` de cada grupo incluso cuando el filtrado por búsqueda reduce la lista de paquetes.

- `GET /packages` (requiere autenticación) -- lista paquetes con búsqueda, ordenación, paginación y filtros opcionales `featured_only` e `installed_only`.
- `GET /packages/featured` (requiere autenticación) -- lista los paquetes destacados de todos los repositorios.
- `GET /packages/by-repo` (requiere autenticación) -- lista los paquetes agrupados por repositorio. Acepta los parámetros de consulta `search` y `featured_only`.

#### Filtro de Paquetes Destacados

La API de lista plana de paquetes (`GET /packages`) y la API de lista agrupada (`GET /packages/by-repo`) aceptan un parámetro de consulta `featured_only`. Cuando vale `"true"`, solo se devuelven los paquetes marcados como destacados. El filtro se cruza con `installed_only` -- ambos pueden estar activos a la vez. En la interfaz, una casilla "Featured only" activa el filtro. El estado predeterminado del filtro de destacados es `true` (mostrando solo los paquetes destacados en la primera visita). Las preferencias de filtro (`pkg_group_by_repo`, `pkg_installed_only`, `pkg_featured_only`) se persisten en `localStorage`.

### Filtro de Paquetes Instalados

La API de lista plana de paquetes (`GET /packages`) acepta un parámetro de consulta `installed_only`. Cuando vale `"true"`, solo se devuelven los paquetes instalados. El filtrado se aplica en el servidor antes de la búsqueda, la ordenación y la paginación, garantizando recuentos de páginas y desplazamientos correctos. En la interfaz, una casilla "Installed only" activa el filtro y reinicia la paginación a la primera página.

### Instalación y Desinstalación de Paquetes

#### API de Instalación

`POST /packages/install` (requiere admin) instala un paquete. Acepta repositorio, nombre, versión, respuestas y flags opcionales:

- `reuse_volumes` -- reutiliza los volúmenes de una versión desinstalada anterior.
- `import_from_version` -- importa los volúmenes de una versión anterior concreta.
- `skip_response_reuse` -- no autocompletar las respuestas de instalaciones anteriores.

La instalación crea un enlace duro desde el archivo de paquete del repositorio al directorio de instalados, persiste las respuestas, crea los volúmenes con cuotas y UID/GID opcionales, siembra los volúmenes desde archivos y git (solo runtime de contenedor), aplica las plantillas de archivo, genera los archivos de unidad de systemd, escribe los archivos de estado de red, instala y arranca las unidades de systemd, y borra las últimas respuestas en caso de éxito. Las últimas respuestas se guardan antes de instalar para poder recuperarlas en la desinstalación. En los paquetes de VM, la imagen de disco de la VM se descarga y se convierte a formato raw (si es una URL remota) antes de generar las unidades; la siembra de volúmenes (archivos, clones git) se omite.

#### API de Desinstalación

`POST /packages/uninstall` (requiere admin) desinstala un paquete. Acepta repositorio, nombre, versión y flags opcionales:

- `purge_volumes` -- elimina de inmediato todos los volúmenes asociados.

Cuando no se purga, los volúmenes se mueven del prefijo `installed/` al prefijo `uninstalled/`. El archivo de estado de red se elimina y las unidades de systemd se detienen, se deshabilitan y se desinstalan.

**Cascada de dependencias.** Desinstalar un paquete padre desinstala recursivamente todas las dependencias que posee. La cascada lee los registros de dependencias persistidos (`LoadDependencies`) del padre y recorre cada hijo en profundidad, repitiendo la búsqueda en cada nivel para que las subdependencias anidadas (`padre--dep--hijo--dep--nieto`) también se eliminen. Para cada dependencia, la cascada desregistra sus registros DNS, desinstala sus unidades de systemd (servicio + NC + sockets), elimina su archivo de estado de red, llama a `inst.Uninstall` para descartar el registro de instalación y, o bien purga sus volúmenes (cuando `purge_volumes` está establecido), o bien los mueve al prefijo `uninstalled/`. La cascada está implementada en `uninstallDependencies` (`src/svc/systemcontroller/controller_install_dependencies.go`) y se ejecuta después de completarse la desinstalación del propio padre. No hay recuento de referencias: cada dependencia pertenece exactamente a un padre (su registro de instalación vive en `installed/<repo>/<padre--dep--clave>/`), así que una dependencia compartida instalada bajo dos padres tiene dos registros independientes, y desinstalar un padre solo elimina su propia copia.

#### Información del Paquete Instalado

`POST /packages/installed/info` (requiere autenticación) devuelve las preguntas, las respuestas, las notas compiladas y los tipos de nota de un paquete instalado.

**Una cuenta que no es de administrador obtiene las notas y nada más.** La ruta sigue siendo `requireAuth` porque el panel renderiza las notas de todos los servicios instalados para todas las cuentas — para eso están las notas — pero una pregunta `type: secret` se responde con una credencial generada y una `type: oauth` con un token de proveedor, así que devolver el mapa completo de respuestas a cualquiera con un inicio de sesión le entregaría las credenciales de todos los paquetes. Las preguntas también se retienen: el `query` de una pregunta es inofensivo, pero emparejarlo con un mapa de respuestas censurado solo anuncia lo que se está ocultando, y la única pantalla que renderiza preguntas es el diálogo de instalación, exclusivo de administradores. Descartar el mapa no basta por sí solo — una nota se compila a partir de esas mismas respuestas, así que `redactSecretsInNotes` enmascara cualquier respuesta de tipo secreto u oauth que una nota citara, haciendo coincidir por valor para que una nota que nunca cita ninguna quede completamente intacta. Las respuestas de menos de seis caracteres se dejan en paz: un secreto de dos caracteres no es una credencial que nadie haya elegido, y enmascarar todas sus apariciones destrozaría texto de notas no relacionado.

#### Versiones de Paquete

`POST /packages/versions` (requiere autenticación) lista las versiones disponibles de un paquete por nombre.

#### Preguntas del Paquete

Dos endpoints recuperan las preguntas de un paquete:

- `POST /packages/questions` (requiere admin) -- obtiene las preguntas por nombre de paquete (última versión).
- `POST /packages/questions/identity` (requiere admin) -- obtiene las preguntas por repositorio, nombre y versión.

### Manejo de Zonas Horarias

La interfaz mantiene una copia estática de los nombres de zona horaria IANA más comunes con una utilidad `getTimezoneOffsetMinutes()` que calcula los desplazamientos UTC en el cliente mediante la API `Intl` del navegador. El servidor expone el desplazamiento UTC del sistema local en minutos a través de la respuesta del ping de estado.

### Previsualización de Instalación

- `POST /packages/install-preview` (requiere autenticación) -- previsualiza lo que se crearía si se instalara un paquete. Acepta repositorio, nombre y versión. Devuelve repositorio, nombre, versión, descripción, imagen, volúmenes, puertos, información de actualización, tipo de runtime y si el paquete tiene preguntas. En los paquetes de VM, la previsualización incluye además la configuración de la VM (URL de la imagen, memoria legible por humanos y número de CPU).

### Hijos de un Paquete

- `POST /packages/children` (requiere autenticación) -- lista los nombres de los paquetes hijos de un repositorio y un nombre de paquete dados.

### Listado de Volúmenes Desinstalados

- `POST /packages/uninstalled-volumes` (requiere autenticación) -- comprueba si un paquete tiene volúmenes sobrantes de una desinstalación anterior. Devuelve si existen volúmenes desinstalados, la lista de versiones desinstaladas y la lista de versiones instaladas.

### Gestión de Paquetes Instalados

- `GET /packages/installed` (requiere autenticación) -- lista todos los paquetes instalados con búsqueda, ordenación y paginación.
- `POST /packages/responses` (requiere admin) -- obtiene las respuestas guardadas de un paquete instalado por repositorio, nombre y versión.
- `POST /packages/purge-volumes` (requiere admin) -- elimina permanentemente los volúmenes de un paquete instalado.

### Habilitar/Deshabilitar Paquetes

- `POST /packages/disable` (requiere admin) -- deshabilita un paquete. Establece la marca de deshabilitado y detiene todos los servicios de systemd asociados.
- `POST /packages/enable` (requiere admin) -- vuelve a habilitar un paquete deshabilitado. Borra la marca de deshabilitado y arranca todos los servicios de systemd asociados.

La interfaz `Installer` admite `SetDisabled`, `IsDisabled` e `IsPackageChanged` además de los métodos centrales `Install`, `Uninstall`, `ListInstalled` y `GetResponses`.

### Gestión de Volúmenes Desinstalados

- `POST /packages/purge-uninstalled-volumes` (requiere admin) -- elimina permanentemente todos los volúmenes desinstalados de un paquete.

## Almacenamiento

El almacenamiento usa subvolúmenes btrfs con aplicación de cuotas.

### Separación de responsabilidades: volúmenes vs. almacenamiento de objetos

**La capa de almacenamiento gestiona volúmenes. gfeh proporciona el almacenamiento de objetos. La capa de almacenamiento no maneja en absoluto el almacenamiento de objetos -- gfeh es el responsable.**

`src/storage` crea, redimensiona, renombra, hace instantáneas y elimina subvolúmenes btrfs, e informa del uso de disco. Ese es todo su cometido. Nunca debe aprender qué es un objeto, un bucket, una clave, un manejador de archivo, un identificador de contenido (CID), una ACL, una compartición o una vista de protocolo. Para la capa de almacenamiento, un subvolumen es una arena opaca de bytes con una cuota.

gfeh (`gitea.com/town-os/gfeh`, un servicio de sistema en Rust distribuido como `town-os-system--gfeh`) es el dueño de todo lo que está por encima de esa línea: el espacio de nombres de objetos, los metadatos y permisos por archivo, la base de datos jerárquica de usuarios/ACL, la compartición, la exposición HTTP por archivo, la federación con servicios externos y todas las vistas de protocolo (S3, IPFS, Google Drive, HTTP simple; SMB/CIFS existe en gfehd pero [Town OS no lo sirve](#sin-vista-smb)). Consume la capa de almacenamiento únicamente para aprovisionar y redimensionar los subvolúmenes en los que viven sus particiones, y luego hace su propia E/S directa sobre el subárbol montado por bind.

Consecuencias que hay que respetar al cambiar cualquiera de los dos lados:

- **No** añadas endpoints de objetos, blobs, clave/valor o por archivo a `src/storage` ni a la API `/storage/*`. Si una funcionalidad necesita direccionar archivos individuales, pertenece a gfeh. Los endpoints existentes `upload-archive`/`download-archive` son un transporte tar para sembrar volúmenes, no una API de objetos, y no deben crecer en esa dirección.
- **No** le enseñes a `storage.Storage` ni a `storage.Controller` qué son los usuarios, los permisos o los protocolos. La cuota es la única política que aplica la capa de almacenamiento.
- Las particiones de gfeh viven bajo el prefijo de subvolumen reservado `gfeh/`. Se aprovisionan mediante `CreateFilesystem`/`ModifyFilesystem` de `storage.Storage` **en proceso**, no a través de la API HTTP `/storage/*`: `createFilesystem` reescribe incondicionalmente todos los nombres enviados a `user/<nombre>` (`controller_storage.go`), así que esa ruta no puede producir un volumen bajo ningún otro prefijo. Por eso el aprovisionamiento de particiones necesita sus propios manejadores `/gfeh/partitions/*`, lo cual además mantiene en un solo sitio la aplicación de prefijos reservados, la política de cuotas y el registro de auditoría, en lugar de duplicarlos en gfeh.

- **gfeh depende de un contrato escrito, y los cambios aquí pueden romperlo.** `TOWNOS_CONTRACT.md`, en el repositorio de gfeh, enumera todas las rutas, comportamientos e invariantes que gfeh espera de Town OS -- la reescritura a `user/`, las reglas de prefijos reservados, los códigos de estado de `/gfeh/partitions/*`, los fallos de autenticación indistinguibles y el significado de "falla en cerrado" de un `Account.Networks` vacío -- y fija la revisión de Town OS contra la que se verificó. gfeh emula ese contrato para que sus pruebas puedan ejecutarse sin root, systemd, podman ni btrfs.

  **Al cambiar `src/storage`, `src/account` o las rutas del controlador del sistema, vuelve a ejecutar `make check-townos-sync` en el checkout de gfeh.** Un emulador desviado le da a gfeh una suite de pruebas en verde y un despliegue roto. Reconcilia el emulador y el documento del contrato a la vez; nunca uno sin el otro.

El lado de Town OS de esa integración — las rutas de particiones, los demonios por red, el socket de administración y cómo llegan los nombres al DNS y al ingress — es [Almacenamiento de Objetos (gfeh)](#almacenamiento-de-objetos-gfeh).

### Operaciones del Sistema de Archivos

La interfaz `Storage` proporciona:

- **CreateFilesystem** -- crea un subvolumen btrfs nuevo con cuota opcional.
- **ModifyFilesystem** -- cambia el nombre y/o la cuota de un volumen.
- **RemoveFilesystem** -- elimina un volumen.
- **ListFilesystems** -- lista volúmenes con filtrado por prefijo y estado (`user`, `installed`, `uninstalled`), ordenación, paginación y búsqueda. Devuelve una lista vacía (no un error) cuando no se encuentra el montaje btrfs.
- **RenameFilesystem** -- renombra un volumen.
- **SnapshotFilesystem** -- crea una instantánea btrfs.
- **DiskUsage** -- informa de las estadísticas de uso de disco.

Las cuotas se aplican a nivel de qgroup de btrfs. Una cuota de 0 significa ilimitada.

### API de Almacenamiento

- `POST /storage/create` (requiere autenticación) -- crea un sistema de archivos de usuario nuevo con nombre y cuota opcional.
- `POST /storage` (requiere autenticación) -- lista los sistemas de archivos con filtrado por prefijo y estado, ordenación, paginación y búsqueda.
- `POST /storage/modify` (requiere autenticación) -- modifica el nombre y/o la cuota de un volumen. Renombrar solo se permite en los sistemas de archivos de usuario; los volúmenes de paquete no pueden renombrarse.
- `POST /storage/remove` (requiere autenticación) -- elimina un sistema de archivos de usuario.
- `POST /storage/package-volumes` (requiere autenticación) -- lista los volúmenes de paquete agrupados por paquete, con inclusión opcional de los volúmenes desinstalados.
- `POST /storage/remove-package-volume` (requiere admin) -- elimina un volumen de paquete concreto por su nombre interno.
- `POST /storage/remove-package-volume-group` (requiere admin) -- borrado en cascada detrás de los botones de borrado de los nodos no hoja del árbol de almacenamiento. `repo` y `name` son obligatorios; un `version` vacío apunta a todas las versiones instaladas del paquete. **Todas las unidades de systemd del árbol de dependencias del paquete objetivo se detienen antes de eliminar ningún subvolumen**, así que un contenedor podman que aún tenga un volumen abierto no puede competir con el borrado de btrfs. `include_uninstalled` barre además el subárbol `uninstalled/` correspondiente (conectado al mismo conmutador "Mostrar desinstalados" que gobierna el listado de volúmenes).
- `POST /storage/upload-archive` (requiere admin) -- sube y desempaqueta un archivo comprimido en un volumen.
- `POST /storage/download-archive` (requiere admin) -- descarga un volumen como archivo comprimido.

### Espacios de Nombres de Volúmenes

- **Volúmenes de usuario** -- `user/<nombre>` en disco. El prefijo `user/` lo anteponen de forma transparente los manejadores de creación, eliminación, modificación y listado, y se elimina en las respuestas de la API para que quien consume la API vea solo el nombre desnudo. El subvolumen raíz `user` lo crea la reconciliación en el arranque.
- **Volúmenes de paquetes instalados** -- `installed/<repo>/<nombre>/<versión>/<nombrevol>`.
- **Volúmenes de paquetes desinstalados** -- `uninstalled/<repo>/<nombre>/<versión>/<nombrevol>`.
- **Almacenamiento de archivos comprimidos** -- prefijo `archives/` (gestionado por el sistema).
- **Imágenes de VM** -- subvolumen `vm-images/` (gestionado por el sistema). Guarda las imágenes de disco raw de VM en caché.
- **Particiones de almacenamiento de objetos** -- `gfeh/<red>`, una por red de Town OS, propiedad del uid/gid 2000. Reservadas: `/storage/create` no puede producir una (reescribe todos los nombres a `user/<nombre>`), así que se aprovisionan a través de [`/gfeh/partitions/*`](#protocolo-1-aprovisionamiento-de-particiones-gfehpartitions).

Todos los nombres de raíz de prefijo (`installed`, `uninstalled`, `archives`, `pages`, `vm-images`, `user`, `gfeh`) están reservados y los usuarios no pueden crearlos, modificarlos ni eliminarlos directamente. La subida y la descarga de archivos comprimidos resuelven los nombres de subvolumen que carecen de un prefijo interno anteponiendo `user/`.

**Un prefijo no es una frontera salvo que el nombre que va después no pueda salirse trepando.** `filepath.Join` colapsa `..`, así que `../gfeh/home` enviado a un manejador que antepone `user/` se convierte en `user/../gfeh/home` y direcciona la partición de almacenamiento de objetos de otra red — y además se cuela por delante de la comprobación de nombres reservados, que compara contra un prefijo inicial que el recorrido todavía no lleva. Por eso `storage.ValidateFilesystemName` (sin barra inicial, sin bytes nulos, sin componentes vacíos ni `.`/`..`, y con un juego de caracteres restringido) se aplica a **ambos** nombres en `ModifyFilesystem` — validar solo el destino del renombrado permitía que quien llamara moviera el subvolumen de otra persona a su propio espacio de nombres — y en `RemoveFilesystem`, que no validaba nada en absoluto y es la operación destructiva. Los manejadores de `/storage/*` validan el nombre enviado **antes** de anteponer `user/`, que es lo que hace que la comprobación de nombres reservados signifique lo que parece decir. Estas rutas son `requireAuth`, no `requireAdmin`, así que esto era alcanzable por cualquier cuenta corriente del equipo.

El prefijo del **listado** está exento a propósito: `nest/` es la forma en que quien llama pide todo lo que hay bajo `nest`, nada lo une a una ruta del sistema de archivos (la capa de almacenamiento lista desde su propia base y lo usa como filtro de cadena) y `user/` se antepone incondicionalmente, así que un prefijo con recorrido no coincide con nada en lugar de alcanzar algo.

### Detección del Formato de Archivo Comprimido

El formato de compresión del archivo se detecta inspeccionando los bytes mágicos al inicio del flujo de subida. Se atisban los primeros 6 bytes mediante un lector con búfer y se comparan con las firmas conocidas:

- **gzip** -- `0x1f 0x8b`
- **bzip2** -- `0x42 0x5a 0x68` (`BZh`)
- **xz** -- `0xfd 0x37 0x7a 0x58 0x5a 0x00` (`\xfd7zXZ\x00`)

Las firmas no reconocidas se rechazan de inmediato. La extensión del nombre de archivo también se valida de forma independiente para confirmar el formato.

### Validación del Flujo del Archivo Comprimido

Tras detectar el formato, el flujo descomprimido se valida como archivo tar usando `io.TeeReader`. Un lado del tee alimenta al lector `archive/tar` de Go para validar las cabeceras tar; el otro lado alimenta al proceso real de desempaquetado `tar -xf`. Si la validación detecta un flujo tar inválido, el desempaquetado se interrumpe. La descompresión usa implementaciones paralelas cuando están disponibles: `pigz` para gzip, `lbzip2` para bzip2 y `xz` para xz.

### Subida de Archivos Comprimidos

`POST /storage/upload-archive` (requiere admin) acepta un formulario multipart:

- `subvolume` (obligatorio) -- ruta del subvolumen destino.
- `archive` (obligatorio) -- archivo comprimido. Formatos admitidos: `.tar`, `.tar.gz`/`.tgz`, `.tar.bz2`/`.tbz2`, `.tar.xz`/`.txz`.
- `subpath` (opcional) -- ruta relativa dentro del volumen para el desempaquetado; se crea bajo demanda.
- `stop_service` (opcional) -- nombre de la unidad de systemd que hay que detener antes de desempaquetar y reiniciar al terminar.

Los archivos se transmiten directamente sin archivos temporales. El recorrido de rutas se valida tras el desempaquetado (resolución de enlaces simbólicos). El tamaño máximo de subida es de 1 GB por defecto (ajuste `max_archive_size`). El tiempo límite de desempaquetado es de 600 segundos por defecto (ajuste `archive_unpack_timeout`).

### Descarga de Archivos Comprimidos

`POST /storage/download-archive` (requiere admin) acepta un cuerpo JSON:

- `subvolume` (obligatorio) -- ruta del subvolumen origen.
- `paths` (opcional) -- array de rutas concretas dentro del subvolumen que hay que incluir.
- `stop_service` (opcional) -- nombre de la unidad de systemd que hay que detener durante el archivado y reiniciar después.
- `format` (opcional) -- formato de compresión: `tar.gz` (predeterminado), `tar.bz2` o `tar.xz`.
- `filename` (opcional) -- nombre base personalizado para el archivo descargado. El servidor sanea el valor (elimina separadores de ruta y caracteres de control), quita cualquier extensión de archivo comprimido existente para evitar duplicarla y añade la extensión adecuada al formato elegido. Por defecto es `download` cuando no se proporciona o cuando el saneado produce una cadena vacía.

Devuelve un archivo comprimido transmitido en el formato solicitado. La compresión usa `pigz`, `lbzip2` o `xz` respectivamente. Las cabeceras Content-Type y el nombre de archivo de Content-Disposition se establecen para coincidir con el formato elegido y el nombre personalizado. Cuando se proporciona `paths`, solo se incluyen las rutas coincidentes.

### Autoarchivado desde Imágenes de Contenedor

Las definiciones de paquete pueden incluir una sección `archives` que referencia imágenes de contenedor. Durante la instalación y la reconciliación, los volúmenes vacíos se pueblan descargando la imagen, creando un contenedor temporal y copiando el directorio indicado en el volumen.

### Siembra de Volúmenes desde Git

Los volúmenes pueden especificar un campo `git` con la URL de un repositorio. Durante la instalación y la reconciliación, los volúmenes vacíos se siembran clonando el repositorio (tiempo límite de 5 minutos). La URL puede referenciar variables de plantilla, lo que permite a los usuarios anular el repositorio mediante la respuesta a una pregunta. Los datos existentes nunca se sobrescriben. Los fallos de clonado se registran y se omiten (no fatales).

### Reconstrucción del Origen Git

`POST /packages/rebuild-git` (requiere admin) actualiza los volúmenes sembrados desde git de un paquete instalado. Trae los últimos cambios de cada volumen git mediante go-git y después reinicia el servicio dependiente. Requiere el repositorio, el nombre y la versión del paquete. Las variables de plantilla se vuelven a evaluar contra las respuestas guardadas antes de reconstruir.

### Gestión de Imágenes de VM

Los paquetes de VM requieren imágenes de disco en formato raw. Las imágenes remotas se descargan y se convierten mediante `qemu-img convert -O raw`; el archivo `.raw` convertido se cachea en el subvolumen `vm-images`. Las instalaciones posteriores reutilizan la imagen cacheada. Las referencias a imágenes locales se resuelven directamente desde el subvolumen `vm-images`.

- `GET /vm-images` (requiere autenticación) -- lista las imágenes de disco de VM cacheadas. Devuelve el nombre y el tamaño de archivo de cada imagen.
- `POST /vm-images/upload` (requiere admin) -- descarga una imagen de VM desde una URL y la convierte a formato raw. Acepta una URL y un nombre opcional. Por defecto, el nombre es el del archivo de la URL con extensión `.raw`. Las descargas tienen un tiempo límite de 30 minutos. La imagen convertida se guarda en el subvolumen `vm-images`.
- `POST /vm-images/delete` (requiere admin) -- elimina una imagen de VM cacheada por nombre.

### Recorte del Nombre a Mostrar

Las respuestas de la API para los volúmenes de paquetes instalados y desinstalados recortan el segmento inicial del repositorio de la ruta (p. ej., `default/nginx/2.0/data` pasa a ser `nginx/2.0/data`). La ruta completa en disco se preserva en un campo `internal_name` para las operaciones que la necesitan (p. ej., derivar el nombre del servicio de systemd para detener/arrancar durante las operaciones con archivos comprimidos).

### Interfaz de Almacenamiento

La pantalla de gestión del almacenamiento tiene dos secciones:

**Sistemas de archivos de usuario** -- una tabla de datos paginada, ordenable y con búsqueda. Cada fila tiene botones de Modificar (nombre y cuota) y Eliminar. El diálogo de creación precarga el campo de cuota a partir del ajuste `default_quota` del sistema.

**Volúmenes de paquete** -- un árbol jerárquico organizado por paquete. Cada paquete es un encabezado de árbol plegable que muestra: el recuento total de volúmenes, el recuento de versiones, la cuota agregada y las insignias de estado de instalación. Cuando un paquete tiene varias versiones, se muestran subencabezados por versión con la cuota y el estado de cada una. Los volúmenes desinstalados se incluyen cuando el conmutador "Mostrar volúmenes desinstalados" está activo.

Cada fila de volumen hoja muestra la cuota y el estado, y ofrece tres acciones:

- **Descargar** (botón de icono) -- abre un diálogo con un campo opcional de nombre de archivo (nombre base del archivo descargado; la extensión se añade automáticamente), un selector de formato de compresión (gzip, bzip2, xz), un filtro opcional de rutas separadas por comas y una casilla para detener el servicio dependiente durante la descarga. Usa la File System Access API para guardado en streaming, con un respaldo de descarga por blob.
- **Subir** (botón de icono) -- abre un diálogo para seleccionar un archivo comprimido (`.tar`, `.tar.gz`, `.tgz`, `.tar.bz2`, `.tbz2`, `.tar.xz`, `.txz`) con una subruta opcional para la extracción y una casilla para detener el servicio dependiente durante la subida.
- **Modificar** (botón) -- abre un diálogo que muestra el nombre del volumen, el estado y el nombre del servicio asociado, con un campo para cambiar la cuota. El campo de nombre no es editable para los volúmenes de paquete.

## Pages

Pages es una funcionalidad de alojamiento de sitios estáticos que admite tres tipos de origen de contenido: subidas de archivos comprimidos, imágenes de contenedor y repositorios git. Los usuarios asignan un dominio o subdominio, y el sistema sirve el contenido mediante un contenedor Caddy. Las actualizaciones se disparan manualmente mediante reconstrucción o nueva subida.

### Modelo de Datos

Cada sitio de pages tiene: un nombre único (clave primaria), un tipo de origen (`archive`, `container_image` o `git`; predeterminado: `archive`), la URL del repositorio (obligatoria para git), la rama (por defecto `main`), la referencia a la imagen de contenedor (obligatoria para container_image), el directorio de la imagen (obligatorio para container_image), el dominio (por defecto el nombre de la página), el estado (`pending`, `active` o `error`), una **red** y marcas de tiempo de creación/actualización. Las páginas se guardan en una tabla SQLite.

`Network` es la red de publicación de la página, exactamente igual que la red de instalación de un paquete: selecciona el TLD bajo el que se nombran el nombre de host de la página, el SAN de la hoja, el propietario DANE TLSA y el vhost del ingress, y decide quién puede resolver la página. Vacía — el valor cero y el predeterminado de la base de datos — significa la red predeterminada/del hogar, la misma convención que `Installer.LoadNetwork` para los paquetes. Véase [Las páginas también están acotadas por red](#las-páginas-también-están-acotadas-por-red). Se acepta al crear y es uno de los campos de actualización parcial.

El contenido de pages se guarda en subvolúmenes btrfs bajo un prefijo `pages/`. Cada página obtiene un subvolumen en `pages/{nombre}` y un enlace simbólico en `pages-webroot/{nombre}` que apunta a `/data/pages/{nombre}`. El prefijo `pages` está reservado y no puede renombrarse ni eliminarse mediante la API general de almacenamiento.

### API de Pages

Todos los endpoints de mutación requieren autenticación de administrador; el endpoint de listado requiere autenticación normal.

- `POST /pages/create` (requiere admin) -- crea una página nueva. Acepta nombre, tipo de origen, URL del repositorio, rama, dominio, imagen de contenedor y directorio de la imagen. El tipo de origen es `archive` por defecto. La validación varía según el tipo de origen: git requiere la URL del repositorio; la imagen de contenedor requiere tanto la imagen como el directorio de la imagen. Crea un subvolumen btrfs y el enlace simbólico del webroot. Las páginas de git y de imagen de contenedor se aprovisionan de forma asíncrona (clonado o extracción de la imagen); el estado pasa de `pending` a `active` si todo va bien, o a `error` si falla. Las páginas de archivo se quedan en estado `pending` hasta que se sube contenido mediante `/pages/upload`. Si no se proporciona dominio, se usa el nombre de la página.
- `POST /pages/upload` (requiere admin) -- sube contenido para una página de tipo archivo. Acepta un formulario multipart con `name` y el archivo `archive`. Solo es válido para páginas con tipo de origen `archive`; devuelve 400 para los demás tipos. Usa la misma detección de formato por bytes mágicos, validación de extensión y validación de flujo que las subidas de archivos de almacenamiento. Desempaqueta directamente en el subvolumen btrfs de la página. Establece el estado a `active` si va bien o a `error` si falla.
- `POST /pages/update` (requiere admin) -- actualización parcial de la URL del repositorio, la rama, el dominio, el tipo de origen, la imagen de contenedor o el directorio de la imagen de una página. Solo se cambian los campos proporcionados.
- `POST /pages/remove` (requiere admin) -- elimina una página de la base de datos, quita el enlace simbólico del webroot y elimina el subvolumen btrfs.
- `POST /pages/rebuild` (requiere admin) -- el comportamiento varía según el tipo de origen: las páginas de git traen los últimos cambios (o hacen un clonado nuevo si falta `.git`); las páginas de imagen de contenedor vuelven a extraerse de la imagen mediante podman; las páginas de archivo devuelven 400 (hay que volver a subir mediante `/pages/upload`).
- `GET /pages` (requiere autenticación) -- lista todas las páginas con ordenación, búsqueda y paginación. Ordenable por nombre, URL del repositorio, rama, dominio, tipo de origen, estado y marcas de tiempo.

### Interfaz de Pages

La pantalla de gestión de pages muestra una tabla de datos paginada, ordenable y con búsqueda, con columnas para el nombre, el dominio, el tipo de origen, la URL del repositorio, la rama y el estado. El tipo de origen se muestra como insignia. El estado se muestra como una insignia con código de color (predeterminada para activa, roja para error, secundaria con un icono de carga girando y el texto "Provisioning..." para pendiente).

El diálogo de creación tiene un desplegable de tipo de origen arriba (Archive Upload / Container Image / Git Repository, predeterminado: Archive Upload). Los campos cambian dinámicamente según el tipo de origen seleccionado: git muestra la URL del repositorio y la rama; la imagen de contenedor muestra la referencia a la imagen y el directorio de la imagen; el archivo muestra una entrada opcional de subida de archivo. En las páginas de git y de imagen de contenedor, enviar el formulario dispara el aprovisionamiento: todas las entradas se deshabilitan, el botón de envío muestra un indicador de carga con el texto "Provisioning..." y el diálogo no puede cerrarse. La interfaz consulta el estado de la página cada 2 segundos durante un máximo de 60 segundos. En las páginas de archivo con un fichero seleccionado, la subida ocurre de forma síncrona tras la creación.

Las acciones por fila varían según el tipo de origen: las páginas de archivo muestran un botón de Subir; las de git y las de imagen de contenedor muestran un botón de Reconstruir (con confirmación). Todas las páginas tienen acciones de Editar y Eliminar. El diálogo de edición muestra los campos apropiados al tipo de origen de la página.

## Almacenamiento de Objetos (gfeh)

gfeh es la mitad de almacenamiento de objetos de la división descrita en [Separación de responsabilidades](#separación-de-responsabilidades-volúmenes-vs-almacenamiento-de-objetos): `src/storage` es dueño de los subvolúmenes btrfs y las cuotas, gfeh es dueño de los objetos, los permisos por archivo, el bosque de usuarios/ACL, la compartición y todas las vistas de protocolo. Esta sección es el lado de Town OS de esa frontera — cómo se despliegan los demonios y todos los protocolos que la cruzan.

`gfehd` es un binario en Rust publicado en crates.io y empaquetado aquí como `quay.io/town/gfeh` (`Containerfile.gfeh`), porque el propio repositorio de gfeh no distribuye ninguna imagen. Es **un proceso por partición**, no un único demonio multiinquilino.

### Forma del despliegue: una partición por red

Una **partición** es un subvolumen btrfs, un proceso `gfehd`, un socket de administración y **su propio conjunto de usuarios**. Hay exactamente una por red de Town OS, así que el espacio de nombres del almacenamiento de objetos queda particionado por la misma frontera que particiona el DNS y WireGuard: un principal, una concesión y una exposición en la partición `office` no significan nada en `home`.

| Cosa | Ubicación |
|---|---|
| Datos de la partición | `<btrfsBase>/gfeh/<red>` → contenedor `/data/<red>` |
| Configuración | `<btrfsBase>/gfeh-control/<red>/gfehd.yaml` → `/etc/gfeh/gfehd.yaml` |
| Socket de administración | `<btrfsBase>/gfeh-control/<red>/run/admin.sock` → `/run/gfeh/admin.sock` |
| Unidad | `town-os-system--gfeh-<red>.service` |

Los ayudantes de rutas viven en `src/gfeh/layout.go` — `PartitionVolume`, `ConfigPath`, `SocketPath`, `ServiceKey`, `NetworkFromKey` — y son el único sitio donde se componen estas cadenas.

El socket se asienta en el btrfs porque es el único sistema de archivos que pueden ver tanto el contenedor de gfehd como el del systemcontroller; es el mismo truco que usa `ingressctl` para su socket gRPC. gfehd se ejecuta como **uid/gid 2000** (`gfeh.UID`/`gfeh.GID`), y un bind mount pasa la propiedad del host tal cual, así que al subvolumen de la partición se le hace chown a ese uid en el momento de crearlo — que es la razón de que `storage.Filesystem` lleve `UID`/`GID` opcionales y de que `storage.Controller` tenga `Chown`. No recursivo, por la misma razón que el chown de `HostVolumeMount`: el demonio crea sus propios hijos con su propio uid, así que ya tienen la propiedad correcta y nunca se desvían.

**Puertos.** Las cuatro vistas HTTP enlazan **puertos de contenedor fijos e idénticos en todas las particiones** — s3 9000, http 9001, drive 9002, ipfs 9003 — y **no publican ningún puerto del host**. Eso es seguro precisamente porque no publican ninguno: cada contenedor tiene su propio netns y el ingress lo alcanza por el nombre de contenedor, exactamente igual que alcanza a un paquete. Dos particiones que sirvan S3 en el 9000 no pueden colisionar, ni siquiera bajo un `make test-full` concurrente.

**Ninguna partición publica ningún puerto del host**, porque SMB — la única vista que necesitaría uno, al no ser HTTP ni poder ponerse tras el ingress — [no se sirve](#sin-vista-smb). `DefaultSMBPortBase` (`4450`) y `GFEH_SMB_PORT_BASE` sobreviven sin usarse, así que el ajuste del arnés se queda inofensivo por si la vista vuelve algún día.

### Protocolo 1: aprovisionamiento de particiones (`/gfeh/partitions/*`)

Estas cuatro rutas existen porque `createFilesystem` reescribe incondicionalmente todos los nombres enviados a `user/<nombre>`, así que `/storage/create` **no puede** producir un volumen bajo el prefijo `gfeh/`. Están declaradas en `TOWNOS_CONTRACT.md` y el cliente Rust de gfeh analiza exactamente estas formas, así que **un cambio aquí es un cambio de contrato, no una refactorización**. `make check-townos-sync` en el checkout de gfeh es lo que detecta la deriva; `controller_gfeh_partitions_test.go` fija las formas del cable en este lado.

| Ruta | Autenticación | Petición | Respuesta |
|---|---|---|---|
| `POST /gfeh/partitions/create` | admin | `{name, quota}`, nombre **sin** prefijo | `Filesystem` `{name:"gfeh/<n>", quota}` |
| `POST /gfeh/partitions/modify` | admin | `{name, quota}` | `Filesystem` |
| `POST /gfeh/partitions/remove` | admin | `{name}` | 200, vacío |
| `POST /gfeh/partitions` | autenticación | sin cuerpo | **array JSON plano** de `Filesystem` |

Dos detalles son estructurales:

- **El listado devuelve un array desnudo, no un `PageResult`.** Todos los demás endpoints de listado de Town OS paginan; este no puede, porque el `list_partitions()` de gfeh deserializa `Vec<Filesystem>` directamente y un envoltorio paginado no decodifica del lado de Rust.
- **El prefijo es asimétrico.** Las peticiones llevan un nombre desnudo, las respuestas llevan `gfeh/<nombre>`. El prefijo es un artefacto del espacio de nombres de Town OS, no parte de la identidad de la partición; el `Partition::from_volume` de gfeh lo quita a la vuelta.

Códigos de estado sobre los que ramifica el cliente de gfeh: **409** ya existe (su aprovisionamiento es un crear-o-redimensionar y distingue ambos por este estado — un demonio cuya partición existe en todos los arranques salvo el primero solo arrancaría una vez), **404** no existe, **400** nombre incorrecto, **403** no es admin. Un nombre que contenga un separador de rutas se rechaza en esta frontera porque gfehd lo rechaza en la suya; discrepar sobre qué es un nombre de partición legal permitiría que `../user/algo` direccionara un volumen fuera de la raíz del almacenamiento de objetos.

Los manejadores llaman a `storage.Storage` en proceso, nunca a `/storage/*`, así que la aplicación de prefijos reservados, la política de cuotas y el registro de auditoría se quedan en un solo sitio. Estas rutas **no** están en `grantRoutes` — aprovisionar la raíz de un árbol de permisos no es algo que compre una concesión, así que a una cuenta con concesiones se le niegan por la lista blanca global antes de que se ejecute ningún manejador.

### Protocolo 2: el socket de administración (`/v1/*`)

La superficie administrativa de cada demonio es JSON sobre HTTP **solo en su socket Unix** — nunca en un puerto. No hay token ni autenticación: los permisos del sistema de archivos sobre el socket son el control de acceso, así que alcanzarlo ya significa ser root en el equipo. `src/gfeh/client.go` (`UnixClient`) es el lado de Go; fija `DialContext` al socket y usa una autoridad falsa `http://gfeh`.

| Llamada | Método + ruta | Propósito |
|---|---|---|
| `Health` | `GET /v1/health` | vitalidad; también el sondeo de disponibilidad |
| `Names` | `GET /v1/names` | los nombres que esta partición quiere publicar |
| `ListPrincipals` / `CreatePrincipal` / `DeletePrincipal` | `GET`/`POST` `/v1/principals`, `DELETE /v1/principals/<nombre>` | el bosque de usuarios de la partición |
| `ListGrants` / `CreateGrant` / `RevokeGrant` | `GET /v1/grants?principal=`, `POST /v1/grants`, `DELETE /v1/grants/<id>` | ACL |
| `ListExposures` / `WithdrawExposure` | `GET /v1/exposures`, `DELETE /v1/exposures/<token>` | enlaces `/f/<token>` publicados |

gfehd mapea sus errores internos a estados HTTP (404/409/400) y `StatusError.Unwrap` los vuelve a mapear a centinelas de Go para que `errors.Is` funcione.

Añadir un usuario es `POST /v1/principals {name, parent, ceiling}` — **sin contraseña**, que es la razón de que la interfaz nunca pida ninguna. El techo sigue la regla de proyección de gfeh: `all` para un administrador de Town OS, lectura/escritura en los demás casos. gfehd sujeta una concesión al techo del principal, así que la interfaz muestra los permisos que *volvieron*, no los que se enviaron: un administrador tiene que poder ver que una concesión se estrechó.

### Protocolo 3: nombres — gfeh responde, Town OS compone

**gfeh nunca registra un registro DNS ni una ruta de ingress.** `RebuildDNS` llama a `TeardownTLD` y `RebuildIngress` llama a `SetRoutes` con el conjunto derivado completo — ambos destruyen el estado ajeno — así que cualquier cosa que gfeh registrara directamente sobreviviría exactamente hasta la siguiente reconciliación. En su lugar, `GET /v1/names` devuelve **etiquetas** (`s3.<partición>`) con una vista y un puerto, y Town OS compone la zona. Los nombres, por tanto, se *piden* en cada reconstrucción en lugar de empujarse una sola vez.

`gfehFQDN(label, tld)` (`gfeh_tls.go`) califica una etiqueta bajo el TLD de la red y es la única cadena en la que deben coincidir el registro A, el SAN de la hoja, el propietario DANE TLSA y el vhost del ingress — el mismo invariante que existen para sostener `packageFQDN` y `pageFQDN`. **Siempre** califica: no consulta `isPublicFQDN`, porque toda etiqueta de gfeh ya contiene un punto (`s3.gfeh`) y ese predicado lee cualquier nombre así como un FQDN público, lo cual dejaría todos los nombres sin calificar y pediría un certificado ACME para un dominio que nadie posee.

**Es además el punto de estrangulamiento donde una etiqueta deja de ser una cadena en un cable y se convierte en un vhost, un registro DNS y una ruta del sistema de archivos**, así que `gfeh.ValidateLabel` se aplica ahí y en ningún otro sitio. Un vhost del ingress se escribe como `https://<hostname> {` sin entrecomillado, así que una etiqueta que lleve un salto de línea y una llave cierra ese bloque y abre otro — y Caddy no rechaza un solo vhost incorrecto, rechaza la configuración entera y tira todos los nombres del equipo. Una etiqueta que no valida produce la cadena vacía, y todos los llamantes ya descartan un FQDN vacío, así que un nombre malformado no aporta ningún registro, ninguna ruta, ningún certificado ni ningún directorio, en lugar de aportar uno roto. La longitud (`gfeh.NameMaxLen`) se comprueba sobre el nombre **compuesto** y no sobre la etiqueta sola: una etiqueta dentro del límite todavía puede pasarse de él al calificarse bajo un TLD largo, y un nombre que el DNS no va a transportar es uno que ni el certificado ni el vhost deberían reclamar.

La publicación coincide exactamente con la de paquetes y páginas:

- **DNS de doble hogar** — la partición de una red no predeterminada obtiene un registro A acotado en la IP de superposición del equipo (servido a los pares WireGuard de esa red) *y* un registro A global en la IP de la LAN, mediante los plegados de `RebuildDNS` y `RebuildNetworkDNS`. El DANE TLSA se ancla en ambas mitades.
- **TLS** — una hoja de la CA local por nombre, que lleva como SAN la IP de superposición del equipo en esa red para que un par pueda marcar por la dirección WireGuard cruda.
- **Ingress** — un vhost por vista HTTP, con backend `<contenedor>:<puerto>` en la red podman compartida `town-os-ingress`. `dedupeIngressRoutes` protege el conjunto de rutas con la regla de que gana el primero, porque Caddy rechaza una configuración entera por un solo vhost duplicado.

`IsHTTPView` controla ese último paso, y una vista **desconocida** se trata como no-HTTP: un vhost para algo que no habla HTTP acepta un handshake TLS y luego falla, lo cual es peor que no tener ruta. (Una vista no-HTTP aportaría un registro DNS y ninguna ruta de ingress; hoy las cuatro vistas servidas son HTTP.)

### El índice de la partición

Todas las vistas que sirve gfeh responden a un **protocolo**, y ninguna responde a
un navegador: la vista HTTP tiene exactamente una ruta, `/f/{token}`, así que su
raíz es un 404; S3 devuelve un error XML ante cualquier cosa que no pueda
analizar como operación; Drive e IPFS responden a sus propias API. Así que lo
único que cualquiera hace con un nombre nuevo — abrirlo — informaba de que el
almacenamiento de objetos estaba roto, cuando en realidad nunca había habido
ningún sitio donde mirar.

Cada partición publica un índice estático en **`gfeh.<tld>`** — `gfeh.IndexLabel`,
que es `VolumePrefix` en lugar de la cadena `"gfeh"` escrita por segunda vez,
porque el índice tiene que aterrizar en el padre de las etiquetas de vista que
indexa. No hay ningún nombre nuevo que aprender: las vistas ya son `s3.gfeh`,
`http.gfeh`, `drive.gfeh`, `ipfs.gfeh`.

- **Lo aporta `collectGfehSites` como un `GfehSite` corriente**, y esa es la gracia: hereda los registros A y AAAA, el registro de superposición acotado, el anclaje DANE, el SAN de la hoja y la ruta de ingress del mismo código que deriva los seis para las vistas, así que el vhost y el certificado no pueden componerse a partir de cadenas distintas. Solo se añade cuando la partición tiene al menos una vista que el ingress da la cara por — un índice para una partición que no sirve nada navegable sería un nombre, un certificado y una ruta, todo para renderizar una página que dice que no hay nada que ver.
- **Lo sirve el contenedor de pages, no gfehd.** El HTML estático no necesita servidor propio, y emitirlo en línea como cuerpo de un `respond` de Caddy metería marcado generado dentro del archivo de configuración, donde un solo error de escapado hace que Caddy lo rechace todo.
- **El contenido vive bajo su propia raíz `gfeh-index/`**, hermana de `gfeh/` por la misma razón que lo es `gfeh-control/`: todo lo que hay bajo `pages/` es una página, propiedad de una fila y barrida por la reconciliación de pages. El webroot es lo único que ambos comparten, porque es desde donde sirve el contenedor. `ViewIndex` deliberadamente **no** está en `HTTPViews`, así que `IsHTTPView` no lo acepta — ese predicado responde a "¿es esta una vista que gfehd informó y por la que el ingress puede dar la cara?", y el índice ni lo informa gfehd ni lo sirve él.
- **`pruneStalePageSymlinks` pliega `gfehIndexHostnames`.** Un índice no es una página, así que sin esto el primer `reconcilePages` elimina todos los enlaces de índice — y un equipo con almacenamiento de objetos y sin páginas se encuentra con el caso más agresivo de eso en cada pasada. El conjunto válido se deriva **únicamente del conjunto de redes**, nunca preguntando a los demonios, así que a una partición que simplemente tarda en arrancar no se le puede podar su propio índice: lo que se puede eliminar tiene que ser decidible a partir de estado del que Town OS es dueño.
- **Los índices los renderiza `reconcileGfehIndexes`, desde `RebuildIngress`**, no desde `ReconcileGfeh`. Esa colocación es estructural: la reconstrucción del ingress se ejecuta en el arranque, en la reconciliación horaria, en el CRUD de paquetes y páginas y, sobre todo, en `publishGfehNames` — la primera pasada en un arranque en frío en la que algún demonio está respondiendo, ya que gfehd sondea `/status/ping`, que da 503 hasta el intercambio del manejador. Un índice escrito desde la reconciliación de gfeh se escribiría antes de que los demonios pudieran decir qué sirven, y se quedaría obsoleto hasta la hora siguiente.

El índice lleva **solo las vistas**, que ya están en el DNS. Ni exposiciones,
ni principales, ni concesiones, ni cuota: se sirve sin ninguna autenticación
delante, y cada enlace `/f/<token>` publicado es una credencial al portador —
precisamente lo que una página sin autenticar nunca debe enumerar.

### Protocolo 4: el proxy de la interfaz (`/gfeh/*`)

El socket de administración no está autenticado ni es alcanzable por red, así que Town OS hace de proxy. Están deliberadamente **separadas de las cuatro rutas del contrato** para que `check-townos-sync` siga coincidiendo exactamente con lo que declara el contrato.

| Ruta | Autenticación |
|---|---|
| `GET /gfeh` | autenticación — particiones con red, TLD, cuota, estado de la unidad y salida de `/v1/names` |
| `GET /gfeh/principals?network=` | autenticación |
| `POST /gfeh/principals/add` / `remove` | `requireObjectStorage` (admin o la concesión `gfeh`) |
| `GET /gfeh/grants?network=&principal=` | autenticación |
| `POST /gfeh/grants/add` / `revoke` | `requireObjectStorage` |
| `GET /gfeh/exposures?network=` | autenticación |
| `POST /gfeh/exposures/withdraw` | `requireObjectStorage` |

Los cuatro `GET` están excluidos de la auditoría; los cinco mutadores llevan claves de auditoría. Sin particiones configuradas, `GET /gfeh` informa de que el almacenamiento de objetos no está configurado en lugar de dar error.

**Todas ellas — lecturas incluidas — están confinadas por `requireNetworkScope` a las redes de quien llama**, porque el "qué red" vive en el cuerpo o en la consulta que solo el manejador ha analizado. Una cuenta acotada listando los principales o los enlaces publicados de otra red sería exactamente la fuga que el ámbito existe para evitar, y las lecturas son `requireAuth`, así que nada aguas arriba lo habría detenido. `GET /gfeh` no nombra ninguna red (las enumera), así que en su lugar filtra filas — con el mismo predicado `Restricted()`, ya que filtrar una cuenta corriente contra su ámbito vacío haría invisible todas las particiones para todas las cuentas corrientes en lugar de confinar a nadie.

**El orden dentro de `gfehClientFor` es estructural: forma, luego autoridad, luego existencia.** Una red vacía es un 400 para todo el mundo (una errata no es un problema de permisos); una red fuera de ámbito es un 403 *antes* de cualquier búsqueda de partición; solo entonces un registro ausente se gana su 503 y una red desconocida su 404. Con la búsqueda primero, quien no tenía por qué preguntar averiguaba si esa partición existía y si su demonio estaba en pie, y lo obtenía como un rechazo *exitoso* de otra clase — así que nada dejaba constancia de que una cuenta acotada había alcanzado fuera de su ámbito.

### Sin cuentas de servicio

Una versión anterior creaba una cuenta de administrador dedicada, `gfeh`, cuya contraseña se guardaba en el ajuste `gfeh_service_password`, para que el demonio pudiera autenticarse ante el plano de control. **Eso ya no existe.** Town OS aprovisiona por sí mismo el subvolumen y la cuota de cada partición antes de que arranque el demonio, y crea los principales por el socket de administración, así que la credencial no compraba nada — a cambio de costar una *cuenta de administrador habilitada que nadie creó*, sentada en la lista de usuarios de todos los equipos con privilegio suficiente para desinstalarlo todo, y de obligar a que toda pregunta del tipo "¿tiene este equipo un administrador?" significara "un administrador *humano*".

`hasEnabledAdmin` (`src/svc/systemcontroller/admin_presence.go`) es ahora la pregunta llana, compartida por la bandera de configuración inicial de `/status/ping` y la rama de arranque inicial de `POST /account/create` para que las dos nunca puedan discrepar — un equipo donde una dice "configurado" y la otra no es un equipo en el que nadie puede entrar.

`account.PurgeLegacyServiceAccounts` elimina la fila y la contraseña almacenada en el primer arranque tras una actualización, informando de si eliminó algo para que el equipo lo diga una vez en lugar de registrarlo en cada arranque. Es SQL crudo deliberadamente: `Manager` no tiene `Delete`, y una capacidad de eliminación de cuentas no es algo que se introduzca como efecto secundario de una limpieza.

Lo que queda en `gfehd.yaml` es `credentials:` y `drive.tokens:` — **usuarios finales autenticándose ante las vistas de gfeh**, nunca inicios de sesión de Town OS. El bloque `town_os:` sigue existiendo en el esquema de configuración (el YAML de gfehd se refleja exactamente) pero Town OS no renderiza ninguna cuenta dentro.

### Sin vista SMB

SMB **no se sirve**. Es la única vista que no puede ponerse tras el ingress y la única que necesita una credencial propia: un hash NT (`MD4(UTF16LE(contraseña))`), que no puede derivarse del hash de contraseña almacenado, así que todo usuario que quisiera un recurso compartido tenía que cargar con una segunda contraseña. Las cuentas de Town OS no tienen una, así que no hay nadie a quien gfehd pudiera autenticar — y un recurso compartido sin autenticar en la LAN no es el respaldo que hay que tomar.

Consecuencias: ninguna partición declara un bloque `smb:`, no se asigna ningún puerto del host para ello (`SMBPortBase` se mantiene solo para que el `GFEH_SMB_PORT_BASE` del arnés siga conectado), `Account.SMBNTHash` y `src/account/smb_credential.go` han desaparecido, y la columna `smb_nt_hash` la elimina `migrateLegacyAccountColumns` — un hash NT no lleva sal, no tiene factor de trabajo y equivale a la contraseña ante cualquier cosa que todavía hable NTLM, así que dejarlo en reposo para una vista que nadie sirve es lo peor de ambos mundos. Las otras cuatro vistas no se ven afectadas.

### Archivo de configuración

`src/gfeh/config.go` refleja el YAML de gfehd **exactamente**. Cada estructura de configuración de gfehd es `#[serde(deny_unknown_fields)]`, así que una clave suelta no se ignora — es un fallo duro de arranque. Nivel superior: `data_dir`, `partition`, `network` (un **puntero**: ausente significa la partición predeterminada, y una cadena vacía es una petición distinta e inválida), `admin_socket`, los cinco bloques opcionales de vista, `credentials` y `town_os`. Town OS renderiza cuatro de las cinco vistas y ni un bloque `smb:` ni una cuenta `town_os:`. Se escribe con permisos `0640` y legible por el grupo gid de gfeh bajo `<btrfsBase>/gfeh-control/<red>/`, ya que el demonio corre como uid 2000 y debe poder leerlo.

### Arranque y reconciliación

`ReconcileGfeh` se ejecuta en el arranque **después del ingress y de pages** y **antes de `Reconcile`** — para entonces la CA TLS y el almacenamiento ya existen, y los nombres tienen que estar disponibles para las llamadas a `RebuildDNS`/`RebuildIngress` de más abajo. Se ejecuta **una segunda vez después de `ReconcileNetworks`**, es idempotente (una partición sin cambios se deja en paz en lugar de rebotarla) y cubre cualquier red que la reconciliación haya traído a la existencia. También se llama desde `/networks/create`, `/networks/remove`, `/networks/enable` y `/networks/disable`, así que una red añadida en tiempo de ejecución obtiene una partición. No fatal en todo momento.

Por cada red asegura el subvolumen (con UID/GID), renderiza la configuración e instala y reinicia la unidad **solo cuando el contenido renderizado cambió** (el idioma de diferencia con `ReadUnit` que la reconciliación ya usa). `pruneGfehPartitions` elimina las unidades de redes que ya no existen.

**La espera por partición ha desaparecido, y su ausencia es estructural.** `reconcileGfehPartition` arranca la unidad y se detiene ahí; si un demonio está respondiendo se pregunta aparte, mediante `GfehReadyNetworks` y los recolectores de nombres, que ya tratan una partición silenciosa como algo que no aporta nada en lugar de como un fallo. La espera solía estar dentro del bucle, una vez por partición — incluidas todas aquellas para las que no hacía nada, ya que `ensureFirstUserPrincipal` retorna en su primera línea para cualquier red que no sea la del hogar. En un contexto con fecha límite eso no era meramente lento: el primer demonio que nunca respondía se gastaba todo el presupuesto restante en `WaitForReady`, así que todas las particiones posteriores intentaban `Start` sobre un contexto caducado y `pruneGfehPartitions` no se ejecutaba nunca. Un demonio muerto se llevaba por delante al resto del almacenamiento de objetos, en el orden en que resultara que se ordenaran los nombres de red.

La única espera que queda es `seatGfehFounder`, al final del todo de la reconciliación: espera solo a la partición del **hogar**, limitada por `gfehFounderWaitBudget` (10 s, anulable por configuración para las pruebas), y luego sienta a la primera cuenta del equipo. Al ser la última, excederse solo puede retrasar trabajo que ya está hecho; a un demonio que aún arranca en frío lo sienta la siguiente pasada, que el arranque ejecuta inmediatamente después de `ReconcileNetworks`. Por la misma razón, `GfehReadyNetworks` le da a cada sondeo de salud su propio presupuesto mediante `context.WithoutCancel` en lugar de tirar de lo que le quede a quien llama — una fecha límite gastada haría que todas las particiones parecieran muertas a la vez. La cancelación se sigue respetando; eso es un apagado.

**El almacenamiento de objetos no tiene ajuste de encendido/apagado.** Guardar archivos es para lo que sirve el equipo, así que se ejecuta como se ejecutan el DNS y el ingress — como parte de lo que Town OS es, no como una función que haya que habilitar. Un interruptor solo compraba la posibilidad de que lo encontraran en posición de apagado mientras alguien depuraba adónde habían ido sus archivos; un administrador que quiera los demonios abajo los detiene desde el panel de servicios como cualquier otro servicio del sistema. Una fila `object_storage_enabled` obsoleta que quede en la tabla de ajustes de un equipo actualizado no la lee nadie.

Las escotillas de escape que quedan son sobre una *compilación*, no sobre política: depende del ingress (con `INGRESS_IMAGE` vacío las cuatro vistas HTTP no son alcanzables por nada, así que arrancar particiones publicaría nombres que nada sirve) y `GFEH_IMAGE` explícitamente vacío se salta el almacenamiento de objetos por completo (modo de desarrollo) — la misma convención de `LookupEnv` que usan `UI_IMAGE` e `INGRESS_IMAGE`, porque `Getenv` haría que un valor vacío significara "usa el predeterminado" y no dejaría interruptor de apagado.

**La primera cuenta se sienta en la partición del hogar.** `ensureFirstUserPrincipal` crea un principal con el nombre de la cuenta creada más temprano en el equipo (por `CreatedAt`, con el nombre de usuario como desempate, para que el fundador no pueda cambiar entre reconciliaciones según el orden de iteración del mapa), con `gfeh.CeilingForAccount(admin)`. Una partición cuyo bosque está vacío no sirve a nadie: el operador abre la pestaña de Usuarios, no encuentra nada y tiene que deducir que su propia cuenta no está ahí. **Solo el hogar** — todos los equipos tienen esa partición, mientras que una red añadida después pertenece a quien reciba una concesión sobre ella, y sentar ahí al fundador le entregaría un espacio de nombres que creó otra persona. Idempotente por vía de gfehd, que responde 409 ante un principal que ya existe.

**Los nombres se publican después del intercambio del manejador.** `publishGfehNames` se ejecuta en segundo plano: gfehd sondea `/status/ping`, que responde **503 hasta que el router completo está en pie** ([Estado de Arranque](#estado-de-arranque-y-refresco)), así que una partición no puede terminar de arrancar hasta que el arranque esté esencialmente hecho. Esperarlo en línea bloquearía el propio arranque al que espera. Si ninguna partición se pone lista a tiempo, los nombres los publica sin más la siguiente reconciliación.

Las particiones se registran en `collectSystemServices()`, así que `POST /system-services/refresh` vuelve a descargarlas y reiniciarlas — la omisión que dejaba el ingress obsoleto en silencio.

### Acoplamiento de versiones

**Town OS compila el gfehd actual, y deliberadamente no hay ningún mando de versión.** `Containerfile.gfeh` ejecuta un `cargo install gfehd` pelado — sin `--version` y sin `--locked` — así que la imagen lleva lo que crates.io tenga en el momento de compilar, resuelto contra dependencias actuales.

Hubo un mando (`GFEH_VERSION`, con una salida de emergencia `GFEH_LATEST`) y hacía daño activo. El Makefile pasaba la versión en cada compilación como `--build-arg`, que **gana sobre el valor por defecto de un `ARG`**, así que el número que se publicaba era el del Makefile y el del Containerfile era decoración. Los dos se separaron: el Containerfile decía `0.1.2` y explicaba largamente por qué nada más antiguo puede ejecutarse bajo Town OS, mientras el Makefile compilaba en silencio `0.1.1`. Tomar la versión publicada actual satisface el mínimo por construcción y elimina el segundo sitio donde vivía la respuesta.

Ninguno de los dos fallos lo atrapan las suites, y por razones distintas. La suite **unitaria** se apoya en un gfehd falso y nunca llega a ejecutar el demonio. Las suites de **integración y de interfaz** sí ejecutan el real — `make/test.sh` carga la imagen construida en ambos contenedores, porque no hay sustituto que demuestre que una partición arranca, responde en su socket de administración y aplica sus propios topes — pero un demonio meramente *viejo* igual compila, se publica, se instala y arranca. Solo aparece como almacenamiento de objetos que nunca arranca en un equipo real. Para que conste, ya que es la razón por la que existía el mínimo: 0.1.1 no puede analizar la configuración de una partición que lleve cualquier usuario SMB (cada struct de configuración de gfehd es `#[serde(deny_unknown_fields)]` y su `SmbConfig` no tiene campo `users`), y se autentica en cuanto arranca, que durante el arranque es el stub de `:5309` respondiendo 403 a todo salvo al ping.

**Ambas compilaciones derrotan la caché de capas, por medios distintos.** `cargo install gfehd` es una línea `RUN` idéntica byte a byte en cada compilación, así que su capa es un acierto de caché permanente — de otro modo, una compilación cuyo contrato entero es "lo que crates.io tenga hoy" serviría para siempre el crate de la primera compilación, en silencio y con registros limpios cada vez. No hay una clave de caché más barata: saber cuándo cambió el crate significa preguntárselo a crates.io, que es justo para lo que existe esta compilación.

- `release-gfeh` pasa **`--no-cache`**. Nada más débil es aceptable en algo que se distribuye.
- `gfeh-local` pasa un build-arg **`GFEH_CACHE_DATE`** con granularidad de día. El fixture es prerrequisito de cada ejecución de integración y de dev, así que `--no-cache` ahí recompilaría el árbol de dependencias de Rust en cada una; pero un acierto de caché puro lo congelaría en el gfehd que fuera actual la primera vez que se construyó en esa máquina — y las suites de integración y de interfaz arrancan particiones reales contra él. Lo diario es el término medio: acierto de caché dentro del día, recompilación al cambiar la fecha.

En ambos casos el volumen del registro de cargo sobrevive, así que el árbol de dependencias se recompila pero no se vuelve a descargar. Este es un caso concreto de una regla general de compilación ([CLAUDE.md](CLAUDE.md)): una imagen local construida a partir del código del repositorio no puede divergir, porque un cambio en el código invalida su caché; una cuyo contenido se descarga en tiempo de construcción necesita una invalidación explícita.

### Interfaz

`/dashboard/objects` (navegación `nav.objects`, "Object Storage"). Un selector de red arriba del todo y después subpestañas `?tab=`, una por archivo bajo `ui/src/routes/objects/`: **Overview** (estado, cuota y nombres publicados por partición, con si a cada uno se llega por el ingress o marcándolo directamente), **Users** (principales y techos; añadir proyecta una cuenta de Town OS), **Grants** y **Links** (exposiciones, con retirada). Las lecturas son `requireAuth`, así que la pestaña no es solo para administradores; los controles que mutan necesitan admin o la concesión `gfeh`, y en cualquier caso solo en las redes de quien llama.

Dos detalles de esa pantalla existen para impedir que un lector actúe sobre un
número o un token que no se puede usar:

- **La columna Port de Overview está en blanco para una vista HTTP.** El puerto que gfehd informa para una es un *puerto de backend del lado del contenedor* al que el ingress hace de proxy, inalcanzable desde donde está sentado cualquier lector — imprimir `9000` junto a "Ingress (HTTPS)" invita a marcar `s3.gfeh.home:9000` y concluir que la funcionalidad está rota. SMB conserva su número, que sí sería un puerto real del host.
- **La pestaña Links renderiza la URL completa, compuesta en el servidor.** `GfehExposureView.URL` se construye a partir de `gfehPublishedLinkBase` — `https://<fqdn-vista-http>/f/` — que viene del mismo recolector que nombra el vhost del ingress y el SAN de la hoja, así que un enlace publicado es por construcción un nombre que el ingress enruta y que el certificado cubre. No se compone en el navegador porque la interfaz tendría que saber cuatro cosas que el servidor ya guarda: que el nombre que sirve es el de la *vista http* y no el de la partición ni el del equipo, que se califica bajo el TLD de la propia red de la partición y no bajo el global, que la ruta es `/f/<token>` y que el puerto informado nunca debe aparecer. El campo está vacío cuando la partición no sirve ninguna vista HTTP — la respuesta honesta, ya que entonces no hay nada sirviendo ese token — y una exposición deshabilitada se renderiza como texto plano en lugar de como un 404 en el que se puede hacer clic.

**Esta pantalla es el único sitio donde se gestiona el almacenamiento de objetos.** La pantalla de servicios no lleva ninguna sección de almacenamiento de objetos: una partición ES un servicio del sistema — una unidad `town-os-system--gfeh-<red>` cada una — así que ya es una fila en la tabla de Servicios del Sistema de esa pantalla, `Object Storage (<red>)`, con la misma insignia de estado y las mismas acciones de arrancar/detener/reiniciar/registros que cualquier otro servicio del sistema. Un panel al lado repetía esa fila y consultaba de forma independiente de ella, así que una unidad tenía dos controles a dos niveles que podían discrepar; además se renderizaba incondicionalmente mientras la tabla dependía de que su consulta hubiera vuelto, lo cual dejaba al almacenamiento de objetos solo en la parte de arriba de la pantalla en el primer pintado y metía los servicios del sistema por encima un momento después. `?expand=objects` en la pantalla de servicios abre Servicios del Sistema, que es donde vive la fila.

## Servicios

### Filtrado de Unidades de Servicio

La consulta de unidades de systemd está acotada al patrón `town-os-package--*` a nivel de dbus, trayendo solo las unidades de paquetes de Town OS en lugar de todas las unidades del sistema. Las unidades de servicios del sistema (`town-os-system--*`) se identifican aparte mediante `IsSystemServiceUnit()`. El conjunto de resultados excluye además los controladores de red (`-network.service`), los ayudantes de UPnP (`-upnp.service`) y los reenvíos de puerto (`-fwd-`). Las unidades de controlador de red se conservan internamente para la detección de fallos, pero se excluyen de la lista de cara al usuario.

### Enriquecimiento de las Descripciones de Servicio

Las descripciones de los paquetes se cargan por lotes usando una sola llamada a `LoadPackages` por repositorio, en lugar de lecturas individuales del YAML de cada paquete. Las descripciones se emparejan con las unidades de servicio construyendo el nombre de unidad esperado a partir de la identidad de cada paquete.

### Generación de Unidades de Servicio

Las unidades de servicio de systemd se generan de forma distinta según el tipo de runtime del paquete.

**Los paquetes de contenedor** generan unidades basadas en podman, con `podman run` para arrancar y `podman stop` para detener, incluyendo mapeos de puertos (`-p`), variables de entorno (`-e`) y montajes de volúmenes (`-v`).

**Los paquetes de VM** generan unidades basadas en QEMU usando `qemu-system-x86_64` con:

- `-m {MB}` -- memoria en megabytes (convertida a partir del valor compilado en bytes).
- `-smp {cpus}` -- número de CPU virtuales.
- `-nographic` -- operación sin cabeza (sin salida de pantalla).
- `-enable-kvm` -- aceleración por hardware KVM.
- `-drive file={imagen},format=raw,if=virtio` -- imagen de disco raw como dispositivo de bloque virtio.
- `-netdev user,id=net0` con `hostfwd=tcp::{externo}-:{interno}` para cada mapeo de puerto -- red en modo usuario de QEMU con reenvío de puertos del host al invitado.
- `-device virtio-net-pci,netdev=net0` -- dispositivo de red paravirtualizado.

Las unidades de VM también gestionan los puertos del cortafuegos mediante `firewall-cmd` en los hooks previos al arranque y posteriores a la parada, y se coordinan con las unidades de socket para evitar conflictos de puertos.

### API de Unidades de Servicio

- `GET /systemd/units` (localhost o autenticación) -- lista todas las unidades de servicio de paquetes, en plano. Devuelve el estado de la unidad enriquecido con el identificador del paquete, la descripción del paquete y una marca de fallo del controlador de red.
- `GET /systemd/units-tree` (localhost o autenticación) -- los mismos datos agrupados en un árbol de dependencias: los paquetes raíz arriba, las dependencias anidadas bajo su padre, recursivamente (la forma refleja la de `/storage/package-volumes`). Cada nodo lleva `repo`/`name`/`version` (nombre efectivo en crudo, que puede contener `--dep--`) junto al `package_identifier` de cara a las personas, además de los mismos campos de estado que el endpoint plano, así que un cliente no necesita una segunda petición para enriquecer las filas. **La búsqueda y la paginación se aplican solo a los nodos raíz** — los descendientes de dependencias no cuentan contra la página, así que un árbol siempre viaja con su subárbol completo, incluso en un límite de página.
- `POST /systemd/status` (requiere admin) -- cambia el estado de una unidad de servicio. Acepta el nombre de la unidad y la acción (start, stop, restart, enable, disable).
- `POST /systemd/status/tree` (requiere admin) -- aplica una acción a todo el árbol de dependencias de un paquete raíz. Acepta `repo`, `name` (nombre efectivo en crudo, para que los valores de las API de instalación se puedan reutilizar sin cambios), `version` y `action`. Solo se permiten `start`, `stop` y `restart` — `enable`/`disable` se rechazan — y detener la propia unidad del controlador del sistema se rehúsa. **El orden de recorrido depende de la acción**: las unidades se recopilan de las hojas hacia arriba (el orden natural para arrancar y reiniciar) y el orden se invierte para detener, de modo que la raíz cae antes que sus descendientes.

### Interfaz de Gestión de Servicios

La pantalla de servicios muestra una tabla de datos paginada con las unidades de systemd de los paquetes instalados. Cada fila muestra el identificador del paquete, la descripción, el estado activo, el subestado y un desplegable de acciones.

#### Acciones de Servicio

El desplegable de acciones de cada servicio ofrece:

- **Arrancar** -- arranca el servicio (con confirmación).
- **Detener** -- detiene el servicio (con confirmación; deshabilitado para el propio controlador del sistema).
- **Reiniciar** -- reinicia el servicio (con confirmación).
- **Registros del servicio** -- abre el visor del diario para la unidad de este servicio.
- **Registros de red** -- abre el visor del diario para la unidad del controlador de red de este servicio (el nombre de la unidad con sufijo `-network.service`).

### Registros Avanzados

Un botón "Advanced Logs" bajo la tabla de servicios abre un modal con:

- **Registros del controlador** -- ver los registros de `town-os-systemcontroller.service`.
- **Registros del sistema** -- ver los registros de todo el sistema (todas las unidades).
- **Errores del diario** -- ver los registros del sistema filtrados al nivel de prioridad 3 (errores y superiores, equivalente a `journalctl -p 3`).
- **Nombre de servicio personalizado** -- entrada de texto para ver los registros de cualquier unidad arbitraria de systemd.

### Visor del Diario

El diálogo del visor del diario ofrece:

- Título dinámico que muestra el nombre de la unidad, "System Logs" o "Journal Errors" según el contexto.
- Insignia de estado con el estado activo y el subestado de la unidad (cuando se está viendo una unidad concreta).
- Búsqueda en tiempo real con filtrado antirrebote (300 ms).
- Filtrado por rango temporal, por fecha y hora.
- Conmutador de modo seguimiento para el volcado continuo de registros con autodesplazamiento (se deshabilita automáticamente cuando hay filtros de búsqueda o de tiempo activos).
- Desplazamiento inicial al final: cuando se abre el visor, el contenedor de registros se desplaza al final una vez que las entradas han terminado de cargarse. El efecto de desplazamiento al final está condicionado a `journalEntries.length > 0` para que no se consuma en el primer renderizado vacío antes de que lleguen las entradas; un `requestAnimationFrame` final vuelve a fijar `scrollTop` después de que el diseño se asiente, por si el árbol expandido crece entre el commit y el pintado.
- Conmutador de vista en árbol para agrupar las entradas por minuto. La vista en árbol es la predeterminada y cada grupo de minuto está **expandido por defecto**. El mapa de estado de expansión guarda únicamente los plegados explícitos: una entrada indefinida se trata como expandida, así que las primeras pulsaciones pliegan en lugar de expandir.
- Copiar al portapapeles todas las entradas de registro mostradas.
- Renderizado de códigos de color ANSI en la salida de registros.
- Resaltado de campos estructurados (pares `nombre=valor`).

### API de Registros

Dos endpoints sirven datos de registros:

- `GET /systemd/logs` (localhost o admin) -- transmite entradas históricas del diario mediante Server-Sent Events. El parámetro de consulta `unit` selecciona el servicio; vacío o `__system__` devuelve los registros de todo el sistema.
- `GET /systemd/logs/tail` (localhost o admin) -- devuelve una página JSON de entradas del diario. Admite los parámetros: `unit`, `lines` (100 por defecto), `before`/`after` (paginación por cursor), `grep` (búsqueda sin distinguir mayúsculas), `since`/`until` (marcas de tiempo Unix) y `priority` (filtro de severidad syslog, 0 = sin filtro).
- `GET /systemd/logs/tree` y `GET /systemd/logs/tree/tail` (localhost o admin) -- las contrapartes acotadas al árbol. En lugar de un `unit`, toman `repo`, `name` y `version` (todos obligatorios) y cubren **todas** las unidades de systemd del árbol de dependencias de ese paquete, así que los registros del padre y los de sus dependencias se entremezclan en una sola vista. Por lo demás, la semántica de reproducción y paginación coincide con la de `/systemd/logs` y `/systemd/logs/tail`.

## Gestión de Cuentas

### Modelo de Cuenta

Cada cuenta tiene: nombre de usuario (clave primaria), hash de contraseña (nunca se expone en JSON), correo electrónico, teléfono, nombre real, marca de administrador, marca de deshabilitada, un **conjunto de concesiones**, un ámbito de redes y marcas de tiempo de creación/actualización. Las cuentas se guardan en una tabla SQLite.

**No existe ningún "tipo" de cuenta.** Una cuenta o es administradora (tiene todas las concesiones, en todas las redes) o no lo es, y una cuenta no administradora lleva las concesiones que estén activadas. `Account.Restricted()` — una cuenta no administradora con al menos una concesión — se deriva, nunca se guarda.

**No hay cuentas de servicio.** Una versión anterior le daba al demonio de almacenamiento de objetos su propia cuenta de administrador; ya no existe, y `account.PurgeLegacyServiceAccounts` la elimina (junto con su contraseña almacenada) en el primer arranque tras la actualización. Véase [Sin cuentas de servicio](#sin-cuentas-de-servicio).

### Reglas de Validación

- **Contraseña** -- mínimo 8 caracteres, y solo ASCII imprimible (`0x21`--`0x7E`, sin espacios). Los bytes con bit alto y los de control se rechazan en el momento de la creación (`ErrPasswordInvalidChars`) en lugar de confiar en que todas las capas del camino hasta bcrypt — la autenticación HTTP Basic, JSON, la codificación de URL, las columnas latin1 de la base de datos — las transporten de forma idéntica.
- **Correo electrónico** -- formato de correo estándar (`usuario@dominio.tld`).
- **Teléfono** -- dígitos con formato opcional (`+`, espacios, guiones, paréntesis).
- **Datos de contacto** -- el correo, el teléfono y el nombre real son todos obligatorios (no vacíos).
- **Concesiones** -- todos los nombres deben estar en `account.AllGrants` (`ErrInvalidGrant`), un administrador no puede tener ninguna de forma explícita (`ErrGrantsAdmin` — ya las tiene todas, así que un subconjunto almacenado solo podría discrepar) y una cuenta que tenga alguna debe estar acotada a al menos una red (`ErrGrantsNoNetworks`).
- **Ámbito de redes** -- cada entrada debe ser un nombre de red válido (`ErrInvalidNetworkName`). Una lista vacía nunca se lee como "cualquier red".

### Concesiones

Una **concesión** (grant) es una capacidad con nombre que puede tener una cuenta no administradora. Existen dos:

| Concesión | Constante | Compra |
|---|---|---|
| `wireguard` | `account.GrantWireGuard` | inscribir y refrescar pares WireGuard en las redes de la cuenta |
| `gfeh` | `account.GrantGfeh` | administrar el almacenamiento de objetos que poseen esas mismas redes — principales, sus concesiones, enlaces publicados |

`account.AllGrants` es el registro: una concesión ausente de él no puede guardarse, que es lo que impide que una errata en una petición de la API se convierta en un permiso que en silencio nunca coincide con nada. Añadir una capacidad es una entrada ahí más sus rutas en `grantRoutes` — sin columna nueva, sin migración nueva, sin puntero nuevo en `UpdateFields`. La interfaz renderiza sus casillas a partir del espejo `ui/src/lib/grants.js`, así que una concesión nueva tampoco necesita marcado nuevo.

Las dos son **independientes**. Tener `wireguard` no compra nada en el almacenamiento de objetos y tener `gfeh` no compra ninguna inscripción de pares; una cuenta puede tener ambas. `Account.HasGrant` responde a "¿puede quien llama hacer esto siquiera?" y `Account.MayAdministerNetwork` responde a "¿en qué red?" — nunca la una por la otra.

#### La aplicación son tres capas, y la composición es la gracia

1. **`grantAllowlist`** es un middleware *global* que falla en cerrado. Una ruta añadida mañana se le niega por defecto a una cuenta restringida hasta que alguien la enumere en `grantRoutes` (`src/svc/systemcontroller/controller_auth.go`), indexada por `"MÉTODO RUTA"`. Las peticiones sin token válido, las de un administrador o las de una cuenta corriente sin concesiones pasan directamente a la autenticación propia de la ruta — una concesión es autoridad *aditiva* para una cuenta que existe para ejercerla, y esto confina solo a esas.
2. **El middleware propio de la ruta** — `requirePeerEnroll` (la concesión `wireguard`) y `requireObjectStorage` (la concesión `gfeh`), ambos construidos a partir de `requireGrant`, que admite a los administradores porque tienen todas las concesiones. Las lecturas siguen siendo `requireAuth`.
3. **`requireNetworkScope`**, dentro del manejador, porque la red vive en el cuerpo o en la consulta de la petición y solo el manejador la ha analizado. **Confina**; no concede, y confina únicamente a las cuentas `Restricted()` — una cuenta corriente no tiene concesiones y por tanto tampoco ámbito, y un ámbito vacío deniega todas las redes, así que aplicárselo a una cuenta corriente daría 403 en todas las lecturas de rutas que son `requireAuth` a propósito.

`grantRoutes` es todo lo que compra una concesión:

```
wireguard: GET  /networks/peers   POST /networks/peers/add   POST /networks/peers/refresh
gfeh:      GET  /gfeh             GET  /gfeh/principals      POST /gfeh/principals/add
           POST /gfeh/principals/remove                      GET  /gfeh/grants
           POST /gfeh/grants/add  POST /gfeh/grants/revoke   GET  /gfeh/exposures
           POST /gfeh/exposures/withdraw
```

más `grantCommonRoutes`, alcanzables por cualquier titular de concesión sea cual sea: `POST /account/authenticate`, `GET /account/me`, `GET /networks`, `GET /dns/services`, `GET /tls/ca.crt` y `GET /status/ping`. Sin ellas una concesión es inutilizable — no puedes ejercer una sin iniciar sesión primero — así que son comunes en lugar de estar duplicadas en cada concesión.

`GET /status/ping` está en esa lista por una segunda razón: es **pública**, registrada sin ningún middleware de autenticación, así que un desconocido anónimo obtiene un 200. Como la lista blanca es global y falla en cerrado, omitirla significaba que un token válido convertía ese 200 en un 403 — autenticarse dejaba a quien llamaba estrictamente peor que no presentar nada. Es además el latido de sesión de 60 segundos del panel y la fuente de toda su superficie de estado, así que una cuenta con `gfeh` podía alcanzar todas las rutas `/gfeh` y aun así no obtener una página usable. Conceder también `wireguard` nunca ayudaba: el ping no está indexado a ninguna de las dos concesiones.

Fíjate en lo que está deliberadamente **ausente**: `/gfeh/partitions/*` sigue siendo `requireAdmin` (aprovisionar una partición crea la raíz de un árbol de permisos y reserva un subvolumen btrfs; `TOWNOS_CONTRACT.md` lo reserva a los administradores y el cliente de gfeh ramifica sobre el 403), y `GET /networks/peers/connected` agrega los pares de todas las cuentas y las direcciones de origen observadas en todas las redes.

A diferencia de `Admin` — inmutable tras la creación — las concesiones son mutables, y `account.Manager.CreateGranted` es un método separado de `Create` para que los invariantes (un titular de concesión nunca es administrador y siempre tiene un ámbito no vacío) se apliquen en un solo sitio en el momento de la creación, en lugar de ensamblarse a partir de una firma posicional ensanchada.

#### Migración desde las columnas antiguas

Versiones anteriores llevaban una columna booleana por capacidad. `legacyGrantColumns` (`src/account/sqlite.go`) mapea cada una a la concesión en que se convierte, y `migrateLegacyAccountColumns` la traslada y elimina la columna:

| Columna heredada | Se convierte en |
|---|---|
| `wireguard` | `wireguard` |
| `object_storage` | `gfeh` |
| `network_only` (un esquema intermedio que plegaba ambas en una sola marca) | ambas |

**Una columna, una concesión.** Una cuenta que podía inscribir pares sigue pudiendo, y una que no podía no lo gana en silencio — ensanchar la autoridad durante una actualización es la dirección que no se puede deshacer, ya que la cuenta conserva su contraseña y nada en pantalla dice que creció. `smb_nt_hash` se elimina sin más (véase [Sin vista SMB](#sin-vista-smb)).

### Toda cuenta pertenece a la red del hogar

`Manager.Create` — el camino que toman la **primera** cuenta y todas las cuentas corrientes — escribe `networks: ["home"]`. `CreateGranted` no lo fusiona: ahí, el ámbito que eligió un administrador son exactamente las redes que la cuenta puede alcanzar, y plegar `home` dentro ensancharía un portal acotado a `office`.

Esto es seguro porque, para una cuenta sin concesiones, el ámbito es **pertenencia, no confinamiento**: `Restricted()` es falso, así que ninguna capa superior lo consulta. Y nunca puede nombrar una red que no esté ahí — véase [La red del hogar siempre existe](#la-red-del-hogar-siempre-existe).

### API de Cuentas

- `POST /account/create` -- crea una cuenta nueva. En modo de arranque inicial (no existe ninguna cuenta de administrador habilitada), se permite el acceso sin autenticar; en caso contrario se requiere autenticación de administrador. Un array `grants` no vacío encamina a `CreateGranted` con las `networks` suministradas; en caso contrario la cuenta se crea mediante `Create` y se une a la red del hogar. Los errores de nombre de usuario duplicado devuelven un mensaje de fallo genérico para evitar la enumeración de usuarios.
- `POST /account` -- obtiene una cuenta por nombre de usuario (requiere autenticación).
- `GET /account` -- lista todas las cuentas con paginación y búsqueda (requiere autenticación).
- `POST /account/update` -- actualiza los campos de una cuenta (requiere autenticación). El nombre de usuario que se actualiza viene del **cuerpo**, así que editar la cuenta de otra persona es solo para administradores: sin esa comprobación, cualquier cuenta autenticada podría enviar `{"username":"admin","fields":{"password":"..."}}` y quedarse con el equipo — el controlador maneja el socket de podman del host, así que eso es root. Una cuenta corriente todavía puede editar sus propios datos de contacto y su contraseña, razón por la cual la ruta no es directamente `requireAdmin`. El estatus de administrador no puede cambiarse tras la creación de la cuenta; las concesiones y el ámbito de redes sí, **solo por un administrador, incluso sobre tu propia cuenta** — de lo contrario un usuario normal podría concederse `gfeh` a sí mismo y entrar en una partición, o `wireguard` e inscribir un par en la superposición. Un `networks` nil deja el ámbito almacenado intacto; uno no nil lo sustituye por completo. `validateGrantResult` comprueba el estado de la fila *después* de la actualización, así que conceder a un administrador, promover a un titular de concesión y vaciar el ámbito por debajo de una concesión se detectan todos.
- `POST /account/disable` -- deshabilita una cuenta, impidiendo la autenticación (requiere admin). También revoca las sesiones vivas de la cuenta. Eso no es lo que hace efectivo el deshabilitado — `SessionManager.Validate` rechaza por su cuenta el token de una cuenta deshabilitada, así que la garantía no depende de que la revocación haya tenido éxito — sino lo que impide que un token emitido antes del deshabilitado vuelva a funcionar si la cuenta se rehabilita más tarde, que no es lo que un administrador entiende por "habilitar" después de haberle revocado el acceso a alguien.
- `POST /account/enable` -- rehabilita una cuenta deshabilitada (requiere admin).

### Interfaz de Gestión de Cuentas

La pantalla de gestión de usuarios (`/dashboard/users`) muestra una tabla de datos de cuentas paginada, ordenable y con búsqueda. Cada fila muestra el nombre de usuario, el correo, el teléfono, el nombre real, una insignia de rol admin/usuario y el estado habilitada/deshabilitada. Las acciones por fila incluyen un botón de Editar (abre un diálogo para actualizar la contraseña, el correo, el teléfono, el nombre real, las **casillas de concesiones** y el selector de ámbito de redes) y un conmutador de Habilitar/Deshabilitar con confirmación. Un enlace navega a una página dedicada de creación de usuario (`/dashboard/users/create`) con un formulario de registro que lleva los mismos controles. Ambos formularios renderizan sus casillas a partir de `ui/src/lib/grants.js` y rechazan conceder nada si no se ha elegido ninguna red.

### Gestión de Sesiones

Las sesiones usan tokens JWT (HS256) con reclamaciones para el identificador de sesión (UUID), el nombre de usuario y la marca de tiempo de emisión. La clave de firma es efímera: 32 bytes aleatorios generados mediante `crypto/rand` en cada arranque del servicio, nunca persistidos a disco. Cuando `InitSessionManager` se ejecuta en el arranque, todas las sesiones existentes se borran (`DELETE FROM sessions`), ya que los tokens previos no son válidos con la clave nueva. La variable de entorno `TOWN_OS_SIGNING_KEY` puede anular la clave generada. Las sesiones caducan a los 7 días desde su último uso. Una tarea de limpieza en segundo plano elimina periódicamente las sesiones caducadas.

**El token de una cuenta deshabilitada está muerto al llegar.** `Validate` comprueba `Disabled` y rechaza, porque todas las peticiones posteriores al inicio de sesión se autorizan únicamente desde esa función: sin la comprobación, deshabilitar una cuenta solo impedía que volviera a *iniciar sesión*, mientras que un token que ya tuviera seguía siendo bueno durante toda la vida de la sesión y se refrescaba con su propio uso.

**Sin gestor de sesiones no hay servicio, no servicio abierto.** Todas las decisiones de autorización del equipo se derivaban de un único nil: `requireAuth`, `requireAdmin`, `requireGrant`, `revokeSession`, `requireNetworkScope` y `callerIsAdmin` leían `GetSessionManager() == nil` como "la autenticación no está configurada, así que déjalo pasar". Eso hacía que *no hay nadie a quien autenticar* y *todo el mundo está autorizado* fueran el mismo estado — toda la superficie de autorización a un campo sin establecer de servir `POST /account/create` y `POST /packages/install` a quien llamara de forma anónima, en un controlador que maneja el socket de podman del host como root, sin nada en el sistema de tipos que lo dijera y sin ningún error si ocurría.

La condición es ahora **`ServerConfig.AuthDisabled`: declarada, no inferida**. Un gestor de sesiones ausente con la autenticación habilitada es una mala configuración, y `NewHandler` devuelve `ErrAuthNotConfigured` en lugar de un manejador — rechazando en la construcción y no en cada petición, porque un equipo que arranca y luego responde 500 en todas las rutas autenticadas es una caída confusa, mientras que uno que no arranca dice qué está mal una vez, en el diario, cuando todavía se puede arreglar. El middleware rechaza también ese mismo estado, así que un conjunto de manejadores ensamblado por cualquier otra vía queda igualmente cerrado.

`InitTestServer` establece `AuthDisabled` cuando — y solo cuando — la configuración no instala ningún gestor de sesiones. Eso es lo que mantiene funcionando sin cambios los ~230 puntos de llamada de prueba que nunca construyen uno, mientras que una prueba que *sí* construye uno conserva su autenticación en vigor; deshabilitarla ahí convertiría todas las comprobaciones de autorización de la suite en una tautología.

`callerIsAdmin` es el único sitio donde la respuesta cambió en lugar de moverse: devuelve **falso** para quien llama sin poder identificarse, donde antes devolvía verdadero. Todas las rutas que llegan hasta él están detrás de `requireAuth` o `requireAdmin`, que ahora rechazan ese estado de plano, así que en la práctica es inalcanzable — pero un ayudante de censura es el sitio equivocado para ser generoso apoyándose en eso.

La interfaz `SessionManager` proporciona: `Create`, `Validate`, `Revoke`, `RevokeAllForUser`, `Cleanup`, `List`, `GetUsername`, `HasActiveAdminSessions` y `StartCleanup`.

Endpoints de la API de sesiones:

- `POST /account/authenticate` -- inicio de sesión con usuario/contraseña (público). Devuelve un token JWT y el objeto de la cuenta. Los fallos de autenticación (contraseña incorrecta, usuario inexistente, cuenta deshabilitada) devuelven todos el mismo error genérico de "credenciales inválidas" para evitar la enumeración de usuarios.
- `GET /account/sessions` -- lista las sesiones del usuario autenticado (requiere autenticación).
- `GET /account/me` -- obtiene el nombre de usuario del usuario autenticado (requiere autenticación).
- `POST /account/session/revoke` -- revoca una sesión concreta por su identificador (requiere autenticación).

### Registro de Auditoría

Todas las acciones administrativas se registran en un log de auditoría. Cada entrada tiene: identificador autoincremental, cuenta (nombre de usuario), descripción de la acción, ruta de la petición, detalle saneado (credenciales enmascaradas), marca de éxito, mensaje de error y marca de tiempo de creación.

**El saneador enmascara en lugar de eliminar**, sustituyendo el valor de una credencial por `[REDACTED]` y dejando la clave. Quien lea la auditoría debería poder ver que un campo estaba presente y se retuvo, no quedarse sin poder distinguirlo de una petición que nunca lo llevó. Compara con `auditRedactedKeys` sin distinguir mayúsculas, contra la clave entera y contra el sufijo de la clave tras el último guion bajo, así que `smtp_password` se detecta sin una regla de subcadena que también se tragaría nombres inocuos, y recursa tanto en arrays como en mapas. El mapa `responses` de la instalación de un paquete se trata como **opaco** y se enmascara entero: sus claves pertenecen al autor del paquete, así que no hay vocabulario contra el que comparar, y sus valores son exactamente las respuestas generadas de `type: secret` y `type: oauth` de las que el registro no debe convertirse en copia. Una `key` a secas está deliberadamente FUERA de la lista — la regla del sufijo capturaría entonces `public_key`, que lleva `POST /networks/peers/add`, y una clave pública de WireGuard es pública por construcción, además de ser el único campo que dice qué dispositivo se inscribió.

Las acciones registradas incluyen: crear/modificar/eliminar sistema de archivos, añadir/eliminar/mover/refrescar repositorio, instalar/desinstalar paquete, purgar volúmenes, deshabilitar/habilitar paquete, establecer el estado de una unidad, crear/actualizar/deshabilitar cuenta, autenticarse, revocar sesión, actualizar ajuste, descartar actualizaciones, subir/descargar archivo comprimido, crear/actualizar/eliminar/reconstruir página, subir/eliminar imagen de VM.

Los endpoints de solo lectura se excluyen explícitamente del registro de auditoría. Entre las rutas excluidas están la ruta raíz (`/`), todos los endpoints GET de listado/consulta, los endpoints de información (`/packages/installed/info`), la recuperación de respuestas (`/packages/last-responses`, `/packages/responses`), la previsualización de instalación (`/packages/install-preview`), las búsquedas de versiones/preguntas, el listado de zonas horarias, el endpoint de listado de pages, el ping de estado, el listado de servicios del sistema (`/system-services`), las consultas al log de auditoría, las lecturas de ajustes y los endpoints de transmisión de registros.

- `POST /audit/log` (localhost o admin) -- consulta el log de auditoría con paginación por cursor o por desplazamiento, filtrado por cuenta, ordenación y búsqueda.

### Gestión de Ajustes

Los ajustes clave-valor se guardan en SQLite. Entre los ajustes predeterminados están `default_quota` (50 GB), `max_archive_size` (1 GB), `archive_unpack_timeout` (600 segundos), `locale` (en-US), `dns_tld` (home), `dns_resolution_mode` (auto), `dns_local_forwarders` (false), `peer_ttl` (7200 segundos) y `gfeh_partition_quota` (0). `proton_image` solo se registra en compilaciones con la etiqueta `proton`. Véase [Ajustes](#ajustes) para la tabla completa.

- `GET /settings` -- obtiene todos los ajustes (requiere admin).
- `POST /settings/get` -- obtiene un ajuste concreto por clave (requiere admin).
- `POST /settings/set` -- establece el valor de un ajuste (requiere admin, se registra en auditoría). Los ajustes con valor en bytes (`default_quota`, `max_archive_size`) aceptan cadenas legibles por humanos (p. ej., "500GB", "10MB") que se analizan y se guardan como recuentos numéricos de bytes.

**Todos los gestores de cuentas toman un contexto en cada método, y `dbTimeout` es un techo más que la historia completa.** Antes abrían su propio contexto raíz por consulta (`account.dbCtx`, ya desaparecido), lo que significaba que la cancelación de quien llamaba se detenía en la frontera del gestor: una petición HTTP abandonada seguía trabajando, y el apagado ordenado no podía interrumpir una consulta. Eso importa más aquí que en otros sitios porque `OpenDB` establece `SetMaxOpenConns(1)` — SQLite permite un solo escritor, así que todas las consultas se serializan tras una única conexión y una consulta lenta retiene a todos los demás llamantes tras una espera ininterrumpible de 30 segundos.

`account.queryCtx` deriva del llamante en su lugar: un llamante con una fecha límite más corta la conserva, uno sin ninguna sigue sin poder colgarse para siempre, y un llamante cancelado detiene la consulta en lugar de dejar que agote su propio reloj. Un contexto nil se lee como `context.Background()` en lugar de entrar en pánico — un gestor es la capa equivocada para tumbar un equipo por un argumento que su llamante olvidó, y las pruebas que construyen manejadores directamente dejan nil el contexto del servidor.

Los manejadores pasan `c.Request().Context()`; las goroutines de fondo pasan el contexto acotado al servidor, nunca el de una petición, ya que la operación debe sobrevivir a la petición que la disparó.

**`getLocale()` es la única excepción deliberada**, usando el contexto del servidor en lugar de tomar uno. Se llama desde unos 55 sitios, casi todos construyendo un mensaje de error, y el contexto de la petición sería el límite equivocado de todos modos: el único caso en que ya está cancelado es el de un cliente que colgó, cuando el mensaje no se va a entregar de ninguna manera.

Los seis están convertidos — `SettingsManager`, `AuditManager`, `PagesManager`, `NetworkManager`, `SessionManager` y `Manager` —, junto con `OpenDB`, y `dbCtx` ya no existe.

Dos métodos toman el contexto del **servidor** en lugar del de quien llama, y ambos son deliberados. `AuditManager.LogEntry` lo llama `auditMiddleware` *después* de que retorna el manejador, para registrar lo que hizo: pasar el contexto de la petición dejaría que un cliente que cuelga a mitad de la petición cancelara la escritura que la registra, así que las acciones menos registradas serían exactamente aquellas durante las cuales alguien se desconectó. `NetworkManager.ReapExpiredPeers` es el barrido de pares en segundo plano, cuya finalización parcial deja pares que el dispositivo WireGuard vivo todavía lleva.

`Manager.Authenticate` toma un contexto que acota sus dos consultas pero **no** el hash argon2id que hay entre ellas — argon2 no tiene cancelación, y `loginGate` es lo que limita los hashes concurrentes a 64 MiB cada uno.

### Interfaz de Ajustes

La pantalla de ajustes del sistema ofrece controles configurables por el administrador para todos los ajustes globales. Cada ajuste se muestra en una sección con borde, con un encabezado, una descripción que muestra el valor actual en formato legible por humanos y un formulario con una entrada numérica, un selector de unidad y un botón de guardar.

- **Cuota de volumen predeterminada** -- configurable en GB, MB o bytes. Muestra "0 (sin cuota)" cuando vale cero.
- **Tamaño máximo de archivo comprimido** -- configurable en GB, MB o bytes. Controla el tamaño máximo de archivo permitido en las subidas de archivos comprimidos.
- **Tiempo límite de desempaquetado** -- configurable en segundos, minutos u horas. Controla el tiempo máximo permitido para desempaquetar un archivo comprimido subido.
- **Idioma** -- un desplegable que muestra los idiomas comunes con sus nombres en escritura nativa. Una sección expandible revela las configuraciones regionales extendidas. Las que no tienen catálogo se muestran con un asterisco y deshabilitadas.
- **Imagen de Proton** -- una entrada de texto editable para la referencia a la imagen de contenedor del runner de Proton (p. ej., `quay.io/town/proton:latest`).
- **Reenviadores DNS locales** -- un conmutador respaldado por `dns_local_forwarders`. Debajo, las direcciones a las que rolodex está reenviando *de verdad*, leídas de `GET /dns/status` en lugar de inferirse del ajuste; cuando el descubrimiento no encontró nada usable, el panel dice que se siguen usando los reenviadores públicos, que es el único caso en el que el conmutador se lee como encendido y no cambió nada. Véase [Reenviadores locales](#reenviadores-locales).

Los valores actuales se descomponen en la unidad más apropiada para mostrarlos (p. ej., 1073741824 bytes se muestra como "1 GB", 120 segundos se muestra como "2 minutos"). La validación de entrada rechaza los valores negativos y no numéricos.

## Actualizaciones de Paquetes

### Detección de Actualizaciones

El sistema de actualizaciones compara las versiones de los paquetes instalados contra las últimas versiones disponibles en los repositorios configurados. Un paquete se marca para actualizar cuando existe una versión más reciente o cuando se detectan modificaciones locales.

- `GET /packages/upgrades` (requiere autenticación) -- lista las actualizaciones disponibles. Cada entrada incluye el repositorio, el nombre, la versión instalada, la última versión y una marca de cambio.
- `POST /packages/upgrades/dismiss` (requiere admin) -- marca las actualizaciones actuales como descartadas. Calcula un hash SHA256 del conjunto de actualizaciones actual y lo guarda como el ajuste `dismissed_upgrades_hash`.

La respuesta del ping de estado incluye `upgrades_available` (recuento) y `upgrades_dismissed` (booleano, verdadero si el hash coincide).

## Redes

### Mapeo de Puertos UPnP

La interfaz `upnp.Manager` proporciona `AddPortMapping` y `RemovePortMapping` para gestionar el reenvío de puertos TCP en la puerta de enlace de la red local mediante UPnP/IGD. La implementación descubre el Internet Gateway Device mediante SSDP y usa los métodos SOAP de WANIPConnection2. La IP local se detecta conectando a una dirección externa (8.8.8.8:80 UDP).

### Controlador de Red

El controlador de red gestiona el reenvío de puertos y los mapeos UPnP por paquete. Cada paquete con requisitos de red tiene un archivo JSON de estado que especifica los puertos con sus mapeos externo/interno, la marca de UPnP y la marca de reenvío.

- **Reenvío con socat** (cuando `forward=true`) -- ejecuta `socat TCP-LISTEN:{puertoExterno},fork,reuseaddr TCP:127.0.0.1:{puertoInterno}` para reenviar el tráfico.
- **Mapeo UPnP** (cuando `upnp=true`) -- mapea puertos en la puerta de enlace. Cuando `forward=true`, mapea externo-a-externo (socat escucha); cuando `forward=false`, mapea externo-a-interno (lo maneja el puente de podman).
- **Reconciliación** -- vigila los archivos de estado mediante fsnotify, deteniendo/arrancando reenviadores y mapeos según haga falta.
- **Renovación** -- los mapeos UPnP se renuevan cada 10 minutos con un TTL de 1800 segundos.
- **Apagado** -- elimina todos los mapeos UPnP y mata todos los procesos socat al cancelarse el contexto.

### Red Compartida de las Dependencias

Las dependencias de un paquete comparten la red podman del paquete padre. Esto permite que los contenedores del mismo árbol de dependencias se comuniquen directamente por nombre de contenedor (mediante el DNS integrado de podman en la red compartida) en lugar de a través del reenvío de puertos del host.

- **Creación idempotente de la red** -- toda unidad de servicio incluye `ExecStartPre=-/usr/bin/podman network create {red}` exista o no un controlador de red (NC). Es una red de seguridad para el orden de arranque: si el NC no ha creado todavía la red (p. ej., imagen sin construir, carrera de systemd), el servicio puede arrancar igualmente. El NC también crea la red — gana quien llegue primero, y para el otro es una operación sin efecto.
- **Propiedad de la red** -- el paquete padre es el dueño de la red podman (`town-os-net--{repo}-{nombre}-{versión}`). El NC crea la red en `ExecStartPre` y la elimina (`podman network rm -f`) en `ExecStopPost`.
- **Las dependencias se unen a la red del padre** -- las unidades de servicio de las dependencias usan `--net {red-del-padre}` en lugar de crear la suya. Crean la red de forma idempotente en `ExecStartPre` (por si arrancan antes que el padre) pero nunca la eliminan.
- **Los paquetes independientes sin puertos** siguen el patrón original: `podman network rm -f` y después `podman network create` en `ExecStartPre`, y `podman network rm -f` en `ExecStopPost`. Solo los paquetes independientes sin NC ni NC de padre hacen `rm -f` antes de `create`.
- **Los padres con dependencias** NO hacen `rm -f` antes de `create` en `ExecStartPre` porque puede que las dependencias ya estén corriendo en la red (arrancan antes por el orden de `Before=`).

### Orden de las Dependencias en Systemd

Las unidades de systemd de las dependencias tienen directivas de orden que garantizan una secuencia correcta de arranque/parada respecto al padre:

- **Unidades de dependencia**: `PartOf={servicio-padre}` (detener el padre cae en cascada a las dependencias) y `Before={servicio-padre}` (la dependencia arranca antes que el padre y se detiene después que él).
- **Unidades padre**: `Wants={dep1} {dep2} ...` y `After={dep1} {dep2} ...` (el padre quiere las dependencias y las espera antes de arrancar).
- **Controlador de red**: el `Wants=` existente del NC se fusiona con los objetivos `Wants=` de las dependencias.

Esto se configura mediante los campos de `PackageUnitConfig`: `ParentNetwork`, `ParentUnitName` (para las dependencias) y `DependencyUnitNames` (para los padres). La reconciliación los calcula a partir de los registros de dependencias y de `ParentName()`.

### Variables de Entorno de las Dependencias

Los paquetes padre reciben variables de entorno para alcanzar a sus dependencias en la red compartida:

- `TOWNOS_DEP_{CLAVE}_HOST` -- el nombre del contenedor podman de la dependencia (resoluble mediante el DNS de podman en la red compartida).
- `TOWNOS_DEP_{CLAVE}_PORT_{puertoContenedor}` -- el número de puerto del lado del contenedor (como el padre y la dependencia están en la misma red, no hace falta ningún mapeo de puertos del host).
- `TOWNOS_DEP_{CLAVE}_PORT_{NOMBRE}` -- se emite además de la forma numérica cuando la dependencia declaró un nombre semántico de puerto en `network.external` / `network.internal` (véase **Puertos con Nombre** más abajo). El nombre se pasa a mayúsculas, así que `sql` en la dependencia se convierte en `TOWNOS_DEP_DB_PORT_SQL` en el padre. Ambas formas, la numérica y la nombrada, coexisten y siempre llevan el mismo valor.

### Variables de Plantilla de las Dependencias

Además de las variables de entorno de ejecución anteriores, los valores de host y puerto de las dependencias también están disponibles como marcadores de plantilla `@variable@` durante la compilación del paquete. Esto permite que los paquetes padre referencien dependencias en los valores de su campo `environment` en tiempo de compilación, y también permite que las **dependencias hermanas** se referencien entre sí en el bloque `dependencies.<clave>.responses`.

- `@dep_CLAVE_host@` -- se resuelve al nombre del contenedor podman de la dependencia (resoluble mediante el DNS de podman en la red compartida).
- `@dep_CLAVE_port_N@` -- se resuelve al puerto numérico N del contenedor de la dependencia.
- `@dep_CLAVE_port_NOMBRE@` -- se resuelve al puerto de contenedor que la dependencia etiquetó con el nombre semántico `NOMBRE` (véase **Puertos con Nombre** más abajo). En minúsculas en la plantilla; coincide con el sufijo de la variable de entorno sin distinguir mayúsculas. Coexiste con `@dep_CLAVE_port_N@` para el mismo puerto.

Las claves de plantilla se derivan de los nombres de las variables de entorno `TOWNOS_DEP_*` quitando el prefijo `TOWNOS_` y pasando el resto a minúsculas. Por ejemplo, `TOWNOS_DEP_DB_HOST` se convierte en la clave de plantilla `dep_db_host`, y `TOWNOS_DEP_DB_PORT_5432` en `dep_db_port_5432`.

La forma `@dep_*@` solo se respeta donde ya se ejecuta la sustitución `@variable@` — los valores de `environment` y los `responses` de las dependencias. Dentro del `content` de una plantilla de archivo, usa en su lugar el espacio de nombres `.Dep` de las plantillas de Go (véase **Plantillas de Archivo** más arriba): `{{.Dep.CLAVE.Host}}` e `{{index .Dep.CLAVE.Ports "sql"}}` llevan los mismos valores. `.Dep` se puebla a partir del mismo cálculo de `TOWNOS_DEP_*` y expone cada puerto tanto bajo su clave numérica (`"5432"`) como bajo su nombre semántico en minúsculas (`"sql"`) cuando se declaró uno.

Del lado del **padre**, estas variables se resuelven tras la instalación de las dependencias, cuando ya se conocen el nombre de contenedor y los puertos de la dependencia. Se aplican a los valores de entorno del padre durante la generación de unidades. La reconciliación también reconstruye las variables de entorno de las dependencias para que las unidades de systemd sigan siendo correctas a través de reinicios y cambios de versión.

Del lado de la **dependencia** (respuestas declaradas bajo `dependencies.<clave>.responses` que referencian otra clave hermana), la resolución ocurre durante `installDependencies` mediante una ordenación topológica:

- `orderDependencies`, en `src/svc/systemcontroller/controller_install_dependencies.go`, analiza los `Responses` de cada dependencia hermana buscando marcadores `@dep_CLAVE_host@` / `@dep_CLAVE_port_N@` y construye un DAG. Las dependencias hermanas sin referencias se ejecutan primero; las que referencian se ejecutan después de la o las hermanas que nombran. El desempate entre dependencias igualmente listas es alfabético por determinismo (la iteración de mapas en Go es aleatoria, así que una ordenación es obligatoria para la reproducibilidad).
- Un ciclo entre dependencias hermanas es un error duro y aborta la instalación antes de aprovisionar ninguna dependencia.
- Para cada dependencia en ese orden, se llama a `applyDepTemplates` sobre los `Responses` de la dependencia **antes** de que se ejecute `depIP.CompileWithContext`, sustituyendo los marcadores `@dep_OTRA_*@` por los valores de nombre de contenedor / puerto acumulados de las hermanas ya instaladas. Sin esa sustitución previa a la compilación, una pregunta tipada del YAML de la dependencia (p. ej. `type: port` o cualquier tipo cuyo `Output` ejecute `strconv.ParseUint`) rechazaría el marcador literal con `ErrInvalidResponseType`, abortando a mitad de la instalación y dejando un padre a medio instalar en disco.
- Las autorreferencias (la dependencia X referencia `@dep_X_host@`) se ignoran, no se tratan como ciclos. Las referencias a nombres que no son claves hermanas declaradas se tratan como variables de plantilla externas y se ignoran a efectos de orden.
- El manejador de instalación transmite los errores por SSE y devuelve `nil` desde el manejador HTTP, así que el log de auditoría siempre registra `success=true` con independencia de si la instalación se completó de verdad. Esto significa que los fallos de instalación parcial (árboles de dependencias a medio instalar, volúmenes btrfs huérfanos bajo `installed/<repo>/<padre>/<versión>/`) solo son visibles en el flujo SSE y en la lista de unidades de systemd — no en `/audit/log`.

Ejemplo: un paquete con una clave de dependencia `db` (un contenedor de Postgres que expone el puerto 5432) puede usar `@dep_db_host@` y `@dep_db_port_5432@` en su sección de entorno en lugar de fijar `127.0.0.1`:

```yaml
environment:
  DB_HOST: "@dep_db_host@"
  DB_PORT: "@dep_db_port_5432@"
```

Ejemplo con referencias entre hermanas (la forma de jitsi): `jitsi` depende de `prosody`, `jicofo` y `jvb`. `jicofo` y `jvb` necesitan cada uno el nombre de contenedor de prosody y su puerto XMPP interno, así que el YAML del padre se los pasa a través del bloque `responses` de cada dependencia que los referencia. `orderDependencies` instala primero `prosody`, después `jicofo` y `jvb` (alfabéticamente entre las dos), cada una con el marcador sustituido por el nombre de contenedor concreto de prosody y el puerto 5222:

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

### Volúmenes Compartidos entre Dependencias

Los paquetes del mismo árbol de dependencias pueden compartir subvolúmenes btrfs mediante una aceptación explícita por ambas partes. El autor de la dependencia marca un volumen con `shareable: true`; el autor del padre declara entonces o bien un bloque `expose:` (montar el volumen de la dependencia dentro del contenedor del padre) o bien un bloque `consume:` en otra dependencia (montar el volumen de una hermana dentro del contenedor de otra hermana). Los volúmenes sin `shareable: true` no pueden montarse de forma cruzada — la pasada de instalación/reconciliación rechaza cualquier referencia a un volumen no compartible.

El cableado es una capa fina sobre la infraestructura existente de `HostVolumeMount`: la ruta de instalación resuelve cada entrada `expose`/`consume` en un flag `-v <rutahost>:<rutacontenedor>:<opciones>` de podman que apunta al subvolumen btrfs en disco de la dependencia productora. La reconciliación reconstruye los mismos flags en cada arranque a partir del YAML persistido del padre, y la comparación de contenido de `installUnitIfChanged` recoge los cambios automáticamente — sin ningún hook especial de reinicio.

**Aceptación del lado de la dependencia.** Una dependencia declara `shareable: true` por volumen:

```yaml
# radarr/1.0.yaml
volumes:
  movies:
    mountpoint: /movies
    quota: "@moviesize@"
    shareable: true     # opt-in: parent or sibling may mount this
  config:
    mountpoint: /config  # not shareable; rejected if any parent tries to expose it
```

**Padre → dependencia (`expose:`).** El mapa `dependencies.<clave>.expose:` de un padre nombra volúmenes de la dependencia para montarlos por bind dentro del contenedor del padre. Cada entrada toma una ruta de contenedor y un flag `readonly` opcional (por defecto `true`, ya que los padres normalmente solo consumen la salida de la dependencia):

```yaml
# plex/1.0.yaml
dependencies:
  radarr:
    package: radarr
    expose:
      movies:                  # volume name in radarr's YAML
        path: /data/movies     # in-container path on Plex
        readonly: true
  sonarr:
    package: sonarr
    expose:
      tv:
        path: /data/tv
        readonly: true
```

**Hermana → hermana (`consume:`).** Una lista `dependencies.<clave>.consume:` monta el volumen de una dependencia hermana dentro del contenedor de ESTA dependencia. Cada entrada toma un `from:` (clave de la dependencia hermana en el mismo mapa `dependencies:` del padre), `volume:` (nombre del volumen en el YAML de la hermana), `path:` (ruta de contenedor en la dependencia consumidora) y un `readonly` opcional (por defecto `false`, ya que la compartición entre hermanas suele necesitar escritura — p. ej., un *arr importando al `/downloads` de un cliente de descargas):

```yaml
# media/1.0.yaml — parent that wires download client + arrs
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

**Orden topológico de instalación.** Las referencias `consume.from` añaden aristas al DAG de tiempo de instalación que construye `orderDependencies`, junto a las referencias existentes en respuestas `@dep_CLAVE_*@`. Una dependencia B que consume de la hermana A se instala estrictamente después de A, para que el subvolumen btrfs de A ya exista cuando arranque el contenedor de B. Los ciclos entre aristas de consumo (A consume de B; B consume de A) son un error duro y abortan la instalación antes de aprovisionar ninguna dependencia. El autoconsumo (`from:` igual a la clave de la propia dependencia) se rechaza en tiempo de validación.

**Validación.** La validación en tiempo de compilación rechaza: rutas de montaje relativas o con recorrido de directorios, referencias `consume.from` a claves no declaradas en el mismo mapa `dependencies:`, el autoconsumo y las rutas de consumo duplicadas dentro de una misma dependencia. La validación entre paquetes (`shareable: true` en el volumen correspondiente del productor) ocurre en tiempo de instalación/reconciliación, cuando se carga el YAML del productor — un padre que expone o consume un volumen no compartible falla la instalación con `volume %q is not marked shareable on %s`.

**Sustitución de plantillas en las rutas.** `expose.<nombrevol>.path` y `consume[].path` participan en la sustitución `@pregunta@` exactamente igual que los puntos de montaje de volúmenes normales. `consume.from` y `consume.volume` (y las claves del mapa `expose`) son identificadores, no datos, y no se sustituyen.

**Advertencia sobre permisos — los bind mounts pasan el UID/GID tal cual.** El subvolumen btrfs de una dependencia en el host pertenece al uid:gid con el que lo creó el contenedor de la dependencia. Si la dependencia corre como 1000:1000 (el predeterminado de linuxserver/*) y el padre o la hermana consumidora corre con un uid distinto, el consumidor recibe EACCES al leer o escribir. El arreglo está en el YAML del paquete, no en la plataforma: alinea los valores por defecto de las preguntas `PUID`/`PGID` entre los paquetes que comparten volúmenes. La línea de chown de `HostVolumeMount.UID`/`GID` es intencionadamente no recursiva y solo se aplica cuando el autor de la dependencia los establece explícitamente en un montaje escribible; el resolutor de volúmenes compartidos nunca hace chown automáticamente.

**Espacio de nombres de plantilla.** Los volúmenes compartibles de una dependencia también aparecen en el espacio de nombres `.Dep` de las plantillas de archivo como `.Dep.<clave>.Volumes.<nombrevol>` (el valor es el punto de montaje del volumen dentro del contenedor de la dependencia). Es paralelo a `.Dep.<clave>.Ports`. Los volúmenes no compartibles se omiten deliberadamente del mapa para que las plantillas de archivo no puedan alcanzar datos que el autor de la dependencia no aceptó exponer.

**Orden de desinstalación.** Las directivas `Before=`/`PartOf=` existentes ya garantizan que el padre se detiene antes que las dependencias y que las dependencias se detienen antes que sus productoras, así que cuando un padre se desinstala (desinstalando en cascada sus dependencias) el contenedor del consumidor ya no está antes de que se toque el volumen del productor. No hace falta ninguna lógica nueva de desinstalación.

**Fuera de alcance.** Una dependencia pertenece exactamente a un padre (invariante existente); los volúmenes compartidos no hacen que las dependencias sean multiinquilino. La compartición en dirección inversa (volumen del padre → dependencia) no se admite en la v1; el esquema queda extensible por si hiciera falta. Los servicios del sistema (`town-os-system--*`) no reciben esta funcionalidad — `GenerateSystemServiceUnit` no consulta `expose`/`consume`.

### Puertos con Nombre

Las referencias a puertos de dependencias pueden usar un nombre semántico en lugar de un número de puerto de contenedor. La dependencia declara el nombre como una clave YAML en `network.external` / `network.internal`; los padres referencian el mismo puerto mediante `@dep_CLAVE_port_NOMBRE@`. Así el número de puerto en crudo vive en exactamente un sitio (la dependencia que lo posee) y el padre puede hablar de roles (`sql`, `http`, `admin`) en lugar de trivialidades de protocolo.

**Forma canónica.** La dependencia es dueña del número de puerto — idealmente como valor por defecto de una pregunta `type: port`, para que tanto la autogeneración como la anulación funcionen con normalidad:

```yaml
# dep: named-db/1.0.yaml
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
# parent: named-parent/1.0.yaml
environment:
  DB_HOST: "@dep_db_host@"
  DB_PORT: "@dep_db_port_sql@"   # no "5432" anywhere in the parent
dependencies:
  db:
    package: named-db
```

**Esquema del mapa.** Una entrada de puerto en `network.external` o `network.internal` tiene una clave YAML que es o bien:

- Una cadena de puerto numérico (forma heredada): `"5432": "5432"` → puerto de host 5432 → puerto de contenedor 5432. No se registra ningún nombre.
- Un nombre semántico que coincide con `PortNameRegexp` (`^[a-zA-Z][a-zA-Z0-9_]*$`): `sql: "5432"` → el puerto del contenedor (el valor) hace también de puerto del host, y el nombre `sql` se guarda en `PackageNetwork.{External,Internal}Names[puertoContenedor]`. Los nombres deben empezar por una letra (para evitar la ambigüedad con el análisis numérico) y pueden contener alfanuméricos y guiones bajos.

Ambas formas coexisten en el mismo mapa; el analizador ramifica según la clave. Un nombre que mapea dos puertos de contenedor distintos, o dos nombres que mapean al mismo puerto de contenedor, es un error en tiempo de compilación. El tipo `Package` compilado gana dos campos `PortNameMap` opcionales junto a los `PortMap` existentes; quienes solo se interesan por los puertos numéricos (la generación de unidades, la serialización del estado de red) no notan ningún cambio.

**Emisión de variables de entorno y plantillas.** Por cada puerto de la dependencia compilada, el instalador emite `TOWNOS_DEP_<CLAVE>_PORT_<N>=<N>` (siempre). Si el puerto tiene nombre, emite además `TOWNOS_DEP_<CLAVE>_PORT_<NOMBRE_MAYÚSCULAS>=<N>` con el mismo valor. El resolutor de plantillas quita el prefijo `TOWNOS_` y pasa el resto a minúsculas, así que tanto `@dep_db_port_5432@` como `@dep_db_port_sql@` se resuelven al mismo valor. El `depKeyRefRegex` de `controller_install_dependencies.go` acepta ambas formas; la ordenación topológica de dependencias hermanas reconoce las referencias con nombre al construir el DAG.

**Retrocompatibilidad.** Los paquetes existentes que usan la forma numérica siguen funcionando sin cambios — no se fuerza ninguna migración. Los padres pueden mezclar referencias numéricas y con nombre a la misma dependencia en el mismo archivo. La reconciliación reconstruye ambas formas durante el arranque, así que las instalaciones existentes que sobreviven nunca sufren una regresión.

**Cuándo usar un nombre.** Siempre que un padre referencie el puerto de una dependencia. Un nombre es el único hecho que el padre puede citar; la dependencia es dueña del número. Usa nombres primero para los puertos internos (que es donde vive el tráfico padre-dependencia en la red podman compartida); los puertos externos con nombre están permitidos pero son poco comunes, ya que los padres no suelen marcar a sus dependencias a través de enlaces del host.

## Redes (Superposiciones WireGuard)

Una **red** es una superposición WireGuard con nombre emparejada con un TLD de DNS. Los paquetes se instalan en una red; los pares se unen a ella; el TLD es lo que particiona quién puede resolver qué (véase [TLD de red, doble hogar y resolución split-horizon](#tld-de-red-doble-hogar-y-resolución-split-horizon)).

### Modelo de Red

`account.Network` (`src/account/network.go`) lleva: `Name`, `TLD`, `Subnet`, `Address` (la propia dirección de superposición del equipo, siempre el host `.1`), `PublicKey`, `PrivateKey` (nunca se serializa), `ListenPort`, `Enabled` y marcas de tiempo. Los nombres son seguros como etiquetas DNS (`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`, máximo 32 caracteres) porque se reutilizan como sufijos de interfaz WireGuard y como nombres de unidades de systemd.

`Enabled` controla únicamente el *transporte*: cuando es falso la interfaz WireGuard no se levanta, cortando el acceso remoto mientras la resolución DNS local y los propios contenedores siguen funcionando.

### La red del hogar siempre existe

`DefaultNetworkName` es `home`, y la **siembra `account.InitNetworkManager`**, junto con las tablas — no la reconciliación del arranque. Así que está ahí desde el momento en que hay una base de datos: antes de que arranque el controlador, en todos los servidores de prueba y para la primera petición que el equipo sirve en su vida. `account.DefaultNetwork()` es la fila canónica.

Eso importa porque todo lo que viene después está escrito dándolo por hecho: la primera cuenta está acotada a ella ([Toda cuenta pertenece a la red del hogar](#toda-cuenta-pertenece-a-la-red-del-hogar)), el TLD predeterminado es su TLD, y gfeh le da una partición y sienta ahí al fundador. Un equipo donde hubiera que crearla primero tiene una ventana en la que todo eso es falso — que es lo que hacía que el almacenamiento de objetos se quedara muerto en un primer arranque hasta que algún reinicio posterior encontrara la red ya presente.

**No puede eliminarse** (`ErrNetworkProtected`, y `POST /networks/remove` la rechaza), ni puede crearse una segunda vez — `POST /networks/create` para `home` recibe un 409 de la comprobación de colisión de TLD.

Es **solo DNS**: `applyNetworkTransport` no le da interfaz WireGuard, ni subred de superposición, ni pares, así que nunca puede tener un dispositivo tunelizado. Por eso la fila sembrada **no lleva ningún campo de transporte** — subred vacía, sin par de claves, puerto 0. Esa es la verdad y no un relleno; una subred y unas claves derivadas serían campos que nadie lee jamás.

**Inscribir un par en ella se rechaza, a propósito y en las dos capas.** `POST /networks/peers/add` devuelve un 400 para `home`, y `NetworkManager.AddPeer` devuelve `ErrNetworkDNSOnly` sin importar quién llame. Esto importa porque [toda cuenta pertenece a la red del hogar](#toda-cuenta-pertenece-a-la-red-del-hogar): si allí se aceptara la inscripción, la mera pertenencia equivaldría a tener una vía hacia un túnel, y el par guardado describiría un túnel que no existe ni existirá. Los pares se crean dinámicamente sobre una superposición real, así que quien quiera un túnel la nombra.

El rechazo antes era **incidental**: nada comprobaba la red, y el manejador caía hasta `netip.ParsePrefix` sobre el `Subnet` vacío de la fila sembrada, que fallaba y salía como un **500**. Eso se lee como un equipo averiado y no como un rechazo, no le dice a quien llama por qué, y habría dejado de rechazar en cuanto algo escribiera una subred en esa fila. La guarda va por nombre, delante de la generación del par de claves en el servidor, y detrás lleva una comprobación de fila sin transporte para cualquier red cuya subred esté vacía por otro motivo.

**Su TLD viene de `dns_tld`, y el controlador los mantiene sincronizados.** La siembra no puede conocerlo (el paquete de cuentas no tiene gestor de ajustes), así que la fila llega con el valor por defecto desnudo y `ensureDefaultNetwork` lo reconcilia en el arranque, escribiendo solo cuando los dos discrepan. `POST /dns/tld` la reapunta a la vez que escribe el ajuste. Ambos pasan por `NetworkManager.SetTLD`, que existe exactamente para esto. Equivocarse no es cosmético: `applyNetworkTransport` le pasa `n.TLD` a `rolodex.EnsureNetworkScope`, que decide qué zona posee el ámbito del hogar.

### Direccionamiento e Interfaces

- **Subred** — `wireguard.SubnetForNetwork(seed, name)` deriva un `/24` determinista a partir de una semilla de identidad del equipo y el nombre de la red. Basarse en la identidad del equipo significa que dos equipos Town OS que sirvan pares eligen subredes distintas, así que un dispositivo que se una a ambos nunca ve una colisión. Las subredes se toman de `10.64.0.0/10` para alejarse de los rangos `10.0`/`10.1` que reparten los routers domésticos. La semilla es `networkIPAMSeed()`: el machine-id de systemd, si no el nombre de host, si no una constante, así que la derivación nunca falla — con la sal de instancia plegada dentro.
- **Nombre de interfaz** — `wireguard.InterfaceName(salt, name)` es `"town" + 4 hex` de un SHA-256 del nombre de red con sal: estable frente al orden de creación, independiente de cuántas redes existan y dentro del límite de 15 caracteres del kernel. wg-quick deriva la interfaz del nombre del archivo de configuración, así que la configuración se escribe como `<InterfaceName>.conf`. `systemcontroller.NetworkInterfaceName(name)` es la forma con sal aplicada que usan las pruebas de integración, así que una prueba nunca comprueba contra un dispositivo que nadie creó.
- **Puerto de escucha** — `wireguard.ListenPortForName(salt, name)` se desplaza desde `DefaultListenPortBase` (51820) según un hash del nombre con sal, sondeando hacia adelante más allá de un puerto que otra red ya tenga.

#### La sal de la instancia

El nombre de una interfaz WireGuard, su puerto UDP de escucha y su subred de superposición son todos **globales al espacio de nombres**, y los contenedores de prueba y de desarrollo se ejecutan ambos con `--net host` (a propósito — el DNS en red bridge se rompe en redes cautivas). Sin una sal, un equipo con `make test-full` y uno con `make dev` derivan el *mismo* nombre de interfaz y el mismo puerto de escucha para el mismo nombre de red: el segundo en levantarse no puede crear su dispositivo, y su superposición simplemente está muerta. Dos árboles de trabajo de prueba concurrentes colisionan igual — REGLA DE HIERRO.

`TOWN_OS_WG_SALT` (`EnvWireGuardSalt`) se lee una vez en `wireGuardSalt`. El arnés la establece a `<rol>-<INSTANCE_ID>` mediante `wireguard_salt` en `make/lib.sh` — el rol separa un equipo de prueba de uno de desarrollo dentro de un mismo checkout, `INSTANCE_ID` separa checkouts, y hacen falta las dos mitades. Es estable para un rol y un checkout dados, lo cual importa para desarrollo, cuya base de datos sobrevive entre ejecuciones y cuyas subredes almacenadas apuntarían si no a dispositivos nombrados según la sal anterior. **Un equipo real no establece nada y conserva los nombres históricos sin sal**; una sal vacía devuelve todas las derivaciones intactas.

**Los grupos de subredes predeterminados de podman deben quedarse fuera de `10.64.0.0/10`.** La imagen de ejecución escribe `/etc/containers/containers.conf` con `default_subnet_pools = [{"base" = "172.16.0.0/12", "size" = 24}]` precisamente porque los valores predeterminados de podman (10.89/16, 10.90/15, 10.96/11, …) caen todos dentro del rango de superposición: los `/24` que caen dentro se saltan por conflicto con las rutas de superposición, el grupo se agota bajo carga con "could not find free subnet from subnet pools" y las redes de contenedor de los paquetes dejan de funcionar. No elimines ese archivo ni vuelvas a ensanchar los grupos hacia `10.64.0.0/10`.

El paquete `wireguard` **no controla ninguna interfaz por sí mismo**. Genera pares de claves y renderiza configuración estilo wg-quick; el systemcontroller escribe la configuración renderizada en el directorio de estado de red compartido con el host y una unidad de systemd generada levanta y baja la interfaz del kernel. Eso es lo que mantiene al contenedor del systemcontroller libre de requisitos sobre el espacio de nombres de red del host.

**El orden importa en `applyNetworkTransport`.** Rolodex debe programarse *después* de que la interfaz esté arrancada y la dirección de superposición asignada, sobre un enlace UP y cubierta por una ruta — asignada no es lo mismo que utilizable. Programarlo primero le pide a rolodex que enlace una dirección que el host todavía no tiene; el enlace falla con `EADDRNOTAVAIL` y el listener muere permanentemente, porque rolodex registra un listener en el momento de lanzarlo y el cadáver bloquea después toda reafirmación.

### Pares

`account.NetworkPeer` lleva `Network`, `PublicKey`, `Name`, `AllowedIP`, `Endpoint`, `Rolodex`, `CreatedBy`, `ExpiresAt` y `CreatedAt`.

- **`Rolodex`** marca un par que ejecuta un servidor DNS rolodex en su dirección de superposición. El equipo registra entonces esa dirección como reenviador por TLD, así que los nombres bajo el TLD compartido que son autoritativos en el par se resuelven a través de la superposición. Los teléfonos y los portátiles lo dejan en falso.
- **`CreatedBy`** es la clave de propiedad: una cuenta con la concesión `wireguard` solo puede refrescar los pares que ella creó, así que una cuenta acotada no puede mantener vivo el par de otra cuenta.
- **`Endpoint`** se deriva de **la dirección que marcó el cliente que se inscribe** (la cabecera `Host` de su petición `peers/add`), no de la visión que el equipo tiene de sí mismo. La IP pública del equipo (de ipinfo.io) o su dirección de LAN son inalcanzables tras un NAT, un reenvío de puertos o un relé — un teléfono en la misma Wi-Fi no puede dar la vuelta hasta la IP pública y no puede enrutar en absoluto hacia la dirección privada de la LAN, y el par entonces hace handshake contra el vacío, lo cual el usuario percibe como un DNS roto. La dirección marcada es alcanzable por construcción: la petición llegó por ella. Sin ninguna dirección marcable (una inscripción por loopback), el endpoint se **omite** en lugar de establecerse a algo que no puede funcionar.

### TTL de Inscripción de Pares y el Segador

Una inscripción no vive para siempre. El ajuste `peer_ttl` (segundos, `7200` por defecto) es cuánto sigue siendo válida. Un cliente de larga vida refresca su par mediante `POST /networks/peers/refresh` antes de que transcurra; el par de un dispositivo abandonado caduca por su cuenta, así que el endpoint aditivo `peers/add` no puede acumular pares muertos en silencio y quemar direcciones de superposición. Un `ExpiresAt` nil significa que el par nunca caduca — pares permanentes como los servidores rolodex y los dispositivos añadidos por el operador.

La caducidad siempre la **calcula el servidor** como `ahora + peer_ttl`; quien llama nunca la elige. Una goroutine segadora en segundo plano llama a `ReapExpiredPeers` y después vuelve a renderizar una vez el transporte de cada red afectada, para que el dispositivo WireGuard vivo y los reenviadores de rolodex descarten los pares segados. Es de mejor esfuerzo e idempotente: el conjunto de pares persistido es la fuente de verdad, y un renderizado fallido lo repara el siguiente tic o la reconciliación del arranque. `peerReapInterval` es una cuarta parte del TTL, limitado a `[1m, 15m]`, así que un par caducado permanece a lo sumo ~TTL/4 más allá de su caducidad, y ni un TTL diminuto ni uno enorme producen una tasa de barrido patológica.

### Pares Conectados

`GET /networks/peers/connected` une las filas persistidas con el estado vivo del kernel de cada túnel. La mitad persistida (nombre, cuenta, dirección de superposición, caducidad) responde a "quién tiene permiso"; la mitad de `wg show <iface> dump` (handshake, endpoint observado, transferencia) responde a "quién está realmente aquí ahora mismo" — ninguna de las dos por sí sola es la pregunta, y por eso existe `ConnectedPeerView` en lugar de reutilizar `account.NetworkPeer`.

El análisis vive en el `wireguard.ParseDump` puro. La **primera** línea de un volcado describe la interfaz misma y se salta deliberadamente; tratarla como un par fabricaría un fantasma que sostiene la clave de la propia interfaz. Los marcadores `(none)` y `off` de `wg` se decodifican en lugar de pasarse tal cual como cadenas literales.

**La conectividad es un handshake dentro de la ventana `REJECT_AFTER_TIME` de 180 s de WireGuard** (`HandshakeStaleAfter`) — la única señal de vitalidad que ofrece el protocolo. No hay cierre de sesión, así que un par que se va es indistinguible de uno que está inactivo hasta que su handshake caduca. Un par que *nunca* ha hecho handshake conserva una marca de tiempo nil en lugar de la época, porque "nunca se configuró" y "lleva un día desconectado" son hechos distintos sobre un dispositivo.

El systemcontroller se ejecuta con `--net host`, así que ya comparte el espacio de nombres donde wg-quick creó el dispositivo; la imagen de ejecución incluye `wireguard-tools` únicamente por el binario `wg` (wg-quick sigue ejecutándose en el host, desde las unidades generadas). Una interfaz ausente no es un error — una red deshabilitada, o una cuyo transporte no ha subido, sencillamente no tiene pares vivos y sus filas persistidas deben renderizarse igualmente — y un fallo del volcado degrada a las filas persistidas en lugar de dejar el panel en blanco. La red `home` queda excluida por completo: no tiene transporte, así que incluirla pondría una fila permanentemente desconectada en un panel que trata de quién está tunelizado.

**Desconectar reutiliza `POST /networks/peers/remove`** en lugar de añadir un endpoint. WireGuard no tiene ninguna sesión que matar, así que eliminar el par es la única terminación forzosa que existe.

### API de Redes

- `GET /networks` (requiere autenticación) -- lista todas las redes con el recuento de pares, el nombre de interfaz derivado y el estado de ejecución. La clave privada nunca se expone.
- `POST /networks/create` (requiere admin) -- crea una red. Acepta el nombre y un TLD opcional (por defecto el nombre). Deriva la subred, genera un par de claves, asigna un puerto de escucha y devuelve la red creada. Un nombre o un TLD ya tomados dan un 409 — incluido `home`, que siempre existe.
- `POST /networks/remove` (requiere admin) -- elimina una red por nombre. La red del hogar no puede eliminarse.
- `POST /networks/enable` / `POST /networks/disable` (requiere admin) -- levanta o baja la interfaz de superposición.
- `GET /networks/peers?network=<nombre>` (requiere autenticación, y confinado por `requireNetworkScope`) -- lista los pares registrados en una red. La ruta está en la lista blanca de la concesión `wireguard`, así que una cuenta acotada llega a ella, y una lista de pares nombra dispositivos, las cuentas que los inscribieron y sus direcciones de superposición — una concesión es autoridad sobre las redes propias de quien llama, y una lectura es donde más fácil resulta olvidarlo.
- `GET /networks/peers/connected` (**requiere admin**) -- todos los pares de todas las redes WireGuard unidos con el estado vivo del túnel. Deliberadamente más estricto que sus hermanas `requireAuth` y ausente de `grantRoutes`.
- `POST /networks/peers/add` (`requirePeerEnroll`: admin o la concesión `wireguard`, confinado a las redes de quien llama) -- registra un par. Cuando `public_key` está vacío, el servidor genera un par de claves y devuelve la clave privada más una configuración de dispositivo lista para importar. Acepta un `endpoint` opcional y una marca `rolodex`. **La red del hogar es un 400** — es solo DNS y no lleva pares, lo pida quien lo pida (ver [La red del hogar siempre existe](#la-red-del-hogar-siempre-existe)).
- `POST /networks/peers/refresh` (`requirePeerEnroll`, y solo para un par que quien llama inscribió) -- extiende el TTL de un par en `peer_ttl` y devuelve la nueva caducidad, para que un cliente pueda marcar su siguiente latido con margen antes de que transcurra el TTL.
- `POST /networks/peers/remove` (requiere admin) -- elimina un par por su clave pública.

### Interfaz de Redes

`/dashboard/networks` lista las redes con acciones de crear/eliminar/habilitar/deshabilitar e inscripción de pares por red. Un segundo panel de **Pares Conectados** detalla todos los pares de todas las redes WireGuard — el dispositivo, la cuenta que lo inscribió, su dirección de superposición, el endpoint desde el que marca, el estado vivo de handshake y transferencia y la caducidad de su inscripción — con una acción de Desconectar por fila.

## TLS y la CA Local

Town OS ejecuta su propia autoridad de certificación X.509 para que el tráfico de paquetes y páginas se sirva por HTTPS y por nombre, sin ninguna CA pública ni dependencia de ACME en la LAN.

- **La CA** (`src/tls/ca.go`) es un par de claves ECDSA P-256 bajo el subvolumen btrfs `tls` (`ca.crt`, `ca.key`), con validez de 10 años, para que sobreviva a los reinicios. `EnsureCA` carga una CA existente o genera una bajo demanda; el certificado es legible por todos y la clave es solo del propietario y nunca debe servirse. Un fallo de la CA no es fatal — el sistema arranca sin HTTPS en lugar de no arrancar.
- **Las hojas** (`src/tls/leaf.go`) son por paquete y por página, escritas como `cert.pem`/`key.pem` en un solo directorio para que quien las consuma necesite una única ruta de montaje. `IssueLeaf` es **idempotente**: cuando un certificado existente ya cubre exactamente el conjunto de SAN solicitado y sigue siendo válido, retorna sin tocar el disco, que es lo que permite que la reconciliación lo llame en cada arranque sin remover los archivos de certificado. Los nombres de host pueden ser nombres DNS o literales de IP; todo lo que se analiza como IP va a `IPAddresses`, y todo lo demás a `DNSNames`.
- **`GET /tls/ca.crt`** es **público** (y está en `grantCommonRoutes`) para que cualquier cliente — un navegador, un teléfono que se une por la superposición — pueda descargar la raíz y confiar en el equipo.

El conjunto de SAN de la hoja de un paquete se deriva del mismo FQDN único que su registro A, su propietario DANE TLSA y su vhost del ingress; véase [El FQDN del paquete es una sola cadena](#el-fqdn-del-paquete-es-una-sola-cadena--registro-a-san-de-la-hoja-propietario-tlsa-vhost-del-ingress). Las hojas también llevan la IP de superposición del equipo en la red de instalación, para que un par pueda alcanzar el paquete por su dirección WireGuard cruda y no solo por nombre.

## Ingress

El ingress es el router de Host compartido: un sidecar que supervisa un hijo Caddy y expone una API de gestión gRPC que el systemcontroller programa, igual que programa rolodex. Mantiene el conjunto de rutas deseado en memoria, renderiza un Caddyfile en cada cambio y recarga Caddy sin tiempo de inactividad.

- **`src/ingress`** es el servicio dentro del contenedor (`Server`, `renderCaddyfile`, el cliente gRPC, el binario `town-os-ingress`). Se compila con `CGO_ENABLED=0`.
- **`src/ingress/ingressctl`** es el controlador de ciclo de vida del lado del systemcontroller: genera, instala y reinicia la unidad `town-os-system--ingress` y expone la ruta del socket gRPC que marca el systemcontroller. Es un paquete separado precisamente para que el binario del ingress, libre de CGO, nunca importe `src/systemd` (que arrastra cgo mediante sdjournal).

### Enrutado

- **`:443`** — un vhost `https://<hostname>` por ruta, terminando TLS con la hoja de la CA local fijada al archivo de esa ruta, o con un emisor ACME explícito para un FQDN público, y haciendo proxy inverso al contenedor de backend en la red podman compartida `town-os-ingress`.
- **`:80`** — enrutado por Host: las páginas (`ServeHttp`) se sirven directamente por HTTP plano (contenido estático, nada sensible), los paquetes reciben una redirección HTTP→HTTPS para que se queden solo en HTTPS, y cualquier host que no coincida con una ruta cae al backend predeterminado — la interfaz de Town OS, para que el inicio de sesión por IP desnuda (`http://<ip-del-equipo>/`) siga funcionando ahora que la interfaz ya no ocupa el `:80` del host.
- Una ruta **sin hoja emitida todavía** (no ACME, directorio de certificados vacío) se salta para HTTPS, así que una entrada a medio aprovisionar nunca hace que Caddy rechace la configuración entera; una página sigue obteniendo su vhost de `:80`, que no necesita certificado. Los paquetes solo se redirigen una vez que el destino HTTPS existe de verdad, así que nada redirige hacia un certificado aún sin aprovisionar.

### Renderizado

La salida está **ordenada por nombre de host** para que los bytes renderizados sean deterministas entre reconciliaciones — eso es lo que permite al supervisor no hacer nada ante una recarga cuyo contenido no ha cambiado. Los globales son `auto_https off` (Town OS gestiona los certificados) y `protocols h1 h2` (el ingress publica solo TCP, así que H3/QUIC sobre UDP es inalcanzable). La API de administración de Caddy se deja deliberadamente **habilitada** en su `localhost:2019` local al contenedor: el supervisor programa las rutas nuevas con `caddy reload`, que habla con ese endpoint, así que `admin off` rompería todas las actualizaciones de rutas después del primer arranque.

El ingress es **agnóstico a la interfaz**: publica `-p 443:443` / `-p 80:80` sin ninguna IP de host y su Caddyfile no lleva **ninguna directiva `bind`**, así que Caddy escucha en todas las interfaces y selecciona el vhost puramente por SNI/Host. Un cliente de la LAN y un par de la superposición llegan al mismo listener, seleccionan por SNI el mismo vhost, obtienen la misma hoja de la CA local y se les hace proxy al mismo contenedor. No añadas directivas `bind` ni listeners por red.

Producción enlaza 443/80; las pruebas de integración pasan puertos efímeros (renderizados como `host:PUERTO`) para que `make test-full` nunca colisione en un puerto privilegiado. El arranque programa el conjunto completo de rutas de forma declarativa mediante `RebuildIngress`, el mismo modelo de empuje que `RebuildDNS`; el CRUD de paquetes y páginas programa cambios incrementales por la misma API gRPC.

## Estado de Arranque y Refresco

`:5309` se enlaza antes de que ocurra cualquier trabajo de arranque, para que la interfaz pueda ver avanzar un arranque — incluida una autoactualización — en lugar de sondear un puerto muerto.

### El Stub de Arranque

`NewBootHandler` es un `http.ServeMux` desnudo (intencionadamente, para que nunca pueda montar por accidente una ruta real de la API) que sirve exactamente tres cosas:

- `GET /status/ping` → `{booting, step, done, error, boot_id}`. Responde **503 mientras arranca** y 200 una vez terminado, para que los sondeos externos de disponibilidad — el `wait_for_url` del contenedor de pruebas, las comprobaciones de salud de un orquestador — no traten el stub como "servicio listo" y empiecen a machacar un controlador a medio arrancar. El cuerpo JSON sigue llevando los campos de progreso, así que la interfaz puede distinguir "levantándose" de "totalmente caído".
- `GET /boot-status` → un flujo SSE de eventos de progreso.
- todo lo demás → **403**, no 404: la ruta existe en el manejador completo, simplemente no está disponible hasta el intercambio.

`RootHandler.Swap` sustituye atómicamente el stub por el router Echo completo al final del arranque. El socket del listener no se cierra nunca, así que no hay parpadeo de puerto, y los manejadores SSE ya despachados conservan su propio escritor y siguen transmitiendo a través del intercambio.

### Etapas de Progreso

Cinco etapas gruesas, deliberadamente pocas y de cara al usuario — una persona que mira una autoactualización quiere saber si es "el controlador", "el DNS", "los servicios del sistema" o "mis paquetes" lo que está frenando las cosas, no qué constructor interno se está ejecutando:

`boot_controller` → `boot_dns` → `boot_services` → `restart_packages` → `ready`

La etapa de frescura emite un evento adicional por paquete instalado, con el prefijo `restarting_` (`PackageStepPrefix`); la interfaz quita el prefijo y renderiza cada uno como su propia fila, con el mismo peso que las etapas gruesas, así que un equipo con muchos paquetes muestra progreso real en lugar de una única barra estancada. Estos nombres por paquete deliberadamente no coinciden con la forma `[a-z0-9_]+` que se exige a las etapas fijas — son valores dinámicos.

Los literales de las etapas están duplicados como llamadas `bs.Step("...")` en `main.go` en lugar de referenciarse como constantes, porque `TestBootStepsFrontendInSyncWithBackend` los extrae de `main.go` para demostrar que la lista del frontend coincide. **Mantén los dos sincronizados**; esa prueba falla ruidosamente si se separan.

### Semántica de la Difusión

`BootStatus` es seguro para uso concurrente y **nunca bloquea el arranque**. `Subscribe` reproduce primero el historial hacia el nuevo suscriptor (para que un suscriptor tardío no se pierda nada), dimensionando el búfer para que quepa la reproducción completa más margen; si el arranque ya terminó, cierra el canal justo después de la reproducción para que los consumidores con `for range` salgan. `publish` envía sin bloquear — un suscriptor cuyo búfer se llena se descarta y se cierra, y su cliente se reconecta y obtiene la reproducción del historial. Ningún evento puede seguir a `Done`.

### Identidad del Proceso y Refresco

`boot_id` es un UUID aleatorio regenerado en cada arranque del systemcontroller, informado por **ambos** `/status/ping`, el del stub y el del router completo (y presente incluso en la respuesta mínima de ping sin autenticar, ya que un navegador se queda brevemente sin token a través de un reinicio). Un cliente que capturó el identificador antes de pedir un refresco puede distinguir "el proceso antiguo sigue respondiendo" (mismo identificador) de "el proceso nuevo está en pie" (identificador distinto) — de otro modo son indistinguibles, porque ambos sirven un ping 200 y ambos dan 404 en `/boot-status` una vez arrancados. Esto es lo que permite al flujo de Refrescar Servicios Centrales de la interfaz observar a su propio sucesor.

`/boot-status` se excluye del registro de auditoría por la misma razón: una interfaz que mantiene el flujo abierto a través del intercambio del manejador aterriza su siguiente petición en el router completo, que da 404. Ese es el final esperado del flujo, no una acción del operador — auditarlo archivaría una fila de acción fallida en cada refresco exitoso e inflaría la píldora roja de fallos del panel.

`POST /system-services/refresh` (admin) descarga la imagen de todos los servicios del sistema en orden de dependencias — primero la imagen del systemcontroller (el ancla de versión, para que la imagen recién descargada ya esté local cuando se autorreinicie al final), luego rolodex (el DNS del equipo, que las demás descargas pueden necesitar para resolver su registro) y después todo lo demás en paralelo (máximo 3 concurrentes) — y deja una marca que la etapa de frescura del siguiente proceso consume para reiniciar los paquetes instalados.

## Gestión de DNS (Rolodex)

Town OS incluye un resolutor DNS local integrado impulsado por un contenedor `rolodex-dns`. El servidor rolodex gestiona archivos de zona y registros para los paquetes instalados, proporcionando resolución de nombres local mediante una interfaz gRPC sobre socket Unix.

### Gestor de Rolodex

Rolodex es en sí mismo un servicio de arranque instalado y supervisado por systemd — el systemcontroller no lo instala, arranca, detiene ni reinicia a nivel de contenedor. En su lugar, el `rolodex.Manager`:

- **`WriteConfig`** -- escribe `rolodex.yml` en `DataDir`. Idempotente: se salta la escritura cuando el archivo existe, es más reciente que el binario del systemcontroller y ya coincide con el contenido esperado. Devuelve un booleano que indica si el archivo se escribió (para que quien llama pueda decidir si reinicia la unidad de systemd).
- **`WaitForDNSReady`** -- sondea `DNSLoopback:{puerto}` por TCP hasta que acepta una conexión o pasa la fecha límite de 30 segundos. Se llama en el arranque antes de cualquier operación que dependa del DNS (p. ej., descargas de imágenes).
- **`SystemServices`** -- devuelve los metadatos del servicio de sistema rolodex (clave, nombre para mostrar, imagen, puerto, nombre de unidad) para que aparezca junto a los demás servicios del sistema en las respuestas de estado y en la interfaz.
- **`Status`** -- consulta el estado de la unidad de systemd para informar de si rolodex está en marcha.

El contenedor de rolodex se ejecuta con `--net host` y enlaza el DNS a `DNSLoopback` (`127.0.0.2`) en el puerto configurado (`53` por defecto, anulable mediante `DNSPort` para las pruebas). La etiqueta de la imagen se deriva de la etiqueta de publicación del controlador del sistema (`quay.io/town/rolodex:<tag>`), anulable mediante la variable de entorno `ROLODEX_IMAGE`.

**Modo de resolución.** `rolodex.yml` fija `resolution.mode` explícitamente mediante `Config.ResolutionMode`, con valor predeterminado **`auto`** (`DefaultResolutionMode`) — la propia cadena escalonada de respaldo de rolodex: iterar desde los servidores raíz, luego DoH/DoT, luego la lista `forwarders:`, luego un resolutor público en el :53, quedándose con el escalón que funcionó la última vez. El modo se escribe explícitamente en lugar de dejarlo al valor predeterminado de rolodex, para que el comportamiento de Town OS no se mueva cuando el proyecto original cambie su predeterminado. La prueba de integración de reenvío opta por `ResolutionModeForward` y apunta los reenviadores a un stub local.

**No uses `recursive` a secas por defecto.** *No* tiene ningún respaldo, y el resolutor iterativo de rolodex (`src/resolver.rs`) envía **un único datagrama UDP sin retransmisión por servidor de nombres con una fecha límite de 1500 ms**; cuando todos los servidores del conjunto de delegación actual fallan, `resolve()` da error e `iterative_query` convierte *cualquier* error en SERVFAIL. Así que un solo paquete perdido produce SERVFAIL en una consulta, y en una red que filtra o secuestra el :53 saliente (hotel, portal cautivo, algunos ISP) *todos* los nombres externos dan SERVFAIL. `auto` conserva la privacidad de la recursión allí donde la red lo permite y degrada en lugar de fallar donde no. Relacionado: la caché de delegación y la caché negativa de rolodex aterrizaron en `ce44bb5`, que **no está en ninguna etiqueta publicada** — hasta que salga una versión con eso, el modo recursivo vuelve a recorrer desde las raíces cada nombre no cacheado y cada NXDOMAIN (medido: 0,6–1,9 s por nombre público en frío, 2,7 s para un PTR de RFC1918).

El modo es configurable por el operador en tiempo de ejecución mediante el ajuste `dns_resolution_mode` (`auto` | `recursive` | `forward`; validado por `ValidateDNSResolutionMode`, así que un valor no analizable nunca puede llegar a `rolodex.yml` y dejar el DNS inservible). `main.go` lo lee en `rolodex.Config` en el arranque; un cambio mediante `POST /settings/set` ejecuta `Controller.RefreshDNSResolutionMode`, que llama a **`Manager.RewriteConfig()`** y reinicia la unidad de rolodex. `RewriteConfig` existe precisamente porque `WriteConfig` se niega a sobrescribir un `rolodex.yml` más reciente que el binario del systemcontroller (lo trata como editado a mano) — y el archivo escrito en el arranque anterior *siempre* cumple esa condición, así que `WriteConfig` no haría nada en silencio ante un cambio iniciado por el operador. Usa `WriteConfig` en el arranque y `RewriteConfig` para los cambios en tiempo de ejecución.

### Reenviadores locales

La lista `forwarders:` que Town OS escribe por defecto es `DefaultForwarders` — resolutores públicos. En una red que bloquea el DNS externo (un hotel, un portal cautivo, un ISP que descarta el `:53` saliente hacia cualquier cosa que no sean sus propios servidores) esas son precisamente las direcciones que se están descartando, así que el escalón de reenviadores de `auto` — el escalón al que se llega *después* de que las raíces y los upstreams cifrados ya han fallado, que es exactamente este caso — no tiene nada a lo que recurrir. El resolutor que esa red repartió por DHCP sí responde.

El ajuste `dns_local_forwarders` (`false` por defecto, validado por `ValidateBool`) sustituye la lista de reenviadores por los resolutores a los que apunta la propia configuración de red de este equipo. **No es un modo de resolución**: cambia *qué* direcciones tiene el escalón local, y el modo sigue decidiendo si ese escalón se consulta siquiera — en `auto` es el último recurso, en `forward` es el único upstream, en `recursive` no se usa. Activarlo, por tanto, nunca debe mover el modo.

**Desactivado es lo predeterminado, y es la dirección que importa.** El resolutor local ve todos los nombres que busca la casa, que es justo lo que resolver desde las raíces existe para evitar. Ese es un intercambio que un operador hace a sabiendas, no uno que un equipo hace por él la primera vez que una red se porta mal.

El descubrimiento vive en `src/rolodex/hostdns.go`. `HostResolversFrom` lee `hostResolvConfPaths` en orden — `/run/systemd/resolve/resolv.conf` **primero**, luego `/etc/resolv.conf` — y gana el primer archivo que produce una dirección utilizable, no meramente el primer archivo que existe. El orden es estructural: en un equipo con resolved, `/etc/resolv.conf` es el stub (`127.0.0.53`), que se descarta por ser loopback, así que un descubrimiento que se detuviera en el primer archivo *legible* no encontraría nada precisamente en los equipos para los que existe esta funcionalidad. El archivo del enlace ascendente es alcanzable desde dentro del contenedor porque la unidad del systemcontroller monta por bind `-v /run/systemd:/run/systemd`; perder ese montaje degrada el descubrimiento en silencio. Las direcciones de loopback, no especificadas, multicast y link-local se descartan todas — reenviar al stub de resolved o al propio listener `DNSLoopback` de rolodex es un bucle de consultas, no un upstream, y una dirección link-local carece de sentido sin la zona que una línea de `resolv.conf` no lleva.

**Un descubrimiento que no encuentra nada conserva los reenviadores ya configurados.** `Manager.forwarders()` recurre a `Config.Forwarders` y después a `DefaultForwarders`, así que activar el conmutador nunca puede dejar el escalón local apuntando a nada — lo cual sería estrictamente peor que los valores públicos predeterminados que se activó para sustituir.

`main.go` lee el ajuste en `rolodex.Config` en el arranque (un valor almacenado no analizable se lee como desactivado — la dirección segura), así que un equipo que cambió de red toma el nuevo resolutor en el siguiente arranque sin ninguna acción del operador. Un cambio mediante `POST /settings/set` ejecuta `Controller.RefreshDNSLocalForwarders`, que — a diferencia del modo de resolución — **no** hace cortocircuito cuando la marca no cambia: con ella ya activada, las direcciones descubiertas pueden haberse movido, y volver a renderizar es como eso llega a rolodex. `RewriteConfig` sigue informando de si los bytes cambiaron de verdad, así que un renderizado idéntico no cuesta ningún reinicio.

`GET /dns/status` informa de **ambos**: `local_forwarders` (lo que pidió el operador) y `forwarders` (lo que realmente contiene `rolodex.yml`). Discrepan en exactamente un caso — el descubrimiento no encontró nada utilizable y se conservaron los valores públicos por defecto — que es el único caso en el que el conmutador se lee como activado y no cambia nada, así que una interfaz que mostrara solo la marca estaría mostrando un ajuste que no está en vigor. La pantalla de Ajustes renderiza la lista efectiva por esa razón, y lo dice explícitamente cuando está vacía.

**La imagen de rolodex se descarga por arquitectura en pruebas y en desarrollo** — el arnés de make descarga la etiqueta rc por arquitectura del host `quay.io/town/rolodex:rc.latest-<arch>` (donde `<arch>` es la forma cruda de `uname -m`, `x86_64`/`aarch64`), NO la `rc.latest` simple sin arquitectura. Las descargas internas de imágenes de Town OS usan por defecto el canal rc, así que el arnés, el entorno de desarrollo y la ejecución siguen todos `rc.latest-<arch>`. Rolodex publica etiquetas por arquitectura subidas de forma nativa desde cada host (`make push-rc` / `make push-release` en el repositorio rolodex-dns), así que no hace falta ensamblar ningún manifiesto multiarquitectura para hosts de prueba de ninguna arquitectura; la `rc.latest` *simple* (sin sufijo de arquitectura) es un manifiesto de una sola arquitectura y entra en bucle de caídas con `exec format error` en la otra arquitectura — solo la `rc.latest-<arch>` con sufijo es segura de descargar directamente. El Makefile calcula `HOST_ARCH` (normalizado a `x86_64`/`aarch64`) y usa por defecto `ROLODEX_IMAGE_TAG ?= rc.latest-$(HOST_ARCH)`; `ROLODEX_IMAGE` se deriva de ella y se inyecta en los contenedores de prueba/desarrollo mediante el entorno. Anúlalo con `make ROLODEX_IMAGE_TAG=<tag> ...` (p. ej. `latest-$(HOST_ARCH)` para un rolodex publicado) o con la variable de entorno `ROLODEX_IMAGE`. El comportamiento en producción/ejecución coincide — el systemcontroller deriva la etiqueta de su etiqueta de publicación (recurriendo a `rc.latest-<arch>` mediante `defaultVersionTag()`) salvo que `ROLODEX_IMAGE` esté establecida; los arneses de prueba y de desarrollo siempre la establecen. La unidad de rolodex incrustada en el contenedor de desarrollo (`integration/testdata/town-os-system--rolodex.service`) usa un marcador `@ROLODEX_IMAGE@` sustituido en el momento de construir la imagen mediante el argumento de compilación `ROLODEX_IMAGE` en `integration/testdata/Containerfile.dev` (la compilación falla si el argumento está vacío), así que la unidad incrustada siempre coincide con la imagen que carga el arnés.

### TLD de red, doble hogar y resolución split-horizon

Cada red posee un TLD, registrado en rolodex como un ámbito de red cuyo
`home_domain` es el TLD (`rolodex.EnsureNetworkScope`, llamado desde
`applyNetworkTransport` en `controller_networks_reconcile.go`). Poseer el TLD es
lo que lo **particiona**: rolodex esconde el TLD de un ámbito de cualquier par
WireGuard unido a un ámbito *distinto*. La red predeterminada/del hogar
(`account.DefaultNetworkName`, con el TLD del ajuste `dns_tld`, `home` por
defecto) posee `home.` como un ámbito **solo DNS** — no obtiene interfaz
WireGuard, ni subred de superposición, ni asociación de pares, así que ninguna IP
de origen se enlaza jamás al ámbito del hogar. Por tanto, `.home` es solo de LAN
y está oculto a todos los pares WireGuard, pero es plenamente resoluble en la LAN.

**Doble hogar.** Un paquete instalado en una red no predeterminada se publica dos
veces (`registerScopedPackageDNS`):

- un registro A **acotado** bajo el TLD de la red en la **IP de superposición** del
  equipo — servido a los pares de la superposición WireGuard por IP de origen
  (`AddScopedRecord`); y
- un registro A **global** para el mismo FQDN en la **IP de LAN** del equipo
  (`RegisterPackageDNS`) — servido a los clientes de loopback/LAN.

Cada lado recibe una dirección a la que realmente puede enrutar. No se publica
ninguna zona autoritativa global para el TLD de la red: un registro A global
desnudo se resuelve en la LAN sin zona, y el **respaldo LAN→ámbito propietario**
de rolodex (rolodex-dns, paso 5 de resolución) trata el TLD propiedad del ámbito
como autoritativo para los orígenes de LAN — así que un nombre sin coincidencia
bajo un TLD de red produce un NXDOMAIN autoritativo desde la LAN, en lugar de
filtrar el TLD privado aguas arriba. Los paquetes de la red predeterminada se
quedan solo en la zona global del hogar (`registerPackageDNS`); un paquete no
predeterminado nunca debe aparecer ahí (el error original de "se resuelve como
`.home`").

**Resumen del split-horizon.** Un cliente de LAN (sin WireGuard) resuelve **todos**
los TLD de red (`.home` y el TLD de todas las redes WireGuard) más la internet
pública. Un par WireGuard unido a una red resuelve **solo** el TLD de esa red más
la internet pública — el TLD de una red hermana y `.home` devuelven ambos
NXDOMAIN. La vista de la LAN nunca se particiona; solo los pares de la
superposición. `RebuildNetworkDNS` (`reconcile.go`, llamado en el arranque) vuelve
a registrar el registro global de cara a la LAN de cada paquete de red no
predeterminada, para que un paquete ya instalado siga resolviendo en la LAN tras
un reinicio; los registros acotados persisten en rolodex de forma independiente.
A la reconciliación de redes del arranque se le pasa el cliente de rolodex para
que el ámbito del hogar (y todos los ámbitos de red) queden establecidos incluso
en un arranque en frío.

### El FQDN del paquete es una sola cadena — registro A, SAN de la hoja, propietario TLSA, vhost del ingress

**El nombre DNS de un paquete siempre se deriva del TLD de su *red de
instalación*, nunca del ajuste global `dns_tld`.** `packageFQDN(repo, name, tld)`
(`src/svc/systemcontroller/controller_tls.go`) es la única fuente de verdad, y el
TLD viene de `networkTLDValue(nm, settingsMgr, network)` (que recurre a `dns_tld`
solo para la red predeterminada). Cuatro cosas deben nombrar un paquete de forma
idéntica, y un desajuste en cualquiera de ellas rompe el servicio en silencio:

1. su **registro A**, 2. el **SAN de su certificado hoja**, 3. su **propietario
DANE TLSA**, y 4. su **vhost del ingress compartido en :443**.

**Los tres publicadores componen ese nombre a través de un único validador.** Un
paquete, una página y una partición de almacenamiento de objetos obtienen cada
uno un nombre bajo el TLD de una red, y cada uno lo componía por su cuenta —
discrepando sobre qué era un nombre legal. `gfehFQDN` normalizaba la etiqueta,
validaba todos los componentes separados por puntos contra la regla estricta LDH
y rechazaba un nombre que al calificarse se pasara del límite de 253 caracteres;
`packageFQDN` era concatenación desnuda sin ninguna de las dos comprobaciones, y
`pageFQDN` no comprobaba nada más allá de recortar. `qualifyPublishedName`
(`src/svc/systemcontroller/published_name.go`) es ahora el único compositor, y
aplica las reglas de gfeh a los tres; `validatePublishedName` es la mitad que no
califica, para un nombre que hay que comprobar pero no componer. Un nombre que
falla se **descarta** — todos los recolectores ya se saltan un FQDN vacío, así
que no aporta ningún registro, ninguna ruta, ningún certificado ni ningún
directorio en lugar de aportar uno roto a los cuatro — y el rechazo se registra
en nivel **Error**, porque `LOG_LEVEL` es `error` por defecto y un servicio que
deja de resolver en silencio no puede ser descubrible solo subiendo el nivel de
registro.

**El dominio de una página se valida en la API, no solo al componerlo.** Para una
página el nombre es una *quinta* cosa: su subvolumen en disco y su enlace
simbólico de webroot, ya que el Caddy de pages tiene su raíz en `/srv/<host>`.
`ValidatePageDomain` se ejecuta tanto en `POST /pages/create` como en
`POST /pages/update`, devolviendo 400. La actualización era la ruta que
importaba: la creación quedaba cubierta de forma incidental porque
`CreateFilesystem` ejecuta `storage.ValidateFilesystemName` y el manejador
deshace los cambios antes de llegar al código del enlace simbólico, mientras que
`migratePageDir` registra un fallo de `RenameFilesystem` y sigue adelante hasta
`RemovePageSymlink` / `EnsurePageSymlink` de todos modos.

Lo sutil es que un **FQDN público está exento de la calificación, pero no de la
validación**. `isPublicFQDN` lee cualquier nombre con puntos que no termine en el
TLD como el dominio propio del operador, que hay que servir literalmente mediante
ACME — lo cual es correcto para `blog.example.com` y es también como
`../escape.example.com`, `site.example.com/../../etc` y
`site.example.com other.example.com` llegaban sin examinar a `filepath.Join` y al
Caddyfile. "Es el dominio del operador" es una razón para no componerlo bajo el
TLD del equipo; nunca es una razón para no comprobarlo.

Para que no se separen, el FQDN se calcula **una sola vez** — en `applyPackageTLS`,
en la misma línea que emite la hoja — y se persiste como
`PackageNetworkState.FQDN` (`fqdn` en el JSON de estado de red por paquete). El
constructor de rutas del ingress (`collectPackageIngressSites`) lee ese campo en
lugar de recomponer el nombre, así que el vhost es por construcción el nombre para
el que el certificado es válido. `reconcileWriteNetworkState` toma el TLD **de
quien la llama** (`reconcilePackage`, que lo resolvió desde la red de instalación);
nunca debe llamar a `reconcileDNSTLD` ella misma. Hacerlo fue un error real: cada
arranque reemitía la hoja de un paquete de la red `fart` con el SAN
`<pkg>.<repo>.home`, machacando el SAN `.fart` correcto, mientras el ingress
renderizaba un vhost `<pkg>.<repo>.home` que nadie marcaba — así que el paquete se
resolvía en la LAN pero nunca se servía. Un `fqdn` vacío (archivo de estado previo
a la actualización, o un paquete no HTTP) recurre al TLD global y se autorrepara en
la siguiente reconciliación.

**El ingress es agnóstico a la interfaz y no necesita ningún enlace por red.**
Publica `-p 443:443` / `-p 80:80` sin IP de host (`0.0.0.0`, así que la LAN +
WireGuard + loopback llegan todos a él) y su Caddyfile no tiene **ninguna
directiva `bind`**, así que Caddy escucha en todas las interfaces y selecciona el
vhost puramente por **SNI/Host**. A los backends se llega por nombre de contenedor
en la red podman compartida `town-os-ingress`, a la que se une todo paquete con
frontal HTTP con independencia de su red WireGuard. Un cliente de LAN y un par de
la superposición llegan por tanto al mismo listener, seleccionan por SNI el mismo
vhost, obtienen la misma hoja de la CA local y se les hace proxy al mismo
contenedor. Nada enlaza un socket de escucha a una IP de superposición —
`BindOverlayAddress` es una *asociación de ámbito DNS* de rolodex, no un enlace de
socket. No añadas directivas `bind` ni listeners por red al ingress.

La hoja del paquete también lleva la **IP de superposición** del equipo en esa red
como SAN (`networkOverlayIPValue`), para que un par pueda alcanzar el paquete por
la dirección WireGuard cruda (`https://10.65.0.1`) y no solo por nombre. Está vacía
para la red predeterminada (que no tiene transporte WireGuard), lo cual evita que
las hojas de la red predeterminada se remuevan en cada reconciliación.

El DANE TLSA de un paquete de red está **en doble hogar como su registro A**:
`RebuildNetworkDNS` registra un anclaje global (servido a los orígenes de LAN
mediante el respaldo LAN→ámbito propietario) *y* un anclaje acotado (servido a los
pares de la superposición, cuyas consultas nunca ven registros globales). La
instalación por sí sola solo escribía la mitad acotada, y nada republicaba ninguna
de las dos mitades a través de un reinicio.

### Las páginas también están acotadas por red

Una página lleva una `network` (la columna `PageSite.Network`; `""` significa la
red predeterminada/del hogar, la misma convención que `Installer.LoadNetwork` para
los paquetes) y recibe **exactamente el mismo trato que un paquete**: su nombre
viene del TLD de esa red, está en doble hogar (registro acotado de superposición +
registro global de LAN), su hoja lleva el FQDN de la red más la IP de superposición
del equipo, su DANE TLSA se ancla bajo el TLD de la red (global + acotado) y está
oculta a los pares de todas las *demás* redes. `pageFQDN` (`pages_tls.go`) es el
gemelo del lado de las páginas de `packageFQDN`, y `pageNetworkTLD` el de
`networkTLDValue`.

La peculiaridad específica de las páginas: el FQDN de una página **también nombra
su subvolumen btrfs en disco y su enlace simbólico de webroot** (el Caddy de pages
tiene su raíz en `/srv/<host>`). Así que el FQDN no es una mera etiqueta —
equivócate y el contenido se sale de debajo del nombre que sirve el ingress. Tres
consecuencias:

- `reconcilePages` construye su conjunto `valid` con `pageFQDN`, porque ese conjunto
  gobierna `pruneStalePageSymlinks` — nombrar ahí `blog.home` a una página de `fart`
  fallaría en encontrar su directorio real `blog.fart` *y además* podaría el enlace
  simbólico vivo.
- Cambiar la **red** de una página renombra su subvolumen/enlace simbólico
  (`migratePageDir`), exactamente igual que hace un cambio de `dns_tld` con las
  páginas de la red predeterminada.
- `migratePageDirsForTLD` (el manejador del cambio de `dns_tld`) **se salta las
  páginas de redes no predeterminadas** — no están nombradas bajo el TLD global, así
  que renombrarlas rompería una página que funcionaba.

Las páginas las sigue sirviendo el único contenedor compartido
`town-os-system--pages` detrás del ingress; la red es solo una cuestión de
nomenclatura/DNS/certificados, sin ningún contenedor ni fontanería de podman por red.

### API de DNS

- `GET /dns/status` (requiere autenticación) -- devuelve el estado del DNS, incluidas la marca de habilitado, el estado de ejecución, el TLD, el recuento de registros, `local_forwarders` (si la lista de reenviadores se toma de los resolutores del propio host) y `forwarders` (las direcciones que realmente contiene `rolodex.yml` — véase [Reenviadores locales](#reenviadores-locales)).
- `GET /dns/records` (requiere autenticación) -- lista todos los registros DNS.
- `POST /dns/records/add` (requiere admin) -- añade un registro DNS. Acepta nombre, tipo de registro, valor y TTL.
- `POST /dns/records/remove` (requiere admin) -- elimina un registro DNS por nombre y tipo.
- `GET /dns/tld` (requiere autenticación) -- obtiene el dominio de nivel superior actual.
- `POST /dns/tld` (requiere admin) -- establece el TLD. Cambia el TLD existente y vuelve a registrar todos los paquetes instalados.
- `POST /dns/setup` (requiere admin) -- inicializa el DNS y registra todos los paquetes instalados.
- `GET /dns/rbl` (requiere autenticación) -- obtiene la configuración de RBL (Realtime Blackhole List, IP inversa): la marca global de habilitado, las zonas de proveedor con sus códigos de rechazo **resueltos a lo que está en vigor**, el `refusal_cooldown_secs` de toda la lista y `rotated_out` (los proveedores actualmente apartados tras rechazar una consulta, con el código y los segundos restantes). Véase [Códigos de rechazo](#códigos-de-rechazo-que-un-proveedor-diga-que-dejes-de-preguntar-no-significa-que-esto-esté-listado).
- `POST /dns/rbl` (requiere admin) -- sustituye la configuración de RBL. Acepta una marca de habilitado, un `refusal_cooldown_secs` para toda la lista y una lista de proveedores `{zone, enabled, refusal_codes, refusal_cooldown_secs}`. Las zonas se validan como nombres de host completamente cualificados, se pasan a minúsculas, se recortan y se deduplican; los códigos de rechazo los valida `ValidateRefusalCodes` (dirección IPv4 o `dirección/prefijo`, enmascarada al prefijo, `"none"` solo como entrada única, sin duplicados).
- `GET /dns/dnsbl` (requiere autenticación) -- obtiene la configuración de DNSBL (lista de bloqueo de dominios, por nombre directo), con la misma forma que `/dns/rbl`.
- `POST /dns/dnsbl` (requiere admin) -- sustituye la configuración de DNSBL (misma forma y validación que `/dns/rbl`; su tiempo de enfriamiento de rechazo es independiente del de la RBL).
- `GET /dns/rbl/local` (requiere autenticación) -- lista las entradas de la lista de bloqueo RBL local (`{name, reason}`).
- `POST /dns/rbl/local/add` (requiere admin) -- añade una entrada RBL local. Acepta un nombre (dominio o IP) y un motivo opcional. El nombre se valida (dominio o IP), se pasa a minúsculas y se recorta.
- `POST /dns/rbl/local/remove` (requiere admin) -- elimina una entrada RBL local por nombre.
- `GET /dns/dnsbl/allowlist` (requiere autenticación) -- lista las entradas de la lista de permitidos de DNSBL (`{name, reason}`).
- `POST /dns/dnsbl/allowlist/add` (requiere admin) -- exime un nombre de la comprobación de la lista de bloqueo por nombre. Acepta un nombre y un motivo opcional. El nombre se pasa a minúsculas, se recorta y se valida **solo como nombre de dominio** -- un literal de IP se rechaza (`ValidateDnsblAllowlistName`), porque la lista de permitidos compara nombres y sus subdominios y nunca podría coincidir con una dirección.
- `POST /dns/dnsbl/allowlist/remove` (requiere admin) -- elimina una entrada de la lista de permitidos por nombre. El nombre se normaliza pero no se vuelve a validar, así que una entrada anterior a un cambio de validación sigue pudiendo eliminarse.
- `GET /dns/services` (requiere autenticación) -- lista los servicios de paquetes instalados con su estado de publicación (en la zona DNS) (`{repo, name, version, fqdn, domains, published}`), deduplicados por repositorio/nombre.
- `POST /dns/services/set` (requiere admin) -- publica o retira de la publicación un servicio de paquete en la zona DNS. Acepta `{repo, name, published}`. Persiste la elección y registra/desregistra los registros de inmediato.

Los endpoints DNS de solo lectura (`/dns/status`, `/dns/records`, `/dns/rbl/local`, `/dns/dnsbl/allowlist`, `/dns/services`, `GET /dns/tld`, `GET /dns/rbl`, `GET /dns/dnsbl`) se excluyen del registro de auditoría. Las *escrituras* de la lista de permitidos sí se auditan (eximir un nombre de todas las listas de bloqueo es un cambio del que hay que rendir cuentas); igual que las escrituras de listas de bloqueo que reflejan, no llevan ninguna acción con nombre en `account.RouteActions` — la ruta las identifica.

### Listas de bloqueo RBL / DNSBL

Rolodex (0.2.4+) proporciona tres mecanismos complementarios de bloqueo de spam/malware/anuncios, más (0.4.3+) un mecanismo para deshacerlos y otro para no creerle a un proveedor que rechazó la consulta, todos expuestos mediante la API de DNS y la envoltura `rolodex.Client` (`SetRblConfig`/`GetRblConfig`, `SetDnsblConfig`/`GetDnsblConfig`, `AddLocalRblEntry`/`RemoveLocalRblEntry`/`ListLocalRblEntries`, `AddDnsblAllowlistEntry`/`RemoveDnsblAllowlistEntry`/`ListDnsblAllowlistEntries`). Todos los **consulta rolodex bajo demanda** — Town OS nunca descarga, analiza ni precachea fuentes de listas de bloqueo.

Ten en cuenta que los dos métodos `Set*` de la envoltura toman el tiempo de enfriamiento de rechazo de toda la lista como argumento final (`SetRblConfig(ctx, enabled, providers, refusalCooldownSecs)`); se mapean sobre los `Set*ConfigWithRefusalCooldown` del proyecto original, ya que las grafías originales que preservan la aridad existen por compatibilidad de API externa, algo que una envoltura interna no necesita.

- **RBL** (Realtime Blackhole List) -- zonas de lista de bloqueo por IP inversa que se consultan bajo demanda con una IP invertida contra una zona (p. ej. `zen.spamhaus.org`). Se comprueban contra las IP encontradas en consultas DNS inversas. Se configura mediante `/dns/rbl` como una lista de proveedores `{zone, enabled, refusal_codes, refusal_cooldown_secs}` más una marca global de habilitado y un `refusal_cooldown_secs` para toda la lista.
- **DNSBL** (lista de bloqueo de dominios) -- zonas de lista de bloqueo de dominios consultadas bajo demanda anteponiendo el dominio buscado a la zona (p. ej. `googleadservices.com` + `dbl.spamhaus.org`). Las coincidencias de DNSBL tienen prioridad sobre las respuestas reenviadas/iterativas. Se configura mediante `/dns/dnsbl` con la misma forma que la RBL, con su propio enfriamiento independiente.
- **Entradas RBL locales** -- una lista respaldada por la base de datos con nombres/IP gestionada manualmente mediante `/dns/rbl/local*`, comprobada antes que los proveedores externos. Una entrada local de **nombre de dominio** bloquea las búsquedas directas A/AAAA de ese dominio con `NXDOMAIN`, y surte efecto de inmediato (rolodex actualiza una caché en memoria al añadirla).
- **Lista de permitidos de DNSBL** (rolodex 0.4.3+) -- la escotilla de escape del operador ante un falso positivo de una fuente de terceros, gestionada mediante `/dns/dnsbl/allowlist*`. Una entrada cubre el nombre **y todos los nombres por debajo de él**, así que permitir `vendor.example` exime también a `cdn.vendor.example`. **Hace cortocircuito de toda la comprobación por nombre**, ganando tanto a los proveedores DNSBL configurados como a cualquier entrada RBL local coincidente, y se ejecuta *antes* de la búsqueda del proveedor, así que un nombre eximido nunca emite ninguna. También está respaldada por la base de datos con una caché en memoria, así que surte efecto de inmediato.

  Sin ella, el único remedio ante una fuente que lista un nombre que la casa necesita es deshabilitar el proveedor entero. Fíjate en la asimetría con la lista de bloqueo local: una entrada de la lista de permitidos es **solo un nombre**, nunca una IP, porque la comprobación que cortocircuita es la basada en nombres. La ruta RBL basada en IP no se ve afectada por ella.

  **Versión mínima:** un rolodex antiguo responde a las tres RPC de la lista de permitidos con `Unimplemented` de gRPC, que sale como un 500. Ni `make test` ni las pruebas de integración con mocks lo detectan — `TestRolodexDnsblAllowlistRoundtripReal` es lo que demuestra que la imagen fijada es lo bastante nueva.

#### Códigos de rechazo: que un proveedor diga que dejes de preguntar no significa que esto esté listado

Un DNSxL responde a una coincidencia y a una queja sobre quien consulta con el **mismo tipo de registro** — un `A` bajo `127.0.0.0/8` — así que lo único que los separa es la dirección. `127.0.0.2` significa que el nombre está listado; `127.255.255.254` significa que la consulta llegó por un resolutor público y `127.255.255.255` significa que quien consulta se pasó de su límite. Lee el segundo tipo como una coincidencia y **todos** los nombres comprobados contra ese proveedor se convierten en `NXDOMAIN`: la lista de bloqueo deja de ser una lista de bloqueo y se convierte en una caída del servicio. Spamhaus publica límites de uso gratuito que un equipo doméstico puede cruzar sin darse cuenta, y el síntoma cuando lo hace es que la web entera se apaga — lo cual se lee como un DNS roto, no como un límite de tasa.

Rolodex reconoce estos códigos y, ante un rechazo, **aparta a ese proveedor de la rotación de búsquedas durante un enfriamiento** en lugar de creerle. Town OS expone ambas mitades:

- **`refusal_codes`**, por proveedor, en ambas listas. Cada entrada es una dirección IPv4 o `dirección/prefijo` — un prefijo porque los proveedores documentan rangos enteros, y Spamhaus reserva todo `127.255.255.0/24` para errores y le añade códigos con el tiempo, así que enumerar los tres de hoy haría que mañana el cuarto se leyera en silencio como una coincidencia.
- **`refusal_cooldown_secs`**, por proveedor y para toda la lista. Un `0` en un proveedor defiere al valor de la lista; un `0` en la lista usa el valor predeterminado incorporado de rolodex (3600).
- **`rotated_out`** en el `GET`, informando de a qué proveedores no se está preguntando actualmente, el código con el que rechazó cada uno y los segundos restantes. Esta es la mitad visible para el operador: sin ella, la única señal de que una lista de bloqueo dejó de consultarse es que dejó de bloquear cosas.

**`ValidateRefusalCodes` (`controller_dns_validate.go`) refleja exactamente `resolve_refusal_codes` de rolodex**, porque la lista se pasa tal cual y discrepar sobre lo que significa una entrada sería peor que no validar en absoluto. Tres casos:

- **vacío** ⇒ rolodex sustituye su conjunto incorporado, así que una configuración escrita antes de que nada de esto existiera recibe la lectura segura sin ser editada;
- **exactamente `"none"`** ⇒ detección desactivada, para una lista de bloqueo privada cuyas coincidencias reales chocan con un código incorporado;
- **cualquier otra cosa** ⇒ exactamente esos códigos, con los incorporados deliberadamente **sin** fusionar.

`"none"` mezclado con códigos reales se rechaza — una lista que a la vez desactiva la detección y nombra códigos que detectar no tiene ninguna lectura que elegir. Los códigos se enmascaran a su prefijo y **un `/32` se renderiza desnudo**, coincidiendo con el `Display` de rolodex: un código que se leyera de vuelta distinto del que se acaba de enviar parecería que el equipo ha reescrito la entrada del operador.

**El `GET` informa de los códigos RESUELTOS**, así que un proveedor que no nombró ninguno se lee de vuelta llevando el conjunto incorporado — que es la gracia, ya que un operador tiene que poder ver con qué está comparando el equipo de verdad. También significa que **un cliente nunca debe devolver eso tal cual en el siguiente guardado**: hacerlo congela la lista de hoy dentro de la configuración almacenada, con lo cual un código que rolodex añada después empieza a leerse como una coincidencia — exactamente el fallo que esto existe para evitar, reintroducido una capa más arriba. `toWire` en `BlocklistsTab.jsx` colapsa un conjunto incorporado resuelto de vuelta a un campo ausente, y la interfaz guarda una copia de la lista incorporada (`BUILTIN_REFUSAL_CODES`) con un único propósito: decidir con qué radio se abre el diálogo de ajustes. Si esa copia se desvía, el diálogo se abre en "Custom" precargado con los códigos en vigor — un valor predeterminado erróneo y cosmético, no una configuración errónea, ya que nada cambia salvo que el operador guarde.

**Versión mínima:** un rolodex anterior al manejo de rechazos acepta estos campos — proto3 ignora los campos desconocidos — y no guarda nada. Las pruebas con mocks no pueden distinguir eso del éxito, porque un mock devuelve lo que le entregaron. `TestRolodexRblRefusalCodesRoundtripReal` y su gemela de DNSBL comprueban que una lista configurada **vacía** se lee de vuelta *resuelta*, que es la comprobación que una imagen antigua no supera.

**No hay ingestión ni precacheo de fuentes**: las zonas de proveedor son la unidad de configuración, y la interfaz ofrece una lista curada de zonas DNSBL/RBL conocidas como añadidos rápidos de un clic, pero el usuario puede añadir cualquier zona. Las escrituras de zonas de proveedor sustituyen la configuración entera (validada, en minúsculas y deduplicada).

**La lista de añadidos rápidos es un respaldo, y se cura sobre esa base** (`DNSBL_SUGGESTIONS` / `RBL_SUGGESTIONS` en `ui/src/routes/dns/BlocklistsTab.jsx`). Una zona pertenece ahí solo si un equipo doméstico puede usarla tal cual: que siga operativa, sea gratuita y responda a un resolutor que recursa por su cuenta sin ningún paso de registro. Actualmente DNSBL — Spamhaus DBL, SURBL, URIBL, NordSpam DBL, Spam Eating Monkey; RBL — Spamhaus ZEN, SpamCop, PSBL.

Tres están deliberadamente **ausentes**, y el caso "offers no decommissioned or registration-gated zones" de `TestBlocklistsTab` las mantiene así: `dnsbl.sorbs.net` se desmanteló el 2024-06-05 y sus zonas se vaciaron, así que es una operación permanentemente sin efecto que se lee como protección; `b.barracudacentral.org` exige registrar antes la IP que consulta, y un equipo sin registrar puede responder un tiempo y luego quedar cortado; los niveles 2/3 de UCEPROTECT listan ASN enteros, así que un solo vecino malo bloquea a todo un ISP. Las tres fallan *en silencio* — el operador ve una zona configurada y da por hecho que funciona.

Ten en cuenta además que las zonas RBL (IP inversa) solo se consultan para las IP encontradas en consultas DNS inversas, que la navegación ordinaria apenas genera. Las zonas DNSBL (de dominios) son las que afectan a la navegación, y están ajustadas para URL de spam en el correo más que para anuncios o rastreadores — el bloqueo de anuncios/rastreadores sería territorio de fuentes, que está [deliberadamente fuera de alcance](#listas-de-bloqueo-rbl--dnsbl).

### Publicación de DNS por Servicio

La publicación es de exclusión voluntaria: todo servicio de paquete instalado se publica en la zona DNS salvo que su clave `repo/name` esté listada en el ajuste `dns_excluded_services` (un array JSON). `/dns/services/set` alterna la pertenencia y registra/desregistra los registros de inmediato; `RebuildDNS` y `ReconcileDNS` filtran los servicios excluidos (mediante `filterExcludedDNSInfo` + `loadDNSExcludedServices`), así que la elección sobrevive a reinicios y reconciliaciones. Los servicios no publicados siguen funcionando pero no son resolubles por nombre.

### Interfaz de Gestión de DNS

La pantalla de gestión de DNS muestra el estado del DNS (habilitado, en marcha, TLD, recuento de registros) sobre cuatro subpestañas enlazables directamente (`?tab=`):

- **Records** -- la tabla de registros DNS con diálogos para añadir registros (tipos: A, AAAA, CNAME, MX, TXT, SRV, PTR), eliminar registros, cambiar el TLD y la configuración inicial del DNS.
- **Blocklists** -- las secciones de zonas de proveedor DNSBL y RBL (conmutador global de habilitado, habilitar/eliminar por zona, ajustes de códigos de rechazo por zona, añadidos rápidos de zonas sugeridas, añadir zona personalizada — todo consultado bajo demanda) más una tabla manual de entradas locales (añadir/eliminar). Cada sección encabeza con los proveedores actualmente apartados tras rechazar una consulta, cuando los hay. Sin fuentes, sin aplicar, sin nada cacheado.
- **Allow Lists** (`?tab=allowlists`, `ui/src/routes/dns/AllowListsTab.jsx`) -- la lista de permitidos de DNSBL: una tabla de dominios eximidos con sus motivos, más añadir y eliminar. Las lecturas son `requireAuth`, así que la pestaña no es solo para administradores; los controles de añadir/eliminar sí lo son. Es una pestaña hermana en lugar de una tarjeta dentro de Blocklists porque una exención es lo que un operador va a buscar por nombre cuando algo es inalcanzable, no algo que encontrar mientras se desplaza más allá de las zonas de proveedor.
- **Services** -- los servicios de paquetes instalados con un conmutador de publicación (publicar/retirar de la zona DNS).

## Endpoint de Estado

`GET /status/ping` (público) devuelve el estado del sistema, incluidos: recuentos de sistemas de archivos (de usuario, instalados, desinstalados), recuentos de repositorios y paquetes, recuento de paquetes instalados, recuentos de cuentas y de administradores, recuentos de unidades de servicio (total, activas, fallidas), recuentos de unidades de servicios del sistema (total, activas, fallidas), errores de auditoría recientes (últimos 5 minutos), estado de configuración inicial (`needs_setup` es verdadero solo cuando no existe ninguna cuenta de administrador habilitada; la página de inicio de sesión se muestra cuando hay administradores con independencia del estado de la sesión), IP externa (obtenida cada hora de ipinfo.io), IP interna (la primera dirección IPv4 que no sea de loopback), estadísticas de uso de disco, disponibilidad de actualizaciones, el desplazamiento UTC del servidor en minutos, la configuración regional actual, `proton_enabled` (si esta compilación lleva la etiqueta de compilación `proton`), `boot_id` y el nombre de usuario autenticado si se proporciona un token válido.

Los recuentos de unidades de servicio se dividen en dos campos: `units` cuenta solo las unidades de servicio de paquetes (las que coinciden con `town-os-package--*`), mientras que `system_services` cuenta las unidades de servicios del sistema (las que coinciden con `town-os-system--*`). Las unidades de systemd sobrantes de paquetes desinstalados se excluyen del recuento de paquetes. La lista de paquetes instalados se cruza con las unidades de systemd descubiertas construyendo el nombre de unidad esperado a partir de la identidad de cada paquete.

El manejador lista las cuentas una sola vez (se usa para `needs_setup`, el total y el recuento de administradores) y usa `FilesystemNames` en lugar de `ListFilesystems` para los recuentos de volúmenes — este último ejecuta `btrfs qgroup show` más una búsqueda de rootid por subvolumen, lo que con ~30 subvolúmenes costaba alrededor de un segundo del presupuesto de latencia del ping por una cuota que el ping nunca lee.

Las peticiones sin autenticar procedentes de orígenes que no son localhost reciben una respuesta mínima que contiene solo `status`, `needs_setup` y `boot_id`. `boot_id` viaja incluso ahí porque el flujo de refresco sondea el ping a través de un reinicio del controlador, durante el cual el navegador está brevemente sin autenticar; es un UUID aleatorio por proceso y no revela nada del sistema. Las peticiones autenticadas y todas las peticiones desde localhost reciben la respuesta completa con todos los campos enumerados arriba, más `repository_errors` (un mapa de nombre de repositorio a cadena de error que registra los fallos de refresco por repositorio).

Mientras el controlador todavía está arrancando, esta ruta la sirve en su lugar el stub de arranque y devuelve **503** con `{booting, step, done, error, boot_id}` — véase [Estado de Arranque y Refresco](#estado-de-arranque-y-refresco).

### Sondeo de la IP Externa

El controlador del sistema obtiene la dirección IP pública (externa) del servidor de `https://ipinfo.io/json`. El sondeador se arranca automáticamente cuando se crea el manejador HTTP (`NewHandler`) y cuando arranca el servidor sobre socket Unix. Obtiene la IP inmediatamente al arrancar y después sondea cada hora. Cada obtención tiene un tiempo límite HTTP de 10 segundos. El resultado se cachea en un valor atómico y se incluye en las respuestas de ping autenticadas como `external_ip`. Los fallos de obtención se registran a nivel de depuración y no afectan al resto del sistema; el campo se omite de la respuesta cuando no se ha obtenido ninguna IP.

## Monitorización

Una pila integrada de monitorización Prometheus + Node Exporter proporciona métricas del sistema. El `monitoring.Manager` gestiona la pila como contenedores podman supervisados por systemd (servicios del sistema) con `Restart=always`, usando el prefijo de nombre `town-os-system--`. El frontend de los paneles es configurable mediante el ajuste `monitoring_backend`.

### Puerto de Monitorización

El puerto **5308** es el puerto dedicado del panel de monitorización (`TOWN_OS_MONITORING_PORT` lo reubica; lo mismo hacen `TOWN_OS_PROMETHEUS_PORT` y `TOWN_OS_NODE_EXPORTER_PORT` con los dos puertos de loopback — véase [Puertos de host de los servicios del sistema](#puertos-de-host-de-los-servicios-del-sistema)). Los puertos llegan a los tres servicios como un único valor `monitoring.Ports` cuyos campos vacíos rellena `withDefaults()`, así que los valores por defecto viven en un solo sitio. El backend activo determina qué escucha en el puerto del panel:

- **Modo uPlot** (predeterminado): un reenviador socat (`socat TCP-LISTEN:5308,fork,reuseaddr TCP:localhost:9090`) expone la API HTTP de Prometheus en el puerto 5308. La interfaz React consulta `/api/v1/query_range` de Prometheus directamente y renderiza las gráficas con uPlot.
- **Modo Grafana**: Grafana escucha directamente en el puerto 5308 (mediante el mapeo de puertos de podman). La interfaz React incrusta un iframe de Grafana.

**No hay ningún proxy inverso** a través del systemcontroller (puerto 5309). El navegador habla directamente con el puerto 5308 para todos los datos de monitorización.

### Ajuste del Backend de Monitorización

El ajuste del sistema `monitoring_backend` controla qué frontend de paneles se usa:

- `"uplot"` (predeterminado) -- gráficas ligeras integradas renderizadas en la interfaz React con uPlot (~35 KB). Consulta Prometheus en el puerto 5308 mediante el reenviador socat. Grafana no se descarga ni se arranca, ahorrando ~771 MB en el primer arranque.
- `"grafana"` -- paneles completos de Grafana. La imagen de contenedor de Grafana se descarga y se arranca en el puerto 5308. Preaprovisionada con una fuente de datos de Prometheus y con todos los paneles del registro.

Cambiar el ajuste surte efecto de inmediato: cambiar a `"grafana"` descarga la imagen de Grafana y arranca el contenedor (deteniendo el reenviador socat); cambiar a `"uplot"` detiene Grafana y arranca el reenviador socat.

### Contenedores de Monitorización

- **Node Exporter** (`quay.io/prometheus/node-exporter:latest`, puerto de host 9100) -- recopila métricas del sistema anfitrión. Se ejecuta con el espacio de nombres PID del host, la capacidad `SYS_TIME` y un bind mount de solo lectura del sistema de archivos raíz del host en `/host`. La unidad de systemd pasa `--collector.diskstats.device-exclude=^(ram|fd)\d+$` (la constante `monitoring.DiskstatsDeviceExclude`) para anular el valor predeterminado original de node_exporter (`^(ram|loop|fd|(h|s|v|xv)d[a-z]|nvme\d+n\d+p)\d+$`), que filtra las particiones (`sda3`, `nvme0n1p3`) y los dispositivos de bucle — exactamente las formas de dispositivo que `monitoring.BtrfsDevices` informa para el sistema de archivos btrfs que respalda `/town-os`. Sin esta anulación, las consultas del panel de E/S de Disco devuelven en silencio cero series y el panel se renderiza vacío. No elimines ni aflojes el flag salvo que muevas también las consultas de E/S de Disco fuera de `node_disk_*`. Cobertura de regresión: `TestNodeExporterUnitConfigDiskstatsExcludeAllowsRealDevices` fija el flag y la expresión regular, y `TestMonitoringNodeExporterEmitsDiskMetricsForFilteredDevices` arranca un contenedor real de node_exporter y confirma que emite `node_disk_read_bytes_total` para al menos un dispositivo excluido por el valor predeterminado original.
- **Prometheus** (`quay.io/prometheus/prometheus:latest`, puerto de host 9090) -- recolecta de Node Exporter, de sí mismo, de rolodex (trabajo `rolodex`) y del controlador del sistema (trabajo `systemcontroller`, véase [Métricas del controlador del sistema](#métricas-del-controlador-del-sistema)) a intervalos de 15 segundos. Los dos trabajos opcionales se omiten cuando su dirección no está definida, en lugar de apuntarlos a un valor adivinado, ya que un objetivo que nadie configuró se queda permanentemente caído y se lee como un servicio roto en lugar de como uno ausente. Los datos se guardan con retención de 30 días en un directorio de datos persistente. Los volúmenes de configuración y datos se montan por bind desde un directorio de datos de monitorización. La unidad de systemd incluye directivas `ExecStartPre` de mkdir para precrear los directorios de volumen en el arranque.
- **Grafana** (`docker.io/grafana/grafana:latest`, puerto de host 5308) -- interfaz opcional de paneles, que solo se arranca cuando `monitoring_backend` es `"grafana"`. Usa un tema claro (`GF_USERS_DEFAULT_THEME=light`). La visualización anónima está habilitada con el rol Viewer y se permite la incrustación en iframe. La unidad de systemd incluye directivas `ExecStartPre` de mkdir para precrear los directorios de volumen en el arranque. Preaprovisionada con una fuente de datos de Prometheus y con los paneles descritos en [Paneles](#paneles); véase [Aprovisionamiento de paneles](#aprovisionamiento-de-paneles) para cómo llegan ahí.
- **Reenviador socat** -- la unidad `monitoring-ui` (`town-os-system--monitoring-ui.service`) en su forma uPlot, arrancada solo cuando `monitoring_backend` es `"uplot"`. Reenvía el puerto 5308 a Prometheus en el puerto 9090. Es la *misma clave de unidad* que usa Grafana, no una segunda: las dos son cuerpos alternativos de un mismo servicio, que es lo que permite que un cambio de backend sea una reescritura y un reinicio de la unidad en lugar de un par de llamadas de arranque/parada que podrían dejar corriendo las dos o ninguna.

### Paneles

Hay tres paneles, y **ambos backends renderizan los mismos tres a partir de las
mismas consultas**. Están separados en lugar de ser una sola página larga porque
responden a preguntas distintas: System es lo que un operador mira cuando el
equipo va lento, DNS es lo que abre cuando un nombre no se resuelve, y Controller
es lo que abre cuando algo que Town OS ejecuta no está ejecutándose. Plegar los
ocho paneles de DNS y los once de controller dentro de la vista general enterraría
los cuatro paneles de host, que son la razón por la que cualquiera la abre.

**System** (uid de Grafana `town-os-overview`, "Town OS Overview") -- cuatro paneles:

1. **E/S de disco (/town-os)** -- rendimiento de lectura/escritura sumado a través de los dispositivos de bloque que respaldan el sistema de archivos btrfs, de modo que el panel muestra una línea de Lectura y una de Escritura por muchos dispositivos que abarque el sistema de archivos. La expresión regular de dispositivos se sustituye desde `monitoring.BtrfsDevices`; una lista vacía se resuelve a `NoBtrfsDevicesSentinel`, que no coincide con nada, así que el panel se renderiza vacío en lugar de sumar en silencio todos los discos del host.
2. **Red (externa)** -- recepción/transmisión en bits/s por dispositivo físico (excluyendo `lo`, veth, podman, cni, tailscale, bridge y docker), unido contra `node_network_up == 1` para que las interfaces que existieron alguna vez pero ahora están caídas no dibujen líneas planas a cero en la leyenda.
3. **Uso de CPU** -- apilado por modo (user, system, iowait, irq, softirq, steal, nice) con una línea superpuesta de Total, 0--100 %.
4. **Uso de memoria** -- total, usada, disponible.

**DNS** (uid de Grafana `town-os-dns`, "Town OS DNS") -- ocho paneles sobre el
trabajo de recolección `rolodex`:

1. **Consultas DNS por código de respuesta** -- `rate(rolodex_dns_queries_total)` sumado por `rcode`, apilado. La separación es el panel y no un desglose porque un recuento de consultas a secas no puede distinguir un resolutor ocupado de uno que da SERVFAIL a todo — son la misma línea.
2. **Latencia de consultas** -- p50/p95/p99 de `rolodex_dns_query_duration_seconds_bucket`. Los cubos se suman por `le` *antes* de `histogram_quantile`, porque las series en crudo llevan una etiqueta `proto` y calcular los cuantiles sin agregarlas dibuja una línea por transporte en lugar de la latencia del equipo entero.
3. **Respuestas por origen** -- qué etapa de resolución respondió (caché, local, acotada, un escalón upstream), apilado. Este es el panel que dice si el equipo está respondiendo por sí mismo o reenviando.
4. **Ratio de aciertos de caché** -- aciertos más aciertos negativos sobre todas las búsquedas, 0--100 %. Un NXDOMAIN cacheado cuenta como acierto: ahorró una ida y vuelta upstream exactamente igual que uno positivo. El denominador está deliberadamente sin acotar, así que un equipo inactivo rompe la línea en lugar de dibujar un 0 % confiado para una caché a la que nadie ha preguntado nada.
5. **Entradas de caché** -- tamaños de las cachés positiva, negativa y de listas de bloqueo.
6. **Actividad de listas de bloqueo** -- bloqueos por tipo, permitidos y **rechazados**. Los rechazos comparten panel con los bloqueos a propósito: un proveedor que responde "deja de preguntar" en lugar de "listado" es lo que convierte en silencio una lista de bloqueo en una caída ([Códigos de rechazo](#códigos-de-rechazo-que-un-proveedor-diga-que-dejes-de-preguntar-no-significa-que-esto-esté-listado)), y solo se lee como anómalo junto a la tasa de bloqueos que sustituyó.
7. **Resultados por escalón upstream** -- victorias y fallos por escalón, más las consultas que agotaron todos los escalones.
8. **Tráfico DNS** -- bytes de cable rx/tx.

**Controller** (uid de Grafana `town-os-controller`, "Town OS Controller") -- once
paneles sobre el trabajo de recolección `systemcontroller`, y el único panel que lee
las [métricas `townos_*`](#métricas-del-controlador-del-sistema) del propio equipo:

1. **Service Units by State** -- `townos_system_units` y `townos_package_units` por estado, en un mismo panel y **sin apilar**: son dos totales separados, y apilarlos dibujaría una altura combinada que no cuenta nada que nadie administre.
2. **Service Health** -- `townos_system_unit_active` y `townos_package_unit_active`, una serie por unidad, fijado a 0--1. Este es el panel que dice *qué* servicio está caído, no cuántos. El eje se fija porque la métrica es booleana: en escala automática, un equipo completamente sano se dibuja como ruido alrededor de 1,0 y se lee como alarmante justo cuando no pasa nada.
3. **API Requests by Status** -- `rate(townos_http_requests_total)` sumado por `status`, apilado. Sumado por estado en concreto: la familia también lleva `method`, y un panel de estados que lo conservara dibujaría una línea por cada par.
4. **Audit Events** -- `rate(townos_audit_events_total)` por `result`, apilado.
5. **Recent Failures** -- `townos_audit_recent_errors` (el mismo recuento de cinco minutos que el panel de inicio muestra como su píldora roja) junto a `townos_repository_errors`. Ambos en un panel porque un operador que comprueba "¿hay algo roto?" no debería tener que saber primero bajo qué subsistema mirar, y ambos son gauges sobre una ventana reciente, así que volver a cero es una recuperación y no un contador que dejó de subir.
6. **Package Inventory** -- instalados, disponibles, actualizables y repositorios configurados.
7. **Town OS Disk Usage** -- `townos_disk_used_bytes` y `townos_disk_available_bytes`, apilados. Usado y disponible en lugar de usado y total: apilados, esos dos *son* el tamaño del sistema de archivos, así que una tercera serie solo lo repetiría.
8. **Accounts** -- `townos_accounts` por tipo, apilado (los tipos particionan la lista de cuentas exactamente una vez, así que la altura de la pila es el total real).
9. **Granted Accounts** -- `townos_accounts_granted`, aparte porque es un *subconjunto* del grupo de usuarios y no un cuarto tipo, y apilarlo lo contaría dos veces.
10. **btrfs Subvolumes** -- `townos_filesystems` por espacio de nombres, apilado.
11. **Controller Uptime** -- `time() - townos_start_time_seconds`. La señal es el diente de sierra, no la altura: un controlador que se reinicia en silencio bajo `Restart=always` se ve sano en todos los demás paneles de aquí.

`townos_up` y `townos_disk_total_bytes` deliberadamente **no** se grafican. El
primero es una constante de vitalidad de la recolección, y una línea plana en 1 no
es un panel; el segundo es la suma de las dos series que el panel 7 ya apila.

Todas las consultas de DNS llevan un selector `{job="rolodex"}` construido a
partir de `monitoring.RolodexJobName`, y todas las de controller uno
`{job="systemcontroller"}` construido a partir de `monitoring.ControllerJobName`,
así que la etiqueta que emite la configuración de recolección y la que seleccionan
los paneles no pueden separarse — un desajuste no es un error en ninguna parte, es
una pestaña entera de paneles leyendo vacío en un equipo que funciona.

Los dos frontends son código separado en lenguajes separados renderizando el mismo
panel, y la **única** diferencia es la ventana de tasa: Grafana expande
`$__rate_interval` por panel, y el frontend de uPlot no tiene expansión de macros,
así que fija `RATE_INTERVAL` (`5m`). Una macro filtrada en el lado de uPlot es un
error de análisis de Prometheus que deja en blanco toda la pestaña.

Cuatro tipos de prueba mantienen unidos los dos lados, porque nada más los conecta:

- `TestRolodexDashboardMirroredInFrontendQueries` y `TestControllerDashboardMirroredInFrontendQueries` leen `ui/src/components/monitoring/queries.js` desde la prueba de Go y fallan si cualquiera de los dos lados nombra una familia de métricas que el otro no — la misma guarda contra la deriva que `TestBootStepsFrontendInSyncWithBackend` aplica a las etapas de arranque.
- La prueba de integración de recolección de rolodex comprueba que la **imagen fijada de rolodex realmente exporta** todas las familias de `monitoring.RolodexDashboardMetrics()`, y `TestControllerDashboardMetricsAreServed` comprueba lo mismo para `monitoring.ControllerDashboardMetrics()` contra una recolección real del propio endpoint del controlador. Ambas comparan por la línea `# TYPE` para que una familia cuyo nombre es prefijo de otra no pueda avalar a una que falta. Un panel que nombra una familia que el equipo no emite renderiza una gráfica vacía, que es indistinguible de un equipo inactivo.
- `TestDashboardQueriesParseInPrometheus` pasa todas las expresiones de todos los paneles por un Prometheus real. Un PromQL malformado dentro de un JSON no es un error de sintaxis en ninguna parte: el archivo se aprovisiona, el panel carga, el gráfico dibuja sus ejes y dice "No data" para siempre.
- `MonitoringDashboard.test.jsx` comprueba que cada pestaña monta un componente uPlot **distinto**. La lista de pestañas nombra su componente en lugar de que una rama lo elija, porque una pestaña cuya rama se olvidó caía en los gráficos de System — un panel real bajo el encabezado equivocado, lo que se lee como que funciona.

El panel de controller es el único que depende de un trabajo de recolección que
puede estar ausente: `ports.ControllerMetrics` se deriva del valor de `-listen`, y
`WritePrometheusConfig` **omite** el trabajo en lugar de adivinar una dirección
cuando no puede derivarlo. Un trabajo omitido es un panel sin datos, no un panel
roto.

### Aprovisionamiento de paneles

`monitoring.GrafanaDashboards(diskDevices)` (`src/monitoring/dashboard.go`) es el
registro — nombre de archivo, uid, título y JSON renderizado por panel — y
`WriteGrafanaProvisioningFiles` lo recorre. Añadir un panel es una entrada ahí y
nada más: el aprovisionador (`GrafanaDashboardProviderYAML`) apunta al
**directorio** `dashboard-json`, así que se recoge todo archivo que haya dentro.
Antes de que existiera el registro, el escritor de archivos era la lista de facto,
lo que significaba que un segundo panel solo podía añadirse editando código que no
tiene nada que ver con paneles.

Los uid son constantes (`OverviewDashboardUID`, `DNSDashboardUID`,
`ControllerDashboardUID`) porque la
interfaz web enlaza directamente a ellos. Un uid desviado no produce ningún error
en ninguna parte — Grafana sirve una página de "dashboard not found" dentro del
iframe.

Los paneles de DNS y de controller se **construyen a partir de especificaciones de
panel y se serializan** (`src/monitoring/dashboard_dns.go`,
`src/monitoring/dashboard_controller.go`) en lugar de concatenarse dentro de una
plantilla JSON, como todavía hace el panel de vista general más antiguo. Un
JSON malformado en un panel no cuesta un panel; hace fallar el aprovisionamiento, y
el panel no aparece en absoluto. Los objetivos de los paneles llevan la referencia
de fuente de datos en forma de objeto (`{"type":"prometheus","uid":GrafanaDatasourceUID}`)
— Grafana 13+ no puede resolver la forma heredada de cadena en un objetivo y
renderiza "No data" sin ningún error.

### Ciclo de Vida

Prometheus y Node Exporter siempre se arrancan en el arranque. El ajuste del backend de monitorización determina si además se arranca Grafana o el reenviador socat. Los fallos de arranque no son fatales; el sistema continúa sin monitorización. Systemd se encarga de los reinicios mediante su política `Restart=always`. El método `Stop()` no hace nada porque los servicios del sistema persisten a través de los reinicios del controlador.

### API de Monitorización

- `GET /monitoring/status` (requiere autenticación) -- devuelve `backend` (`"uplot"` o `"grafana"`), una marca de ejecución por servicio (`prometheus`, `node_exporter`, `monitoring_ui`, y `grafana` solo en modo Grafana), y `disk_devices`: los nombres base de dispositivo del kernel que respaldan el sistema de archivos btrfs, que el frontend sustituye en la consulta de E/S de Disco. Un `disk_devices` vacío significa que el descubrimiento falló y el panel recurre a una expresión regular que no coincide con nada. Devuelve `{"status": "disabled"}` cuando la monitorización no está configurada. Los metadatos de imagen y unidad por servicio no están aquí — eso es `GET /system-services`.
- `GET /metrics` (localhost o admin) -- el propio endpoint Prometheus del controlador del sistema. Véase [Métricas del controlador del sistema](#métricas-del-controlador-del-sistema).

### Métricas del controlador del sistema

El controlador exporta su propio estado en el formato de exposición de texto de Prometheus en **su listener ya existente** (`:5309`, `MetricsPath = "/metrics"`), no en un puerto propio. Eso es deliberado: el endpoint viaja entonces sobre el listener que el arnés ya reubica con `TOWN_OS_LISTEN`, así que no hay ningún puerto de host adicional que añadir a `SYSTEM_PORT_FILES` ni forma de que un `make test-full` y un `make dev` colisionen en él — REGLA DE HIERRO.

Es de **localhost o admin**, no público. La recolección agrega recuentos de cuentas, uso de disco y qué servicios están caídos: un mapa de qué atacar y de cuándo el equipo está menos preparado para resistir. Prometheus se ejecuta con `--net host`, así que llega al loopback sin ningún salto por la red de podman, exactamente igual que el objetivo de node-exporter.

`src/metrics` renderiza el formato en unos cientos de líneas en lugar de depender de `prometheus/client_golang`, por la misma razón por la que `errgroup` se quedó fuera. El valor de la biblioteca es su registro, su interfaz de recolectores y su maquinaria de histogramas — nada de lo cual se usa, ya que todos los valores de aquí son o bien un recuento de la vida del proceso o bien una lectura de un gestor por recolección — mientras que su árbol transitivo (`prometheus/common`, `procfs`, protobuf) es real y aterriza en una imagen que arranca desde RAM.

**El escapado de los valores de etiqueta es estructural, no defensivo.** Los valores de etiqueta llevan entrada del operador (un nombre de repositorio, un nombre de paquete, una unidad de systemd). Una comilla sin escapar no corrompe una línea — hace que Prometheus rechace la recolección *entera*, así que un paquete con un nombre raro tumbaría en silencio toda la monitorización.

Lo que se exporta:

| Métrica | Tipo | Notas |
|---|---|---|
| `townos_up` | gauge | siempre 1 mientras sirve; ausente cuando no |
| `townos_start_time_seconds` | gauge | el tiempo en marcha es `time() - esto`, en el reloj de quien recolecta |
| `townos_package_units{state}` | gauge | `active`/`failed`/`inactive`, filtrado a paquetes instalados |
| `townos_system_units{state}` | gauge | `town-os-system--*`, excluyendo unidades NC y de socket |
| `townos_package_unit_active{unit}` | gauge | 1/0 por unidad, para que el operador vea *qué* servicio está caído |
| `townos_system_unit_active{unit}` | gauge | ídem para los servicios del sistema |
| `townos_packages_installed` / `townos_packages_available` | gauge | inventario |
| `townos_repositories` / `townos_repository_errors` | gauge | los errores se cuentan, no se etiquetan por nombre |
| `townos_upgrades_available` | gauge | |
| `townos_accounts{kind}` | gauge | `admin`/`user`/`disabled` |
| `townos_accounts_granted` | gauge | no administradores con al menos una concesión |
| `townos_filesystems{state}` | gauge | `user`/`installed`/`uninstalled` |
| `townos_disk_total_bytes` / `_used_bytes` / `_available_bytes` | gauge | |
| `townos_audit_recent_errors` | gauge | el mismo número que renderiza la píldora roja del panel |
| `townos_audit_events_total{result}` | counter | `success`/`failure`, incrementado por `auditMiddleware` |
| `townos_http_requests_total{method,status}` | counter | el estado es una **clase** (`2xx`…), nunca el código exacto |

Todas ellas salvo `townos_up` y `townos_disk_total_bytes` las grafica el [panel de
Controller](#paneles), cuyo conjunto de paneles se declara contra
`monitoring.ControllerDashboardMetrics()` para que las dos listas no puedan
separarse.

Varias de estas decisiones son la gracia y no algo incidental:

- **Una recolección nunca falla como bloque.** Cada recolector tolera un gestor nil y registra-y-omite ante un error. Un 500 porque un subsistema está enfermo hace desaparecer todas las demás métricas justo en el momento en que se las quiere, así que el equipo se lee como completamente muerto en lugar de parcialmente degradado — y una recolección durante el arranque debería informar de lo que sí está en pie.
- **Los cubos a cero se emiten igualmente.** Un gauge que desaparece en cero es indistinguible de uno que el equipo dejó de informar, así que "ninguna unidad fallida" se vería exactamente igual que "la recolección de unidades está rota".
- **El estado se agrupa por clase.** Cada código distinto se convertiría en una serie permanente, y un plano de control que responde 400/401/403/404/409/422 en decenas de rutas se multiplica rápido para una pregunta que nadie le hace a un equipo doméstico. El código exacto ya está en el log de auditoría y en el log de peticiones.
- **Los contadores están en memoria y son por proceso.** Un contador que sobreviviera a un reinicio describiría la historia del equipo en lugar de la de este proceso, y Prometheus ya entiende un reinicio. También mantiene una recolección — y el middleware de auditoría que la alimenta — completamente fuera de la base de datos.
- **`/metrics` se excluye del registro de auditoría** y de su propio contador de peticiones. Una recolección cada 15 s escribiría si no unas 5.700 filas de auditoría al día describiendo nada que hiciera un operador, y dominaría el contador al que sirve.
- **`metricsMiddleware` se registra el más externo** de los tres (antes de la auditoría y de la lista blanca de concesiones) para que una petición denegada por cualquiera de las dos barreras se cuente igual — un 403 inexplicado es exactamente lo que el contador existe para sacar a la luz. Toma el estado del error devuelto, porque un manejador que devuelve uno todavía no ha escrito su estado.

**El objetivo de recolección no se recompone en ningún sitio.** `MetricsScrapeTarget(listenAddr)` lo deriva de la misma cadena a la que se enlaza el servidor y `main.go` entrega el resultado a `monitoring.Ports.ControllerMetrics` — la misma razón de fuente única de verdad por la que existen `PackageNetworkState.FQDN` y `Manager.MetricsAddr()`. Un enlace comodín (`:5309`, `0.0.0.0:5309`, `[::]:5309`) se reescribe a `localhost` porque un comodín no es una dirección a la que nada pueda conectarse; un host fijado explícitamente se deja en paz, ya que reescribirlo apuntaría la recolección a una dirección en la que el controlador deliberadamente no está. Un resultado vacío omite el trabajo en lugar de apuntarlo a una suposición. Cuando `TOWN_OS_TLS` está activado, `ControllerMetricsScheme` es `https` y el trabajo lleva además `insecure_skip_verify` — la hoja la emite la propia CA del equipo, en la que Prometheus no tiene razón para confiar ni forma limpia de que se la entreguen, y la recolección es por loopback dentro del espacio de nombres del host, así que nada más puede responder por él.

### Interfaz de Monitorización

La pestaña de monitorización de la navegación lateral abre una página de paneles
que lleva **subpestañas System / DNS / Controller**, enlazables directamente como
`?tab=system|dns|controller` como cualquier otra pantalla con subpestañas, para que
un panel que alguien está mirando durante una caída sobreviva a una recarga y pueda
enlazarse. Un valor `?tab=` desconocido recurre a System en lugar de no renderizar
nada. La lista de pestañas es un único array que contiene tanto el componente uPlot
que hay que montar como el uid de Grafana que muestra los mismos paneles, así que
una pestaña no puede existir en un backend y no en el otro — y nombrar ahí el
componente, en lugar de ramificar según el valor de la pestaña, es lo que impide
que una rama olvidada renderice los gráficos de System bajo el encabezado de otra
pestaña.

El renderizado depende del campo `backend` de la respuesta de estado:

- **Modo uPlot**: paneles renderizados directamente en React usando uPlot, consultando Prometheus en el puerto 5308. La rejilla de System se ajusta al viewport (cuatro paneles, dos por fila); las de DNS y Controller **no** — ocho u once paneles apretados en una pantalla dejan a cada uno unos 100 px de lienzo o menos, punto en el que un gráfico de latencia es decoración, así que los paneles tienen una altura fija y la página se desplaza.
- **Modo Grafana**: un iframe de Grafana incrustado apuntando al puerto 5308 en modo kiosco con tema claro. Cambiar de pestaña reapunta el marco al uid del otro panel, y el iframe está indexado por ese uid para que el marco se *sustituya* en lugar de navegar — Grafana mantiene su propio historial, y un cambio de `src` sobre un marco vivo deja el botón Atrás del navegador recorriendo paneles en lugar de salir de la página.

Los títulos de los paneles son idénticos en los dos backends: un operador que
cambia no debería tener que averiguar qué panel se convirtió en cuál. Están
escritos en inglés de forma fija — esta pantalla no lleva ninguna llamada `t()`, y
un título de panel de Grafana no se puede traducir en ningún caso, ya que vive en
el JSON aprovisionado.

Cuando los servicios necesarios no están en marcha, se muestran en su lugar un aviso destacado y un mensaje de marcador de posición.

## Contenedor de la Interfaz

El controlador del sistema gestiona un contenedor de interfaz independiente (`quay.io/town/ui`) como servicio del sistema mediante `ui.Manager`. La etiqueta de la imagen se deriva de la etiqueta de publicación del controlador del sistema (`quay.io/town/ui:<tag>`), anulable mediante la variable de entorno `UI_IMAGE`. Los fallos de arranque no son fatales; el sistema continúa sin el contenedor de la interfaz.

## Disposición de la Interfaz Web

### Panel de Servicios del Panel Principal

La página de inicio del panel muestra un panel de servicios instalados a todo lo ancho, encima de la rejilla de tarjetas de estadísticas. El panel lista todas las unidades de servicio de paquetes obtenidas de `GET /systemd/units`. Cada fila de servicio muestra:

- Un icono de estado: círculo verde con marca para activo, círculo rojo con X para fallido, círculo gris para inactivo.
- El nombre del paquete (extraído del campo `package_identifier`).
- El estado activo como texto.
- La descripción del paquete (si está disponible).
- Las notas compiladas de `POST /packages/installed/info`, renderizadas en línea con enlaces según su tipo (URL, correo, teléfono).

Al hacer clic en una fila de servicio —tanto en el icono de estado como en el nombre del paquete— se navega a `/dashboard/system?search=<package_identifier>`, la fila de ese servicio en la pantalla de servicios. Esa pantalla inicializa su caja de filtro con `?search=` y pasa el término a `GET /systemd/units-tree`, cuya búsqueda compara con los campos propios de cada raíz, de modo que la pantalla se abre sobre ese único paquete con su subárbol de dependencias en lugar de sobre la lista completa. El término es un valor inicial, no un bloqueo: borrarlo o editarlo vuelve a ensanchar la lista. El enlace lleva el `package_identifier` en bruto, nunca el `display_identifier` embellecido: este último no es un término que la búsqueda del árbol pueda encontrar, así que un enlace construido con él aterrizaría en un árbol vacío. El panel se oculta cuando no hay servicios instalados. Las notas se obtienen una vez por servicio y se cachean.

### Disposición

El panel usa una disposición de dos paneles: una barra lateral izquierda fija y un área de contenido a la derecha con una barra de cabecera superior fija.

**Barra lateral** -- un panel vertical de 256 px de ancho (`w-56`) con el logotipo de Town OS y el texto de marca en un banner gris arriba, seguido de botones de navegación apilados verticalmente (cada uno con un icono y una etiqueta). Las rutas activas usan `variant="secondary"` y las inactivas `variant="ghost"`.

**Barra de estado superior** -- una barra horizontal alineada a la derecha que muestra: la píldora de estado de conexión (cargando/desconectado/conectado), el recuento de fallos de servicios del sistema (insignia roja que enlaza a `/dashboard/system?expand=system` cuando `system_services.failed > 0`), el nombre de usuario con el que se ha iniciado sesión con su insignia de administrador y el botón de cerrar sesión.

## Servicios del Sistema

Los servicios del sistema son contenedores de infraestructura gestionados por systemd (distintos de los servicios de paquetes instalados por el usuario). Usan el prefijo de nombre de unidad `town-os-system--`.

El conjunto es: rolodex, el ingress, pages, la interfaz, node-exporter, Prometheus, la interfaz de monitorización (reenviador socat o Grafana) y **una partición gfeh por red** (`town-os-system--gfeh-<red>`). Todo lo de esa lista debe registrarse en `collectSystemServices()` para que `POST /system-services/refresh` lo vuelva a descargar y reiniciar — una omisión ahí es invisible hasta que una actualización deja en silencio el servicio en su imagen antigua.

### Actualizaciones automáticas

**El instalador entrega dos imágenes y el controlador consigue el resto.** `install.sh` escribe exactamente dos referencias de imagen en las unidades que despliega — `quay.io/town/town` (el systemcontroller) y `quay.io/town/rolodex` — y no hornea ningún contenido de imagen en el squashfs. Todas las demás imágenes de servicio de sistema (interfaz, ingress, controlador de red, almacenamiento de objetos) las descarga **en el propio equipo** el controlador: en el arranque mediante `parallelEnsureImages` sobre `coreBootImages`, y bajo demanda mediante `POST /system-services/refresh`. Por eso esos repositorios deben ser legibles de forma anónima: el equipo no lleva credenciales de registro, y nada en el controlador escribe jamás un `auth.json` ni ejecuta `podman login`. Una imagen cuyo repositorio es privado es una imagen que este diseño no puede obtener, y el fallo se manifiesta como una unidad en bucle de caídas con `unauthorized` en lugar de algo que nombre la causa real.

**Un temporizador diario ejecuta el mismo procedimiento de actualización que ejecuta el botón de la interfaz, y viene con el instalador.** `town-os-update.timer` y su servicio viven en `../install/systemd/` y se habilitan al construir la imagen (la lista `ENABLE_UNITS` de `make/install.sh`), así que un equipo los tiene desde el primer arranque en lugar de conseguirlos de un controlador que ya tiene que estar en marcha. El temporizador se dispara a las `04:23` y hace POST a `/system-services/refresh` — deliberadamente el mismo endpoint en lugar de una segunda implementación de «descargarlo todo», para que la ruta programada no pueda divergir de la manual. La unidad decide *cuándo*; el controlador decide *qué*. Es `Persistent=true`, así que un equipo apagado a las 04:23 se actualiza en el siguiente arranque en vez de esperar otro día — en un equipo que está apagado más de lo que está encendido, ese arrastre es la única actualización que recibirá. El horario evita deliberadamente el `03:17` del podman prune: una limpieza recogiendo imágenes no referenciadas mientras una actualización descarga otras nuevas es una carrera sin nada que ganar.

**El POST del temporizador va sin credenciales, y por eso la ruta es `localhostOrAdmin`.** La unidad no tiene ningún token que presentar, así que `/system-services/refresh` admite llamadas sin autenticar **solo desde loopback** y sigue exigiendo admin desde cualquier otro sitio — la misma exención en la que se apoya `GET /metrics`, sólida por la misma razón: un paquete con origen en `127.0.0.0/8` no puede enrutarse hasta el equipo desde la red. La unidad generada apunta por tanto a `127.0.0.1` explícitamente y nunca a la dirección enrutable del equipo, que tanto enviaría un POST sin credenciales por la LAN como sería rechazado al otro extremo. El puerto sigue a la dirección `-listen` del controlador, de modo que un controlador reubicado (el arnés de integración da a cada ejecución su propio puerto) sigue actualizándose.

**`auto_update_enabled` limita al temporizador, no al operador.** El temporizador marca su propia llamada con `?scheduled=1`; solo una llamada que lleve esa marca consulta el ajuste. Un administrador que pulsa el botón de actualizar siempre actualiza, porque un interruptor rotulado «actualizar automáticamente» no tiene nada que decir sobre una petición explícita. Una ejecución programada omitida responde `200` con `{"status":"skipped"}` en lugar de un error — el temporizador preguntó si debía actualizar y obtuvo la respuesta válida «ahora no», que no es un fallo que `systemctl status` deba mostrar en rojo. El temporizador permanece instalado y en marcha mientras el ajuste está desactivado, así que cambiarlo surte efecto en el siguiente disparo sin cirugía de unidades y sin modo de que el estado de la unidad y el ajuste discrepen.

El ajuste viene **activado** por defecto, y los valores no reconocidos se leen como activado. Desactivado es una lista cerrada (`0`, `false`, `off`, `no`, sin distinguir mayúsculas); todo lo demás — incluida una errata y una fila de ajustes ilegible — deja las actualizaciones en marcha. La asimetría es deliberada: como el instalador entrega solo dos imágenes, un equipo que deja de descargar es un equipo que nunca consigue la mayoría de sus servicios, así que el coste de equivocarse hacia «desactivado» es mucho mayor que el de una descarga de más.

### Generación de Unidades de Servicios del Sistema

`GenerateSystemServiceUnit` produce unidades de systemd basadas en podman con `Restart=always`. La configuración de la unidad admite un campo `VolumeDirs` que enumera los directorios del host que hay que precrear mediante líneas `ExecStartPre=/bin/mkdir -p <dir>`, evitando fallos de montaje cuando los contenedores arrancan tras un reinicio antes de que se haya ejecutado el controlador del sistema.

### API de Servicios del Sistema

- `GET /system-services` (localhost o autenticación) -- lista los servicios del sistema con el estado vivo de la unidad. Cada entrada incluye la clave, el nombre para mostrar, la imagen, el puerto y los campos de estado de la unidad de systemd. Devuelve una lista vacía cuando la monitorización no está configurada. Excluido del registro de auditoría.
- `POST /system-services/status` (requiere admin) -- cambia el estado de un servicio del sistema. Acepta la clave y la acción (`start`, `stop`, `restart`). Las acciones `enable` y `disable` se rechazan.
- `POST /system-services/refresh` (requiere admin) -- refresca el estado de los servicios del sistema.

## Imagen de Producción de la Interfaz Web

Una imagen de contenedor de interfaz independiente (`quay.io/town/ui`) se construye desde `Containerfile.ui`. Usa una compilación en dos etapas: `oven/bun:latest` construye los archivos estáticos de la interfaz, y después `docker.io/library/caddy:latest` los sirve en el puerto 80 con enrutado SPA (`try_files {path} /index.html`). A la interfaz se llega a través del ingress compartido en lugar de ocupar directamente el `:80` del host — es el backend predeterminado de `:80` del ingress para cualquier host que no coincida con una ruta, así que el inicio de sesión por IP desnuda sigue funcionando.

**Las cabeceras de caché son estructurales** (`Caddyfile.ui`). Todo lo que hay bajo `/assets/*` lleva huella digital puesta por Vite, así que la URL de un recurso nombra una compilación exacta para siempre y se sirve con `public, max-age=31536000, immutable`. `index.html` es el único archivo al que Vite **no** le pone huella, y es el que nombra el paquete actual; servido sin ningún `Cache-Control`, un navegador puede aplicar frescura heurística (RFC 9111 §4.2.2) y reutilizar su copia cacheada sin revalidar, así que un equipo actualizado sigue repartiendo el `index.html` de la versión anterior, que apunta al paquete de la versión anterior. El síntoma es una actualización que parece no haber ocurrido — las funciones nuevas se renderizan como si la interfaz nunca hubiera oído hablar de ellas. Toda ruta que no sea de recursos es una ruta SPA que `try_files` resuelve a `index.html`, así que la regla `no-cache` está escrita para cubrirlas todas (`@html not path /assets/*`).

`make release-ui` compila con `--no-cache` para que un `push-rc` siempre distribuya recursos de interfaz recién construidos en lugar de un paquete cacheado por capas.

**Las pruebas nunca descargan la imagen de interfaz de quay** — el objetivo de make `ui-image` construye `Containerfile.ui` localmente como `localhost/town-os-ui:<INSTANCE_ID>` (siempre coincidiendo con la arquitectura del host y con el código de interfaz del repositorio), lo guarda en la caché de imágenes, y el arnés de pruebas lo carga en los contenedores de prueba y lo inyecta mediante la variable de entorno `UI_IMAGE`. `test-integration-build` y `test-ui-integration` dependen de `ui-image`. Las etiquetas de quay.io/town/ui son solo para las subidas de producción/publicación. `uiTestImage` en `integration/systemcontroller_ui_test.go` se salta su prueba cuando `UI_IMAGE` no está establecida, en lugar de recurrir a una etiqueta de quay.

## Imagen del Runner de Proton

La imagen del runner de Proton (`quay.io/town/proton`) se construye desde `Containerfile.proton`. Usa una compilación en dos etapas: una etapa de descarga obtiene el tarball de la publicación de GE-Proton (fijado mediante el argumento de compilación `GE_PROTON_VERSION`), y la etapa de ejecución instala las dependencias de Wine/Proton (de 64 y de 32 bits), Xvfb para operar sin pantalla, y un script envoltorio en `/usr/local/bin/proton` que arranca un framebuffer virtual y configura el entorno de Proton antes de ejecutar la aplicación.

La cadena de make proporciona: `release-proton-image` (construir), `push-proton-rc` (subir etiquetas de candidata a publicación por arquitectura `rc.<fecha>-<arch>` + `rc.latest-<arch>`) y `push-proton-release` (subir etiquetas de publicación por arquitectura `release.<fecha>-<arch>` + `latest-<arch>`). La imagen de proton también se incluye en los flujos completos `push-rc` / `push-release` y en el ensamblado `manifest-rc` / `manifest-release` cuando `PROTON_ENABLED=1`.

## Cliente de la API de la Interfaz Web

El navegador determina la URL base de la API en tiempo de ejecución a partir de `window.location`, usando el protocolo y el nombre de host actuales con el puerto 5309 (p. ej., `https://myhost:5309`). No hay ningún proxy del lado del servidor; el navegador habla directamente con la API del controlador del sistema.

La variable de entorno `VITE_API_URL` anula la URL derivada del navegador cuando está establecida. Es útil durante el desarrollo, cuando el servidor de la API se ejecuta en otro host o puerto.

El panel de monitorización deriva la URL de su puerto de monitorización (el 5308) del nombre de host actual. Cuando `VITE_API_URL` está establecida, el nombre de host se extrae de ella; en caso contrario se usa `window.location.hostname`.

## Accesibilidad de la Interfaz Web

Todos los componentes de diálogo incluyen un elemento `DialogDescription` que proporciona una descripción concisa del propósito del diálogo. Esto satisface el requisito de accesibilidad de Radix UI para lectores de pantalla y elimina las advertencias de `aria-describedby`. Las descripciones se colocan dentro de la cabecera del diálogo, tras el título, y son visibles para todos los usuarios.

## Internacionalización

Todas las cadenas de cara al usuario (etiquetas de la interfaz, mensajes de error, notificaciones emergentes, descripciones de acciones del log de auditoría) son traducibles mediante un patrón de catálogo de mensajes.

### Backend

El paquete `i18n` proporciona una función `T(locale, key, args...)` que resuelve claves de traducción. La cadena de respaldo es: la configuración regional solicitada, después `en-US`, después la cadena cruda de la clave. Cuando se proporcionan `args`, se aplica el formateo de `fmt.Sprintf`. Las claves de mensaje usan espacios de nombres separados por puntos (p. ej., `auth.login_failed`, `pages.toast_provisioned`).

### Catálogos Poblados

Los catálogos del backend viven en un archivo por configuración regional en `src/i18n` (`de_de.go`, `zh_cn.go`, …); el espejo del frontend vive en `ui/src/i18n` (`de-DE.js`, `zh-CN.js`, …). Los dos lados se mantienen sincronizados — todo catálogo poblado del backend tiene su gemelo en el frontend.

`PopulatedLocales()` es la lista autoritativa (48 entradas): `en-US`, `ar-AE`, `ar-EG`, `ar-SA`, `bn-BD`, `bn-IN`, `cs-CZ`, `da-DK`, `de-AT`, `de-CH`, `de-DE`, `en-AU`, `en-CA`, `en-GB`, `en-IN`, `en-NZ`, `en-ZA`, `es-AR`, `es-ES`, `es-MX`, `fi-FI`, `fr-BE`, `fr-CA`, `fr-CH`, `fr-FR`, `hi-IN`, `hr-HR`, `hu-HU`, `it-IT`, `ja-JP`, `ko-KR`, `nl-BE`, `nl-NL`, `pl-PL`, `pt-BR`, `pt-PT`, `ro-RO`, `ru-RU`, `sa-IN`, `sk-SK`, `sl-SI`, `sv-SE`, `th-TH`, `tr-TR`, `uk-UA`, `vi-VN`, `zh-CN`, `zh-TW`. Todo lo que no esté en ella recurre al inglés. `IsPopulated(code)` es lo que usa la interfaz para deshabilitar una entrada no poblada en el selector de idioma.

La lista se **deriva del mapa de catálogos en lugar de escribirse a mano**: `buildPopulatedLocales()` lee las claves de `catalogs` en el init, las ordena y fija `en-US` al principio, e `IsPopulated` indexa `catalogs` directamente. Antes era un literal de slice mantenido a mano, que tenía exactamente un modo de fallo y era silencioso — un catálogo registrado en `catalogs` pero olvidado en el literal estaba traducido, se distribuía y nunca se ofrecía en el selector. `PopulatedLocales()` devuelve un clon, porque la lista ahora es estado del paquete en lugar de un literal nuevo por llamada, y quien ordene o trunque el resultado no debe poder perturbar la siguiente llamada.

### Variantes de País

Un catálogo es de uno de dos tipos, y la diferencia está en cómo se escribe el archivo, no en cómo se selecciona — los dos tipos están poblados y los dos aparecen en el selector.

Un **catálogo de idioma** es una traducción, escrita por completo: `de_de.go`, `cs_cz.go`, `ja_jp.go`.

Un **catálogo de país** lo construye `derive(base, overrides)` (`src/i18n/derive.go`, con espejo en `ui/src/i18n/derive.js`) a partir del catálogo del idioma al que pertenece más únicamente las cadenas que ese país expresa de otra manera. El alemán de Austria es alemán; la pregunta que responde `de_at.go` no es "cómo se dice esto en alemán", sino "cuál de estas frases no habría escrito una persona austriaca". Copiar `de-DE` dentro de `de_at.go` y editar cuatro líneas significaría que la siguiente clave de mensaje añadida a `de-DE` llegaría en silencio a Austria en inglés, y que una corrección en una cadena alemana habría que encontrarla y repetirla en tres archivos. Heredar la base y enumerar solo las divergencias mantiene una variante correcta por defecto: una clave nueva aterriza en todas partes en cuanto la tiene su idioma base.

Dieciocho configuraciones regionales se derivan así:

| Base | Derivadas de ella |
| --- | --- |
| `en-US` | `en-CA`, `en-GB` |
| `en-GB` | `en-AU`, `en-IN`, `en-NZ`, `en-ZA` |
| `de-DE` | `de-AT`, `de-CH` |
| `fr-FR` | `fr-BE`, `fr-CA`, `fr-CH` |
| `es-ES` → `es-latam` | `es-AR`, `es-MX` |
| `pt-BR` | `pt-PT` |
| `nl-NL` | `nl-BE` |
| `ar-SA` | `ar-AE`, `ar-EG` |
| `bn-BD` | `bn-IN` |

`es-latam` (`src/i18n/es_latam.go`, `ui/src/i18n/es-latam.js`) es el único intermedio: contiene las divergencias respecto del español peninsular que comparten todas las variedades americanas — `inválido` en lugar de `no válido`, `agregar` en lugar de `añadir`, comillas rectas en lugar de `« »` — y tanto `es-AR` como `es-MX` se construyen sobre él. **No está registrado en `catalogs` y no es seleccionable**, porque es un fragmento compartido y no un sitio donde viva nadie; anunciarlo ofrecería un código de país que no lo es.

Algunos mapas de anulaciones son pequeños y varios (`en-CA`, `de-CH` en el backend, `es-MX`) están vacíos. Esa es la respuesta honesta para un panel de control técnico — el inglés de Canadá conserva las grafías estadounidenses en `-ize`, y ningún mensaje de `de_de.go` contiene una `ß` a la que pueda llegar la regla suiza del `ss` (el `de-CH.js` del frontend sí lleva anulaciones reales, porque `de-DE.js` usa `ß`). Un mapa de anulaciones vacío sigue marcando la configuración regional como revisada deliberadamente y no como olvidada.

El esquema lo sostienen pruebas en ambos lados (`src/i18n/derive_test.go`, `ui/src/i18n/derive.test.js`): toda clave de anulación debe existir en su base, toda anulación debe diferir realmente de la cadena base que sustituye, todo catálogo derivado debe llevar el conjunto completo de claves de su base, y todo catálogo derivado debe estar listado en la tabla `variants()` de la prueba — de modo que un catálogo de país no puede distribuirse sin que esas reglas se le apliquen.

**Todos los códigos de configuración regional llevan una subetiqueta de región**, y `TestLocaleCodesAreRegionQualified` lo mantiene. El sumerio (`sux`) era la única excepción — un código ISO 639-3 desnudo — y ya no está. Se eliminó por su escritura, no por su forma: el cuneiforme vive en `U+12000`–`U+1254F`, para lo cual casi nada distribuye una fuente, así que en cualquier equipo sin Noto Sans Cuneiform todas las cadenas de esa configuración regional se pintaban como cajitas de sustitución. La romanización que el catálogo llevaba entre paréntesis sobrevivía, lo cual lo hacía peor que estar en blanco — fragmentos latinos y puntuación alrededor de agujeros. Renderizarlo con honestidad significaba empaquetar una fuente web (el catálogo usaba 45 puntos de código distintos, pero la tipografía completa ocupa 462 K y hacer un subconjunto quiere `fonttools` en el host de compilación) y añadir maquinaria de `@font-face` que la interfaz no tiene, lo cual es mucho aparato para un idioma sin hablantes.

### Listas de Configuraciones Regionales

Se usan códigos BCP 47 en todo el sistema. Se proporcionan dos listas curadas:

- **CommonLanguages** (21 entradas) -- árabe (ar-SA), bengalí (bn-BD), alemán (de-DE), inglés (en-US), español (es-ES), francés (fr-FR), hindi (hi-IN), italiano (it-IT), japonés (ja-JP), coreano (ko-KR), neerlandés (nl-NL), polaco (pl-PL), portugués (pt-BR), ruso (ru-RU), sánscrito (sa-IN), sueco (sv-SE), tailandés (th-TH), turco (tr-TR), ucraniano (uk-UA), vietnamita (vi-VN), chino (zh-CN). Cada entrada incluye el nombre en escritura nativa y el nombre en inglés.
- **ExtendedLocales** (89 entradas) -- lista exhaustiva de variantes regionales específicas por país (p. ej., de-AT, en-GB, es-MX, fr-CA, pt-PT, zh-TW).

### Frontend

Un proveedor de contexto de React (`I18nProvider`) envuelve la aplicación y expone un hook `useI18n()` que devuelve `{ locale, setLocale, syncServerLocale, t }`. La función `t` resuelve claves contra el catálogo del frontend con la misma cadena de respaldo que el backend. La interpolación de parámetros usa marcadores `{name}` (p. ej., `t('greeting', { name: 'Alice' })`).

Junto a ella se exporta `translateIn(locale, key, params)`, que traduce en una configuración regional nombrada en vez de en la activa, con la misma cadena de respaldo. Existe por el mensaje que confirma un cambio de idioma: `t` captura la configuración regional del renderizado desde el que se la llamó, así que una confirmación lanzada desde el formulario de idioma quedaría escrita en el idioma que se está *dejando* — el único mensaje de la página cuyo asunto es precisamente que ese idioma ya no está en uso.

### Detección, Almacenamiento y Sincronización de la Configuración Regional

La interfaz elige su idioma **primero desde el navegador**, no desde el ajuste global. Al cargar lee `navigator.languages` y compara las preferencias ordenadas contra los catálogos distribuidos. La comparación no distingue mayúsculas e intenta las etiquetas exactas de todas las preferencias antes de recurrir, en este orden, a:

1. **Coincidencia exacta.** `de-CH` ya distribuye catálogo, así que `de-CH` se resuelve a `de-CH` en lugar de plegarse a `de-DE`.
2. **Chino por escritura/región.** `zh-Hant` o una región `TW`/`HK`/`MO` → `zh-TW`, en caso contrario `zh-CN`. La escritura es una señal más fuerte que cualquier valor predeterminado, así que esto se ejecuta antes que las dos reglas siguientes.
3. **Un predeterminado regional con nombre.** Países que no distribuyen catálogo pero que leen una variante en lugar del predeterminado de su idioma: la América Latina hispanohablante → `es-MX`, el África lusófona y Timor → `pt-PT`, y los ingleses de Irlanda, África y el sur y el sudeste de Asia → `en-GB`. Sin esto, `es-CO` obtendría el español peninsular y `en-IE` obtendría el estadounidense.
4. **Un predeterminado de idioma con nombre.** `ar` → `ar-SA`, `bn` → `bn-BD`, `de` → `de-DE`, `en` → `en-US`, `es` → `es-ES`, `fr` → `fr-FR`, `nl` → `nl-NL`, `pt` → `pt-BR`.
5. **Cualquier catálogo que comparta la subetiqueta primaria.**

Los pasos 3 y 4 existen porque el respaldo era antes solo el paso 5, y eso solo era correcto mientras cada idioma tenía exactamente un catálogo. Ocho idiomas distribuyen ahora más de uno: un navegador que pidiera un `en` a secas, o un `en-PH`, aterrizaría si no en el inglés que estuviera declarado primero en el objeto `catalogs`, con lo que la respuesta sería una propiedad del orden de importación en lugar de una decisión que alguien tomó.

Precedencia, de mayor a menor:

1. una elección explícita, persistida **por navegador** en `localStorage` — *fijada*
2. un idioma detectado del navegador emparejado con un catálogo distribuido — *fijada*
3. el ajuste global `locale` del servidor, aplicado después mediante `syncServerLocale` — *no fijada*

Una vez que la configuración regional está fijada, `syncServerLocale` no hace nada. Esa es la gracia de la separación: el ping de estado de 60 segundos solía llamar a `setLocale` y por tanto imponía el ajuste global `locale` del administrador a todos los navegadores en cada sondeo. El ajuste `locale` (global, `en-US` por defecto, todavía informado en la respuesta del ping) ahora es solo el respaldo para un idioma del que Town OS no distribuye catálogo.

### API de Configuraciones Regionales

- `GET /locales` (requiere autenticación) -- devuelve la configuración regional actual, la lista de las pobladas, los idiomas comunes y las configuraciones regionales extendidas. Excluido del registro de auditoría.

### Interfaz de Ajustes

La página de ajustes del sistema incluye un selector de idioma. Los idiomas comunes se muestran en un desplegable con sus nombres en escritura nativa. Una sección expandible revela la lista de configuraciones regionales extendidas. Las no pobladas (las que no tienen catálogo de traducción) se muestran con un asterisco al final y están deshabilitadas en el selector, impidiendo su selección.

El selector se abre en **la configuración regional en la que la página está renderizada** — la que mantiene `useI18n()` — y no en el valor `current` de `GET /locales`. Ambas discrepan en el caso corriente, porque el navegador elige la configuración regional y la fija mientras el ajuste global `locale` se queda en su valor por defecto `en-US` (véase [Detección, Almacenamiento y Sincronización de la Configuración Regional](#detección-almacenamiento-y-sincronización-de-la-configuración-regional)); preseleccionar `current` hacía que el control dijera «English» en una página que no estaba en inglés. Cuando la configuración regional activa es una variante de país, vive en la lista extendida, que está plegada, así que la lista se despliega al cargar en vez de dejar el desplegable con un valor que ninguna de sus opciones visibles lleva; si se vuelve a plegar, esa entrada se sigue mostrando, por la misma razón. `current` solo se usa como respaldo, para una configuración regional activa que el servidor no ofrece.

Al guardar, la elección se compara con **ambas**. Coincidir con solo una sigue siendo trabajo: igual que el servidor pero distinta de la página significa cambiar la página (`setLocale`, que fija la elección para este navegador) sin escribir el ajuste; igual que la página pero distinta del servidor significa escribir el ajuste. Solo cuando la elección coincide con las dos no hay nada que hacer. El aviso de éxito se escribe con `translateIn` en el idioma recién elegido, porque la interfaz que hay detrás ya ha cambiado; el aviso de «no hay nada que hacer» se queda en el idioma que está en pantalla, porque no ha cambiado nada. Comparar únicamente con `current` hacía que el idioma mostrado fuera imposible de elegir: pulsar Guardar sobre él informaba de que «no hay nada que hacer», así que volver al inglés exigía guardar antes un tercer idioma.

## Configuración del Controlador del Sistema

### Secuencia de Arranque

El orden de arranque paso a paso autoritativo vive en [Secuencia de Arranque del Controlador del Sistema](#secuencia-de-arranque-del-controlador-del-sistema). En resumen:

1. `setupPodmanEnv()` apunta `CONTAINER_HOST` al socket de podman del host.
2. Análisis de flags, y después `:5309` se enlaza de inmediato con el stub de estado de arranque.
3. Creación de directorios, limpieza de la base de datos obsoleta de la raíz, base de datos, y los gestores de cuentas (más la purga de la cuenta de servicio heredada), sesiones, auditoría, ajustes, pages y red — este último siembra la red del hogar.
4. Siembra de repositorios, refresco forzado de la raíz de repositorios.
5. Gestor de instalación, almacenamiento btrfs, gestor de systemd; resolución de la etiqueta de imagen.
6. Escritura de la configuración de rolodex + espera de disponibilidad (el propio rolodex lo supervisa systemd).
7. Descargas de las imágenes centrales (NC, monitorización, interfaz) y arranque de los servicios de sistema de monitorización.
8. CA TLS local, ingress y servicio de pages.
9. Reconciliación del almacenamiento de objetos (una partición gfeh por red).
10. Detección de cambio de versión, reconciliación, comandos posteriores a la actualización.
11. Reconstrucción del DNS, reconciliación de redes, una segunda reconciliación (idempotente) del almacenamiento de objetos, programación del ingress, arranque del contenedor de la interfaz.
12. Etapa de frescura (reinicios por paquete tras un refresco).
13. Construcción del manejador y el intercambio atómico del stub de arranque por el router completo.
14. Publicación en segundo plano de los nombres del almacenamiento de objetos, en cuanto una partición responde.

Los fallos de arranque de la monitorización, la configuración de Rolodex, las descargas de imágenes centrales, la CA TLS, el ingress, el servicio de pages, el almacenamiento de objetos, la reconciliación de redes y el contenedor de la interfaz no son fatales; el sistema continúa sin ellos. Todas las descargas de imágenes de contenedor usan el ayudante `ensureImage`, que comprueba `podman image exists` antes de descargar, evitando descargas redundantes en entornos de prueba/desarrollo donde las imágenes están precargadas. Los fallos de descarga de servicios no esenciales se registran en stderr y no impiden el arranque, permitiendo que el sistema arranque incluso cuando la red no está disponible temporalmente.

### Detección de la etiqueta de versión

El controlador del sistema deriva etiquetas de imagen coincidentes para todos los servicios hermanos (interfaz, Rolodex, controlador de red, ingress) a partir de una única etiqueta resuelta por `resolveImageTag()`: la variable de entorno `TOWN_OS_TAG` si está establecida, y si no `rc.latest-<arch>` (`defaultVersionTag()`, con la arquitectura de `runtime.GOARCH` mapeada a `x86_64`/`aarch64` mediante `archTag()`). No hay ninguna versión `Version` fijada en tiempo de compilación ni ningún archivo `/town-os.tag` — ambos se eliminaron porque un valor obsoleto en cualquiera de los dos retenía en silencio todas las imágenes hermanas en una etiqueta antigua incluso después de que el controlador avanzara. El sistema de compilación de la instalación fija una etiqueta concreta estableciendo `TOWN_OS_TAG` en la unidad de systemd del systemcontroller (`../install/make/install.sh` la deriva de `CONTROLLER_IMAGE`); sin ninguna anulación, la flota siempre sigue `rc.latest-<arch>`. Esta etiqueta construye referencias de imagen como `quay.io/town/ui:<tag>` y `quay.io/town/rolodex:<tag>`; las etiquetas que se suben son por arquitectura, así que toda etiqueta hermana derivada lleva el sufijo de arquitectura.

### Formato de Errores

Todos los errores de la API se devuelven como objetos Problem Detail de la RFC 9457 (JSON estructurado con campos de tipo, título, estado y detalle). Un `ProblemDetailHTTPErrorHandler` personalizado se establece como manejador de errores de Echo.

### Registro de Peticiones

El middleware `RequestLogger()` de Echo está habilitado globalmente, registrando todas las peticiones HTTP en stderr. La verbosidad se controla con la variable de entorno `LOG_LEVEL`.

### Limitación de Inicios de Sesión

`POST /account/authenticate` es público y cada intento cuesta un hash argon2id de 64 MiB. Ese es el coste correcto para un hash de contraseña y lo incorrecto para dejar que quien llama sin autenticar lo programe sin límite: unos pocos cientos de intentos simultáneos son decenas de gigabytes de reserva de memoria en un equipo cuyo diseño entero se basa en ejecutarse desde RAM, y el fallo no es un inicio de sesión lento — es el asesino de OOM llevándose el controlador.

Dos límites independientes, porque responden a preguntas distintas. `loginLimiter` limita los **intentos por origen** en una ventana (20 cada 5 minutos), que es lo que hace inviable adivinar contraseñas en línea, y está indexado por dirección de origen para que un cliente abusivo no pueda dejar fuera a toda la casa. `loginGate` limita los **hashes concurrentes** de todos los orígenes (4, acotando la memoria pico de argon2 cerca de un cuarto de gigabyte), que es lo que el limitador por origen no puede hacer por sí solo. Ambos están en memoria y son por proceso: protegen la memoria y la CPU de este proceso, y persistirlos convertiría un inicio de sesión fallido en una escritura en la base de datos.

Ambos se comprueban **antes** de calcular el hash, no después — el coste del que se defiende es el propio hash, así que un rechazo que aun así lo calculara habría pagado por el ataque que estaba rechazando. La ranura de la barrera se libera mediante un `defer` dentro de un cierre en lugar de después de la llamada, porque una ranura filtrada por un pánico se perdería durante toda la vida del proceso y cuatro de ellas atascarían todos los inicios de sesión del equipo hasta un reinicio. Una contraseña demostrada correcta limpia la ventana de ese origen, así que una casa detrás de una sola dirección NAT no puede caer en un bloqueo por uso ordinario.

### CORS

En modo `DEBUG` se permiten todos los orígenes. Si no, se permiten las peticiones entre puertos del mismo nombre de host (p. ej., un navegador en el puerto 80 hablando con la API en el 5309), **pero solo una vez que la cabecera Host se ha comprobado contra los nombres por los que este equipo puede legítimamente ser llamado**. Métodos permitidos: GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS. Se permiten las credenciales con una edad máxima de 3600 segundos.

La comprobación importa porque la regla antigua — "el nombre de host del Origin es igual al nombre de host de la cabecera Host" — comparaba dos valores que vienen ambos de la misma URL elegida por el atacante. Apunta `box.evil.example` a la dirección de LAN del equipo y un navegador envía `Origin: http://box.evil.example` y `Host: box.evil.example:5309`, que coinciden. Esa es la forma del DNS rebinding, y con `AllowCredentials` le entregaba a una página al paso la ventana de arranque inicial (`POST /account/create` responde sin autenticación mientras no exista ningún administrador habilitado).

Por eso `originAllowed` exige que la cabecera Host nombre a este equipo: su propio nombre de host, `<hostname>.local`, `<hostname>.<dns_tld>`, las direcciones de loopback y de LAN en las que responde, o lo que el operador haya configurado en `AllowedHosts`. Esas formas están **enumeradas, no comparadas por sufijo** — una regla como "cualquier nombre cuya primera etiqueta sea el nombre de host" aceptaría `townos.evil.example`, que un atacante puede sencillamente registrar. Un literal de IP se acepta por sí solo: una dirección no puede tener alias por DNS, así que `http://192.168.1.10/` llegando a `http://192.168.1.10:5309` es el mismo equipo por construcción, que es la forma habitual en que esto se usa de verdad.

**El acceso a redes privadas (PNA) solo se responde para un origen que CORS aceptaría.** La cabecera `Access-Control-Allow-Private-Network` se devolvía antes incondicionalmente, lo cual entrega a todos los orígenes de internet el permiso del navegador para alcanzar una dirección privada — la única protección que PNA existe para añadir por encima de CORS. Su middleware se registra **antes** que el middleware de CORS para que aún se ejecute en una comprobación previa (preflight), que CORS responde por su cuenta sin llamar más abajo en la cadena.

### Apagado Ordenado

SIGINT dispara la cancelación del contexto. El servidor HTTP se apaga y todas las goroutines de fondo salen mediante los canales del contexto. Rolodex lo supervisa systemd y el systemcontroller no lo detiene.

### Flags de CLI

- `-db <ruta>` -- ruta a la base de datos SQLite (por defecto, un archivo temporal efímero).
- `-btrfs <ruta>` -- ruta base para las operaciones con subvolúmenes btrfs.
- `-repo-dir <ruta>` -- directorio base para los repositorios git (por defecto, un directorio temporal efímero).
- `-network-state <ruta>` -- directorio para los archivos de estado de red por paquete (por defecto `/run/town-os`, `DefaultNetworkStatePath`; debe ser una ruta que compartan el contenedor del systemcontroller y el host — nunca `/var/run/...` ni `/tmp`).
- `-listen <dirección>` -- dirección de escucha HTTP (por defecto `:5309`).

La imagen del controlador de red tampoco es un flag; se deriva de la etiqueta de imagen resuelta y es anulable con `NC_IMAGE`.

### Variables de Entorno

- `CONTAINER_HOST` -- URL del socket unix del demonio podman del host. Se establece automáticamente al arrancar a `unix:///run/podman/podman.sock` (véase `HostPodmanSocket`). Toda invocación de `podman` — incluidos los procesos hijos que bifurca el systemcontroller — la hereda del entorno del proceso y se encamina por el socket del host en lugar del almacenamiento aislado de podman del contenedor del systemcontroller. La unidad de systemd del repositorio de instalación debería establecer además `Environment=CONTAINER_HOST=...` para que se vea en la salida de `systemctl`, pero la llamada a `setupPodmanEnv()` es la fuente de verdad en tiempo de ejecución.
- `TOWN_OS_LISTEN` -- anula el flag `-listen`.
- `TOWN_OS_SIGNING_KEY` -- anula la clave efímera de firma JWT (véase Gestión de Sesiones).
- `TOWN_OS_TLS` -- sirve el propio listener del plano de control (`:5309`) por HTTPS, terminado por la CA local del equipo con una hoja emitida exactamente igual que la de un paquete. **Desactivado por defecto, y eso es una cuestión de secuencia y no una reserva**: un navegador al que no se le ha dado la CA del equipo no puede completar una XHR contra un certificado no confiable y, a diferencia de una navegación, no hay ninguna pantalla intermedia que aceptar — la interfaz sencillamente dejaría de funcionar sin ninguna forma de llegar a la pantalla que lo explica. La interfaz además se sirve hoy por HTTP plano (es el backend predeterminado de `:80` del ingress), así que un equipo que activara esto sin instalar antes la CA pasaría de "sin cifrar" a "caído". El operador instala la CA (`GET /tls/ca.crt`, público) y luego establece esto. Acepta `1`/`true`/`yes`/`on`. Se resuelve **antes** de enlazar el listener, así que un flujo de estado de arranque que empieza como HTTP nunca se convierte en HTTPS por debajo de su cliente, y es **fatal** si falla en lugar de recurrir a texto claro: un operador que pidió TLS y obtuvo en silencio texto plano está peor que uno cuyo equipo se niega a arrancar y dice por qué.
- `TOWN_OS_TLS_CERT` / `TOWN_OS_TLS_KEY` -- un certificado y una clave suministrados por el operador, para un equipo tras un nombre que ya tiene un certificado de confianza pública. Establecer **ambos** habilita TLS por sí solo y la CA local no se consulta; establecer solo uno no hace nada.
- `TOWN_OS_TLS_SANS` -- nombres o IP adicionales separados por comas para la hoja generada, para un equipo al que se llega por un nombre que el controlador no puede derivar (un CNAME, un nombre DHCP asignado por el router).
- `TOWN_OS_TEST` -- si está establecida, usa repositorios de prueba en lugar de los predeterminados de producción.
- `DEBUG` -- si está establecida, permite todos los orígenes CORS y antepone los repositorios de prueba a los predeterminados.
- `LOG_LEVEL` -- nivel de registro: `debug`, `info`, `warn`, `error` (`error` por defecto).
- `TOWN_OS_REPO_USERNAME` / `TOWN_OS_REPO_PASSWORD` -- credenciales de repositorio aplicadas a todos los repositorios en la primera inicialización.
- `TOWN_OS_TAG` -- fija la etiqueta de imagen de la que se deriva toda imagen hermana (véase [Detección de la etiqueta de versión](#detección-de-la-etiqueta-de-versión)). La establece el sistema de compilación de la instalación en la unidad de systemd del systemcontroller.
- `ROLODEX_IMAGE` -- anula la imagen de contenedor de Rolodex (por defecto `quay.io/town/rolodex:<tag>`).
- `UI_IMAGE` -- anula la imagen de contenedor de la interfaz (por defecto `quay.io/town/ui:<tag>`). Establecerla a la **cadena vacía** (presente explícitamente pero vacía) se salta el contenedor de la interfaz por completo — modo de desarrollo, donde bun sirve la interfaz.
- `NC_IMAGE` -- anula la imagen del controlador de red (por defecto `quay.io/town/networkcontroller:<tag>`). La usa el arnés de integración para inyectar un NC construido localmente.
- `INGRESS_IMAGE` -- anula la imagen del ingress (por defecto `quay.io/town/ingress:<tag>`). Establecerla a la cadena vacía se salta el ingress y el servicio de pages — modo de desarrollo.
- `GFEH_IMAGE` -- anula la imagen del almacenamiento de objetos (por defecto `quay.io/town/gfeh:<tag>`). Establecerla a la **cadena vacía** se salta el almacenamiento de objetos por completo — modo de desarrollo. El almacenamiento de objetos también se salta cuando el ingress está deshabilitado, ya que las cuatro vistas HTTP solo son alcanzables a través de él.
- `GFEH_SMB_PORT_BASE` -- anula el puerto de host desde el que empezarían los listeners SMB (por defecto `4450`). Vestigial: [ninguna partición sirve SMB](#sin-vista-smb), así que no se asigna ningún puerto del host. Se mantiene conectado para que el ajuste del arnés siga siendo inofensivo.
- `TOWN_OS_WG_SALT` -- la sal de instancia que separa los nombres de interfaz WireGuard, los puertos de escucha y las subredes de superposición de este equipo de los de otro Town OS que comparta el espacio de nombres de red. Sin establecer en un equipo real; la establecen los arneses de prueba y de desarrollo. Véase [La sal de la instancia](#la-sal-de-la-instancia).

#### Puertos de host de los servicios del sistema

Todos los servicios del sistema se ejecutan con `--net host`, así que todos estos enlazan en el espacio de nombres de red en el que esté el controlador — el espacio de nombres del *host*, incluso dentro del arnés de integración (cuyo contenedor también se ejecuta con `--net host`, a propósito, para que las compilaciones sigan funcionando en redes cautivas donde el DNS por bridge está roto). Por tanto, un equipo con `make test-full` y uno con `make dev` se pelean por todos y cada uno de estos puertos y, bajo `Restart=always`, se hacen entrar en bucle de caídas mutuamente para siempre.

Cada una de estas reubica uno de ellos y **usa por defecto el puerto de producción**, así que un entorno sin establecer reproduce exactamente el arranque actual. El `system_port_env` de `make/lib.sh` los asigna por ejecución en `SYSTEM_PORT_FILES` y se los pasa al contenedor de pruebas — REGLA DE HIERRO. `make dev` deliberadamente **no** establece ninguno: dev refleja un equipo real, donde `redirect_host_dns` necesita rolodex en el `:53` y un navegador necesita el ingress en el `:443`. Un valor no analizable se informa por stderr y recurre al predeterminado, porque de otro modo una errata se vería exactamente igual que no establecerlo.

- `TOWN_OS_DNS_PORT` -- el puerto en el que rolodex sirve DNS (`53` por defecto, en `DNSLoopback`). **El enrutado de systemd-resolved se salta por completo cuando este no es el predeterminado**: una dirección de servidor DNS por dominio no lleva puerto, así que apuntar resolved a `DNSLoopback` enviaría en silencio al vacío todas las consultas `.tld` en lugar de dejarlas a la ruta normal del resolutor.
- `TOWN_OS_ROLODEX_METRICS_PORT` -- el puerto en el que rolodex sirve su endpoint `/metrics` de Prometheus, también en `DNSLoopback` (`9153` por defecto). Es un listener distinto del puerto de DNS y necesita su propia anulación; `rolodex.Manager.MetricsAddr()` es la única cadena a partir de la cual se construyen tanto `rolodex.yml` como el objetivo de recolección de Prometheus, así que reubicarlo mueve los dos.
- `TOWN_OS_NODE_EXPORTER_PORT` -- el puerto de métricas de loopback de node-exporter (`9100` por defecto).
- `TOWN_OS_PROMETHEUS_PORT` -- el puerto de la API HTTP de loopback de Prometheus (`9090` por defecto).
- `TOWN_OS_MONITORING_PORT` -- el único puerto de monitorización de cara a la LAN (`5308` por defecto).
- `INGRESS_HTTPS_PORT` / `INGRESS_HTTP_PORT` -- los puertos publicados del ingress (`443` / `80` por defecto).

## Ajustes

| Clave                    | Por defecto                      | Descripción                                     |
| ------------------------ | -------------------------------- | ----------------------------------------------- |
| `default_quota`          | `53687091200`                    | Cuota de volumen predeterminada en bytes (50 GB) |
| `max_archive_size`       | `1073741824`                     | Tamaño máximo de subida en bytes (1 GB)         |
| `archive_unpack_timeout` | `600`                            | Tiempo límite de desempaquetado en segundos (10 min) |
| `locale`                 | `en-US`                          | Código de configuración regional BCP 47 (respaldo global) |
| `dns_tld`                | `home`                           | Dominio de nivel superior predeterminado para los registros DNS de paquetes |
| `dns_resolution_mode`    | `auto`                           | Resolución upstream de rolodex: `auto`, `recursive` o `forward` |
| `dns_local_forwarders`   | `false`                          | Tomar la lista de reenviadores de los resolutores que la propia red de este equipo le entregó, en lugar de los predeterminados públicos |
| `peer_ttl`               | `7200`                           | Vida de una inscripción de par WireGuard en segundos (2 h) |
| `gfeh_partition_quota`   | `0`                              | Cuota en bytes de cada partición de almacenamiento de objetos (0 = ilimitada) |
| `proton_image`           | `quay.io/town/proton:latest`     | Imagen del runner de Proton — **registrada solo bajo la etiqueta de compilación `proton`** |

`DefaultSettings` (`src/account/settings.go`) se siembra en la primera inicialización y los valores existentes nunca se sobrescriben.

Varias claves se **leen pero nunca se siembran** — no tienen fila hasta que algo
escribe una, y su valor por defecto vive en el punto de lectura como respaldo para
la cadena vacía. No las añadas a `DefaultSettings` esperando que no cambie nada
más: una fila sembrada es indistinguible de la elección de un operador, que para
las configuraciones de listas de bloqueo es la diferencia entre "nunca se
configuró, déjalo en paz" y "se estableció explícitamente a vacío, empújalo"
([Listas de bloqueo RBL / DNSBL](#listas-de-bloqueo-rbl--dnsbl)).

| Clave | Valor cuando está ausente | Escrita por |
| --- | --- | --- |
| `monitoring_backend`     | `uplot` | `POST /settings/set` |
| `dns_rbl_config` / `dns_dnsbl_config` | sin configurar (no es lo mismo que vacío) | `POST /dns/rbl`, `POST /dns/dnsbl` |
| `dns_excluded_services`  | lista vacía (la publicación es de exclusión voluntaria) | `POST /dns/services/set` |
| `dismissed_upgrades_hash` | ausente (nada descartado) | `POST /packages/upgrades/dismiss` |

**No existe ningún `object_storage_enabled` ni ninguna contraseña de cuenta de servicio.** El almacenamiento de objetos no es una función que haya que encender ([Arranque y reconciliación](#arranque-y-reconciliación)), y el demonio no tiene ninguna credencial de Town OS ([Sin cuentas de servicio](#sin-cuentas-de-servicio)). Una fila de cualquiera de las dos, olvidada en un equipo actualizado, no la lee nadie.

`proton_image` no está en el mapa base: `src/account/settings_proton.go` es `//go:build proton` y registra el valor predeterminado en `init()`, así que una compilación sin la etiqueta no tiene ajuste de Proton, ni ruta de instalación de Proton, e informa `proton_enabled: false` en el ping de estado. Se usa un registro condicionado por etiqueta de compilación en lugar de una función `Register` exportada para que ningún llamante adquiera una dependencia del orden de llamadas sobre `DefaultSettings`.
