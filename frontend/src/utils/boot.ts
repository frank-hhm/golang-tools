/**
 * 撤掉 index.html 里内联的启动骨架层。
 *
 * 骨架盖在 #app 之外（Vue 挂载会整体替换 #app 的内容，写在里面会被连根拔掉），
 * 所以必须显式撤。等两帧再淡出：挂载完成的那一帧页面还没真正画出来，
 * 立刻撤掉就只是把「裸底色」换成了「空白页面」，没有意义。
 *
 * 就算这里没被执行到（JS 报错、挂载失败），骨架自己也带了一条 3.2s 的 CSS
 * 自动退场兜底，不会永久盖着页面。
 */
export function dismissBootSplash() {
    const el = document.getElementById("boot");
    if (!el) return;
    requestAnimationFrame(() => requestAnimationFrame(() => {
        el.classList.add("off");
        // 页面被后台节流时 transitionend 可能不来，兜一次定时清理
        window.setTimeout(() => el.remove(), 400);
    }));
}
