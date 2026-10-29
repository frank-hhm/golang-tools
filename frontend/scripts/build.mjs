// 打包入口：把「类型检查」和「产物构建」并行跑。
//
// 为什么并行：vue-tsc（~14s）与 vite build（~5s）互不依赖 —— 前者只读源码，后者自己
// 也会编译一次。串行写成 `vue-tsc && vite build` 等于白等前面那一整段。
//
// 退出码：任一方失败就整体非 0，并且两边的原始输出都直接透传到控制台
// （build/Taskfile.yml 的 build:frontend 依赖这个退出码来中断 wails3 package）。
import { spawn } from 'node:child_process';
import path from 'node:path';
import process from 'node:process';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const dev = process.argv.includes('--dev');

/** 用当前 node 进程直接跑本地 bin 的入口文件：不走 shell，避开 Windows 的 .cmd shim 与 PATH 差异 */
function run(name, entry, args) {
  const t0 = Date.now();
  return new Promise((resolve) => {
    const child = spawn(process.execPath, [path.join(root, entry), ...args], {
      cwd: root,
      stdio: 'inherit',
      windowsHide: true,
    });
    child.on('error', (e) => {
      console.error(`[${name}] 启动失败: ${e.message}`);
      resolve({ name, code: 1, ms: Date.now() - t0 });
    });
    child.on('close', (code) => resolve({ name, code: code ?? 1, ms: Date.now() - t0 }));
  });
}

const jobs = [
  run('vue-tsc', 'node_modules/vue-tsc/bin/vue-tsc.js', ['--noEmit']),
  run('vite build', 'node_modules/vite/bin/vite.js',
    dev ? ['build', '--minify', 'false', '--mode', 'development'] : ['build', '--mode', 'production']),
];

const results = await Promise.all(jobs);
for (const r of results) {
  console.log(`[${r.name}] ${r.code === 0 ? '通过' : '失败（退出码 ' + r.code + '）'}，用时 ${(r.ms / 1000).toFixed(1)}s`);
}
process.exit(results.every((r) => r.code === 0) ? 0 : 1);
