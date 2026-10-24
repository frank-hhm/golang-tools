let PREFIX = "sys"

if (process.env.NODE_ENV === 'development') {
    PREFIX = 'dev-' + PREFIX
}
/** 缓存数据时用到的 Key */
class CacheKey {
    static PAGE_LIMIT = `${PREFIX}-page-limit-key`
    /** 抓包页筛选状态（窗口关闭重开后恢复） */
    static SNIFFER_FILTER = `${PREFIX}-sniffer-filter-key`
}

export default CacheKey
