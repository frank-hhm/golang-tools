import { reactive, ref } from "vue"
import store from "@/store"
import { Result} from "@/types"
import { defineStore } from "pinia"
export const useAppStore = defineStore("app", () => {

    return {  }
})

/** 在 setup 外使用 */
export function useAppStoreHook() {
    return useAppStore(store)
}

export default {
    useAppStore,
    useAppStoreHook
}