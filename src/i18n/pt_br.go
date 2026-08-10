package i18n

// ptBRMessages contains all Portuguese (Brazil) translations.
var ptBRMessages = map[string]string{
	// Authentication and authorization.
	MsgAuthMissingToken:   "token de autorização ausente",
	MsgAuthInvalidSession: "sessão inválida",
	MsgAuthAdminRequired:  "acesso de administrador necessário",

	// Authentication.
	MsgAuthInvalidCredentials: "credenciais inválidas",
	MsgAuthNotConfigured:      "a autenticação não está configurada",

	// Account management.
	MsgAccountAdminStatusImmutable: "o status de administrador não pode ser alterado após a criação da conta",
	MsgAccountListError:            "listar contas",
	MsgAccountCheckSessions:        "verificar sessões de administrador ativas",
	MsgAccountCreateFailed:         "falha na criação da conta",

	// Settings.
	MsgSettingNotFound:     "configuração %q não encontrada",
	MsgSettingKeyRequired:  "a chave é obrigatória",
	MsgSettingInvalidBytes: "valor de bytes inválido para %q: %v",
	MsgSettingsMgrMissing:  "gerenciador de configurações não disponível",

	// Audit.
	MsgAuditNotConfigured: "registro de auditoria não configurado",

	// Systemd units.
	MsgUnitEnableDisableNotAllowed: "ativar/desativar não permitido",
	MsgUnitCannotStopController:    "não é possível parar o systemcontroller",
	MsgUnitInvalidLines:            "parâmetro de linhas inválido",
	MsgUnitInvalidSince:            "parâmetro since inválido",
	MsgUnitInvalidUntil:            "parâmetro until inválido",
	MsgUnitInvalidPriority:         "parâmetro de prioridade inválido",

	// Repository management.
	MsgRepoInvalidURL: "url inválida",

	// Pages management.
	MsgPagesNotConfigured:    "pages não configurado",
	MsgPagesGitNotConfigured: "cliente git ou diretório de pages não configurado",

	// Package installation.
	MsgInstallNoRepoRoot:      "nenhuma raiz de repositório configurada",
	MsgInstallSummaryUpgrade:  "Atualizar %s de %s para %s",
	MsgInstallSummaryInstall:  "Instalar %s %s",
	MsgInstallSummaryImage:    "Imagem: %s",
	MsgInstallSummaryVolumes:  "%d volume(s)",
	MsgInstallSummaryNewVols:  "%d novo(s)",
	MsgInstallSummaryMigrated: "%d migrado(s)",
	MsgInstallSummaryNoVols:   "Nenhum volume",
	MsgInstallSummaryPorts:    "Portas externas: %s",
	MsgInstallSummaryConfig:   "Configuração necessária",
	MsgInstallSummaryVMImage:  "Imagem da VM: %s",

	// Package manifest.
	MsgManifestFieldsRequired: "repo, nome e versão são obrigatórios",
	MsgManifestNotFound:       "manifesto do pacote não encontrado: %s/%s@%s",

	// Rebuild git.
	MsgRebuildFieldsRequired:    "repo, nome e versão são obrigatórios",
	MsgRebuildRepoNotConfigured: "raiz de repositório não configurada",
	MsgRebuildGitNotConfigured:  "cliente git não configurado",

	// Archive operations.
	MsgArchiveSubvolumeRequired: "o campo subvolume é obrigatório",
	MsgArchiveFileRequired:      "arquivo compactado obrigatório: %v",
	MsgArchiveUnsupportedFormat: "formato de download não suportado: %s",
	MsgArchiveUnpackSuccess:     "arquivo descompactado com sucesso",

	// Pages extended messages.
	MsgPagesDirNotConfigured:       "diretório de pages não configurado",
	MsgPagesNameRequired:           "o campo nome é obrigatório",
	MsgPagesUploadArchiveOnly:      "o upload só é permitido para páginas do tipo arquivo",
	MsgPagesArchiveRebuildRequired: "páginas de arquivo devem ser reconstruídas enviando um novo arquivo via /pages/upload",

	// Monitoring.
	MsgMonitoringNotConfigured: "monitoramento não configurado",

	// Upgrades.
	MsgUpgradeSettingsMissing: "gerenciador de configurações não disponível",

	// Audit action descriptions (shown in the audit log).
	MsgAuditCreateFilesystem:         "criar sistema de arquivos",
	MsgAuditModifyFilesystem:         "modificar sistema de arquivos",
	MsgAuditRemoveFilesystem:         "remover sistema de arquivos",
	MsgAuditAddRepository:            "adicionar repositório",
	MsgAuditRemoveRepository:         "remover repositório",
	MsgAuditMoveRepository:           "mover repositório",
	MsgAuditRefreshRepositories:      "atualizar repositórios",
	MsgAuditInstallPackage:           "instalar pacote",
	MsgAuditUninstallPackage:         "desinstalar pacote",
	MsgAuditPurgeUninstalledVolumes:  "expurgar volumes desinstalados",
	MsgAuditPurgeVolumes:             "expurgar volumes",
	MsgAuditDisablePackage:           "desativar pacote",
	MsgAuditEnablePackage:            "ativar pacote",
	MsgAuditSetUnitStatus:            "definir status da unidade",
	MsgAuditCreateAccount:            "criar conta",
	MsgAuditUpdateAccount:            "atualizar conta",
	MsgAuditDisableAccount:           "desativar conta",
	MsgAuditAuthenticate:             "autenticar",
	MsgAuditRevokeSession:            "revogar sessão",
	MsgAuditUpdateSetting:            "atualizar configuração",
	MsgAuditDismissUpgrades:          "dispensar atualizações de pacote",
	MsgAuditUploadArchive:            "enviar arquivo",
	MsgAuditDownloadArchive:          "baixar arquivo",
	MsgAuditCreatePage:               "criar página",
	MsgAuditUpdatePage:               "atualizar página",
	MsgAuditRemovePage:               "remover página",
	MsgAuditRebuildPage:              "reconstruir página",
	MsgAuditUploadPageArchive:        "enviar arquivo de página",
	MsgAuditEnableAccount:            "ativar conta",
	MsgAuditRebuildGit:               "reconstruir git",
	MsgAuditUploadVMImage:            "enviar imagem de VM",
	MsgAuditDeleteVMImage:            "excluir imagem de VM",
	MsgAuditAddDNSRecord:             "adicionar registro dns",
	MsgAuditRemoveDNSRecord:          "remover registro dns",
	MsgAuditSetDNSTLD:                "definir tld dns",
	MsgAuditSetupDNS:                 "configurar dns",
	MsgAuditRemovePackageVolume:      "remover volume de pacote",
	MsgAuditRemovePackageVolumeGroup: "remover grupo de volumes de pacote",
	MsgAuditClearLastResponses:       "limpar respostas de instalação em cache",
	MsgAuditSetSystemServiceStatus:   "definir status do serviço de sistema",
	MsgAuditRefreshSystemServices:    "atualizar serviços de sistema",
	MsgAuditCreateNetwork:            "criar rede",
	MsgAuditRemoveNetwork:            "remover rede",
	MsgAuditEnableNetwork:            "ativar rede",
	MsgAuditDisableNetwork:           "desativar rede",
	MsgAuditAddNetworkPeer:           "adicionar peer de rede",
	MsgAuditRemoveNetworkPeer:        "remover peer de rede",
	MsgAuditRefreshNetworkPeer:       "atualizar peer de rede",

	// Network-only account restrictions.
	MsgAuthNetworkOnlyRestricted:    "esta conta só pode usar endpoints de inscrição de rede e armazenamento de objetos",
	MsgAuthNetworkOnlyNetworkDenied: "esta conta não tem permissão nessa rede",
	MsgAuthWireGuardPeerNotOwned:    "esta conta só pode atualizar peers que ela inscreveu",
	MsgAuthSessionNotOwned:          "esta conta só pode revogar as próprias sessões",
	MsgAuthObjectStorageRequired:    "é necessário acesso de administrador ou de armazenamento de objetos",

	// Object storage (gfeh).
	MsgArchiveGfehRefused:        "envios e downloads de arquivos não podem endereçar uma partição de armazenamento de objetos",
	MsgGfehNotConfigured:         "o armazenamento de objetos não está configurado",
	MsgGfehNameRequired:          "o campo nome é obrigatório",
	MsgGfehPartitionExists:       "a partição já existe",
	MsgGfehPartitionNotFound:     "partição não encontrada",
	MsgGfehNetworkRequired:       "o campo rede é obrigatório",
	MsgGfehPrincipalRequired:     "o campo principal é obrigatório",
	MsgGfehPathRequired:          "o campo caminho é obrigatório",
	MsgGfehUnknownAccount:        "conta inexistente",
	MsgAuditCreateGfehPartition:  "criar partição de armazenamento de objetos",
	MsgAuditModifyGfehPartition:  "modificar partição de armazenamento de objetos",
	MsgAuditRemoveGfehPartition:  "remover partição de armazenamento de objetos",
	MsgAuditAddGfehPrincipal:     "adicionar usuário de armazenamento de objetos",
	MsgAuditRemoveGfehPrincipal:  "remover usuário de armazenamento de objetos",
	MsgAuditAddGfehGrant:         "adicionar permissão de armazenamento de objetos",
	MsgAuditRevokeGfehGrant:      "revogar permissão de armazenamento de objetos",
	MsgAuditWithdrawGfehExposure: "retirar link de armazenamento de objetos",
}
