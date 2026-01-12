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
  const minHeight = 50;

  horizontalSplitter.addEventListener("mousedown", (e) => {
    draggingH = true;
    document.body.style.userSelect = "none";

    // полностью скрываем верхнюю панель
    topPanel.style.height = "0%";
    bottomPanel.style.height = "100%";
    localStorage.setItem("topPanelHeightPercent", topPanel.style.height);
  });

  document.addEventListener("mousemove", (e) => {
    if (!draggingH) return;

    const rect = topPanel.parentElement.getBoundingClientRect();
    let offsetY = e.clientY - rect.top;

    // минимум и максимум для bottom panel
    offsetY = Math.max(0, Math.min(offsetY, rect.height));

    if (offsetY < 10) {
      // полностью скрыть top panel
      topPanel.style.height = "0%";
      bottomPanel.style.height = "100%";
    } else {
      // растягиваем top panel обратно, если тянем вниз
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

  // === Double click horizontal splitter для возврата Top panel на 50% ===
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

  // === Mobile support ===
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
      let newWidth = Math.max(minLeft, Math.min(touch.clientX, maxLeft));
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
