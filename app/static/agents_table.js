// app/static/agents_table.js
"use strict";

// ======== ПЕРЕМЕННЫЕ ========
let clickTimer = null;
let lastClickedRow = null;

// ======== Подсветка строки по agent_id ========
function highlightAgentRow(agentId) {
  const table = document.querySelector(".agents-table");
  if (!table) return;

  table.querySelectorAll("tr.is-active").forEach((el) => {
    el.classList.remove("is-active");
  });

  if (!agentId) return;
  const row = table.querySelector(`tr[data-agent-id="${agentId}"]`);
  if (row) row.classList.add("is-active");
}

// ======== Подсветка строки при клике ========
function highlightRow(row) {
  if (!row) return;

  // Снимаем подсветку со всех
  document.querySelectorAll(".agents-table tbody tr").forEach((r) => {
    r.classList.remove("row-active");
  });

  row.classList.add("row-active");

  // Автоснятие через 2 секунды
  setTimeout(() => {
    row.classList.remove("row-active");
  }, 2000);
}

// ======== Работа с URL ========
function getAgentIdFromURL() {
  return new URLSearchParams(window.location.search).get("agent_id");
}

function setAgentIdInURL(agentId) {
  const params = new URLSearchParams(window.location.search);
  if (agentId) params.set("agent_id", agentId);
  else params.delete("agent_id");
  history.replaceState(
    null,
    "",
    `${window.location.pathname}?${params.toString()}`,
  );
}

// ======== Обработчики кликов ========
function initClickHandlers() {
  const rows = document.querySelectorAll(
    ".agents-table tbody tr[data-agent-id]",
  );
  rows.forEach((row) => {
    // Одинарный клик
    row.addEventListener("click", function () {
      const agentId = this.getAttribute("data-agent-id");

      if (this === lastClickedRow && clickTimer) {
        clearTimeout(clickTimer);
        clickTimer = null;
        lastClickedRow = null;
        return;
      }

      lastClickedRow = this;
      clickTimer = setTimeout(() => {
        highlightRow(this);
        highlightAgentRow(agentId);
        setAgentIdInURL(agentId);
        clickTimer = null;
        lastClickedRow = null;
      }, 300);
    });

    // Двойной клик
    row.addEventListener("dblclick", function (event) {
      event.preventDefault();
      const agentId = this.getAttribute("data-agent-id");

      if (clickTimer) {
        clearTimeout(clickTimer);
        clickTimer = null;
      }

      openAgentModal(agentId, this);
    });
  });
}

// ======== Инициализация таблицы ========
function initAgentTable() {
  const agentId = getAgentIdFromURL();
  highlightAgentRow(agentId);
  initClickHandlers();
}

// ======== Открытие модалки ========
async function openAgentModal(agentId, rowElement) {
  highlightRow(rowElement);

  const modal = document.getElementById("agent-modal");
  if (!modal) return;

  modal.innerHTML = `
    <div class="modal-content" style="text-align: center; padding: 40px;">
      <div class="loading-spinner"></div>
      <p>Загрузка данных агента...</p>
    </div>
  `;
  modal.style.display = "block";
  document.body.style.overflow = "hidden";

  try {
    const response = await fetch(`/ui/modal-panel?agent_id=${agentId}`);
    const html = await response.text();
    modal.innerHTML = html;

    // Инициализация кнопок внутри модалки
    window.initAgentModal();
  } catch (error) {
    console.error("Ошибка загрузки модального окна:", error);
    modal.innerHTML = `
      <div class="modal-content" style="padding: 20px; color: red;">
        <h3>Ошибка</h3>
        <p>Не удалось загрузить данные агента</p>
        <button onclick="closeAgentModal()" style="padding: 10px 20px; margin-top: 20px;">
          Закрыть
        </button>
      </div>
    `;
  }
}

// ======== Инициализация ========
window.addEventListener("DOMContentLoaded", function () {
  initAgentTable();

  // Закрытие модалки по клику вне
  document.addEventListener("click", function (event) {
    const modal = document.getElementById("agent-modal");
    if (modal && event.target === modal) closeAgentModal();
  });

  // Закрытие по ESC
  document.addEventListener("keydown", function (event) {
    if (event.key === "Escape") closeAgentModal();
  });
});

// ======== Обработка HTMX обновлений ========
document.addEventListener("htmx:afterSwap", (evt) => {
  if (
    evt.target.querySelector(".agents-table") ||
    evt.target.classList.contains("agents-table")
  ) {
    initAgentTable();
  }
});
