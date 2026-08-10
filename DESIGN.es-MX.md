# Diseño de Town OS

Cómo funciona Town OS: la arquitectura, el comportamiento de cada subsistema, la
superficie de la API y los invariantes que lo sostienen todo. Las instrucciones
de compilación, las reglas de pruebas y el estilo de código están en
[CLAUDE.md](CLAUDE.md) (traducción al español de México en
[CLAUDE.es-MX.md](CLAUDE.es-MX.md)).

Un cambio de comportamiento le toca a este archivo, dentro del mismo commit que
lo introduce. Un cambio en cómo se compila o se prueba el repositorio le toca a
CLAUDE.md.

> **Este archivo es la traducción al español (México) de [DESIGN.md](DESIGN.md).
> El original en inglés es el autoritativo** — describe el cambio allá, y la
> traducción va después. Los identificadores de código, rutas de archivo,
> comandos, variables de entorno, rutas de la API y nombres de llaves YAML se
> conservan sin traducir.
>
> Otras traducciones: español de España ([DESIGN.es-ES.md](DESIGN.es-ES.md)) y
> chino, en escritura simplificada ([DESIGN.zh-Hans.md](DESIGN.zh-Hans.md)) y
> tradicional ([DESIGN.zh-Hant.md](DESIGN.zh-Hant.md)).

## Invariantes Arquitectónicos

Reglas que restringen el diseño, no el código. Romper una de ellas no hace fallar
una compilación ni un linter — produce un equipo que arranca y luego se porta
mal, normalmente en algún punto muy lejano del cambio.

- **La capa de almacenamiento administra volúmenes; gfeh provee el almacenamiento de objetos.** `src/storage` se ocupa de subvolúmenes btrfs y cuotas y de nada más -- no maneja para nada el almacenamiento de objetos. Los objetos, los metadatos y permisos por archivo, la base de datos jerárquica de usuarios/ACL, el compartir, la exposición HTTP por archivo, la federación y todas las vistas de protocolo (S3, IPFS, Google Drive, HTTP simple — y SMB/CIFS, que gfehd implementa pero Town OS no sirve) le pertenecen a gfeh, que es el responsable. Nunca agregues endpoints de objetos/blobs/por archivo a `src/storage` ni a `/storage/*`, y nunca le enseñes a `storage.Storage` ni a `storage.Controller` qué son los usuarios, los permisos o los protocolos. Ve [Almacenamiento](#almacenamiento).

- **La función de pages siempre está habilitada** — el subsistema de pages (alojamiento de sitios estáticos con Caddy) se inicializa incondicionalmente al arrancar; no hay ninguna barrera de entorno `TOWN_OS_PAGES`. El administrador de pages no es nil en un arranque normal, así que la API de pages siempre está disponible. Los manejadores todavía conservan una guarda defensiva de administrador nil que devuelve "pages not configured" (la ejercitan las pruebas que construyen un servidor sin `ServerConfig.PagesMgr`), pero los arranques reales nunca llegan ahí.

- **Detección de cambio de versión y reinicio de unidades** — el systemcontroller detecta las actualizaciones de imagen comparando el SHA de la imagen del contenedor en ejecución (de `/proc/1/cgroup` → `podman inspect`) contra un archivo de versión persistido en `<btrfsPath>/town-os-version`. Cuando cambia la versión: (1) se bajan todas las imágenes de contenedor, (2) se reconstruye la imagen del NC, (3) la reconciliación regenera todas las unidades de systemd, (4) las unidades cuyo contenido cambió se reinician en orden: primero las unidades NC (son las dueñas de las redes), luego los servicios de dependencias, luego los servicios padre/independientes, (5) los comandos posteriores a la actualización (campo `post_update`) se ejecutan con `podman exec` para los paquetes de contenedor cuyas unidades cambiaron. El archivo de versión se escribe después de una reconciliación exitosa. El contenido de las unidades se compara antes/después con `ReadUnit()` para evitar reinicios innecesarios cuando el contenido no cambió.

- **La imagen del controlador de red se baja, no se construye al arrancar** — la imagen del NC es una imagen hermana publicada (`quay.io/town/networkcontroller:<tag>`, con la etiqueta de `resolveImageTag()`) que se baja junto con las demás imágenes centrales, igualito que las imágenes de la interfaz, rolodex e ingress. **No** se construye con `podman build` durante el arranque; la construcción vieja en tiempo de arranque (`localhost/town-os-networkcontroller:local`, base alpine, `--dns=8.8.8.8`) ya no existe. `NC_IMAGE` sobrescribe el valor derivado por omisión y es lo que pone el arnés de integración para inyectar una imagen construida localmente. La descarga no es fatal: cada unidad NC de paquete trae un respaldo `ExecStartPre` de creación de red con `--pull=never`, así que una descarga fallida se puede recuperar en el siguiente arranque.

- **Todos los servicios de monitoreo son servicios del sistema** — Prometheus, Node Exporter y la interfaz de monitoreo corren todos bajo el espacio de nombres de servicios del sistema (prefijo `town-os-system--`), arrancados directamente desde `main.go` antes de la reconciliación. Nunca se instalan a través del sistema de repositorios de paquetes; no existe un paquete "monitoring" instalable. Los tres servicios son: `town-os-system--node-exporter.service` (red del host, puerto 9100), `town-os-system--prometheus.service` (puerto 9090, con configuración/datos montados desde `{btrfsBase}/monitoring/`) y `town-os-system--monitoring-ui.service` (puerto 5308). El servicio de la interfaz de monitoreo corre o bien un reenviador socat (modo uPlot, el predeterminado) o bien Grafana (modo grafana), controlado por el ajuste `monitoring_backend`. La configuración de Prometheus se escribe directamente en disco. Prometheus, Grafana y el reenviador socat de uPlot se generan con `systemd.GeneratePackageUnits` y `PackageUnitConfig.SystemServiceKey` definido, así que obtienen un controlador de red completo, activación por socket y una red podman privada — la misma plomería que los paquetes normales, pero con la nomenclatura de servicios del sistema.

- **La propiedad de los volúmenes del host es declarativa en `HostVolumeMount`, y no recursiva** — las imágenes de contenedor con un uid interno fijo (el `472` de Grafana, el `65534` de Prometheus, etc.) necesitan escribir en su ruta del host montada por bind, y los bind mounts pasan la propiedad del host tal cual, así que la ruta del host tiene que pertenecerle a ese uid:gid antes de que arranque el contenedor. Usamos bind mount (en lugar de un volumen podman con nombre, al que podman le haría chown en la primera creación) porque queremos los datos en un subvolumen btrfs con cuota. La estructura `systemd.HostVolumeMount` de `src/systemd/unit.go` trae los campos opcionales `UID *uint32` y `GID *uint32`; cuando los dos están definidos, el generador de unidades emite **`ExecStartPre=/bin/chown <uid>:<gid> <rutahost>`** (sin `-R`) para ese montaje justo después de las líneas `ExecStartPre=/bin/mkdir -p` y antes de `podman run`. Esta es la única fuente declarativa de propiedad para los volúmenes montados por bind desde el host en los servicios del sistema, y reemplaza las entradas de chown hechas a mano que antes vivían en `ExecStartPreExtra` de `GrafanaPackageConfig` y `PrometheusPackageConfig`.

  El chown es deliberadamente no recursivo, y con eso basta porque:
  1. **Los montajes escribibles** (`grafana-data` → `/var/lib/grafana`, `prometheus-data` → `/prometheus`) solo necesitan la propiedad del nivel de arriba para que el contenedor pueda crear adentro sus propios subdirectorios. El proceso del contenedor crea esos hijos con su propio uid (472 o 65534), así que ya quedan con la propiedad correcta y nunca se desvían. No hace falta recursión.
  2. **Los montajes de solo lectura** (`grafana-provisioning` → `/etc/grafana/provisioning`) no declaran UID/GID para nada y no emiten ninguna línea de chown. Mientras los permisos del host sean 0755/0644 (que es lo que pone `WriteGrafanaProvisioningFiles`), cualquier uid puede leer el contenido sin importar de quién sea.

  `EnsureGrafanaStorage` (`src/monitoring/monitoring_ui.go`) ahora nada más crea los directorios y regresa; no hace ningún chown. `WriteGrafanaProvisioningFiles` escribe los archivos YAML/JSON de la fuente de datos y de los tableros con permisos legibles por todos y no necesita arreglar la propiedad después. El chown en proceso basado en `filepath.WalkDir` que antes recorría `grafana-data` en cada arranque ya no existe; la única llamada al sistema `chown` que emite systemd es el arreglo autoritativo. Las constantes uid/gid siguen viviendo en sus respectivos archivos (`grafanaUID = 472` / `grafanaGID = 472` en `monitoring_ui.go`, `prometheusUID = 65534` / `prometheusGID = 65534` en `prometheus.go`); no las cambies sin que coincidan con la imagen de contenedor de origen.

- **El directorio de estado de red tiene que compartirse con el host** — el valor predeterminado de `-network-state` es `/run/town-os` (`DefaultNetworkStatePath` en `src/svc/systemcontroller/cmd/systemcontroller/main.go`). El systemcontroller corre dentro de un contenedor pero crea los contenedores NC en el host mediante `CONTAINER_HOST`, así que la ruta de origen del bind mount (`-v /run/town-os:/run/town-os:ro` en todas las unidades NC) tiene que existir en el sistema de archivos del host. La unidad de systemd del systemcontroller del repositorio de instalación debe montar por bind `/run/town-os:/run/town-os` y asegurarse de que el directorio del host exista antes de que arranque el systemcontroller (`ExecStartPre=/usr/bin/mkdir -p /run/town-os` o `RuntimeDirectory=town-os`). Sin ese montaje, el `os.MkdirAll` del systemcontroller y las escrituras de archivos de estado aterrizan dentro del tmpfs del contenedor, el directorio del host no existe y los contenedores NC no arrancan, con `Error: statfs /run/town-os: no such file or directory` — tirando Prometheus, la interfaz de monitoreo y todos los paquetes con red. Nunca uses por omisión `/var/run/town-os` ni ninguna ruta bajo `/var/run` o `/tmp`; la ruta tiene que vivir bajo `/run` (u otro bind mount compartido con el host) y tiene que ser la misma ruta de los dos lados del montaje.

## Secuencia de Arranque del Controlador del Sistema

El arranque del controlador del sistema en `src/svc/systemcontroller/cmd/systemcontroller/main.go` sigue este orden exacto. Cada paso marcado como **(no fatal)** registra en stderr y sigue; todo lo demás es fatal y aborta el arranque.

El arranque es **observable**: `:5309` se enlaza antes de que pase cualquier trabajo, respaldado por un stub mínimo de estado de arranque que transmite el progreso; el router Echo completo se intercambia al final sin cerrar nunca el listener. El progreso se reporta como cinco etapas gruesas (`boot_controller`, `boot_dns`, `boot_services`, `restart_packages`, `ready`) — ve [Estado de Arranque y Refresco](#estado-de-arranque-y-refresco).

1. **Definir `CONTAINER_HOST`** — `setupPodmanEnv()` define `CONTAINER_HOST=unix:///run/podman/podman.sock` para que toda invocación posterior de `podman` (y todo proceso hijo) se vaya por el socket de podman del host en lugar del almacenamiento aislado del contenedor del systemcontroller.
2. **Analizar los flags de CLI y las variables de entorno** — `-db`, `-btrfs`, `-repo-dir`, `-network-state`, `-listen`. Sobrescrituras por entorno: `TOWN_OS_LISTEN`.
3. **Enlazar `:5309` con el manejador de arranque** — `NewBootStatus()` + `NewRootHandler(NewBootHandler(bs))` enlazan el listener de inmediato, antes de cualquier trabajo de arranque. Hasta el intercambio del paso 24, el socket responde únicamente a `GET /status/ping` (503 con `{booting, step, done, boot_id}`) y `GET /boot-status` (SSE); todo lo demás es 403.
4. **Etapa `boot_controller`** — directorio de trabajo temporal; crear la base btrfs y el directorio de estado de red; borrar cualquier `town-os.db` viejo que hayan dejado despliegues antiguos en la raíz de btrfs (`cleanupStaleRootDB`) y rechazar una ruta `-db` que lo volviera a crear (`validateDBPath`) — la base de datos en ejecución vive en `<btrfsBase>/data/db/system.db`, nunca en la raíz.
5. **Abrir la base de datos SQLite** — persistente si `-db` está definido; si no, un archivo temporal efímero.
6. **Inicializar el administrador de cuentas** — crea la tabla de cuentas y migra una heredada (las columnas de capacidades pasan a ser permisos otorgados; se elimina `smb_nt_hash`). Después, `PurgeLegacyServiceAccounts` **(no fatal)** elimina la cuenta vieja de administrador del demonio de almacenamiento de objetos y su contraseña guardada, una sola vez, en el primer arranque después de una actualización — ve [Sin cuentas de servicio](#sin-cuentas-de-servicio).
7. **Generar una llave efímera de firma JWT** — 32 bytes aleatorios con `crypto/rand`, sobrescribible con `TOWN_OS_SIGNING_KEY`. Inicializa el administrador de sesiones, que borra todas las sesiones previas (los tokens viejos no sirven con la llave nueva).
8. **Inicializar los administradores de auditoría, ajustes, pages y red** — los ajustes se siembran con valores predeterminados (`default_quota`, `max_archive_size`, `locale`, `dns_tld`, `dns_resolution_mode`, `peer_ttl`, …); pages siempre se inicializa; el administrador de red es el dueño de las tablas de redes y pares de WireGuard **y siembra la red del hogar**, así que de aquí en adelante siempre existe (ve [La red del hogar siempre existe](#la-red-del-hogar-siempre-existe)).
9. **Sembrar los repositorios** — si `repositories.json` no existe, escribe los repositorios predeterminados (o los de prueba si `TOWN_OS_TEST`/`DEBUG`). Aplica las credenciales `TOWN_OS_REPO_USERNAME`/`TOWN_OS_REPO_PASSWORD`.
10. **Inicializar la raíz de repositorios y forzar un refresco** — clona/baja todos los repositorios configurados con go-git.
11. **Inicializar el administrador de instalación, el almacenamiento btrfs y el administrador de systemd**.
12. **Resolver la etiqueta de imagen** — `resolveImageTag()`: la variable de entorno `TOWN_OS_TAG` (que define el sistema de compilación de la instalación) y, si no, `rc.latest-<arch>` (`defaultVersionTag()`, con la arquitectura de `runtime.GOARCH` mapeada a `x86_64`/`aarch64` con `archTag()`). No hay archivo `/town-os.tag` ni versión `Version` fijada en tiempo de compilación. Todas las etiquetas de las imágenes hermanas (interfaz, rolodex, controlador de red, ingress) se derivan de este único valor; las etiquetas que se suben son por arquitectura, así que las etiquetas hermanas derivadas también.
13. **Derivar la imagen del NC** — `quay.io/town/networkcontroller:<tag>`, sobrescribible con `NC_IMAGE`. Se baja (paso 17), nunca se construye.
14. **Arrancar el refresco de repositorios en segundo plano** — una goroutine consulta cada 5 minutos.
15. **Etapa `boot_dns`: escribir la configuración de Rolodex y reiniciar si cambió** **(no fatal)** — Rolodex es un servicio de arranque que administra systemd. El systemcontroller escribe `rolodex.yml` (idempotente: lo omite si el archivo es más nuevo que el binario y el contenido no cambió) y reinicia el servicio solo cuando el archivo se escribió. `resolution.mode` viene del ajuste `dns_resolution_mode`, y un valor guardado que no se pueda analizar se va al predeterminado en lugar de renderizar una configuración que rolodex rechazaría. `forwarders:` viene del ajuste `dns_local_forwarders`: cuando está prendido, la lista se descubre a partir de los resolvedores del host en cada arranque, de modo que un equipo que cambió de red agarra el nuevo sin que el operador toque nada (ve [Reenviadores locales](#reenviadores-locales)). El contenedor de rolodex corre con `--net host` y enlaza el DNS directamente a `127.0.0.2:{puerto}`. Después espera a que el DNS esté listo (sondeo de conexión TCP) y configura systemd-resolved para enrutar el TLD a rolodex — **se omite cuando `TOWN_OS_DNS_PORT` movió a rolodex fuera de `:53`**, ya que una dirección de servidor por dominio en resolved no lleva puerto y mandaría al vacío todas las consultas de ese TLD.
16. **Leer el backend de monitoreo y descubrir los dispositivos de disco btrfs** — `monitoring_backend` (predeterminado `uplot`); `monitoring.BtrfsDevices(btrfsPath)` **(no fatal)** expone los dispositivos de bloque de respaldo a través de `/monitoring/status`.
17. **Etapa `boot_services`: bajar las imágenes de contenedor centrales** **(no fatal)** — la imagen del NC, Prometheus, Node Exporter, la imagen de la interfaz y Grafana cuando ese backend está seleccionado, en paralelo con `parallelEnsureImages` (se salta la descarga cuando la imagen ya está cargada).
18. **Arrancar los servicios de monitoreo del sistema** **(todos no fatales)** — primero se desmantelan las unidades NC/socket heredadas del diseño anterior (todavía retienen `-p 9090`/`-p 5308` y meterían en bucle de caídas a los servicios nuevos). Node Exporter, Prometheus y la interfaz de monitoreo corren todos con `--net host`; node-exporter y Prometheus enlazan al loopback, y solo el `:5308` de la interfaz de monitoreo da a la LAN. Los tres puertos vienen de `monitoringPortsFromEnv()`, cuyo valor cero son los predeterminados de producción ([Puertos de host de los servicios del sistema](#puertos-de-host-de-los-servicios-del-sistema)). Después se instala el temporizador nocturno de poda de podman **(no fatal)**.
19. **Asegurar la CA TLS local** **(no fatal)** — `tls.EnsureCA(<btrfsPath>/tls)` antes de la reconciliación, para que esta pueda emitir certificados hoja mientras recorre los paquetes instalados.
20. **Arrancar el ingress y el servicio de pages** **(no fatal)** — `ingressctl.Manager` instala y arranca `town-os-system--ingress` (router compartido SNI en `:443` + Host en `:80`), en doble pila solo cuando el host tiene una IPv6 global. El servicio Caddy de pages arranca junto con él. Los dos se omiten cuando `INGRESS_IMAGE` se define explícitamente como vacío (modo de desarrollo).
21. **Reconciliar el almacenamiento de objetos** **(no fatal)** — `ReconcileGfeh` asegura una partición gfeh por red: el subvolumen `gfeh/<network>` (con chown a uid 2000), el `gfehd.yaml` renderizado y la unidad `town-os-system--gfeh-<network>`, que se reinicia solo cuando el contenido renderizado cambió. Se omite por completo cuando `GFEH_IMAGE` está explícitamente vacío, y se omite cuando el ingress está deshabilitado (las cuatro vistas HTTP solo se alcanzan a través de él). Los *nombres* de las particiones se publican después y de forma asíncrona — ve el paso 30. Ve [Almacenamiento de Objetos (gfeh)](#almacenamiento-de-objetos-gfeh).
22. **Detectar el cambio de versión** — compara el SHA de la imagen del contenedor en ejecución (`/proc/1/cgroup` → `podman inspect`) contra `<btrfsPath>/town-os-version`. Define `versionChanged` para la reconciliación.
23. **Reconciliar** — recorre todos los paquetes instalados y restaura el estado de ejecución:
    - Crea los subvolúmenes btrfs raíz (`installed`, `uninstalled`, `archives`, `pages`, `vm-images`, `user`, `tls`, `gfeh`).
    - Para cada paquete instalado (la última versión por repositorio/nombre): carga el YAML, compila con las respuestas guardadas, crea los volúmenes btrfs con cuotas, siembra los volúmenes vacíos desde archivos/git/proton, aplica las plantillas de archivo, emite el certificado hoja TLS del paquete, escribe los archivos de estado de red (incluido el `fqdn` resuelto), genera e instala las unidades de systemd (servicio + NC + sockets) y arranca los servicios.
    - Si `versionChanged`: reinicia las unidades cuyo contenido cambió (primero NC, luego dependencias, luego servicios) y después corre los comandos `post_update`.
    - Reconcilia pages: asegura subvolúmenes, enlaces simbólicos y contenido de las páginas.
    Después persiste el SHA de la imagen actual en `<btrfsPath>/town-os-version`.
24. **Reconciliar el DNS y las redes** — marca el socket gRPC de rolodex (reintentando hasta 30 s). `RebuildDNS` limpia y reconstruye rolodex desde cero para descartar la deriva de una corrida previa que se cayó; `RebuildNetworkDNS` vuelve a registrar los registros globales que dan a la LAN (y los anclajes DANE) de los paquetes que no están en la red predeterminada. `ReconcileNetworks` reconcilia entonces el TLD de la red del hogar contra `dns_tld` y levanta la interfaz WireGuard de cada red habilitada, pasando el cliente de rolodex para que el alcance del TLD de cada red tenga dueño — incluido el alcance solo-DNS del hogar. Todo no fatal. Después se reconcilia el almacenamiento de objetos **por segunda vez** (idempotente), de modo que una red que este paso levantó obtiene su partición sin esperar a un reinicio.
25. **Programar el ingress** **(no fatal)** — espera a que esté listo, marca su socket gRPC y `RebuildIngress` empuja el conjunto completo de rutas (paquetes HTTP + páginas + vistas e índices del almacenamiento de objetos) de forma declarativa, el mismo modelo que `RebuildDNS`. También renderiza la página de índice de cada partición a partir de exactamente el mismo conjunto de sitios con el que se construyen esas rutas, en la misma pasada — una ruta no se puede programar antes de que existan los bytes que sirve ([El índice de la partición](#el-índice-de-la-partición)).
26. **Arrancar el contenedor de la interfaz** **(no fatal)** — `town-os-system--ui.service`; se omite cuando `UI_IMAGE` está explícitamente vacío (modo de desarrollo, donde bun sirve la interfaz).
27. **Etapa `restart_packages`: etapa de frescura** — si el proceso anterior dejó una marca de refresco, reinicia en serie todas las unidades de paquetes instalados, emitiendo un evento de progreso por paquete para que la interfaz renderice un renglón por cada uno. Una marca vieja de una caída es inofensiva.
28. **Crear el manejador HTTP** — conecta todos los administradores a `ServerConfig`, arranca los sondeos de fondo (IP externa cada hora, reparación de deriva de DNS, segador de pares vencidos) y configura el router Echo con CORS, la lista blanca de permisos otorgados que falla en cerrado, la autenticación y el middleware de auditoría.
29. **Etapa `ready`: intercambiar el manejador raíz** — el stub de arranque se reemplaza atómicamente por el router Echo completo en el listener ya enlazado, así que no hay ningún parpadeo de puerto y los suscriptores de `/boot-status` (SSE) en vuelo sobreviven al traspaso. `BootStatus.Done()` cierra entonces el flujo. **El sistema ya está listo.**
30. **Publicar los nombres del almacenamiento de objetos** **(no fatal, en segundo plano)** — `publishGfehNames` espera a que por lo menos una partición conteste en su socket de administración y luego vuelve a correr las reconstrucciones de DNS e ingress para que la salida de `/v1/names` de cada partición se convierta en registros A, anclajes TLSA, SAN de las hojas y vhosts del ingress. Corre **después** del intercambio, y de forma asíncrona, porque gfehd sondea `/status/ping` — que responde 503 hasta el paso 29 — antes de autenticarse, así que esperarlo en línea trabaría el mismo arranque que está esperando. Si nada queda listo a tiempo, los nombres los publica la siguiente reconciliación.
31. **Apagado ordenado** — con SIGINT: cancela el contexto y apaga el servidor HTTP con un tiempo límite de 30 s. Todas las goroutines de fondo salen por la cancelación del contexto.


# Especificación Funcional de Town OS

Town OS es una plataforma de nube autoalojada para usuarios domésticos. Corre por completo desde una unidad USB en RAM, usando todo el almacenamiento del sistema para los datos del usuario. El empaquetado, el almacenamiento y la red están totalmente integrados. Una interfaz web da la administración para usuarios no técnicos.

## Biblioteca de Git

Todas las operaciones internas de git usan una biblioteca puramente en Go (`go-git/go-git/v5`) en lugar de invocar el CLI `git`.

### Interfaz del Cliente

La interfaz `git.Client` abstrae todas las operaciones de git:

- **Clone** -- clona un repositorio en un subdirectorio con nombre de un directorio padre.
- **Pull** -- hace pull con rebase.
- **Diff** -- reporta si el árbol de trabajo tiene cambios sin confirmar.
- **Stash / StashApply** -- guarda y vuelve a aplicar los cambios sin confirmar.
- **Fetch** -- baja del remoto origin.
- **Checkout** -- se cambia a una rama, etiqueta o hash de commit.
- **Init** -- inicializa un repositorio nuevo. Devuelve un error si el directorio padre no existe.
- **Add** -- prepara archivos por pathspec (acepta `"."` para todos los archivos).
- **Commit** -- crea un commit usando la configuración local de usuario de git (se va a `Town OS <town-os@localhost>` si no hay).
- **RevParse** -- resuelve una referencia a un hash SHA.
- **Run** -- despacha subcomandos arbitrarios de git (`config`, `branch`, `rev-parse --abbrev-ref`, `log`, `init`, `status`).

### Implementación

`GoGitClient` implementa la interfaz usando `go-git`. Acepta:

- Credenciales incrustadas en la URL (`esquema://usuario:contraseña@host/...`), que se extraen y se pasan como `http.BasicAuth`.
- Tiempos límite y cancelación basados en contexto en todas las operaciones.
- Un campo `Home` que sobrescribe el directorio HOME para operaciones aisladas.

### Cliente Simulado

`MockClient` provee una implementación simulada y segura para hilos para las pruebas unitarias. Registra todas las llamadas a métodos con sus argumentos y acepta errores y valores de retorno inyectables por método.

### Uso

- **Repositorios de paquetes**: clonado, pull (con stash/apply alrededor de los árboles sucios) y fetch para el refresco de repositorios (con `GoGitClient`).
- **Siembra de volúmenes**: clonar repositorios git en volúmenes vacíos durante la instalación y la reconciliación (con `GoGitClient`).
- **Pages**: clonar y actualizar repositorios de sitios estáticos (con `GoGitClient`).
- **Reconstrucción de origen git**: actualizar los volúmenes git de un paquete instalado y reiniciar el servicio dependiente (con `GoGitClient`).

## Administración de Repositorios

### Modelo de Repositorio

Los repositorios se definen por un nombre, una URL y credenciales opcionales (usuario y contraseña). Se guardan en un archivo `repositories.json` en el directorio base. Se siembra un repositorio predeterminado si no hay ninguno configurado.

### API de Repositorios

- `POST /repository/add` (requiere admin) -- agrega un repositorio nuevo. Acepta nombre, URL y credenciales opcionales de usuario/contraseña. Si no se dan credenciales, se usan las predeterminadas del sistema. El repositorio se clona con go-git y se dispara un refresco.
- `POST /repository/remove` (requiere admin) -- elimina un repositorio por nombre y dispara un refresco.
- `POST /repository/move` (requiere admin) -- cambia la posición de prioridad de un repositorio. Acepta el nombre y el índice de la posición destino.
- `POST /repository/refresh` (requiere admin) -- fuerza el refresco de todos los repositorios. Devuelve los errores de refresco que haya.
- `GET /repository` (requiere autenticación) -- lista todos los repositorios con búsqueda, ordenamiento y paginación. Cada entrada incluye nombre, URL, usuario y cualquier error de refresco.

### Refresco de Repositorios

Los repositorios se refrescan periódicamente (intervalo predeterminado de 5 minutos) bajando de origin con go-git. Se usa stash/apply alrededor de los árboles sucios durante el refresco. Los errores de refresco se registran por repositorio y se exponen a través de los endpoints de listado y de ping de estado.

## Sistema de Paquetes

### Definición de Paquete

Los paquetes se definen en YAML con la siguiente estructura:

- `image` -- referencia a la imagen de contenedor (mutuamente excluyente con `vm`).
- `vm` -- configuración de máquina virtual (mutuamente excluyente con `image`). Ve **Configuración de VM** más abajo.
- `proton` -- configuración del runner Proton/Wine para ejecutables de Windows (mutuamente excluyente con `vm` y `command`). Ve **Configuración de Proton** más abajo.
- `entrypoint` -- lista de cadenas que reemplaza el `ENTRYPOINT` que trae la imagen al momento de `podman run`. Se emite como `podman run --entrypoint='["..."]'` (arreglo JSON, entrecomillado con comillas simples para que systemd lo reenvíe literal). Necesario para imágenes cuyo ENTRYPOINT de origen es un script envoltorio que rechaza argumentos de comando arbitrarios (p. ej., el `/start.py` de `matrixdotorg/synapse` interpreta el primer argumento como un "modo" y da error con cualquier valor desconocido — un paquete que quiera `command: [sh, -c, "…"]` también tiene que poner `entrypoint: [sh, -c]` para que podman reemplace `/start.py` por completo). Solo para el runtime de contenedor; se rechaza en paquetes de VM (`ErrEntrypointVMNotSupported`) y en paquetes de Proton (Proton genera su propio comando automáticamente).
- `command` -- lista de cadenas que se convierte en el CMD del contenedor (los argv que se pasan DESPUÉS del entrypoint). Solo para el runtime de contenedor; mutuamente excluyente con `proton`. Los argumentos de varias palabras que traen espacios en blanco o metacaracteres de shell se entrecomillan con comillas simples en el archivo de unidad generado para que el tokenizador de ExecStart de systemd los reenvíe como un solo elemento de argv — una cadena encadenada `"a && exec b"` se queda como un solo argumento y su `&&` se reenvía a `sh -c` (cuando el entrypoint es `[sh, -c]`) en lugar de que systemd la parta.
- `environment` -- variables de entorno llave-valor (acepta sustitución de plantillas; solo runtime de contenedor).
- `network` -- mapeos de puertos externos e internos (acepta sustitución de plantillas).
- `volumes` -- volúmenes con nombre, con punto de montaje, cuota opcional, origen de archivo opcional, URL de siembra git opcional y UID/GID opcionales.
- `questions` -- preguntas con nombre que se le presentan al usuario durante la instalación.
- `notes` -- metadatos tipados (URL, teléfono, correo electrónico) que se muestran después de instalar. Los tipos se validan durante la compilación: las URL se tienen que analizar como URL válidas, los correos tienen que coincidir con el formato `usuario@dominio.tld` y los números de teléfono tienen que coincidir con dígitos y caracteres de formato opcionales.
- `description` -- descripción del paquete legible por humanos.
- `supplies` -- lista de capacidades que provee este paquete.
- `archives` -- lista de archivos comprimidos de imágenes de contenedor con los que poblar volúmenes al momento de instalar (solo runtime de contenedor).
- `templates` -- plantillas de archivo con nombre que se renderizan en los volúmenes con text/template de Go. Cada plantilla especifica un volumen destino, una ruta de archivo y el contenido de la plantilla.
- `post_update` -- lista de comandos de shell que se ejecutan dentro del contenedor en marcha después de detectar un cambio de SHA de imagen durante la reconciliación (solo runtime de contenedor; no se admite en paquetes de VM). Ve **Comandos Posteriores a la Actualización** más abajo.

### Tipo de Runtime

Cada paquete tiene un tipo de runtime: `container` (predeterminado) o `vm`. El runtime se determina por cuál campo de primer nivel está presente: `image` (o `proton`) selecciona el runtime de contenedor (podman), `vm` selecciona el runtime de VM (QEMU). Un paquete debe especificar exactamente uno de `image`/`proton` o `vm`; especificar los dos o ninguno es un error de validación. Los paquetes de Proton son una forma especializada de paquete de contenedor -- usan el runtime de contenedor pero generan el comando automáticamente y extraen los archivos de la aplicación de Windows de una imagen de contenedor aparte.

### Configuración de VM

La sección `vm` configura una máquina virtual QEMU:

- `image` -- URL de la imagen de disco de la VM o nombre de archivo local (obligatorio). Puede ser una URL HTTP/HTTPS para imágenes remotas o un nombre de archivo que referencie una imagen en caché en el subvolumen `vm-images`. Acepta sustitución de plantillas `@variable@`.
- `memory` -- memoria de la VM como cadena de bytes legible por humanos (p. ej., `2gb`, `512mb`). Por omisión `1gb`. Acepta sustitución de plantillas `@variable@`.
- `cpus` -- número de CPU virtuales. Por omisión `1`. Debe ser no negativo.

### Configuración de Proton

La sección `proton` configura una aplicación de Windows para correrla con la capa de compatibilidad Proton/Wine:

- `app_image` -- referencia a la imagen de contenedor que trae los archivos de la aplicación de Windows (obligatorio). Se normaliza durante la compilación. Acepta sustitución de plantillas `@variable@`.
- `app_directory` -- ruta absoluta dentro del contenedor donde está instalada la aplicación (obligatorio, p. ej., `/app`). Acepta sustitución de plantillas `@variable@`.
- `volume` -- nombre de un volumen de paquete definido donde se extraerán los archivos de la aplicación (obligatorio). Acepta sustitución de plantillas `@variable@`.
- `exe` -- ruta al ejecutable de Windows que hay que correr (obligatorio, p. ej., `/app/myapp.exe`). Acepta sustitución de plantillas `@variable@`.
- `args` -- argumentos opcionales de línea de comandos que se le pasan al ejecutable. Cada elemento acepta sustitución de plantillas `@variable@`.

Al momento de instalar, el sistema baja `app_image`, extrae `app_directory` en el volumen indicado y genera automáticamente el comando del contenedor como `proton run <exe> [args]`. La imagen de contenedor que se usa para correr la aplicación es la del ajuste global `proton_image` (`quay.io/town/proton:latest` por omisión), que se puede sobrescribir por paquete definiendo `image`. Durante la reconciliación, la extracción de la aplicación solo se repite si el volumen destino está vacío.

### Variables de Plantilla

La sustitución de plantillas usa la sintaxis `@nombre_variable@`. Las variables se reemplazan con las respuestas a las preguntas durante la compilación del paquete. La sustitución aplica a: valores de entorno, nombres y destinos de puertos de red, puntos de montaje de volúmenes, cuotas de volúmenes, referencias de archivos de volúmenes, URL de git de volúmenes, URL de imágenes de VM y valores de memoria de VM. También hay dos variables integradas disponibles: `@LOCAL_EXTERNAL_HOST@` y `@LOCAL_INTERNAL_HOST@`.

La secuencia `@@` es un escape literal de `@`. Para producir un `@` literal seguido de una variable de plantilla, usa tres signos `@`: `@@@variable@`. Por ejemplo, `ssh://git@@@PACKAGE_DNS@:@sshport@` se resuelve como `ssh://git@gitea.default.home:2222`. Un `@@` solito se resuelve como `@` (p. ej., `admin@@example.com` → `admin@example.com`).

La compilación de las notas usa un resolvedor de una sola pasada (`ApplyTemplates`) que junta las variables de contexto (`PACKAGE_DNS`, `LOCAL_EXTERNAL_HOST`, `LOCAL_INTERNAL_HOST`) y las respuestas del usuario en una sola pasada, manejando bien los escapes `@@`. Los demás campos (entorno, puertos, volúmenes) usan un resolvedor por llave (`applyTemplate`) que conserva `@@` a través de varias pasadas, con una resolución final de `@@` → `@` al terminar `Compile`.

### Preguntas

Las preguntas se le hacen al usuario durante la instalación del paquete. Cada pregunta tiene un `query` (texto que se muestra), un `type` opcional (tipo de salida para la validación) y un valor `default` opcional. Los nombres de las preguntas deben empezar con un carácter alfanumérico y solo pueden contener caracteres alfanuméricos y guiones bajos (p. ej. `port`, `dbpass`, `registration_secret`). Los guiones, los puntos y otros signos de puntuación se rechazan; los guiones bajos se permiten porque los nombres de las preguntas se usan como marcadores `@plantilla@` y los identificadores de varias palabras como `registration_secret` son comunes en paquetes reales.

#### Tipos de Salida

- **port** -- número de puerto validado (1--65535). Genera automáticamente un puerto libre aleatorio en el rango 10000--60000 cuando la respuesta está vacía o es `"auto"`.
- **hostname** -- alfanumérico en minúsculas con guiones. Genera automáticamente `<nombre-paquete>-<4-hex>` cuando está vacío.
- **volume** -- alfanumérico con guiones y guiones bajos.
- **bytes** -- tamaños en bytes legibles por humanos (sufijos `mb`, `gb`, `tb`).
- **archive** -- nombre de archivo comprimido.
- **duration** -- duraciones de tiempo (sufijos `s`, `m`, `h`, `d`).
- **secret** -- genera automáticamente un valor criptográficamente seguro cuando la respuesta está vacía o es `"auto"`. Genera 32 bytes con `crypto/rand`, devueltos como una cadena hexadecimal de 64 caracteres (256 bits de entropía). Sirve para contraseñas, sales de llaves de cifrado y otros valores secretos. Los usuarios pueden sobrescribirlo dando una respuesta explícita.
- **boolean** -- una opción sí/no, renderizada como **casilla de verificación** en el diálogo de preguntas de instalación en lugar de como entrada de texto. La validación es `strconv.ParseBool`, que acepta exactamente las grafías que yaml.v3 (YAML 1.2) trata como booleanas más `1`/`0`/`t`/`f`, sin distinguir mayúsculas; `yes`/`no` **no** se aceptan. La respuesta se normaliza a la cadena `"true"` o `"false"`, de modo que la sustitución `@variable@` y las plantillas de archivo (`{{.Responses.key}}`) siempre ven una forma canónica y se pueden probar con `{{if eq .Responses.key "true"}}`.

  Una casilla sin marcar no envía nada, y muchas veces la pregunta booleana de una dependencia se queda sin responder por parte de su padre — las dos cosas harían brincar de otro modo la validación de respuesta vacía de `Compile`. Por eso `autoGenerateResponses` (`controller_install_preview.go`) resuelve un booleano ausente o vacío al `default` de la pregunta (normalizado), o a `"false"` cuando no se declara ningún valor por omisión. Un `"false"` explícito del formulario siempre le gana a un `default: "true"`, de modo que una opción prendida por omisión sí se puede apagar; un `default` que `strconv.ParseBool` no pueda analizar es un error del paquete y hace fallar la instalación en lugar de instalar en silencio con la opción apagada.

  El diálogo de información del paquete muestra las respuestas booleanas guardadas como Sí/No en lugar de la cadena cruda `"true"`/`"false"`, y las preguntas booleanas se saltan la ruta de valor en caché/botón de borrar del diálogo de instalación — una respuesta guardada nada más premarca la casilla y sigue siendo editable directamente.

- **oauth** -- un token que se obtiene corriendo un flujo de dispositivo desde el diálogo de instalación, en lugar de escribirse. Se valida como un secreto (cualquier cadena no vacía), nunca se genera automáticamente y se enmascara en el diálogo de información del paquete. El diálogo de instalación muestra un botón **Conectar** en lugar de un campo de texto; una respuesta en caché de una instalación anterior se muestra como ya conectada, de modo que una reinstalación no manda al operador de regreso con el proveedor.

#### Preguntas OAuth

Algunas aplicaciones se configuran con una credencial que solo su proveedor puede acuñar -- un token de cuenta de Plex, un token personal de GitHub -- y la única forma de conseguirla ha sido correr un script de shell a mano y pegar lo que imprimió. Una pregunta `oauth` corre ese flujo desde el diálogo.

**No hay ningún registro de proveedores.** La pregunta trae un bloque `oauth:` que nombra las URL del propio proveedor, así que un paquete puede usar cualquier proveedor con un flujo tipo dispositivo sin cambiarle nada a Town OS:

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

`start` abre el flujo; `extract` nombra los campos JSON que hay que sacar de su respuesta; `approve` es la URL que abre el navegador; `poll` se repite hasta que el campo JSON que nombra `token` deja de estar ausente o nulo, que es exactamente cómo se ve "el usuario todavía no aprueba" sobre el cable. Los marcadores `{{...}}` se resuelven contra los valores extraídos más `{{client_id}}`, un identificador aleatorio por flujo que el controlador manda en cada paso (Plex amarra el pin a él). Un número JSON extraído se renderiza como dígitos, no como `1.234567e+06` -- un id de pin formateado como flotante daría 404 en la URL de sondeo y se quedaría colgado para siempre en "pendiente".

El flujo vive en `src/packages/oauth.go` (esquema más validación) y en `src/svc/systemcontroller/controller_oauth.go` (ejecución). `POST /packages/oauth/start` corre el paso de inicio y devuelve `{flow_id, approve_url, user_code, interval_ms}`; `POST /packages/oauth/poll` corre un paso de sondeo y devuelve `pending`, `approved` con el token, o `expired`. Los dos requieren admin. El servidor conserva el flujo solo hasta que se canjea -- el token se le entrega al navegador, que lo envía como respuesta de la pregunta igual que cualquier otra, así que guardar una copia en el servidor nada más agregaría un segundo lugar del cual se puede filtrar.

La validación viene en dos mitades, y confundirlas es un error. `ValidateOAuthSpec` revisa la *forma* del flujo (campos obligatorios, duraciones analizables, ninguna plantilla en el host de una URL) y es lo que corre `Compile` cuando se instala un paquete. `ValidateOAuthFlow` es eso más la política de direcciones de abajo, y solo corre cuando un flujo está a punto de *ejecutarse*. Una instalación pasa mucho después de que corrió su flujo, en un host cuyo ajuste `OAuthAllowPrivate` `Compile` no puede ver — así que aplicar la política de direcciones en tiempo de compilación rechazaría una instalación cuyo propio flujo acababa de salir bien.

**La guarda de direcciones carga peso.** Un paquete nombra URL arbitrarias y es el *controlador* el que las marca, así que sin una guarda un paquete podría apuntarlo a la propia red del host. `packages.CheckOAuthAddr` corre en el `DialContext` del cliente HTTP (y en cada redirección) y rechaza las direcciones de loopback, privadas, link-local, multicast, sin especificar y CGNAT; las URL tienen que ser `https`. Revisarlo al momento de la conexión y no al del análisis es lo que lo hace a prueba de DNS rebinding. `ServerConfig.OAuthAllowPrivate` lo afloja y existe únicamente para que las pruebas puedan apuntar un flujo a un servidor `httptest` en 127.0.0.1.

#### Preguntas opcionales

Cualquier pregunta puede poner `optional: true`. Todas las demás preguntas se tienen que responder con un valor no vacío, lo cual no le deja al autor de un paquete ninguna manera de expresar un ajuste del que la aplicación de veras puede prescindir — un relay SMTP, una llave de API — más que inventarse un valor por omisión de relleno y confiar en que el operador lo sobrescriba.

Una pregunta opcional puede estar ausente del mapa de respuestas o responderse con una cadena vacía; `Compile` la exenta tanto de `ErrMissingResponse` como de `ErrEmptyResponse`, y sustituye la **cadena vacía** en sus puntos `@variable@`. Una respuesta en blanco también se salta `OutputType.Output`, cuyo trabajo es rechazar justo eso en una pregunta tipada — una cadena vacía no es un puerto válido — así que `optional` se compone con `type`: un puerto opcional que sí se responde se sigue validando como puerto, mientras que uno en blanco se compila a nada.

Dos detalles importan para la corrección. `Compile` sustituye recorriendo las respuestas que recibió, así que una pregunta omitida por completo del mapa recibe una segunda pasada que rellena sus marcadores con la cadena vacía; sin ella, el literal `@smtp_host@` sobreviviría hasta el entorno del contenedor. Y `autoGenerateResponses` se salta las preguntas opcionales antes del switch de tipo: generar un valor derrotaría a la pregunta, ya que un secreto opcional en blanco llegaría si no como una cadena aleatoria de 256 bits con la que la aplicación trataría diligentemente de autenticarse. Una pregunta opcional en blanco se va a su `default` si declara uno, y a la cadena vacía si no.

`optional` no tiene sentido en un booleano, que es una casilla de verificación y siempre se resuelve a uno de sus dos valores.

#### Preguntas condicionales (`show_if`)

Una pregunta puede traer `show_if: <pregunta_booleana>`, nombrando una pregunta booleana del mismo paquete. El diálogo de instalación mantiene la pregunta escondida hasta que esa casilla se marca, de modo que un paquete puede esconder un grupo avanzado — un relay SMTP, una llave de API — detrás de un solo interruptor en lugar de aventarle al operador todos los campos de golpe.

Es más que una pista de interfaz: el compilador la respeta. Mientras el booleano de control se resuelva a falso, la pregunta condicional compila a la **cadena vacía** y queda exenta del requisito de estar respondida y no vacía — exactamente como si fuera `optional` y se hubiera dejado en blanco — *sin importar lo que haya enviado el campo que sigue montado*. `questionHidden` (`src/packages/questions.go`) lee el valor de control de la respuesta enviada, yéndose al `default` declarado del booleano cuando el operador nunca lo tocó, y lo analiza con manga ancha porque una casilla sin marcar puede llegar como `"false"`, `"0"` o no llegar para nada. `Compile` fuerza la cadena vacía y se salta `Output()` en una pregunta escondida, así que un valor viejo no puede hacer fallar la validación de tipo de un campo que el operador ni siquiera puede ver; una pregunta omitida por completo del mapa de respuestas de todos modos recibe sus puntos `@marcador@` rellenos con la cadena vacía. Cuando el booleano es verdadero, una pregunta condicional no opcional se exige como siempre.

`ValidateShowIf` rechaza un `show_if` que referencia una pregunta que no existe (`ErrShowIfUnknown`), una que no es de tipo `boolean` (`ErrShowIfNotBool`), la pregunta misma (`ErrShowIfSelf`) u otra pregunta que a su vez es condicional (`ErrShowIfChain` — nada de cadenas). Una pregunta condicional solo es coherente si lo que controla su visibilidad es una casilla de verificación simple.

### Compilación

La compilación valida todas las respuestas, aplica la validación específica de cada tipo, sustituye todas las variables de plantilla, normaliza las URL de las imágenes de contenedor y produce una estructura `Package` resuelta. En los paquetes de VM, las cadenas de memoria se analizan a conteos de bytes y se aplican los valores por omisión de CPU. A los comandos posteriores a la actualización se les recortan los espacios en blanco de inicio y de final. Los errores de validación se juntan y se devuelven todos juntos.

### Comandos Posteriores a la Actualización

El campo `post_update` es una lista de cadenas de comandos de shell que se ejecutan dentro del contenedor en marcha después de que el controlador del sistema detecta un cambio de SHA de imagen durante la reconciliación. Esto permite tareas de migración automatizadas (p. ej., `pg_upgrade` después de que se actualiza un contenedor de PostgreSQL).

- **Solo contenedor** -- `post_update` se rechaza durante la validación en paquetes de VM (`ErrPostUpdateVMNotSupported`).
- **Sustitución de plantillas** -- cada comando acepta sustitución `@variable@` a partir de las respuestas a las preguntas, igual que los campos de entorno y de red.
- **Recorte de espacios** -- a cada comando se le recortan los espacios de inicio y de final durante la compilación. Los comandos vacíos o que solo traen espacios se rechazan durante la validación.
- **Disparador de ejecución** -- los comandos se ejecutan solo cuando `ReconcileConfig.VersionChanged` es verdadero Y el contenido de la unidad de systemd del paquete difiere del de la unidad instalada antes. Si cualquiera de las dos condiciones es falsa, no corre ningún comando.
- **Orden de ejecución** -- los comandos corren en secuencia después de que terminan todos los reinicios por cambio de versión (primero las unidades NC, luego las dependencias, luego los servicios, luego los comandos posteriores a la actualización). Dentro de un paquete, los comandos corren en el orden de la lista.
- **Método de ejecución** -- cada comando corre con `podman exec <nombre-contenedor> sh -c '<comando>'` con un tiempo límite de 5 minutos. La función `PostUpdateExec` de `ReconcileConfig` provee el mecanismo de ejecución; si es nil, la ejecución posterior a la actualización queda deshabilitada.
- **No fatal** -- las fallas de los comandos se registran pero no detienen la reconciliación ni impiden que corran los comandos siguientes.

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

El contexto de datos de la plantilla provee cuatro espacios de nombres:

- `.Responses.key` -- valores de las respuestas a las preguntas (indexados por el nombre de la pregunta).
- `.Package.Name`, `.Package.Version`, `.Package.Repo`, `.Package.Image`, `.Package.Description` -- metadatos del paquete.
- `.System.Hostname`, `.System.ExternalIP`, `.System.InternalIP` -- información a nivel de sistema.
- `.Dep.KEY.Host` y `.Dep.KEY.Ports` -- coordenadas de ejecución de las dependencias instaladas, indexadas por la misma llave de dependencia que declara el YAML del padre bajo `dependencies:`. `Host` es el nombre del contenedor podman (resoluble con el DNS de podman en la red compartida); `Ports` es un `map[string]string` indexado tanto por el puerto numérico del contenedor (p. ej. `"5432"`) como por cualquier nombre semántico declarado en la entrada de red de la dependencia (en minúsculas, p. ej. `"sql"`). Accede a un puerto con nombre con `{{index .Dep.db.Ports "sql"}}`. El mapa es nil en paquetes sin dependencias; `{{.Dep.db.Host}}` sobre una dependencia ausente renderiza `<no value>` (como cualquier otra llave de mapa que falte) e `index` sobre unos `Ports` nil da error a propósito para que las plantillas mal configuradas fallen de forma ruidosa.

Los campos `volume` y `path` aceptan sustitución `@variable@` (el mismo mecanismo que usan los campos de entorno, red y volúmenes). El campo `content` usa la sintaxis `text/template` de Go con `{{.Responses.key}}`, `{{.Package.Name}}`, `{{.Dep.KEY.Host}}`, etc. La forma de marcador `@dep_*@` NO se respeta dentro de `content` — usa en su lugar el espacio de nombres `.Dep` de las plantillas de Go; `@dep_*@` sigue siendo la forma correcta en los valores de `environment:` y en los bloques `responses:` de las dependencias.

Las plantillas se aplican después de la siembra de volúmenes (archivos, clones git) **y después de que se instale cualquier dependencia**, así que `.Dep` ya está poblado para cuando se renderiza el contenido del padre. Durante la reconciliación, las plantillas se vuelven a renderizar pero los archivos existentes nunca se sobrescriben, conservando los datos de las subidas de archivos o de corridas anteriores; el mapa de dependencias se reconstruye a partir de los registros de dependencias persistidos, de modo que `.Dep` sigue resolviendo cuando la reconciliación de veras escribe una plantilla que faltaba.

La validación exige: los nombres de las plantillas siguen la convención de nombres de volúmenes (alfanuméricos con puntos, guiones y guiones bajos), las rutas tienen que ser relativas y sin recorrido de directorios, el volumen tiene que referenciar un volumen definido del paquete (salvo que el campo de volumen traiga variables de plantilla) y el contenido tiene que analizarse como `text/template` de Go válido.

### Normalización de Imágenes

Las referencias a imágenes de contenedor se normalizan durante la compilación:
- Un nombre solito (`nginx`) pasa a `docker.io/library/nginx:latest`.
- Dos componentes (`user/app`) pasan a `docker.io/user/app:latest`.
- Las referencias completas se conservan; se agrega `:latest` si no hay etiqueta.

### Persistencia de Respuestas

Las respuestas se guardan por versión en `responses/<repo>/<pkg>/<version>.json`. Se guarda una copia `last` en `responses/last/<repo>/<pkg>.json` para reutilizarla en actualizaciones y reinstalaciones desde volúmenes desinstalados. Las últimas respuestas se borran después de una instalación exitosa.

Dos endpoints de la API administran las últimas respuestas:

- `POST /packages/last-responses` (requiere admin) -- recupera las últimas respuestas en caché de un paquete (por repositorio y nombre).
- `POST /packages/clear-last-responses` (requiere admin) -- elimina el archivo de últimas respuestas en caché.

### Interfaz de Preguntas de Instalación

Cuando un usuario instala un paquete, el diálogo de preguntas carga las respuestas existentes (de una instalación actual) y, si no hay ninguna, las últimas respuestas en caché (de una desinstalación previa). Las respuestas actuales tienen prioridad sobre las últimas respuestas.

**Las respuestas en caché** se muestran como contenedores estilizados de solo lectura con fondo atenuado, mostrando el valor guardado (las contraseñas se muestran como `********`). Una entrada de formulario escondida conserva el valor para el envío. Cada campo en caché tiene un botón de borrado (icono X) con un tooltip ("Bórralo para escribir un valor nuevo") que, al hacerle clic, reemplaza la vista de solo lectura por una entrada editable. El botón de borrado usa un estilo fantasma que se pone rojo al pasar el cursor.

**Los valores por omisión** se muestran de dos formas cuando no hay valor en caché: como texto de marcador de posición en la entrada (p. ej., "Default: 8080") y como texto de ayuda debajo de la entrada, atenuado y con el valor en monoespaciado. Cuando no hay ningún valor por omisión definido se muestran marcadores de posición específicos del tipo: "Auto-assigned if empty" para los puertos, "Auto-generated if empty" para los nombres de host y "e.g. 30s, 5m, 2h, 1d" para las duraciones.

**Los errores de validación** del servidor se muestran por campo como texto rojo debajo de la entrada, y la entrada recibe un borde rojo.

**Tamaño y paginación.** El diálogo está limitado a la altura del viewport (menos los márgenes) y acomodado como columna flex, de modo que el encabezado y el pie se quedan quietos mientras el área de preguntas se desplaza — el `overflow-hidden` del `DialogContent` base hacía inalcanzable de otro modo lo que se desbordaba en un paquete con muchas preguntas. Las preguntas se paginan **5 por página** con controles Anterior/Siguiente que le dan paso al botón Instalar en la última página. Todas las páginas se quedan montadas (las inactivas están en `display:none`) para que las entradas de formulario no controladas conserven los valores escritos y se sigan enviando; desmontar una página tiraría en silencio las respuestas que trae. Un error de campo brinca a la página que lo trae, así que un error de validación nunca queda escondido detrás del paginador. El paginador reutiliza las cadenas existentes `datatable.next`/`previous` y un contador numérico de páginas, así que no agrega llaves de traducción.

**Las preguntas condicionales** declaradas con `show_if` están escondidas hasta que se marca su casilla de control (ve [Preguntas condicionales](#preguntas-condicionales-show_if)).

**Las preguntas OAuth** se renderizan a partir de un solo estado por pregunta — `idle`, `starting`, `waiting`, `connected`, `error` — sembrado desde la respuesta en caché, no a partir de "¿existe un token en algún lado?". Un token en caché de una instalación anterior hacía que el campo se leyera como conectado antes de que hubiera pasado nada, y lo mantenía así a lo largo de una reconexión fallida, poniendo una insignia verde de Conectado encima de un error rojo. Ahora el token se lee para exactamente una decisión (Conectar contra Reconectar) y por lo demás es solo lo que envía la entrada escondida: una reconexión fallida le deja al operador el token que ya tenía, pero nada afirma que el intento fallido haya funcionado, una reconexión todavía en vuelo no se lee como conectada, y una aprobación que no trae token es un error en lugar de un éxito silencioso que instalaría una credencial vacía.

### Diálogo de Información del Paquete

El diálogo de información del paquete muestra las notas como una lista etiquetada. Las notas se renderizan según su tipo: las notas de URL son hipervínculos que se abren en una pestaña nueva (`target="_blank"`), las notas de correo electrónico son enlaces `mailto:` que abren el cliente de correo del usuario y las notas de teléfono son enlaces `tel:`. Las notas sin tipo se renderizan como bloques de código simples, sin enlaces.

### API del Manifiesto de Paquete

`POST /packages/manifest` (requiere autenticación) devuelve la definición YAML cruda de un paquete. Acepta repositorio, nombre y versión. Devuelve el contenido del archivo con `Content-Type: text/x-yaml; charset=utf-8`. Devuelve 404 si el archivo del paquete no existe.

### Menú Desplegable de Acciones del Paquete

En la interfaz de la lista de paquetes, cada renglón de paquete tiene un menú desplegable `...` (tanto en la vista plana como en la agrupada por repositorio). El desplegable trae:

- **Info** (solo paquetes instalados) -- abre el diálogo de información del paquete con las preguntas, las respuestas y las notas compiladas.
- **Manifiesto** -- abre un diálogo que muestra la definición YAML cruda del paquete con un botón de copiar.
- **Versión/Repositorio** -- se muestra como un elemento deshabilitado con la versión y el nombre del repositorio.
- **Desinstalar** (solo paquetes instalados) -- dispara el diálogo de confirmación de desinstalación.

### Paquetes Destacados

Cada repositorio puede incluir un archivo `featured.json` con un arreglo JSON de nombres de paquetes. Los carga `LoadFeatured` y se devuelven junto con la lista de paquetes en `RepoPackageGroup`. La API de lista plana de paquetes define un booleano `featured` en cada entrada. La API de lista agrupada conserva el arreglo `Featured` de cada grupo incluso cuando el filtrado por búsqueda reduce la lista de paquetes.

- `GET /packages` (requiere autenticación) -- lista paquetes con búsqueda, ordenamiento, paginación y filtros opcionales `featured_only` e `installed_only`.
- `GET /packages/featured` (requiere autenticación) -- lista los paquetes destacados de todos los repositorios.
- `GET /packages/by-repo` (requiere autenticación) -- lista los paquetes agrupados por repositorio. Acepta los parámetros de consulta `search` y `featured_only`.

#### Filtro de Paquetes Destacados

La API de lista plana de paquetes (`GET /packages`) y la API de lista agrupada (`GET /packages/by-repo`) aceptan un parámetro de consulta `featured_only`. Cuando vale `"true"`, solo se devuelven los paquetes marcados como destacados. El filtro se cruza con `installed_only` -- los dos pueden estar activos al mismo tiempo. En la interfaz, una casilla "Featured only" prende el filtro. El estado predeterminado del filtro de destacados es `true` (mostrando solo los paquetes destacados en la primera visita). Las preferencias de filtro (`pkg_group_by_repo`, `pkg_installed_only`, `pkg_featured_only`) se guardan en `localStorage`.

### Filtro de Paquetes Instalados

La API de lista plana de paquetes (`GET /packages`) acepta un parámetro de consulta `installed_only`. Cuando vale `"true"`, solo se devuelven los paquetes instalados. El filtrado se aplica en el servidor antes de la búsqueda, el ordenamiento y la paginación, garantizando conteos de páginas y desplazamientos correctos. En la interfaz, una casilla "Installed only" prende el filtro y reinicia la paginación a la primera página.

### Instalación y Desinstalación de Paquetes

#### API de Instalación

`POST /packages/install` (requiere admin) instala un paquete. Acepta repositorio, nombre, versión, respuestas y flags opcionales:

- `reuse_volumes` -- reutiliza los volúmenes de una versión desinstalada anterior.
- `import_from_version` -- importa los volúmenes de una versión anterior específica.
- `skip_response_reuse` -- no autocompletar las respuestas de instalaciones anteriores.

La instalación crea un enlace duro del archivo de paquete del repositorio al directorio de instalados, persiste las respuestas, crea los volúmenes con cuotas y UID/GID opcionales, siembra los volúmenes desde archivos y git (solo runtime de contenedor), aplica las plantillas de archivo, genera los archivos de unidad de systemd, escribe los archivos de estado de red, instala y arranca las unidades de systemd, y borra las últimas respuestas si todo sale bien. Las últimas respuestas se guardan antes de instalar para poder recuperarlas en la desinstalación. En los paquetes de VM, la imagen de disco de la VM se baja y se convierte a formato raw (si es una URL remota) antes de generar las unidades; la siembra de volúmenes (archivos, clones git) se omite.

#### API de Desinstalación

`POST /packages/uninstall` (requiere admin) desinstala un paquete. Acepta repositorio, nombre, versión y flags opcionales:

- `purge_volumes` -- elimina de inmediato todos los volúmenes asociados.

Cuando no se purga, los volúmenes se mueven del prefijo `installed/` al prefijo `uninstalled/`. El archivo de estado de red se elimina y las unidades de systemd se detienen, se deshabilitan y se desinstalan.

**Cascada de dependencias.** Desinstalar un paquete padre desinstala recursivamente todas las dependencias que le pertenecen. La cascada lee los registros de dependencias persistidos (`LoadDependencies`) del padre y recorre cada hijo en profundidad, repitiendo la búsqueda en cada nivel para que las subdependencias anidadas (`padre--dep--hijo--dep--nieto`) también se eliminen. Para cada dependencia, la cascada desregistra sus registros DNS, desinstala sus unidades de systemd (servicio + NC + sockets), elimina su archivo de estado de red, llama a `inst.Uninstall` para tirar el registro de instalación y, o bien purga sus volúmenes (cuando `purge_volumes` está definido), o bien los mueve al prefijo `uninstalled/`. La cascada está implementada en `uninstallDependencies` (`src/svc/systemcontroller/controller_install_dependencies.go`) y corre después de que se completa la desinstalación del propio padre. No hay conteo de referencias: cada dependencia le pertenece exactamente a un padre (su registro de instalación vive en `installed/<repo>/<padre--dep--llave>/`), así que una dependencia compartida instalada bajo dos padres tiene dos registros independientes, y desinstalar un padre solo elimina su propia copia.

#### Información del Paquete Instalado

`POST /packages/installed/info` (requiere autenticación) devuelve las preguntas, las respuestas, las notas compiladas y los tipos de nota de un paquete instalado.

**Una cuenta que no es de administrador obtiene las notas y nada más.** La ruta se queda como `requireAuth` porque el tablero renderiza las notas de todos los servicios instalados para todas las cuentas — para eso son las notas — pero una pregunta `type: secret` se responde con una credencial generada y una `type: oauth` con un token de proveedor, así que devolverle el mapa completo de respuestas a cualquiera que tenga un inicio de sesión le entregaría las credenciales de todos los paquetes. Las preguntas también se retienen: el `query` de una pregunta es inofensivo, pero emparejarlo con un mapa de respuestas censurado nada más anuncia lo que se está guardando, y la única pantalla que renderiza preguntas es el diálogo de instalación, exclusivo de administradores. Tirar el mapa no basta por sí solo — una nota se compila a partir de esas mismas respuestas, así que `redactSecretsInNotes` enmascara cualquier respuesta de tipo secreto u oauth que una nota haya citado, haciendo coincidir por valor para que una nota que nunca cita ninguna quede completamente intacta. Las respuestas de menos de seis caracteres se dejan en paz: un secreto de dos caracteres no es una credencial que alguien haya escogido, y enmascarar todas sus apariciones haría trizas texto de notas que no tiene nada que ver.

#### Versiones de Paquete

`POST /packages/versions` (requiere autenticación) lista las versiones disponibles de un paquete por nombre.

#### Preguntas del Paquete

Dos endpoints recuperan las preguntas de un paquete:

- `POST /packages/questions` (requiere admin) -- obtiene las preguntas por nombre de paquete (última versión).
- `POST /packages/questions/identity` (requiere admin) -- obtiene las preguntas por repositorio, nombre y versión.

### Manejo de Zonas Horarias

La interfaz mantiene una copia estática de los nombres de zona horaria IANA más comunes con una utilidad `getTimezoneOffsetMinutes()` que calcula los desplazamientos UTC en el cliente con la API `Intl` del navegador. El servidor expone el desplazamiento UTC del sistema local en minutos a través de la respuesta del ping de estado.

### Vista Previa de Instalación

- `POST /packages/install-preview` (requiere autenticación) -- muestra una vista previa de lo que se crearía si se instalara un paquete. Acepta repositorio, nombre y versión. Devuelve repositorio, nombre, versión, descripción, imagen, volúmenes, puertos, información de actualización, tipo de runtime y si el paquete tiene preguntas. En los paquetes de VM, la vista previa también incluye la configuración de la VM (URL de la imagen, memoria legible por humanos y número de CPU).

### Hijos de un Paquete

- `POST /packages/children` (requiere autenticación) -- lista los nombres de los paquetes hijos de un repositorio y un nombre de paquete dados.

### Listado de Volúmenes Desinstalados

- `POST /packages/uninstalled-volumes` (requiere autenticación) -- revisa si un paquete tiene volúmenes sobrantes de una desinstalación anterior. Devuelve si existen volúmenes desinstalados, la lista de versiones desinstaladas y la lista de versiones instaladas.

### Administración de Paquetes Instalados

- `GET /packages/installed` (requiere autenticación) -- lista todos los paquetes instalados con búsqueda, ordenamiento y paginación.
- `POST /packages/responses` (requiere admin) -- obtiene las respuestas guardadas de un paquete instalado por repositorio, nombre y versión.
- `POST /packages/purge-volumes` (requiere admin) -- elimina permanentemente los volúmenes de un paquete instalado.

### Habilitar/Deshabilitar Paquetes

- `POST /packages/disable` (requiere admin) -- deshabilita un paquete. Pone la marca de deshabilitado y detiene todos los servicios de systemd asociados.
- `POST /packages/enable` (requiere admin) -- vuelve a habilitar un paquete deshabilitado. Quita la marca de deshabilitado y arranca todos los servicios de systemd asociados.

La interfaz `Installer` acepta `SetDisabled`, `IsDisabled` e `IsPackageChanged` además de los métodos centrales `Install`, `Uninstall`, `ListInstalled` y `GetResponses`.

### Administración de Volúmenes Desinstalados

- `POST /packages/purge-uninstalled-volumes` (requiere admin) -- elimina permanentemente todos los volúmenes desinstalados de un paquete.

## Almacenamiento

El almacenamiento usa subvolúmenes btrfs con aplicación de cuotas.

### Separación de responsabilidades: volúmenes vs. almacenamiento de objetos

**La capa de almacenamiento administra volúmenes. gfeh provee el almacenamiento de objetos. La capa de almacenamiento no maneja para nada el almacenamiento de objetos -- gfeh es el responsable.**

`src/storage` crea, redimensiona, renombra, toma instantáneas y elimina subvolúmenes btrfs, y reporta el uso de disco. Ese es todo su encargo. Nunca debe aprender qué es un objeto, un bucket, una llave, un manejador de archivo, un identificador de contenido (CID), una ACL, un recurso compartido o una vista de protocolo. Para la capa de almacenamiento, un subvolumen es una arena opaca de bytes con una cuota.

gfeh (`gitea.com/town-os/gfeh`, un servicio de sistema en Rust que se distribuye como `town-os-system--gfeh`) es el dueño de todo lo que está arriba de esa línea: el espacio de nombres de objetos, los metadatos y permisos por archivo, la base de datos jerárquica de usuarios/ACL, el compartir, la exposición HTTP por archivo, la federación con servicios externos y todas las vistas de protocolo (S3, IPFS, Google Drive, HTTP simple; SMB/CIFS existe en gfehd pero [Town OS no lo sirve](#sin-vista-smb)). Consume la capa de almacenamiento únicamente para aprovisionar y redimensionar los subvolúmenes donde viven sus particiones, y luego hace su propia E/S directa sobre el subárbol montado por bind.

Consecuencias que hay que respetar al cambiar cualquiera de los dos lados:

- **No** agregues endpoints de objetos, blobs, llave/valor o por archivo a `src/storage` ni a la API `/storage/*`. Si una funcionalidad necesita direccionar archivos individuales, le toca a gfeh. Los endpoints existentes `upload-archive`/`download-archive` son un transporte tar para sembrar volúmenes, no una API de objetos, y no deben crecer en esa dirección.
- **No** le enseñes a `storage.Storage` ni a `storage.Controller` qué son los usuarios, los permisos o los protocolos. La cuota es la única política que aplica la capa de almacenamiento.
- Las particiones de gfeh viven bajo el prefijo de subvolumen reservado `gfeh/`. Se aprovisionan con `CreateFilesystem`/`ModifyFilesystem` de `storage.Storage` **en proceso**, no a través de la API HTTP `/storage/*`: `createFilesystem` reescribe incondicionalmente todos los nombres enviados a `user/<nombre>` (`controller_storage.go`), así que esa ruta no puede producir un volumen bajo ningún otro prefijo. Por eso el aprovisionamiento de particiones necesita sus propios manejadores `/gfeh/partitions/*`, lo cual además deja en un solo lugar la aplicación de prefijos reservados, la política de cuotas y el registro de auditoría, en vez de duplicarlos en gfeh.

- **gfeh depende de un contrato escrito, y los cambios aquí lo pueden romper.** `TOWNOS_CONTRACT.md`, en el repositorio de gfeh, enlista todas las rutas, comportamientos e invariantes que gfeh espera de Town OS -- la reescritura a `user/`, las reglas de prefijos reservados, los códigos de estado de `/gfeh/partitions/*`, las fallas de autenticación indistinguibles y el significado de "falla en cerrado" de un `Account.Networks` vacío -- y fija la revisión de Town OS contra la que se verificó. gfeh emula ese contrato para que sus pruebas puedan correr sin root, systemd, podman ni btrfs.

  **Al cambiar `src/storage`, `src/account` o las rutas del controlador del sistema, vuelve a correr `make check-townos-sync` en el checkout de gfeh.** Un emulador desviado le da a gfeh una suite de pruebas en verde y un despliegue roto. Reconcilia el emulador y el documento del contrato juntos; nunca uno sin el otro.

El lado de Town OS de esa integración — las rutas de particiones, los demonios por red, el socket de administración y cómo llegan los nombres al DNS y al ingress — es [Almacenamiento de Objetos (gfeh)](#almacenamiento-de-objetos-gfeh).

### Operaciones del Sistema de Archivos

La interfaz `Storage` provee:

- **CreateFilesystem** -- crea un subvolumen btrfs nuevo con cuota opcional.
- **ModifyFilesystem** -- cambia el nombre y/o la cuota de un volumen.
- **RemoveFilesystem** -- elimina un volumen.
- **ListFilesystems** -- lista volúmenes con filtrado por prefijo y estado (`user`, `installed`, `uninstalled`), ordenamiento, paginación y búsqueda. Devuelve una lista vacía (no un error) cuando no encuentra el montaje btrfs.
- **RenameFilesystem** -- renombra un volumen.
- **SnapshotFilesystem** -- crea una instantánea btrfs.
- **DiskUsage** -- reporta las estadísticas de uso de disco.

Las cuotas se aplican a nivel de qgroup de btrfs. Una cuota de 0 significa ilimitada.

### API de Almacenamiento

- `POST /storage/create` (requiere autenticación) -- crea un sistema de archivos de usuario nuevo con nombre y cuota opcional.
- `POST /storage` (requiere autenticación) -- lista los sistemas de archivos con filtrado por prefijo y estado, ordenamiento, paginación y búsqueda.
- `POST /storage/modify` (requiere autenticación) -- modifica el nombre y/o la cuota de un volumen. Renombrar solo se permite en los sistemas de archivos de usuario; los volúmenes de paquete no se pueden renombrar.
- `POST /storage/remove` (requiere autenticación) -- elimina un sistema de archivos de usuario.
- `POST /storage/package-volumes` (requiere autenticación) -- lista los volúmenes de paquete agrupados por paquete, con inclusión opcional de los volúmenes desinstalados.
- `POST /storage/remove-package-volume` (requiere admin) -- elimina un volumen de paquete específico por su nombre interno.
- `POST /storage/remove-package-volume-group` (requiere admin) -- borrado en cascada detrás de los botones de borrado de los nodos que no son hoja del árbol de almacenamiento. `repo` y `name` son obligatorios; un `version` vacío apunta a todas las versiones instaladas del paquete. **Todas las unidades de systemd del árbol de dependencias del paquete objetivo se detienen antes de eliminar cualquier subvolumen**, así que un contenedor podman que todavía tenga un volumen abierto no puede competir con el borrado de btrfs. `include_uninstalled` barre además el subárbol `uninstalled/` que corresponda (conectado al mismo interruptor "Mostrar desinstalados" que maneja el listado de volúmenes).
- `POST /storage/upload-archive` (requiere admin) -- sube y desempaqueta un archivo comprimido en un volumen.
- `POST /storage/download-archive` (requiere admin) -- baja un volumen como archivo comprimido.

### Espacios de Nombres de Volúmenes

- **Volúmenes de usuario** -- `user/<nombre>` en disco. El prefijo `user/` lo ponen de forma transparente los manejadores de creación, eliminación, modificación y listado, y se quita en las respuestas de la API para que quien consume la API vea solo el nombre pelón. El subvolumen raíz `user` lo crea la reconciliación al arrancar.
- **Volúmenes de paquetes instalados** -- `installed/<repo>/<nombre>/<versión>/<nombrevol>`.
- **Volúmenes de paquetes desinstalados** -- `uninstalled/<repo>/<nombre>/<versión>/<nombrevol>`.
- **Almacenamiento de archivos comprimidos** -- prefijo `archives/` (lo administra el sistema).
- **Imágenes de VM** -- subvolumen `vm-images/` (lo administra el sistema). Guarda las imágenes de disco raw de VM en caché.
- **Particiones de almacenamiento de objetos** -- `gfeh/<red>`, una por red de Town OS, propiedad del uid/gid 2000. Reservadas: `/storage/create` no puede producir una (reescribe todos los nombres a `user/<nombre>`), así que se aprovisionan a través de [`/gfeh/partitions/*`](#protocolo-1-aprovisionamiento-de-particiones-gfehpartitions).

Todos los nombres de raíz de prefijo (`installed`, `uninstalled`, `archives`, `pages`, `vm-images`, `user`, `gfeh`) están reservados y los usuarios no los pueden crear, modificar ni eliminar directamente. La subida y la bajada de archivos comprimidos resuelven los nombres de subvolumen que no traen un prefijo interno poniéndoles `user/` adelante.

**Un prefijo no es una frontera a menos que el nombre que va después no se pueda salir trepando.** `filepath.Join` colapsa `..`, así que `../gfeh/home` enviado a un manejador que pone `user/` adelante se convierte en `user/../gfeh/home` y direcciona la partición de almacenamiento de objetos de otra red — y además se cuela por delante de la revisión de nombres reservados, que compara contra un prefijo inicial que el recorrido todavía no trae. Por eso `storage.ValidateFilesystemName` (sin diagonal inicial, sin bytes nulos, sin componentes vacíos ni `.`/`..`, y con un juego de caracteres restringido) se aplica a **los dos** nombres en `ModifyFilesystem` — validar solo el destino del renombrado dejaba que quien llamara moviera el subvolumen de alguien más a su propio espacio de nombres — y en `RemoveFilesystem`, que no validaba nada y es la operación destructiva. Los manejadores de `/storage/*` validan el nombre enviado **antes** de ponerle `user/` adelante, que es lo que hace que la revisión de nombres reservados signifique lo que parece decir. Estas rutas son `requireAuth`, no `requireAdmin`, así que esto lo podía alcanzar cualquier cuenta común del equipo.

El prefijo del **listado** está exento a propósito: `nest/` es como quien llama pide todo lo que hay bajo `nest`, nada lo une a una ruta del sistema de archivos (la capa de almacenamiento lista desde su propia base y lo usa como filtro de cadena) y `user/` se pone adelante incondicionalmente, así que un prefijo con recorrido no coincide con nada en vez de alcanzar algo.

### Detección del Formato de Archivo Comprimido

El formato de compresión del archivo se detecta inspeccionando los bytes mágicos al inicio del flujo de subida. Se asoman los primeros 6 bytes con un lector con búfer y se comparan contra las firmas conocidas:

- **gzip** -- `0x1f 0x8b`
- **bzip2** -- `0x42 0x5a 0x68` (`BZh`)
- **xz** -- `0xfd 0x37 0x7a 0x58 0x5a 0x00` (`\xfd7zXZ\x00`)

Las firmas que no se reconocen se rechazan de inmediato. La extensión del nombre de archivo también se valida por separado para confirmar el formato.

### Validación del Flujo del Archivo Comprimido

Después de detectar el formato, el flujo descomprimido se valida como archivo tar usando `io.TeeReader`. Un lado del tee alimenta al lector `archive/tar` de Go para validar los encabezados tar; el otro lado alimenta al proceso real de desempaquetado `tar -xf`. Si la validación detecta un flujo tar inválido, el desempaquetado se interrumpe. La descompresión usa implementaciones paralelas donde están disponibles: `pigz` para gzip, `lbzip2` para bzip2 y `xz` para xz.

### Subida de Archivos Comprimidos

`POST /storage/upload-archive` (requiere admin) acepta un formulario multipart:

- `subvolume` (obligatorio) -- ruta del subvolumen destino.
- `archive` (obligatorio) -- archivo comprimido. Formatos aceptados: `.tar`, `.tar.gz`/`.tgz`, `.tar.bz2`/`.tbz2`, `.tar.xz`/`.txz`.
- `subpath` (opcional) -- ruta relativa dentro del volumen para el desempaquetado; se crea bajo demanda.
- `stop_service` (opcional) -- nombre de la unidad de systemd que hay que detener antes de desempaquetar y reiniciar al terminar.

Los archivos se transmiten directamente sin archivos temporales. El recorrido de rutas se valida después de desempaquetar (resolución de enlaces simbólicos). El tamaño máximo de subida es de 1 GB por omisión (ajuste `max_archive_size`). El tiempo límite de desempaquetado es de 600 segundos por omisión (ajuste `archive_unpack_timeout`).

### Bajada de Archivos Comprimidos

`POST /storage/download-archive` (requiere admin) acepta un cuerpo JSON:

- `subvolume` (obligatorio) -- ruta del subvolumen origen.
- `paths` (opcional) -- arreglo de rutas específicas dentro del subvolumen que hay que incluir.
- `stop_service` (opcional) -- nombre de la unidad de systemd que hay que detener durante el archivado y reiniciar después.
- `format` (opcional) -- formato de compresión: `tar.gz` (predeterminado), `tar.bz2` o `tar.xz`.
- `filename` (opcional) -- nombre base personalizado para el archivo bajado. El servidor limpia el valor (quita separadores de ruta y caracteres de control), quita cualquier extensión de archivo comprimido que ya traiga para no duplicarla y agrega la extensión que corresponde al formato elegido. Por omisión es `download` cuando no se da o cuando la limpieza produce una cadena vacía.

Devuelve un archivo comprimido transmitido en el formato solicitado. La compresión usa `pigz`, `lbzip2` o `xz` respectivamente. Los encabezados Content-Type y el nombre de archivo de Content-Disposition se ponen para que coincidan con el formato elegido y el nombre personalizado. Cuando se da `paths`, solo se incluyen las rutas que coinciden.

### Autoarchivado desde Imágenes de Contenedor

Las definiciones de paquete pueden incluir una sección `archives` que referencia imágenes de contenedor. Durante la instalación y la reconciliación, los volúmenes vacíos se pueblan bajando la imagen, creando un contenedor temporal y copiando el directorio indicado al volumen.

### Siembra de Volúmenes desde Git

Los volúmenes pueden especificar un campo `git` con la URL de un repositorio. Durante la instalación y la reconciliación, los volúmenes vacíos se siembran clonando el repositorio (tiempo límite de 5 minutos). La URL puede referenciar variables de plantilla, lo que deja a los usuarios sobrescribir el repositorio con la respuesta a una pregunta. Los datos existentes nunca se sobrescriben. Las fallas de clonado se registran y se omiten (no fatales).

### Reconstrucción del Origen Git

`POST /packages/rebuild-git` (requiere admin) actualiza los volúmenes sembrados desde git de un paquete instalado. Trae los últimos cambios de cada volumen git con go-git y luego reinicia el servicio dependiente. Requiere el repositorio, el nombre y la versión del paquete. Las variables de plantilla se vuelven a evaluar contra las respuestas guardadas antes de reconstruir.

### Administración de Imágenes de VM

Los paquetes de VM requieren imágenes de disco en formato raw. Las imágenes remotas se bajan y se convierten con `qemu-img convert -O raw`; el archivo `.raw` convertido se guarda en caché en el subvolumen `vm-images`. Las instalaciones posteriores reutilizan la imagen en caché. Las referencias a imágenes locales se resuelven directamente desde el subvolumen `vm-images`.

- `GET /vm-images` (requiere autenticación) -- lista las imágenes de disco de VM en caché. Devuelve el nombre y el tamaño de archivo de cada imagen.
- `POST /vm-images/upload` (requiere admin) -- baja una imagen de VM desde una URL y la convierte a formato raw. Acepta una URL y un nombre opcional. Por omisión, el nombre es el del archivo de la URL con extensión `.raw`. Las descargas tienen un tiempo límite de 30 minutos. La imagen convertida se guarda en el subvolumen `vm-images`.
- `POST /vm-images/delete` (requiere admin) -- elimina una imagen de VM en caché por nombre.

### Recorte del Nombre a Mostrar

Las respuestas de la API para los volúmenes de paquetes instalados y desinstalados recortan el segmento inicial del repositorio de la ruta (p. ej., `default/nginx/2.0/data` pasa a ser `nginx/2.0/data`). La ruta completa en disco se conserva en un campo `internal_name` para las operaciones que la necesitan (p. ej., derivar el nombre del servicio de systemd para detener/arrancar durante las operaciones con archivos comprimidos).

### Interfaz de Almacenamiento

La pantalla de administración del almacenamiento tiene dos secciones:

**Sistemas de archivos de usuario** -- una tabla de datos paginada, ordenable y con búsqueda. Cada renglón tiene botones de Modificar (nombre y cuota) y Eliminar. El diálogo de creación precarga el campo de cuota a partir del ajuste `default_quota` del sistema.

**Volúmenes de paquete** -- un árbol jerárquico organizado por paquete. Cada paquete es un encabezado de árbol plegable que muestra: el conteo total de volúmenes, el conteo de versiones, la cuota agregada y las insignias de estado de instalación. Cuando un paquete tiene varias versiones, se muestran subencabezados por versión con la cuota y el estado de cada una. Los volúmenes desinstalados se incluyen cuando el interruptor "Mostrar volúmenes desinstalados" está activo.

Cada renglón de volumen hoja muestra la cuota y el estado, y ofrece tres acciones:

- **Bajar** (botón de icono) -- abre un diálogo con un campo opcional de nombre de archivo (nombre base del archivo bajado; la extensión se agrega automáticamente), un selector de formato de compresión (gzip, bzip2, xz), un filtro opcional de rutas separadas por comas y una casilla para detener el servicio dependiente durante la bajada. Usa la File System Access API para guardado en streaming, con un respaldo de bajada por blob.
- **Subir** (botón de icono) -- abre un diálogo para escoger un archivo comprimido (`.tar`, `.tar.gz`, `.tgz`, `.tar.bz2`, `.tbz2`, `.tar.xz`, `.txz`) con una subruta opcional para la extracción y una casilla para detener el servicio dependiente durante la subida.
- **Modificar** (botón) -- abre un diálogo que muestra el nombre del volumen, el estado y el nombre del servicio asociado, con un campo para cambiar la cuota. El campo de nombre no es editable para los volúmenes de paquete.

## Pages

Pages es una funcionalidad de alojamiento de sitios estáticos que acepta tres tipos de origen de contenido: subidas de archivos comprimidos, imágenes de contenedor y repositorios git. Los usuarios asignan un dominio o subdominio, y el sistema sirve el contenido con un contenedor Caddy. Las actualizaciones se disparan a mano con reconstrucción o volviendo a subir el contenido.

### Modelo de Datos

Cada sitio de pages tiene: un nombre único (llave primaria), un tipo de origen (`archive`, `container_image` o `git`; predeterminado: `archive`), la URL del repositorio (obligatoria para git), la rama (por omisión `main`), la referencia a la imagen de contenedor (obligatoria para container_image), el directorio de la imagen (obligatorio para container_image), el dominio (por omisión el nombre de la página), el estado (`pending`, `active` o `error`), una **red** y marcas de tiempo de creación/actualización. Las páginas se guardan en una tabla SQLite.

`Network` es la red de publicación de la página, igualito que la red de instalación de un paquete: selecciona el TLD bajo el que se nombran el nombre de host de la página, el SAN de la hoja, el propietario DANE TLSA y el vhost del ingress, y decide quién puede resolver la página. Vacía — el valor cero y el predeterminado de la base de datos — significa la red predeterminada/del hogar, la misma convención que `Installer.LoadNetwork` para los paquetes. Ve [Las páginas también están acotadas por red](#las-páginas-también-están-acotadas-por-red). Se acepta al crear y es uno de los campos de actualización parcial.

El contenido de pages se guarda en subvolúmenes btrfs bajo un prefijo `pages/`. Cada página obtiene un subvolumen en `pages/{nombre}` y un enlace simbólico en `pages-webroot/{nombre}` que apunta a `/data/pages/{nombre}`. El prefijo `pages` está reservado y no se puede renombrar ni eliminar con la API general de almacenamiento.

### API de Pages

Todos los endpoints de mutación requieren autenticación de administrador; el endpoint de listado requiere autenticación normal.

- `POST /pages/create` (requiere admin) -- crea una página nueva. Acepta nombre, tipo de origen, URL del repositorio, rama, dominio, imagen de contenedor y directorio de la imagen. El tipo de origen es `archive` por omisión. La validación varía según el tipo de origen: git requiere la URL del repositorio; la imagen de contenedor requiere tanto la imagen como el directorio de la imagen. Crea un subvolumen btrfs y el enlace simbólico del webroot. Las páginas de git y de imagen de contenedor se aprovisionan de forma asíncrona (clonado o extracción de la imagen); el estado pasa de `pending` a `active` si todo sale bien, o a `error` si falla. Las páginas de archivo se quedan en estado `pending` hasta que se sube contenido con `/pages/upload`. Si no se da dominio, se usa el nombre de la página.
- `POST /pages/upload` (requiere admin) -- sube contenido para una página de tipo archivo. Acepta un formulario multipart con `name` y el archivo `archive`. Solo vale para páginas con tipo de origen `archive`; devuelve 400 para los demás tipos. Usa la misma detección de formato por bytes mágicos, validación de extensión y validación de flujo que las subidas de archivos de almacenamiento. Desempaqueta directamente en el subvolumen btrfs de la página. Pone el estado en `active` si sale bien o en `error` si falla.
- `POST /pages/update` (requiere admin) -- actualización parcial de la URL del repositorio, la rama, el dominio, el tipo de origen, la imagen de contenedor o el directorio de la imagen de una página. Solo se cambian los campos que se dan.
- `POST /pages/remove` (requiere admin) -- elimina una página de la base de datos, quita el enlace simbólico del webroot y elimina el subvolumen btrfs.
- `POST /pages/rebuild` (requiere admin) -- el comportamiento varía según el tipo de origen: las páginas de git traen los últimos cambios (o hacen un clonado nuevo si falta `.git`); las páginas de imagen de contenedor se vuelven a extraer de la imagen con podman; las páginas de archivo devuelven 400 (hay que volver a subir con `/pages/upload`).
- `GET /pages` (requiere autenticación) -- lista todas las páginas con ordenamiento, búsqueda y paginación. Ordenable por nombre, URL del repositorio, rama, dominio, tipo de origen, estado y marcas de tiempo.

### Interfaz de Pages

La pantalla de administración de pages muestra una tabla de datos paginada, ordenable y con búsqueda, con columnas para el nombre, el dominio, el tipo de origen, la URL del repositorio, la rama y el estado. El tipo de origen se muestra como insignia. El estado se muestra como una insignia con código de color (predeterminada para activa, roja para error, secundaria con un icono de carga girando y el texto "Provisioning..." para pendiente).

El diálogo de creación tiene una lista desplegable de tipo de origen hasta arriba (Archive Upload / Container Image / Git Repository, predeterminado: Archive Upload). Los campos cambian dinámicamente según el tipo de origen que se escoja: git muestra la URL del repositorio y la rama; la imagen de contenedor muestra la referencia a la imagen y el directorio de la imagen; el archivo muestra una entrada opcional de subida de archivo. En las páginas de git y de imagen de contenedor, enviar el formulario dispara el aprovisionamiento: todas las entradas se deshabilitan, el botón de envío muestra un indicador de carga con el texto "Provisioning..." y el diálogo no se puede cerrar. La interfaz consulta el estado de la página cada 2 segundos hasta por 60 segundos. En las páginas de archivo con un archivo escogido, la subida pasa de forma síncrona después de crearla.

Las acciones por renglón varían según el tipo de origen: las páginas de archivo muestran un botón de Subir; las de git y las de imagen de contenedor muestran un botón de Reconstruir (con confirmación). Todas las páginas tienen acciones de Editar y Eliminar. El diálogo de edición muestra los campos que corresponden al tipo de origen de la página.

## Almacenamiento de Objetos (gfeh)

gfeh es la mitad de almacenamiento de objetos de la división que describe [Separación de responsabilidades](#separación-de-responsabilidades-volúmenes-vs-almacenamiento-de-objetos): `src/storage` es dueño de los subvolúmenes btrfs y las cuotas, gfeh es dueño de los objetos, los permisos por archivo, el bosque de usuarios/ACL, el compartir y todas las vistas de protocolo. Esta sección es el lado de Town OS de esa frontera — cómo se despliegan los demonios y todos los protocolos que la cruzan.

`gfehd` es un binario en Rust publicado en crates.io y empaquetado aquí como `quay.io/town/gfeh` (`Containerfile.gfeh`), porque el propio repositorio de gfeh no distribuye ninguna imagen. Es **un proceso por partición**, no un solo demonio multiinquilino.

### Forma del despliegue: una partición por red

Una **partición** es un subvolumen btrfs, un proceso `gfehd`, un socket de administración y **su propio conjunto de usuarios**. Hay exactamente una por red de Town OS, así que el espacio de nombres del almacenamiento de objetos queda dividido por la misma frontera que divide el DNS y WireGuard: un principal, un permiso otorgado y una exposición en la partición `office` no significan nada en `home`.

| Cosa | Ubicación |
|---|---|
| Datos de la partición | `<btrfsBase>/gfeh/<red>` → contenedor `/data/<red>` |
| Configuración | `<btrfsBase>/gfeh-control/<red>/gfehd.yaml` → `/etc/gfeh/gfehd.yaml` |
| Socket de administración | `<btrfsBase>/gfeh-control/<red>/run/admin.sock` → `/run/gfeh/admin.sock` |
| Unidad | `town-os-system--gfeh-<red>.service` |

Los ayudantes de rutas viven en `src/gfeh/layout.go` — `PartitionVolume`, `ConfigPath`, `SocketPath`, `ServiceKey`, `NetworkFromKey` — y son el único lugar donde se componen estas cadenas.

El socket se pone en el btrfs porque es el único sistema de archivos que pueden ver tanto el contenedor de gfehd como el del systemcontroller; es el mismo truco que usa `ingressctl` para su socket gRPC. gfehd corre como **uid/gid 2000** (`gfeh.UID`/`gfeh.GID`), y un bind mount pasa la propiedad del host tal cual, así que al subvolumen de la partición se le hace chown a ese uid al crearlo — por eso `storage.Filesystem` trae `UID`/`GID` opcionales y `storage.Controller` tiene `Chown`. No recursivo, por la misma razón que el chown de `HostVolumeMount`: el demonio crea sus propios hijos con su propio uid, así que ya quedan con la propiedad correcta y nunca se desvían.

**Puertos.** Las cuatro vistas HTTP enlazan **puertos de contenedor fijos e idénticos en todas las particiones** — s3 9000, http 9001, drive 9002, ipfs 9003 — y **no publican ningún puerto del host**. Eso es seguro justo porque no publican ninguno: cada contenedor tiene su propio netns y el ingress lo alcanza por el nombre del contenedor, igualito que alcanza a un paquete. Dos particiones que sirvan S3 en el 9000 no pueden chocar, ni siquiera bajo un `make test-full` concurrente.

**Ninguna partición publica ningún puerto del host**, porque SMB — la única vista que necesitaría uno, al no ser HTTP ni poder ponerse detrás del ingress — [no se sirve](#sin-vista-smb). `DefaultSMBPortBase` (`4450`) y `GFEH_SMB_PORT_BASE` sobreviven sin usarse, así que el ajuste del arnés queda inofensivo por si la vista regresa algún día.

### Protocolo 1: aprovisionamiento de particiones (`/gfeh/partitions/*`)

Estas cuatro rutas existen porque `createFilesystem` reescribe incondicionalmente todos los nombres enviados a `user/<nombre>`, así que `/storage/create` **no puede** producir un volumen bajo el prefijo `gfeh/`. Están declaradas en `TOWNOS_CONTRACT.md` y el cliente Rust de gfeh analiza exactamente estas formas, así que **un cambio aquí es un cambio de contrato, no una refactorización**. `make check-townos-sync` en el checkout de gfeh es lo que detecta la deriva; `controller_gfeh_partitions_test.go` fija las formas del cable de este lado.

| Ruta | Autenticación | Petición | Respuesta |
|---|---|---|---|
| `POST /gfeh/partitions/create` | admin | `{name, quota}`, nombre **sin** prefijo | `Filesystem` `{name:"gfeh/<n>", quota}` |
| `POST /gfeh/partitions/modify` | admin | `{name, quota}` | `Filesystem` |
| `POST /gfeh/partitions/remove` | admin | `{name}` | 200, vacío |
| `POST /gfeh/partitions` | autenticación | sin cuerpo | **arreglo JSON plano** de `Filesystem` |

Dos detalles cargan peso:

- **El listado devuelve un arreglo pelón, no un `PageResult`.** Todos los demás endpoints de listado de Town OS paginan; este no puede, porque el `list_partitions()` de gfeh deserializa `Vec<Filesystem>` directamente y un envoltorio paginado no decodifica del lado de Rust.
- **El prefijo es asimétrico.** Las peticiones traen un nombre pelón, las respuestas traen `gfeh/<nombre>`. El prefijo es un artefacto del espacio de nombres de Town OS, no parte de la identidad de la partición; el `Partition::from_volume` de gfeh se lo quita de regreso.

Códigos de estado sobre los que ramifica el cliente de gfeh: **409** ya existe (su aprovisionamiento es un crear-o-redimensionar y distingue los dos por este estado — un demonio cuya partición existe en todos los arranques menos el primero solo arrancaría una vez), **404** no existe, **400** nombre incorrecto, **403** no es admin. Un nombre que traiga un separador de rutas se rechaza en esta frontera porque gfehd lo rechaza en la suya; no ponerse de acuerdo sobre qué es un nombre de partición legal dejaría que `../user/algo` direccionara un volumen fuera de la raíz del almacenamiento de objetos.

Los manejadores llaman a `storage.Storage` en proceso, nunca a `/storage/*`, así que la aplicación de prefijos reservados, la política de cuotas y el registro de auditoría se quedan en un solo lugar. Estas rutas **no** están en `grantRoutes` — aprovisionar la raíz de un árbol de permisos no es algo que compre un permiso otorgado, así que a una cuenta con permisos otorgados se las niega la lista blanca global antes de que corra ningún manejador.

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

Agregar un usuario es `POST /v1/principals {name, parent, ceiling}` — **sin contraseña**, que es la razón de que la interfaz nunca pida una. El techo sigue la regla de proyección de gfeh: `all` para un administrador de Town OS, lectura/escritura en los demás casos. gfehd le pone tope a un permiso otorgado según el techo del principal, así que la interfaz muestra los permisos que *regresaron*, no los que se enviaron: un administrador tiene que poder ver que un permiso se estrechó.

### Protocolo 3: nombres — gfeh contesta, Town OS compone

**gfeh nunca registra un registro DNS ni una ruta de ingress.** `RebuildDNS` llama a `TeardownTLD` y `RebuildIngress` llama a `SetRoutes` con el conjunto derivado completo — los dos destruyen el estado ajeno — así que cualquier cosa que gfeh registrara directamente sobreviviría exactamente hasta la siguiente reconciliación. En cambio, `GET /v1/names` devuelve **etiquetas** (`s3.<partición>`) con una vista y un puerto, y Town OS compone la zona. Por eso los nombres se *piden* en cada reconstrucción en lugar de empujarse una sola vez.

`gfehFQDN(label, tld)` (`gfeh_tls.go`) califica una etiqueta bajo el TLD de la red y es la única cadena en la que tienen que coincidir el registro A, el SAN de la hoja, el propietario DANE TLSA y el vhost del ingress — el mismo invariante que existen para sostener `packageFQDN` y `pageFQDN`. **Siempre** califica: no consulta `isPublicFQDN`, porque toda etiqueta de gfeh ya trae un punto (`s3.gfeh`) y ese predicado lee cualquier nombre así como un FQDN público, lo cual dejaría todos los nombres sin calificar y pediría un certificado ACME para un dominio que nadie tiene.

**Es además el cuello de botella donde una etiqueta deja de ser una cadena en un cable y se vuelve un vhost, un registro DNS y una ruta del sistema de archivos**, así que `gfeh.ValidateLabel` se aplica ahí y en ningún otro lado. Un vhost del ingress se escribe como `https://<hostname> {` sin entrecomillado, así que una etiqueta que traiga un salto de línea y una llave cierra ese bloque y abre otro — y Caddy no rechaza un solo vhost malo, rechaza toda la configuración y tira todos los nombres del equipo. Una etiqueta que no valida produce la cadena vacía, y todos los que llaman ya descartan un FQDN vacío, así que un nombre malformado no aporta ningún registro, ninguna ruta, ningún certificado ni ningún directorio, en vez de aportar uno roto. La longitud (`gfeh.NameMaxLen`) se revisa sobre el nombre **compuesto** y no sobre la etiqueta sola: una etiqueta que cabe en el límite todavía se puede pasar de él al calificarse bajo un TLD largo, y un nombre que el DNS no va a cargar es uno que ni el certificado ni el vhost deberían reclamar.

La publicación coincide exactamente con la de paquetes y páginas:

- **DNS de doble hogar** — la partición de una red que no es la predeterminada obtiene un registro A acotado en la IP de superposición del equipo (que se sirve a los pares WireGuard de esa red) *y* un registro A global en la IP de la LAN, con los plegados de `RebuildDNS` y `RebuildNetworkDNS`. El DANE TLSA se ancla en las dos mitades.
- **TLS** — una hoja de la CA local por nombre, que trae como SAN la IP de superposición del equipo en esa red para que un par pueda marcar por la dirección WireGuard cruda.
- **Ingress** — un vhost por vista HTTP, con backend `<contenedor>:<puerto>` en la red podman compartida `town-os-ingress`. `dedupeIngressRoutes` protege el conjunto de rutas con la regla de que gana el primero, porque Caddy rechaza una configuración entera por un solo vhost duplicado.

`IsHTTPView` controla ese último paso, y una vista **desconocida** se trata como no-HTTP: un vhost para algo que no habla HTTP acepta un handshake TLS y luego falla, lo cual es peor que no tener ruta. (Una vista no-HTTP aportaría un registro DNS y ninguna ruta de ingress; hoy las cuatro vistas que se sirven son HTTP.)

### El índice de la partición

Todas las vistas que sirve gfeh contestan a un **protocolo**, y ninguna le
contesta a un navegador: la vista HTTP tiene exactamente una ruta, `/f/{token}`,
así que su raíz es un 404; S3 devuelve un error XML ante cualquier cosa que no
pueda analizar como operación; Drive e IPFS contestan a sus propias API. Así que
lo único que cualquiera hace con un nombre nuevo — abrirlo — reportaba que el
almacenamiento de objetos estaba roto, cuando en realidad nunca había habido
ningún lugar donde mirar.

Cada partición publica un índice estático en **`gfeh.<tld>`** — `gfeh.IndexLabel`,
que es `VolumePrefix` en lugar de la cadena `"gfeh"` escrita por segunda vez,
porque el índice tiene que aterrizar en el padre de las etiquetas de vista que
indexa. No hay ningún nombre nuevo que aprender: las vistas ya son `s3.gfeh`,
`http.gfeh`, `drive.gfeh`, `ipfs.gfeh`.

- **Lo aporta `collectGfehSites` como un `GfehSite` común y corriente**, y esa es la gracia: hereda los registros A y AAAA, el registro de superposición acotado, el anclaje DANE, el SAN de la hoja y la ruta de ingress del mismo código que deriva los seis para las vistas, así que el vhost y el certificado no se pueden componer a partir de cadenas distintas. Solo se agrega cuando la partición tiene por lo menos una vista por la que el ingress da la cara — un índice para una partición que no sirve nada navegable sería un nombre, un certificado y una ruta, todo para renderizar una página que dice que no hay nada que ver.
- **Lo sirve el contenedor de pages, no gfehd.** El HTML estático no necesita servidor propio, y emitirlo en línea como cuerpo de un `respond` de Caddy metería marcado generado dentro del archivo de configuración, donde un solo error de escapado hace que Caddy rechace todo.
- **El contenido vive bajo su propia raíz `gfeh-index/`**, hermana de `gfeh/` por la misma razón que lo es `gfeh-control/`: todo lo que está bajo `pages/` es una página, propiedad de un renglón y barrida por la reconciliación de pages. El webroot es lo único que los dos comparten, porque es desde donde sirve el contenedor. `ViewIndex` a propósito **no** está en `HTTPViews`, así que `IsHTTPView` no lo acepta — ese predicado contesta "¿es esta una vista que gfehd reportó y por la que el ingress puede dar la cara?", y el índice ni lo reporta gfehd ni lo sirve él.
- **`pruneStalePageSymlinks` pliega `gfehIndexHostnames`.** Un índice no es una página, así que sin esto el primer `reconcilePages` borra todos los enlaces de índice — y un equipo con almacenamiento de objetos y sin páginas se topa con el caso más agresivo de eso en cada pasada. El conjunto válido se deriva **únicamente del conjunto de redes**, nunca preguntándoles a los demonios, así que a una partición que nada más tarda en arrancar no se le puede podar su propio índice: lo que se puede borrar tiene que poder decidirse a partir de estado del que Town OS es dueño.
- **Los índices los renderiza `reconcileGfehIndexes`, desde `RebuildIngress`**, no desde `ReconcileGfeh`. Esa colocación carga peso: la reconstrucción del ingress corre al arrancar, en la reconciliación de cada hora, en el CRUD de paquetes y páginas y, sobre todo, en `publishGfehNames` — la primera pasada en un arranque en frío en la que algún demonio está contestando siquiera, ya que gfehd sondea `/status/ping`, que da 503 hasta el intercambio del manejador. Un índice escrito desde la reconciliación de gfeh se escribiría antes de que los demonios pudieran decir qué sirven, y se quedaría viejo hasta la hora siguiente.

El índice trae **solo las vistas**, que ya están en el DNS. Ni exposiciones, ni
principales, ni permisos otorgados, ni cuota: se sirve sin ninguna autenticación
enfrente, y cada enlace `/f/<token>` publicado es una credencial al portador —
justo lo que una página sin autenticar nunca debe enumerar.

### Protocolo 4: el proxy de la interfaz (`/gfeh/*`)

El socket de administración no está autenticado ni se alcanza por red, así que Town OS le hace de proxy. Están a propósito **separadas de las cuatro rutas del contrato** para que `check-townos-sync` siga coincidiendo exactamente con lo que declara el contrato.

| Ruta | Autenticación |
|---|---|
| `GET /gfeh` | autenticación — particiones con red, TLD, cuota, estado de la unidad y salida de `/v1/names` |
| `GET /gfeh/principals?network=` | autenticación |
| `POST /gfeh/principals/add` / `remove` | `requireObjectStorage` (admin o el permiso `gfeh`) |
| `GET /gfeh/grants?network=&principal=` | autenticación |
| `POST /gfeh/grants/add` / `revoke` | `requireObjectStorage` |
| `GET /gfeh/exposures?network=` | autenticación |
| `POST /gfeh/exposures/withdraw` | `requireObjectStorage` |

Los cuatro `GET` están excluidos de la auditoría; los cinco mutadores traen llaves de auditoría. Sin particiones configuradas, `GET /gfeh` reporta que el almacenamiento de objetos no está configurado en lugar de dar error.

**Todas ellas — lecturas incluidas — están confinadas por `requireNetworkScope` a las redes de quien llama**, porque el "cuál red" vive en el cuerpo o en la consulta que solo el manejador analizó. Una cuenta acotada listando los principales o los enlaces publicados de otra red sería justo la fuga que el alcance existe para evitar, y las lecturas son `requireAuth`, así que nada río arriba lo habría detenido. `GET /gfeh` no nombra ninguna red (las enumera), así que en cambio filtra renglones — con el mismo predicado `Restricted()`, ya que filtrar una cuenta común contra su alcance vacío haría invisibles todas las particiones para todas las cuentas comunes en lugar de confinar a nadie.

**El orden dentro de `gfehClientFor` carga peso: forma, luego autoridad, luego existencia.** Una red vacía es un 400 para todos (un dedazo no es un problema de permisos); una red fuera de alcance es un 403 *antes* de cualquier búsqueda de partición; solo entonces un registro ausente se gana su 503 y una red desconocida su 404. Con la búsqueda primero, quien no tenía por qué preguntar se enteraba de si esa partición existía y de si su demonio estaba en pie, y lo recibía como un rechazo *exitoso* de otro tipo — así que nada dejaba constancia de que una cuenta acotada había alcanzado fuera de su alcance.

### Sin cuentas de servicio

Una versión anterior creaba una cuenta de administrador dedicada, `gfeh`, cuya contraseña se guardaba en el ajuste `gfeh_service_password`, para que el demonio pudiera autenticarse ante el plano de control. **Eso ya no existe.** Town OS aprovisiona por sí mismo el subvolumen y la cuota de cada partición antes de que arranque el demonio, y crea los principales por el socket de administración, así que la credencial no compraba nada — a cambio de costar una *cuenta de administrador habilitada que nadie creó*, sentada en la lista de usuarios de todos los equipos con privilegio de sobra para desinstalar todo, y de obligar a que toda pregunta del tipo "¿este equipo tiene un administrador?" significara "un administrador *humano*".

`hasEnabledAdmin` (`src/svc/systemcontroller/admin_presence.go`) es ahora la pregunta llana, compartida por la bandera de configuración inicial de `/status/ping` y la rama de arranque inicial de `POST /account/create` para que las dos nunca puedan discrepar — un equipo donde una dice "configurado" y la otra no es un equipo al que nadie puede entrar.

`account.PurgeLegacyServiceAccounts` borra el renglón y la contraseña guardada en el primer arranque después de una actualización, reportando si borró algo para que el equipo lo diga una vez en lugar de registrarlo en cada arranque. Es SQL crudo a propósito: `Manager` no tiene `Delete`, y una capacidad de borrado de cuentas no es algo que se introduzca como efecto secundario de una limpieza.

Lo que queda en `gfehd.yaml` es `credentials:` y `drive.tokens:` — **usuarios finales autenticándose ante las vistas de gfeh**, nunca inicios de sesión de Town OS. El bloque `town_os:` sigue existiendo en el esquema de configuración (el YAML de gfehd se refleja exactamente) pero Town OS no renderiza ninguna cuenta adentro.

### Sin vista SMB

SMB **no se sirve**. Es la única vista que no puede ponerse detrás del ingress y la única que necesita una credencial propia: un hash NT (`MD4(UTF16LE(contraseña))`), que no se puede derivar del hash de contraseña guardado, así que todo usuario que quisiera un recurso compartido tenía que cargar con una segunda contraseña. Las cuentas de Town OS no tienen una, así que no hay a quién pudiera autenticar gfehd — y un recurso compartido sin autenticar en la LAN no es el respaldo que hay que tomar.

Consecuencias: ninguna partición declara un bloque `smb:`, no se aparta ningún puerto del host para eso (`SMBPortBase` se conserva nada más para que el `GFEH_SMB_PORT_BASE` del arnés siga conectado), `Account.SMBNTHash` y `src/account/smb_credential.go` ya no existen, y la columna `smb_nt_hash` la elimina `migrateLegacyAccountColumns` — un hash NT no lleva sal, no tiene factor de trabajo y equivale a la contraseña ante cualquier cosa que todavía hable NTLM, así que dejarlo en reposo para una vista que nadie sirve es lo peor de los dos mundos. Las otras cuatro vistas no se ven afectadas.

### Archivo de configuración

`src/gfeh/config.go` refleja el YAML de gfehd **exactamente**. Cada estructura de configuración de gfehd es `#[serde(deny_unknown_fields)]`, así que una llave suelta no se ignora — es una falla dura de arranque. Nivel superior: `data_dir`, `partition`, `network` (un **puntero**: ausente significa la partición predeterminada, y una cadena vacía es una petición distinta e inválida), `admin_socket`, los cinco bloques opcionales de vista, `credentials` y `town_os`. Town OS renderiza cuatro de las cinco vistas y ni un bloque `smb:` ni una cuenta `town_os:`. Se escribe con permisos `0640` y legible por el grupo gid de gfeh bajo `<btrfsBase>/gfeh-control/<red>/`, ya que el demonio corre como uid 2000 y tiene que poder leerlo.

### Arranque y reconciliación

`ReconcileGfeh` corre al arrancar **después del ingress y de pages** y **antes de `Reconcile`** — para entonces la CA TLS y el almacenamiento ya existen, y los nombres tienen que estar disponibles para las llamadas a `RebuildDNS`/`RebuildIngress` de más abajo. Corre **una segunda vez después de `ReconcileNetworks`**, es idempotente (una partición sin cambios se deja en paz en vez de rebotarla) y cubre cualquier red que la reconciliación haya traído a la existencia. También se llama desde `/networks/create`, `/networks/remove`, `/networks/enable` y `/networks/disable`, así que una red que se agregue en tiempo de ejecución obtiene una partición. No fatal en todo momento.

Por cada red asegura el subvolumen (con UID/GID), renderiza la configuración e instala y reinicia la unidad **solo cuando el contenido renderizado cambió** (el mismo modismo de diferencia con `ReadUnit` que ya usa la reconciliación). `pruneGfehPartitions` elimina las unidades de redes que ya no existen.

**La espera por partición ya no existe, y su ausencia carga peso.** `reconcileGfehPartition` arranca la unidad y ahí se queda; si un demonio está contestando se pregunta aparte, con `GfehReadyNetworks` y los recolectores de nombres, que ya tratan una partición silenciosa como algo que no aporta nada en vez de como una falla. La espera antes estaba dentro del bucle, una vez por partición — incluidas todas aquellas para las que no hacía nada, ya que `ensureFirstUserPrincipal` regresa en su primera línea para cualquier red que no sea la del hogar. En un contexto con fecha límite eso no era nada más lento: el primer demonio que nunca contestaba se gastaba todo el presupuesto que quedaba en `WaitForReady`, así que todas las particiones que venían después trataban de hacer `Start` sobre un contexto vencido y `pruneGfehPartitions` no corría nunca. Un demonio muerto se llevaba entre las patas al resto del almacenamiento de objetos, en el orden en que se acomodaran los nombres de red.

La única espera que queda es `seatGfehFounder`, hasta el final de la reconciliación: espera solo a la partición del **hogar**, con tope en `gfehFounderWaitBudget` (10 s, sobrescribible por configuración para las pruebas), y luego sienta a la primera cuenta del equipo. Al ser la última, pasarse solo puede retrasar trabajo que ya está hecho; a un demonio que todavía arranca en frío lo sienta la siguiente pasada, que el arranque corre inmediatamente después de `ReconcileNetworks`. Por la misma razón, `GfehReadyNetworks` le da a cada sondeo de salud su propio presupuesto con `context.WithoutCancel` en vez de jalar de lo que le quede a quien llama — una fecha límite gastada haría que todas las particiones se vieran muertas al mismo tiempo. La cancelación se sigue respetando; eso es un apagado.

**El almacenamiento de objetos no tiene ajuste de prendido/apagado.** Guardar archivos es para lo que sirve el equipo, así que corre como corren el DNS y el ingress — como parte de lo que Town OS es, no como una función que haya que habilitar. Un interruptor solo compraba la posibilidad de que lo encontraran apagado mientras alguien depuraba a dónde se fueron sus archivos; un administrador que quiera los demonios abajo los detiene desde el panel de servicios como cualquier otro servicio del sistema. Un renglón `object_storage_enabled` viejo que quede en la tabla de ajustes de un equipo actualizado no lo lee nadie.

Las salidas de emergencia que quedan son sobre una *compilación*, no sobre política: depende del ingress (con `INGRESS_IMAGE` vacío las cuatro vistas HTTP no las alcanza nada, así que arrancar particiones publicaría nombres que nada sirve) y `GFEH_IMAGE` explícitamente vacío se salta el almacenamiento de objetos por completo (modo de desarrollo) — la misma convención de `LookupEnv` que usan `UI_IMAGE` e `INGRESS_IMAGE`, porque `Getenv` haría que un valor vacío significara "usa el predeterminado" y no dejaría interruptor de apagado.

**La primera cuenta se sienta en la partición del hogar.** `ensureFirstUserPrincipal` crea un principal con el nombre de la cuenta más antigua del equipo (por `CreatedAt`, con el nombre de usuario como desempate, para que el fundador no cambie entre reconciliaciones según el orden de iteración del mapa), con `gfeh.CeilingForAccount(admin)`. Una partición cuyo bosque está vacío no le sirve a nadie: el operador abre la pestaña de Usuarios, no encuentra nada y tiene que deducir que su propia cuenta no está ahí. **Solo el hogar** — todos los equipos tienen esa partición, mientras que una red que se agregue después le pertenece a quien reciba un permiso sobre ella, y sentar ahí al fundador le entregaría un espacio de nombres que creó alguien más. Idempotente por vía de gfehd, que contesta 409 ante un principal que ya existe.

**Los nombres se publican después del intercambio del manejador.** `publishGfehNames` corre en segundo plano: gfehd sondea `/status/ping`, que responde **503 hasta que el router completo está en pie** ([Estado de Arranque](#estado-de-arranque-y-refresco)), así que una partición no puede terminar de arrancar hasta que el arranque esté prácticamente hecho. Esperarlo en línea trabaría el mismo arranque al que espera. Si ninguna partición queda lista a tiempo, los nombres los publica sin más la siguiente reconciliación.

Las particiones se registran en `collectSystemServices()`, así que `POST /system-services/refresh` las vuelve a bajar y reiniciar — la omisión que dejaba el ingress viejo en silencio.

### Acoplamiento de versiones

**Town OS fija un mínimo de gfehd, y es un mínimo y no una preferencia.** `Containerfile.gfeh` compila desde crates.io en `GFEH_VERSION` (sobrescribible, o con `GFEH_LATEST` no vacío para agarrar lo que haya hoy en crates.io — la misma forma que `TTYFORCE_LATEST` en el repositorio de instalación). El mínimo actual es **0.1.2**.

Ninguna de las dos fallas se ve en `make test` — tanto la suite unitaria como la de integración se apoyan en un **gfehd falso**, así que fijar por debajo del mínimo compra una suite en verde y un equipo donde el almacenamiento de objetos está muerto en silencio. Sube el mínimo cuando Town OS empiece a depender de comportamiento nuevo del demonio, y deja que la compilación de la imagen falle de forma ruidosa si esa versión todavía no está publicada.

### Interfaz

`/dashboard/objects` (navegación `nav.objects`, "Object Storage"). Un selector de red hasta arriba y luego subpestañas `?tab=`, una por archivo bajo `ui/src/routes/objects/`: **Overview** (estado, cuota y nombres publicados por partición, con si a cada uno se llega por el ingress o marcándolo directo), **Users** (principales y techos; agregar proyecta una cuenta de Town OS), **Grants** y **Links** (exposiciones, con retiro). Las lecturas son `requireAuth`, así que la pestaña no es solo para administradores; los controles que mutan necesitan admin o el permiso `gfeh`, y de cualquier forma solo en las redes de quien llama.

Dos detalles de esa pantalla existen para que un lector no actúe sobre un número
o un token que no se puede usar:

- **La columna Port de Overview está en blanco para una vista HTTP.** El puerto que gfehd reporta para una es un *puerto de backend del lado del contenedor* al que el ingress le hace proxy, inalcanzable desde donde está sentado cualquier lector — imprimir `9000` junto a "Ingress (HTTPS)" invita a marcar `s3.gfeh.home:9000` y concluir que la función está rota. SMB conserva su número, que sí sería un puerto real del host.
- **La pestaña Links renderiza la URL completa, compuesta en el servidor.** `GfehExposureView.URL` se construye a partir de `gfehPublishedLinkBase` — `https://<fqdn-vista-http>/f/` — que viene del mismo recolector que nombra el vhost del ingress y el SAN de la hoja, así que un enlace publicado es por construcción un nombre que el ingress enruta y que el certificado cubre. No se compone en el navegador porque la interfaz tendría que saber cuatro cosas que el servidor ya guarda: que el nombre que sirve es el de la *vista http* y no el de la partición ni el del equipo, que se califica bajo el TLD de la propia red de la partición y no bajo el global, que la ruta es `/f/<token>` y que el puerto reportado nunca debe aparecer. El campo queda vacío cuando la partición no sirve ninguna vista HTTP — la respuesta honesta, ya que entonces no hay nada sirviendo ese token — y una exposición deshabilitada se renderiza como texto plano en lugar de como un 404 al que se le puede hacer clic.

**Esta pantalla es el único lugar donde se administra el almacenamiento de objetos.** La pantalla de servicios no trae ninguna sección de almacenamiento de objetos: una partición ES un servicio del sistema — una unidad `town-os-system--gfeh-<red>` cada una — así que ya es un renglón en la tabla de Servicios del Sistema de esa pantalla, `Object Storage (<red>)`, con la misma insignia de estado y las mismas acciones de arrancar/detener/reiniciar/registros que cualquier otro servicio del sistema. Un panel junto a ella repetía ese renglón y consultaba por su cuenta, así que una unidad tenía dos controles a dos niveles que podían discrepar; además se renderizaba incondicionalmente mientras la tabla dependía de que su consulta hubiera regresado, lo cual dejaba al almacenamiento de objetos solito hasta arriba de la pantalla en el primer pintado y metía los servicios del sistema encima un momento después. `?expand=objects` en la pantalla de servicios abre Servicios del Sistema, que es donde vive el renglón.

## Servicios

### Filtrado de Unidades de Servicio

La consulta de unidades de systemd está acotada al patrón `town-os-package--*` a nivel de dbus, trayendo solo las unidades de paquetes de Town OS en vez de todas las unidades del sistema. Las unidades de servicios del sistema (`town-os-system--*`) se identifican aparte con `IsSystemServiceUnit()`. El conjunto de resultados excluye además los controladores de red (`-network.service`), los ayudantes de UPnP (`-upnp.service`) y los reenvíos de puerto (`-fwd-`). Las unidades de controlador de red se conservan internamente para detectar fallas, pero se excluyen de la lista que ve el usuario.

### Enriquecimiento de las Descripciones de Servicio

Las descripciones de los paquetes se cargan por lotes usando una sola llamada a `LoadPackages` por repositorio, en vez de leer el YAML de cada paquete por separado. Las descripciones se emparejan con las unidades de servicio construyendo el nombre de unidad esperado a partir de la identidad de cada paquete.

### Generación de Unidades de Servicio

Las unidades de servicio de systemd se generan distinto según el tipo de runtime del paquete.

**Los paquetes de contenedor** generan unidades basadas en podman, con `podman run` para arrancar y `podman stop` para detener, incluyendo mapeos de puertos (`-p`), variables de entorno (`-e`) y montajes de volúmenes (`-v`).

**Los paquetes de VM** generan unidades basadas en QEMU usando `qemu-system-x86_64` con:

- `-m {MB}` -- memoria en megabytes (convertida a partir del valor compilado en bytes).
- `-smp {cpus}` -- número de CPU virtuales.
- `-nographic` -- operación sin cabeza (sin salida de pantalla).
- `-enable-kvm` -- aceleración por hardware KVM.
- `-drive file={imagen},format=raw,if=virtio` -- imagen de disco raw como dispositivo de bloque virtio.
- `-netdev user,id=net0` con `hostfwd=tcp::{externo}-:{interno}` para cada mapeo de puerto -- red en modo usuario de QEMU con reenvío de puertos del host al invitado.
- `-device virtio-net-pci,netdev=net0` -- dispositivo de red paravirtualizado.

Las unidades de VM también administran los puertos del cortafuegos con `firewall-cmd` en los hooks previos al arranque y posteriores a la parada, y se coordinan con las unidades de socket para evitar conflictos de puertos.

### API de Unidades de Servicio

- `GET /systemd/units` (localhost o autenticación) -- lista todas las unidades de servicio de paquetes, en plano. Devuelve el estado de la unidad enriquecido con el identificador del paquete, la descripción del paquete y una marca de falla del controlador de red.
- `GET /systemd/units-tree` (localhost o autenticación) -- los mismos datos agrupados en un árbol de dependencias: los paquetes raíz arriba, las dependencias anidadas bajo su padre, recursivamente (la forma refleja la de `/storage/package-volumes`). Cada nodo trae `repo`/`name`/`version` (nombre efectivo en crudo, que puede traer `--dep--`) junto al `package_identifier` que ven las personas, además de los mismos campos de estado que el endpoint plano, así que un cliente no necesita una segunda petición para enriquecer los renglones. **La búsqueda y la paginación aplican solo a los nodos raíz** — los descendientes de dependencias no cuentan contra la página, así que un árbol siempre viaja con su subárbol completo, incluso en un límite de página.
- `POST /systemd/status` (requiere admin) -- cambia el estado de una unidad de servicio. Acepta el nombre de la unidad y la acción (start, stop, restart, enable, disable).
- `POST /systemd/status/tree` (requiere admin) -- aplica una acción a todo el árbol de dependencias de un paquete raíz. Acepta `repo`, `name` (nombre efectivo en crudo, para que los valores de las API de instalación se puedan reutilizar sin cambios), `version` y `action`. Solo se permiten `start`, `stop` y `restart` — `enable`/`disable` se rechazan — y detener la propia unidad del controlador del sistema se rehúsa. **El orden del recorrido depende de la acción**: las unidades se juntan de las hojas hacia arriba (el orden natural para arrancar y reiniciar) y el orden se invierte para detener, de modo que la raíz cae antes que sus descendientes.

### Interfaz de Administración de Servicios

La pantalla de servicios muestra una tabla de datos paginada con las unidades de systemd de los paquetes instalados. Cada renglón muestra el identificador del paquete, la descripción, el estado activo, el subestado y una lista desplegable de acciones.

#### Acciones de Servicio

La lista desplegable de acciones de cada servicio ofrece:

- **Arrancar** -- arranca el servicio (con confirmación).
- **Detener** -- detiene el servicio (con confirmación; deshabilitado para el propio controlador del sistema).
- **Reiniciar** -- reinicia el servicio (con confirmación).
- **Registros del servicio** -- abre el visor del diario para la unidad de este servicio.
- **Registros de red** -- abre el visor del diario para la unidad del controlador de red de este servicio (el nombre de la unidad con sufijo `-network.service`).

### Registros Avanzados

Un botón "Advanced Logs" debajo de la tabla de servicios abre un modal con:

- **Registros del controlador** -- ver los registros de `town-os-systemcontroller.service`.
- **Registros del sistema** -- ver los registros de todo el sistema (todas las unidades).
- **Errores del diario** -- ver los registros del sistema filtrados al nivel de prioridad 3 (errores y superiores, equivalente a `journalctl -p 3`).
- **Nombre de servicio personalizado** -- entrada de texto para ver los registros de cualquier unidad arbitraria de systemd.

### Visor del Diario

El diálogo del visor del diario ofrece:

- Título dinámico que muestra el nombre de la unidad, "System Logs" o "Journal Errors" según el contexto.
- Insignia de estado con el estado activo y el subestado de la unidad (cuando se está viendo una unidad específica).
- Búsqueda en tiempo real con filtrado antirrebote (300 ms).
- Filtrado por rango de tiempo, por fecha y hora.
- Interruptor de modo seguimiento para el volcado continuo de registros con autodesplazamiento (se deshabilita solo cuando hay filtros de búsqueda o de tiempo activos).
- Desplazamiento inicial hasta abajo: cuando se abre el visor, el contenedor de registros se desplaza al final una vez que las entradas terminaron de cargar. El efecto de desplazamiento hasta abajo está condicionado a `journalEntries.length > 0` para que no se consuma en el primer renderizado vacío antes de que lleguen las entradas; un `requestAnimationFrame` final vuelve a fijar `scrollTop` después de que el diseño se asienta, por si el árbol expandido crece entre el commit y el pintado.
- Interruptor de vista en árbol para agrupar las entradas por minuto. La vista en árbol es la predeterminada y cada grupo de minuto está **expandido por omisión**. El mapa de estado de expansión guarda únicamente los plegados explícitos: una entrada indefinida se trata como expandida, así que las primeras pulsaciones pliegan en vez de expandir.
- Copiar al portapapeles todas las entradas de registro que se muestran.
- Renderizado de códigos de color ANSI en la salida de registros.
- Resaltado de campos estructurados (pares `nombre=valor`).

### API de Registros

Dos endpoints sirven datos de registros:

- `GET /systemd/logs` (localhost o admin) -- transmite entradas históricas del diario con Server-Sent Events. El parámetro de consulta `unit` escoge el servicio; vacío o `__system__` devuelve los registros de todo el sistema.
- `GET /systemd/logs/tail` (localhost o admin) -- devuelve una página JSON de entradas del diario. Acepta los parámetros: `unit`, `lines` (100 por omisión), `before`/`after` (paginación por cursor), `grep` (búsqueda sin distinguir mayúsculas), `since`/`until` (marcas de tiempo Unix) y `priority` (filtro de severidad syslog, 0 = sin filtro).
- `GET /systemd/logs/tree` y `GET /systemd/logs/tree/tail` (localhost o admin) -- las contrapartes acotadas al árbol. En vez de un `unit`, toman `repo`, `name` y `version` (todos obligatorios) y cubren **todas** las unidades de systemd del árbol de dependencias de ese paquete, así que los registros del padre y los de sus dependencias se entremezclan en una sola vista. Por lo demás, la semántica de reproducción y de paginación coincide con la de `/systemd/logs` y `/systemd/logs/tail`.

## Administración de Cuentas

### Modelo de Cuenta

Cada cuenta tiene: nombre de usuario (llave primaria), hash de contraseña (nunca se expone en JSON), correo electrónico, teléfono, nombre real, marca de administrador, marca de deshabilitada, un **conjunto de permisos otorgados**, un alcance de redes y marcas de tiempo de creación/actualización. Las cuentas se guardan en una tabla SQLite.

**No existe ningún "tipo" de cuenta.** Una cuenta o es administradora (tiene todos los permisos, en todas las redes) o no lo es, y una cuenta que no es administradora trae los permisos que estén prendidos. `Account.Restricted()` — una cuenta no administradora con por lo menos un permiso otorgado — se deriva, nunca se guarda.

**No hay cuentas de servicio.** Una versión anterior le daba al demonio de almacenamiento de objetos su propia cuenta de administrador; ya no existe, y `account.PurgeLegacyServiceAccounts` la borra (junto con su contraseña guardada) en el primer arranque después de la actualización. Ve [Sin cuentas de servicio](#sin-cuentas-de-servicio).

### Reglas de Validación

- **Contraseña** -- mínimo 8 caracteres, y solo ASCII imprimible (`0x21`--`0x7E`, sin espacios). Los bytes con bit alto y los de control se rechazan al momento de crearla (`ErrPasswordInvalidChars`) en vez de confiar en que todas las capas del camino hasta bcrypt — la autenticación HTTP Basic, JSON, la codificación de URL, las columnas latin1 de la base de datos — las transporten idénticas.
- **Correo electrónico** -- formato de correo estándar (`usuario@dominio.tld`).
- **Teléfono** -- dígitos con formato opcional (`+`, espacios, guiones, paréntesis).
- **Datos de contacto** -- el correo, el teléfono y el nombre real son todos obligatorios (no vacíos).
- **Permisos otorgados** -- todos los nombres tienen que estar en `account.AllGrants` (`ErrInvalidGrant`), un administrador no puede tener ninguno de forma explícita (`ErrGrantsAdmin` — ya los tiene todos, así que un subconjunto guardado solo podría discrepar) y una cuenta que tenga alguno tiene que estar acotada a por lo menos una red (`ErrGrantsNoNetworks`).
- **Alcance de redes** -- cada entrada tiene que ser un nombre de red válido (`ErrInvalidNetworkName`). Una lista vacía nunca se lee como "cualquier red".

### Permisos Otorgados (grants)

Un **permiso otorgado** (grant) es una capacidad con nombre que puede tener una cuenta no administradora. Existen dos:

| Permiso | Constante | Compra |
|---|---|---|
| `wireguard` | `account.GrantWireGuard` | inscribir y refrescar pares WireGuard en las redes de la cuenta |
| `gfeh` | `account.GrantGfeh` | administrar el almacenamiento de objetos que tienen esas mismas redes — principales, sus permisos, enlaces publicados |

`account.AllGrants` es el registro: un permiso que no esté ahí no se puede guardar, que es lo que impide que un dedazo en una petición de la API se convierta en un permiso que en silencio nunca coincide con nada. Agregar una capacidad es una entrada ahí más sus rutas en `grantRoutes` — sin columna nueva, sin migración nueva, sin puntero nuevo en `UpdateFields`. La interfaz renderiza sus casillas a partir del espejo `ui/src/lib/grants.js`, así que un permiso nuevo tampoco necesita marcado nuevo.

Los dos son **independientes**. Tener `wireguard` no compra nada en el almacenamiento de objetos y tener `gfeh` no compra ninguna inscripción de pares; una cuenta puede tener los dos. `Account.HasGrant` contesta "¿puede quien llama hacer esto siquiera?" y `Account.MayAdministerNetwork` contesta "¿en cuál red?" — nunca uno por el otro.

#### La aplicación son tres capas, y la composición es la gracia

1. **`grantAllowlist`** es un middleware *global* que falla en cerrado. Una ruta que se agregue mañana se le niega por omisión a una cuenta restringida hasta que alguien la enliste en `grantRoutes` (`src/svc/systemcontroller/controller_auth.go`), indexada por `"MÉTODO RUTA"`. Las peticiones sin token válido, las de un administrador o las de una cuenta común sin permisos pasan directo a la autenticación propia de la ruta — un permiso otorgado es autoridad *aditiva* para una cuenta que existe para ejercerla, y esto confina solo a esas.
2. **El middleware propio de la ruta** — `requirePeerEnroll` (el permiso `wireguard`) y `requireObjectStorage` (el permiso `gfeh`), los dos construidos a partir de `requireGrant`, que admite a los administradores porque tienen todos los permisos. Las lecturas se quedan como `requireAuth`.
3. **`requireNetworkScope`**, dentro del manejador, porque la red vive en el cuerpo o en la consulta de la petición y solo el manejador la analizó. **Confina**; no otorga, y confina únicamente a las cuentas `Restricted()` — una cuenta común no tiene permisos y por lo tanto tampoco alcance, y un alcance vacío niega todas las redes, así que aplicárselo a una cuenta común daría 403 en todas las lecturas de rutas que son `requireAuth` a propósito.

`grantRoutes` es todo lo que compra un permiso otorgado:

```
wireguard: GET  /networks/peers   POST /networks/peers/add   POST /networks/peers/refresh
gfeh:      GET  /gfeh             GET  /gfeh/principals      POST /gfeh/principals/add
           POST /gfeh/principals/remove                      GET  /gfeh/grants
           POST /gfeh/grants/add  POST /gfeh/grants/revoke   GET  /gfeh/exposures
           POST /gfeh/exposures/withdraw
```

más `grantCommonRoutes`, que puede alcanzar cualquier titular de permiso sin importar cuál: `POST /account/authenticate`, `GET /account/me`, `GET /networks`, `GET /dns/services`, `GET /tls/ca.crt` y `GET /status/ping`. Sin ellas un permiso no se puede usar — no puedes ejercer uno sin antes iniciar sesión — así que son comunes en vez de estar duplicadas en cada permiso.

`GET /status/ping` está en esa lista por una segunda razón: es **pública**, registrada sin ningún middleware de autenticación, así que un desconocido anónimo recibe un 200. Como la lista blanca es global y falla en cerrado, omitirla significaba que un token válido convertía ese 200 en un 403 — autenticarse dejaba a quien llamaba estrictamente peor que no presentar nada. Además es el latido de sesión de 60 segundos del tablero y la fuente de toda su superficie de estado, así que una cuenta con `gfeh` podía alcanzar todas las rutas `/gfeh` y de todos modos no obtener una página usable. Otorgar también `wireguard` nunca ayudaba: el ping no está indexado a ninguno de los dos permisos.

Fíjate en lo que está a propósito **ausente**: `/gfeh/partitions/*` se queda como `requireAdmin` (aprovisionar una partición crea la raíz de un árbol de permisos y aparta un subvolumen btrfs; `TOWNOS_CONTRACT.md` lo reserva a los administradores y el cliente de gfeh ramifica sobre el 403), y `GET /networks/peers/connected` agrega los pares de todas las cuentas y las direcciones de origen observadas en todas las redes.

A diferencia de `Admin` — inmutable después de crearse — los permisos son mutables, y `account.Manager.CreateGranted` es un método aparte de `Create` para que los invariantes (un titular de permiso nunca es administrador y siempre tiene un alcance no vacío) se apliquen en un solo lugar al momento de crear, en vez de armarse a partir de una firma posicional ensanchada.

#### Migración desde las columnas viejas

Las versiones anteriores traían una columna booleana por capacidad. `legacyGrantColumns` (`src/account/sqlite.go`) mapea cada una al permiso en que se convierte, y `migrateLegacyAccountColumns` la traspasa y elimina la columna:

| Columna heredada | Se convierte en |
|---|---|
| `wireguard` | `wireguard` |
| `object_storage` | `gfeh` |
| `network_only` (un esquema intermedio que juntaba las dos en una sola marca) | los dos |

**Una columna, un permiso.** Una cuenta que podía inscribir pares sigue pudiendo, y una que no podía no lo gana en silencio — ensanchar la autoridad durante una actualización es la dirección que no se puede deshacer, ya que la cuenta conserva su contraseña y nada en pantalla dice que creció. `smb_nt_hash` se elimina sin más (ve [Sin vista SMB](#sin-vista-smb)).

### Toda cuenta pertenece a la red del hogar

`Manager.Create` — el camino que toman la **primera** cuenta y todas las cuentas comunes — escribe `networks: ["home"]`. `CreateGranted` no lo junta: ahí, el alcance que escogió un administrador son exactamente las redes que la cuenta puede alcanzar, y meterle `home` ensancharía un portal acotado a `office`.

Esto es seguro porque, para una cuenta sin permisos, el alcance es **pertenencia, no confinamiento**: `Restricted()` es falso, así que ninguna capa de arriba lo consulta. Y nunca puede nombrar una red que no esté ahí — ve [La red del hogar siempre existe](#la-red-del-hogar-siempre-existe).

### API de Cuentas

- `POST /account/create` -- crea una cuenta nueva. En modo de arranque inicial (no existe ninguna cuenta de administrador habilitada), se permite el acceso sin autenticar; si no, se requiere autenticación de administrador. Un arreglo `grants` no vacío se va a `CreateGranted` con las `networks` que se den; si no, la cuenta se crea con `Create` y se une a la red del hogar. Los errores de nombre de usuario duplicado devuelven un mensaje de falla genérico para evitar la enumeración de usuarios.
- `POST /account` -- obtiene una cuenta por nombre de usuario (requiere autenticación).
- `GET /account` -- lista todas las cuentas con paginación y búsqueda (requiere autenticación).
- `POST /account/update` -- actualiza los campos de una cuenta (requiere autenticación). El nombre de usuario que se actualiza viene del **cuerpo**, así que editar la cuenta de alguien más es solo para administradores: sin esa revisión, cualquier cuenta autenticada podría mandar `{"username":"admin","fields":{"password":"..."}}` y quedarse con el equipo — el controlador maneja el socket de podman del host, así que eso es root. Una cuenta común todavía puede editar sus propios datos de contacto y su contraseña, por eso la ruta no es directamente `requireAdmin`. El estatus de administrador no se puede cambiar después de crear la cuenta; los permisos otorgados y el alcance de redes sí, **solo por un administrador, incluso sobre tu propia cuenta** — si no, un usuario normal podría otorgarse `gfeh` y meterse a una partición, o `wireguard` e inscribir un par en la superposición. Un `networks` nil deja el alcance guardado intacto; uno que no es nil lo reemplaza por completo. `validateGrantResult` revisa el estado del renglón *después* de la actualización, así que otorgarle algo a un administrador, promover a un titular de permiso y vaciarle el alcance a un permiso se detectan todos.
- `POST /account/disable` -- deshabilita una cuenta, impidiendo la autenticación (requiere admin). También revoca las sesiones vivas de la cuenta. Eso no es lo que hace efectivo el deshabilitado — `SessionManager.Validate` rechaza por su cuenta el token de una cuenta deshabilitada, así que la garantía no depende de que la revocación haya salido bien — sino lo que impide que un token emitido antes del deshabilitado vuelva a funcionar si la cuenta se rehabilita después, que no es lo que un administrador entiende por "habilitar" después de haberle quitado el acceso a alguien.
- `POST /account/enable` -- rehabilita una cuenta deshabilitada (requiere admin).

### Interfaz de Administración de Cuentas

La pantalla de administración de usuarios (`/dashboard/users`) muestra una tabla de datos de cuentas paginada, ordenable y con búsqueda. Cada renglón muestra el nombre de usuario, el correo, el teléfono, el nombre real, una insignia de rol admin/usuario y el estado habilitada/deshabilitada. Las acciones por renglón incluyen un botón de Editar (abre un diálogo para actualizar la contraseña, el correo, el teléfono, el nombre real, las **casillas de permisos otorgados** y el selector de alcance de redes) y un interruptor de Habilitar/Deshabilitar con confirmación. Un enlace lleva a una página dedicada para crear usuario (`/dashboard/users/create`) con un formulario de registro que trae los mismos controles. Los dos formularios renderizan sus casillas a partir de `ui/src/lib/grants.js` y rechazan otorgar nada si no se escogió ninguna red.

### Administración de Sesiones

Las sesiones usan tokens JWT (HS256) con reclamos para el identificador de sesión (UUID), el nombre de usuario y la marca de tiempo de emisión. La llave de firma es efímera: 32 bytes aleatorios generados con `crypto/rand` en cada arranque del servicio, nunca guardados en disco. Cuando `InitSessionManager` corre al arrancar, todas las sesiones existentes se borran (`DELETE FROM sessions`), ya que los tokens previos no sirven con la llave nueva. La variable de entorno `TOWN_OS_SIGNING_KEY` puede sobrescribir la llave generada. Las sesiones vencen a los 7 días desde su último uso. Una tarea de limpieza en segundo plano elimina periódicamente las sesiones vencidas.

**El token de una cuenta deshabilitada está muerto al llegar.** `Validate` revisa `Disabled` y rechaza, porque todas las peticiones posteriores al inicio de sesión se autorizan únicamente desde esa función: sin la revisión, deshabilitar una cuenta solo impedía que volviera a *iniciar sesión*, mientras que un token que ya tuviera seguía sirviendo toda la vida de la sesión y se refrescaba con su propio uso.

La interfaz `SessionManager` provee: `Create`, `Validate`, `Revoke`, `RevokeAllForUser`, `Cleanup`, `List`, `GetUsername`, `HasActiveAdminSessions` y `StartCleanup`.

Endpoints de la API de sesiones:

- `POST /account/authenticate` -- inicio de sesión con usuario/contraseña (público). Devuelve un token JWT y el objeto de la cuenta. Las fallas de autenticación (contraseña equivocada, usuario que no existe, cuenta deshabilitada) devuelven todas el mismo error genérico de "credenciales inválidas" para evitar la enumeración de usuarios.
- `GET /account/sessions` -- lista las sesiones del usuario autenticado (requiere autenticación).
- `GET /account/me` -- obtiene el nombre de usuario del usuario autenticado (requiere autenticación).
- `POST /account/session/revoke` -- revoca una sesión específica por su identificador (requiere autenticación).

### Registro de Auditoría

Todas las acciones administrativas se registran en un log de auditoría. Cada entrada tiene: identificador autoincremental, cuenta (nombre de usuario), descripción de la acción, ruta de la petición, detalle limpiado (credenciales enmascaradas), marca de éxito, mensaje de error y marca de tiempo de creación.

**El limpiador enmascara en vez de borrar**, reemplazando el valor de una credencial con `[REDACTED]` y dejando la llave. Quien lea la auditoría debería poder ver que un campo estaba presente y se retuvo, no quedarse sin poder distinguirlo de una petición que nunca lo trajo. Compara contra `auditRedactedKeys` sin distinguir mayúsculas, contra la llave entera y contra el sufijo de la llave después del último guion bajo, así que `smtp_password` se detecta sin una regla de subcadena que también se tragaría nombres inocuos, y recursa tanto en arreglos como en mapas. El mapa `responses` de la instalación de un paquete se trata como **opaco** y se enmascara entero: sus llaves le pertenecen al autor del paquete, así que no hay vocabulario contra el cual comparar, y sus valores son exactamente las respuestas generadas de `type: secret` y `type: oauth` de las que el registro no debe volverse copia. Una `key` pelona está a propósito FUERA de la lista — la regla del sufijo agarraría entonces `public_key`, que trae `POST /networks/peers/add`, y una llave pública de WireGuard es pública por construcción, además de ser el único campo que dice qué dispositivo se inscribió.

Las acciones que se registran incluyen: crear/modificar/eliminar sistema de archivos, agregar/eliminar/mover/refrescar repositorio, instalar/desinstalar paquete, purgar volúmenes, deshabilitar/habilitar paquete, poner el estado de una unidad, crear/actualizar/deshabilitar cuenta, autenticarse, revocar sesión, actualizar ajuste, descartar actualizaciones, subir/bajar archivo comprimido, crear/actualizar/eliminar/reconstruir página, subir/eliminar imagen de VM.

Los endpoints de solo lectura se excluyen explícitamente del registro de auditoría. Entre las rutas excluidas están la ruta raíz (`/`), todos los endpoints GET de listado/consulta, los endpoints de información (`/packages/installed/info`), la recuperación de respuestas (`/packages/last-responses`, `/packages/responses`), la vista previa de instalación (`/packages/install-preview`), las búsquedas de versiones/preguntas, el listado de zonas horarias, el endpoint de listado de pages, el ping de estado, el listado de servicios del sistema (`/system-services`), las consultas al log de auditoría, las lecturas de ajustes y los endpoints de transmisión de registros.

- `POST /audit/log` (localhost o admin) -- consulta el log de auditoría con paginación por cursor o por desplazamiento, filtrado por cuenta, ordenamiento y búsqueda.

### Administración de Ajustes

Los ajustes llave-valor se guardan en SQLite. Entre los ajustes predeterminados están `default_quota` (50 GB), `max_archive_size` (1 GB), `archive_unpack_timeout` (600 segundos), `locale` (en-US), `dns_tld` (home), `dns_resolution_mode` (auto), `dns_local_forwarders` (false), `peer_ttl` (7200 segundos) y `gfeh_partition_quota` (0). `proton_image` solo se registra en compilaciones con la etiqueta `proton`. Ve [Ajustes](#ajustes) para la tabla completa.

- `GET /settings` -- obtiene todos los ajustes (requiere admin).
- `POST /settings/get` -- obtiene un ajuste específico por llave (requiere admin).
- `POST /settings/set` -- pone el valor de un ajuste (requiere admin, se registra en auditoría). Los ajustes con valor en bytes (`default_quota`, `max_archive_size`) aceptan cadenas legibles por humanos (p. ej., "500GB", "10MB") que se analizan y se guardan como conteos numéricos de bytes.

### Interfaz de Ajustes

La pantalla de ajustes del sistema ofrece controles configurables por el administrador para todos los ajustes globales. Cada ajuste se muestra en una sección con borde, con un encabezado, una descripción que muestra el valor actual en formato legible por humanos y un formulario con una entrada numérica, un selector de unidad y un botón de guardar.

- **Cuota de volumen predeterminada** -- configurable en GB, MB o bytes. Muestra "0 (sin cuota)" cuando vale cero.
- **Tamaño máximo de archivo comprimido** -- configurable en GB, MB o bytes. Controla el tamaño máximo de archivo que se permite en las subidas de archivos comprimidos.
- **Tiempo límite de desempaquetado** -- configurable en segundos, minutos u horas. Controla el tiempo máximo que se permite para desempaquetar un archivo comprimido que se subió.
- **Idioma** -- una lista desplegable con los idiomas comunes y sus nombres en escritura nativa. Una sección expandible revela las configuraciones regionales extendidas. Las que no tienen catálogo se muestran con un asterisco y deshabilitadas.
- **Imagen de Proton** -- una entrada de texto editable para la referencia a la imagen de contenedor del runner de Proton (p. ej., `quay.io/town/proton:latest`).
- **Reenviadores DNS locales** -- un interruptor respaldado por `dns_local_forwarders`. Debajo, las direcciones a las que rolodex está reenviando *de verdad*, leídas de `GET /dns/status` en vez de deducirse del ajuste; cuando el descubrimiento no encontró nada usable, el panel dice que se siguen usando los reenviadores públicos, que es el único caso en el que el interruptor se lee como prendido y no cambió nada. Ve [Reenviadores locales](#reenviadores-locales).

Los valores actuales se descomponen en la unidad más apropiada para mostrarlos (p. ej., 1073741824 bytes se muestra como "1 GB", 120 segundos se muestra como "2 minutos"). La validación de entrada rechaza los valores negativos y los que no son numéricos.

## Actualizaciones de Paquetes

### Detección de Actualizaciones

El sistema de actualizaciones compara las versiones de los paquetes instalados contra las últimas versiones disponibles en los repositorios configurados. Un paquete se marca para actualizar cuando existe una versión más reciente o cuando se detectan modificaciones locales.

- `GET /packages/upgrades` (requiere autenticación) -- lista las actualizaciones disponibles. Cada entrada incluye el repositorio, el nombre, la versión instalada, la última versión y una marca de cambio.
- `POST /packages/upgrades/dismiss` (requiere admin) -- marca las actualizaciones actuales como descartadas. Calcula un hash SHA256 del conjunto de actualizaciones actual y lo guarda como el ajuste `dismissed_upgrades_hash`.

La respuesta del ping de estado incluye `upgrades_available` (conteo) y `upgrades_dismissed` (booleano, verdadero si el hash coincide).

## Redes

### Mapeo de Puertos UPnP

La interfaz `upnp.Manager` provee `AddPortMapping` y `RemovePortMapping` para administrar el reenvío de puertos TCP en la puerta de enlace de la red local con UPnP/IGD. La implementación descubre el Internet Gateway Device con SSDP y usa los métodos SOAP de WANIPConnection2. La IP local se detecta conectándose a una dirección externa (8.8.8.8:80 UDP).

### Controlador de Red

El controlador de red administra el reenvío de puertos y los mapeos UPnP por paquete. Cada paquete con requisitos de red tiene un archivo JSON de estado que especifica los puertos con sus mapeos externo/interno, la marca de UPnP y la marca de reenvío.

- **Reenvío con socat** (cuando `forward=true`) -- corre `socat TCP-LISTEN:{puertoExterno},fork,reuseaddr TCP:127.0.0.1:{puertoInterno}` para reenviar el tráfico.
- **Mapeo UPnP** (cuando `upnp=true`) -- mapea puertos en la puerta de enlace. Cuando `forward=true`, mapea externo-a-externo (socat escucha); cuando `forward=false`, mapea externo-a-interno (lo maneja el puente de podman).
- **Reconciliación** -- vigila los archivos de estado con fsnotify, deteniendo/arrancando reenviadores y mapeos según haga falta.
- **Renovación** -- los mapeos UPnP se renuevan cada 10 minutos con un TTL de 1800 segundos.
- **Apagado** -- elimina todos los mapeos UPnP y mata todos los procesos socat cuando se cancela el contexto.

### Red Compartida de las Dependencias

Las dependencias de un paquete comparten la red podman del paquete padre. Esto deja que los contenedores del mismo árbol de dependencias se comuniquen directo por nombre de contenedor (con el DNS integrado de podman en la red compartida) en vez de pasar por el reenvío de puertos del host.

- **Creación idempotente de la red** -- toda unidad de servicio incluye `ExecStartPre=-/usr/bin/podman network create {red}` exista o no un controlador de red (NC). Es una red de seguridad para el orden de arranque: si el NC todavía no creó la red (p. ej., imagen sin construir, carrera de systemd), el servicio puede arrancar de todos modos. El NC también crea la red — gana quien llegue primero, y para el otro es una operación sin efecto.
- **Propiedad de la red** -- el paquete padre es el dueño de la red podman (`town-os-net--{repo}-{nombre}-{versión}`). El NC crea la red en `ExecStartPre` y la elimina (`podman network rm -f`) en `ExecStopPost`.
- **Las dependencias se unen a la red del padre** -- las unidades de servicio de las dependencias usan `--net {red-del-padre}` en vez de crear la suya. Crean la red de forma idempotente en `ExecStartPre` (por si arrancan antes que el padre) pero nunca la eliminan.
- **Los paquetes independientes sin puertos** siguen el patrón original: `podman network rm -f` y luego `podman network create` en `ExecStartPre`, y `podman network rm -f` en `ExecStopPost`. Solo los paquetes independientes sin NC ni NC de padre hacen `rm -f` antes de `create`.
- **Los padres con dependencias** NO hacen `rm -f` antes de `create` en `ExecStartPre` porque puede que las dependencias ya estén corriendo en la red (arrancan primero por el orden de `Before=`).

### Orden de las Dependencias en Systemd

Las unidades de systemd de las dependencias traen directivas de orden que garantizan una secuencia correcta de arranque/parada respecto al padre:

- **Unidades de dependencia**: `PartOf={servicio-padre}` (detener el padre cae en cascada a las dependencias) y `Before={servicio-padre}` (la dependencia arranca antes que el padre y se detiene después que él).
- **Unidades padre**: `Wants={dep1} {dep2} ...` y `After={dep1} {dep2} ...` (el padre quiere las dependencias y las espera antes de arrancar).
- **Controlador de red**: el `Wants=` que ya existe para el NC se junta con los objetivos `Wants=` de las dependencias.

Esto se configura con los campos de `PackageUnitConfig`: `ParentNetwork`, `ParentUnitName` (para las dependencias) y `DependencyUnitNames` (para los padres). La reconciliación los calcula a partir de los registros de dependencias y de `ParentName()`.

### Variables de Entorno de las Dependencias

Los paquetes padre reciben variables de entorno para alcanzar a sus dependencias en la red compartida:

- `TOWNOS_DEP_{LLAVE}_HOST` -- el nombre del contenedor podman de la dependencia (resoluble con el DNS de podman en la red compartida).
- `TOWNOS_DEP_{LLAVE}_PORT_{puertoContenedor}` -- el número de puerto del lado del contenedor (como el padre y la dependencia están en la misma red, no hace falta ningún mapeo de puertos del host).
- `TOWNOS_DEP_{LLAVE}_PORT_{NOMBRE}` -- se emite además de la forma numérica cuando la dependencia declaró un nombre semántico de puerto en `network.external` / `network.internal` (ve **Puertos con Nombre** más abajo). El nombre se pasa a mayúsculas, así que `sql` en la dependencia se vuelve `TOWNOS_DEP_DB_PORT_SQL` en el padre. Las dos formas, la numérica y la nombrada, conviven y siempre traen el mismo valor.

### Variables de Plantilla de las Dependencias

Además de las variables de entorno de ejecución de arriba, los valores de host y puerto de las dependencias también están disponibles como marcadores de plantilla `@variable@` durante la compilación del paquete. Esto deja que los paquetes padre referencien dependencias en los valores de su campo `environment` en tiempo de compilación, y también deja que las **dependencias hermanas** se referencien entre sí en el bloque `dependencies.<llave>.responses`.

- `@dep_LLAVE_host@` -- se resuelve al nombre del contenedor podman de la dependencia (resoluble con el DNS de podman en la red compartida).
- `@dep_LLAVE_port_N@` -- se resuelve al puerto numérico N del contenedor de la dependencia.
- `@dep_LLAVE_port_NOMBRE@` -- se resuelve al puerto de contenedor que la dependencia etiquetó con el nombre semántico `NOMBRE` (ve **Puertos con Nombre** más abajo). En minúsculas en la plantilla; coincide con el sufijo de la variable de entorno sin distinguir mayúsculas. Convive con `@dep_LLAVE_port_N@` para el mismo puerto.

Las llaves de plantilla se derivan de los nombres de las variables de entorno `TOWNOS_DEP_*` quitando el prefijo `TOWNOS_` y pasando el resto a minúsculas. Por ejemplo, `TOWNOS_DEP_DB_HOST` se vuelve la llave de plantilla `dep_db_host`, y `TOWNOS_DEP_DB_PORT_5432` se vuelve `dep_db_port_5432`.

La forma `@dep_*@` solo se respeta donde ya corre la sustitución `@variable@` — los valores de `environment` y los `responses` de las dependencias. Dentro del `content` de una plantilla de archivo, usa en su lugar el espacio de nombres `.Dep` de las plantillas de Go (ve **Plantillas de Archivo** más arriba): `{{.Dep.LLAVE.Host}}` e `{{index .Dep.LLAVE.Ports "sql"}}` traen los mismos valores. `.Dep` se puebla a partir del mismo cálculo de `TOWNOS_DEP_*` y expone cada puerto tanto bajo su llave numérica (`"5432"`) como bajo su nombre semántico en minúsculas (`"sql"`) cuando se declaró uno.

Del lado del **padre**, estas variables se resuelven después de instalar las dependencias, cuando ya se conocen el nombre de contenedor y los puertos de la dependencia. Se aplican a los valores de entorno del padre durante la generación de unidades. La reconciliación también reconstruye las variables de entorno de las dependencias para que las unidades de systemd sigan correctas a través de reinicios y cambios de versión.

Del lado de la **dependencia** (respuestas declaradas bajo `dependencies.<llave>.responses` que referencian otra llave hermana), la resolución pasa durante `installDependencies` con un ordenamiento topológico:

- `orderDependencies`, en `src/svc/systemcontroller/controller_install_dependencies.go`, analiza los `Responses` de cada dependencia hermana buscando marcadores `@dep_LLAVE_host@` / `@dep_LLAVE_port_N@` y construye un DAG. Las dependencias hermanas sin referencias corren primero; las que referencian corren después de la o las hermanas que nombran. El desempate entre dependencias igual de listas es alfabético por determinismo (la iteración de mapas en Go es aleatoria, así que un ordenamiento es obligatorio para la reproducibilidad).
- Un ciclo entre dependencias hermanas es un error duro y aborta la instalación antes de aprovisionar ninguna dependencia.
- Para cada dependencia en ese orden, se llama a `applyDepTemplates` sobre los `Responses` de la dependencia **antes** de que corra `depIP.CompileWithContext`, sustituyendo los marcadores `@dep_OTRA_*@` con los valores de nombre de contenedor / puerto acumulados de las hermanas ya instaladas. Sin esa sustitución previa a la compilación, una pregunta tipada del YAML de la dependencia (p. ej. `type: port` o cualquier tipo cuyo `Output` corra `strconv.ParseUint`) rechazaría el marcador literal con `ErrInvalidResponseType`, abortando a media instalación y dejando un padre a medio instalar en disco.
- Las autorreferencias (la dependencia X referencia `@dep_X_host@`) se ignoran, no se tratan como ciclos. Las referencias a nombres que no son llaves hermanas declaradas se tratan como variables de plantilla externas y se ignoran para el orden.
- El manejador de instalación transmite los errores por SSE y devuelve `nil` desde el manejador HTTP, así que el log de auditoría siempre registra `success=true` sin importar si la instalación de veras se completó. Esto significa que las fallas de instalación parcial (árboles de dependencias a medio instalar, volúmenes btrfs huérfanos bajo `installed/<repo>/<padre>/<versión>/`) solo se ven en el flujo SSE y en la lista de unidades de systemd — no en `/audit/log`.

Ejemplo: un paquete con una llave de dependencia `db` (un contenedor de Postgres que expone el puerto 5432) puede usar `@dep_db_host@` y `@dep_db_port_5432@` en su sección de entorno en vez de dejar fijo `127.0.0.1`:

```yaml
environment:
  DB_HOST: "@dep_db_host@"
  DB_PORT: "@dep_db_port_5432@"
```

Ejemplo con referencias entre hermanas (la forma de jitsi): `jitsi` depende de `prosody`, `jicofo` y `jvb`. `jicofo` y `jvb` necesitan cada uno el nombre de contenedor de prosody y su puerto XMPP interno, así que el YAML del padre se los pasa por el bloque `responses` de cada dependencia que los referencia. `orderDependencies` instala primero `prosody`, luego `jicofo` y `jvb` (alfabéticamente entre las dos), cada una con el marcador sustituido por el nombre de contenedor concreto de prosody y el puerto 5222:

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

Los paquetes del mismo árbol de dependencias pueden compartir subvolúmenes btrfs con una aceptación explícita de los dos lados. El autor de la dependencia marca un volumen con `shareable: true`; el autor del padre declara entonces o bien un bloque `expose:` (montar el volumen de la dependencia dentro del contenedor del padre) o bien un bloque `consume:` en otra dependencia (montar el volumen de una hermana dentro del contenedor de otra hermana). Los volúmenes sin `shareable: true` no se pueden montar de forma cruzada — la pasada de instalación/reconciliación rechaza cualquier referencia a un volumen que no sea compartible.

El cableado es una capa delgada sobre la infraestructura que ya existe de `HostVolumeMount`: la ruta de instalación resuelve cada entrada `expose`/`consume` en un flag `-v <rutahost>:<rutacontenedor>:<opciones>` de podman que apunta al subvolumen btrfs en disco de la dependencia productora. La reconciliación reconstruye los mismos flags en cada arranque a partir del YAML persistido del padre, y la comparación de contenido de `installUnitIfChanged` agarra los cambios automáticamente — sin ningún hook especial de reinicio.

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

**Padre → dependencia (`expose:`).** El mapa `dependencies.<llave>.expose:` de un padre nombra volúmenes de la dependencia para montarlos por bind dentro del contenedor del padre. Cada entrada toma una ruta de contenedor y un flag `readonly` opcional (por omisión `true`, ya que los padres normalmente nada más consumen la salida de la dependencia):

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

**Hermana → hermana (`consume:`).** Una lista `dependencies.<llave>.consume:` monta el volumen de una dependencia hermana dentro del contenedor de ESTA dependencia. Cada entrada toma un `from:` (llave de la dependencia hermana en el mismo mapa `dependencies:` del padre), `volume:` (nombre del volumen en el YAML de la hermana), `path:` (ruta de contenedor en la dependencia consumidora) y un `readonly` opcional (por omisión `false`, ya que compartir entre hermanas suele necesitar escritura — p. ej., un *arr importando al `/downloads` de un cliente de descargas):

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

**Orden topológico de instalación.** Las referencias `consume.from` agregan aristas al DAG de tiempo de instalación que construye `orderDependencies`, junto a las referencias que ya existen en respuestas `@dep_LLAVE_*@`. Una dependencia B que consume de la hermana A se instala estrictamente después de A, para que el subvolumen btrfs de A ya exista cuando arranque el contenedor de B. Los ciclos entre aristas de consumo (A consume de B; B consume de A) son un error duro y abortan la instalación antes de aprovisionar ninguna dependencia. El autoconsumo (`from:` igual a la llave de la propia dependencia) se rechaza en tiempo de validación.

**Validación.** La validación en tiempo de compilación rechaza: rutas de montaje relativas o con recorrido de directorios, referencias `consume.from` a llaves que no se declararon en el mismo mapa `dependencies:`, el autoconsumo y las rutas de consumo duplicadas dentro de una misma dependencia. La validación entre paquetes (`shareable: true` en el volumen que corresponde del productor) pasa en tiempo de instalación/reconciliación, cuando se carga el YAML del productor — un padre que expone o consume un volumen no compartible falla la instalación con `volume %q is not marked shareable on %s`.

**Sustitución de plantillas en las rutas.** `expose.<nombrevol>.path` y `consume[].path` participan en la sustitución `@pregunta@` igualito que los puntos de montaje de volúmenes normales. `consume.from` y `consume.volume` (y las llaves del mapa `expose`) son identificadores, no datos, y no se sustituyen.

**Advertencia sobre permisos — los bind mounts pasan el UID/GID tal cual.** El subvolumen btrfs de una dependencia en el host le pertenece al uid:gid con el que lo creó el contenedor de la dependencia. Si la dependencia corre como 1000:1000 (el predeterminado de linuxserver/*) y el padre o la hermana consumidora corre con un uid distinto, el consumidor recibe EACCES al leer o escribir. El arreglo está en el YAML del paquete, no en la plataforma: alinea los valores por omisión de las preguntas `PUID`/`PGID` entre los paquetes que comparten volúmenes. La línea de chown de `HostVolumeMount.UID`/`GID` es a propósito no recursiva y solo aplica cuando el autor de la dependencia los define explícitamente en un montaje escribible; el resolvedor de volúmenes compartidos nunca hace chown automático.

**Espacio de nombres de plantilla.** Los volúmenes compartibles de una dependencia también salen en el espacio de nombres `.Dep` de las plantillas de archivo como `.Dep.<llave>.Volumes.<nombrevol>` (el valor es el punto de montaje del volumen dentro del contenedor de la dependencia). Es paralelo a `.Dep.<llave>.Ports`. Los volúmenes no compartibles se omiten a propósito del mapa para que las plantillas de archivo no puedan alcanzar datos que el autor de la dependencia no aceptó exponer.

**Orden de desinstalación.** Las directivas `Before=`/`PartOf=` que ya existen garantizan que el padre se detiene antes que las dependencias y que las dependencias se detienen antes que sus productoras, así que cuando un padre se desinstala (desinstalando en cascada sus dependencias) el contenedor del consumidor ya no está antes de que se toque el volumen del productor. No hace falta ninguna lógica nueva de desinstalación.

**Fuera de alcance.** Una dependencia le pertenece exactamente a un padre (invariante que ya existe); los volúmenes compartidos no hacen que las dependencias sean multiinquilino. Compartir en dirección inversa (volumen del padre → dependencia) no se acepta en la v1; el esquema queda extensible por si hiciera falta. Los servicios del sistema (`town-os-system--*`) no reciben esta funcionalidad — `GenerateSystemServiceUnit` no consulta `expose`/`consume`.

### Puertos con Nombre

Las referencias a puertos de dependencias pueden usar un nombre semántico en vez de un número de puerto de contenedor. La dependencia declara el nombre como una llave YAML en `network.external` / `network.internal`; los padres referencian el mismo puerto con `@dep_LLAVE_port_NOMBRE@`. Así el número de puerto crudo vive en exactamente un lugar (la dependencia que lo tiene) y el padre puede hablar de roles (`sql`, `http`, `admin`) en vez de trivialidades de protocolo.

**Forma canónica.** La dependencia es dueña del número de puerto — idealmente como valor por omisión de una pregunta `type: port`, para que tanto la autogeneración como la sobrescritura funcionen normal:

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

**Esquema del mapa.** Una entrada de puerto en `network.external` o `network.internal` tiene una llave YAML que es o bien:

- Una cadena de puerto numérico (forma heredada): `"5432": "5432"` → puerto de host 5432 → puerto de contenedor 5432. No se registra ningún nombre.
- Un nombre semántico que coincide con `PortNameRegexp` (`^[a-zA-Z][a-zA-Z0-9_]*$`): `sql: "5432"` → el puerto del contenedor (el valor) hace también de puerto del host, y el nombre `sql` se guarda en `PackageNetwork.{External,Internal}Names[puertoContenedor]`. Los nombres tienen que empezar con una letra (para evitar la ambigüedad con el análisis numérico) y pueden traer alfanuméricos y guiones bajos.

Las dos formas conviven en el mismo mapa; el analizador ramifica según la llave. Un nombre que mapea dos puertos de contenedor distintos, o dos nombres que mapean al mismo puerto de contenedor, es un error en tiempo de compilación. El tipo `Package` compilado gana dos campos `PortNameMap` opcionales junto a los `PortMap` que ya existen; quienes solo se interesan por los puertos numéricos (la generación de unidades, la serialización del estado de red) no ven ningún cambio.

**Emisión de variables de entorno y plantillas.** Por cada puerto de la dependencia compilada, el instalador emite `TOWNOS_DEP_<LLAVE>_PORT_<N>=<N>` (siempre). Si el puerto tiene nombre, emite además `TOWNOS_DEP_<LLAVE>_PORT_<NOMBRE_MAYÚSCULAS>=<N>` con el mismo valor. El resolvedor de plantillas quita el prefijo `TOWNOS_` y pasa el resto a minúsculas, así que tanto `@dep_db_port_5432@` como `@dep_db_port_sql@` se resuelven al mismo valor. El `depKeyRefRegex` de `controller_install_dependencies.go` acepta las dos formas; el ordenamiento topológico de dependencias hermanas reconoce las referencias con nombre al construir el DAG.

**Retrocompatibilidad.** Los paquetes existentes que usan la forma numérica siguen funcionando sin cambios — no se fuerza ninguna migración. Los padres pueden mezclar referencias numéricas y con nombre a la misma dependencia en el mismo archivo. La reconciliación reconstruye las dos formas durante el arranque, así que las instalaciones existentes que sobreviven nunca sufren una regresión.

**Cuándo usar un nombre.** Siempre que un padre referencie el puerto de una dependencia. Un nombre es el único hecho que el padre puede citar; la dependencia es dueña del número. Usa nombres primero para los puertos internos (que es donde vive el tráfico padre-dependencia en la red podman compartida); los puertos externos con nombre se permiten pero son poco comunes, ya que los padres no suelen marcarles a sus dependencias por enlaces del host.

## Redes (Superposiciones WireGuard)

Una **red** es una superposición WireGuard con nombre emparejada con un TLD de DNS. Los paquetes se instalan en una red; los pares se unen a ella; el TLD es lo que divide quién puede resolver qué (ve [TLD de red, doble hogar y resolución split-horizon](#tld-de-red-doble-hogar-y-resolución-split-horizon)).

### Modelo de Red

`account.Network` (`src/account/network.go`) trae: `Name`, `TLD`, `Subnet`, `Address` (la propia dirección de superposición del equipo, siempre el host `.1`), `PublicKey`, `PrivateKey` (nunca se serializa), `ListenPort`, `Enabled` y marcas de tiempo. Los nombres son seguros como etiquetas DNS (`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`, máximo 32 caracteres) porque se reutilizan como sufijos de interfaz WireGuard y como nombres de unidades de systemd.

`Enabled` controla únicamente el *transporte*: cuando es falso la interfaz WireGuard no se levanta, cortando el acceso remoto mientras la resolución DNS local y los propios contenedores siguen corriendo.

### La red del hogar siempre existe

`DefaultNetworkName` es `home`, y la **siembra `account.InitNetworkManager`**, junto con las tablas — no la reconciliación del arranque. Así que está ahí desde el momento en que hay una base de datos: antes de que arranque el controlador, en todos los servidores de prueba y para la primera petición que el equipo sirve en su vida. `account.DefaultNetwork()` es el renglón canónico.

Eso importa porque todo lo que viene después está escrito dándolo por hecho: la primera cuenta está acotada a ella ([Toda cuenta pertenece a la red del hogar](#toda-cuenta-pertenece-a-la-red-del-hogar)), el TLD predeterminado es su TLD, y gfeh le da una partición y sienta ahí al fundador. Un equipo donde hubiera que crearla primero tiene una ventana en la que todo eso es falso — que es lo que hacía que el almacenamiento de objetos se quedara muerto en un primer arranque hasta que algún reinicio posterior encontrara la red ya presente.

**No se puede eliminar** (`ErrNetworkProtected`, y `POST /networks/remove` la rechaza), ni se puede crear una segunda vez — `POST /networks/create` para `home` recibe un 409 de la revisión de colisión de TLD.

Es **solo DNS**: `applyNetworkTransport` no le da interfaz WireGuard, ni subred de superposición, ni pares, así que nunca puede tener un dispositivo tunelizado. Por eso el renglón sembrado **no trae ningún campo de transporte** — subred vacía, sin par de llaves, puerto 0. Esa es la verdad y no un relleno; una subred y unas llaves derivadas serían campos que nadie lee jamás.

**Su TLD viene de `dns_tld`, y el controlador los mantiene sincronizados.** La siembra no puede conocerlo (el paquete de cuentas no tiene administrador de ajustes), así que el renglón llega con el valor por omisión pelón y `ensureDefaultNetwork` lo reconcilia al arrancar, escribiendo solo cuando los dos discrepan. `POST /dns/tld` la reapunta al mismo tiempo que escribe el ajuste. Los dos pasan por `NetworkManager.SetTLD`, que existe exactamente para esto. Equivocarse no es cosmético: `applyNetworkTransport` le pasa `n.TLD` a `rolodex.EnsureNetworkScope`, que decide cuál zona tiene el alcance del hogar.

### Direccionamiento e Interfaces

- **Subred** — `wireguard.SubnetForNetwork(seed, name)` deriva un `/24` determinista a partir de una semilla de identidad del equipo y el nombre de la red. Basarse en la identidad del equipo significa que dos equipos Town OS que sirvan pares escogen subredes distintas, así que un dispositivo que se una a los dos nunca ve una colisión. Las subredes se toman de `10.64.0.0/10` para alejarse de los rangos `10.0`/`10.1` que reparten los routers caseros. La semilla es `networkIPAMSeed()`: el machine-id de systemd, si no el nombre de host, si no una constante, así que la derivación nunca falla — con la sal de instancia metida adentro.
- **Nombre de interfaz** — `wireguard.InterfaceName(salt, name)` es `"town" + 4 hex` de un SHA-256 del nombre de red con sal: estable frente al orden de creación, independiente de cuántas redes existan y dentro del límite de 15 caracteres del kernel. wg-quick deriva la interfaz del nombre del archivo de configuración, así que la configuración se escribe como `<InterfaceName>.conf`. `systemcontroller.NetworkInterfaceName(name)` es la forma con sal aplicada que usan las pruebas de integración, así que una prueba nunca comprueba contra un dispositivo que nadie creó.
- **Puerto de escucha** — `wireguard.ListenPortForName(salt, name)` se desplaza desde `DefaultListenPortBase` (51820) según un hash del nombre con sal, sondeando hacia adelante más allá de un puerto que ya tenga otra red.

#### La sal de la instancia

El nombre de una interfaz WireGuard, su puerto UDP de escucha y su subred de superposición son todos **globales al espacio de nombres**, y los contenedores de prueba y de desarrollo corren los dos con `--net host` (a propósito — el DNS en red bridge se rompe en redes cautivas). Sin una sal, un equipo con `make test-full` y uno con `make dev` derivan el *mismo* nombre de interfaz y el mismo puerto de escucha para el mismo nombre de red: el segundo en levantarse no puede crear su dispositivo, y su superposición simplemente está muerta. Dos árboles de trabajo de prueba al mismo tiempo chocan igual — REGLA DE HIERRO.

`TOWN_OS_WG_SALT` (`EnvWireGuardSalt`) se lee una vez en `wireGuardSalt`. El arnés la pone en `<rol>-<INSTANCE_ID>` con `wireguard_salt` en `make/lib.sh` — el rol separa un equipo de prueba de uno de desarrollo dentro de un mismo checkout, `INSTANCE_ID` separa checkouts, y se necesitan las dos mitades. Es estable para un rol y un checkout dados, lo cual importa para desarrollo, cuya base de datos sobrevive entre corridas y cuyas subredes guardadas apuntarían si no a dispositivos nombrados según la sal anterior. **Un equipo real no pone nada y conserva los nombres históricos sin sal**; una sal vacía devuelve todas las derivaciones intactas.

**Los grupos de subredes predeterminados de podman tienen que quedarse fuera de `10.64.0.0/10`.** La imagen de ejecución escribe `/etc/containers/containers.conf` con `default_subnet_pools = [{"base" = "172.16.0.0/12", "size" = 24}]` justamente porque los valores predeterminados de podman (10.89/16, 10.90/15, 10.96/11, …) caen todos dentro del rango de superposición: los `/24` que caen adentro se saltan por conflicto con las rutas de superposición, el grupo se agota bajo carga con "could not find free subnet from subnet pools" y las redes de contenedor de los paquetes dejan de funcionar. No borres ese archivo ni vuelvas a ensanchar los grupos hacia `10.64.0.0/10`.

El paquete `wireguard` **no controla ninguna interfaz por sí mismo**. Genera pares de llaves y renderiza configuración estilo wg-quick; el systemcontroller escribe la configuración renderizada en el directorio de estado de red compartido con el host y una unidad de systemd generada levanta y baja la interfaz del kernel. Eso es lo que mantiene al contenedor del systemcontroller libre de requisitos sobre el espacio de nombres de red del host.

**El orden importa en `applyNetworkTransport`.** Rolodex se tiene que programar *después* de que la interfaz esté arrancada y la dirección de superposición asignada, sobre un enlace UP y cubierta por una ruta — asignada no es lo mismo que usable. Programarlo primero le pide a rolodex que enlace una dirección que el host todavía no tiene; el enlace falla con `EADDRNOTAVAIL` y el listener muere para siempre, porque rolodex registra un listener cuando lo lanza y el cadáver bloquea después toda reafirmación.

### Pares

`account.NetworkPeer` trae `Network`, `PublicKey`, `Name`, `AllowedIP`, `Endpoint`, `Rolodex`, `CreatedBy`, `ExpiresAt` y `CreatedAt`.

- **`Rolodex`** marca un par que corre un servidor DNS rolodex en su dirección de superposición. El equipo registra entonces esa dirección como reenviador por TLD, así que los nombres bajo el TLD compartido que son autoritativos en el par se resuelven a través de la superposición. Los teléfonos y las laptops lo dejan en falso.
- **`CreatedBy`** es la llave de propiedad: una cuenta con el permiso `wireguard` solo puede refrescar los pares que ella creó, así que una cuenta acotada no puede mantener vivo el par de otra cuenta.
- **`Endpoint`** se deriva de **la dirección que marcó el cliente que se inscribe** (el encabezado `Host` de su petición `peers/add`), no de la visión que el equipo tiene de sí mismo. La IP pública del equipo (de ipinfo.io) o su dirección de LAN son inalcanzables detrás de un NAT, un reenvío de puertos o un relevo — un celular en la misma Wi-Fi no puede dar la vuelta hasta la IP pública y no puede enrutar para nada hacia la dirección privada de la LAN, y el par entonces hace handshake contra el vacío, lo cual el usuario percibe como un DNS roto. La dirección marcada es alcanzable por construcción: la petición llegó por ahí. Sin ninguna dirección marcable (una inscripción por loopback), el endpoint se **omite** en vez de ponerlo en algo que no puede funcionar.

### TTL de Inscripción de Pares y el Segador

Una inscripción no vive para siempre. El ajuste `peer_ttl` (segundos, `7200` por omisión) es cuánto sigue siendo válida. Un cliente de larga vida refresca su par con `POST /networks/peers/refresh` antes de que se acabe; el par de un dispositivo abandonado vence solo, así que el endpoint aditivo `peers/add` no puede acumular pares muertos en silencio y quemar direcciones de superposición. Un `ExpiresAt` nil significa que el par nunca vence — pares permanentes como los servidores rolodex y los dispositivos que agrega el operador.

El vencimiento siempre lo **calcula el servidor** como `ahora + peer_ttl`; quien llama nunca lo escoge. Una goroutine segadora de fondo llama a `ReapExpiredPeers` y luego vuelve a renderizar una vez el transporte de cada red afectada, para que el dispositivo WireGuard vivo y los reenviadores de rolodex tiren los pares segados. Es de mejor esfuerzo e idempotente: el conjunto de pares persistido es la fuente de verdad, y un renderizado fallido lo repara el siguiente tic o la reconciliación del arranque. `peerReapInterval` es una cuarta parte del TTL, con tope en `[1m, 15m]`, así que un par vencido se queda a lo mucho ~TTL/4 más allá de su vencimiento, y ni un TTL chiquito ni uno enorme producen una tasa de barrido patológica.

### Pares Conectados

`GET /networks/peers/connected` junta los renglones persistidos con el estado vivo del kernel de cada túnel. La mitad persistida (nombre, cuenta, dirección de superposición, vencimiento) contesta "quién tiene permiso"; la mitad de `wg show <iface> dump` (handshake, endpoint observado, transferencia) contesta "quién está de veras aquí ahorita" — ninguna de las dos por sí sola es la pregunta, y por eso existe `ConnectedPeerView` en vez de reutilizar `account.NetworkPeer`.

El análisis vive en el `wireguard.ParseDump` puro. El **primer** renglón de un volcado describe la interfaz misma y se salta a propósito; tratarlo como un par fabricaría un fantasma que trae la llave de la propia interfaz. Los marcadores `(none)` y `off` de `wg` se decodifican en vez de pasarse tal cual como cadenas literales.

**La conectividad es un handshake dentro de la ventana `REJECT_AFTER_TIME` de 180 s de WireGuard** (`HandshakeStaleAfter`) — la única señal de vida que ofrece el protocolo. No hay cierre de sesión, así que un par que se va no se distingue de uno que está inactivo hasta que su handshake se pasa de viejo. Un par que *nunca* hizo handshake conserva una marca de tiempo nil en vez de la época, porque "nunca se configuró" y "lleva un día desconectado" son hechos distintos sobre un dispositivo.

El systemcontroller corre con `--net host`, así que ya comparte el espacio de nombres donde wg-quick creó el dispositivo; la imagen de ejecución incluye `wireguard-tools` únicamente por el binario `wg` (wg-quick sigue corriendo en el host, desde las unidades generadas). Una interfaz ausente no es un error — una red deshabilitada, o una cuyo transporte no subió, sencillamente no tiene pares vivos y sus renglones persistidos se tienen que renderizar de todos modos — y una falla del volcado degrada a los renglones persistidos en vez de dejar el panel en blanco. La red `home` queda excluida por completo: no tiene transporte, así que incluirla pondría un renglón permanentemente desconectado en un panel que trata de quién está tunelizado.

**Desconectar reutiliza `POST /networks/peers/remove`** en vez de agregar un endpoint. WireGuard no tiene ninguna sesión que matar, así que eliminar el par es la única terminación forzosa que hay.

### API de Redes

- `GET /networks` (requiere autenticación) -- lista todas las redes con el conteo de pares, el nombre de interfaz derivado y el estado de ejecución. La llave privada nunca se expone.
- `POST /networks/create` (requiere admin) -- crea una red. Acepta el nombre y un TLD opcional (por omisión el nombre). Deriva la subred, genera un par de llaves, asigna un puerto de escucha y devuelve la red creada. Un nombre o un TLD que ya estén tomados dan un 409 — incluido `home`, que siempre existe.
- `POST /networks/remove` (requiere admin) -- elimina una red por nombre. La red del hogar no se puede eliminar.
- `POST /networks/enable` / `POST /networks/disable` (requiere admin) -- levanta o baja la interfaz de superposición.
- `GET /networks/peers?network=<nombre>` (requiere autenticación, y confinado por `requireNetworkScope`) -- lista los pares registrados en una red. La ruta está en la lista blanca del permiso `wireguard`, así que una cuenta acotada llega a ella, y una lista de pares nombra dispositivos, las cuentas que los inscribieron y sus direcciones de superposición — un permiso otorgado es autoridad sobre las redes propias de quien llama, y una lectura es donde más fácil se olvida.
- `GET /networks/peers/connected` (**requiere admin**) -- todos los pares de todas las redes WireGuard juntados con el estado vivo del túnel. A propósito más estricto que sus hermanas `requireAuth` y ausente de `grantRoutes`.
- `POST /networks/peers/add` (`requirePeerEnroll`: admin o el permiso `wireguard`, confinado a las redes de quien llama) -- registra un par. Cuando `public_key` está vacío, el servidor genera un par de llaves y devuelve la llave privada más una configuración de dispositivo lista para importar. Acepta un `endpoint` opcional y una marca `rolodex`.
- `POST /networks/peers/refresh` (`requirePeerEnroll`, y solo para un par que inscribió quien llama) -- extiende el TTL de un par en `peer_ttl` y devuelve el nuevo vencimiento, para que un cliente pueda acomodar su siguiente latido con buen margen antes de que se acabe el TTL.
- `POST /networks/peers/remove` (requiere admin) -- elimina un par por su llave pública.

### Interfaz de Redes

`/dashboard/networks` lista las redes con acciones de crear/eliminar/habilitar/deshabilitar e inscripción de pares por red. Un segundo panel de **Pares Conectados** detalla todos los pares de todas las redes WireGuard — el dispositivo, la cuenta que lo inscribió, su dirección de superposición, el endpoint desde el que está marcando, el estado vivo de handshake y transferencia y el vencimiento de su inscripción — con una acción de Desconectar por renglón.

## TLS y la CA Local

Town OS corre su propia autoridad certificadora X.509 para que el tráfico de paquetes y páginas se sirva por HTTPS y por nombre, sin ninguna CA pública ni dependencia de ACME en la LAN.

- **La CA** (`src/tls/ca.go`) es un par de llaves ECDSA P-256 bajo el subvolumen btrfs `tls` (`ca.crt`, `ca.key`), con validez de 10 años, para que sobreviva a los reinicios. `EnsureCA` carga una CA existente o genera una bajo demanda; el certificado es legible por todos y la llave es solo del dueño y nunca se debe servir. Una falla de la CA no es fatal — el sistema arranca sin HTTPS en vez de no arrancar.
- **Las hojas** (`src/tls/leaf.go`) son por paquete y por página, escritas como `cert.pem`/`key.pem` en un solo directorio para que quien las consuma necesite una sola ruta de montaje. `IssueLeaf` es **idempotente**: cuando un certificado existente ya cubre exactamente el conjunto de SAN que se pide y sigue siendo válido, regresa sin tocar el disco, que es lo que deja que la reconciliación lo llame en cada arranque sin andar moviendo los archivos de certificado. Los nombres de host pueden ser nombres DNS o literales de IP; todo lo que se analiza como IP se va a `IPAddresses`, y todo lo demás a `DNSNames`.
- **`GET /tls/ca.crt`** es **público** (y está en `grantCommonRoutes`) para que cualquier cliente — un navegador, un celular que se une por la superposición — pueda bajar la raíz y confiar en el equipo.

El conjunto de SAN de la hoja de un paquete se deriva del mismo FQDN único que su registro A, su propietario DANE TLSA y su vhost del ingress; ve [El FQDN del paquete es una sola cadena](#el-fqdn-del-paquete-es-una-sola-cadena--registro-a-san-de-la-hoja-propietario-tlsa-vhost-del-ingress). Las hojas también traen la IP de superposición del equipo en la red de instalación, para que un par pueda alcanzar el paquete por su dirección WireGuard cruda y no solo por nombre.

## Ingress

El ingress es el router de Host compartido: un sidecar que supervisa un hijo Caddy y expone una API de administración gRPC que el systemcontroller programa, igual que programa rolodex. Guarda el conjunto de rutas deseado en memoria, renderiza un Caddyfile en cada cambio y recarga Caddy sin tiempo caído.

- **`src/ingress`** es el servicio dentro del contenedor (`Server`, `renderCaddyfile`, el cliente gRPC, el binario `town-os-ingress`). Se compila con `CGO_ENABLED=0`.
- **`src/ingress/ingressctl`** es el controlador de ciclo de vida del lado del systemcontroller: genera, instala y reinicia la unidad `town-os-system--ingress` y expone la ruta del socket gRPC que marca el systemcontroller. Es un paquete aparte justamente para que el binario del ingress, libre de CGO, nunca importe `src/systemd` (que arrastra cgo por sdjournal).

### Enrutado

- **`:443`** — un vhost `https://<hostname>` por ruta, terminando TLS con la hoja de la CA local fijada al archivo de esa ruta, o con un emisor ACME explícito para un FQDN público, y haciendo proxy inverso al contenedor de backend en la red podman compartida `town-os-ingress`.
- **`:80`** — enrutado por Host: las páginas (`ServeHttp`) se sirven directo por HTTP simple (contenido estático, nada sensible), los paquetes reciben una redirección HTTP→HTTPS para que se queden solo en HTTPS, y cualquier host que no coincida con una ruta cae al backend predeterminado — la interfaz de Town OS, para que el inicio de sesión por IP pelona (`http://<ip-del-equipo>/`) siga funcionando ahora que la interfaz ya no ocupa el `:80` del host.
- Una ruta **sin hoja emitida todavía** (no ACME, directorio de certificados vacío) se salta para HTTPS, así que una entrada a medio aprovisionar nunca hace que Caddy rechace toda la configuración; una página sigue obteniendo su vhost de `:80`, que no necesita certificado. Los paquetes solo se redirigen una vez que el destino HTTPS de veras existe, así que nada redirige hacia un certificado que todavía no se aprovisiona.

### Renderizado

La salida está **ordenada por nombre de host** para que los bytes renderizados sean deterministas entre reconciliaciones — eso es lo que deja al supervisor no hacer nada ante una recarga cuyo contenido no cambió. Los globales son `auto_https off` (Town OS administra los certificados) y `protocols h1 h2` (el ingress publica solo TCP, así que H3/QUIC sobre UDP no se alcanza). La API de administración de Caddy se deja a propósito **habilitada** en su `localhost:2019` local al contenedor: el supervisor programa las rutas nuevas con `caddy reload`, que habla con ese endpoint, así que `admin off` rompería todas las actualizaciones de rutas después del primer arranque.

El ingress es **agnóstico a la interfaz**: publica `-p 443:443` / `-p 80:80` sin ninguna IP de host y su Caddyfile no trae **ninguna directiva `bind`**, así que Caddy escucha en todas las interfaces y escoge el vhost puramente por SNI/Host. Un cliente de la LAN y un par de la superposición llegan al mismo listener, escogen por SNI el mismo vhost, obtienen la misma hoja de la CA local y se les hace proxy al mismo contenedor. No agregues directivas `bind` ni listeners por red.

Producción enlaza 443/80; las pruebas de integración pasan puertos efímeros (renderizados como `host:PUERTO`) para que `make test-full` nunca choque en un puerto privilegiado. El arranque programa el conjunto completo de rutas de forma declarativa con `RebuildIngress`, el mismo modelo de empuje que `RebuildDNS`; el CRUD de paquetes y páginas programa cambios incrementales por la misma API gRPC.

## Estado de Arranque y Refresco

`:5309` se enlaza antes de que pase cualquier trabajo de arranque, para que la interfaz pueda ver avanzar un arranque — incluida una autoactualización — en vez de sondear un puerto muerto.

### El Stub de Arranque

`NewBootHandler` es un `http.ServeMux` pelón (a propósito, para que nunca pueda montar por accidente una ruta real de la API) que sirve exactamente tres cosas:

- `GET /status/ping` → `{booting, step, done, error, boot_id}`. Contesta **503 mientras arranca** y 200 una vez que termina, para que los sondeos externos de disponibilidad — el `wait_for_url` del contenedor de pruebas, las revisiones de salud de un orquestador — no traten el stub como "servicio listo" y empiecen a machacar un controlador a medio arrancar. El cuerpo JSON sigue trayendo los campos de progreso, así que la interfaz puede distinguir "levantándose" de "totalmente caído".
- `GET /boot-status` → un flujo SSE de eventos de progreso.
- todo lo demás → **403**, no 404: la ruta existe en el manejador completo, nada más no está disponible hasta el intercambio.

`RootHandler.Swap` reemplaza atómicamente el stub por el router Echo completo al final del arranque. El socket del listener nunca se cierra, así que no hay parpadeo de puerto, y los manejadores SSE ya despachados conservan su propio escritor y siguen transmitiendo a través del intercambio.

### Etapas de Progreso

Cinco etapas gruesas, a propósito pocas y de cara al usuario — una persona que ve una autoactualización quiere saber si es "el controlador", "el DNS", "los servicios del sistema" o "mis paquetes" lo que está frenando las cosas, no cuál constructor interno está corriendo:

`boot_controller` → `boot_dns` → `boot_services` → `restart_packages` → `ready`

La etapa de frescura emite un evento adicional por paquete instalado, con el prefijo `restarting_` (`PackageStepPrefix`); la interfaz le quita el prefijo y renderiza cada uno como su propio renglón, con el mismo peso que las etapas gruesas, así que un equipo con muchos paquetes muestra progreso real en vez de una sola barra atorada. Estos nombres por paquete a propósito no coinciden con la forma `[a-z0-9_]+` que se exige a las etapas fijas — son valores dinámicos.

Los literales de las etapas están duplicados como llamadas `bs.Step("...")` en `main.go` en vez de referenciarse como constantes, porque `TestBootStepsFrontendInSyncWithBackend` los saca de `main.go` para demostrar que la lista del frontend coincide. **Mantén los dos sincronizados**; esa prueba falla de forma ruidosa si se separan.

### Semántica de la Difusión

`BootStatus` es seguro para uso concurrente y **nunca bloquea el arranque**. `Subscribe` reproduce primero el historial hacia el suscriptor nuevo (para que un suscriptor tardío no se pierda nada), dimensionando el búfer para que quepa la reproducción completa más margen; si el arranque ya terminó, cierra el canal justo después de la reproducción para que los consumidores con `for range` salgan. `publish` envía sin bloquear — un suscriptor cuyo búfer se llena se descarta y se cierra, y su cliente se reconecta y obtiene la reproducción del historial. Ningún evento puede ir después de `Done`.

### Identidad del Proceso y Refresco

`boot_id` es un UUID aleatorio que se regenera en cada arranque del systemcontroller, reportado por **los dos** `/status/ping`, el del stub y el del router completo (y presente incluso en la respuesta mínima de ping sin autenticar, ya que un navegador se queda un ratito sin token a través de un reinicio). Un cliente que capturó el identificador antes de pedir un refresco puede distinguir "el proceso viejo sigue contestando" (mismo identificador) de "el proceso nuevo ya está arriba" (identificador distinto) — si no, son indistinguibles, porque los dos sirven un ping 200 y los dos dan 404 en `/boot-status` una vez arrancados. Esto es lo que le permite al flujo de Refrescar Servicios Centrales de la interfaz ver a su propio sucesor.

`/boot-status` se excluye del registro de auditoría por la misma razón: una interfaz que mantiene el flujo abierto a través del intercambio del manejador aterriza su siguiente petición en el router completo, que da 404. Ese es el final esperado del flujo, no una acción del operador — auditarlo archivaría un renglón de acción fallida en cada refresco exitoso e inflaría la píldora roja de fallas del tablero.

`POST /system-services/refresh` (admin) baja la imagen de todos los servicios del sistema en orden de dependencias — primero la imagen del systemcontroller (el ancla de versión, para que la imagen recién bajada ya esté local cuando se autorreinicie al final), luego rolodex (el DNS del equipo, que las demás descargas pueden necesitar para resolver su registro) y después todo lo demás en paralelo (máximo 3 al mismo tiempo) — y deja una marca que la etapa de frescura del siguiente proceso consume para reiniciar los paquetes instalados.

## Administración de DNS (Rolodex)

Town OS incluye un resolvedor DNS local integrado impulsado por un contenedor `rolodex-dns`. El servidor rolodex administra archivos de zona y registros para los paquetes instalados, dando resolución de nombres local con una interfaz gRPC sobre socket Unix.

### Administrador de Rolodex

Rolodex es en sí mismo un servicio de arranque que systemd instala y supervisa — el systemcontroller no lo instala, arranca, detiene ni reinicia a nivel de contenedor. En cambio, el `rolodex.Manager`:

- **`WriteConfig`** -- escribe `rolodex.yml` en `DataDir`. Idempotente: se salta la escritura cuando el archivo existe, es más nuevo que el binario del systemcontroller y ya coincide con el contenido esperado. Devuelve un booleano que indica si el archivo se escribió (para que quien llama pueda decidir si reinicia la unidad de systemd).
- **`WaitForDNSReady`** -- sondea `DNSLoopback:{puerto}` por TCP hasta que acepta una conexión o pasa la fecha límite de 30 segundos. Se llama al arrancar, antes de cualquier operación que dependa del DNS (p. ej., descargas de imágenes).
- **`SystemServices`** -- devuelve los metadatos del servicio de sistema rolodex (llave, nombre a mostrar, imagen, puerto, nombre de unidad) para que salga junto a los demás servicios del sistema en las respuestas de estado y en la interfaz.
- **`Status`** -- consulta el estado de la unidad de systemd para reportar si rolodex está corriendo.

El contenedor de rolodex corre con `--net host` y enlaza el DNS a `DNSLoopback` (`127.0.0.2`) en el puerto configurado (`53` por omisión, sobrescribible con `DNSPort` para las pruebas). La etiqueta de la imagen se deriva de la etiqueta de publicación del controlador del sistema (`quay.io/town/rolodex:<tag>`), sobrescribible con la variable de entorno `ROLODEX_IMAGE`.

**Modo de resolución.** `rolodex.yml` fija `resolution.mode` explícitamente con `Config.ResolutionMode`, con valor predeterminado **`auto`** (`DefaultResolutionMode`) — la propia cadena escalonada de respaldo de rolodex: iterar desde los servidores raíz, luego DoH/DoT, luego la lista `forwarders:`, luego un resolvedor público en el :53, quedándose con el escalón que funcionó la última vez. El modo se escribe explícito en vez de dejarlo al valor predeterminado de rolodex, para que el comportamiento de Town OS no se mueva cuando el proyecto de origen cambie su predeterminado. La prueba de integración de reenvío escoge `ResolutionModeForward` y apunta los reenviadores a un stub local.

**No uses `recursive` pelón por omisión.** *No* tiene ningún respaldo, y el resolvedor iterativo de rolodex (`src/resolver.rs`) manda **un solo datagrama UDP sin retransmisión por servidor de nombres con una fecha límite de 1500 ms**; cuando todos los servidores del conjunto de delegación actual fallan, `resolve()` da error e `iterative_query` convierte *cualquier* error en SERVFAIL. Así que un solo paquete perdido produce SERVFAIL en una consulta, y en una red que filtra o secuestra el :53 saliente (hotel, portal cautivo, algunos ISP) *todos* los nombres externos dan SERVFAIL. `auto` conserva la privacidad de la recursión donde la red lo permite y degrada en vez de fallar donde no. Relacionado: la caché de delegación y la caché negativa de rolodex aterrizaron en `ce44bb5`, que **no está en ninguna etiqueta publicada** — hasta que salga una versión con eso, el modo recursivo vuelve a recorrer desde las raíces cada nombre que no está en caché y cada NXDOMAIN (medido: 0.6–1.9 s por nombre público en frío, 2.7 s para un PTR de RFC1918).

El modo lo puede configurar el operador en tiempo de ejecución con el ajuste `dns_resolution_mode` (`auto` | `recursive` | `forward`; validado por `ValidateDNSResolutionMode`, así que un valor que no se pueda analizar nunca puede llegar a `rolodex.yml` y dejar inservible el DNS). `main.go` lo lee en `rolodex.Config` al arrancar; un cambio con `POST /settings/set` corre `Controller.RefreshDNSResolutionMode`, que llama a **`Manager.RewriteConfig()`** y reinicia la unidad de rolodex. `RewriteConfig` existe justamente porque `WriteConfig` se niega a sobrescribir un `rolodex.yml` más nuevo que el binario del systemcontroller (lo trata como editado a mano) — y el archivo que se escribió en el arranque anterior *siempre* cumple esa condición, así que `WriteConfig` no haría nada en silencio ante un cambio que inició el operador. Usa `WriteConfig` al arrancar y `RewriteConfig` para los cambios en tiempo de ejecución.

### Reenviadores locales

La lista `forwarders:` que Town OS escribe por omisión es `DefaultForwarders` — resolvedores públicos. En una red que bloquea el DNS externo (un hotel, un portal cautivo, un ISP que tira el `:53` saliente hacia cualquier cosa que no sean sus propios servidores) esas son justamente las direcciones que se están tirando, así que el escalón de reenviadores de `auto` — el escalón al que se llega *después* de que las raíces y los upstreams cifrados ya fallaron, que es exactamente este caso — no tiene a qué recurrir. El resolvedor que esa red repartió por DHCP sí contesta.

El ajuste `dns_local_forwarders` (`false` por omisión, validado por `ValidateBool`) reemplaza la lista de reenviadores con los resolvedores a los que apunta la propia configuración de red de este equipo. **No es un modo de resolución**: cambia *cuáles* direcciones trae el escalón local, y el modo sigue decidiendo si ese escalón se consulta siquiera — en `auto` es el último recurso, en `forward` es el único upstream, en `recursive` no se usa. Prenderlo, entonces, nunca debe mover el modo.

**Apagado es lo predeterminado, y es la dirección que importa.** El resolvedor local ve todos los nombres que busca la casa, que es justo lo que resolver desde las raíces existe para evitar. Ese es un intercambio que un operador hace a sabiendas, no uno que un equipo hace por él la primera vez que una red se porta mal.

El descubrimiento vive en `src/rolodex/hostdns.go`. `HostResolversFrom` lee `hostResolvConfPaths` en orden — `/run/systemd/resolve/resolv.conf` **primero**, luego `/etc/resolv.conf` — y gana el primer archivo que produce una dirección usable, no nada más el primer archivo que existe. El orden carga peso: en un equipo con resolved, `/etc/resolv.conf` es el stub (`127.0.0.53`), que se descarta por ser loopback, así que un descubrimiento que se detuviera en el primer archivo *legible* no encontraría nada justo en los equipos para los que existe esta función. El archivo del enlace de subida se alcanza desde dentro del contenedor porque la unidad del systemcontroller monta por bind `-v /run/systemd:/run/systemd`; perder ese montaje degrada el descubrimiento en silencio. Las direcciones de loopback, sin especificar, multicast y link-local se descartan todas — reenviar al stub de resolved o al propio listener `DNSLoopback` de rolodex es un bucle de consultas, no un upstream, y una dirección link-local no tiene sentido sin la zona que un renglón de `resolv.conf` no trae.

**Un descubrimiento que no encuentra nada conserva los reenviadores que ya estaban configurados.** `Manager.forwarders()` se va a `Config.Forwarders` y luego a `DefaultForwarders`, así que prender el interruptor nunca puede dejar el escalón local apuntando a nada — lo cual sería estrictamente peor que los valores públicos predeterminados que se prendió para reemplazar.

`main.go` lee el ajuste en `rolodex.Config` al arrancar (un valor guardado que no se puede analizar se lee como apagado — la dirección segura), así que un equipo que cambió de red agarra el resolvedor nuevo en el siguiente arranque sin que el operador haga nada. Un cambio con `POST /settings/set` corre `Controller.RefreshDNSLocalForwarders`, que — a diferencia del modo de resolución — **no** hace cortocircuito cuando la marca no cambia: con ella ya prendida, las direcciones descubiertas mismas pueden haberse movido, y volver a renderizar es como eso llega a rolodex. `RewriteConfig` sigue reportando si los bytes de veras cambiaron, así que un renderizado idéntico no cuesta ningún reinicio.

`GET /dns/status` reporta **los dos**: `local_forwarders` (lo que pidió el operador) y `forwarders` (lo que de veras trae `rolodex.yml`). Discrepan en exactamente un caso — el descubrimiento no encontró nada usable y se conservaron los valores públicos por omisión — que es el único caso en el que el interruptor se lee como prendido y no cambia nada, así que una interfaz que mostrara solo la marca estaría mostrando un ajuste que no está en vigor. La pantalla de Ajustes renderiza la lista efectiva por esa razón, y lo dice explícito cuando está vacía.

**La imagen de rolodex se baja por arquitectura en pruebas y en desarrollo** — el arnés de make baja la etiqueta rc por arquitectura del host `quay.io/town/rolodex:rc.latest-<arch>` (donde `<arch>` es la forma cruda de `uname -m`, `x86_64`/`aarch64`), NO la `rc.latest` simple sin arquitectura. Las descargas internas de imágenes de Town OS usan por omisión el canal rc, así que el arnés, el entorno de desarrollo y la ejecución siguen todos `rc.latest-<arch>`. Rolodex publica etiquetas por arquitectura que se suben de forma nativa desde cada host (`make push-rc` / `make push-release` en el repositorio rolodex-dns), así que no hace falta ensamblar ningún manifiesto multiarquitectura para hosts de prueba de ninguna arquitectura; la `rc.latest` *simple* (sin sufijo de arquitectura) es un manifiesto de una sola arquitectura y se cae en bucle con `exec format error` en la otra arquitectura — solo la `rc.latest-<arch>` con sufijo es segura de bajar directo. El Makefile calcula `HOST_ARCH` (normalizado a `x86_64`/`aarch64`) y usa por omisión `ROLODEX_IMAGE_TAG ?= rc.latest-$(HOST_ARCH)`; `ROLODEX_IMAGE` se deriva de ella y se inyecta en los contenedores de prueba/desarrollo por el entorno. Sobrescríbela con `make ROLODEX_IMAGE_TAG=<tag> ...` (p. ej. `latest-$(HOST_ARCH)` para un rolodex publicado) o con la variable de entorno `ROLODEX_IMAGE`. El comportamiento en producción/ejecución coincide — el systemcontroller deriva la etiqueta de su etiqueta de publicación (yéndose a `rc.latest-<arch>` con `defaultVersionTag()`) a menos que `ROLODEX_IMAGE` esté definida; los arneses de prueba y de desarrollo siempre la definen. La unidad de rolodex incrustada en el contenedor de desarrollo (`integration/testdata/town-os-system--rolodex.service`) usa un marcador `@ROLODEX_IMAGE@` que se sustituye al construir la imagen con el argumento de compilación `ROLODEX_IMAGE` en `integration/testdata/Containerfile.dev` (la compilación falla si el argumento está vacío), así que la unidad incrustada siempre coincide con la imagen que carga el arnés.

### TLD de red, doble hogar y resolución split-horizon

Cada red tiene un TLD, registrado en rolodex como un alcance de red cuyo
`home_domain` es el TLD (`rolodex.EnsureNetworkScope`, llamado desde
`applyNetworkTransport` en `controller_networks_reconcile.go`). Tener el TLD es lo
que lo **divide**: rolodex esconde el TLD de un alcance de cualquier par WireGuard
unido a un alcance *distinto*. La red predeterminada/del hogar
(`account.DefaultNetworkName`, con el TLD del ajuste `dns_tld`, `home` por
omisión) tiene `home.` como un alcance **solo DNS** — no obtiene interfaz
WireGuard, ni subred de superposición, ni asociación de pares, así que ninguna IP
de origen se enlaza jamás al alcance del hogar. Por eso `.home` es solo de LAN y
está escondido de todos los pares WireGuard, pero se resuelve perfecto en la LAN.

**Doble hogar.** Un paquete instalado en una red que no es la predeterminada se
publica dos veces (`registerScopedPackageDNS`):

- un registro A **acotado** bajo el TLD de la red en la **IP de superposición** del
  equipo — que se sirve a los pares de la superposición WireGuard por IP de origen
  (`AddScopedRecord`); y
- un registro A **global** para el mismo FQDN en la **IP de LAN** del equipo
  (`RegisterPackageDNS`) — que se sirve a los clientes de loopback/LAN.

Cada lado recibe una dirección a la que de veras puede enrutar. No se publica
ninguna zona autoritativa global para el TLD de la red: un registro A global pelón
se resuelve en la LAN sin zona, y el **respaldo LAN→alcance propietario** de
rolodex (rolodex-dns, paso 5 de resolución) trata el TLD que tiene el alcance como
autoritativo para los orígenes de LAN — así que un nombre sin coincidencia bajo un
TLD de red produce un NXDOMAIN autoritativo desde la LAN, en vez de filtrar el TLD
privado río arriba. Los paquetes de la red predeterminada se quedan solo en la zona
global del hogar (`registerPackageDNS`); un paquete que no es de la predeterminada
nunca debe salir ahí (el error original de "se resuelve como `.home`").

**Resumen del split-horizon.** Un cliente de LAN (sin WireGuard) resuelve **todos**
los TLD de red (`.home` y el TLD de todas las redes WireGuard) más el internet
público. Un par WireGuard unido a una red resuelve **solo** el TLD de esa red más
el internet público — el TLD de una red hermana y `.home` devuelven los dos
NXDOMAIN. La vista de la LAN nunca se divide; solo los pares de la superposición.
`RebuildNetworkDNS` (`reconcile.go`, que se llama al arrancar) vuelve a registrar
el registro global que da a la LAN de cada paquete de red no predeterminada, para
que un paquete ya instalado siga resolviendo en la LAN después de un reinicio; los
registros acotados persisten en rolodex por separado. A la reconciliación de redes
del arranque se le pasa el cliente de rolodex para que el alcance del hogar (y
todos los alcances de red) queden establecidos incluso en un arranque en frío.

### El FQDN del paquete es una sola cadena — registro A, SAN de la hoja, propietario TLSA, vhost del ingress

**El nombre DNS de un paquete siempre se deriva del TLD de su *red de
instalación*, nunca del ajuste global `dns_tld`.** `packageFQDN(repo, name, tld)`
(`src/svc/systemcontroller/controller_tls.go`) es la única fuente de verdad, y el
TLD viene de `networkTLDValue(nm, settingsMgr, network)` (que se va a `dns_tld`
solo para la red predeterminada). Cuatro cosas tienen que nombrar un paquete
idéntico, y un desajuste en cualquiera de ellas rompe el servicio en silencio:

1. su **registro A**, 2. el **SAN de su certificado hoja**, 3. su **propietario
DANE TLSA**, y 4. su **vhost del ingress compartido en :443**.

Para que no se separen, el FQDN se calcula **una sola vez** — en `applyPackageTLS`,
en el mismo renglón que emite la hoja — y se persiste como
`PackageNetworkState.FQDN` (`fqdn` en el JSON de estado de red por paquete). El
constructor de rutas del ingress (`collectPackageIngressSites`) lee ese campo en
vez de recomponer el nombre, así que el vhost es por construcción el nombre para el
que el certificado es válido. `reconcileWriteNetworkState` toma el TLD **de quien
la llama** (`reconcilePackage`, que lo resolvió desde la red de instalación); nunca
debe llamar a `reconcileDNSTLD` ella misma. Hacerlo fue un error real: cada
arranque reemitía la hoja de un paquete de la red `fart` con el SAN
`<pkg>.<repo>.home`, machacando el SAN `.fart` correcto, mientras el ingress
renderizaba un vhost `<pkg>.<repo>.home` que nadie marcaba — así que el paquete se
resolvía en la LAN pero nunca se servía. Un `fqdn` vacío (archivo de estado previo
a la actualización, o un paquete que no es HTTP) se va al TLD global y se
autorrepara en la siguiente reconciliación.

**El ingress es agnóstico a la interfaz y no necesita ningún enlace por red.**
Publica `-p 443:443` / `-p 80:80` sin IP de host (`0.0.0.0`, así que la LAN +
WireGuard + loopback llegan todos) y su Caddyfile no tiene **ninguna directiva
`bind`**, así que Caddy escucha en todas las interfaces y escoge el vhost puramente
por **SNI/Host**. A los backends se llega por nombre de contenedor en la red podman
compartida `town-os-ingress`, a la que se une todo paquete con frontal HTTP sin
importar su red WireGuard. Un cliente de LAN y un par de la superposición llegan
entonces al mismo listener, escogen por SNI el mismo vhost, obtienen la misma hoja
de la CA local y se les hace proxy al mismo contenedor. Nada enlaza un socket de
escucha a una IP de superposición — `BindOverlayAddress` es una *asociación de
alcance DNS* de rolodex, no un enlace de socket. No le agregues directivas `bind`
ni listeners por red al ingress.

La hoja del paquete también trae la **IP de superposición** del equipo en esa red
como SAN (`networkOverlayIPValue`), para que un par pueda alcanzar el paquete por
la dirección WireGuard cruda (`https://10.65.0.1`) y no solo por nombre. Está vacía
para la red predeterminada (que no tiene transporte WireGuard), lo cual evita que
las hojas de la red predeterminada se anden moviendo en cada reconciliación.

El DANE TLSA de un paquete de red está en **doble hogar como su registro A**:
`RebuildNetworkDNS` registra un anclaje global (que se sirve a los orígenes de LAN
por el respaldo LAN→alcance propietario) *y* un anclaje acotado (que se sirve a los
pares de la superposición, cuyas consultas nunca ven registros globales). La
instalación por sí sola solo escribía la mitad acotada, y nada republicaba ninguna
de las dos mitades a través de un reinicio.

### Las páginas también están acotadas por red

Una página trae una `network` (la columna `PageSite.Network`; `""` significa la red
predeterminada/del hogar, la misma convención que `Installer.LoadNetwork` para los
paquetes) y recibe **exactamente el mismo trato que un paquete**: su nombre viene
del TLD de esa red, está en doble hogar (registro acotado de superposición +
registro global de LAN), su hoja trae el FQDN de la red más la IP de superposición
del equipo, su DANE TLSA se ancla bajo el TLD de la red (global + acotado) y está
escondida de los pares de todas las *demás* redes. `pageFQDN` (`pages_tls.go`) es
el gemelo del lado de las páginas de `packageFQDN`, y `pageNetworkTLD` el de
`networkTLDValue`.

La particularidad de las páginas: el FQDN de una página **también nombra su
subvolumen btrfs en disco y su enlace simbólico de webroot** (el Caddy de pages
tiene su raíz en `/srv/<host>`). Así que el FQDN no es nada más una etiqueta —
equivócate y el contenido se sale de debajo del nombre que sirve el ingress. Tres
consecuencias:

- `reconcilePages` construye su conjunto `valid` con `pageFQDN`, porque ese conjunto
  maneja `pruneStalePageSymlinks` — nombrar ahí `blog.home` a una página de `fart`
  fallaría en encontrar su directorio real `blog.fart` *y además* podaría el enlace
  simbólico vivo.
- Cambiar la **red** de una página renombra su subvolumen/enlace simbólico
  (`migratePageDir`), igualito que hace un cambio de `dns_tld` con las páginas de la
  red predeterminada.
- `migratePageDirsForTLD` (el manejador del cambio de `dns_tld`) **se salta las
  páginas de redes que no son la predeterminada** — no están nombradas bajo el TLD
  global, así que renombrarlas rompería una página que estaba funcionando.

Las páginas las sigue sirviendo el único contenedor compartido
`town-os-system--pages` detrás del ingress; la red es nada más una cuestión de
nombres/DNS/certificados, sin ningún contenedor ni plomería de podman por red.

### API de DNS

- `GET /dns/status` (requiere autenticación) -- devuelve el estado del DNS, incluidas la marca de habilitado, el estado de ejecución, el TLD, el conteo de registros, `local_forwarders` (si la lista de reenviadores se toma de los resolvedores del propio host) y `forwarders` (las direcciones que de veras trae `rolodex.yml` — ve [Reenviadores locales](#reenviadores-locales)).
- `GET /dns/records` (requiere autenticación) -- lista todos los registros DNS.
- `POST /dns/records/add` (requiere admin) -- agrega un registro DNS. Acepta nombre, tipo de registro, valor y TTL.
- `POST /dns/records/remove` (requiere admin) -- elimina un registro DNS por nombre y tipo.
- `GET /dns/tld` (requiere autenticación) -- obtiene el dominio de nivel superior actual.
- `POST /dns/tld` (requiere admin) -- define el TLD. Cambia el TLD existente y vuelve a registrar todos los paquetes instalados.
- `POST /dns/setup` (requiere admin) -- inicializa el DNS y registra todos los paquetes instalados.
- `GET /dns/rbl` (requiere autenticación) -- obtiene la configuración de RBL (Realtime Blackhole List, IP inversa): la marca global de habilitado, las zonas de proveedor con sus códigos de rechazo **resueltos a lo que está en vigor**, el `refusal_cooldown_secs` de toda la lista y `rotated_out` (los proveedores que ahorita están apartados tras rechazar una consulta, con el código y los segundos que faltan). Ve [Códigos de rechazo](#códigos-de-rechazo-que-un-proveedor-diga-que-dejes-de-preguntar-no-significa-que-esto-esté-listado).
- `POST /dns/rbl` (requiere admin) -- reemplaza la configuración de RBL. Acepta una marca de habilitado, un `refusal_cooldown_secs` para toda la lista y una lista de proveedores `{zone, enabled, refusal_codes, refusal_cooldown_secs}`. Las zonas se validan como nombres de host completamente calificados, se pasan a minúsculas, se recortan y se deduplican; los códigos de rechazo los valida `ValidateRefusalCodes` (dirección IPv4 o `dirección/prefijo`, enmascarada al prefijo, `"none"` solo como entrada única, sin duplicados).
- `GET /dns/dnsbl` (requiere autenticación) -- obtiene la configuración de DNSBL (lista de bloqueo de dominios, por nombre directo), con la misma forma que `/dns/rbl`.
- `POST /dns/dnsbl` (requiere admin) -- reemplaza la configuración de DNSBL (misma forma y validación que `/dns/rbl`; su enfriamiento de rechazo es independiente del de la RBL).
- `GET /dns/rbl/local` (requiere autenticación) -- lista las entradas de la lista de bloqueo RBL local (`{name, reason}`).
- `POST /dns/rbl/local/add` (requiere admin) -- agrega una entrada RBL local. Acepta un nombre (dominio o IP) y un motivo opcional. El nombre se valida (dominio o IP), se pasa a minúsculas y se recorta.
- `POST /dns/rbl/local/remove` (requiere admin) -- elimina una entrada RBL local por nombre.
- `GET /dns/dnsbl/allowlist` (requiere autenticación) -- lista las entradas de la lista de permitidos de DNSBL (`{name, reason}`).
- `POST /dns/dnsbl/allowlist/add` (requiere admin) -- exenta un nombre de la revisión de la lista de bloqueo por nombre. Acepta un nombre y un motivo opcional. El nombre se pasa a minúsculas, se recorta y se valida **solo como nombre de dominio** -- un literal de IP se rechaza (`ValidateDnsblAllowlistName`), porque la lista de permitidos compara nombres y sus subdominios y nunca podría coincidir con una dirección.
- `POST /dns/dnsbl/allowlist/remove` (requiere admin) -- elimina una entrada de la lista de permitidos por nombre. El nombre se normaliza pero no se vuelve a validar, así que una entrada anterior a un cambio de validación de todos modos se puede eliminar.
- `GET /dns/services` (requiere autenticación) -- lista los servicios de paquetes instalados con su estado de publicación (en la zona DNS) (`{repo, name, version, fqdn, domains, published}`), deduplicados por repositorio/nombre.
- `POST /dns/services/set` (requiere admin) -- publica o quita de la publicación un servicio de paquete en la zona DNS. Acepta `{repo, name, published}`. Persiste la decisión y registra/desregistra los registros de inmediato.

Los endpoints DNS de solo lectura (`/dns/status`, `/dns/records`, `/dns/rbl/local`, `/dns/dnsbl/allowlist`, `/dns/services`, `GET /dns/tld`, `GET /dns/rbl`, `GET /dns/dnsbl`) se excluyen del registro de auditoría. Las *escrituras* de la lista de permitidos sí se auditan (exentar un nombre de todas las listas de bloqueo es un cambio del que hay que rendir cuentas); igual que las escrituras de listas de bloqueo que reflejan, no traen ninguna acción con nombre en `account.RouteActions` — la ruta las identifica.

### Listas de bloqueo RBL / DNSBL

Rolodex (0.2.4+) da tres mecanismos complementarios de bloqueo de spam/malware/anuncios, más (0.4.3+) un mecanismo para deshacerlos y otro para no creerle a un proveedor que rechazó la consulta, todos expuestos por la API de DNS y la envoltura `rolodex.Client` (`SetRblConfig`/`GetRblConfig`, `SetDnsblConfig`/`GetDnsblConfig`, `AddLocalRblEntry`/`RemoveLocalRblEntry`/`ListLocalRblEntries`, `AddDnsblAllowlistEntry`/`RemoveDnsblAllowlistEntry`/`ListDnsblAllowlistEntries`). Todos los **consulta rolodex bajo demanda** — Town OS nunca baja, analiza ni precachea fuentes de listas de bloqueo.

Fíjate en que los dos métodos `Set*` de la envoltura toman el enfriamiento de rechazo de toda la lista como argumento final (`SetRblConfig(ctx, enabled, providers, refusalCooldownSecs)`); se mapean sobre los `Set*ConfigWithRefusalCooldown` del proyecto de origen, ya que las grafías originales que conservan la aridad existen por compatibilidad de API externa, algo que una envoltura interna no necesita.

- **RBL** (Realtime Blackhole List) -- zonas de lista de bloqueo por IP inversa que se consultan bajo demanda con una IP invertida contra una zona (p. ej. `zen.spamhaus.org`). Se revisan contra las IP que aparecen en consultas DNS inversas. Se configura con `/dns/rbl` como una lista de proveedores `{zone, enabled, refusal_codes, refusal_cooldown_secs}` más una marca global de habilitado y un `refusal_cooldown_secs` para toda la lista.
- **DNSBL** (lista de bloqueo de dominios) -- zonas de lista de bloqueo de dominios que se consultan bajo demanda anteponiendo el dominio que se busca a la zona (p. ej. `googleadservices.com` + `dbl.spamhaus.org`). Las coincidencias de DNSBL le ganan a las respuestas reenviadas/iterativas. Se configura con `/dns/dnsbl` con la misma forma que la RBL, con su propio enfriamiento independiente.
- **Entradas RBL locales** -- una lista respaldada por la base de datos con nombres/IP que se administra a mano con `/dns/rbl/local*`, que se revisa antes que los proveedores externos. Una entrada local de **nombre de dominio** bloquea las búsquedas directas A/AAAA de ese dominio con `NXDOMAIN`, y surte efecto de inmediato (rolodex actualiza una caché en memoria al agregarla).
- **Lista de permitidos de DNSBL** (rolodex 0.4.3+) -- la salida de emergencia del operador ante un falso positivo de una fuente de terceros, administrada con `/dns/dnsbl/allowlist*`. Una entrada cubre el nombre **y todos los nombres debajo de él**, así que permitir `vendor.example` también exenta a `cdn.vendor.example`. **Hace cortocircuito de toda la revisión por nombre**, ganándoles tanto a los proveedores DNSBL configurados como a cualquier entrada RBL local que coincida, y corre *antes* de la búsqueda del proveedor, así que un nombre exentado nunca emite ninguna. También está respaldada por la base de datos con una caché en memoria, así que surte efecto de inmediato.

  Sin ella, el único remedio ante una fuente que lista un nombre que la casa necesita es deshabilitar todo el proveedor. Fíjate en la asimetría con la lista de bloqueo local: una entrada de la lista de permitidos es **solo un nombre**, nunca una IP, porque la revisión que cortocircuita es la que se basa en nombres. La ruta RBL basada en IP no se ve afectada por ella.

  **Versión mínima:** un rolodex viejo contesta las tres RPC de la lista de permitidos con `Unimplemented` de gRPC, que sale como un 500. Ni `make test` ni las pruebas de integración con mocks lo detectan — `TestRolodexDnsblAllowlistRoundtripReal` es lo que demuestra que la imagen fijada es lo bastante nueva.

#### Códigos de rechazo: que un proveedor diga que dejes de preguntar no significa que esto esté listado

Un DNSxL contesta una coincidencia y una queja sobre quien consulta con el **mismo tipo de registro** — un `A` bajo `127.0.0.0/8` — así que lo único que los separa es la dirección. `127.0.0.2` significa que el nombre está listado; `127.255.255.254` significa que la consulta llegó por un resolvedor público y `127.255.255.255` significa que quien consulta se pasó de su límite. Lee el segundo tipo como una coincidencia y **todos** los nombres que se revisen contra ese proveedor se vuelven `NXDOMAIN`: la lista de bloqueo deja de ser una lista de bloqueo y se vuelve una caída del servicio. Spamhaus publica límites de uso gratuito que un equipo casero puede cruzar sin darse cuenta, y el síntoma cuando lo hace es que toda la web se apaga — lo cual se lee como un DNS roto, no como un límite de tasa.

Rolodex reconoce estos códigos y, ante un rechazo, **aparta a ese proveedor de la rotación de búsquedas durante un enfriamiento** en vez de creerle. Town OS expone las dos mitades:

- **`refusal_codes`**, por proveedor, en las dos listas. Cada entrada es una dirección IPv4 o `dirección/prefijo` — un prefijo porque los proveedores documentan rangos enteros, y Spamhaus aparta todo `127.255.255.0/24` para errores y le agrega códigos con el tiempo, así que enumerar los tres de hoy haría que mañana el cuarto se leyera en silencio como una coincidencia.
- **`refusal_cooldown_secs`**, por proveedor y para toda la lista. Un `0` en un proveedor se va al valor de la lista; un `0` en la lista usa el valor predeterminado integrado de rolodex (3600).
- **`rotated_out`** en el `GET`, reportando a cuáles proveedores no se les está preguntando ahorita, el código con el que rechazó cada uno y los segundos que faltan. Esta es la mitad que ve el operador: sin ella, la única señal de que una lista de bloqueo dejó de consultarse es que dejó de bloquear cosas.

**`ValidateRefusalCodes` (`controller_dns_validate.go`) refleja exactamente el `resolve_refusal_codes` de rolodex**, porque la lista se pasa tal cual y discrepar sobre lo que significa una entrada sería peor que no validar nada. Tres casos:

- **vacío** ⇒ rolodex sustituye su conjunto integrado, así que una configuración escrita antes de que nada de esto existiera recibe la lectura segura sin que la editen;
- **exactamente `"none"`** ⇒ detección apagada, para una lista de bloqueo privada cuyas coincidencias reales chocan con un código integrado;
- **cualquier otra cosa** ⇒ exactamente esos códigos, con los integrados a propósito **sin** juntar.

`"none"` mezclado con códigos reales se rechaza — una lista que a la vez apaga la detección y nombra códigos que detectar no tiene ninguna lectura que escoger. Los códigos se enmascaran a su prefijo y **un `/32` se renderiza pelón**, coincidiendo con el `Display` de rolodex: un código que se leyera de regreso distinto del que se acaba de enviar parecería que el equipo reescribió lo que puso el operador.

**El `GET` reporta los códigos RESUELTOS**, así que un proveedor que no nombró ninguno se lee de regreso trayendo el conjunto integrado — que es la gracia, ya que un operador tiene que poder ver contra qué está comparando el equipo de verdad. También significa que **un cliente nunca debe devolver eso tal cual en el siguiente guardado**: hacerlo congela la lista de hoy dentro de la configuración guardada, con lo cual un código que rolodex agregue después empieza a leerse como una coincidencia — exactamente la falla que esto existe para evitar, reintroducida una capa más arriba. `toWire` en `BlocklistsTab.jsx` colapsa un conjunto integrado resuelto de regreso a un campo ausente, y la interfaz guarda una copia de la lista integrada (`BUILTIN_REFUSAL_CODES`) con un solo propósito: decidir con cuál radio se abre el diálogo de ajustes. Si esa copia se desvía, el diálogo se abre en "Custom" precargado con los códigos en vigor — un valor predeterminado equivocado y cosmético, no una configuración equivocada, ya que nada cambia a menos que el operador guarde.

**Versión mínima:** un rolodex anterior al manejo de rechazos acepta estos campos — proto3 ignora los campos desconocidos — y no guarda nada. Las pruebas con mocks no pueden distinguir eso del éxito, porque un mock devuelve lo que le entregaron. `TestRolodexRblRefusalCodesRoundtripReal` y su gemela de DNSBL comprueban que una lista configurada **vacía** se lee de regreso *resuelta*, que es la comprobación que una imagen vieja no pasa.

**No hay ingestión ni precacheo de fuentes**: las zonas de proveedor son la unidad de configuración, y la interfaz ofrece una lista curada de zonas DNSBL/RBL conocidas como agregados rápidos de un clic, pero el usuario puede agregar cualquier zona. Las escrituras de zonas de proveedor reemplazan toda la configuración (validada, en minúsculas y deduplicada).

**La lista de agregados rápidos es un respaldo, y se cura sobre esa base** (`DNSBL_SUGGESTIONS` / `RBL_SUGGESTIONS` en `ui/src/routes/dns/BlocklistsTab.jsx`). Una zona pertenece ahí solo si un equipo casero puede usarla tal cual: que siga operando, sea gratis y le conteste a un resolvedor que recursa por su cuenta sin ningún paso de registro. Ahorita DNSBL — Spamhaus DBL, SURBL, URIBL, NordSpam DBL, Spam Eating Monkey; RBL — Spamhaus ZEN, SpamCop, PSBL.

Tres están a propósito **ausentes**, y el caso "offers no decommissioned or registration-gated zones" de `TestBlocklistsTab` las mantiene así: `dnsbl.sorbs.net` se desmanteló el 2024-06-05 y sus zonas se vaciaron, así que es una operación permanentemente sin efecto que se lee como protección; `b.barracudacentral.org` exige registrar antes la IP que consulta, y un equipo sin registrar puede contestar un rato y luego quedar cortado; los niveles 2/3 de UCEPROTECT listan ASN enteros, así que un solo vecino malo bloquea a todo un ISP. Las tres fallan *en silencio* — el operador ve una zona configurada y da por hecho que funciona.

Fíjate además en que las zonas RBL (IP inversa) solo se consultan para las IP que aparecen en consultas DNS inversas, que la navegación normal casi no genera. Las zonas DNSBL (de dominios) son las que afectan la navegación, y están afinadas para URL de spam en el correo más que para anuncios o rastreadores — el bloqueo de anuncios/rastreadores sería territorio de fuentes, que está [a propósito fuera de alcance](#listas-de-bloqueo-rbl--dnsbl).

### Publicación de DNS por Servicio

La publicación es de exclusión voluntaria: todo servicio de paquete instalado se publica en la zona DNS a menos que su llave `repo/name` esté listada en el ajuste `dns_excluded_services` (un arreglo JSON). `/dns/services/set` prende y apaga la pertenencia y registra/desregistra los registros de inmediato; `RebuildDNS` y `ReconcileDNS` filtran los servicios excluidos (con `filterExcludedDNSInfo` + `loadDNSExcludedServices`), así que la decisión sobrevive a reinicios y reconciliaciones. Los servicios que no se publican siguen corriendo pero no se resuelven por nombre.

### Interfaz de Administración de DNS

La pantalla de administración de DNS muestra el estado del DNS (habilitado, corriendo, TLD, conteo de registros) arriba de cuatro subpestañas con enlace directo (`?tab=`):

- **Records** -- la tabla de registros DNS con diálogos para agregar registros (tipos: A, AAAA, CNAME, MX, TXT, SRV, PTR), eliminar registros, cambiar el TLD y la configuración inicial del DNS.
- **Blocklists** -- las secciones de zonas de proveedor DNSBL y RBL (interruptor global de habilitado, habilitar/eliminar por zona, ajustes de códigos de rechazo por zona, agregados rápidos de zonas sugeridas, agregar zona personalizada — todo consultado bajo demanda) más una tabla manual de entradas locales (agregar/eliminar). Cada sección empieza con los proveedores que ahorita están apartados tras rechazar una consulta, cuando los hay. Sin fuentes, sin aplicar, sin nada en caché.
- **Allow Lists** (`?tab=allowlists`, `ui/src/routes/dns/AllowListsTab.jsx`) -- la lista de permitidos de DNSBL: una tabla de dominios exentados con sus motivos, más agregar y eliminar. Las lecturas son `requireAuth`, así que la pestaña no es solo para administradores; los controles de agregar/eliminar sí lo son. Es una pestaña hermana en vez de una tarjeta dentro de Blocklists porque una exención es lo que un operador va a buscar por nombre cuando algo no se alcanza, no algo que uno se encuentra mientras se desplaza más allá de las zonas de proveedor.
- **Services** -- los servicios de paquetes instalados con un interruptor de publicación (publicar/quitar de la zona DNS).

## Endpoint de Estado

`GET /status/ping` (público) devuelve el estado del sistema, incluidos: conteos de sistemas de archivos (de usuario, instalados, desinstalados), conteos de repositorios y paquetes, conteo de paquetes instalados, conteos de cuentas y de administradores, conteos de unidades de servicio (total, activas, fallidas), conteos de unidades de servicios del sistema (total, activas, fallidas), errores de auditoría recientes (últimos 5 minutos), estado de configuración inicial (`needs_setup` es verdadero solo cuando no existe ninguna cuenta de administrador habilitada; la página de inicio de sesión se muestra cuando hay administradores sin importar el estado de la sesión), IP externa (que se obtiene cada hora de ipinfo.io), IP interna (la primera dirección IPv4 que no sea de loopback), estadísticas de uso de disco, disponibilidad de actualizaciones, el desplazamiento UTC del servidor en minutos, la configuración regional actual, `proton_enabled` (si esta compilación trae la etiqueta de compilación `proton`), `boot_id` y el nombre de usuario autenticado si se da un token válido.

Los conteos de unidades de servicio se dividen en dos campos: `units` cuenta solo las unidades de servicio de paquetes (las que coinciden con `town-os-package--*`), mientras que `system_services` cuenta las unidades de servicios del sistema (las que coinciden con `town-os-system--*`). Las unidades de systemd que quedaron de paquetes desinstalados se excluyen del conteo de paquetes. La lista de paquetes instalados se cruza con las unidades de systemd que se descubren, construyendo el nombre de unidad esperado a partir de la identidad de cada paquete.

El manejador lista las cuentas una sola vez (se usa para `needs_setup`, el total y el conteo de administradores) y usa `FilesystemNames` en vez de `ListFilesystems` para los conteos de volúmenes — este último corre `btrfs qgroup show` más una búsqueda de rootid por subvolumen, lo que con ~30 subvolúmenes costaba como un segundo del presupuesto de latencia del ping por una cuota que el ping nunca lee.

Las peticiones sin autenticar que vienen de orígenes que no son localhost reciben una respuesta mínima que trae solo `status`, `needs_setup` y `boot_id`. `boot_id` viaja incluso ahí porque el flujo de refresco sondea el ping a través de un reinicio del controlador, durante el cual el navegador se queda un ratito sin autenticar; es un UUID aleatorio por proceso y no revela nada del sistema. Las peticiones autenticadas y todas las peticiones desde localhost reciben la respuesta completa con todos los campos de arriba, más `repository_errors` (un mapa de nombre de repositorio a cadena de error que registra las fallas de refresco por repositorio).

Mientras el controlador todavía está arrancando, esta ruta la sirve el stub de arranque y devuelve **503** con `{booting, step, done, error, boot_id}` — ve [Estado de Arranque y Refresco](#estado-de-arranque-y-refresco).

### Sondeo de la IP Externa

El controlador del sistema obtiene la dirección IP pública (externa) del servidor de `https://ipinfo.io/json`. El sondeador arranca automáticamente cuando se crea el manejador HTTP (`NewHandler`) y cuando arranca el servidor sobre socket Unix. Obtiene la IP de inmediato al arrancar y luego sondea cada hora. Cada obtención tiene un tiempo límite HTTP de 10 segundos. El resultado se guarda en caché en un valor atómico y se incluye en las respuestas de ping autenticadas como `external_ip`. Las fallas al obtenerla se registran a nivel de depuración y no afectan al resto del sistema; el campo se omite de la respuesta cuando no se ha obtenido ninguna IP.

## Monitoreo

Una pila integrada de monitoreo Prometheus + Node Exporter da métricas del sistema. El `monitoring.Manager` administra la pila como contenedores podman supervisados por systemd (servicios del sistema) con `Restart=always`, usando el prefijo de nombre `town-os-system--`. El frontend de los tableros se configura con el ajuste `monitoring_backend`.

### Puerto de Monitoreo

El puerto **5308** es el puerto dedicado del tablero de monitoreo (`TOWN_OS_MONITORING_PORT` lo mueve; igual `TOWN_OS_PROMETHEUS_PORT` y `TOWN_OS_NODE_EXPORTER_PORT` con los dos puertos de loopback — ve [Puertos de host de los servicios del sistema](#puertos-de-host-de-los-servicios-del-sistema)). Los puertos llegan a los tres servicios como un solo valor `monitoring.Ports` cuyos campos vacíos rellena `withDefaults()`, así que los valores por omisión viven en un solo lugar. El backend activo determina qué escucha en el puerto del tablero:

- **Modo uPlot** (predeterminado): un reenviador socat (`socat TCP-LISTEN:5308,fork,reuseaddr TCP:localhost:9090`) expone la API HTTP de Prometheus en el puerto 5308. La interfaz React consulta `/api/v1/query_range` de Prometheus directo y renderiza las gráficas con uPlot.
- **Modo Grafana**: Grafana escucha directamente en el puerto 5308 (con el mapeo de puertos de podman). La interfaz React incrusta un iframe de Grafana.

**No hay ningún proxy inverso** por el systemcontroller (puerto 5309). El navegador habla directamente con el puerto 5308 para todos los datos de monitoreo.

### Ajuste del Backend de Monitoreo

El ajuste del sistema `monitoring_backend` controla cuál frontend de tableros se usa:

- `"uplot"` (predeterminado) -- gráficas ligeras integradas renderizadas en la interfaz React con uPlot (~35 KB). Consulta Prometheus en el puerto 5308 por el reenviador socat. Grafana no se baja ni se arranca, ahorrando ~771 MB en el primer arranque.
- `"grafana"` -- tableros completos de Grafana. La imagen de contenedor de Grafana se baja y se arranca en el puerto 5308. Preaprovisionada con una fuente de datos de Prometheus y con todos los tableros del registro.

Cambiar el ajuste surte efecto de inmediato: cambiar a `"grafana"` baja la imagen de Grafana y arranca el contenedor (deteniendo el reenviador socat); cambiar a `"uplot"` detiene Grafana y arranca el reenviador socat.

### Contenedores de Monitoreo

- **Node Exporter** (`quay.io/prometheus/node-exporter:latest`, puerto de host 9100) -- recolecta métricas del sistema anfitrión. Corre con el espacio de nombres PID del host, la capacidad `SYS_TIME` y un bind mount de solo lectura del sistema de archivos raíz del host en `/host`. La unidad de systemd pasa `--collector.diskstats.device-exclude=^(ram|fd)\d+$` (la constante `monitoring.DiskstatsDeviceExclude`) para sobrescribir el valor predeterminado de origen de node_exporter (`^(ram|loop|fd|(h|s|v|xv)d[a-z]|nvme\d+n\d+p)\d+$`), que filtra las particiones (`sda3`, `nvme0n1p3`) y los dispositivos de bucle — exactamente las formas de dispositivo que `monitoring.BtrfsDevices` reporta para el sistema de archivos btrfs que respalda `/town-os`. Sin esa sobrescritura, las consultas del tablero de E/S de Disco devuelven en silencio cero series y el panel se renderiza vacío. No quites ni aflojes el flag a menos que también muevas las consultas de E/S de Disco fuera de `node_disk_*`. Cobertura de regresión: `TestNodeExporterUnitConfigDiskstatsExcludeAllowsRealDevices` fija el flag y la expresión regular, y `TestMonitoringNodeExporterEmitsDiskMetricsForFilteredDevices` arranca un contenedor real de node_exporter y confirma que emite `node_disk_read_bytes_total` para por lo menos un dispositivo que el predeterminado de origen excluye.
- **Prometheus** (`quay.io/prometheus/prometheus:latest`, puerto de host 9090) -- recolecta de Node Exporter, de sí mismo, de rolodex (trabajo `rolodex`) y del controlador del sistema (trabajo `systemcontroller`, ve [Métricas del controlador del sistema](#métricas-del-controlador-del-sistema)) cada 15 segundos. Los dos trabajos opcionales se omiten cuando su dirección no está definida, en vez de apuntarlos a un valor adivinado, ya que un objetivo que nadie configuró se queda permanentemente caído y se lee como un servicio roto en vez de como uno ausente. Los datos se guardan con retención de 30 días en un directorio de datos persistente. Los volúmenes de configuración y datos se montan por bind desde un directorio de datos de monitoreo. La unidad de systemd incluye directivas `ExecStartPre` de mkdir para crear de antemano los directorios de volumen al arrancar.
- **Grafana** (`docker.io/grafana/grafana:latest`, puerto de host 5308) -- interfaz opcional de tableros, que solo arranca cuando `monitoring_backend` es `"grafana"`. Usa un tema claro (`GF_USERS_DEFAULT_THEME=light`). La visualización anónima está habilitada con el rol Viewer y se permite incrustarlo en iframe. La unidad de systemd incluye directivas `ExecStartPre` de mkdir para crear de antemano los directorios de volumen al arrancar. Preaprovisionada con una fuente de datos de Prometheus y con los tableros que describe [Paneles](#paneles); ve [Aprovisionamiento de paneles](#aprovisionamiento-de-paneles) para cómo llegan ahí.
- **Reenviador socat** -- la unidad `monitoring-ui` (`town-os-system--monitoring-ui.service`) en su forma uPlot, que arranca solo cuando `monitoring_backend` es `"uplot"`. Reenvía el puerto 5308 a Prometheus en el puerto 9090. Es la *misma llave de unidad* que usa Grafana, no una segunda: las dos son cuerpos alternativos de un mismo servicio, que es lo que deja que un cambio de backend sea una reescritura y un reinicio de la unidad en vez de un par de llamadas de arranque/parada que podrían dejar corriendo las dos o ninguna.

### Paneles

Hay dos tableros, y **los dos backends renderizan los mismos dos a partir de las
mismas consultas**. Están separados en vez de ser una sola página larga porque
contestan preguntas distintas: System es lo que un operador mira cuando el equipo
se siente lento, DNS es lo que abre cuando un nombre no se resuelve. Meter los
ocho paneles de DNS dentro de la vista general enterraría los cuatro paneles de
host, que son la razón por la que cualquiera la abre.

**System** (uid de Grafana `town-os-overview`, "Town OS Overview") -- cuatro paneles:

1. **E/S de disco (/town-os)** -- rendimiento de lectura/escritura sumado a través de los dispositivos de bloque que respaldan el sistema de archivos btrfs, de modo que el panel muestra una línea de Lectura y una de Escritura por más dispositivos que abarque el sistema de archivos. La expresión regular de dispositivos se sustituye desde `monitoring.BtrfsDevices`; una lista vacía se resuelve a `NoBtrfsDevicesSentinel`, que no coincide con nada, así que el panel se renderiza vacío en vez de sumar en silencio todos los discos del host.
2. **Red (externa)** -- recepción/transmisión en bits/s por dispositivo físico (excluyendo `lo`, veth, podman, cni, tailscale, bridge y docker), juntado contra `node_network_up == 1` para que las interfaces que existieron alguna vez pero ahora están caídas no dibujen líneas planas en cero en la leyenda.
3. **Uso de CPU** -- apilado por modo (user, system, iowait, irq, softirq, steal, nice) con una línea superpuesta de Total, 0--100%.
4. **Uso de memoria** -- total, usada, disponible.

**DNS** (uid de Grafana `town-os-dns`, "Town OS DNS") -- ocho paneles sobre el
trabajo de recolección `rolodex`:

1. **Consultas DNS por código de respuesta** -- `rate(rolodex_dns_queries_total)` sumado por `rcode`, apilado. La separación es el panel y no un desglose porque un conteo de consultas pelón no puede distinguir un resolvedor ocupado de uno que le da SERVFAIL a todo — son la misma línea.
2. **Latencia de consultas** -- p50/p95/p99 de `rolodex_dns_query_duration_seconds_bucket`. Los cubos se suman por `le` *antes* de `histogram_quantile`, porque las series crudas traen una etiqueta `proto` y sacar cuantiles sin agregarlas dibuja una línea por transporte en vez de la latencia de todo el equipo.
3. **Respuestas por origen** -- cuál etapa de resolución contestó (caché, local, acotada, un escalón upstream), apilado. Este es el panel que dice si el equipo está contestando por sí mismo o reenviando.
4. **Proporción de aciertos de caché** -- aciertos más aciertos negativos sobre todas las búsquedas, 0--100%. Un NXDOMAIN en caché cuenta como acierto: se ahorró una ida y vuelta upstream igual que uno positivo. El denominador está a propósito sin tope, así que un equipo inactivo rompe la línea en vez de dibujar un 0% muy seguro para una caché a la que nadie le ha preguntado nada.
5. **Entradas de caché** -- tamaños de las cachés positiva, negativa y de listas de bloqueo.
6. **Actividad de listas de bloqueo** -- bloqueos por tipo, permitidos y **rechazados**. Los rechazos comparten panel con los bloqueos a propósito: un proveedor que contesta "deja de preguntar" en vez de "listado" es lo que en silencio convierte una lista de bloqueo en una caída ([Códigos de rechazo](#códigos-de-rechazo-que-un-proveedor-diga-que-dejes-de-preguntar-no-significa-que-esto-esté-listado)), y solo se lee como anómalo junto a la tasa de bloqueos que reemplazó.
7. **Resultados por escalón upstream** -- éxitos y fallas por escalón, más las consultas que agotaron todos los escalones.
8. **Tráfico DNS** -- bytes de cable rx/tx.

Todas las consultas de DNS traen un selector `{job="rolodex"}` construido a partir
de `monitoring.RolodexJobName`, así que la etiqueta que emite la configuración de
recolección y la que seleccionan los tableros no se pueden separar — un desajuste
no es un error en ningún lado, son ocho paneles leyendo vacío en un equipo cuyo
DNS está funcionando.

Los dos frontends son código aparte en lenguajes aparte renderizando el mismo
tablero, y la **única** diferencia es la ventana de tasa: Grafana expande
`$__rate_interval` por panel, y el frontend de uPlot no tiene expansión de macros,
así que fija `RATE_INTERVAL` (`5m`). Una macro que se cuele del lado de uPlot es un
error de análisis de Prometheus que deja en blanco toda la pestaña.

Tres pruebas mantienen unidos los dos lados, porque nada más los conecta:

- `TestRolodexDashboardMirroredInFrontendQueries` lee `ui/src/components/monitoring/queries.js` desde la prueba de Go y falla si cualquiera de los dos lados nombra una familia de métricas de rolodex que el otro no — la misma guarda contra la deriva que `TestBootStepsFrontendInSyncWithBackend` aplica a las etapas de arranque.
- La prueba de integración de recolección de rolodex comprueba que la **imagen fijada de rolodex de veras exporta** todas las familias de `monitoring.RolodexDashboardMetrics()`, comparando por el renglón `# TYPE` para que una familia cuyo nombre es prefijo de otra no pueda salir de aval por una que falta. Un panel que nombra una familia que el demonio no emite renderiza una gráfica vacía, que no se distingue de un resolvedor inactivo.
- `TestDashboardQueriesParseInPrometheus` pasa todas las expresiones de todos los tableros por un Prometheus real. Un PromQL malformado dentro de un JSON no es un error de sintaxis en ningún lado: el archivo se aprovisiona, el tablero carga, el panel dibuja sus ejes y dice "No data" para siempre.

### Aprovisionamiento de paneles

`monitoring.GrafanaDashboards(diskDevices)` (`src/monitoring/dashboard.go`) es el
registro — nombre de archivo, uid, título y JSON renderizado por tablero — y
`WriteGrafanaProvisioningFiles` lo recorre. Agregar un tablero es una entrada ahí y
nada más: el aprovisionador (`GrafanaDashboardProviderYAML`) apunta al
**directorio** `dashboard-json`, así que se recoge todo archivo que haya adentro.
Antes de que existiera el registro, el escritor de archivos era la lista de facto,
lo que significaba que un segundo tablero solo se podía agregar editando código que
no tiene nada que ver con tableros.

Los uid son constantes (`OverviewDashboardUID`, `DNSDashboardUID`) porque la
interfaz web enlaza directo a ellos. Un uid desviado no produce ningún error en
ningún lado — Grafana sirve una página de "dashboard not found" dentro del iframe.

El tablero de DNS se **construye a partir de especificaciones de panel y se
serializa** (`src/monitoring/dashboard_dns.go`) en vez de concatenarse dentro de
una plantilla JSON, como todavía hace el tablero de vista general más viejo. Un
JSON malformado en un tablero no cuesta un panel; hace fallar el aprovisionamiento,
y el tablero no aparece para nada. Los objetivos de los paneles traen la referencia
de fuente de datos en forma de objeto (`{"type":"prometheus","uid":GrafanaDatasourceUID}`)
— Grafana 13+ no puede resolver la forma vieja de cadena en un objetivo y renderiza
"No data" sin ningún error.

### Ciclo de Vida

Prometheus y Node Exporter siempre arrancan al inicio. El ajuste del backend de monitoreo determina si además arranca Grafana o el reenviador socat. Las fallas de arranque no son fatales; el sistema sigue sin monitoreo. Systemd se encarga de los reinicios con su política `Restart=always`. El método `Stop()` no hace nada porque los servicios del sistema persisten a través de los reinicios del controlador.

### API de Monitoreo

- `GET /monitoring/status` (requiere autenticación) -- devuelve `backend` (`"uplot"` o `"grafana"`), una marca de ejecución por servicio (`prometheus`, `node_exporter`, `monitoring_ui`, y `grafana` solo en modo Grafana), y `disk_devices`: los nombres base de dispositivo del kernel que respaldan el sistema de archivos btrfs, que el frontend sustituye en la consulta de E/S de Disco. Un `disk_devices` vacío significa que el descubrimiento falló y el panel se va a una expresión regular que no coincide con nada. Devuelve `{"status": "disabled"}` cuando el monitoreo no está configurado. Los metadatos de imagen y unidad por servicio no están aquí — eso es `GET /system-services`.
- `GET /metrics` (localhost o admin) -- el propio endpoint de Prometheus del controlador del sistema. Ve [Métricas del controlador del sistema](#métricas-del-controlador-del-sistema).

### Métricas del controlador del sistema

El controlador exporta su propio estado en el formato de exposición de texto de Prometheus en **su listener que ya existe** (`:5309`, `MetricsPath = "/metrics"`), no en un puerto propio. Eso es a propósito: el endpoint viaja entonces sobre el listener que el arnés ya mueve con `TOWN_OS_LISTEN`, así que no hay ningún puerto de host adicional que agregar a `SYSTEM_PORT_FILES` ni forma de que un `make test-full` y un `make dev` choquen ahí — REGLA DE HIERRO.

Es de **localhost o admin**, no público. La recolección junta conteos de cuentas, uso de disco y cuáles servicios están caídos: un mapa de qué atacar y de cuándo el equipo está menos listo para resistir. Prometheus corre con `--net host`, así que llega al loopback sin ningún salto por la red de podman, igualito que el objetivo de node-exporter.

`src/metrics` renderiza el formato en unos cientos de renglones en vez de depender de `prometheus/client_golang`, por la misma razón por la que `errgroup` se quedó afuera. El valor de la biblioteca es su registro, su interfaz de recolectores y su maquinaria de histogramas — nada de lo cual se usa, ya que todos los valores de aquí son o un conteo de la vida del proceso o una lectura de un administrador por recolección — mientras que su árbol transitivo (`prometheus/common`, `procfs`, protobuf) es real y aterriza en una imagen que arranca desde RAM.

**El escapado de los valores de etiqueta carga peso, no es defensivo.** Los valores de etiqueta traen entrada del operador (un nombre de repositorio, un nombre de paquete, una unidad de systemd). Una comilla sin escapar no corrompe un renglón — hace que Prometheus rechace *toda* la recolección, así que un paquete con un nombre raro tiraría en silencio todo el monitoreo.

Lo que se exporta:

| Métrica | Tipo | Notas |
|---|---|---|
| `townos_up` | gauge | siempre 1 mientras sirve; ausente cuando no |
| `townos_start_time_seconds` | gauge | el tiempo encendido es `time() - esto`, en el reloj de quien recolecta |
| `townos_package_units{state}` | gauge | `active`/`failed`/`inactive`, filtrado a paquetes instalados |
| `townos_system_units{state}` | gauge | `town-os-system--*`, excluyendo unidades NC y de socket |
| `townos_package_unit_active{unit}` | gauge | 1/0 por unidad, para que el operador vea *cuál* servicio está caído |
| `townos_system_unit_active{unit}` | gauge | ídem para los servicios del sistema |
| `townos_packages_installed` / `townos_packages_available` | gauge | inventario |
| `townos_repositories` / `townos_repository_errors` | gauge | los errores se cuentan, no se etiquetan por nombre |
| `townos_upgrades_available` | gauge | |
| `townos_accounts{kind}` | gauge | `admin`/`user`/`disabled` |
| `townos_accounts_granted` | gauge | no administradores con por lo menos un permiso otorgado |
| `townos_filesystems{state}` | gauge | `user`/`installed`/`uninstalled` |
| `townos_disk_total_bytes` / `_used_bytes` / `_available_bytes` | gauge | |
| `townos_audit_recent_errors` | gauge | el mismo número que renderiza la píldora roja del tablero |
| `townos_audit_events_total{result}` | counter | `success`/`failure`, que incrementa `auditMiddleware` |
| `townos_http_requests_total{method,status}` | counter | el estado es una **clase** (`2xx`…), nunca el código exacto |

Varias de estas decisiones son la gracia y no algo incidental:

- **Una recolección nunca falla como bloque.** Cada recolector tolera un administrador nil y registra-y-se-salta ante un error. Un 500 porque un subsistema está enfermo hace desaparecer todas las demás métricas justo cuando se las quiere, así que el equipo se lee como completamente muerto en vez de parcialmente degradado — y una recolección durante el arranque debería reportar lo que sí está arriba.
- **Los cubos en cero se emiten de todos modos.** Un gauge que desaparece en cero no se distingue de uno que el equipo dejó de reportar, así que "ninguna unidad fallida" se vería exactamente igual que "la recolección de unidades está rota".
- **El estado se agrupa por clase.** Cada código distinto se volvería una serie permanente, y un plano de control que contesta 400/401/403/404/409/422 en decenas de rutas se multiplica rápido para una pregunta que nadie le hace a un equipo casero. El código exacto ya está en el log de auditoría y en el log de peticiones.
- **Los contadores están en memoria y son por proceso.** Un contador que sobreviviera a un reinicio describiría la historia del equipo en vez de la de este proceso, y Prometheus ya entiende un reinicio. También mantiene una recolección — y el middleware de auditoría que la alimenta — completamente fuera de la base de datos.
- **`/metrics` se excluye del registro de auditoría** y de su propio contador de peticiones. Una recolección cada 15 s escribiría si no unos 5,700 renglones de auditoría al día describiendo nada que hiciera un operador, y dominaría el contador al que sirve.
- **`metricsMiddleware` se registra como el más externo** de los tres (antes de la auditoría y de la lista blanca de permisos) para que una petición que niegue cualquiera de las dos barreras se cuente igual — un 403 sin explicación es justo lo que el contador existe para sacar a la luz. Toma el estado del error que se devuelve, porque un manejador que devuelve uno todavía no escribió su estado.

**El objetivo de recolección no se recompone en ningún lado.** `MetricsScrapeTarget(listenAddr)` lo deriva de la misma cadena a la que se enlaza el servidor y `main.go` le entrega el resultado a `monitoring.Ports.ControllerMetrics` — la misma razón de fuente única de verdad por la que existen `PackageNetworkState.FQDN` y `Manager.MetricsAddr()`. Un enlace comodín (`:5309`, `0.0.0.0:5309`, `[::]:5309`) se reescribe a `localhost` porque un comodín no es una dirección a la que nada se pueda conectar; un host fijado explícitamente se deja en paz, ya que reescribirlo apuntaría la recolección a una dirección en la que el controlador a propósito no está. Un resultado vacío omite el trabajo en vez de apuntarlo a una adivinanza. Cuando `TOWN_OS_TLS` está prendido, `ControllerMetricsScheme` es `https` y el trabajo también trae `insecure_skip_verify` — la hoja la emite la propia CA del equipo, en la que Prometheus no tiene razón para confiar ni forma limpia de que se la entreguen, y la recolección es por loopback dentro del espacio de nombres del host, así que nada más puede contestar por él.

### Interfaz de Monitoreo

La pestaña de monitoreo de la navegación lateral abre una página de tableros que
trae **subpestañas System / DNS**, con enlace directo como `?tab=system|dns` como
cualquier otra pantalla con subpestañas, para que un tablero que alguien está
mirando durante una caída sobreviva a una recarga y se pueda enlazar. Un valor
`?tab=` desconocido se va a System en vez de no renderizar nada. La lista de
pestañas es un solo arreglo que trae tanto el componente uPlot que hay que montar
como el uid de Grafana que muestra los mismos paneles, así que una pestaña no puede
existir en un backend y no en el otro.

El renderizado depende del campo `backend` de la respuesta de estado:

- **Modo uPlot**: paneles renderizados directo en React usando uPlot, consultando Prometheus en el puerto 5308. La rejilla de System se ajusta al viewport (cuatro paneles, dos por renglón); la de DNS **no** — ocho paneles apretados en una pantalla le dejan a cada uno como 100 px de lienzo, y a esa altura una gráfica de latencia es decoración, así que los paneles tienen altura fija y la página se desplaza.
- **Modo Grafana**: un iframe de Grafana incrustado apuntando al puerto 5308 en modo kiosco con tema claro. Cambiar de pestaña reapunta el marco al uid del otro tablero, y el iframe está indexado por ese uid para que el marco se *reemplace* en vez de navegar — Grafana lleva su propio historial, y un cambio de `src` sobre un marco vivo deja el botón Atrás del navegador recorriendo tableros en vez de salir de la página.

Los títulos de los paneles son idénticos en los dos backends: un operador que se
cambia no debería tener que averiguar cuál panel se volvió cuál. Están escritos en
inglés de forma fija — esta pantalla no trae ninguna llamada `t()`, y un título de
panel de Grafana no se puede traducir de cualquier forma, ya que vive en el JSON
aprovisionado.

Cuando los servicios necesarios no están corriendo, se muestran en su lugar un aviso destacado y un mensaje de marcador de posición.

## Contenedor de la Interfaz

El controlador del sistema administra un contenedor de interfaz independiente (`quay.io/town/ui`) como servicio del sistema con `ui.Manager`. La etiqueta de la imagen se deriva de la etiqueta de publicación del controlador del sistema (`quay.io/town/ui:<tag>`), sobrescribible con la variable de entorno `UI_IMAGE`. Las fallas de arranque no son fatales; el sistema sigue sin el contenedor de la interfaz.

## Disposición de la Interfaz Web

### Panel de Servicios del Tablero

La página de inicio del tablero muestra un panel de servicios instalados a todo lo ancho, arriba de la rejilla de tarjetas de estadísticas. El panel lista todas las unidades de servicio de paquetes que se obtienen de `GET /systemd/units`. Cada renglón de servicio muestra:

- Un icono de estado: círculo verde con palomita para activo, círculo rojo con X para fallido, círculo gris para inactivo.
- El nombre del paquete (sacado del campo `package_identifier`).
- El estado activo como texto.
- La descripción del paquete (si está disponible).
- Las notas compiladas de `POST /packages/installed/info`, renderizadas en línea con enlaces según su tipo (URL, correo, teléfono).

Al hacerle clic a cualquier renglón de servicio se navega a `/dashboard/system`. El panel se esconde cuando no hay servicios instalados. Las notas se obtienen una vez por servicio y se guardan en caché.

### Disposición

El tablero usa una disposición de dos paneles: una barra lateral izquierda fija y un área de contenido a la derecha con una barra de encabezado superior fija.

**Barra lateral** -- un panel vertical de 256 px de ancho (`w-56`) con el logotipo de Town OS y el texto de marca en un banner gris arriba, seguido de botones de navegación apilados verticalmente (cada uno con un icono y una etiqueta). Las rutas activas usan `variant="secondary"` y las inactivas `variant="ghost"`.

**Barra de estado superior** -- una barra horizontal alineada a la derecha que muestra: la píldora de estado de conexión (cargando/desconectado/conectado), el conteo de fallas de servicios del sistema (insignia roja que enlaza a `/dashboard/system?expand=system` cuando `system_services.failed > 0`), el nombre de usuario con el que se inició sesión con su insignia de administrador y el botón de cerrar sesión.

## Servicios del Sistema

Los servicios del sistema son contenedores de infraestructura que administra systemd (distintos de los servicios de paquetes que instala el usuario). Usan el prefijo de nombre de unidad `town-os-system--`.

El conjunto es: rolodex, el ingress, pages, la interfaz, node-exporter, Prometheus, la interfaz de monitoreo (reenviador socat o Grafana) y **una partición gfeh por red** (`town-os-system--gfeh-<red>`). Todo lo de esa lista se tiene que registrar en `collectSystemServices()` para que `POST /system-services/refresh` lo vuelva a bajar y reiniciar — una omisión ahí es invisible hasta que una actualización deja en silencio el servicio en su imagen vieja.

### Generación de Unidades de Servicios del Sistema

`GenerateSystemServiceUnit` produce unidades de systemd basadas en podman con `Restart=always`. La configuración de la unidad acepta un campo `VolumeDirs` que enlista los directorios del host que hay que crear de antemano con renglones `ExecStartPre=/bin/mkdir -p <dir>`, evitando fallas de montaje cuando los contenedores arrancan tras un reinicio antes de que haya corrido el controlador del sistema.

### API de Servicios del Sistema

- `GET /system-services` (localhost o autenticación) -- lista los servicios del sistema con el estado vivo de la unidad. Cada entrada incluye la llave, el nombre a mostrar, la imagen, el puerto y los campos de estado de la unidad de systemd. Devuelve una lista vacía cuando el monitoreo no está configurado. Excluido del registro de auditoría.
- `POST /system-services/status` (requiere admin) -- cambia el estado de un servicio del sistema. Acepta la llave y la acción (`start`, `stop`, `restart`). Las acciones `enable` y `disable` se rechazan.
- `POST /system-services/refresh` (requiere admin) -- refresca el estado de los servicios del sistema.

## Imagen de Producción de la Interfaz Web

Una imagen de contenedor de interfaz independiente (`quay.io/town/ui`) se construye desde `Containerfile.ui`. Usa una compilación en dos etapas: `oven/bun:latest` construye los archivos estáticos de la interfaz, y luego `docker.io/library/caddy:latest` los sirve en el puerto 80 con enrutado SPA (`try_files {path} /index.html`). A la interfaz se llega por el ingress compartido en vez de ocupar directo el `:80` del host — es el backend predeterminado de `:80` del ingress para cualquier host que no coincida con una ruta, así que el inicio de sesión por IP pelona sigue funcionando.

**Los encabezados de caché cargan peso** (`Caddyfile.ui`). Todo lo que está bajo `/assets/*` lleva huella digital que le pone Vite, así que la URL de un recurso nombra una compilación exacta para siempre y se sirve con `public, max-age=31536000, immutable`. `index.html` es el único archivo al que Vite **no** le pone huella, y es el que nombra el paquete actual; servido sin ningún `Cache-Control`, un navegador puede aplicar frescura heurística (RFC 9111 §4.2.2) y reutilizar su copia en caché sin revalidar, así que un equipo actualizado sigue repartiendo el `index.html` de la versión anterior, que apunta al paquete de la versión anterior. El síntoma es una actualización que parece no haber pasado — las funciones nuevas se renderizan como si la interfaz nunca hubiera oído de ellas. Toda ruta que no sea de recursos es una ruta SPA que `try_files` resuelve a `index.html`, así que la regla `no-cache` está escrita para cubrirlas todas (`@html not path /assets/*`).

`make release-ui` compila con `--no-cache` para que un `push-rc` siempre distribuya recursos de interfaz recién construidos en vez de un paquete que quedó en caché por capas.

**Las pruebas nunca bajan la imagen de interfaz de quay** — el objetivo de make `ui-image` construye `Containerfile.ui` localmente como `localhost/town-os-ui:<INSTANCE_ID>` (siempre coincidiendo con la arquitectura del host y con el código de interfaz del repositorio), lo guarda en la caché de imágenes, y el arnés de pruebas lo carga en los contenedores de prueba y lo inyecta con la variable de entorno `UI_IMAGE`. `test-integration-build` y `test-ui-integration` dependen de `ui-image`. Las etiquetas de quay.io/town/ui son solo para las subidas de producción/publicación. `uiTestImage` en `integration/systemcontroller_ui_test.go` se salta su prueba cuando `UI_IMAGE` no está definida, en vez de irse a una etiqueta de quay.

## Imagen del Runner de Proton

La imagen del runner de Proton (`quay.io/town/proton`) se construye desde `Containerfile.proton`. Usa una compilación en dos etapas: una etapa de descarga baja el tarball de la publicación de GE-Proton (fijado con el argumento de compilación `GE_PROTON_VERSION`), y la etapa de ejecución instala las dependencias de Wine/Proton (de 64 y de 32 bits), Xvfb para operar sin pantalla, y un script envoltorio en `/usr/local/bin/proton` que arranca un framebuffer virtual y configura el entorno de Proton antes de ejecutar la aplicación.

La cadena de make provee: `release-proton-image` (construir), `push-proton-rc` (subir etiquetas de candidata a publicación por arquitectura `rc.<fecha>-<arch>` + `rc.latest-<arch>`) y `push-proton-release` (subir etiquetas de publicación por arquitectura `release.<fecha>-<arch>` + `latest-<arch>`). La imagen de proton también se incluye en los flujos completos `push-rc` / `push-release` y en el ensamblado `manifest-rc` / `manifest-release` cuando `PROTON_ENABLED=1`.

## Cliente de la API de la Interfaz Web

El navegador determina la URL base de la API en tiempo de ejecución a partir de `window.location`, usando el protocolo y el nombre de host actuales con el puerto 5309 (p. ej., `https://myhost:5309`). No hay ningún proxy del lado del servidor; el navegador habla directo con la API del controlador del sistema.

La variable de entorno `VITE_API_URL` sobrescribe la URL derivada del navegador cuando está definida. Sirve durante el desarrollo, cuando el servidor de la API corre en otro host o puerto.

El tablero de monitoreo deriva la URL de su puerto de monitoreo (el 5308) del nombre de host actual. Cuando `VITE_API_URL` está definida, el nombre de host se saca de ahí; si no, se usa `window.location.hostname`.

## Accesibilidad de la Interfaz Web

Todos los componentes de diálogo incluyen un elemento `DialogDescription` que da una descripción concisa del propósito del diálogo. Esto cumple el requisito de accesibilidad de Radix UI para lectores de pantalla y quita las advertencias de `aria-describedby`. Las descripciones se ponen dentro del encabezado del diálogo, después del título, y son visibles para todos los usuarios.

## Internacionalización

Todas las cadenas de cara al usuario (etiquetas de la interfaz, mensajes de error, notificaciones emergentes, descripciones de acciones del log de auditoría) se pueden traducir con un patrón de catálogo de mensajes.

### Backend

El paquete `i18n` provee una función `T(locale, key, args...)` que resuelve llaves de traducción. La cadena de respaldo es: la configuración regional que se pide, luego `en-US`, luego la cadena cruda de la llave. Cuando se dan `args`, se aplica el formateo de `fmt.Sprintf`. Las llaves de mensaje usan espacios de nombres separados por puntos (p. ej., `auth.login_failed`, `pages.toast_provisioned`).

### Catálogos Poblados

Los catálogos del backend viven en un archivo por configuración regional en `src/i18n` (`de_de.go`, `zh_cn.go`, …); el espejo del frontend vive en `ui/src/i18n` (`de-DE.js`, `zh-CN.js`, …). Los dos lados se mantienen a la par — todo catálogo poblado del backend tiene su gemelo en el frontend.

`PopulatedLocales()` es la lista autoritativa (24 entradas): `en-US`, `ar-SA`, `bn-BD`, `da-DK`, `de-DE`, `es-ES`, `fi-FI`, `fr-FR`, `hi-IN`, `it-IT`, `ja-JP`, `ko-KR`, `nl-NL`, `pl-PL`, `pt-BR`, `ru-RU`, `sa-IN`, `sv-SE`, `th-TH`, `tr-TR`, `uk-UA`, `vi-VN`, `zh-CN`, `zh-TW`. Todo lo que no esté ahí se va al inglés. `IsPopulated(code)` es lo que usa la interfaz para deshabilitar una entrada no poblada en el selector de idioma.

**Todos los códigos de configuración regional traen una subetiqueta de región**, y `TestLocaleCodesAreRegionQualified` lo sostiene. El sumerio (`sux`) era la única excepción — un código ISO 639-3 pelón — y ya no está. Se quitó por su escritura, no por su forma: el cuneiforme vive en `U+12000`–`U+1254F`, para lo cual casi nada distribuye una fuente, así que en cualquier equipo sin Noto Sans Cuneiform todas las cadenas de esa configuración regional se pintaban como cuadritos de sustitución. La romanización que el catálogo traía entre paréntesis sí sobrevivía, lo cual lo hacía peor que estar en blanco — pedazos latinos y puntuación alrededor de agujeros. Renderizarlo con honestidad significaba empaquetar una fuente web (el catálogo usaba 45 puntos de código distintos, pero la tipografía completa pesa 462 K y hacer un subconjunto quiere `fonttools` en el host de compilación) y agregar maquinaria de `@font-face` que la interfaz no tiene, lo cual es mucho aparato para un idioma sin hablantes.

### Listas de Configuraciones Regionales

Se usan códigos BCP 47 en todo el sistema. Se proveen dos listas curadas:

- **CommonLanguages** (21 entradas) -- árabe (ar-SA), bengalí (bn-BD), alemán (de-DE), inglés (en-US), español (es-ES), francés (fr-FR), hindi (hi-IN), italiano (it-IT), japonés (ja-JP), coreano (ko-KR), neerlandés (nl-NL), polaco (pl-PL), portugués (pt-BR), ruso (ru-RU), sánscrito (sa-IN), sueco (sv-SE), tailandés (th-TH), turco (tr-TR), ucraniano (uk-UA), vietnamita (vi-VN), chino (zh-CN). Cada entrada incluye el nombre en escritura nativa y el nombre en inglés.
- **ExtendedLocales** (89 entradas) -- lista completa de variantes regionales por país (p. ej., de-AT, en-GB, es-MX, fr-CA, pt-PT, zh-TW).

### Frontend

Un proveedor de contexto de React (`I18nProvider`) envuelve la aplicación y expone un hook `useI18n()` que devuelve `{ locale, setLocale, syncServerLocale, t }`. La función `t` resuelve llaves contra el catálogo del frontend con la misma cadena de respaldo que el backend. La interpolación de parámetros usa marcadores `{name}` (p. ej., `t('greeting', { name: 'Alice' })`).

### Detección, Almacenamiento y Sincronización de la Configuración Regional

La interfaz escoge su idioma **primero desde el navegador**, no desde el ajuste global. Al cargar lee `navigator.languages` y compara las preferencias en orden contra los catálogos que se distribuyen: las variantes regionales se pliegan al idioma base (`de-AT` → `de-DE`), y el chino se desambigua por escritura/región (`zh-Hant` o una región `TW`/`HK`/`MO` → `zh-TW`, si no `zh-CN`). La comparación no distingue mayúsculas e intenta las etiquetas exactas de todas las preferencias antes de irse a las subetiquetas primarias.

Precedencia, de mayor a menor:

1. una elección explícita, guardada **por navegador** en `localStorage` — *fijada*
2. un idioma detectado del navegador emparejado con un catálogo distribuido — *fijada*
3. el ajuste global `locale` del servidor, aplicado después con `syncServerLocale` — *no fijada*

Una vez que la configuración regional queda fijada, `syncServerLocale` no hace nada. Esa es la gracia de la separación: el ping de estado de 60 segundos antes llamaba a `setLocale` y por eso le imponía el ajuste global `locale` del administrador a todos los navegadores en cada sondeo. El ajuste `locale` (global, `en-US` por omisión, que todavía se reporta en la respuesta del ping) ahora es solo el respaldo para un idioma del que Town OS no distribuye catálogo.

### API de Configuraciones Regionales

- `GET /locales` (requiere autenticación) -- devuelve la configuración regional actual, la lista de las pobladas, los idiomas comunes y las configuraciones regionales extendidas. Excluido del registro de auditoría.

### Interfaz de Ajustes

La página de ajustes del sistema incluye un selector de idioma. Los idiomas comunes se muestran en una lista desplegable con sus nombres en escritura nativa. Una sección expandible revela la lista de configuraciones regionales extendidas. Las no pobladas (las que no tienen catálogo de traducción) se muestran con un asterisco al final y están deshabilitadas en el selector, para que no se puedan escoger.

## Configuración del Controlador del Sistema

### Secuencia de Arranque

El orden de arranque paso a paso autoritativo vive en [Secuencia de Arranque del Controlador del Sistema](#secuencia-de-arranque-del-controlador-del-sistema). En resumen:

1. `setupPodmanEnv()` apunta `CONTAINER_HOST` al socket de podman del host.
2. Análisis de flags, y luego `:5309` se enlaza de inmediato con el stub de estado de arranque.
3. Creación de directorios, limpieza de la base de datos vieja de la raíz, base de datos, y los administradores de cuentas (más la purga de la cuenta de servicio heredada), sesiones, auditoría, ajustes, pages y red — este último siembra la red del hogar.
4. Siembra de repositorios, refresco forzado de la raíz de repositorios.
5. Administrador de instalación, almacenamiento btrfs, administrador de systemd; resolución de la etiqueta de imagen.
6. Escritura de la configuración de rolodex + espera de disponibilidad (al propio rolodex lo supervisa systemd).
7. Descargas de las imágenes centrales (NC, monitoreo, interfaz) y arranque de los servicios de sistema de monitoreo.
8. CA TLS local, ingress y servicio de pages.
9. Reconciliación del almacenamiento de objetos (una partición gfeh por red).
10. Detección de cambio de versión, reconciliación, comandos posteriores a la actualización.
11. Reconstrucción del DNS, reconciliación de redes, una segunda reconciliación (idempotente) del almacenamiento de objetos, programación del ingress, arranque del contenedor de la interfaz.
12. Etapa de frescura (reinicios por paquete después de un refresco).
13. Construcción del manejador y el intercambio atómico del stub de arranque por el router completo.
14. Publicación en segundo plano de los nombres del almacenamiento de objetos, en cuanto una partición contesta.

Las fallas de arranque del monitoreo, la configuración de Rolodex, las descargas de imágenes centrales, la CA TLS, el ingress, el servicio de pages, el almacenamiento de objetos, la reconciliación de redes y el contenedor de la interfaz no son fatales; el sistema sigue sin ellos. Todas las descargas de imágenes de contenedor usan el ayudante `ensureImage`, que revisa `podman image exists` antes de bajar, evitando descargas de más en entornos de prueba/desarrollo donde las imágenes están precargadas. Las fallas de descarga de servicios que no son esenciales se registran en stderr y no impiden el arranque, dejando que el sistema arranque incluso cuando la red no está disponible por un rato.

### Detección de la etiqueta de versión

El controlador del sistema deriva etiquetas de imagen que coinciden para todos los servicios hermanos (interfaz, Rolodex, controlador de red, ingress) a partir de una sola etiqueta que resuelve `resolveImageTag()`: la variable de entorno `TOWN_OS_TAG` si está definida, y si no `rc.latest-<arch>` (`defaultVersionTag()`, con la arquitectura de `runtime.GOARCH` mapeada a `x86_64`/`aarch64` con `archTag()`). No hay ninguna versión `Version` fijada en tiempo de compilación ni ningún archivo `/town-os.tag` — los dos se quitaron porque un valor viejo en cualquiera de ellos dejaba en silencio todas las imágenes hermanas atoradas en una etiqueta vieja incluso después de que el controlador avanzara. El sistema de compilación de la instalación fija una etiqueta específica definiendo `TOWN_OS_TAG` en la unidad de systemd del systemcontroller (`../install/make/install.sh` la deriva de `CONTROLLER_IMAGE`); sin ninguna sobrescritura, la flota siempre sigue `rc.latest-<arch>`. Esta etiqueta construye referencias de imagen como `quay.io/town/ui:<tag>` y `quay.io/town/rolodex:<tag>`; las etiquetas que se suben son por arquitectura, así que toda etiqueta hermana derivada trae el sufijo de arquitectura.

### Formato de Errores

Todos los errores de la API se devuelven como objetos Problem Detail de la RFC 9457 (JSON estructurado con campos de tipo, título, estado y detalle). Un `ProblemDetailHTTPErrorHandler` personalizado se define como manejador de errores de Echo.

### Registro de Peticiones

El middleware `RequestLogger()` de Echo está habilitado globalmente, registrando todas las peticiones HTTP en stderr. La verbosidad se controla con la variable de entorno `LOG_LEVEL`.

### Limitación de Inicios de Sesión

`POST /account/authenticate` es público y cada intento cuesta un hash argon2id de 64 MiB. Ese es el costo correcto para un hash de contraseña y lo incorrecto para dejar que quien llama sin autenticar lo programe sin límite: unos cuantos cientos de intentos al mismo tiempo son decenas de gigabytes de reserva de memoria en un equipo cuyo diseño entero se basa en correr desde RAM, y la falla no es un inicio de sesión lento — es el asesino de OOM llevándose el controlador.

Dos límites independientes, porque contestan preguntas distintas. `loginLimiter` limita los **intentos por origen** en una ventana (20 cada 5 minutos), que es lo que hace inviable adivinar contraseñas en línea, y está indexado por dirección de origen para que un cliente abusivo no pueda dejar afuera a toda la casa. `loginGate` limita los **hashes simultáneos** de todos los orígenes (4, dejando la memoria pico de argon2 cerca de un cuarto de gigabyte), que es lo que el limitador por origen no puede hacer solo. Los dos están en memoria y son por proceso: protegen la memoria y la CPU de este proceso, y persistirlos convertiría un inicio de sesión fallido en una escritura en la base de datos.

Los dos se revisan **antes** de calcular el hash, no después — el costo del que se defiende es el propio hash, así que un rechazo que de todos modos lo calculara habría pagado por el ataque que estaba rechazando. La ranura de la barrera se libera con un `defer` dentro de un cierre en vez de después de la llamada, porque una ranura que se pierda por un pánico se iría por toda la vida del proceso y cuatro de ellas atorarían todos los inicios de sesión del equipo hasta un reinicio. Una contraseña que resulta correcta limpia la ventana de ese origen, así que una casa detrás de una sola dirección NAT no puede caer en un bloqueo por uso normal.

### CORS

En modo `DEBUG` se permiten todos los orígenes. Si no, se permiten las peticiones entre puertos del mismo nombre de host (p. ej., un navegador en el puerto 80 hablándole a la API en el 5309), **pero solo una vez que el encabezado Host se revisó contra los nombres por los que este equipo puede legítimamente llamarse**. Métodos permitidos: GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS. Se permiten las credenciales con una edad máxima de 3600 segundos.

La revisión importa porque la regla vieja — "el nombre de host del Origin es igual al nombre de host del encabezado Host" — comparaba dos valores que vienen los dos de la misma URL que escogió el atacante. Apunta `box.evil.example` a la dirección de LAN del equipo y un navegador manda `Origin: http://box.evil.example` y `Host: box.evil.example:5309`, que coinciden. Esa es la forma del DNS rebinding, y con `AllowCredentials` le entregaba a una página de paso la ventana de arranque inicial (`POST /account/create` contesta sin autenticación mientras no exista ningún administrador habilitado).

Por eso `originAllowed` exige que el encabezado Host nombre a este equipo: su propio nombre de host, `<hostname>.local`, `<hostname>.<dns_tld>`, las direcciones de loopback y de LAN en las que contesta, o lo que el operador haya configurado en `AllowedHosts`. Esas formas están **enlistadas, no comparadas por sufijo** — una regla como "cualquier nombre cuya primera etiqueta sea el nombre de host" aceptaría `townos.evil.example`, que un atacante nada más registra. Un literal de IP se acepta solo: una dirección no puede tener alias por DNS, así que `http://192.168.1.10/` llegando a `http://192.168.1.10:5309` es el mismo equipo por construcción, que es la forma común en que esto se usa de verdad.

**El acceso a redes privadas (PNA) solo se contesta para un origen que CORS aceptaría.** El encabezado `Access-Control-Allow-Private-Network` antes se devolvía sin condiciones, lo cual le entrega a todos los orígenes de internet el permiso del navegador para alcanzar una dirección privada — la única protección que PNA existe para agregar encima de CORS. Su middleware se registra **antes** que el middleware de CORS para que de todos modos corra en una revisión previa (preflight), que CORS contesta por su cuenta sin llamar más abajo en la cadena.

### Apagado Ordenado

SIGINT dispara la cancelación del contexto. El servidor HTTP se apaga y todas las goroutines de fondo salen por los canales del contexto. A Rolodex lo supervisa systemd y el systemcontroller no lo detiene.

### Flags de CLI

- `-db <ruta>` -- ruta a la base de datos SQLite (por omisión, un archivo temporal efímero).
- `-btrfs <ruta>` -- ruta base para las operaciones con subvolúmenes btrfs.
- `-repo-dir <ruta>` -- directorio base para los repositorios git (por omisión, un directorio temporal efímero).
- `-network-state <ruta>` -- directorio para los archivos de estado de red por paquete (por omisión `/run/town-os`, `DefaultNetworkStatePath`; tiene que ser una ruta que compartan el contenedor del systemcontroller y el host — nunca `/var/run/...` ni `/tmp`).
- `-listen <dirección>` -- dirección de escucha HTTP (por omisión `:5309`).

La imagen del controlador de red tampoco es un flag; se deriva de la etiqueta de imagen resuelta y se sobrescribe con `NC_IMAGE`.

### Variables de Entorno

- `CONTAINER_HOST` -- URL del socket unix del demonio podman del host. Se define automáticamente al arrancar como `unix:///run/podman/podman.sock` (ve `HostPodmanSocket`). Toda invocación de `podman` — incluidos los procesos hijos que bifurca el systemcontroller — la hereda del entorno del proceso y se va por el socket del host en vez del almacenamiento aislado de podman del contenedor del systemcontroller. La unidad de systemd del repositorio de instalación también debería definir `Environment=CONTAINER_HOST=...` para que se vea en la salida de `systemctl`, pero la llamada a `setupPodmanEnv()` es la fuente de verdad en tiempo de ejecución.
- `TOWN_OS_LISTEN` -- sobrescribe el flag `-listen`.
- `TOWN_OS_SIGNING_KEY` -- sobrescribe la llave efímera de firma JWT (ve Administración de Sesiones).
- `TOWN_OS_TLS` -- sirve el propio listener del plano de control (`:5309`) por HTTPS, terminado por la CA local del equipo con una hoja emitida igualito que la de un paquete. **Apagado por omisión, y eso es cuestión de secuencia y no una reserva**: un navegador al que no se le dio la CA del equipo no puede completar una XHR contra un certificado que no es de confianza y, a diferencia de una navegación, no hay ninguna pantalla intermedia que aceptar — la interfaz nada más dejaría de funcionar sin ninguna forma de llegar a la pantalla que lo explica. La interfaz además hoy se sirve por HTTP simple (es el backend predeterminado de `:80` del ingress), así que un equipo que prendiera esto sin instalar antes la CA pasaría de "sin cifrar" a "caído". El operador instala la CA (`GET /tls/ca.crt`, público) y luego define esto. Acepta `1`/`true`/`yes`/`on`. Se resuelve **antes** de enlazar el listener, así que un flujo de estado de arranque que empieza como HTTP nunca se vuelve HTTPS por debajo de su cliente, y es **fatal** si falla en vez de irse a texto claro: un operador que pidió TLS y en silencio recibió texto plano está peor que uno cuyo equipo se niega a arrancar y dice por qué.
- `TOWN_OS_TLS_CERT` / `TOWN_OS_TLS_KEY` -- un certificado y una llave que da el operador, para un equipo que está detrás de un nombre que ya tiene un certificado de confianza pública. Definir **los dos** habilita TLS por sí solo y la CA local no se consulta; definir solo uno no hace nada.
- `TOWN_OS_TLS_SANS` -- nombres o IP adicionales separados por comas para la hoja generada, para un equipo al que se llega por un nombre que el controlador no puede derivar (un CNAME, un nombre DHCP que asigna el router).
- `TOWN_OS_TEST` -- si está definida, usa repositorios de prueba en vez de los predeterminados de producción.
- `DEBUG` -- si está definida, permite todos los orígenes CORS y pone los repositorios de prueba antes que los predeterminados.
- `LOG_LEVEL` -- nivel de registro: `debug`, `info`, `warn`, `error` (`error` por omisión).
- `TOWN_OS_REPO_USERNAME` / `TOWN_OS_REPO_PASSWORD` -- credenciales de repositorio que se aplican a todos los repositorios en la primera inicialización.
- `TOWN_OS_TAG` -- fija la etiqueta de imagen de la que se deriva toda imagen hermana (ve [Detección de la etiqueta de versión](#detección-de-la-etiqueta-de-versión)). La define el sistema de compilación de la instalación en la unidad de systemd del systemcontroller.
- `ROLODEX_IMAGE` -- sobrescribe la imagen de contenedor de Rolodex (por omisión `quay.io/town/rolodex:<tag>`).
- `UI_IMAGE` -- sobrescribe la imagen de contenedor de la interfaz (por omisión `quay.io/town/ui:<tag>`). Definirla como la **cadena vacía** (explícitamente presente pero vacía) se salta el contenedor de la interfaz por completo — modo de desarrollo, donde bun sirve la interfaz.
- `NC_IMAGE` -- sobrescribe la imagen del controlador de red (por omisión `quay.io/town/networkcontroller:<tag>`). La usa el arnés de integración para inyectar un NC construido localmente.
- `INGRESS_IMAGE` -- sobrescribe la imagen del ingress (por omisión `quay.io/town/ingress:<tag>`). Definirla como la cadena vacía se salta el ingress y el servicio de pages — modo de desarrollo.
- `GFEH_IMAGE` -- sobrescribe la imagen del almacenamiento de objetos (por omisión `quay.io/town/gfeh:<tag>`). Definirla como la **cadena vacía** se salta el almacenamiento de objetos por completo — modo de desarrollo. El almacenamiento de objetos también se salta cuando el ingress está deshabilitado, ya que a las cuatro vistas HTTP solo se llega por él.
- `GFEH_SMB_PORT_BASE` -- sobrescribe el puerto de host desde el que empezarían los listeners SMB (por omisión `4450`). Vestigial: [ninguna partición sirve SMB](#sin-vista-smb), así que no se aparta ningún puerto del host. Se deja conectado para que el ajuste del arnés siga siendo inofensivo.
- `TOWN_OS_WG_SALT` -- la sal de instancia que separa los nombres de interfaz WireGuard, los puertos de escucha y las subredes de superposición de este equipo de los de otro Town OS que comparta el espacio de nombres de red. Sin definir en un equipo real; la definen los arneses de prueba y de desarrollo. Ve [La sal de la instancia](#la-sal-de-la-instancia).

#### Puertos de host de los servicios del sistema

Todos los servicios del sistema corren con `--net host`, así que todos estos enlazan en el espacio de nombres de red en el que esté el controlador — el espacio de nombres del *host*, incluso dentro del arnés de integración (cuyo contenedor también corre con `--net host`, a propósito, para que las compilaciones sigan funcionando en redes cautivas donde el DNS por bridge está roto). Por eso un equipo con `make test-full` y uno con `make dev` se pelean por todos y cada uno de estos puertos y, bajo `Restart=always`, se meten en bucle de caídas uno al otro para siempre.

Cada una de estas mueve uno de ellos y **usa por omisión el puerto de producción**, así que un entorno sin definir reproduce exactamente el arranque de hoy. El `system_port_env` de `make/lib.sh` los aparta por corrida en `SYSTEM_PORT_FILES` y se los pasa al contenedor de pruebas — REGLA DE HIERRO. `make dev` a propósito **no** define ninguno: dev refleja un equipo real, donde `redirect_host_dns` necesita rolodex en el `:53` y un navegador necesita el ingress en el `:443`. Un valor que no se puede analizar se reporta por stderr y se va al predeterminado, porque si no un dedazo se vería exactamente igual que no definirlo.

- `TOWN_OS_DNS_PORT` -- el puerto en el que rolodex sirve DNS (`53` por omisión, en `DNSLoopback`). **El enrutado de systemd-resolved se salta por completo cuando este no es el predeterminado**: una dirección de servidor DNS por dominio no lleva puerto, así que apuntar resolved a `DNSLoopback` mandaría en silencio al vacío todas las consultas `.tld` en vez de dejarlas a la ruta normal del resolvedor.
- `TOWN_OS_ROLODEX_METRICS_PORT` -- el puerto en el que rolodex sirve su endpoint `/metrics` de Prometheus, también en `DNSLoopback` (`9153` por omisión). Es un listener aparte del puerto de DNS y necesita su propia sobrescritura; `rolodex.Manager.MetricsAddr()` es la única cadena a partir de la cual se construyen tanto `rolodex.yml` como el objetivo de recolección de Prometheus, así que moverlo mueve los dos.
- `TOWN_OS_NODE_EXPORTER_PORT` -- el puerto de métricas de loopback de node-exporter (`9100` por omisión).
- `TOWN_OS_PROMETHEUS_PORT` -- el puerto de la API HTTP de loopback de Prometheus (`9090` por omisión).
- `TOWN_OS_MONITORING_PORT` -- el único puerto de monitoreo que da a la LAN (`5308` por omisión).
- `INGRESS_HTTPS_PORT` / `INGRESS_HTTP_PORT` -- los puertos publicados del ingress (`443` / `80` por omisión).

## Ajustes

| Llave                    | Por omisión                      | Descripción                                     |
| ------------------------ | -------------------------------- | ----------------------------------------------- |
| `default_quota`          | `53687091200`                    | Cuota de volumen predeterminada en bytes (50 GB) |
| `max_archive_size`       | `1073741824`                     | Tamaño máximo de subida en bytes (1 GB)         |
| `archive_unpack_timeout` | `600`                            | Tiempo límite de desempaquetado en segundos (10 min) |
| `locale`                 | `en-US`                          | Código de configuración regional BCP 47 (respaldo global) |
| `dns_tld`                | `home`                           | Dominio de nivel superior predeterminado para los registros DNS de paquetes |
| `dns_resolution_mode`    | `auto`                           | Resolución upstream de rolodex: `auto`, `recursive` o `forward` |
| `dns_local_forwarders`   | `false`                          | Tomar la lista de reenviadores de los resolvedores que le entregó la propia red de este equipo, en vez de los predeterminados públicos |
| `peer_ttl`               | `7200`                           | Vida de una inscripción de par WireGuard en segundos (2 h) |
| `gfeh_partition_quota`   | `0`                              | Cuota en bytes de cada partición de almacenamiento de objetos (0 = ilimitada) |
| `proton_image`           | `quay.io/town/proton:latest`     | Imagen del runner de Proton — **registrada solo bajo la etiqueta de compilación `proton`** |

`DefaultSettings` (`src/account/settings.go`) se siembra en la primera inicialización y los valores existentes nunca se sobrescriben.

Varias llaves se **leen pero nunca se siembran** — no tienen renglón hasta que algo
escribe uno, y su valor por omisión vive en el punto de lectura como respaldo para
la cadena vacía. No las agregues a `DefaultSettings` esperando que no cambie nada
más: un renglón sembrado no se distingue de la decisión de un operador, que para
las configuraciones de listas de bloqueo es la diferencia entre "nunca se
configuró, déjalo en paz" y "se puso explícitamente en vacío, empújalo"
([Listas de bloqueo RBL / DNSBL](#listas-de-bloqueo-rbl--dnsbl)).

| Llave | Valor cuando está ausente | La escribe |
| --- | --- | --- |
| `monitoring_backend`     | `uplot` | `POST /settings/set` |
| `dns_rbl_config` / `dns_dnsbl_config` | sin configurar (no es lo mismo que vacío) | `POST /dns/rbl`, `POST /dns/dnsbl` |
| `dns_excluded_services`  | lista vacía (la publicación es de exclusión voluntaria) | `POST /dns/services/set` |
| `dismissed_upgrades_hash` | ausente (nada descartado) | `POST /packages/upgrades/dismiss` |

**No existe ningún `object_storage_enabled` ni ninguna contraseña de cuenta de servicio.** El almacenamiento de objetos no es una función que haya que prender ([Arranque y reconciliación](#arranque-y-reconciliación)), y el demonio no tiene ninguna credencial de Town OS ([Sin cuentas de servicio](#sin-cuentas-de-servicio)). Un renglón de cualquiera de las dos, que quedó olvidado en un equipo actualizado, no lo lee nadie.

`proton_image` no está en el mapa base: `src/account/settings_proton.go` es `//go:build proton` y registra el valor predeterminado en `init()`, así que una compilación sin la etiqueta no tiene ajuste de Proton, ni ruta de instalación de Proton, y reporta `proton_enabled: false` en el ping de estado. Se usa un registro condicionado por etiqueta de compilación en vez de una función `Register` exportada para que quien llama no adquiera una dependencia del orden de llamadas sobre `DefaultSettings`.
