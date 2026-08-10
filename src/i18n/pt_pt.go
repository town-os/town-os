package i18n

// ptPTOverrides holds the strings where European Portuguese departs from the
// Brazilian Portuguese of pt_br.go. This is the largest override map in the
// package, and it should be: pt-PT and pt-BR are the pair on this list that
// diverge most, far enough that Brazilian text reads as foreign in Portugal
// rather than merely regional.
//
// The recurring swaps:
//
//   - utilizador, not usuário — the word for a person with an account.
//   - definições, not configurações — for the system's settings. Configuração
//     survives where it means a package's own configuration, which is why not
//     every occurrence of the word is listed here.
//   - registo, not registro — Portugal dropped the p in this word.
//   - ficheiro, not arquivo — for a file. Note the trap: the tar.gz this
//     system uploads and downloads is an arquivo in both, because there it
//     means archive. Only "sistema de ficheiros" and the file in
//     MsgArchiveFileRequired change; the archives stay arquivos.
//   - estado, not status.
//   - carregar and transferir, not enviar and baixar, for upload and download.
var ptPTOverrides = map[string]string{ //nolint:gosec // G101 -- map keys are message IDs, not credentials
	// Idiom.
	MsgAuthMissingToken: "token de autorização em falta",

	// estado, not status.
	MsgAccountAdminStatusImmutable: "o estado de administrador não pode ser alterado após a criação da conta",
	MsgAuditSetUnitStatus:          "definir estado da unidade",
	MsgAuditSetSystemServiceStatus: "definir estado do serviço de sistema",

	// definições, not configurações — the system's own settings.
	MsgSettingNotFound:        "definição %q não encontrada",
	MsgSettingsMgrMissing:     "gestor de definições não disponível",
	MsgUpgradeSettingsMissing: "gestor de definições não disponível",
	MsgAuditUpdateSetting:     "atualizar definição",

	// registo, not registro.
	MsgAuditNotConfigured:   "registo de auditoria não configurado",
	MsgAuditAddDNSRecord:    "adicionar registo dns",
	MsgAuditRemoveDNSRecord: "remover registo dns",

	// ficheiro, not arquivo — where the word means file rather than archive.
	MsgArchiveFileRequired:   "ficheiro de arquivo obrigatório: %v",
	MsgAuditCreateFilesystem: "criar sistema de ficheiros",
	MsgAuditModifyFilesystem: "modificar sistema de ficheiros",
	MsgAuditRemoveFilesystem: "remover sistema de ficheiros",

	// carregar and transferir, not enviar and baixar.
	MsgArchiveUnsupportedFormat:    "formato de transferência não suportado: %s",
	MsgPagesUploadArchiveOnly:      "o carregamento só é permitido para páginas do tipo arquivo",
	MsgPagesArchiveRebuildRequired: "páginas de arquivo devem ser reconstruídas carregando um novo arquivo via /pages/upload",
	MsgAuditUploadArchive:          "carregar arquivo",
	MsgAuditDownloadArchive:        "transferir arquivo",
	MsgAuditUploadPageArchive:      "carregar arquivo de página",
	MsgAuditUploadVMImage:          "carregar imagem de VM",
	MsgArchiveGfehRefused:          "carregamentos e transferências de arquivos não podem endereçar uma partição de armazenamento de objetos",

	// utilizador, not usuário.
	MsgAuditAddGfehPrincipal:    "adicionar utilizador de armazenamento de objetos",
	MsgAuditRemoveGfehPrincipal: "remover utilizador de armazenamento de objetos",

	// Remaining lexical departures.
	MsgMonitoringNotConfigured:      "monitorização não configurada",
	MsgAuditPurgeUninstalledVolumes: "purgar volumes desinstalados",
	MsgAuditPurgeVolumes:            "purgar volumes",
	MsgAuditDeleteVMImage:           "eliminar imagem de VM",
}

// ptPTMessages is pt-BR with the European departures applied.
var ptPTMessages = derive(ptBRMessages, ptPTOverrides)
