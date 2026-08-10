package i18n

// frFRMessages contains all French translations.
var frFRMessages = map[string]string{ //nolint:gosec // G101 -- map keys are message IDs, not credentials
	// Authentication and authorization.
	MsgAuthMissingToken:   "jeton d'autorisation manquant",
	MsgAuthInvalidSession: "session invalide",
	MsgAuthAdminRequired:  "accès administrateur requis",

	// Authentication.
	MsgAuthInvalidCredentials: "identifiants invalides",
	MsgAuthNotConfigured:      "l'authentification n'est pas configurée",

	// Account management.
	MsgAccountAdminStatusImmutable: "le statut d'administrateur ne peut pas être modifié après la création du compte",
	MsgAccountListError:            "lister les comptes",
	MsgAccountCheckSessions:        "vérifier les sessions administrateur actives",
	MsgAccountCreateFailed:         "échec de la création du compte",

	// Settings.
	MsgSettingNotFound:     "paramètre %q introuvable",
	MsgSettingKeyRequired:  "la clé est requise",
	MsgSettingInvalidBytes: "valeur en octets invalide pour %q : %v",
	MsgSettingsMgrMissing:  "gestionnaire de paramètres indisponible",

	// Audit.
	MsgAuditNotConfigured: "journalisation d'audit non configurée",

	// Systemd units.
	MsgUnitEnableDisableNotAllowed: "activation/désactivation non autorisée",
	MsgUnitCannotStopController:    "impossible d'arrêter le systemcontroller",
	MsgUnitInvalidLines:            "paramètre lines invalide",
	MsgUnitInvalidSince:            "paramètre since invalide",
	MsgUnitInvalidUntil:            "paramètre until invalide",
	MsgUnitInvalidPriority:         "paramètre priority invalide",

	// Repository management.
	MsgRepoInvalidURL: "url invalide",

	// Pages management.
	MsgPagesNotConfigured:    "pages non configurées",
	MsgPagesGitNotConfigured: "client git ou répertoire des pages non configuré",

	// Package installation.
	MsgInstallNoRepoRoot:      "aucune racine de repository configurée",
	MsgInstallSummaryUpgrade:  "Mettre à niveau %s de %s vers %s",
	MsgInstallSummaryInstall:  "Installer %s %s",
	MsgInstallSummaryImage:    "Image : %s",
	MsgInstallSummaryVolumes:  "%d volume(s)",
	MsgInstallSummaryNewVols:  "%d nouveau(x)",
	MsgInstallSummaryMigrated: "%d migré(s)",
	MsgInstallSummaryNoVols:   "Aucun volume",
	MsgInstallSummaryPorts:    "Ports externes : %s",
	MsgInstallSummaryConfig:   "Configuration requise",
	MsgInstallSummaryVMImage:  "Image VM : %s",

	// Package manifest.
	MsgManifestFieldsRequired: "repo, nom et version sont requis",
	MsgManifestNotFound:       "manifeste de paquet introuvable : %s/%s@%s",

	// Rebuild git.
	MsgRebuildFieldsRequired:    "repo, nom et version sont requis",
	MsgRebuildRepoNotConfigured: "racine de repository non configurée",
	MsgRebuildGitNotConfigured:  "client git non configuré",

	// Archive operations.
	MsgArchiveSubvolumeRequired: "le champ subvolume est requis",
	MsgArchiveFileRequired:      "fichier d'archive requis : %v",
	MsgArchiveUnsupportedFormat: "format de téléchargement non pris en charge : %s",
	MsgArchiveUnpackSuccess:     "archive décompressée avec succès",

	// Pages extended messages.
	MsgPagesDirNotConfigured:       "répertoire des pages non configuré",
	MsgPagesNameRequired:           "le champ nom est requis",
	MsgPagesUploadArchiveOnly:      "l'envoi n'est autorisé que pour les pages de type archive",
	MsgPagesArchiveRebuildRequired: "les pages archive doivent être reconstruites en envoyant une nouvelle archive via /pages/upload",

	// Monitoring.
	MsgMonitoringNotConfigured: "la surveillance n'est pas configurée",

	// Upgrades.
	MsgUpgradeSettingsMissing: "gestionnaire de paramètres indisponible",

	// Audit action descriptions (shown in the audit log).
	MsgAuditCreateFilesystem:         "créer un système de fichiers",
	MsgAuditModifyFilesystem:         "modifier un système de fichiers",
	MsgAuditRemoveFilesystem:         "supprimer un système de fichiers",
	MsgAuditAddRepository:            "ajouter un repository",
	MsgAuditRemoveRepository:         "supprimer un repository",
	MsgAuditMoveRepository:           "déplacer un repository",
	MsgAuditRefreshRepositories:      "actualiser les repositories",
	MsgAuditInstallPackage:           "installer un paquet",
	MsgAuditUninstallPackage:         "désinstaller un paquet",
	MsgAuditPurgeUninstalledVolumes:  "purger les volumes désinstallés",
	MsgAuditPurgeVolumes:             "purger les volumes",
	MsgAuditDisablePackage:           "désactiver un paquet",
	MsgAuditEnablePackage:            "activer un paquet",
	MsgAuditSetUnitStatus:            "définir l'état d'une unité",
	MsgAuditCreateAccount:            "créer un compte",
	MsgAuditUpdateAccount:            "mettre à jour un compte",
	MsgAuditDisableAccount:           "désactiver un compte",
	MsgAuditAuthenticate:             "authentifier",
	MsgAuditRevokeSession:            "révoquer une session",
	MsgAuditUpdateSetting:            "mettre à jour un paramètre",
	MsgAuditDismissUpgrades:          "ignorer les mises à niveau de paquets",
	MsgAuditUploadArchive:            "envoyer une archive",
	MsgAuditDownloadArchive:          "télécharger une archive",
	MsgAuditCreatePage:               "créer une page",
	MsgAuditUpdatePage:               "mettre à jour une page",
	MsgAuditRemovePage:               "supprimer une page",
	MsgAuditRebuildPage:              "reconstruire une page",
	MsgAuditUploadPageArchive:        "envoyer une archive de page",
	MsgAuditEnableAccount:            "activer un compte",
	MsgAuditRebuildGit:               "reconstruire git",
	MsgAuditUploadVMImage:            "envoyer une image VM",
	MsgAuditDeleteVMImage:            "supprimer une image VM",
	MsgAuditAddDNSRecord:             "ajouter un enregistrement DNS",
	MsgAuditRemoveDNSRecord:          "supprimer un enregistrement DNS",
	MsgAuditSetDNSTLD:                "définir le TLD DNS",
	MsgAuditSetupDNS:                 "configurer le DNS",
	MsgAuditRemovePackageVolume:      "supprimer un volume de paquet",
	MsgAuditRemovePackageVolumeGroup: "supprimer un groupe de volumes de paquet",
	MsgAuditClearLastResponses:       "effacer les réponses d'installation en cache",
	MsgAuditSetSystemServiceStatus:   "définir l'état d'un service système",
	MsgAuditRefreshSystemServices:    "actualiser les services système",
	MsgAuditCreateNetwork:            "créer un réseau",
	MsgAuditRemoveNetwork:            "supprimer un réseau",
	MsgAuditEnableNetwork:            "activer un réseau",
	MsgAuditDisableNetwork:           "désactiver un réseau",
	MsgAuditAddNetworkPeer:           "ajouter un pair réseau",
	MsgAuditRemoveNetworkPeer:        "supprimer un pair réseau",
	MsgAuditRefreshNetworkPeer:       "actualiser un pair réseau",

	// Network-only account restrictions.
	MsgAuthNetworkOnlyRestricted:    "ce compte ne peut utiliser que les points d'accès d'enrôlement réseau et de stockage objet",
	MsgAuthNetworkOnlyNetworkDenied: "ce compte n'est pas autorisé sur ce réseau",
	MsgAuthWireGuardPeerNotOwned:    "ce compte ne peut actualiser que les pairs qu'il a enrôlés",
	MsgAuthSessionNotOwned:          "ce compte ne peut révoquer que ses propres sessions",
	MsgAuthObjectStorageRequired:    "accès administrateur ou stockage d'objets requis",

	// Object storage (gfeh).
	MsgArchiveGfehRefused:        "les envois et téléchargements d'archives ne peuvent pas viser une partition de stockage d'objets",
	MsgGfehNotConfigured:         "le stockage d'objets n'est pas configuré",
	MsgGfehNameRequired:          "le champ nom est obligatoire",
	MsgGfehPartitionExists:       "la partition existe déjà",
	MsgGfehPartitionNotFound:     "partition introuvable",
	MsgGfehNetworkRequired:       "le champ réseau est obligatoire",
	MsgGfehPrincipalRequired:     "le champ principal est obligatoire",
	MsgGfehPathRequired:          "le champ chemin est obligatoire",
	MsgGfehUnknownAccount:        "compte inexistant",
	MsgAuditCreateGfehPartition:  "créer une partition de stockage d'objets",
	MsgAuditModifyGfehPartition:  "modifier une partition de stockage d'objets",
	MsgAuditRemoveGfehPartition:  "supprimer une partition de stockage d'objets",
	MsgAuditAddGfehPrincipal:     "ajouter un utilisateur de stockage d'objets",
	MsgAuditRemoveGfehPrincipal:  "supprimer un utilisateur de stockage d'objets",
	MsgAuditAddGfehGrant:         "ajouter une autorisation de stockage d'objets",
	MsgAuditRevokeGfehGrant:      "révoquer une autorisation de stockage d'objets",
	MsgAuditWithdrawGfehExposure: "retirer un lien de stockage d'objets",
}
