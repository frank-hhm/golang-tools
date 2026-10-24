let PREFIX = "sys"

if (process.env.NODE_ENV === 'development') {
    PREFIX = 'dev-' + PREFIX
}
/** 缓存数据时用到的 Key */
class CacheKey {
    static PAGE_LIMIT = `${PREFIX}-page-limit-key`
}

export default CacheKey
