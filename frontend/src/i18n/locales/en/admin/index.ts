import overview from './overview'
import channels from './channels'
import accounts from './accounts'
import resources from './resources'
import ops from './ops'
import settings from './settings'
import audit from './audit'
import videoTasks from './videoTasks'
import promptAudit from './promptAudit'
import plugins from './plugins'
import forkEmailSignupFilter from './forkEmailSignupFilter'

import checkin from './checkin'

export default {
  checkin,
  ...overview,
  ...channels,
  ...accounts,
  ...resources,
  ...ops,
  ...settings,
  ...audit,
  ...videoTasks,
  ...promptAudit,
  ...plugins,
  ...forkEmailSignupFilter,
}
