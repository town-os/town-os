package i18n

// frCAOverrides holds the strings where Canadian French departs from the
// French of France. There are two real departures, and both are substantive
// rather than orthographic.
//
// The first is téléverser. France writes envoyer for uploading a file;
// Québec has a distinct verb for it, standardised by the OQLF and in ordinary
// use, which pairs with télécharger the way "upload" pairs with "download".
// Reusing envoyer leaves the two directions sharing one word.
//
// The second is dépôt. France's technical writing borrows "repository"
// wholesale; Canadian French resists the anglicism where a French term exists,
// and dépôt is that term. This is the difference an actual reader notices.
var frCAOverrides = map[string]string{
	// Uploading is téléverser, not envoyer.
	MsgPagesUploadArchiveOnly:      "le téléversement n'est autorisé que pour les pages de type archive",
	MsgPagesArchiveRebuildRequired: "les pages archive doivent être reconstruites en téléversant une nouvelle archive via /pages/upload",
	MsgAuditUploadArchive:          "téléverser une archive",
	MsgAuditUploadPageArchive:      "téléverser une archive de page",
	MsgAuditUploadVMImage:          "téléverser une image VM",
	MsgArchiveGfehRefused:          "les téléversements et téléchargements d'archives ne peuvent pas viser une partition de stockage d'objets",

	// A repository is a dépôt.
	MsgInstallNoRepoRoot:        "aucune racine de dépôt configurée",
	MsgRebuildRepoNotConfigured: "racine de dépôt non configurée",
	MsgAuditAddRepository:       "ajouter un dépôt",
	MsgAuditRemoveRepository:    "supprimer un dépôt",
	MsgAuditMoveRepository:      "déplacer un dépôt",
	MsgAuditRefreshRepositories: "actualiser les dépôts",
}

// frCAMessages is fr-FR with the Canadian departures applied.
var frCAMessages = derive(frFRMessages, frCAOverrides)
