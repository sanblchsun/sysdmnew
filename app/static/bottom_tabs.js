// app/static/bottom_tabs.js
"use strict";

function initBottomTabs() {
  const tabs = document.querySelectorAll(".bottom-tabs .tab");
  const panes = document.querySelectorAll(".tab-pane");

  tabs.forEach((tab) => {
    tab.addEventListener("click", () => {
      const name = tab.dataset.tab;

      tabs.forEach((t) => t.classList.remove("active"));
      panes.forEach((p) => p.classList.remove("active"));

      tab.classList.add("active");

      const pane = document.getElementById(`tab-${name}`);
      if (pane) pane.classList.add("active");
    });
  });
}

document.addEventListener("DOMContentLoaded", initBottomTabs);
document.addEventListener("htmx:afterSwap", initBottomTabs);

function initActionsDropdown() {
  const btn = document.getElementById("actions-btn");
  const menu = document.getElementById("actions-menu");

  if (!btn || !menu) return;

  btn.addEventListener("click", (e) => {
    e.stopPropagation();
    const open = menu.classList.toggle("open");
    btn.setAttribute("aria-expanded", open);
  });

  document.addEventListener("click", () => {
    menu.classList.remove("open");
    btn.setAttribute("aria-expanded", "false");
  });

  menu.addEventListener("click", (e) => {
    e.stopPropagation();
  });
}

document.addEventListener("DOMContentLoaded", initActionsDropdown);
document.addEventListener("htmx:afterSwap", initActionsDropdown);
