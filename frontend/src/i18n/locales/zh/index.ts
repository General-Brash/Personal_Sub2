import landing from './landing'
import common from './common'
import dashboard from './dashboard'
import checkin from './checkin'
import bank from './bank'
import batchImage from './batchImage'
import admin from './admin'
import misc from './misc'
import finance from './finance'

export default {
  ...landing,
  ...common,
  ...dashboard,
  ...checkin,
  ...bank,
  ...batchImage,
  admin,
  ...misc,
  ...finance,
}
