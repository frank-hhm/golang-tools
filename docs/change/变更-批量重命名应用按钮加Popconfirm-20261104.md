# 变更-批量重命名应用按钮加Popconfirm-20261104

> 状态：**已执行到构建层**（vue-tsc + vite build exit=0，纯前端单文件改动）。气泡位置与文案换行观感需人工 GUI 确认。

## 一、范围

磁盘工具 [BatchRenameModal.vue](../frontend/src/views/tools/disk/components/BatchRenameModal.vue)：「应用」批量改名前增加 a-popconfirm 二次确认。改名规则、预览计算、逐个执行流程一行未动。

## 二、改动点

- **内置 OK 钮换自定义 `#footer`**：a-modal 的内置 OK 按钮无法包一层气泡，故去掉 `:ok-loading` / `:ok-button-props` / `ok-text` / `cancel-text` / `:on-before-ok`，页脚改为「取消」+ `a-popconfirm` 包裹的 primary「应用」钮。
- **气泡口径与全项目一致**：`type="warning" position="lt" :disabled="!canApply" @ok="apply"`（同 popconfirm.scss 收口的 34 处用法）；文案动态带项数：`确定重命名这 N 项？将逐项执行，个别失败不会回滚已改名的项。`
- **关闭语义补齐**：自定义页脚后没有 `on-before-ok` 返回值控制关闭的逻辑了，`apply()` 末尾显式 `if (!fails.length) close()`——全部成功才收弹窗，有失败留着让用户修正标红项后重试（与原语义一致）。
- `:disabled="!canApply"` 同时挂在气泡与按钮上：无预览项 / 有标红项 / 执行中时气泡不弹出。

## 三、验证到哪一层

- `vue-tsc --noEmit`（Node v22.23.2）exit=0；`vite build` exit=0。
- **未做**：GUI 真机确认气泡弹出位置（`position="lt"`，页脚右下角按钮左上方）与长文案在 320px 限宽下的换行观感。
