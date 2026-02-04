// app/static/agents_table.js

// Подсветка строки таблицы по agent_id
function highlightAgentRow(agentId) {
  const table = document.querySelector(".agents-table");
  if (!table) return;
  table
    .querySelectorAll("tr.is-active")
    .forEach((el) => el.classList.remove("is-active"));
  if (!agentId) return;
  const row = table.querySelector(`tr[data-agent-id="${agentId}"]`);
  if (row) row.classList.add("is-active");
}

// Получаем agent_id из URL
function getAgentIdFromURL() {
  return new URLSearchParams(window.location.search).get("agent_id");
}

// Обновляем URL без перезагрузки
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

// Инициализация таблицы
function initAgentTable() {
  const agentId = getAgentIdFromURL();
  highlightAgentRow(agentId);

  const table = document.querySelector(".agents-table");
  if (!table) return;

  table.querySelectorAll("tr").forEach((row) => {
    row.onclick = () => {
      const agentId = row.dataset.agentId;
      highlightAgentRow(agentId);
      setAgentIdInURL(agentId); // URL обновляется только через JS
    };
  });
}

// ---------------- INITIAL LOAD ----------------
window.addEventListener("DOMContentLoaded", initAgentTable);

// ---------------- HTMX SWAP HANDLERS ----------------
document.addEventListener("htmx:afterSwap", (evt) => {
  if (
    evt.target.querySelector(".agents-table") ||
    evt.target.classList.contains("agents-table")
  ) {
    initAgentTable();
  }
});

// app/static/agents_table.js - добавляем функции
let clickTimer = null;
let lastClickedRow = null;

// Инициализация обработчиков кликов
function initClickHandlers() {
  const rows = document.querySelectorAll(
    ".agents-table tbody tr[data-agent-id]",
  );

  rows.forEach((row) => {
    // Уже есть HTMX для одинарного клика, добавляем двойной
    row.addEventListener("click", function (event) {
      const agentId = this.getAttribute("data-agent-id");

      // Если это второй клик по той же строке
      if (this === lastClickedRow && clickTimer) {
        clearTimeout(clickTimer);
        clickTimer = null;
        lastClickedRow = null;
        return; // Двойной клик обработается отдельно
      }

      lastClickedRow = this;

      // Устанавливаем таймер для одинарного клика
      clickTimer = setTimeout(() => {
        // Подсвечиваем строку
        highlightRow(this);
        clickTimer = null;
        lastClickedRow = null;
      }, 300);
    });

    // Обработчик двойного клика
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

// Функция открытия модального окна агента
async function openAgentModal(agentId, rowElement) {
  console.log("Двойной клик по агенту:", agentId);

  // Подсвечиваем строку
  highlightRow(rowElement);

  // Показываем модальное окно
  const modal = document.getElementById("agent-modal");
  if (!modal) {
    console.error("Модальное окно не найдено");
    return;
  }

  // Показываем загрузку
  modal.innerHTML =
    '<div style="text-align: center; padding: 40px;">Загрузка...</div>';
  modal.style.display = "block";
  document.body.style.overflow = "hidden";

  try {
    // Загружаем контент модального окна
    const response = await fetch(`/ui/modal-panel?agent_id=${agentId}`);
    const html = await response.text();

    modal.innerHTML = html;

    // Добавляем обработчик закрытия для крестика
    const closeBtn = modal.querySelector(".modal-close");
    if (closeBtn) {
      closeBtn.addEventListener("click", closeAgentModal);
    }
  } catch (error) {
    console.error("Ошибка загрузки модального окна:", error);
    modal.innerHTML =
      '<div style="padding: 20px; color: red;">Ошибка загрузки данных агента</div>';
  }
}

// Функция закрытия модального окна
function closeAgentModal() {
  const modal = document.getElementById("agent-modal");
  if (modal) {
    modal.style.display = "none";
    modal.innerHTML = "";
    document.body.style.overflow = "auto";
  }
}

// Подсветка строки
function highlightRow(row) {
  if (!row) return;

  // Убираем подсветку у всех строк
  document.querySelectorAll(".agents-table tbody tr").forEach((r) => {
    r.classList.remove("row-active");
  });

  // Добавляем подсветку текущей строке
  row.classList.add("row-active");

  // Автоснятие через 2 секунды
  setTimeout(() => {
    row.classList.remove("row-active");
  }, 2000);
}

// Инициализация при загрузке
document.addEventListener("DOMContentLoaded", function () {
  setTimeout(initClickHandlers, 500);

  // Также инициализируем при обновлении через HTMX
  document.body.addEventListener("htmx:afterSwap", function (event) {
    if (event.detail.target.id === "top-right") {
      setTimeout(initClickHandlers, 100);
    }
  });

  // Закрытие по клику вне модального окна
  document.addEventListener("click", function (event) {
    const modal = document.getElementById("agent-modal");
    if (modal && event.target === modal) {
      closeAgentModal();
    }
  });

  // Закрытие по ESC
  document.addEventListener("keydown", function (event) {
    if (event.key === "Escape") {
      closeAgentModal();
    }
  });
});
