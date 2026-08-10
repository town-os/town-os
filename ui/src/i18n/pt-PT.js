import derive from './derive.js'
import ptBR from './pt-BR.js'

/**
 * Strings where European Portuguese departs from the Brazilian Portuguese of
 * pt-BR.js. This is by far the largest override map here, and it should be:
 * pt-PT and pt-BR are the pair on this list that diverge most, far enough that
 * Brazilian text reads as foreign in Portugal rather than merely regional.
 *
 * The rules being applied, roughly in order of how loudly they announce
 * themselves:
 *
 * - **Continuous aspect.** Brazil writes *Carregando...*; Portugal writes
 *   *A carregar...*. Every progress string, every toast, every boot step. This
 *   single rule accounts for a third of this file and is the difference a
 *   Portuguese reader notices in the first second.
 * - **utilizador**, not *usuário*.
 * - **palavra-passe**, not *senha*.
 * - **registo**, not *registro* — Portugal dropped the p in this word.
 * - **eliminar**, not *excluir*.
 * - **guardar**, not *salvar*.
 * - **gerir**, not *gerenciar*.
 * - **transferir**, not *baixar*, for downloading; **carregar** for uploading.
 * - **quota**, not *cota*. **estado**, not *status*. **contentor**, not
 *   *contêiner*. **ecrã**, not *tela*. **aplicações**, not *aplicativos*.
 *
 * One trap worth naming: *arquivo*. In Portugal it means archive, so the
 * tar.gz this system uploads and downloads stays an *arquivo* — but a file is
 * a *ficheiro*, so "sistema de arquivos" becomes "sistema de ficheiros". The
 * word changes in one sense and not the other.
 *
 * What is *not* here: the `objects.*` block. pt-BR.js leaves most of it in
 * English, so pt-PT inherits English there. That is a gap in the base catalog
 * rather than something this file should paper over locale by locale.
 */
export const ptPTOverrides = {
  // --- Login / register ---
  'login.error_invalid_credentials': 'Utilizador ou palavra-passe inválidos',
  'login.username_label': 'Utilizador',
  'login.password_label': 'Palavra-passe',
  'login.submit_loading': 'A entrar...',
  'login.api_offline_message': 'O controlador do sistema não está a responder. Aguarde enquanto reinicia.',
  'register.error_password_min_length': 'A palavra-passe deve ter pelo menos 8 caracteres',
  'register.error_passwords_mismatch': 'As palavras-passe não coincidem',
  'register.username_label': 'Utilizador',
  'register.password_label': 'Palavra-passe',
  'register.confirm_password_label': 'Confirmar Palavra-passe',
  'register.submit_loading': 'A criar...',

  // --- Dashboard / navigation ---
  'dashboard.loading': 'A carregar...',
  'dashboard.stat_user_accounts': 'Contas de utilizador',
  'dashboard.stat_audit_log': 'Registo de Auditoria',
  'dashboard.services_status_label': 'estado de {name}: {state}',
  'nav.users': 'Utilizadores',
  'nav.monitoring': 'Monitorização',
  'nav.audit_log': 'Registo de Auditoria',
  'nav.loading': 'A carregar...',
  'nav.api_offline_message':
    'O controlador do sistema não está a responder. Aguarde enquanto reinicia — esta página será atualizada automaticamente.',

  // --- Storage ---
  'storage.description': 'Gerir subvolumes btrfs',
  'storage.loading': 'A carregar...',
  'storage.col_quota': 'Quota',
  'storage.col_delete': 'Eliminar',
  'storage.download_archive_label': 'Transferir arquivo',
  'storage.upload_archive_label': 'Carregar arquivo',
  'storage.user_filesystems_title': 'Sistemas de Ficheiros do Utilizador',
  'storage.dialog_create_description': 'Crie um novo subvolume btrfs com uma quota opcional.',
  'storage.dialog_modify_description': 'Altere o nome ou a quota deste sistema de ficheiros.',
  'storage.quota_label': 'Quota (0 = ilimitado)',
  'storage.save_changes': 'Guardar Alterações',
  'storage.delete_dialog_title': 'Eliminar Sistema de Ficheiros',
  'storage.delete_confirm_btn': 'Eliminar',
  'storage.toast_created': 'Sistema de ficheiros criado',
  'storage.toast_modified': 'Sistema de ficheiros modificado',
  'storage.toast_removed': 'Sistema de ficheiros removido',
  'storage.toast_archive_downloaded': 'Arquivo transferido',
  'storage.toast_upload_no_file': 'Selecione um ficheiro de arquivo',
  'storage.toast_archive_uploaded': 'Arquivo carregado',
  'storage.delete_confirm_message':
    'Tem a certeza de que pretende eliminar o sistema de ficheiros {name}? Isto não pode ser anulado.',
  'storage.col_pkg_delete': 'Eliminar',
  'storage.delete_pkg_volume_title': 'Eliminar Volume de Pacote',
  'storage.delete_pkg_volume_message':
    'Tem a certeza de que pretende eliminar o volume {name}? Isto não pode ser anulado.',
  'storage.delete_pkg_group_title': 'Eliminar Volumes de Pacote',
  'storage.delete_pkg_group_message_package':
    'Tem a certeza de que pretende eliminar todos os volumes em {name}? Quaisquer serviços em execução deste pacote — e todas as dependências que instalou — serão parados primeiro. Isto não pode ser anulado.',
  'storage.delete_pkg_group_message_version':
    'Tem a certeza de que pretende eliminar todos os volumes de {name} versão {version}? O seu serviço e todos os serviços de dependência que instalou serão parados primeiro. Isto não pode ser anulado.',

  // --- Users ---
  'users.page_title': 'Town OS - Utilizadores',
  'users.title': 'Utilizadores',
  'users.description': 'Gerir contas de utilizador',
  'users.create_btn': 'Criar Utilizador',
  'users.loading': 'A carregar...',
  'users.col_username': 'Utilizador',
  'users.col_status': 'Estado',
  'users.role_user': 'Utilizador',
  'users.edit_dialog_title': 'Editar Utilizador',
  'users.edit_dialog_description': 'Atualize os detalhes da conta deste utilizador.',
  'users.new_password_label': 'Nova Palavra-passe',
  'users.confirm_password_label': 'Confirmar Palavra-passe',
  'users.save_changes': 'Guardar Alterações',
  'users.activate_dialog_title': 'Ativar Utilizador',
  'users.deactivate_dialog_title': 'Desativar Utilizador',
  'users.deactivate_last_admin_warning':
    'Aviso: Esta é a última conta de administrador ativada. Desativá-la bloqueará o acesso de todos os utilizadores ao sistema até que uma nova conta de administrador seja criada através do fluxo de inicialização.',
  'users.toast_updated': 'Utilizador atualizado',
  'users.toast_activated': 'Utilizador ativado',
  'users.toast_deactivated': 'Utilizador desativado',
  'users.error_password_min_length': 'A palavra-passe deve ter pelo menos 8 caracteres',
  'users.error_passwords_mismatch': 'As palavras-passe não coincidem',
  'users.activate_confirm_message': 'Tem a certeza de que pretende ativar o utilizador {username}?',
  'users.deactivate_confirm_message': 'Tem a certeza de que pretende desativar o utilizador {username}?',

  // --- Create user ---
  'create_user.page_title': 'Town OS - Criar Utilizador',
  'create_user.card_title': 'Criar Novo Utilizador',
  'create_user.username_label': 'Utilizador',
  'create_user.password_label': 'Palavra-passe',
  'create_user.confirm_password_label': 'Confirmar Palavra-passe',
  'create_user.submit_loading': 'A criar...',
  'create_user.submit': 'Criar Utilizador',
  'create_user.error_username_required': 'O utilizador é obrigatório',
  'create_user.error_password_required': 'A palavra-passe é obrigatória',
  'create_user.error_password_min_length': 'A palavra-passe deve ter pelo menos 8 caracteres',
  'create_user.error_confirm_required': 'A confirmação da palavra-passe é obrigatória',
  'create_user.error_passwords_mismatch': 'As palavras-passe não coincidem',
  'create_user.error_create_failed': 'Falha ao criar utilizador',

  // --- Services ---
  'system.description': 'Gerir pacotes instalados',
  'system.loading': 'A carregar...',
  'system.col_status': 'Estado',
  'system.system_services_description': 'Serviços de infraestrutura geridos pelo Town OS.',
  'system.refresh_warning_1':
    'Isto irá transferir as imagens de contentor mais recentes e reiniciar TODOS os serviços principais, incluindo o controlador do sistema.',
  'system.toast_starting': 'A iniciar {name}...',
  'system.toast_stopping': 'A parar {name}...',
  'system.toast_restarting': 'A reiniciar {name}...',
  'system.toast_action_waiting': 'A aguardar que o serviço fique {state}...',
  'system.refresh_toast_started': 'A atualizar serviços principais...',
  'system.refresh_in_progress': 'A atualizar serviços, a aguardar que o controlador do sistema reinicie...',
  'system.refresh_waiting_ui':
    'O controlador do sistema voltou. A aguardar que a interface web termine de reiniciar...',

  // --- Boot stepper ---
  'boot.label.boot_controller': 'A inicializar o controlador do sistema',
  'boot.label.boot_dns': 'A inicializar o DNS',
  'boot.label.boot_services': 'A iniciar serviços de sistema',
  'boot.label.restart_packages': 'A reiniciar pacotes',
  'boot.label.restarting_pkg': 'A reiniciar {name}',
  'boot.label.reconnecting': 'A aguardar o controlador... a reconectar em {seconds}s',
  'boot.label.mdns_hint':
    'Se isto persistir, o seu dispositivo pode estar a guardar em cache um endereço {hostname} desatualizado. Tente recarregar a página.',

  // --- Packages ---
  'packages.description': 'Gerir pacotes e repositórios',
  'packages.loading': 'A carregar...',
  'packages.col_status': 'Estado',
  'packages.col_repo_status': 'Estado',
  'packages.refreshing_btn': 'A atualizar...',
  'packages.manifest_loading': 'A carregar manifesto...',
  'packages.uninstall_dialog_description': 'Remova este pacote e, opcionalmente, purgue os seus volumes.',

  // --- Pages ---
  'pages.description': 'Gerir sites de conteúdo HTML estático',
  'pages.loading': 'A carregar...',
  'pages.col_status': 'Estado',
  'pages.delete_btn': 'Eliminar',
  'pages.create_dialog_description':
    'Adicione um novo site estático a partir de um arquivo, imagem de contentor ou repositório git.',
  'pages.save_changes': 'Guardar Alterações',
  'pages.delete_dialog_title': 'Eliminar Página',
  'pages.delete_confirm_btn': 'Eliminar',
  'pages.create_submit_provisioning': 'A aprovisionar...',
  'pages.status_provisioning': 'A aprovisionar',
  'pages.delete_confirm_message':
    'Tem a certeza de que pretende eliminar a página {name}? Isto também removerá os dados do repositório clonado.',

  // --- Audit log ---
  'audit.page_title': 'Town OS - Registo de Auditoria',
  'audit.title': 'Registo de Auditoria',
  'audit.loading': 'A carregar...',
  'audit.col_user': 'Utilizador',
  'audit.col_status': 'Estado',

  // --- Settings ---
  'settings.description': 'Predefinições de todo o sistema para todos os utilizadores',
  'settings.quota_title': 'Quota de Volume Predefinida',
  'settings.quota_description':
    'A quota predefinida aplicada a novos volumes de armazenamento. Defina como 0 para nenhum limite de quota.',
  'settings.quota_label': 'Quota',
  'settings.archive_size_description': 'O tamanho máximo de ficheiro permitido para carregamentos de arquivos.',
  'settings.save_btn': 'Guardar',
  'settings.saving_btn': 'A guardar...',
  'settings.toast_quota_updated': 'Quota predefinida atualizada',
  'settings.error_invalid_quota': 'Valor de quota inválido',
  'settings.format_no_quota': '0 (sem quota)',
  'settings.proton_image_description':
    'A imagem OCI usada para executar aplicações Windows através da camada de compatibilidade Proton da Valve. Isto deve ser configurado antes de instalar pacotes Proton.',
  'settings.monitoring_title': 'Painel de Monitorização',
  'settings.monitoring_description':
    'Selecione qual frontend de monitorização usar. O uPlot é um renderizador de gráficos integrado e leve. O Grafana oferece painéis completos com uso adicional de recursos.',
  'settings.toast_monitoring_updated':
    'Backend de monitorização atualizado. A alteração entra em vigor no próximo reinício do serviço.',
  'settings.toast_monitoring_restarting': 'A reiniciar a interface de monitorização...',
  'settings.toast_monitoring_ready': 'Interface de monitorização reiniciada ({backend})',
  'settings.toast_monitoring_timeout':
    'O backend de monitorização foi guardado, mas a interface de monitorização não voltou a ficar online a tempo. Verifique os logs do serviço.',
  'settings.dns_resolution_description':
    'Como os nomes fora das suas zonas locais são resolvidos. O modo Automático consulta os servidores raiz diretamente e recorre ao DNS encriptado ou a um resolvedor upstream apenas quando a rede bloqueia isso, mantendo-se privado sempre que possível. O modo Recursivo usa os servidores raiz e nada mais, o que falha completamente em redes que bloqueiam o DNS direto. O modo Encaminhamento usa sempre os resolvedores upstream.',
  'settings.toast_dns_resolution_saved': 'Modo de resolução de DNS atualizado. O Rolodex está a reiniciar.',
  'settings.dns_local_forwarders_description':
    'Usar os servidores DNS que a sua própria rede fornece em vez dos públicos. Uma rede que bloqueia DNS externo — um hotel, um portal cativo, alguns fornecedores — continua a responder às consultas enviadas ao resolvedor que ela própria distribui, e é isso que mantém a resolução de nomes a funcionar aí. Deixe desligado onde o DNS direto funciona: o resolvedor da sua rede vê todos os nomes que a sua casa consulta.',
  'settings.dns_local_forwarders_active': 'A encaminhar atualmente para: {value}',
  'settings.toast_dns_local_forwarders_saved': 'Encaminhadores DNS atualizados. O Rolodex está a reiniciar.',

  // --- Networks ---
  'networks.col_status': 'Estado',
  'networks.description':
    'Redes WireGuard com túnel. Desligue uma rede para cortar o acesso remoto aos seus serviços; o acesso local continua a funcionar.',

  // --- Journal ---
  'journal.loading': 'A carregar...',
  'journal.loading_journal': 'A carregar journal...',
  'journal.following_btn': 'A acompanhar',

  // --- Install preview / questions ---
  'install_preview.col_status': 'Estado',
  'install_preview.col_quota': 'Quota',
  'install_preview.upgrading_from': 'A atualizar da versão {version}',
  'install_preview.quota_exceeds_disk': 'O total das quotas de volume pode exceder o espaço em disco disponível.',
  'install_preview.has_questions_hint': 'As perguntas de configuração aparecerão no ecrã seguinte.',
  'install_questions.oauth_starting': 'A iniciar…',
  'install_questions.oauth_waiting': 'A aguardar aprovação…',

  // --- Archive dialogs ---
  'archive.download_title': 'Transferir Arquivo',
  'archive.download_btn': 'Transferir',
  'archive.stop_service_download': 'Parar o serviço durante a transferência',
  'archive.upload_title': 'Carregar Arquivo',
  'archive.upload_description': 'Carregue e extraia um arquivo no volume.',
  'archive.archive_file_label': 'Ficheiro de Arquivo',
  'archive.stop_service_upload': 'Parar o serviço durante o carregamento',
  'archive.upload_btn': 'Carregar',

  // --- Volume modify ---
  'volume_modify.description': 'Altere o nome ou a quota deste volume.',
  'volume_modify.quota_label': 'Quota (0 = ilimitado)',
  'volume_modify.save_btn': 'Guardar Alterações',

  // --- DNS ---
  'dns.description': 'Gerir registos DNS e configuração de serviços',
  'dns.loading': 'A carregar...',
  'dns.add_record_btn': 'Adicionar Registo',
  'dns.status_records': '{count} registo{s}',
  'dns.add_dialog_title': 'Adicionar Registo DNS',
  'dns.add_dialog_description': 'Crie um novo registo DNS.',
  'dns.add_submit': 'Adicionar Registo',
  'dns.change_tld_dialog_description':
    'Alterar o TLD irá reaprovisionar a zona DNS. Os registos existentes serão migrados para o novo domínio.',
  'dns.remove_dialog_title': 'Remover Registo DNS',
  'dns.remove_confirm_message': 'Remover o registo {name} ({type})?',
  'dns.toast_record_added': 'Registo DNS adicionado',
  'dns.toast_record_removed': 'Registo DNS removido',
  'dns.tab_records': 'Registos',
  'dns.bl.rbl_description':
    'Zonas de Realtime Blackhole List consultadas a pedido com um IP invertido (ex.: zen.spamhaus.org). Nada é transferido ou armazenado em cache.',
  'dns.bl.dnsbl_description':
    'Zonas de lista de bloqueio de domínio consultadas a pedido pelo nome do domínio (ex.: dbl.spamhaus.org). Têm precedência sobre as respostas upstream; nada é transferido ou armazenado em cache.',

  // --- Common ---
  'common.loading': 'A carregar...',
  'common.save': 'Guardar',
  'common.delete': 'Eliminar',

  // --- Progress dialog ---
  'progress.title_installing': 'A instalar...',
  'progress.title_uninstalling': 'A desinstalar...',
  'progress.title_refreshing': 'A atualizar...',
  'progress.starting': 'A iniciar...',
  'progress.compiling': 'A compilar pacote...',
  'progress.preparing_upgrade': 'A preparar atualização...',
  'progress.provisioning_volumes': 'A aprovisionar volumes...',
  'progress.seeding_data': 'A semear dados do volume...',
  'progress.applying_templates': 'A aplicar templates...',
  'progress.installing_dependencies': 'A instalar dependências...',
  'progress.saving_install': 'A guardar registo de instalação...',
  'progress.downloading_vm_image': 'A transferir imagem da VM...',
  'progress.installing_services': 'A iniciar serviços...',
  'progress.registering_dns': 'A registar DNS...',
  'progress.unregistering_dns': 'A remover registo DNS...',
  'progress.stopping_services': 'A parar serviços...',
  'progress.removing_network': 'A remover rede...',
  'progress.saving_responses': 'A guardar respostas...',
  'progress.removing_install': 'A remover registo de instalação...',
  'progress.uninstalling_dependencies': 'A desinstalar dependências...',
  'progress.cleaning_volumes': 'A limpar volumes...',
  'progress.refreshing': 'A atualizar repositórios...',

  // --- Object storage: only the strings pt-BR.js actually translated ---
  'objects.stopped_no_names':
    'O daemon desta partição não está a responder, portanto nenhum endereço é publicado. O Town OS tenta novamente a cada poucos minutos.',
  'objects.link_unavailable': 'A visualização HTTP não está a ser servida, portanto nada responde a esta ligação.',
  'objects.link_disabled': 'Esta ligação está desativada e não está a ser servida.',
}

/** Portuguese (Portugal) — pt-BR with the European departures applied. */
const ptPT = derive(ptBR, ptPTOverrides)

export default ptPT
