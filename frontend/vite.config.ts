// 3.3vite.config.ts具体配置
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
// import image from 'rollup-plugin-image';
import AutoImport from 'unplugin-auto-import/vite'
import Components from 'unplugin-vue-components/vite';
import { ArcoResolver } from 'unplugin-vue-components/resolvers';
import wails from "@wailsio/runtime/plugins/vite";

import path from 'path'

const buildOptions = {
  assetsInclude: ['**/*.jpg', '**/*.png', '**/*.svg'] as string[]
};

export default defineConfig({
  base: './',
  // ...
  plugins: [
    // Arco 相关插件只留下面两个「按模板注入组件」的用法，不再单独挂 @arco-plugins/vite-vue：
    //  - 组件与图标在 main.ts 里 `app.use(ArcoVue)` / `app.use(ArcoVueIcon)` 全量注册；
    //  - 样式在 src/plugins/arco 里引主题包 `@arco-themes/vue-dev-frank-brank/index.less`，
    //    它 import 的是完整的 `dist/arco.less`，所有 .arco-* 规则已经在里面。
    // 之前那个插件对每个模块做 transform（实测 2205 次调用、6.6s，占构建 61%），
    // 去掉后 CSS 产物只差 2.8 KB（少一份重复的按组件样式），构建从 10s 降到 4s。别顺手加回来。
    AutoImport({
      resolvers: [ArcoResolver()],
    }),
    Components({
      resolvers: [ArcoResolver()]
    }),
    vue(),
    wails("./bindings")
  ],
  
  // css: {
  //   preprocessorOptions: {
  //     less: {
  //       modifyVars: {
  //         'arcoblue-1': '#E8FFEA',
  //         'arcoblue-2': '#AFF0B5',
  //         'arcoblue-3': '#7BE188',
  //         'arcoblue-4': '#4CD263',
  //         'arcoblue-5': '#23C343',
  //         'arcoblue-6': '#00B42A',
  //         'arcoblue-7': '#009A29',
  //         'arcoblue-8': '#008026',
  //         'arcoblue-9': '#006622',
  //         'arcoblue-10': '#004D1C',
  //       },
  //       javascriptEnabled: true
  //     }
  //   }
  // },
  resolve: {
    alias: {
      // 用 import.meta.dirname 而不是 __dirname：本文件是 ESM（package.json "type": "module"），
      // __dirname 只是 Vite 旧 configLoader 打包配置时注入的 shim，原生 loader（未来的默认）里没有，
      // 会打「unsupported by configLoader: 'native'」的告警。需 Node ≥ 20.11。
      '@': path.resolve(import.meta.dirname, './src'),
      bindings: path.resolve(import.meta.dirname, 'bindings')
    }
  },
  server: {
    // 不写 `https: false`：Vite 8 的 ServerOptions 没这个字段（不配就是不启用 https），
    // 硬写会让 vite.config.ts 自己的类型检查报「没有与此调用匹配的重载」。
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    host: '127.0.0.1',
    strictPort: true,
  },
  build: {
    outDir: 'dist',
    // 打包产物体积看一眼原始 kB 就够；gzip 全量资产要多花一秒多，交给 CI/分析时临时开
    reportCompressedSize: false,
    // rollupOptions: {
    //   plugins: [
    //     image({
    //       extensions: /\.(png|jpg|jpeg)$/
    //     })
    //   ]
    // }
  },
})
