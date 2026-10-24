/** 统一处理 localStorage */

import CacheKey from "@/constants/cache-key"
import { getStorage, setStorage } from "@/utils"

export const getCachePageLimit = () => {
    return getStorage(CacheKey.PAGE_LIMIT)
}
export const setCachePageLimit = (data: number) => {
    setStorage(CacheKey.PAGE_LIMIT, data)
}

export default {
    getCachePageLimit,
    setCachePageLimit
}