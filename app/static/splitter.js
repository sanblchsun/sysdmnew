// app/static/splitter.js
window.addEventListener("DOMContentLoaded", () => {
  const leftPanel = document.getElementById("left-panel");
  const topPanel = document.getElementById("top-right");
  const bottomPanel = document.getElementById("bottom-right");
  const verticalSplitter = document.getElementById("vertical-splitter");
  const horizontalSplitter = document.getElementById("horizontal-splitter");

  if (
    !leftPanel ||
    !topPanel ||
    !bottomPanel ||
    !verticalSplitter ||
    !horizontalSplitter
  ) {
    console.warn("Splitter init aborted: missing DOM elements");
    return;
  }

  /* =======================
     Restore saved positions
     ======================= */
  const savedLeftWidth = localStorage.getItem("leftPanelWidth");
  const savedTopPercent = localStorage.getItem("topPanelHeightPercent");

  if (savedLeftWidth) {
    leftPanel.style.width = savedLeftWidth;
  }

  if (savedTopPercent !== null) {
    const topPercent = parseFloat(savedTopPercent);
    topPanel.style.height = `${topPercent}%`;
    bottomPanel.style.height = `${100 - topPercent}%`;
  }

  /* =======================
     Vertical splitter
     ======================= */
  let draggingV = false;

  verticalSplitter.addEventListener("mousedown", () => {
    draggingV = true;
    document.body.style.userSelect = "none";
  });

  document.addEventListener("mousemove", (e) => {
    if (!draggingV) return;

    const min = 50;
    const max = window.innerWidth - 50;
    const width = Math.max(min, Math.min(e.clientX, max));

    leftPanel.style.width = `${width}px`;
    localStorage.setItem("leftPanelWidth", `${width}px`);
  });

  document.addEventListener("mouseup", () => {
    draggingV = false;
    document.body.style.userSelect = "auto";
  });

  /* =======================
     Horizontal splitter
     ======================= */
  let draggingH = false;
  let dragStartedH = false;
  let startY = 0;

  horizontalSplitter.addEventListener("mousedown", (e) => {
    draggingH = true;
    dragStartedH = false;
    startY = e.clientY;
    document.body.style.userSelect = "none";
  });

  document.addEventListener("mousemove", (e) => {
    if (!draggingH) return;

    if (Math.abs(e.clientY - startY) > 3) {
      dragStartedH = true;
    }

    const container = topPanel.parentElement;
    const rect = container.getBoundingClientRect();

    let offsetY = e.clientY - rect.top;
    offsetY = Math.max(0, Math.min(offsetY, rect.height));

    const percent = (offsetY / rect.height) * 100;

    topPanel.style.height = `${percent}%`;
    bottomPanel.style.height = `${100 - percent}%`;

    localStorage.setItem("topPanelHeightPercent", percent.toFixed(2));
  });

  document.addEventListener("mouseup", () => {
    draggingH = false;
    document.body.style.userSelect = "auto";
  });

  /* =======================
     Double click toggle
     ======================= */
  horizontalSplitter.addEventListener("dblclick", () => {
    const current = parseFloat(topPanel.style.height || "0");

    if (current === 0) {
      topPanel.style.height = "50%";
      bottomPanel.style.height = "50%";
      localStorage.setItem("topPanelHeightPercent", "50");
    } else {
      topPanel.style.height = "0%";
      bottomPanel.style.height = "100%";
      localStorage.setItem("topPanelHeightPercent", "0");
    }
  });

  /* =======================
     Touch support (mobile)
     ======================= */
  verticalSplitter.addEventListener("touchstart", (e) => {
    draggingV = true;
    e.preventDefault();
  });

  horizontalSplitter.addEventListener("touchstart", (e) => {
    draggingH = true;
    dragStartedH = false;
    startY = e.touches[0].clientY;
    e.preventDefault();
  });

  document.addEventListener("touchmove", (e) => {
    const touch = e.touches[0];

    if (draggingV) {
      const min = 50;
      const max = window.innerWidth - 50;
      const width = Math.max(min, Math.min(touch.clientX, max));

      leftPanel.style.width = `${width}px`;
      localStorage.setItem("leftPanelWidth", `${width}px`);
    }

    if (draggingH) {
      const container = topPanel.parentElement;
      const rect = container.getBoundingClientRect();

      let offsetY = touch.clientY - rect.top;
      offsetY = Math.max(0, Math.min(offsetY, rect.height));

      const percent = (offsetY / rect.height) * 100;

      topPanel.style.height = `${percent}%`;
      bottomPanel.style.height = `${100 - percent}%`;

      localStorage.setItem("topPanelHeightPercent", percent.toFixed(2));
    }
  });

  document.addEventListener("touchend", () => {
    draggingV = false;
    draggingH = false;
  });
});

// app/static/splitter.js

function resetSplitters() {
  console.log("Reset splitters");

  const leftPanel = document.getElementById("left-panel");
  const topRight = document.getElementById("top-right");
  const bottomRight = document.getElementById("bottom-right");

  if (leftPanel) {
    leftPanel.style.width = "250px";
    localStorage.setItem("leftPanelWidth", "250px");
  }

  if (topRight && bottomRight) {
    topRight.style.height = "50%";
    bottomRight.style.height = "50%";
    localStorage.setItem("topPanelHeightPercent", "50");
  }
}

/* =======================
   Bind reset button
   ======================= */
function bindResetButton() {
  const btn = document.getElementById("reset-splitters-btn");

  if (!btn) {
    console.warn("reset-splitters-btn not found");
    return;
  }

  // защита от двойного навешивания
  if (btn.dataset.bound === "1") return;

  btn.addEventListener("click", resetSplitters);
  btn.dataset.bound = "1";

  console.log("reset-splitters-btn bound");
}

/* =======================
   Initial bind
   ======================= */
window.addEventListener("DOMContentLoaded", bindResetButton);

/* =======================
   HTMX rebind
   ======================= */
document.addEventListener("htmx:afterSwap", bindResetButton);
