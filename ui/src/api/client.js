// Barrel: import core + all domain mixins, then re-export.
// Each client-*.js file adds methods to SystemControllerClient.prototype.

export { ApiError, SystemControllerClient } from './core.js'

import './client-ping.js'
import './client-auth.js'
import './client-storage.js'
import './client-repository.js'
import './client-package.js'
import './client-unit.js'
import './client-account.js'
import './client-audit.js'
import './client-settings.js'
import './client-upgrade.js'
import './client-archive.js'
import './client-pages.js'
import './client-locale.js'
import './client-monitoring.js'
