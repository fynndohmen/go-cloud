let currentPath = "/";
let currentContextEntry = null;

// Explorer state (sidebar)
const explorerState = {
    container: null,
    rootItem: null,
    loaded: new Set(),
    loading: new Set(),
};

// Folder picker state (modal)
const pickerState = {
    overlay: null,
    backdrop: null,
    closeBtn: null,
    cancelBtn: null,
    confirmBtn: null,
    titleEl: null,
    selectedEl: null,
    tree: null,

    resolve: null,
    selectedPath: "/",
    rootItem: null,
    loaded: new Set(),
    loading: new Set(),
};

function isImageFile(name) {
    if (!name) return false;
    const lower = name.toLowerCase();
    return (
        lower.endsWith(".png") ||
        lower.endsWith(".jpg") ||
        lower.endsWith(".jpeg") ||
        lower.endsWith(".gif") ||
        lower.endsWith(".webp") ||
        lower.endsWith(".bmp")
    );
}

function isTrashView(path) {
    return path === "/trash" || path.startsWith("/trash/");
}

function normalizePath(p) {
    if (!p || p === "") return "/";
    let x = p.trim();
    if (!x.startsWith("/")) x = "/" + x;
    x = x.replace(/\/+$/, "");
    if (x === "") x = "/";
    return x;
}

function shouldHideFromExplorer(dirName) {
    // trash stays hidden from normal folder listings (destinations should not be /trash)
    return dirName === "trash";
}

function getPathFromLocation() {
    const params = new URLSearchParams(window.location.search);
    const p = params.get("path");
    if (!p || p === "") return "/";
    return p.startsWith("/") ? p : "/" + p;
}

function updateUrlForPath(path) {
    const url = window.location.pathname + "?path=" + encodeURIComponent(path);
    history.pushState({ path }, "", url);
}

function joinPath(base, name) {
    base = normalizePath(base);
    if (base === "/" || base === "") return "/" + name;
    return base.replace(/\/+$/, "") + "/" + name;
}

// ===== Breadcrumbs =====

function renderBreadcrumbs(path) {
    const el = document.getElementById("breadcrumbs");
    if (!el) return;

    const p = normalizePath(path);
    el.innerHTML = "";

    const parts = p.split("/").filter(Boolean);
    let accum = "";

    const rootBtn = document.createElement("button");
    rootBtn.type = "button";
    rootBtn.className = "breadcrumb";
    rootBtn.textContent = "/";
    rootBtn.addEventListener("click", async () => {
        const target = "/";
        updateUrlForPath(target);
        await loadFiles(target);
    });
    el.appendChild(rootBtn);

    for (const part of parts) {
        accum = normalizePath(accum + "/" + part);
        const target = accum;

        const sep = document.createElement("span");
        sep.className = "breadcrumb-sep";
        sep.textContent = "›";
        el.appendChild(sep);

        const btn = document.createElement("button");
        btn.type = "button";
        btn.className = "breadcrumb";
        btn.textContent = part;
        btn.addEventListener("click", async () => {
            updateUrlForPath(target);
            await loadFiles(target);
        });

        el.appendChild(btn);
    }
}

// ===== API helpers =====

async function fetchDirs(path) {
    const res = await fetch("/api/files?path=" + encodeURIComponent(path));

    if (res.status === 401) {
        window.location.href = "/login";
        return [];
    }
    if (!res.ok) {
        console.error("Failed to load dirs:", res.status);
        return [];
    }

    const entries = await res.json();
    return (entries || []).filter(e => e && e.type === "dir" && !shouldHideFromExplorer(e.name));
}

// ===== Explorer (Sidebar) =====

function findTreeItemByPath(path) {
    if (!explorerState.container) return null;
    return explorerState.container.querySelector(`.tree-item[data-path="${CSS.escape(path)}"]`);
}

function setActiveTreeRow(path) {
    if (!explorerState.container) return;

    explorerState.container.querySelectorAll(".tree-row.active").forEach(el => {
        el.classList.remove("active");
    });

    const item = findTreeItemByPath(path);
    if (!item) return;

    const row = item.querySelector(":scope > .tree-row");
    if (row) row.classList.add("active");
}

async function expandTreeItem(item, forceReload = false) {
    if (!item) return;

    const path = item.getAttribute("data-path") || "/";
    const children = item.querySelector(":scope > .tree-children");
    const caretBtn = item.querySelector(":scope > .tree-row .tree-caret");

    if (!children) return;

    if (forceReload) {
        explorerState.loaded.delete(path);
        children.innerHTML = "";
    }

    item.classList.add("expanded");
    children.classList.remove("hidden");

    if (explorerState.loading.has(path)) return;
    if (explorerState.loaded.has(path) && !forceReload) return;

    explorerState.loading.add(path);

    const loading = document.createElement("div");
    loading.className = "tree-loading";
    loading.textContent = "Loading...";
    children.appendChild(loading);

    try {
        const dirs = await fetchDirs(path);
        children.innerHTML = "";

        if (!dirs || dirs.length === 0) {
            if (caretBtn) caretBtn.classList.add("disabled");
        } else {
            if (caretBtn) caretBtn.classList.remove("disabled");
            for (const d of dirs) {
                const childPath = normalizePath(d.path || joinPath(path, d.name));
                const child = createTreeItem(childPath, d.name);
                children.appendChild(child);
            }
        }

        explorerState.loaded.add(path);
    } catch (err) {
        console.error("Explorer expand error:", err);
        children.innerHTML = "";
    } finally {
        explorerState.loading.delete(path);
    }
}

function collapseTreeItem(item) {
    if (!item) return;
    const children = item.querySelector(":scope > .tree-children");
    if (children) children.classList.add("hidden");
    item.classList.remove("expanded");
}

async function toggleTreeItem(item) {
    if (!item) return;
    if (item.classList.contains("expanded")) {
        collapseTreeItem(item);
    } else {
        await expandTreeItem(item);
    }
}

/**
 * Preserve Explorer expand state
 */
function snapshotExplorerState() {
    const snap = { expanded: [], active: null, scrollTop: 0 };

    const sidebar = document.querySelector(".sidebar");
    if (sidebar) snap.scrollTop = sidebar.scrollTop;

    if (!explorerState.container) return snap;

    explorerState.container.querySelectorAll(".tree-item.expanded").forEach(item => {
        const p = item.getAttribute("data-path");
        if (p) snap.expanded.push(p);
    });

    const activeRow = explorerState.container.querySelector(".tree-row.active");
    if (activeRow) {
        const item = activeRow.closest(".tree-item");
        if (item) snap.active = item.getAttribute("data-path");
    }

    return snap;
}

async function ensureExpandedPath(path) {
    const p = normalizePath(path);
    if (isTrashView(p)) return;
    if (!explorerState.rootItem) return;

    await expandTreeItem(explorerState.rootItem);
    if (p === "/") return;

    const parts = p.split("/").filter(Boolean);
    let accum = "";
    let current = explorerState.rootItem;

    for (const seg of parts) {
        accum = normalizePath(accum + "/" + seg);

        await expandTreeItem(current);

        const child = findTreeItemByPath(accum);
        if (!child) return;

        current = child;
    }

    await expandTreeItem(current);
}

async function restoreExplorerState(snap) {
    if (!snap || !explorerState.container) return;

    const sorted = (snap.expanded || []).slice().sort((a, b) => a.split("/").length - b.split("/").length);
    for (const p of sorted) {
        await ensureExpandedPath(p);
    }

    if (snap.active) setActiveTreeRow(snap.active);

    const sidebar = document.querySelector(".sidebar");
    if (sidebar) sidebar.scrollTop = snap.scrollTop || 0;
}

async function rebuildExplorerPreserveState() {
    if (!explorerState.container) return;
    const snap = snapshotExplorerState();
    await initExplorer();
    await restoreExplorerState(snap);
}

function createTreeItem(path, name) {
    const item = document.createElement("div");
    item.className = "tree-item";
    item.setAttribute("data-path", path);
    item.setAttribute("data-name", name);

    const row = document.createElement("div");
    row.className = "tree-row";

    const caret = document.createElement("button");
    caret.type = "button";
    caret.className = "tree-caret";
    caret.innerHTML = `<span class="tree-caret-icon">▾</span>`;
    caret.addEventListener("click", async (e) => {
        e.preventDefault();
        e.stopPropagation();

        // allow collapsing even if disabled
        if (item.classList.contains("expanded")) {
            collapseTreeItem(item);
            return;
        }
        if (caret.classList.contains("disabled")) return;

        await toggleTreeItem(item);
    });

    const label = document.createElement("button");
    label.type = "button";
    label.className = "tree-label";
    label.innerHTML = `<span>${name}</span>`;
    label.addEventListener("click", async (e) => {
        e.preventDefault();
        e.stopPropagation();
        const p = item.getAttribute("data-path") || "/";
        updateUrlForPath(p);
        await loadFiles(p);
        closeContextMenu();
        closePreview();
        setActiveTreeRow(p);
    });

    row.appendChild(caret);
    row.appendChild(label);

    const children = document.createElement("div");
    children.className = "tree-children hidden";

    item.appendChild(row);
    item.appendChild(children);

    return item;
}

async function initExplorer() {
    explorerState.container = document.getElementById("folder-tree");
    if (!explorerState.container) return;

    explorerState.loaded.clear();
    explorerState.loading.clear();

    explorerState.container.innerHTML = "";

    const root = document.createElement("div");
    root.className = "tree-item";
    root.setAttribute("data-path", "/");
    root.setAttribute("data-name", "/");

    const row = document.createElement("div");
    row.className = "tree-row";

    const caret = document.createElement("button");
    caret.type = "button";
    caret.className = "tree-caret";
    caret.innerHTML = `<span class="tree-caret-icon">▾</span>`;
    caret.addEventListener("click", async (e) => {
        e.preventDefault();
        e.stopPropagation();
        await toggleTreeItem(root);
    });

    const label = document.createElement("button");
    label.type = "button";
    label.className = "tree-label";
    label.innerHTML = `<span>/</span>`;
    label.addEventListener("click", async (e) => {
        e.preventDefault();
        e.stopPropagation();
        updateUrlForPath("/");
        await loadFiles("/");
        setActiveTreeRow("/");
    });

    row.appendChild(caret);
    row.appendChild(label);

    const children = document.createElement("div");
    children.className = "tree-children hidden"; // root closed by default

    root.appendChild(row);
    root.appendChild(children);

    explorerState.container.appendChild(root);
    explorerState.rootItem = root;

    if (currentPath === "/") setActiveTreeRow("/");
}

// ===== Folder Picker (Move + Restore) =====

function pickerInitDom() {
    pickerState.overlay = document.getElementById("folder-picker-overlay");
    pickerState.backdrop = document.getElementById("picker-backdrop");
    pickerState.closeBtn = document.getElementById("picker-close");
    pickerState.cancelBtn = document.getElementById("picker-cancel");
    pickerState.confirmBtn = document.getElementById("picker-confirm");
    pickerState.titleEl = document.getElementById("picker-title");
    pickerState.selectedEl = document.getElementById("picker-selected");
    pickerState.tree = document.getElementById("picker-tree");

    const close = () => pickerClose(null);

    if (pickerState.backdrop) pickerState.backdrop.addEventListener("click", close);
    if (pickerState.closeBtn) pickerState.closeBtn.addEventListener("click", close);
    if (pickerState.cancelBtn) pickerState.cancelBtn.addEventListener("click", close);

    if (pickerState.confirmBtn) {
        pickerState.confirmBtn.addEventListener("click", () => {
            pickerClose(pickerState.selectedPath || "/");
        });
    }
}

function pickerIsOpen() {
    return pickerState.overlay && !pickerState.overlay.classList.contains("hidden");
}

function pickerClose(result) {
    if (!pickerState.overlay) return;

    pickerState.overlay.classList.add("hidden");

    const resolve = pickerState.resolve;
    pickerState.resolve = null;

    if (typeof resolve === "function") resolve(result);
}

function pickerFindItemByPath(path) {
    if (!pickerState.tree) return null;
    return pickerState.tree.querySelector(`.tree-item[data-path="${CSS.escape(path)}"]`);
}

function pickerSetActive(path) {
    if (!pickerState.tree) return;

    pickerState.tree.querySelectorAll(".tree-row.active").forEach(el => el.classList.remove("active"));

    const item = pickerFindItemByPath(path);
    if (!item) return;

    const row = item.querySelector(":scope > .tree-row");
    if (row) row.classList.add("active");
}

async function pickerExpand(item) {
    if (!item) return;

    const path = item.getAttribute("data-path") || "/";
    const children = item.querySelector(":scope > .tree-children");
    const caretBtn = item.querySelector(":scope > .tree-row .tree-caret");
    if (!children) return;

    item.classList.add("expanded");
    children.classList.remove("hidden");

    if (pickerState.loading.has(path)) return;
    if (pickerState.loaded.has(path)) return;

    pickerState.loading.add(path);

    const loading = document.createElement("div");
    loading.className = "tree-loading";
    loading.textContent = "Loading...";
    children.appendChild(loading);

    try {
        const dirs = await fetchDirs(path);
        children.innerHTML = "";

        if (!dirs || dirs.length === 0) {
            if (caretBtn) caretBtn.classList.add("disabled");
        } else {
            if (caretBtn) caretBtn.classList.remove("disabled");
            for (const d of dirs) {
                const childPath = normalizePath(d.path || joinPath(path, d.name));
                const child = pickerCreateItem(childPath, d.name);
                children.appendChild(child);
            }
        }

        pickerState.loaded.add(path);
    } finally {
        pickerState.loading.delete(path);
    }
}

function pickerCollapse(item) {
    if (!item) return;
    const children = item.querySelector(":scope > .tree-children");
    if (children) children.classList.add("hidden");
    item.classList.remove("expanded");
}

async function pickerToggle(item) {
    if (!item) return;
    if (item.classList.contains("expanded")) pickerCollapse(item);
    else await pickerExpand(item);
}

function pickerCreateItem(path, name) {
    const item = document.createElement("div");
    item.className = "tree-item";
    item.setAttribute("data-path", path);
    item.setAttribute("data-name", name);

    const row = document.createElement("div");
    row.className = "tree-row";

    const caret = document.createElement("button");
    caret.type = "button";
    caret.className = "tree-caret";
    caret.innerHTML = `<span class="tree-caret-icon">▾</span>`;
    caret.addEventListener("click", async (e) => {
        e.preventDefault();
        e.stopPropagation();

        // allow collapsing even if disabled
        if (item.classList.contains("expanded")) {
            pickerCollapse(item);
            return;
        }
        if (caret.classList.contains("disabled")) return;

        await pickerToggle(item);
    });

    const label = document.createElement("button");
    label.type = "button";
    label.className = "tree-label";
    label.innerHTML = `<span>${name}</span>`;
    label.addEventListener("click", (e) => {
        e.preventDefault();
        e.stopPropagation();
        pickerState.selectedPath = path;
        if (pickerState.selectedEl) pickerState.selectedEl.textContent = path;
        pickerSetActive(path);
    });

    row.appendChild(caret);
    row.appendChild(label);

    const children = document.createElement("div");
    children.className = "tree-children hidden";

    item.appendChild(row);
    item.appendChild(children);
    return item;
}

async function pickerInitTree(initialPath) {
    if (!pickerState.tree) return;

    pickerState.loaded.clear();
    pickerState.loading.clear();
    pickerState.tree.innerHTML = "";

    const root = pickerCreateItem("/", "/");
    pickerState.tree.appendChild(root);
    pickerState.rootItem = root;

    // Select initial path (clamp trash destinations away)
    let init = normalizePath(initialPath || "/");
    if (isTrashView(init)) init = "/";

    pickerState.selectedPath = init;
    if (pickerState.selectedEl) pickerState.selectedEl.textContent = init;

    await pickerEnsureExpandedPath(init);
    pickerSetActive(init);
}

async function pickerEnsureExpandedPath(path) {
    const p = normalizePath(path);
    if (!pickerState.rootItem) return;

    await pickerExpand(pickerState.rootItem);
    if (p === "/") return;

    const parts = p.split("/").filter(Boolean);
    let accum = "";
    let current = pickerState.rootItem;

    for (const seg of parts) {
        accum = normalizePath(accum + "/" + seg);
        await pickerExpand(current);

        const child = pickerFindItemByPath(accum);
        if (!child) return;
        current = child;
    }

    await pickerExpand(current);
}

async function openFolderPicker({ title, confirmText, initialPath }) {
    if (!pickerState.overlay) pickerInitDom();
    if (!pickerState.overlay || !pickerState.tree) {
        const fallback = prompt(title || "Select folder:", initialPath || "/");
        return fallback ? normalizePath(fallback.trim()) : null;
    }

    if (pickerState.titleEl) pickerState.titleEl.textContent = title || "Select folder";
    if (pickerState.confirmBtn) pickerState.confirmBtn.textContent = confirmText || "Select";

    pickerState.overlay.classList.remove("hidden");
    await pickerInitTree(initialPath || "/");

    return new Promise((resolve) => {
        pickerState.resolve = resolve;
    });
}

// ===== Files grid =====

async function loadFiles(path) {
    currentPath = normalizePath(path);

    renderBreadcrumbs(currentPath);

    // Trash sidebar active highlight
    const trashLink = document.getElementById("trash-link");
    if (trashLink) trashLink.classList.toggle("active", isTrashView(currentPath));

    const emptyTrashBtn = document.getElementById("btn-empty-trash");
    if (emptyTrashBtn) {
        emptyTrashBtn.classList.toggle("hidden", !isTrashView(currentPath));
    }

    try {
        const res = await fetch("/api/files?path=" + encodeURIComponent(currentPath));

        if (res.status === 401) {
            window.location.href = "/login";
            return;
        }

        if (!res.ok) {
            console.error("Error loading files:", res.status);
            renderError("Error loading files (" + res.status + ").");
            return;
        }

        const files = await res.json();
        renderFiles(files);

        const visible = findTreeItemByPath(currentPath);
        if (visible) setActiveTreeRow(currentPath);
        else if (currentPath === "/") setActiveTreeRow("/");
        else if (isTrashView(currentPath)) setActiveTreeRow(""); // clears active folder highlight
    } catch (err) {
        console.error("Network error:", err);
        renderError("Network error while loading files.");
    }
}

function renderFiles(files) {
    const container = document.getElementById("file-list");
    container.innerHTML = "";

    if (!files || files.length === 0) {
        const empty = document.createElement("div");
        empty.className = "empty-message";
        empty.textContent = "No files in this folder.";
        container.appendChild(empty);
        return;
    }

    // folders first (then alphabetical)
    const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: "base" });
    const sorted = [...files].sort((a, b) => {
        const ad = a.type === "dir";
        const bd = b.type === "dir";
        if (ad !== bd) return ad ? -1 : 1;
        return collator.compare(a.name || "", b.name || "");
    });

    sorted.forEach(file => {
        const card = document.createElement("div");
        card.className = "file-card";

        const thumb = document.createElement("div");
        thumb.className = "file-thumb";

        if (file.type === "dir") {
            const icon = document.createElement("span");
            icon.className = "file-icon";
            icon.textContent = "📁";
            thumb.appendChild(icon);
        } else if (isImageFile(file.name)) {
            const img = document.createElement("img");
            img.src = "/api/download?path=" + encodeURIComponent(file.path);
            img.alt = file.name;
            thumb.appendChild(img);
        } else {
            const icon = document.createElement("span");
            icon.className = "file-icon";
            icon.textContent = "📄";
            thumb.appendChild(icon);
        }

        const name = document.createElement("div");
        name.className = "file-name";
        name.textContent = file.name;

        const menuBtn = document.createElement("button");
        menuBtn.type = "button";
        menuBtn.className = "file-menu-button";
        menuBtn.textContent = "⋮";
        menuBtn.addEventListener("click", (event) => {
            event.stopPropagation();
            event.preventDefault();
            openContextMenu(file, event.clientX, event.clientY);
        });

        if (file.type === "dir") {
            card.addEventListener("dblclick", async () => {
                const nextPath = joinPath(currentPath, file.name);
                updateUrlForPath(nextPath);
                await loadFiles(nextPath);
            });
        } else if (isImageFile(file.name)) {
            card.addEventListener("dblclick", () => openPreview(file));
        } else {
            card.addEventListener("dblclick", () => {
                window.location.href = "/api/download?path=" + encodeURIComponent(file.path);
            });
        }

        card.addEventListener("contextmenu", (event) => {
            event.preventDefault();
            openContextMenu(file, event.clientX, event.clientY);
        });

        card.appendChild(thumb);
        card.appendChild(name);
        card.appendChild(menuBtn);
        container.appendChild(card);
    });
}

function renderError(message) {
    const container = document.getElementById("file-list");
    container.innerHTML = "";
    const err = document.createElement("div");
    err.className = "empty-message";
    err.textContent = message;
    container.appendChild(err);
}

// ===== Preview =====

function openPreview(file) {
    const overlay = document.getElementById("preview-overlay");
    const img = document.getElementById("preview-image");
    const caption = document.getElementById("preview-caption");
    if (!overlay || !img || !caption) return;

    img.src = "/api/download?path=" + encodeURIComponent(file.path);
    img.alt = file.name;
    caption.textContent = file.name;

    overlay.classList.remove("hidden");
}

function closePreview() {
    const overlay = document.getElementById("preview-overlay");
    const img = document.getElementById("preview-image");
    if (!overlay || !img) return;

    overlay.classList.add("hidden");
    img.src = "";
}

// ===== Context menu =====

function openContextMenu(file, clientX, clientY) {
    const menu = document.getElementById("file-context-menu");
    if (!menu) return;

    currentContextEntry = file;

    const inTrash = isTrashView(currentPath);

    const btnDownload = menu.querySelector('[data-action="download"]');
    const btnRename = menu.querySelector('[data-action="rename"]');
    const btnMove = menu.querySelector('[data-action="move"]');
    const btnTrash = menu.querySelector('[data-action="trash"]');

    const btnRestore = menu.querySelector('[data-action="restore"]');
    const btnDeleteForever = menu.querySelector('[data-action="delete-forever"]');

    if (btnDownload) btnDownload.classList.toggle("hidden", inTrash);
    if (btnRename) btnRename.classList.toggle("hidden", inTrash);
    if (btnMove) btnMove.classList.toggle("hidden", inTrash);
    if (btnTrash) btnTrash.classList.toggle("hidden", inTrash);

    if (btnRestore) btnRestore.classList.toggle("hidden", !inTrash);
    if (btnDeleteForever) btnDeleteForever.classList.toggle("hidden", !inTrash);

    const menuRect = { width: 180, height: 180 };
    let x = clientX;
    let y = clientY;

    const maxX = window.innerWidth - menuRect.width - 8;
    const maxY = window.innerHeight - menuRect.height - 8;

    if (x > maxX) x = maxX;
    if (y > maxY) y = maxY;

    menu.style.left = x + "px";
    menu.style.top = y + "px";
    menu.classList.remove("hidden");
}

function closeContextMenu() {
    const menu = document.getElementById("file-context-menu");
    if (!menu) return;
    menu.classList.add("hidden");
    currentContextEntry = null;
}

// ===== Actions =====

async function renameEntry(file) {
    const suggested = file.name || "";
    const newName = prompt("New name:", suggested);
    if (!newName) return;

    const trimmed = newName.trim();
    if (trimmed === "" || trimmed === file.name) return;

    try {
        const res = await fetch("/api/rename", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ old_path: file.path, new_name: trimmed }),
        });

        if (res.status === 401) { window.location.href = "/login"; return; }
        if (!res.ok) { alert("Could not rename entry (" + res.status + ")."); return; }

        await loadFiles(currentPath);
        if (explorerState.container) await rebuildExplorerPreserveState();
    } catch {
        alert("Network error while renaming.");
    }
}

function downloadEntry(file) {
    window.location.href = "/api/download?path=" + encodeURIComponent(file.path);
}

async function moveEntry(file) {
    const initial = isTrashView(currentPath) ? "/" : currentPath;

    const target = await openFolderPicker({
        title: "Move to…",
        confirmText: "Move",
        initialPath: initial,
    });

    if (!target) return;

    try {
        const res = await fetch("/api/move", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ source_path: file.path, target_dir: target }),
        });

        if (res.status === 401) { window.location.href = "/login"; return; }
        if (!res.ok) { alert("Could not move entry (" + res.status + ")."); return; }

        await loadFiles(currentPath);
        if (explorerState.container) await rebuildExplorerPreserveState();
    } catch {
        alert("Network error while moving entry.");
    }
}

async function trashEntry(file) {
    const msg =
        file.type === "dir"
            ? `Move folder "${file.name}" (including all contents) to Trash?`
            : `Move "${file.name}" to Trash?`;

    if (!confirm(msg)) return;

    try {
        const res = await fetch("/api/trash", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ path: file.path }),
        });

        if (res.status === 401) { window.location.href = "/login"; return; }
        if (!res.ok) { alert("Could not move entry to Trash (" + res.status + ")."); return; }

        await loadFiles(currentPath);
        if (explorerState.container) await rebuildExplorerPreserveState();
    } catch {
        alert("Network error while moving to Trash.");
    }
}

async function deleteForeverEntry(file) {
    const msg =
        file.type === "dir"
            ? `Delete folder "${file.name}" forever? This cannot be undone.`
            : `Delete "${file.name}" forever? This cannot be undone.`;

    if (!confirm(msg)) return;

    try {
        const res = await fetch("/api/trash/delete", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ path: file.path }),
        });

        if (res.status === 401) { window.location.href = "/login"; return; }
        if (!res.ok) { alert("Could not delete forever (" + res.status + ")."); return; }

        await loadFiles(currentPath);
    } catch {
        alert("Network error while deleting forever.");
    }
}

async function restoreEntry(file) {
    const target = await openFolderPicker({
        title: "Restore to…",
        confirmText: "Restore",
        initialPath: "/",
    });

    if (!target) return;

    try {
        const res = await fetch("/api/trash/restore", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ path: file.path, target_dir: target }),
        });

        if (res.status === 401) { window.location.href = "/login"; return; }
        if (!res.ok) { alert("Could not restore (" + res.status + ")."); return; }

        await loadFiles(currentPath);
        if (explorerState.container) await rebuildExplorerPreserveState();
    } catch {
        alert("Network error while restoring.");
    }
}

async function emptyTrash() {
    if (!confirm("Empty Trash? This cannot be undone.")) return;

    try {
        const res = await fetch("/api/trash/empty", { method: "POST" });

        if (res.status === 401) { window.location.href = "/login"; return; }
        if (!res.ok) { alert("Could not empty trash (" + res.status + ")."); return; }

        await loadFiles(currentPath);
    } catch {
        alert("Network error while emptying trash.");
    }
}

// ===== Init =====

document.addEventListener("DOMContentLoaded", async () => {
    pickerInitDom();

    const newBtn = document.getElementById("btn-new");
    const newMenu = document.getElementById("new-menu");
    const logoutBtn = document.getElementById("btn-logout");
    const emptyTrashBtn = document.getElementById("btn-empty-trash");
    const trashLink = document.getElementById("trash-link");

    const fileInput = document.getElementById("file-input");
    const folderInput = document.getElementById("folder-input");

    const menuNewFolder = document.getElementById("menu-new-folder");
    const menuUploadFile = document.getElementById("menu-upload-file");
    const menuUploadFolder = document.getElementById("menu-upload-folder");

    function resetNewMenuPosition() {
        if (!newMenu) return;
        newMenu.style.position = "";
        newMenu.style.left = "";
        newMenu.style.top = "";
        newMenu.style.right = "";
        newMenu.style.bottom = "";
        delete newMenu.dataset.mode;
    }

    function hideNewMenu() {
        if (!newMenu) return;
        newMenu.classList.add("hidden");
        resetNewMenuPosition();
    }

    function openNewMenuAt(x, y) {
        if (!newMenu) return;

        closeContextMenu();

        newMenu.classList.remove("hidden");
        newMenu.dataset.mode = "context";
        newMenu.style.position = "fixed";
        newMenu.style.right = "auto";

        // clamp with measured size if possible
        const rect = newMenu.getBoundingClientRect();
        const w = rect.width || 190;
        const h = rect.height || 170;

        let nx = x;
        let ny = y;

        const maxX = window.innerWidth - w - 8;
        const maxY = window.innerHeight - h - 8;

        if (nx > maxX) nx = maxX;
        if (ny > maxY) ny = maxY;

        newMenu.style.left = nx + "px";
        newMenu.style.top = ny + "px";
    }

    if (logoutBtn) logoutBtn.addEventListener("click", () => { window.location.href = "/logout"; });
    if (emptyTrashBtn) emptyTrashBtn.addEventListener("click", () => { emptyTrash(); });

    if (trashLink) {
        trashLink.addEventListener("click", async () => {
            updateUrlForPath("/trash");
            await loadFiles("/trash");
            closeContextMenu();
            closePreview();
        });
    }

    if (newBtn && newMenu) {
        newBtn.addEventListener("click", (e) => {
            e.stopPropagation();
            resetNewMenuPosition();
            newMenu.classList.toggle("hidden");
        });

        document.addEventListener("click", (e) => {
            if (newMenu.classList.contains("hidden")) return;
            if (!newMenu.contains(e.target) && e.target !== newBtn) hideNewMenu();
        });
    }

    // ✅ Right-click anywhere in main area (including empty space below grid)
    const mainArea = document.querySelector("main.main");
    if (mainArea && newMenu) {
        mainArea.addEventListener(
            "contextmenu",
            (e) => {
                // if on a card -> keep file context menu
                if (e.target.closest(".file-card")) return;

                // ignore when clicking menus/modal
                if (e.target.closest("#file-context-menu")) return;
                if (e.target.closest("#new-menu")) return;
                if (e.target.closest("#folder-picker-overlay")) return;

                // ignore sidebar
                if (e.target.closest(".sidebar")) return;

                e.preventDefault();
                openNewMenuAt(e.clientX, e.clientY);
            },
            { capture: true }
        );
    }

    if (menuNewFolder) {
        menuNewFolder.addEventListener("click", async () => {
            hideNewMenu();
            const name = prompt("Folder name:");
            if (!name) return;

            try {
                const res = await fetch("/api/mkdir", {
                    method: "POST",
                    headers: { "Content-Type": "application/json" },
                    body: JSON.stringify({ path: currentPath, name }),
                });

                if (res.status === 401) { window.location.href = "/login"; return; }
                if (!res.ok) { alert("Could not create folder (" + res.status + ")."); return; }

                await loadFiles(currentPath);
                if (explorerState.container) await rebuildExplorerPreserveState();
            } catch {
                alert("Network error while creating folder.");
            }
        });
    }

    if (menuUploadFile && fileInput) {
        menuUploadFile.addEventListener("click", () => {
            hideNewMenu();
            fileInput.value = "";
            fileInput.click();
        });
    }

    if (menuUploadFolder && folderInput) {
        menuUploadFolder.addEventListener("click", () => {
            hideNewMenu();
            folderInput.value = "";
            folderInput.click();
        });
    }

    if (fileInput) {
        fileInput.addEventListener("change", async () => {
            if (!fileInput.files || fileInput.files.length === 0) return;

            const formData = new FormData();
            formData.append("path", currentPath);
            for (const file of fileInput.files) formData.append("file", file);

            try {
                const res = await fetch("/api/upload", { method: "POST", body: formData });
                if (res.status === 401) { window.location.href = "/login"; return; }
                if (!res.ok) { alert("Upload failed (" + res.status + ")."); return; }

                await loadFiles(currentPath);
                if (explorerState.container) await rebuildExplorerPreserveState();
            } catch {
                alert("Network error during upload.");
            }
        });
    }

    if (folderInput) {
        folderInput.addEventListener("change", async () => {
            if (!folderInput.files || folderInput.files.length === 0) return;

            const formData = new FormData();
            formData.append("path", currentPath);
            formData.append("preserve_paths", "1");

            for (const file of folderInput.files) {
                const rel = (file.webkitRelativePath && file.webkitRelativePath !== "")
                    ? file.webkitRelativePath
                    : file.name;

                formData.append("relpath", rel);
                formData.append("file", file);
            }

            try {
                const res = await fetch("/api/upload", { method: "POST", body: formData });
                if (res.status === 401) { window.location.href = "/login"; return; }
                if (!res.ok) { alert("Folder upload failed (" + res.status + ")."); return; }

                await loadFiles(currentPath);
                if (explorerState.container) await rebuildExplorerPreserveState();
            } catch {
                alert("Network error during folder upload.");
            }
        });
    }

    const backdrop = document.getElementById("preview-backdrop");
    const closeBtn = document.getElementById("preview-close");

    if (backdrop) backdrop.addEventListener("click", () => closePreview());
    if (closeBtn) closeBtn.addEventListener("click", (e) => { e.stopPropagation(); closePreview(); });

    document.addEventListener("keydown", (e) => {
        if (e.key === "Escape") {
            closePreview();
            closeContextMenu();
            if (pickerIsOpen()) pickerClose(null);
            hideNewMenu();
        }
    });

    const contextMenu = document.getElementById("file-context-menu");
    if (contextMenu) {
        contextMenu.addEventListener("click", (e) => {
            const target = e.target;
            const action = target.getAttribute("data-action");
            if (!action || !currentContextEntry) { closeContextMenu(); return; }

            if (action === "rename") renameEntry(currentContextEntry);
            else if (action === "download") downloadEntry(currentContextEntry);
            else if (action === "move") moveEntry(currentContextEntry);
            else if (action === "trash") trashEntry(currentContextEntry);
            else if (action === "restore") restoreEntry(currentContextEntry);
            else if (action === "delete-forever") deleteForeverEntry(currentContextEntry);

            closeContextMenu();
        });

        contextMenu.addEventListener("mousedown", (e) => e.stopPropagation());
    }

    document.addEventListener("mousedown", (e) => {
        const menu = document.getElementById("file-context-menu");
        if (menu && !menu.classList.contains("hidden") && !menu.contains(e.target)) closeContextMenu();

        if (newMenu && !newMenu.classList.contains("hidden")) {
            const clickInsideNew = newMenu.contains(e.target);
            const clickNewBtn = (newBtn && (e.target === newBtn || newBtn.contains(e.target)));
            if (!clickInsideNew && !clickNewBtn) hideNewMenu();
        }
    });

    const initialPath = normalizePath(getPathFromLocation());
    currentPath = initialPath;

    history.replaceState(
        { path: initialPath },
        "",
        window.location.pathname + "?path=" + encodeURIComponent(initialPath)
    );

    await initExplorer();
    await loadFiles(initialPath);
});

window.addEventListener("popstate", async (event) => {
    let path = "/";
    if (event.state && event.state.path) path = event.state.path;
    else path = getPathFromLocation();
    await loadFiles(path);
});

window.addEventListener("pageshow", async () => {
    await loadFiles(currentPath);
});
