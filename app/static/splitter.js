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

  // === Double click collapse/expand ===
  let leftCollapsed = false;
  let bottomCollapsed = false;
  let lastLeftWidth = leftPanel.offsetWidth;
  let lastTopHeightPercent = parseFloat(topPanel.style.height) || 50;

  verticalSplitter.addEventListener("dblclick", () => {
    if (!leftCollapsed) {
      lastLeftWidth = leftPanel.offsetWidth;
      leftPanel.style.width = "0px";
    } else {
      leftPanel.style.width = `${lastLeftWidth}px`;
    }
    leftCollapsed = !leftCollapsed;
    localStorage.setItem("leftPanelWidth", leftPanel.style.width);
  });

  horizontalSplitter.addEventListener("dblclick", () => {
    if (!bottomCollapsed) {
      lastTopHeightPercent = parseFloat(topPanel.style.height);
      topPanel.style.height = "100%";
      bottomPanel.style.height = "0%";
    } else {
      topPanel.style.height = `${lastTopHeightPercent}%`;
      bottomPanel.style.height = `${100 - lastTopHeightPercent}%`;
    }
    bottomCollapsed = !bottomCollapsed;
    localStorage.setItem("topPanelHeightPercent", topPanel.style.height);
  });

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
  const minHeight = 50;

  horizontalSplitter.addEventListener("mousedown", () => {
    draggingH = true;
    document.body.style.userSelect = "none";
  });
  document.addEventListener("mousemove", (e) => {
    if (!draggingH) return;
    const rect = topPanel.parentElement.getBoundingClientRect();
    let offsetY = Math.max(
      minHeight,
      Math.min(e.clientY - rect.top, rect.height - minHeight)
    );
    const topPercent = (offsetY / rect.height) * 100;
    topPanel.style.height = `${topPercent}%`;
    bottomPanel.style.height = `${100 - topPercent}%`;
    localStorage.setItem("topPanelHeightPercent", `${topPercent}%`);
  });
  document.addEventListener("mouseup", () => {
    draggingH = false;
    document.body.style.userSelect = "auto";
  });

  // === Mobile support ===
  verticalSplitter.addEventListener("touchstart", (e) => {
    draggingV = true;
    e.preventDefault();
  });
  horizontalSplitter.addEventListener("touchstart", (e) => {
    draggingH = true;
    e.preventDefault();
  });

  document.addEventListener("touchmove", (e) => {
    const touch = e.touches[0];
    if (draggingV) {
      let newWidth = Math.max(minLeft, Math.min(touch.clientX, maxLeft));
      leftPanel.style.width = `${newWidth}px`;
      localStorage.setItem("leftPanelWidth", `${newWidth}px`);
    }
    if (draggingH) {
      const rect = topPanel.parentElement.getBoundingClientRect();
      let offsetY = Math.max(
        minHeight,
        Math.min(touch.clientY - rect.top, rect.height - minHeight)
      );
      const topPercent = (offsetY / rect.height) * 100;
      topPanel.style.height = `${topPercent}%`;
      bottomPanel.style.height = `${100 - topPercent}%`;
      localStorage.setItem("topPanelHeightPercent", `${topPercent}%`);
    }
  });

  document.addEventListener("touchend", () => {
    draggingV = false;
    draggingH = false;
  });
});
