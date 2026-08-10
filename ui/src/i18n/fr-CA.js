import derive from './derive.js'
import frFR from './fr-FR.js'

/**
 * Strings where Canadian French departs from the French of France.
 *
 * Three departures, all substantive rather than orthographic:
 *
 * - **téléverser.** France writes *envoyer* for uploading a file. Québec has a
 *   distinct verb, standardised by the OQLF and in ordinary use, that pairs
 *   with *télécharger* the way "upload" pairs with "download". Keeping
 *   *envoyer* leaves both directions sharing one word in a dialog that shows
 *   them side by side.
 * - **dépôt.** France's technical writing borrows "repository" wholesale.
 *   Canadian French reaches for the French term where one exists, and this is
 *   the change a reader notices first — it appears on every packages screen.
 * - **courriel.** The Québec word for email, and the one point in this catalog
 *   where fr-FR's *E-mail* would read as plainly foreign.
 */
export const frCAOverrides = {
  // Uploading is téléverser, not envoyer.
  'storage.upload_archive_label': 'Téléverser une archive',
  'storage.toast_archive_uploaded': 'Archive téléversée',
  'archive.upload_title': 'Téléverser une archive',
  'archive.upload_description': 'Téléverser et extraire une archive dans le volume.',
  'archive.stop_service_upload': "Arrêter le service pendant le téléversement",
  'archive.upload_btn': 'Téléverser',
  'settings.archive_size_description': "La taille maximale de fichier autorisée pour le téléversement d'archives.",
  'settings.timeout_description': "Le temps maximal autorisé pour décompresser une archive téléversée avant l'annulation de l'opération.",

  // A repository is a dépôt.
  'dashboard.stat_repositories': 'Dépôts',
  'dashboard.stat_package_repositories': 'Dépôts de paquets',
  'packages.description': 'Gérer les paquets et les dépôts',
  'packages.tab_repositories': 'Dépôts',
  'packages.group_by_repo': 'Grouper par dépôt',
  'packages.col_repository': 'Dépôt',
  'packages.add_repo_btn': 'Ajouter un dépôt',
  'packages.repo_priority_hint':
    "Le premier dépôt a la priorité la plus élevée. Si le même paquet apparaît dans plusieurs dépôts, celui le plus proche du haut est utilisé. Utilisez les boutons fléchés pour réorganiser.",
  'packages.move_repo_up_label': 'Monter le dépôt',
  'packages.move_repo_down_label': 'Descendre le dépôt',
  'packages.remove_repo_label': 'Supprimer le dépôt',
  'packages.add_repo_dialog_title': 'Ajouter un dépôt',
  'packages.add_repo_dialog_description': 'Ajouter un nouveau dépôt de paquets par nom et URL.',
  'packages.repo_url_label': 'URL du dépôt',
  'packages.remove_repo_dialog_title': 'Supprimer le dépôt',
  'packages.toast_repo_added': 'Dépôt ajouté',
  'packages.toast_repo_removed': 'Dépôt supprimé',
  'packages.toast_repos_refreshed': 'Dépôts actualisés',
  'packages.toast_repos_refresh_failed': "Certains dépôts n'ont pas pu être actualisés",
  'pages.col_repository': 'Dépôt',
  'pages.create_dialog_description':
    "Ajouter un nouveau site statique depuis une archive, une image de conteneur ou un dépôt git.",
  'pages.repo_url_label': 'URL du dépôt',
  'pages.error_repo_required': "L'URL du dépôt est requise",
  'pages.delete_confirm_message':
    'Voulez-vous vraiment supprimer la page {name} ? Cela supprimera également les données du dépôt cloné.',
  'pages.rebuild_confirm_message': 'Récupérer le dernier contenu du dépôt git pour {name} ?',

  // Email is courriel.
  'register.email_label': 'Courriel',
  'users.col_email': 'Courriel',
  'users.email_label': 'Courriel',
  'create_user.email_label': 'Courriel',
}

/** French (Canada) — fr-FR with the Canadian departures applied. */
const frCA = derive(frFR, frCAOverrides)

export default frCA
