export default {
  dataCleanup: {
    title: 'Data Cleanup',
    lastRun: 'Last scheduled cleanup',
    historyNotice: 'Statistics include permanent daily history. The request list contains only retained original details.',
    tabs: { overview: 'Storage', cleanup: 'Cleanup and Compaction', policies: 'Retention', tasks: 'Tasks' },
    categories: { serverRetry: 'Server auto retry', httpAccess: 'Normal successful access', auditStatistics: 'Read-only statistics audit', runtime: 'Runtime logs', errors: 'Operational errors', alerts: 'Alerts', minuteMetrics: 'Minute metrics', hourlyMetrics: 'Hourly metrics', dailyMetrics: 'Daily metrics', cleanupAudit: 'Legacy runtime cleanup audits', operationAudit: 'Operation audit', usageCompaction: 'Historical usage compaction', promptAudit: 'Prompt audit' },
    databaseSize: 'Database size', totalRequests: 'Lifetime requests', totalTokens: 'Lifetime tokens', totalCost: 'Lifetime usage cost',
    table: 'Table', rows: 'Estimated rows', dataSize: 'Table data', indexSize: 'Indexes', total: 'Total size', state: 'Status',
    protected: 'Protected', managed: 'Policy managed', closedMonths: 'Compacted months', month: 'Month', users: 'Users',
    category: 'Category', startDate: 'Start date', endExclusive: 'End date (exclusive)', preview: 'Preview', allRecords: 'All records',
    usageProtected: 'Current-month details, lifetime totals and financial records are protected. Only completed calendar months can be compacted.',
    matched: 'Matched records', retained: 'Retained evidence', estimated: 'Estimated allocated size', confirmation: 'Confirmation', totp: 'Two-factor code', compact: 'Compact and clean', execute: 'Run cleanup',
    diskNotice: 'Deleted space is normally reused internally; disk files may not shrink immediately.',
    automatic: 'Scheduled cleanup', schedule: 'Schedule', enabled: 'Enabled', retention: 'Retention days', closedMonthPolicy: 'Compact completed months; numeric history is permanent',
    time: 'Created', operator: 'Operator', progress: 'Progress', deleted: 'Deleted', error: 'Error',
    status: { pending: 'Pending', running: 'Running', succeeded: 'Completed', failed: 'Failed', canceled: 'Canceled' },
    cancelTitle: 'Cancel task', cancelNotice: 'Stop future batches. Committed compaction and deletion are not undone.',
    loadFailed: 'Failed to load cleanup data', previewFailed: 'Preview failed', executeFailed: 'Submission failed', saveFailed: 'Failed to save policies', cancelFailed: 'Cancellation failed', submitted: 'Task submitted'
  }
}
