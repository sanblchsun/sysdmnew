const STORAGE_KEY = "tree-state";
let allExpanded = false;

const state = JSON.parse(localStorage.getItem(STORAGE_KEY) || "{}");

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
  save();
  document.getElementById("toggle-all-btn").textContent = allExpanded
    ? "Свернуть всё"
    : "Развернуть всё";
}

// Restore tree state
const observer = new MutationObserver(() => {
  const nodes = document.querySelectorAll(".collapsible");
  if (!nodes.length) return;
  nodes.forEach((ul) => {
    const arrow = ul.previousElementSibling.querySelector(".arrow");
    const expanded = !!state[ul.dataset.id];
    ul.classList.toggle("expanded", expanded);
    arrow?.classList.toggle("expanded", expanded);
  });
  observer.disconnect();
});
observer.observe(document.body, { childList: true, subtree: true });
