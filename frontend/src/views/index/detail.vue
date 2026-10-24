<template>
    <div>
        sss
    </div>
</template>
<script setup lang="ts">
import { getCurrentInstance, ref, onMounted, watch } from "vue";

const {
    proxy,
    proxy: { $utils },
} = getCurrentInstance() as any;

const cropperRef = ref<HTMLElement>();

</script>
<style scoped>
</style>
