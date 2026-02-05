"use strict";

function initBottomTabs() {
  const tabs = document.querySelectorAll(".bottom-tabs .tab");
  const panes = document.querySelectorAll(".tab-pane");

  tabs.forEach(tab => {
    tab.addEventListener("click", () => {
      const name = tab.dataset.tab;

      tabs.forEach(t => t.classList.remove("active"));
      panes.forEach(p => p.classList.remove("active"));

      tab.classList.add("active");

      const pane = document.getElementById(`tab-${name}`);
      if (pane) pane.classList.add("active");
    });
  });
}

document.addEventListener("DOMContentLoaded", initBottomTabs);
document.addEventListener("htmx:afterSwap", initBottomTabs);
