import overview from './overview'
import channels from './channels'
import accounts from './accounts'
import resources from './resources'
import ops from './ops'
import settings from './settings'
import audit from './audit'
import promptAudit from './promptAudit'
import plugins from './plugins'
import secondaryReview from './secondaryReview'
import oidcProvider from './oidcProvider'
import serverAutoRetry from './serverAutoRetry'

export default {
  ...overview,
  ...channels,
  ...accounts,
  ...resources,
  ...ops,
  ...settings,
  ...audit,
  ...promptAudit,
  ...plugins,
  ...secondaryReview,
  ...oidcProvider,
  ...serverAutoRetry,
}
