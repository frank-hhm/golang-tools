<template>
    <router-view />
</template>

<script lang="ts" setup>
import { getCurrentInstance, onMounted, watch, onBeforeUnmount } from "vue";
</script>



<style>
#nprogress .bar {
    background: rgba(var(--primary-6)) !important;
}

#nprogress .peg {
    box-shadow: 0 0 10px rgba(var(--primary-6)), 0 0 5px rgba(var(--primary-6)) !important;
}
</style>