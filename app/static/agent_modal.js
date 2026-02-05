// app/static/agent_modal.js
"use strict";

// ====== Закрытие модалки ======
window.closeAgentModal = function () {
  const modal = document.getElementById("agent-modal");
  if (modal) {
    modal.style.display = "none";
    modal.innerHTML = "";
    document.body.style.overflow = "auto";
  }
};

// ====== Изменение отдела ======
async function changeDepartment(agentId) {
  const selected = document.querySelector('input[name="department"]:checked');
  if (!selected) {
    alert("Выберите отдел");
    return;
  }

  const deptId = parseInt(selected.value);
  const btn = document.getElementById("save-department-btn");
  const oldText = btn.textContent;
  btn.textContent = "💾 Сохранение...";
  btn.disabled = true;

  try {
    const response = await fetch(`/api/agent/${agentId}/change-department`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ department_id: deptId }),
    });

    const result = await response.json();

    if (result.status === "success") {
      // Закрываем модалку
      closeAgentModal();

      // Обновляем top-panel
      const targetId = document.getElementById("target-id")?.value;
      const targetType = document.getElementById("target-type")?.value;
      if (targetId && targetType) {
        htmx.ajax(
          "GET",
          `/ui/top-panel?target_id=${targetId}&target_type=${targetType}`,
          { target: "#top-right", swap: "innerHTML" },
        );
      }

      // Обновляем дерево слева и таблицу справа
      htmx.trigger(document.body, "data-reload");
    } else {
      alert(result.message);
      btn.textContent = oldText;
      btn.disabled = false;
    }
  } catch (error) {
    console.error("Ошибка:", error);
    alert("Ошибка сохранения");
    btn.textContent = oldText;
    btn.disabled = false;
  }
}

// ====== Инициализация кнопок модалки ======
function initAgentModal() {
  const saveBtn = document.getElementById("save-department-btn");
  if (saveBtn) {
    saveBtn.addEventListener("click", function () {
      const agentId = parseInt(this.getAttribute("data-agent-id"));
      changeDepartment(agentId);
    });
  }

  const cancelBtn = document.getElementById("cancel-btn");
  if (cancelBtn) cancelBtn.addEventListener("click", closeAgentModal);

  const closeBtn = document.getElementById("modal-close-btn");
  if (closeBtn) closeBtn.addEventListener("click", closeAgentModal);

  // Выбираем текущий отдел по дефолту
  const currentId = document
    .querySelector(".departments-list")
    ?.getAttribute("data-current-dept-id");
  if (currentId) {
    const radio = document.querySelector(
      `input[name="department"][value="${currentId}"]`,
    );
    if (radio) radio.checked = true;
  }
}

// Делаем функцию глобальной для вызова из agents_table.js
window.initAgentModal = initAgentModal;

// ====== Автоинициализация при swap или DOMContentLoaded ======
document.addEventListener("htmx:afterSwap", initAgentModal);
window.addEventListener("DOMContentLoaded", initAgentModal);
