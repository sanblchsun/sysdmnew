window.addEventListener("DOMContentLoaded", () => {
  const leftPanel = document.getElementById("left-panel");
  const topPanel = document.getElementById("top-right");
  const bottomPanel = document.getElementById("bottom-right");
  const verticalSplitter = document.getElementById("vertical-splitter");
  const horizontalSplitter = document.getElementById("horizontal-splitter");

  // === Restore positions from localStorage ===
  const leftWidth = localStorage.getItem("leftPanelWidth");
  const topHeightPercent = localStorage.getItem("topPanelHeightPercent");

  if (leftWidth) leftPanel.style.width = leftWidth;
  if (topHeightPercent) {
    topPanel.style.height = topHeightPercent;
    bottomPanel.style.height = `${100 - parseFloat(topHeightPercent)}%`;
  }

  // === Vertical splitter drag ===
  let draggingV = false;
  const minLeft = 50;
  const maxLeft = window.innerWidth - 50;

  verticalSplitter.addEventListener("mousedown", () => {
    draggingV = true;
    document.body.style.userSelect = "none";
  });
  document.addEventListener("mousemove", (e) => {
    if (!draggingV) return;
    let newWidth = Math.max(minLeft, Math.min(e.clientX, maxLeft));
    leftPanel.style.width = `${newWidth}px`;
    localStorage.setItem("leftPanelWidth", `${newWidth}px`);
  });
  document.addEventListener("mouseup", () => {
    draggingV = false;
    document.body.style.userSelect = "auto";
  });

  // === Horizontal splitter drag ===
  let draggingH = false;

  horizontalSplitter.addEventListener("mousedown", () => {
    draggingH = true;
    document.body.style.userSelect = "none";

    // при начале drag полностью скрываем top panel
    topPanel.style.height = "0%";
    bottomPanel.style.height = "100%";
    localStorage.setItem("topPanelHeightPercent", topPanel.style.height);
  });

  document.addEventListener("mousemove", (e) => {
    if (!draggingH) return;

    const rect = topPanel.parentElement.getBoundingClientRect();
    let offsetY = e.clientY - rect.top;

    // если тянем в верхнюю часть контейнера — top panel скрыта
    if (offsetY < 10) {
      topPanel.style.height = "0%";
      bottomPanel.style.height = "100%";
    } else {
      const topPercent = (offsetY / rect.height) * 100;
      topPanel.style.height = `${topPercent}%`;
      bottomPanel.style.height = `${100 - topPercent}%`;
    }

    localStorage.setItem("topPanelHeightPercent", topPanel.style.height);
  });

  document.addEventListener("mouseup", () => {
    draggingH = false;
    document.body.style.userSelect = "auto";
  });

  // === Double click horizontal splitter: вернуть top panel на 50% ===
  horizontalSplitter.addEventListener("dblclick", () => {
    if (parseFloat(topPanel.style.height) === 0) {
      topPanel.style.height = "50%";
      bottomPanel.style.height = "50%";
    } else {
      topPanel.style.height = "0%";
      bottomPanel.style.height = "100%";
    }
    localStorage.setItem("topPanelHeightPercent", topPanel.style.height);
  });

  // === Touch events для mobile ===
  verticalSplitter.addEventListener("touchstart", (e) => {
    draggingV = true;
    e.preventDefault();
  });
  horizontalSplitter.addEventListener("touchstart", (e) => {
    draggingH = true;
    e.preventDefault();
    topPanel.style.height = "0%";
    bottomPanel.style.height = "100%";
    localStorage.setItem("topPanelHeightPercent", topPanel.style.height);
  });

  document.addEventListener("touchmove", (e) => {
    const touch = e.touches[0];

    if (draggingV) {
      let newWidth = Math.max(
        50,
        Math.min(touch.clientX, window.innerWidth - 50)
      );
      leftPanel.style.width = `${newWidth}px`;
      localStorage.setItem("leftPanelWidth", `${newWidth}px`);
    }

    if (draggingH) {
      const rect = topPanel.parentElement.getBoundingClientRect();
      let offsetY = Math.max(
        0,
        Math.min(touch.clientY - rect.top, rect.height)
      );

      if (offsetY < 10) {
        topPanel.style.height = "0%";
        bottomPanel.style.height = "100%";
      } else {
        const topPercent = (offsetY / rect.height) * 100;
        topPanel.style.height = `${topPercent}%`;
        bottomPanel.style.height = `${100 - topPercent}%`;
      }

      localStorage.setItem("topPanelHeightPercent", topPanel.style.height);
    }
  });

  document.addEventListener("touchend", () => {
    draggingV = false;
    draggingH = false;
  });
});

// для дерева в левом окне

const STORAGE_KEY = "tree-state";

/* ---------- state ---------- */

function loadState() {
  const raw = localStorage.getItem(STORAGE_KEY);
  return raw ? JSON.parse(raw) : {};
}

function saveState(state) {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(state));
}

/* ---------- toggle single node ---------- */

function toggleNode(id, arrowEl) {
  const ul = document.querySelector(`ul[data-id="${id}"]`);
  if (!ul) return;

  const expanded = ul.classList.toggle("expanded");
  arrowEl.classList.toggle("expanded", expanded);

  const state = loadState();
  state[id] = expanded;
  saveState(state);
}

/* ---------- restore on load ---------- */

window.addEventListener("DOMContentLoaded", () => {
  const state = loadState();

  document.querySelectorAll(".collapsible").forEach((ul) => {
    const id = ul.dataset.id;
    if (state[id]) {
      ul.classList.add("expanded");
      const arrow = ul.previousElementSibling.querySelector(".arrow");
      if (arrow) arrow.classList.add("expanded");
    }
  });
});

/* ---------- toggle all ---------- */

let allExpanded = false;

function toggleAll() {
  const state = loadState();

  document.querySelectorAll(".collapsible").forEach((ul) => {
    const arrow = ul.previousElementSibling.querySelector(".arrow");

    if (allExpanded) {
      ul.classList.remove("expanded");
      arrow?.classList.remove("expanded");
      state[ul.dataset.id] = false;
    } else {
      ul.classList.add("expanded");
      arrow?.classList.add("expanded");
      state[ul.dataset.id] = true;
    }
  });

  saveState(state);
  allExpanded = !allExpanded;
  document.getElementById("toggle-all-btn").textContent = allExpanded
    ? "Свернуть всё"
    : "Развернуть всё";
}
