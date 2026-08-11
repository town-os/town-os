[![Town OS](./banner.png)](https://town-os.github.io)

**Inicio: <https://town-os.github.io>** | [Código fuente (Gitea)](https://gitea.com/town-os/town-os) | [Espejo (GitHub)](https://github.com/town-os/town-os)

> **Tu nube en tu armario, lo bastante fácil para cualquiera.**

Town OS es una plataforma autogestionada que funciona íntegramente desde una memoria USB y convierte cualquier ordenador en un servidor de nube personal. Gestiona su propio almacenamiento, su red y sus servicios en contenedores -- sin necesidad de instalación. Está diseñada para cualquier persona, no solo para expertos: una interfaz amable te guía en todo, de modo que no necesitas saber cómo funciona nada de esto salvo que quieras.

**GITHUB ES UN ESPEJO:** El repositorio de origen está en <https://gitea.com/town-os/town-os>.

> **Traducción al español (España).** Este archivo es la traducción al español
> peninsular de [README.md](README.md). **El original en inglés es el
> autoritativo**; cuando ambos discrepen, el inglés es el correcto y lo que hay
> que corregir es la traducción. Los identificadores de código, rutas de
> archivo, comandos, variables de entorno, nombres de objetivos de make y rutas
> de la API se conservan sin traducir.
>
> El resto de la documentación en español (España):
> [CLAUDE.es-ES.md](CLAUDE.es-ES.md) (reglas de compilación y estilo de código)
> y [DESIGN.es-ES.md](DESIGN.es-ES.md) (arquitectura y especificación
> funcional). También hay una versión en español de México
> ([README.es-MX.md](README.es-MX.md), [CLAUDE.es-MX.md](CLAUDE.es-MX.md),
> [DESIGN.es-MX.md](DESIGN.es-MX.md)) y en chino, en escritura simplificada
> ([README.zh-CN.md](README.zh-CN.md), [CLAUDE.zh-CN.md](CLAUDE.zh-CN.md),
> [DESIGN.zh-CN.md](DESIGN.zh-CN.md)) y tradicional
> ([README.zh-TW.md](README.zh-TW.md), [CLAUDE.zh-TW.md](CLAUDE.zh-TW.md),
> [DESIGN.zh-TW.md](DESIGN.zh-TW.md)).

## Índice

- [Por Qué Importa](#por-qué-importa)
- [Funciones Existentes y Planificadas](#funciones-existentes-y-planificadas)
- [Imágenes Arrancables (PC y Raspberry Pi)](#imágenes-arrancables-pc-y-raspberry-pi)
- [Solicitudes de Comentarios](#solicitudes-de-comentarios)
- [Requisitos Previos](#requisitos-previos)
- [Desarrollo](#desarrollo)
- [Objetivos del Makefile](#objetivos-del-makefile)
    - [Pruebas](#pruebas)
    - [Análisis Estático](#análisis-estático)
    - [Registro Local](#registro-local)
    - [Servidor Gitea Local](#servidor-gitea-local)
    - [Compilación](#compilación)
    - [Publicación y Push](#publicación-y-push)
    - [Autenticación de Registros](#autenticación-de-registros)
    - [Gestión de Btrfs](#gestión-de-btrfs)
    - [Limpieza](#limpieza)
    - [Comprobaciones Previas](#comprobaciones-previas)
    - [SSH](#ssh)
    - [Comprobaciones de Dependencias](#comprobaciones-de-dependencias)
- [Licencia](#licencia)

## Por Qué Importa

Tus datos deberían vivir en tu casa, no en el ordenador de otra persona. Los servicios en la nube son cómodos, pero traen consigo cuotas mensuales, concesiones en materia de privacidad y el riesgo de que una empresa cambie sus condiciones, suba los precios o cierre en cualquier momento. Town OS te da la misma comodidad sin renunciar al control.

No hace falta ser una persona técnica para usarlo. Conecta una memoria USB, enciende el equipo y tienes un sistema funcionando. Actualizas cambiando la memoria USB. Reinicias para volver al punto de partida. Si algo sale mal, siempre puedes regresar a un estado operativo -- no puedes dejarte fuera del sistema.

Town OS está construido para que cualquiera pueda ayudar a su familia a ejecutar servicios en casa. Monta un servidor multimedia para tus padres, mantén los dispositivos de tus hijos libres de programas espía o aloja tu propio sitio web -- todo sin pedir permiso a un proveedor de nube.

## Funciones Existentes y Planificadas

El empaquetado está totalmente integrado con el almacenamiento y la red, creando recursos bajo demanda, lo que incluye abrir puertos mediante UPnP y gestionar el reenvío de puertos a través de un controlador de red por paquete, o establecer túneles.

Town OS ejecuta su propio resolutor local (Rolodex), programado por el propio sistema: cada paquete instalado obtiene un nombre bajo un TLD privado (`plex.default.home`), de modo que los servicios son accesibles por nombre en lugar de por número de puerto. El mismo resolutor se encarga del filtrado a nivel de router -- las listas de bloqueo de dominios DNSBL y las listas inversas RBL se consultan bajo demanda contra las zonas de los proveedores que elijas (Spamhaus, SURBL, URIBL y compañía se ofrecen para añadir con un clic), además de una lista de bloqueo local que mantienes a mano. Así es como mantienes la tableta de un niño, o la casa entera, libre de adware y spyware, sin descargas de fuentes y sin nada almacenado en caché a tus espaldas.

Las redes pueden segmentarse como superposiciones (overlays) WireGuard. Cada red posee su propio TLD y su propia subred de superposición, y la separación es real: un portátil en la LAN resuelve todas las redes, mientras que un teléfono conectado por túnel a una red resuelve esa red y la internet pública y nada más -- los nombres de una red hermana sencillamente no existen para él. Los pares se inscriben desde la interfaz (o desde una cuenta a la que solo hayas concedido la inscripción de pares en una red), reciben una configuración lista para importar y se les entrega la dirección que realmente marcaron, de modo que a un teléfono en la misma red Wi-Fi no se le pide dar la vuelta a través de la IP pública. Las inscripciones llevan un TTL y caducan por sí solas, así que un dispositivo abandonado no se queda ocupando una dirección de superposición para siempre. La pantalla de Redes muestra quién está conectado en este momento -- dispositivo, cuenta, dirección de superposición, negociación (handshake) y transferencia en vivo -- con un botón para desconectar.

Todo lo que es HTTP se sirve por HTTPS y por nombre. Town OS ejecuta su propia autoridad de certificación; cada paquete y cada página obtiene un certificado hoja para su FQDN, y un único ingress compartido termina TLS en `:443`, seleccionando el servicio correcto por SNI. Descarga la raíz una vez desde `/tls/ca.crt` y el navegador deja de protestar. El mismo certificado funciona desde la LAN y desde un túnel WireGuard, porque el ingress escucha en todas las interfaces y enruta por nombre, no por dirección.

El sistema de almacenamiento está diseñado junto con el sistema de empaquetado para permitir actualizaciones y también la desinstalación temporal y la restauración posterior. El almacenamiento se particiona de forma exclusiva para cada paquete, lo que permite estrategias de recuperación ante desastres priorizables según coste y disponibilidad. Se emplean cuotas para que las necesidades de almacenamiento no sorprendan al usuario.

Tus archivos tienen un hogar propio. El almacenamiento de objetos (gfeh) ejecuta un demonio por red, cada uno con sus propios usuarios, sus propios permisos y su propia porción del disco, y publica los mismos archivos de cuatro formas a la vez -- un endpoint S3, HTTPS plano, una vista compatible con Google Drive e IPFS -- todo por nombre, todo tras el mismo ingress y el mismo certificado. Los usuarios forman un árbol en lugar de una lista plana, así que puedes entregarle a alguien una rama y todo lo que cuelga de ella, y un archivo puede compartirse como un enlace que luego puedes revocar. La pantalla de Almacenamiento de Objetos muestra la partición de cada red, quién está en ella, a qué puede acceder y todos los enlaces publicados en este momento. Quien haya configurado el equipo se coloca automáticamente en la partición del hogar, porque un almacén de archivos al que tienes que concederte acceso a ti mismo no es un almacén de archivos.

Los paquetes pueden solicitar datos al usuario -- de forma parecida a debconf -- pero a través de la interfaz (mira las capturas de pantalla). Se trata de variables de plantilla y pueden usarse para configurar imágenes de contenedor y gestionar la red. Las preguntas tienen tipo, y el tipo hace trabajo real: un puerto se asigna automáticamente si lo dejas en blanco, un secreto se genera con 256 bits de entropía en lugar de pedirte que lo inventes, un booleano se muestra como casilla de verificación, una pregunta puede marcarse como opcional para que una respuesta vacía signifique «déjalo sin establecer», y un grupo avanzado puede ocultarse tras una casilla con `show_if` para que el diálogo no sea un muro de campos. Una pregunta de tipo `oauth` sustituye al script de shell que antes ejecutabas a mano: pulsa Conectar, aprueba en la propia página del proveedor y el token aparece en el campo -- sin ninguna lista de proveedores incrustada en Town OS, ya que el propio paquete indica las URL.

Los paquetes también pueden definir plantillas de archivo `text/template` de Go que se renderizan en los volúmenes en el momento de la instalación, con acceso a las respuestas del usuario, a los metadatos del paquete y a información del sistema (nombre de host, IP). Las plantillas se aplican después de sembrar los volúmenes pero antes de arrancar el servicio, y los archivos existentes nunca se sobrescriben. Los paquetes pueden depender de otros paquetes: las dependencias se instalan automáticamente, comparten una red de contenedores privada con su padre para hablar por nombre en lugar de a través de puertos del host, pueden pasarse valores entre sí (el nombre de contenedor y el puerto de una base de datos) en el momento de la instalación, y pueden compartir volúmenes de almacenamiento con una aceptación explícita por ambas partes. [El repositorio de paquetes](https://github.com/town-os/default-packages) tiene más información. También puedes sustituir por completo los repositorios por una lista privada de repositorios -- perfecto para tus colegas gamers, familiares a los que tienes que dar soporte, etc. Se espera mucha expansión en este terreno.

Todos los servicios cuentan con registro (logging) y supervisión adecuados. Hay una interfaz cómoda para acceder a esa información, presentada de forma que su consumo sea seguro para usuarios no técnicos. Los servicios se muestran como un árbol -- un paquete con sus dependencias anidadas debajo -- y puedes actuar sobre un árbol entero de una vez o leer en una sola vista los registros combinados de todas sus unidades. Hay cuentas separadas para administradores y usuarios normales: podrías ayudar a tus padres a gestionar un Plex (o algo similar) si quisieras. Podrías mantenerlos libres de spyware. Entre «administrador» y «usuario corriente» existen las concesiones (grants) -- marca una casilla para permitir que una cuenta inscriba dispositivos en una red, o gestione el almacén de archivos de esa red, y nada más. Lo que una concesión no habilita permanece cerrado por omisión, de modo que una capacidad añadida el año que viene no se entrega en silencio a las cuentas que ya existen.

Actualizar el equipo muestra su trabajo. El controlador del sistema se enlaza a su puerto antes de empezar a arrancar, así que la interfaz transmite el progreso en vivo -- controlador, DNS, servicios del sistema y después una fila por paquete a medida que cada uno se reinicia -- en lugar de mostrar un indicador giratorio contra un puerto muerto. Etiqueta cada encarnación del proceso con un identificador, de modo que el navegador puede distinguir «la versión antigua todavía responde» de «la nueva ya está en pie» y sabe cuándo la actualización ha aterrizado de verdad.

Toda la interfaz está internacionalizada. Todas las cadenas visibles para el usuario -- tanto los mensajes de error del backend como el texto de la interfaz del frontend -- pasan por un catálogo de mensajes indexado por códigos de configuración regional BCP 47 (p. ej. `en-US`, `de-DE`). **30 idiomas se distribuyen completamente traducidos** tanto en el backend como en el frontend: alemán, árabe, bengalí, checo, chino (simplificado y tradicional), coreano, croata, danés, eslovaco, esloveno, español, finés, francés, hindi, húngaro, inglés, italiano, japonés, neerlandés, polaco, portugués, rumano, ruso, sánscrito, sueco, tailandés, turco, ucraniano y vietnamita.

Por encima de esos se asientan **18 variantes de país** -- `en-GB`, `en-AU`, `en-CA`, `en-IN`, `en-NZ`, `en-ZA`, `de-AT`, `de-CH`, `fr-BE`, `fr-CA`, `fr-CH`, `es-AR`, `es-MX`, `pt-PT`, `nl-BE`, `ar-AE`, `ar-EG`, `bn-IN` --, lo que da **48 configuraciones regionales seleccionables** en total. Una variante no es una segunda traducción: hereda el catálogo de su idioma y enumera solo las cadenas que ese país dice realmente de otra manera, de modo que un mensaje nuevo llega a todos los países en cuanto lo tiene su idioma base, y una corrección en una cadena francesa se hace una vez en lugar de en cuatro archivos. Varias de esas listas de anulaciones están honestamente vacías. El inglés de Canadá conserva las grafías estadounidenses en *-ize*, y el español de Argentina es famoso por el *voseo* -- pero el voseo sustituye al *tú* familiar, y este catálogo se dirige al lector de *usted* en todo momento, lo cual se lee igual en Buenos Aires que en Madrid. Una lista vacía sigue significando revisada, no olvidada. Una pantalla de idioma en los Ajustes del Sistema presenta 21 idiomas comunes en su escritura nativa, con una lista desplegable de 89 códigos específicos de país; lo que no tiene catálogo se muestra pero aparece deshabilitado.

La interfaz elige tu idioma a partir de tu navegador (`navigator.languages`). Una coincidencia exacta gana primero, así que `de-CH` obtiene el alemán de Suiza en lugar de plegarse al de Alemania. El chino se desambigua por escritura. Un país para el que no distribuimos nada va a donde de verdad se lee -- `es-CO` al español de México en lugar del peninsular, `en-IE` al británico en lugar del estadounidense -- y un `en` o un `pt` a secas se resuelven a un predeterminado con nombre en lugar de a la variante que resulte cargarse primero. Una elección explícita se recuerda por navegador, de modo que el ajuste global de idioma del equipo ya no anula lo que ve cada persona -- solo es el respaldo para un idioma del que Town OS no tiene catálogo.

Las aplicaciones de Windows pueden ejecutarse junto a contenedores Linux nativos gracias a la capa de compatibilidad Proton de Valve. Una definición de paquete con una sección `proton` especifica una imagen de aplicación Windows, un directorio de extracción y una ruta al ejecutable; el sistema descarga la aplicación desde una imagen OCI, la extrae en un volumen persistente y la ejecuta dentro de un contenedor runner de Proton. La imagen del runner se configura para todo el sistema mediante el ajuste `proton_image`. **La compatibilidad con Proton es opcional (opt-in)**: recompila con `make PROTON_ENABLED=1 …` (o `go build -tags proton`) para incluirla. Sin esa etiqueta, los bloques `proton:` en el YAML de paquete se rechazan en el momento de la instalación, el ajuste `proton_image` no se siembra, la interfaz de Ajustes omite la tarjeta de Proton y la cadena de publicación no compila ni sube la imagen del runner.

Una pila de monitorización integrada aporta observabilidad del sistema desde el primer momento. Prometheus recoge métricas y Node Exporter informa de las estadísticas a nivel de host; ambos se gestionan como servicios del sistema sin requerir configuración manual. Los paneles se renderizan directamente en la interfaz con uPlot (~35 KB) por omisión -- E/S de disco, red, CPU y memoria -- lo que mantiene una imagen de Grafana de ~771 MB completamente fuera del equipo. Cambia el ajuste `monitoring_backend` a `grafana` y, en su lugar, se descarga, arranca y preaprovisiona la pila completa de Grafana con una fuente de datos y dos paneles.

La compatibilidad con máquinas virtuales QEMU convive con los contenedores como un runtime de primera clase. Un paquete lo selecciona sencillamente incluyendo una sección `vm:` de primer nivel -- una imagen de disco, memoria y número de CPU -- en lugar de una `image:`. El Servicio del Plano de Control descarga las imágenes desde URL, las convierte a formato raw con `qemu-img` y las almacena en un subvolumen btrfs `vm-images`. En el momento de la instalación, una unidad de servicio de systemd lanza `qemu-system-x86_64` con aceleración KVM, red virtio y reenvío de puertos en modo usuario. Las imágenes de VM pueden listarse, subirse y eliminarse a través de la API y de la interfaz.

El alojamiento de páginas estáticas te permite publicar contenido HTML directamente desde la interfaz. Se admiten tres tipos de origen: subir un archivo tar (el predeterminado), extraer archivos de una imagen de contenedor o clonar un repositorio git. Un desplegable en el diálogo de creación selecciona el tipo de origen, y cada página se sirve por HTTPS en su propio dominio a través del ingress compartido, con un certificado de la CA local. Una página pertenece a una red igual que un paquete, así que un sitio puede publicarse para todos los de la LAN o solo para los pares de una red WireGuard. Las páginas de tipo archivo pueden actualizarse subiendo un archivo nuevo en cualquier momento; las páginas de git y de imagen de contenedor pueden reconstruirse bajo demanda para traer el contenido más reciente.

Echa un vistazo a algunas [capturas de pantalla](./screenshots/). Todo esto ya funciona hoy en las tareas de desarrollo.

Para instrucciones de uso detalladas, incluidos el sistema de empaquetado, el almacenamiento, las páginas y la documentación de la API, visita **<https://town-os.github.io>**.

## Imágenes Arrancables (PC y Raspberry Pi)

Este repositorio contiene el software de Town OS. La **imagen de disco arrancable** se construye desde el [repositorio de instalación](https://gitea.com/town-os/install), que también la lanza en una VM:

```bash
git clone https://gitea.com/town-os/install.git
cd install
make deps        # una sola vez, manual — ningún objetivo de compilación instala paquetes
make run         # construye la imagen y la lanza en una VM
```

Las compilaciones de imagen son nativas por omisión -- sin `TARGET`, la arquitectura de la imagen coincide con la del host de compilación. Indica otra distinta con `TARGET`:

| Comando                    | Resultado                                                                     |
| -------------------------- | ----------------------------------------------------------------------------- |
| `make image TARGET=x86_64` | Imagen para PC (UEFI/GRUB). Solo en host x86_64 -- no hay ruta de emulación x86. |
| `make image TARGET=aarch64`| Imagen aarch64 genérica (UEFI/GRUB), p. ej. una VM de Apple Silicon.          |
| `make image TARGET=rpi`    | **Imagen de arranque nativo para Raspberry Pi.** Una sola imagen cubre Pi 4/400/CM4 y Pi 5/CM5.|

**Raspberry Pi.** `TARGET=rpi` es exclusivamente aarch64 y exclusivamente btrfs, y arranca mediante el propio cargador de la GPU del Pi y `config.txt` en lugar de GRUB. En un host aarch64 se compila de forma nativa; en un host x86_64 se produce de forma cruzada dentro de una VM `qemu-system-aarch64` de sistema completo -- una máquina emulada entera, no `binfmt`/qemu-user ni un compilador cruzado, de modo que la compilación sigue ejecutándose como código aarch64 nativo. Funciona, y es lento.

Graba la imagen `-rpi` resultante en una tarjeta SD, una memoria USB o un NVMe:

```bash
make flash RPI=1 USB_DEV=/dev/sdX   # o haz dd de town-os-<fecha>-aarch64-rpi.img
```

Para el **arranque por NVMe en Pi 5**, configura además el orden de arranque de la EEPROM para incluir NVMe (`rpi-eeprom-config --edit` → `BOOT_ORDER=0xf416`; añade `PCIE_PROBE=1` para adaptadores que no sean HAT+); `dtparam=pciex1` ya está en el `config.txt` generado. Los límites de corriente USB ya vienen levantados para ambas placas en `config.txt` (`usb_max_current_enable=1` en Pi 5, `max_usb_current=1` en Pi 4), lo cual importa porque los SSD USB y los adaptadores NVMe alimentados por el bus son exactamente aquello sobre lo que se ejecuta una imagen de arranque desde USB, y sufren caídas de tensión con el valor predeterminado del firmware.

Consulta el [README del repositorio de instalación](https://gitea.com/town-os/install) para el grabado, la consola serie, las publicaciones y la lista completa de variables.

## Solicitudes de Comentarios

Por favor, prueba la compilación de desarrollo (`make dev` en cualquier Linux; más abajo hay más detalles) y abre [issues](https://gitea.com/town-os/town-os/issues) (se requiere cuenta de Gitea; puede usarse el SSO de GitHub) con las funciones que te gustaría tener. Intento ser muy receptivo y de mente abierta ante todas las posibilidades, así que no sientas que tu idea es demasiado grande o demasiado descabellada. Publícala sin más. <3

## Requisitos Previos

La vía rápida en una máquina recién instalada es `make deps`:

```bash
make deps
```

Esto instala todas las dependencias del host (Go 1.25, podman, runc, btrfs-progs, cabeceras de libsystemd, golangci-lint, bun, qemu, herramientas de compilación) en distribuciones basadas en Arch y en Debian/Ubuntu. También ejecuta `bun install` en `ui/` para que las herramientas de la interfaz (eslint, vite, vitest) estén listas para `make lint` y `make test`. Es seguro volver a ejecutarlo.

Lista completa de lo que Town OS necesita en el host:

- Go 1.25+
- [Bun](https://bun.sh) (runtime de JS; las devDeps de `ui/` incluyen eslint, vite y vitest y las instala automáticamente `make deps`)
- Podman (rootful, con `sudo`) y el runtime `runc`
- QEMU (`qemu-system-x86_64`, `qemu-img`) para la compatibilidad con paquetes de VM
- btrfs-progs (`mkfs.btrfs`)
- libsystemd (cabeceras de desarrollo para la integración con systemd)
- golangci-lint
- Python 3 (se usa para asignar puertos en los objetivos de prueba)

Si prefieres instalarlo todo a mano, los comandos siguientes reflejan lo que hace `make/deps.sh`.

### Ubuntu / Debian

```bash
sudo apt-get update
sudo apt-get install -y build-essential pkg-config ca-certificates libsystemd-dev \
    btrfs-progs podman runc python3 curl git unzip qemu-system-x86 qemu-utils
```

Instala Go 1.25+ desde <https://go.dev/dl/> y después:

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

Después:

```bash
curl -fsSL https://bun.sh/install | bash
(cd ui && bun install)
```

Opcionalmente, crea un archivo `.env` con credenciales de repositorio para el **entorno de desarrollo**:

```
TOWN_OS_REPO_USERNAME=<usuario>
TOWN_OS_REPO_PASSWORD=<contraseña>
```

`make dev` las usa para establecer credenciales de repositorio predeterminadas en el backend. La contraseña puede ser una clave de API HTTP de Gitea o de GitHub. Si se omiten, no se establecen credenciales predeterminadas y solo pueden añadirse repositorios públicos sin credenciales explícitas.

Las pruebas de integración (`make test-full`) **no** usan las credenciales de `.env`. Ejecutan una instancia local de Gitea con credenciales de prueba fijas (`town-os` / `town-os-test`) y nunca contactan con GitHub para las operaciones de repositorio.

Tras instalar los requisitos previos, ejecuta `make pull-images` una vez para traer todas las imágenes de contenedor desde Docker Hub y guardarlas en la caché de imágenes de este checkout, en `.cache/images/`. Las credenciales de Docker Hub (mediante `DOCKER_USERNAME` / `DOCKER_PASSWORD` en `.env`) solo hacen falta si te topas con límites de tasa. Todos los demás objetivos de compilación y prueba cargan las imágenes desde esa caché y nunca contactan con Docker Hub. Si falta alguna imagen en caché cuando un objetivo la necesita, `make pull-images` se ejecuta automáticamente.

## Desarrollo

> **Empieza aquí en una máquina nueva:**
>
> - **`make deps`** — instala todas las dependencias del host (Go, podman, runc, btrfs-progs, cabeceras de libsystemd, golangci-lint, bun, qemu, herramientas de compilación) en Arch o Ubuntu/Debian. Es seguro volver a ejecutarlo.
> - **`make help`** — imprime una lista agrupada de todos los objetivos de make de cara al usuario. Es además el objetivo predeterminado, así que un simple `make` también funciona.

**Es probable que aún queden algunos problemas al ejecutar las pruebas de integración mientras el servidor de desarrollo está en marcha. Se está investigando.**

Si solo estás probando cosas, usa la rama `stable` (la predeterminada). Si quieres los últimos cambios (que puede que no sean buenos), usa `main`. Ambas ramas irán avanzando a medida que las cosas se consideren estables o se integren en el repositorio.

Ejecuta `make dev` para construir la imagen de prueba, crear un volumen btrfs de desarrollo, arrancar el contenedor del backend en el puerto 5309 y lanzar el servidor de desarrollo de Vite con recarga en caliente. Una vez en marcha, accede a la interfaz en `http://<hostname>:5173`.

Los puertos 5309 (API del backend) y 5173 (servidor de desarrollo de Vite) deben ser accesibles en el host.

| Objetivo             | Descripción                                                                                 |
| -------------------- | ------------------------------------------------------------------------------------------- |
| `make dev`           | Arranca el entorno de desarrollo completo (backend + servidor de desarrollo de Vite).       |
| `make dev-stop`      | Detiene y elimina el contenedor del backend de desarrollo de este árbol de trabajo.         |
| `make dev-stop-all`  | Detiene todos los contenedores `town-os-dev-*` del host, no solo los de este árbol de trabajo. |
| `make dev-logs`      | Sigue journalctl dentro del contenedor de desarrollo en marcha.                             |
| `make btrfs-dev`     | Crea un volumen btrfs loopback nuevo de 50 GB para el entorno de desarrollo.                |
| `make dev-btrfs`     | Crea el volumen btrfs de desarrollo solo si no hay uno montado ya (lo usa `dev` automáticamente).|
| `make clean-btrfs-dev` | Desmonta, desconecta los dispositivos de bucle y elimina el volumen btrfs de desarrollo.  |
| `make clean-dev`     | Detiene el contenedor de desarrollo y desmantela el volumen btrfs de desarrollo. Elimina `dev-data/`. |

## Objetivos del Makefile

Todos los objetivos usan un `INSTANCE_ID` único derivado de la ruta del directorio de trabajo, de modo que varios checkouts pueden ejecutarse simultáneamente sin conflictos. El estado efímero (archivos de puerto, volúmenes btrfs, datos de desarrollo) vive en `/tmp/town-os-$(INSTANCE_ID)/`.

### Pruebas

| Objetivo                        | Descripción                                                                                                                                          |
| ------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| `make test`                     | Ejecuta el linter, las pruebas unitarias de Go y las pruebas unitarias de JS.                                                                         |
| `make test-race`                | Ejecuta el linter y las pruebas unitarias de Go bajo el detector de carreras. Más lento que `test` y solo de Go (bun no tiene equivalente); informa de los hallazgos para triarlos en lugar de actuar como barrera. |
| `make test-integration`         | Construye la imagen de prueba y ejecuta las pruebas de integración de Go dentro de un contenedor podman privilegiado con systemd y btrfs. Limpia el loopback btrfs al salir. |
| `make test-integration-build`   | Construye la imagen de prueba y arranca el contenedor de pruebas de integración con todas las imágenes cargadas, pero no ejecuta ninguna prueba. Útil para preparar un `test-integration-rerun`. |
| `make test-integration-rerun`   | Vuelve a ejecutar las pruebas de integración en un contenedor ya en marcha (de un `test-integration-build` previo). Se salta la construcción de imágenes. |
| `make test-ui-unit`             | Ejecuta solo las pruebas unitarias de la interfaz en JS (bun).                                                                                        |
| `make test-ui-integration`      | Ejecuta las pruebas de integración de la interfaz en JS (bun) contra un contenedor de backend. Limpia el loopback btrfs al salir.                     |
| `make test-ui-integration-local`| Ejecuta las pruebas de integración de la interfaz contra un backend en ejecución local (sin contenedor).                                              |
| `make test-full`                | Ejecuta `test`, `test-integration` y `test-ui-integration` en secuencia. Usa una trampa de señales para garantizar la limpieza.                       |
| `make test-full-log`            | Ejecuta `test-full` y redirige toda la salida a un archivo de registro con marca de tiempo en `/tmp/town-os/log/`.                                    |
| `make auto-test`                | Vigila los cambios en archivos `.go`/`.js` y vuelve a ejecutar `make test` automáticamente. Instala [reflex](https://github.com/cespare/reflex) cuando hace falta. |
| `make auto-test-full`           | Vigila los cambios en archivos y vuelve a ejecutar `make test-full` automáticamente. Instala reflex cuando hace falta.                                |

Usa `TEST_RUN=<regex>` para filtrar qué pruebas de integración se ejecutan (p. ej. `make test-integration TEST_RUN=TestInstall`). Usa `TEST_TIMEOUT=<duración>` para anular el tiempo límite predeterminado de 60 minutos.

**Una ejecución de pruebas nunca colisiona con otra, ni con `make dev`.** El contenedor de pruebas se ejecuta con red del host (deliberadamente -- el DNS en red bridge se rompe en redes cautivas), así que todos los servicios del sistema que arranca se enlazan en el espacio de nombres de red del host, igual que todo lo que arranca `make dev`. Por eso el arnés entrega a cada ejecución sus propios puertos efímeros para rolodex, node-exporter, Prometheus, la interfaz de monitorización y el ingress, además de su propia sal (salt) de WireGuard, de modo que los nombres de interfaz, los puertos de escucha y las subredes de superposición difieren por checkout y por rol. `make dev` no acepta ninguna de esas anulaciones a propósito: está pensado para reflejar un equipo real, donde el DNS está en `:53` y el ingress en `:443`.

### Análisis Estático

| Objetivo    | Descripción                       |
| ----------- | --------------------------------- |
| `make lint` | Ejecuta `go vet` y `golangci-lint`. |

### Registro Local

Las pruebas de integración usan un contenedor `registry:2` local para evitar los límites de tasa de Docker Hub. Cuando ejecutas `make test-integration` o `make test-ui-integration`, la compilación automáticamente:

1. Descubre todas las imágenes de `docker.io` referenciadas por los repositorios de paquetes de prueba (herramienta `discover-images`)
2. Arranca un registro local en un puerto aleatorio
3. Carga cada imagen desde la caché de imágenes y la sube al registro local
4. Genera un `registries.conf` que redirige las descargas de `docker.io` al espejo local
5. Monta la configuración en el contenedor de pruebas

Esto es transparente -- no hacen falta cambios de código. Todas las imágenes se cargan desde la caché de imágenes del checkout (`.cache/images/`); no se producen descargas de docker.io durante la cadena de pruebas.

| Objetivo                 | Descripción                                                    |
| ------------------------ | --------------------------------------------------------------- |
| `make registry`          | Arranca el contenedor del registro local.                       |
| `make registry-populate` | Replica las imágenes de docker.io descubiertas al registro local. |
| `make registry-stop`     | Detiene y elimina el contenedor del registro local.             |

Cada directorio de trabajo obtiene su propia instancia de registro (mediante `INSTANCE_ID`), de modo que las ejecuciones de pruebas concurrentes no entran en conflicto.

### Servidor Gitea Local

Las pruebas de integración también usan una instancia local de Gitea para evitar clonar los repositorios de paquetes de prueba directamente desde GitHub. Cuando ejecutas `make test-integration` o `make test-ui-integration`, la compilación automáticamente:

1. Arranca un servidor Gitea local en un puerto aleatorio
2. Crea un usuario administrador
3. Guarda en caché los repositorios de paquetes de prueba como clones bare en `.cache/git-repos/` (hace fetch para refrescarlos en ejecuciones posteriores)
4. Sube los repos en caché a Gitea mediante go-git (herramienta `populate-repos`)
5. Pasa las variables de entorno `TOWN_OS_TEST_REPO_CORE_URL` y `TOWN_OS_TEST_REPO_EXTRAS_URL` al contenedor de pruebas para que todos los clones de git vayan a la instancia local de Gitea

La caché de clones bare en `.cache/git-repos/` persiste entre reinicios de Gitea, así que solo la primera ejecución llega a GitHub. Las ejecuciones posteriores traen las actualizaciones y las suben a la nueva instancia de Gitea. `make clean` elimina la caché.

Esto es transparente -- no hacen falta cambios de código. Las pruebas recurren a las URL de GitHub si las variables de entorno no están definidas.

| Objetivo              | Descripción                                                    |
| --------------------- | --------------------------------------------------------------- |
| `make gitea`          | Arranca el contenedor local de Gitea y crea el usuario administrador. |
| `make gitea-populate` | Migra los repos de prueba desde GitHub a la Gitea local.        |
| `make gitea-stop`     | Detiene y elimina el contenedor local de Gitea.                 |

Cada directorio de trabajo obtiene su propia instancia de Gitea (mediante `INSTANCE_ID`), de modo que las ejecuciones de pruebas concurrentes no entran en conflicto.

### Compilación

| Objetivo                      | Descripción                                                                                                            |
| ----------------------------- | ---------------------------------------------------------------------------------------------------------------------- |
| `make production-image`       | Construye la imagen base de producción (para las pruebas de integración).                                              |
| `make dev-production-image`   | Construye la imagen base de producción (para desarrollo).                                                              |
| `make test-image`             | Construye la imagen de contenedor de pruebas (incluye el binario de pruebas de integración).                           |
| `make dev-image`              | Construye la imagen del contenedor de desarrollo.                                                                      |
| `make ui-integration-image`   | Construye la imagen del contenedor de pruebas de integración de la interfaz.                                           |
| `make build-networkcontroller`| Construye localmente el binario del controlador de red (`town-os-networkcontroller`).                                  |
| `make ui-image`               | Construye la imagen de la interfaz localmente como `localhost/town-os-ui:<INSTANCE_ID>` para las pruebas (nunca descarga la imagen de interfaz de quay). |
| `make nc-image`               | Construye localmente la imagen del controlador de red para las pruebas; `make nc-image-dev` hace lo mismo contra la base de desarrollo. |
| `make ingress-image`          | Construye localmente la imagen del ingress para las pruebas.                                                           |
| `make pull-images`            | Descarga todas las imágenes de contenedor desde Docker Hub y las guarda en la caché de imágenes del checkout. Se ejecuta automáticamente si falta alguna imagen en caché. |

Desarrollo e integración usan imágenes base de producción y cachés de compilación separadas, de modo que las compilaciones concurrentes no pueden interferir entre sí.

### Publicación y Push

Todas las imágenes de publicación se suben a `quay.io/town/`. Todas las etiquetas que se suben están particionadas por arquitectura: cada host sube su arquitectura nativa como `rc.<fecha>-<arch>` / `rc.latest-<arch>` (candidatas a publicación) o `release.<fecha>-<arch>` / `latest-<arch>` (publicaciones), donde `<arch>` es la forma cruda de `uname -m` — `x86_64` o `aarch64`, *no* los nombres de plataforma OCI `amd64`/`arm64`. Los nombres simples (`rc.latest`, `latest` y las etiquetas con fecha) existen únicamente como listas de manifiestos multiarquitectura ensambladas por `make manifest-rc` / `make manifest-release` después de que todas las arquitecturas hayan subido; un nombre simple nunca debe subirse como etiqueta de una sola arquitectura, ya que falla en la otra arquitectura con `exec format error`.

En tiempo de ejecución, el controlador del sistema deriva la etiqueta de todas las imágenes hermanas (interfaz, Rolodex, controlador de red, ingress) de un único valor: la variable de entorno `TOWN_OS_TAG` si está definida y, en caso contrario, `rc.latest-<arch>`. No existe ninguna versión fijada en tiempo de compilación — el sistema de compilación de la instalación establece `TOWN_OS_TAG` en la unidad de systemd del controlador del sistema para fijar una publicación y, sin anulación, un equipo siempre sigue `rc.latest-<arch>`.

| Objetivo                    | Descripción                                                                                                        |
| --------------------------- | ------------------------------------------------------------------------------------------------------------------ |
| `make release-image`        | Construye la imagen de publicación del controlador del sistema (`quay.io/town/town`).                              |
| `make release-ui-image`     | Construye la imagen de publicación de la interfaz (`quay.io/town/ui`).                                             |
| `make release-proton-image` | Construye la imagen de publicación del runner de Proton (`quay.io/town/proton`). **Requiere `PROTON_ENABLED=1`**; en caso contrario el objetivo no existe. |
| `make release-nc-image`     | Construye la imagen de publicación del controlador de red (`quay.io/town/networkcontroller`).                      |
| `make release-ingress-image`| Construye la imagen de publicación del ingress (`quay.io/town/ingress`).                                           |
| `make release-build`        | Descarga las imágenes, ejecuta `test-full` y después construye las imágenes de publicación. Incluye el runner de Proton cuando `PROTON_ENABLED=1`. |
| `make push`                 | Alias de `push-rc`.                                                                                                |
| `make push-rc`              | Sube todas las imágenes (controlador del sistema, interfaz, controlador de red; Proton cuando `PROTON_ENABLED=1`) como candidatas a publicación por arquitectura (`rc.<fecha>-<arch>` + `rc.latest-<arch>`). |
| `make manifest-rc`          | Ensambla y sube las listas de manifiestos multiarquitectura simples `rc.<fecha>` / `rc.latest` a partir de las etiquetas por arquitectura. Ejecútalo una vez tras haber subido todas las arquitecturas. |
| `make push-release`         | Ejecuta `release-build` y después sube todas las imágenes como publicación por arquitectura (`release.<fecha>-<arch>` + `latest-<arch>`). |
| `make manifest-release`     | Ensambla y sube las listas de manifiestos multiarquitectura simples `release.<fecha>` / `latest` a partir de las etiquetas por arquitectura. Ejecútalo una vez tras haber subido todas las arquitecturas. |
| `make push-ui-rc`           | Sube solo la imagen de la interfaz como candidata a publicación por arquitectura (`rc.<fecha>-<arch>` + `rc.latest-<arch>`). |
| `make push-ui-release`      | Sube solo la imagen de la interfaz como publicación por arquitectura (`release.<fecha>-<arch>` + `latest-<arch>`). |
| `make push-proton-rc`       | Sube solo la imagen del runner de Proton como candidata a publicación. **Requiere `PROTON_ENABLED=1`**.             |
| `make push-proton-release`  | Sube solo la imagen del runner de Proton como publicación. **Requiere `PROTON_ENABLED=1`**.                         |
| `make push-nc-rc`           | Sube solo la imagen del controlador de red como candidata a publicación por arquitectura (`rc.<fecha>-<arch>` + `rc.latest-<arch>`). |
| `make push-nc-release`      | Sube solo la imagen del controlador de red como publicación por arquitectura (`release.<fecha>-<arch>` + `latest-<arch>`). |
| `make push-ingress-rc`      | Sube solo la imagen del ingress como candidata a publicación por arquitectura (`rc.<fecha>-<arch>` + `rc.latest-<arch>`). |
| `make push-ingress-release` | Sube solo la imagen del ingress como publicación por arquitectura (`release.<fecha>-<arch>` + `latest-<arch>`).    |
| `make push-tag PUSH_TAG=x`  | Construye y sube todas las imágenes con la etiqueta personalizada `x`. Incluye el runner de Proton cuando `PROTON_ENABLED=1`. |

### Autenticación de Registros

| Objetivo           | Descripción                                                                                       |
| ------------------ | ------------------------------------------------------------------------------------------------- |
| `make docker-login`| Inicia sesión en Docker Hub usando `DOCKER_USERNAME` / `DOCKER_PASSWORD` de `.env`. Se omite si no están definidas. |
| `make quay-login`  | Inicia sesión en Quay.io usando `QUAY_USERNAME` / `QUAY_PASSWORD` de `.env`. Se omite si no están definidas. |

### Gestión de Btrfs

| Objetivo               | Descripción                                                                 |
| ---------------------- | --------------------------------------------------------------------------- |
| `make btrfs`           | Crea un volumen btrfs loopback de 50 GB para las pruebas de integración.    |
| `make clean-btrfs`     | Desmonta, desconecta los dispositivos de bucle y elimina el volumen btrfs de pruebas de integración. |
| `make btrfs-dev`       | Crea un volumen btrfs loopback de 50 GB para el entorno de desarrollo.      |
| `make clean-btrfs-dev` | Desmonta, desconecta los dispositivos de bucle y elimina el volumen btrfs de desarrollo. |
| `make dev-btrfs`       | Crea el volumen btrfs de desarrollo solo si no hay uno montado ya.          |

Los entornos de desarrollo y de pruebas de integración usan volúmenes btrfs, imágenes de contenedor y cachés de compilación separados, de modo que pueden ejecutarse simultáneamente sin conflicto.

### Limpieza

`make test-full` limpia automáticamente todos los contenedores de integración, el registro, Gitea y los volúmenes btrfs loopback una vez terminadas las pruebas. Una trampa EXIT del shell garantiza que la limpieza se ejecute incluso cuando se interrumpe con señales. Cada objetivo de pruebas de integración (`test-integration`, `test-ui-integration`) usa además su propia trampa EXIT para garantizar la limpieza del loopback btrfs con independencia de cómo termine la receta (éxito, fallo o interrupción por señal). El objetivo `clean-btrfs` incluye una red de seguridad que busca dispositivos de bucle huérfanos respaldados por imágenes btrfs del directorio actual, cubriendo los casos en que faltan los archivos de seguimiento.

| Objetivo                 | Descripción                                                                             |
| ------------------------ | --------------------------------------------------------------------------------------- |
| `make clean`             | Elimina el directorio de caché de compilación `.cache/`.                                |
| `make clean-dev`         | Detiene todos los contenedores de desarrollo, desmantela el btrfs de desarrollo y elimina dev-data/dev-repos. |
| `make clean-cache`       | Elimina los datos de desarrollo, los repos de desarrollo y los datos de Rolodex de desarrollo del directorio de estado efímero. |
| `make clean-integration` | Elimina los contenedores de pruebas de integración (pruebas, backend de la interfaz, runner de la interfaz) y limpia btrfs. |
| `make clean-btrfs`       | Desmonta y elimina el volumen btrfs de pruebas de integración y los dispositivos de bucle huérfanos. |
| `make clean-image-cache` | Elimina la caché de imágenes de este checkout (`.cache/images/`).                       |
| `make clean-containers`  | Elimina todos los contenedores de town-os y de preflight de cualquier directorio de trabajo / instancia. |
| `make clean-all`         | Limpia todo: todos los contenedores, la caché de compilación, desarrollo, integración y btrfs. |

### Comprobaciones Previas

| Objetivo          | Descripción                                                                                                    |
| ----------------- | -------------------------------------------------------------------------------------------------------------- |
| `make preflight-dev` | Valida el entorno de desarrollo: comprueba podman, btrfs-progs, las credenciales de repositorio y la red bridge. |

### SSH

| Objetivo   | Descripción                                                                                   |
| ---------- | --------------------------------------------------------------------------------------------- |
| `make ssh` | Conecta por SSH a un dispositivo Town OS en marcha en `town-os.local` (limpia automáticamente las claves de host obsoletas). |

### Comprobaciones de Dependencias

Normalmente no se invocan directamente; se ejecutan como prerrequisitos de otros objetivos.

| Objetivo                   | Descripción                                          |
| -------------------------- | ---------------------------------------------------- |
| `make check-go`            | Verifica que `go` está disponible.                   |
| `make check-bun`           | Verifica que `bun` está disponible.                  |
| `make check-podman`        | Verifica que `podman` está disponible.               |
| `make check-runc`          | Verifica que `runc` está disponible.                 |
| `make check-btrfs`         | Verifica que `mkfs.btrfs` está disponible.           |
| `make check-golangci-lint` | Verifica que `golangci-lint` está disponible.        |
| `make check-python3`       | Verifica que `python3` está disponible.              |
| `make check-libsystemd`    | Verifica que existen las cabeceras de desarrollo de libsystemd. |

## Licencia

GNU Affero GPL 3.0
