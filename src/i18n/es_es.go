package i18n

// esESMessages contains all Spanish translations.
var esESMessages = map[string]string{ //nolint:gosec // G101 -- map keys are message IDs, not credentials
	// Authentication and authorization.
	MsgAuthMissingToken:   "falta el token de autorización",
	MsgAuthInvalidSession: "sesión no válida",
	MsgAuthAdminRequired:  "se requiere acceso de administrador",

	// Authentication.
	MsgAuthInvalidCredentials: "credenciales no válidas",
	MsgAuthNotConfigured:      "la autenticación no está configurada",

	// Account management.
	MsgAccountAdminStatusImmutable: "el estado de administrador no se puede cambiar después de crear la cuenta",
	MsgAccountListError:            "listar cuentas",
	MsgAccountCheckSessions:        "comprobar sesiones de administrador activas",
	MsgAccountCreateFailed:         "error al crear la cuenta",

	// Settings.
	MsgSettingNotFound:     "no se encontró la configuración %q",
	MsgSettingKeyRequired:  "la clave es obligatoria",
	MsgSettingInvalidBytes: "valor de bytes no válido para %q: %v",
	MsgSettingsMgrMissing:  "el gestor de configuración no está disponible",

	// Audit.
	MsgAuditNotConfigured: "el registro de auditoría no está configurado",

	// Systemd units.
	MsgUnitEnableDisableNotAllowed: "no se permite habilitar/deshabilitar",
	MsgUnitCannotStopController:    "no se puede detener el systemcontroller",
	MsgUnitInvalidLines:            "parámetro de líneas no válido",
	MsgUnitInvalidSince:            "parámetro «since» no válido",
	MsgUnitInvalidUntil:            "parámetro «until» no válido",
	MsgUnitInvalidPriority:         "parámetro de prioridad no válido",

	// Repository management.
	MsgRepoInvalidURL: "url no válida",

	// Pages management.
	MsgPagesNotConfigured:    "páginas no configuradas",
	MsgPagesGitNotConfigured: "cliente git o directorio de páginas no configurado",

	// Package installation.
	MsgInstallNoRepoRoot:      "no hay ninguna raíz de repositorio configurada",
	MsgInstallSummaryUpgrade:  "Actualizar %s de %s a %s",
	MsgInstallSummaryInstall:  "Instalar %s %s",
	MsgInstallSummaryImage:    "Imagen: %s",
	MsgInstallSummaryVolumes:  "%d volumen(es)",
	MsgInstallSummaryNewVols:  "%d nuevos",
	MsgInstallSummaryMigrated: "%d migrados",
	MsgInstallSummaryNoVols:   "Sin volúmenes",
	MsgInstallSummaryPorts:    "Puertos externos: %s",
	MsgInstallSummaryConfig:   "Se requiere configuración",
	MsgInstallSummaryVMImage:  "Imagen de VM: %s",

	// Package manifest.
	MsgManifestFieldsRequired: "repo, nombre y versión son obligatorios",
	MsgManifestNotFound:       "no se encontró el manifiesto del paquete: %s/%s@%s",

	// Rebuild git.
	MsgRebuildFieldsRequired:    "repo, nombre y versión son obligatorios",
	MsgRebuildRepoNotConfigured: "raíz de repositorio no configurada",
	MsgRebuildGitNotConfigured:  "cliente git no configurado",

	// Archive operations.
	MsgArchiveSubvolumeRequired: "el campo subvolumen es obligatorio",
	MsgArchiveFileRequired:      "se requiere un archivo comprimido: %v",
	MsgArchiveUnsupportedFormat: "formato de descarga no admitido: %s",
	MsgArchiveUnpackSuccess:     "archivo descomprimido correctamente",

	// Pages extended messages.
	MsgPagesDirNotConfigured:       "directorio de páginas no configurado",
	MsgPagesNameRequired:           "el campo nombre es obligatorio",
	MsgPagesUploadArchiveOnly:      "la subida solo se permite para páginas de tipo archivo",
	MsgPagesArchiveRebuildRequired: "las páginas de tipo archivo deben reconstruirse subiendo un nuevo archivo mediante /pages/upload",

	// Monitoring.
	MsgMonitoringNotConfigured: "la monitorización no está configurada",

	// Upgrades.
	MsgUpgradeSettingsMissing: "el gestor de configuración no está disponible",

	// Audit action descriptions (shown in the audit log).
	MsgAuditCreateFilesystem:         "crear sistema de archivos",
	MsgAuditModifyFilesystem:         "modificar sistema de archivos",
	MsgAuditRemoveFilesystem:         "eliminar sistema de archivos",
	MsgAuditAddRepository:            "añadir repositorio",
	MsgAuditRemoveRepository:         "eliminar repositorio",
	MsgAuditMoveRepository:           "mover repositorio",
	MsgAuditRefreshRepositories:      "actualizar repositorios",
	MsgAuditInstallPackage:           "instalar paquete",
	MsgAuditUninstallPackage:         "desinstalar paquete",
	MsgAuditPurgeUninstalledVolumes:  "purgar volúmenes desinstalados",
	MsgAuditPurgeVolumes:             "purgar volúmenes",
	MsgAuditDisablePackage:           "deshabilitar paquete",
	MsgAuditEnablePackage:            "habilitar paquete",
	MsgAuditSetUnitStatus:            "establecer estado de unidad",
	MsgAuditCreateAccount:            "crear cuenta",
	MsgAuditUpdateAccount:            "actualizar cuenta",
	MsgAuditDisableAccount:           "deshabilitar cuenta",
	MsgAuditAuthenticate:             "autenticar",
	MsgAuditRevokeSession:            "revocar sesión",
	MsgAuditUpdateSetting:            "actualizar configuración",
	MsgAuditDismissUpgrades:          "descartar actualizaciones de paquetes",
	MsgAuditUploadArchive:            "subir archivo",
	MsgAuditDownloadArchive:          "descargar archivo",
	MsgAuditCreatePage:               "crear página",
	MsgAuditUpdatePage:               "actualizar página",
	MsgAuditRemovePage:               "eliminar página",
	MsgAuditRebuildPage:              "reconstruir página",
	MsgAuditUploadPageArchive:        "subir archivo de página",
	MsgAuditEnableAccount:            "habilitar cuenta",
	MsgAuditRebuildGit:               "reconstruir git",
	MsgAuditUploadVMImage:            "subir imagen de vm",
	MsgAuditDeleteVMImage:            "eliminar imagen de vm",
	MsgAuditAddDNSRecord:             "añadir registro dns",
	MsgAuditRemoveDNSRecord:          "eliminar registro dns",
	MsgAuditSetDNSTLD:                "establecer tld de dns",
	MsgAuditSetupDNS:                 "configurar dns",
	MsgAuditRemovePackageVolume:      "eliminar volumen de paquete",
	MsgAuditRemovePackageVolumeGroup: "eliminar grupo de volúmenes de paquete",
	MsgAuditClearLastResponses:       "borrar respuestas de instalación en caché",
	MsgAuditSetSystemServiceStatus:   "establecer estado de servicio del sistema",
	MsgAuditRefreshSystemServices:    "actualizar servicios del sistema",
	MsgAuditCreateNetwork:            "crear red",
	MsgAuditRemoveNetwork:            "eliminar red",
	MsgAuditEnableNetwork:            "habilitar red",
	MsgAuditDisableNetwork:           "deshabilitar red",
	MsgAuditAddNetworkPeer:           "añadir par de red",
	MsgAuditRemoveNetworkPeer:        "eliminar par de red",
	MsgAuditRefreshNetworkPeer:       "actualizar par de red",

	// Network-only account restrictions.
	MsgAuthNetworkOnlyRestricted:    "esta cuenta solo puede usar los endpoints de inscripción de red y almacenamiento de objetos",
	MsgAuthNetworkOnlyNetworkDenied: "esta cuenta no está autorizada en esa red",
	MsgAuthWireGuardPeerNotOwned:    "esta cuenta solo puede actualizar los pares que ha inscrito",
	MsgAuthSessionNotOwned:          "esta cuenta solo puede revocar sus propias sesiones",
	MsgAuthObjectStorageRequired:    "se requiere acceso de administrador o de almacenamiento de objetos",

	// Object storage (gfeh).
	MsgArchiveGfehRefused:        "las cargas y descargas de archivos no pueden dirigirse a una partición de almacenamiento de objetos",
	MsgGfehNotConfigured:         "el almacenamiento de objetos no está configurado",
	MsgGfehNameRequired:          "el campo nombre es obligatorio",
	MsgGfehPartitionExists:       "la partición ya existe",
	MsgGfehPartitionNotFound:     "partición no encontrada",
	MsgGfehNetworkRequired:       "el campo red es obligatorio",
	MsgGfehPrincipalRequired:     "el campo principal es obligatorio",
	MsgGfehPathRequired:          "el campo ruta es obligatorio",
	MsgGfehUnknownAccount:        "no existe esa cuenta",
	MsgAuditCreateGfehPartition:  "crear partición de almacenamiento de objetos",
	MsgAuditModifyGfehPartition:  "modificar partición de almacenamiento de objetos",
	MsgAuditRemoveGfehPartition:  "eliminar partición de almacenamiento de objetos",
	MsgAuditAddGfehPrincipal:     "añadir usuario de almacenamiento de objetos",
	MsgAuditRemoveGfehPrincipal:  "eliminar usuario de almacenamiento de objetos",
	MsgAuditAddGfehGrant:         "añadir permiso de almacenamiento de objetos",
	MsgAuditRevokeGfehGrant:      "revocar permiso de almacenamiento de objetos",
	MsgAuditWithdrawGfehExposure: "retirar enlace de almacenamiento de objetos",
}
