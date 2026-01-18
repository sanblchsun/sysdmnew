const STORAGE_KEY = "tree-state";
let allExpanded = false;

const state = JSON.parse(localStorage.getItem(STORAGE_KEY) || {});

function save() {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(state));
}

function toggleNode(id, arrow) {
  const ul = document.querySelector(`ul[data-id="${id}"]`);
  if (!ul) return;
  const expanded = ul.classList.toggle("expanded");
  arrow.classList.toggle("expanded", expanded);
  state[id] = expanded;
  save();
}

function toggleAll() {
  allExpanded = !allExpanded;
  document.querySelectorAll(".collapsible").forEach((ul) => {
    const arrow = ul.previousElementSibling.querySelector(".arrow");
    ul.classList.toggle("expanded", allExpanded);
    arrow?.classList.toggle("expanded", allExpanded);
    state[ul.dataset.id] = allExpanded;
  });
  state["toggleAll"] = allExpanded;
  save();
  const btn = document.getElementById("toggle-all-btn");
  if (btn) btn.textContent = allExpanded ? "Свернуть всё" : "Развернуть всё";
}

function restoreTreeState() {
  const nodes = document.querySelectorAll(".collapsible");
  if (!nodes.length) return;

  nodes.forEach((ul) => {
    const arrow = ul.previousElementSibling.querySelector(".arrow");
    const expanded = !!state[ul.dataset.id];
    ul.classList.toggle("expanded", expanded);
    arrow?.classList.toggle("expanded", expanded);
  });

  const btn = document.getElementById("toggle-all-btn");
  if (btn) {
    allExpanded = state["toggleAll"] === true;
    btn.textContent = allExpanded ? "Свернуть всё" : "Развернуть всё";
  }
}

document.addEventListener("htmx:afterSwap", restoreTreeState);

if (document.readyState === "complete") {
  restoreTreeState();
} else {
  window.addEventListener("DOMContentLoaded", restoreTreeState);
}

function highlightSelectedNodeFromURL() {
  const params = new URLSearchParams(window.location.search);
  const targetType = params.get("target_type");
  const targetId = params.get("target_id");

  document.querySelectorAll(".label.is-active").forEach((el) => {
    el.classList.remove("is-active");
  });

  if (!targetType || !targetId) return;

  const active = document.querySelector(
    `.label[data-node-type="${targetType}"][data-node-id="${targetId}"]`,
  );

  if (active) active.classList.add("is-active");
}

document.addEventListener("htmx:afterSwap", highlightSelectedNodeFromURL);
window.addEventListener("DOMContentLoaded", highlightSelectedNodeFromURL);
