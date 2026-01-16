// app/static/tree.js
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
  state["toggleAll"] = allExpanded; // Сохраняем состояние кнопки
  save();
  document.getElementById("toggle-all-btn").textContent = allExpanded
    ? "Свернуть всё"
    : "Развернуть всё";
}

// Функция восстановления состояния после полной загрузки дерева
function restoreTreeState() {
  const nodes = document.querySelectorAll(".collapsible");
  if (!nodes.length) return;

  nodes.forEach((ul) => {
    const arrow = ul.previousElementSibling.querySelector(".arrow");
    const expanded = !!state[ul.dataset.id];
    ul.classList.toggle("expanded", expanded);
    arrow?.classList.toggle("expanded", expanded);
  });

  // Восстановление состояния кнопки
  const btn = document.getElementById("toggle-all-btn");
  if (btn) {
    allExpanded = state["toggleAll"] === true;
    btn.textContent = allExpanded ? "Свернуть всё" : "Развернуть всё";
  }
}

// Добавляем обработчик события для завершения загрузки дерева
document.addEventListener("htmx:afterSwap", () => {
  restoreTreeState(); // Начинаем восстановление состояния после успешной замены
});

// Первоначальное восстановление (для тех случаев, когда DOM уже готов)
if (document.readyState === "complete") {
  restoreTreeState();
} else {
  window.addEventListener("DOMContentLoaded", restoreTreeState);
}
