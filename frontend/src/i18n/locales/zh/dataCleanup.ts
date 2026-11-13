export default {
  dataCleanup: {
    title: '数据清理',
    lastRun: '上次定时清理',
    historyNotice: '统计包含永久保留的历史数值汇总；历史按日汇总，逐请求列表仅显示仍保留的原始明细。',
    tabs: { overview: '空间概览', cleanup: '清理与归并', policies: '保留策略', tasks: '任务记录' },
    categories: { serverRetry: '服务器自动重试', httpAccess: '普通成功访问', auditStatistics: '纯统计查询审计', runtime: '系统运行日志', errors: '运维错误', alerts: '告警事件', minuteMetrics: '分钟指标', hourlyMetrics: '小时指标', dailyMetrics: '日指标', cleanupAudit: '旧运行日志清理审计', operationAudit: '操作审计', usageCompaction: '历史用量归并', promptAudit: '提示词审计' },
    databaseSize: '数据库占用', totalRequests: '累计请求', totalTokens: '累计 Token', totalCost: '累计使用费用',
    table: '数据表', rows: '估算条数', dataSize: '表数据', indexSize: '索引', total: '总占用', state: '状态',
    protected: '受保护', managed: '按策略管理', closedMonths: '已归并月份', month: '月份', users: '用户数',
    category: '数据类别', startDate: '开始日期', endExclusive: '结束日期（不含）', preview: '预览', allRecords: '全部记录',
    usageProtected: '当月明细、累计用量和金融流水受保护。仅归并已结束的自然月。',
    matched: '匹配记录', retained: '保留凭证', estimated: '估算对应占用', confirmation: '确认文本', totp: '二次验证码', compact: '归并并清理', execute: '执行清理',
    diskNotice: '删除后的空间通常由数据库内部复用，不等同于立即减少磁盘占用。',
    automatic: '定时清理', schedule: '执行计划', enabled: '启用', retention: '保留天数', closedMonthPolicy: '归并已结束的月份；永久保留数值汇总',
    time: '创建时间', operator: '操作者', progress: '处理进度', deleted: '已删除', error: '错误',
    status: { pending: '等待中', running: '执行中', succeeded: '完成', failed: '失败', canceled: '已取消' },
    cancelTitle: '取消任务', cancelNotice: '停止后续批次。已提交的归并和删除不会撤销。',
    loadFailed: '数据清理信息加载失败', previewFailed: '预览失败', executeFailed: '任务提交失败', saveFailed: '策略保存失败', cancelFailed: '取消任务失败', submitted: '任务已提交'
  }
}
