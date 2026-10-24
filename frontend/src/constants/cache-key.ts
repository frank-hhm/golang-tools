let PREFIX = "sys"

if (process.env.NODE_ENV === 'development') {
    PREFIX = 'dev-' + PREFIX
}
/** 缓存数据时用到的 Key */
class CacheKey {
    
}

export default CacheKey
