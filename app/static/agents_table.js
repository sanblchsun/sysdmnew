// app/static/agents_table.js

// =============== ПЕРЕМЕННЫЕ =================
let clickTimer = null;
let lastClickedRow = null;

// =============== ФУНКЦИИ ДЛЯ ТАБЛИЦЫ =================

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

// Подсветка строки при клике
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

// Инициализация обработчиков кликов
function initClickHandlers() {
  const rows = document.querySelectorAll(
    ".agents-table tbody tr[data-agent-id]",
  );

  rows.forEach((row) => {
    // Обработчик одинарного клика
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
        highlightAgentRow(agentId);
        setAgentIdInURL(agentId);
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

// Инициализация таблицы
function initAgentTable() {
  const agentId = getAgentIdFromURL();
  highlightAgentRow(agentId);
  initClickHandlers();
}

// =============== ФУНКЦИИ ДЛЯ МОДАЛЬНОГО ОКНА =================

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
  modal.innerHTML = `
    <div class="modal-content" style="text-align: center; padding: 40px;">
      <div class="loading-spinner"></div>
      <p>Загрузка данных агента...</p>
    </div>
  `;
  modal.style.display = "block";
  document.body.style.overflow = "hidden";

  try {
    // Загружаем контент модального окна
    const response = await fetch(`/ui/modal-panel?agent_id=${agentId}`);
    const html = await response.text();

    modal.innerHTML = html;

    // Инициализируем кнопки в модальном окне
    setTimeout(initModalButtons, 100);
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

// Функция закрытия модального окна
function closeAgentModal() {
  const modal = document.getElementById("agent-modal");
  if (modal) {
    modal.style.display = "none";
    modal.innerHTML = "";
    document.body.style.overflow = "auto";
  }
}

// Инициализация кнопок в модальном окне
function initModalButtons() {
  // Кнопка сохранения
  const saveBtn = document.getElementById("save-department-btn");
  if (saveBtn) {
    saveBtn.addEventListener("click", function () {
      const agentId = parseInt(this.getAttribute("data-agent-id"));
      changeDepartment(agentId);
    });
  }

  // Кнопка отмены
  const cancelBtn = document.getElementById("cancel-btn");
  if (cancelBtn) {
    cancelBtn.addEventListener("click", closeAgentModal);
  }

  // Кнопка закрытия (крестик)
  const closeBtn = document.getElementById("modal-close-btn");
  if (closeBtn) {
    closeBtn.addEventListener("click", closeAgentModal);
  }
}

// Функция изменения отдела
async function changeDepartment(agentId) {
  const selectedDept = document.querySelector(
    'input[name="department"]:checked',
  );
  if (!selectedDept) {
    alert("Пожалуйста, выберите отдел");
    return;
  }

  const departmentId = parseInt(selectedDept.value);

  const saveBtn = document.getElementById("save-department-btn");
  const originalText = saveBtn.textContent;
  saveBtn.textContent = "Сохранение...";
  saveBtn.disabled = true;

  try {
    const response = await fetch(
      "/api/agent/" + agentId + "/change-department",
      {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify({ department_id: departmentId }),
      },
    );

    const result = await response.json();

    if (result.status === "success") {
      showNotification(result.message, "success");

      setTimeout(() => {
        closeAgentModal();

        const targetId = document.getElementById("target-id")?.value;
        const targetType = document.getElementById("target-type")?.value;

        if (targetId && targetType) {
          htmx.ajax(
            "GET",
            "/ui/top-panel?target_id=" +
              targetId +
              "&target_type=" +
              targetType,
            {
              target: "#top-right",
              swap: "innerHTML",
            },
          );
        }
      }, 1000);
    } else {
      showNotification(result.message, "error");
      saveBtn.textContent = originalText;
      saveBtn.disabled = false;
    }
  } catch (error) {
    console.error("Ошибка:", error);
    showNotification("Ошибка сохранения", "error");
    saveBtn.textContent = originalText;
    saveBtn.disabled = false;
  }
}

// Функция показа уведомлений
function showNotification(message, type = "info") {
  const container =
    document.getElementById("notification") || createNotificationContainer();

  const notification = document.createElement("div");
  notification.className = "notification notification-" + type;
  notification.textContent = message;
  notification.style.cssText =
    "padding: 10px 15px; margin-bottom: 10px; border-radius: 5px; color: white; font-weight: bold; animation: slideIn 0.3s;";

  if (type === "success") {
    notification.style.backgroundColor = "#4CAF50";
  } else if (type === "error") {
    notification.style.backgroundColor = "#f44336";
  } else {
    notification.style.backgroundColor = "#2196F3";
  }

  container.appendChild(notification);

  setTimeout(() => {
    notification.style.opacity = "0";
    notification.style.transition = "opacity 0.5s";
    setTimeout(() => notification.remove(), 500);
  }, 5000);
}

function createNotificationContainer() {
  const container = document.createElement("div");
  container.id = "notification";
  container.style.cssText =
    "position: fixed; top: 20px; right: 20px; z-index: 10000; max-width: 300px;";
  document.body.appendChild(container);
  return container;
}

// =============== ИНИЦИАЛИЗАЦИЯ =================

// Инициализация при загрузке
window.addEventListener("DOMContentLoaded", function () {
  initAgentTable();

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

  // Добавляем стили для анимации
  const style = document.createElement("style");
  style.textContent =
    "@keyframes slideIn { from { transform: translateX(100%); opacity: 0; } to { transform: translateX(0); opacity: 1; } }";
  document.head.appendChild(style);
});

// Обработка HTMX обновлений
document.addEventListener("htmx:afterSwap", (evt) => {
  if (
    evt.target.querySelector(".agents-table") ||
    evt.target.classList.contains("agents-table")
  ) {
    initAgentTable();
  }
});
