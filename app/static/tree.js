"use strict";

const STORAGE_KEY = "tree-state";
const state = JSON.parse(localStorage.getItem(STORAGE_KEY) || "{}");
let allExpanded = false;

function save() {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(state));
}

// ====== Toggle одного узла ======
window.toggleNode = function (id, arrowEl) {
  const ul = document.querySelector(`ul[data-id="${id}"]`);
  if (!ul) return;

  const expanded = ul.classList.toggle("expanded");
  arrowEl?.classList.toggle("expanded", expanded);

  ul.style.maxHeight = expanded ? ul.scrollHeight + "px" : "0";
  state[id] = expanded;
  save();

  ul.addEventListener(
    "transitionend",
    () => {
      ul.style.maxHeight = expanded ? "2000px" : "0";
    },
    { once: true },
  );
};

// ====== Toggle всех узлов ======
window.toggleAll = function () {
  allExpanded = !allExpanded;
  const nodes = document.querySelectorAll(".collapsible");

  nodes.forEach((ul) => {
    const arrow = ul.previousElementSibling.querySelector(".arrow");

    ul.classList.toggle("expanded", allExpanded);
    arrow?.classList.toggle("expanded", allExpanded);
    ul.style.maxHeight = allExpanded ? ul.scrollHeight + "px" : "0";
    state[ul.dataset.id] = allExpanded;

    ul.addEventListener(
      "transitionend",
      () => {
        ul.style.maxHeight = allExpanded ? "2000px" : "0";
      },
      { once: true },
    );
  });

  state.toggleAll = allExpanded;
  save();

  const btn = document.getElementById("toggle-all-btn");
  if (btn) btn.textContent = allExpanded ? "Свернуть всё" : "Развернуть всё";
};

// ====== Restore состояния дерева ======
function restoreTreeState() {
  const nodes = document.querySelectorAll(".collapsible");
  nodes.forEach((ul) => {
    const arrow = ul.previousElementSibling.querySelector(".arrow");
    const expanded = !!state[ul.dataset.id];
    ul.classList.toggle("expanded", expanded);
    arrow?.classList.toggle("expanded", expanded);
    ul.style.maxHeight = expanded ? "2000px" : "0";
  });

  const btn = document.getElementById("toggle-all-btn");
  if (btn) {
    allExpanded = state.toggleAll === true;
    btn.textContent = allExpanded ? "Свернуть всё" : "Развернуть всё";
  }
}

// ====== Highlight активного узла и скролл ======
function highlightSelectedNodeFromURL() {
  const params = new URLSearchParams(window.location.search);
  const targetType = params.get("target_type");
  const targetId = params.get("target_id");

  document
    .querySelectorAll(".label.is-active")
    .forEach((el) => el.classList.remove("is-active"));

  if (!targetType || !targetId) return;

  const active = document.querySelector(
    `.label[data-node-type="${targetType}"][data-node-id="${targetId}"]`,
  );

  if (active) {
    active.classList.add("is-active");
    active.scrollIntoView({ behavior: "smooth", block: "center" });

    let parentUl = active.closest("ul.collapsible");
    while (parentUl) {
      const arrow = parentUl.previousElementSibling.querySelector(".arrow");
      parentUl.classList.add("expanded");
      arrow?.classList.add("expanded");
      state[parentUl.dataset.id] = true;
      save();
      parentUl = parentUl.parentElement.closest("ul.collapsible");
    }
  }
}

// ====== Инициализация дерева ======
function initTree() {
  restoreTreeState();
  highlightSelectedNodeFromURL();
}

// ====== Автообновление дерева через tree-reload ======
document.addEventListener("tree-reload", async function () {
  const leftPanel = document.getElementById("left-panel");
  if (!leftPanel) return;

  await htmx.ajax("GET", "/ui/left-menu", {
    target: "#left-panel",
    swap: "innerHTML",
  });

  // После вставки нового HTML
  initTree();
});

// ====== Инициализация после загрузки и htmx swap ======
document.addEventListener("htmx:afterSwap", initTree);
window.addEventListener("DOMContentLoaded", initTree);
